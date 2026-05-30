package core

import (
	"context"
	"crypto/tls"
	"encoding/binary"
	"fmt"
	"log"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"hasta-vaquet/protocol"

	"github.com/gorilla/websocket"
)

const (
	TransportAuto = "auto"
	TransportWSS  = "wss"
	TransportWS   = "ws"
	TransportQUIC = "quic"
	TransportUDP  = "udp"
)

var defaultTransportPriority = []string{TransportWSS, TransportQUIC, TransportUDP}

// wsBufPool — reusable буферы для WebSocket/QUIC фреймов.
var wsBufPool = sync.Pool{
	New: func() any { return make([]byte, 65535+14) },
}

type TransportManager struct {
	config *protocol.Config
	conn   net.Conn
	active string
	mu     sync.Mutex
}

func NewTransportManager(cfg *protocol.Config) *TransportManager {
	return &TransportManager{config: cfg}
}

func (tm *TransportManager) ActiveTransport() string {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	return tm.active
}

func (tm *TransportManager) Dial(ctx context.Context) (net.Conn, string, error) {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	priorities := tm.resolvePriorities()
	var lastErr error
	for _, t := range priorities {
		conn, err := tm.dialTransport(ctx, t)
		if err == nil {
			tm.conn = conn
			tm.active = t
			log.Printf("[TRANSPORT] Connected via %s", t)
			return conn, t, nil
		}
		lastErr = err
		log.Printf("[TRANSPORT] %s failed: %v", t, err)
	}
	return nil, "", fmt.Errorf("all transports failed, last error: %w", lastErr)
}

func (tm *TransportManager) Fallback(ctx context.Context) (net.Conn, string, error) {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	priorities := tm.resolvePriorities()
	start := 0
	for i, t := range priorities {
		if t == tm.active {
			start = (i + 1) % len(priorities)
			break
		}
	}

	var lastErr error
	for i := 0; i < len(priorities); i++ {
		t := priorities[(start+i)%len(priorities)]
		if t == tm.active {
			continue
		}
		conn, err := tm.dialTransport(ctx, t)
		if err == nil {
			if tm.conn != nil {
				tm.conn.Close()
			}
			tm.conn = conn
			tm.active = t
			log.Printf("[TRANSPORT] Fallback to %s", t)
			return conn, t, nil
		}
		lastErr = err
	}
	return nil, "", fmt.Errorf("fallback failed, last error: %w", lastErr)
}

func (tm *TransportManager) resolvePriorities() []string {
	cfg := tm.config
	if cfg.Transport != "" && cfg.Transport != TransportAuto {
		return []string{cfg.Transport}
	}
	if len(cfg.TransportPriority) > 0 {
		return cfg.TransportPriority
	}
	if cfg.CDNDomain != "" {
		return []string{TransportWSS, TransportUDP}
	}
	return []string{TransportUDP}
}

func (tm *TransportManager) dialTransport(ctx context.Context, transport string) (net.Conn, error) {
	cfg := tm.config
	switch transport {
	case TransportWSS:
		if cfg.CDNDomain != "" {
			return DialWSS(ctx, cfg.CDNDomain, 443, &tls.Config{ServerName: cfg.CDNDomain})
		}
		return DialWSS(ctx, cfg.ServerIP, cfg.Port, &tls.Config{ServerName: cfg.ServerIP})
	case TransportWS:
		return DialWS(ctx, cfg.ServerIP, 19998)
	case TransportQUIC:
		return DialQUICUDP(ctx, cfg.ServerIP, cfg.Port, cfg.ShortID, cfg.SecretKey)
	case TransportUDP:
		return DialRawUDP(ctx, cfg.ServerIP, cfg.Port)
	default:
		return nil, fmt.Errorf("unknown transport: %s", transport)
	}
}

// ─── WSS/WS Transport via gorilla/websocket ───────────────────────

type wsConn struct {
	ws   *websocket.Conn
	addr net.Addr
}

func (c *wsConn) Read(b []byte) (int, error) {
	_, msg, err := c.ws.ReadMessage()
	if err != nil {
		return 0, err
	}
	return copy(b, msg), nil
}

func (c *wsConn) Write(b []byte) (int, error) {
	if err := c.ws.WriteMessage(websocket.BinaryMessage, b); err != nil {
		return 0, err
	}
	return len(b), nil
}

func (c *wsConn) Close() error                       { return c.ws.Close() }
func (c *wsConn) SetReadDeadline(t time.Time) error  { return c.ws.SetReadDeadline(t) }
func (c *wsConn) SetWriteDeadline(t time.Time) error { return c.ws.SetWriteDeadline(t) }
func (c *wsConn) SetDeadline(t time.Time) error      { return c.ws.UnderlyingConn().SetDeadline(t) }
func (c *wsConn) RemoteAddr() net.Addr               { return c.addr }
func (c *wsConn) LocalAddr() net.Addr {
	if c.ws != nil && c.ws.UnderlyingConn() != nil {
		return c.ws.UnderlyingConn().LocalAddr()
	}
	return c.addr
}

// DialWSS connects via WSS using gorilla/websocket with NextProtos=http/1.1.
// Goes through Cloudflare (TLS).
func DialWSS(ctx context.Context, server string, port int, tlsConfig *tls.Config) (net.Conn, error) {
	if tlsConfig == nil {
		tlsConfig = &tls.Config{ServerName: server}
	}
	tlsConfig.NextProtos = []string{"http/1.1"}

	url := fmt.Sprintf("wss://%s:%d/hasta-vaquet/ws", server, port)
	dialer := &websocket.Dialer{
		HandshakeTimeout: 12 * time.Second,
		TLSClientConfig:  tlsConfig,
		WriteBufferSize:  65535 + 14,
	}

	ws, resp, err := dialer.DialContext(ctx, url, nil)
	if err != nil {
		return nil, fmt.Errorf("wss dial %s: %w", url, err)
	}
	resp.Body.Close()

	addr, _ := net.ResolveTCPAddr("tcp", fmt.Sprintf("%s:%d", server, port))
	log.Printf("[WSS] Connected via %s -> %s", ws.UnderlyingConn().LocalAddr(), ws.UnderlyingConn().RemoteAddr())
	return &wsConn{ws: ws, addr: addr}, nil
}

// DialWS connects via plain WS (no TLS) directly to the server.
// Bypasses Cloudflare. Use when DPI evasion is not needed.
func DialWS(ctx context.Context, server string, port int) (net.Conn, error) {
	url := fmt.Sprintf("ws://%s:%d/hasta-vaquet/ws", server, port)
	dialer := &websocket.Dialer{
		HandshakeTimeout: 10 * time.Second,
		WriteBufferSize:  65535 + 14,
	}

	ws, resp, err := dialer.DialContext(ctx, url, nil)
	if err != nil {
		return nil, fmt.Errorf("ws dial %s: %w", url, err)
	}
	resp.Body.Close()

	addr, _ := net.ResolveTCPAddr("tcp", fmt.Sprintf("%s:%d", server, port))
	log.Printf("[WS] Connected via %s -> %s", ws.UnderlyingConn().LocalAddr(), ws.UnderlyingConn().RemoteAddr())
	return &wsConn{ws: ws, addr: addr}, nil
}

// ─── QUIC-header UDP Transport ────────────────────────────────────

type quicUDPConn struct {
	*net.UDPConn
	key         [32]byte
	shortID     uint16
	routingSalt string
	packetNum   atomic.Uint64
}

func (c *quicUDPConn) Write(b []byte) (int, error) {
	pn := uint32(c.packetNum.Add(1) - 1)
	// Pre-allocate single buffer from pool for QUIC frame
	frame := wsBufPool.Get().([]byte)[:7+len(b)]
	frame[0] = 0x40 | byte(pn&0x07)                    // QUIC Short Header + spin bit
	binary.BigEndian.PutUint16(frame[1:3], 0)          // connID[0:2] = 0
	binary.BigEndian.PutUint16(frame[3:5], c.shortID)  // connID[2:4] = shortID (plain)
	binary.BigEndian.PutUint16(frame[5:7], uint16(pn)) // packet number
	copy(frame[7:], b)
	n, err := c.UDPConn.Write(frame)
	wsBufPool.Put(frame[:cap(frame)])
	if n > 0 && n > len(b) {
		return len(b), err
	}
	return n, err
}

func (c *quicUDPConn) Read(b []byte) (int, error) {
	// Server responds in standard format (no QUIC header on responses)
	return c.UDPConn.Read(b)
}

func DialQUICUDP(ctx context.Context, server string, port int, shortID uint16, secretKey string) (net.Conn, error) {
	addr := &net.UDPAddr{IP: net.ParseIP(server), Port: port}
	conn, err := net.DialUDP("udp", nil, addr)
	if err != nil {
		return nil, fmt.Errorf("quic-udp dial %s:%d: %w", server, port, err)
	}
	conn.SetWriteBuffer(2 * 1024 * 1024)
	conn.SetReadBuffer(2 * 1024 * 1024)
	// QUIC header key = DeriveKey(secretKey) = peer.Key on server
	quicKey := DeriveKey(secretKey)
	return &quicUDPConn{
		UDPConn:     conn,
		key:         quicKey,
		shortID:     shortID,
		routingSalt: "",
	}, nil
}

// ─── Raw UDP Transport ────────────────────────────────────────────

func DialRawUDP(ctx context.Context, server string, port int) (net.Conn, error) {
	addr := &net.UDPAddr{IP: net.ParseIP(server), Port: port}
	conn, err := net.DialUDP("udp", nil, addr)
	if err != nil {
		return nil, fmt.Errorf("udp dial %s:%d: %w", server, port, err)
	}
	conn.SetWriteBuffer(2 * 1024 * 1024)
	conn.SetReadBuffer(2 * 1024 * 1024)
	return conn, nil
}
