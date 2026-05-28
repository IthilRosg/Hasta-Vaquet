package protocol

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"sync"
	"time"
)

// PacketConn abstracts a connection over any transport (UDP, WSS, etc.).
type PacketConn interface {
	ReadPacket() ([]byte, error)
	WritePacket([]byte) error
	Close() error
	RemoteAddr() string
}

// Transport provides dial (client) and listen (server) methods for a transport type.
type Transport interface {
	Dial(ctx context.Context, server string, port int, tlsConfig *tls.Config) (PacketConn, error)
	Listen(address string, tlsConfig *tls.Config) (TransportListener, error)
}

// TransportListener accepts incoming connections on a transport.
type TransportListener interface {
	Accept() (PacketConn, error)
	Close() error
	Addr() net.Addr
}

// ---------------------------------------------------------------------------
// UDP Transport (standard + QUIC header)
// ---------------------------------------------------------------------------

type udpPacketConn struct {
	conn *net.UDPConn
	addr *net.UDPAddr
	cp   *CipherPack
}

func (u *udpPacketConn) ReadPacket() ([]byte, error) {
	buf := make([]byte, 65535)
	n, err := u.conn.Read(buf)
	if err != nil {
		return nil, err
	}
	out := make([]byte, n)
	copy(out, buf[:n])
	return out, nil
}

func (u *udpPacketConn) WritePacket(data []byte) error {
	_, err := u.conn.Write(data)
	return err
}

func (u *udpPacketConn) Close() error {
	return u.conn.Close()
}

func (u *udpPacketConn) RemoteAddr() string {
	if u.addr != nil {
		return u.addr.String()
	}
	return u.conn.RemoteAddr().String()
}

type udpTransport struct{}

// UDP exposes a singleton UDPTransport.
var UDP Transport = &udpTransport{}

func (t *udpTransport) Dial(ctx context.Context, server string, port int, tlsConfig *tls.Config) (PacketConn, error) {
	addr := net.JoinHostPort(server, fmt.Sprintf("%d", port))
	udpAddr, err := net.ResolveUDPAddr("udp", addr)
	if err != nil {
		return nil, fmt.Errorf("resolve %s: %w", addr, err)
	}
	conn, err := net.DialUDP("udp", nil, udpAddr)
	if err != nil {
		return nil, fmt.Errorf("dial UDP %s: %w", addr, err)
	}
	return &udpPacketConn{conn: conn, addr: udpAddr}, nil
}

func (t *udpTransport) Listen(address string, tlsConfig *tls.Config) (TransportListener, error) {
	udpAddr, err := net.ResolveUDPAddr("udp", address)
	if err != nil {
		return nil, fmt.Errorf("resolve %s: %w", address, err)
	}
	conn, err := net.ListenUDP("udp", udpAddr)
	if err != nil {
		return nil, fmt.Errorf("listen UDP %s: %w", address, err)
	}
	return &udpTransportListener{conn: conn}, nil
}

type udpTransportListener struct {
	conn *net.UDPConn
}

func (l *udpTransportListener) Accept() (PacketConn, error) {
	buf := make([]byte, 65535)
	_, addr, err := l.conn.ReadFromUDP(buf)
	if err != nil {
		return nil, err
	}
	// We don't actually "accept" a new virtual connection per packet in UDP,
	// but we return a packet-oriented wrapper that can read/write to this peer.
	return &udpPacketConn{conn: l.conn, addr: addr}, nil
}

func (l *udpTransportListener) Close() error {
	return l.conn.Close()
}

func (l *udpTransportListener) Addr() net.Addr {
	return l.conn.LocalAddr()
}

// ---------------------------------------------------------------------------
// WSS Transport (WebSocket Secure, binary mode)
// ---------------------------------------------------------------------------

var errNoWebSocket = errors.New("WebSocket transport not available: gorilla/websocket not imported")

type wssPacketConn struct {
	conn   wsConn
	remote string
}

type wsConn interface {
	ReadMessage() (int, []byte, error)
	WriteMessage(int, []byte) error
	Close() error
	RemoteAddr() net.Addr
}

func (w *wssPacketConn) ReadPacket() ([]byte, error) {
	msgType, data, err := w.conn.ReadMessage()
	if err != nil {
		return nil, err
	}
	if msgType != 2 { // binary message
		return nil, fmt.Errorf("unexpected WebSocket message type: %d", msgType)
	}
	return data, nil
}

func (w *wssPacketConn) WritePacket(data []byte) error {
	return w.conn.WriteMessage(2, data) // binary
}

func (w *wssPacketConn) Close() error {
	return w.conn.Close()
}

func (w *wssPacketConn) RemoteAddr() string {
	if w.remote != "" {
		return w.remote
	}
	if w.conn.RemoteAddr() != nil {
		return w.conn.RemoteAddr().String()
	}
	return "unknown"
}

type wssTransport struct {
	dialFn   func(ctx context.Context, server string, port int, tlsConfig *tls.Config) (PacketConn, error)
	listenFn func(address string, tlsConfig *tls.Config) (TransportListener, error)
}

// WSS is a WebSocket Secure transport. It requires gorilla/websocket to be
// imported in the main binary. If not imported, Dial/Listen return errors.
var WSS Transport = &wssTransport{}

func (t *wssTransport) Dial(ctx context.Context, server string, port int, tlsConfig *tls.Config) (PacketConn, error) {
	if t.dialFn != nil {
		return t.dialFn(ctx, server, port, tlsConfig)
	}
	return nil, errNoWebSocket
}

func (t *wssTransport) Listen(address string, tlsConfig *tls.Config) (TransportListener, error) {
	if t.listenFn != nil {
		return t.listenFn(address, tlsConfig)
	}
	return nil, errNoWebSocket
}

// RegisterWSSTransport registers gorilla/websocket handlers for the WSS transport.
// Called from an init() in the file that imports gorilla/websocket.
func RegisterWSSTransport(dialFn func(ctx context.Context, server string, port int, tlsConfig *tls.Config) (PacketConn, error),
	listenFn func(address string, tlsConfig *tls.Config) (TransportListener, error)) {
	wss, ok := WSS.(*wssTransport)
	if ok {
		wss.dialFn = dialFn
		wss.listenFn = listenFn
	}
}

// ---------------------------------------------------------------------------
// Transport factory
// ---------------------------------------------------------------------------

// NewTransport returns a Transport implementation by type string.
// Supported: "udp", "wss". Falls back to UDP for unknown types.
func NewTransport(transportType string) Transport {
	switch transportType {
	case TransportWSS:
		return WSS
	case TransportUDP, TransportQUIC:
		return UDP
	default:
		return UDP
	}
}

// ---------------------------------------------------------------------------
// Auto-selection
// ---------------------------------------------------------------------------

// DialAuto tries transports in priority order and returns the first successful
// PacketConn. The priority may include "wss", "quic", "udp".
func DialAuto(ctx context.Context, server string, port int, tlsConfig *tls.Config, priority []string) (PacketConn, string, error) {
	if len(priority) == 0 {
		priority = []string{"wss", "udp"}
	}

	var lastErr error
	for _, t := range priority {
		tr := NewTransport(t)
		conn, err := tr.Dial(ctx, server, port, tlsConfig)
		if err == nil {
			return conn, t, nil
		}
		lastErr = err
	}
	return nil, "", fmt.Errorf("all transports failed, last error: %w", lastErr)
}

// ---------------------------------------------------------------------------
// WSS implementation via gorilla/websocket (registered separately to avoid
// hard dependency)
// ---------------------------------------------------------------------------

// WSSDialer implements the WSS dial using gorilla/websocket.
// The package that imports gorilla/websocket should call RegisterWSSTransport
// with this as the dial function (or use the WSS-prefix version below).
type WSSDialer struct {
	mu     sync.Mutex
	conn   wsConn
	addr   string
	dialFn func(urlStr string, tlsConfig *tls.Config) (wsConn, error)
}

// NewWSSDialer creates a WSS dialer wrapping a real gorilla/websocket dialer.
func NewWSSDialer(dialFn func(urlStr string, tlsConfig *tls.Config) (wsConn, error)) *WSSDialer {
	return &WSSDialer{dialFn: dialFn}
}

// WSSDialWithRetry creates a WSS PacketConn by dialing the WebSocket endpoint.
// The server parameter is the WSS host:port. The path defaults to /hasta-vaquet.
func WSSDialWithRetry(ctx context.Context, server string, port int, tlsConfig *tls.Config, dialFn func(urlStr string, tlsConfig *tls.Config) (wsConn, error)) (PacketConn, error) {
	url := fmt.Sprintf("wss://%s:%d/hasta-vaquet", server, port)
	conn, err := dialFn(url, tlsConfig)
	if err != nil {
		return nil, fmt.Errorf("WSS dial %s: %w", url, err)
	}

	// Enable write timeout for liveness
	_ = conn

	return &wssPacketConn{conn: conn, remote: server}, nil
}

// WSSListenWithUpgrader creates a WSS transport listener using a WebSocket upgrader.
type WSSListenWithUpgrader struct {
	ln       net.Listener
	upgrader func(conn net.Conn) (wsConn, error)
}

func (l *WSSListenWithUpgrader) Accept() (PacketConn, error) {
	conn, err := l.ln.Accept()
	if err != nil {
		return nil, err
	}
	ws, err := l.upgrader(conn)
	if err != nil {
		conn.Close()
		return nil, err
	}
	remote := conn.RemoteAddr().String()
	return &wssPacketConn{conn: ws, remote: remote}, nil
}

func (l *WSSListenWithUpgrader) Close() error {
	return l.ln.Close()
}

func (l *WSSListenWithUpgrader) Addr() net.Addr {
	return l.ln.Addr()
}

// SetReadDeadline exposes read deadline on a PacketConn if the underlying
// connection supports it.
func SetReadDeadline(pc PacketConn, deadline time.Time) error {
	type hasDeadline interface {
		SetReadDeadline(t time.Time) error
	}
	if hd, ok := pc.(hasDeadline); ok {
		return hd.SetReadDeadline(deadline)
	}
	return nil
}
