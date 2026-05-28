package main

import (
	"crypto/tls"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	"hasta-vaquet/protocol"

	"github.com/gorilla/websocket"
)

// ─── WSS Peer Connection ─────────────────────────────────────────

// WSSPeerConn представляет одно WebSocket-соединение от клиента.
// Реализует PacketWriter для отправки ответов обратно клиенту.
type WSSPeerConn struct {
	conn    *websocket.Conn
	ShortID uint16 // extracted after first packet; 0 until then
	mu      sync.Mutex
}

// WritePacket отправляет binary message через WebSocket.
// При ошибке (разрыв соединения) закрывает conn.
func (w *WSSPeerConn) WritePacket(data []byte, fec int) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if fec < 1 {
		fec = 1
	}
	if fec > 5 {
		fec = 5
	}
	for i := 0; i < fec; i++ {
		if err := w.conn.WriteMessage(websocket.BinaryMessage, data); err != nil {
			w.conn.Close()
			return err
		}
	}
	return nil
}

func (w *WSSPeerConn) TransportType() string { return "wss" }

// ReadMessage — обёртка над conn.ReadMessage (нужна для интерфейса).
func (w *WSSPeerConn) ReadMessage() (int, []byte, error) {
	return w.conn.ReadMessage()
}

// Close закрывает WebSocket-соединение.
func (w *WSSPeerConn) Close() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.conn.Close()
}

// ─── WSS Server ──────────────────────────────────────────────────

// WSSServer управляет HTTPS-сервером с WebSocket endpoint /ws.
type WSSServer struct {
	server    *http.Server
	upgrader  websocket.Upgrader
	newPeerCh chan *WSSPeerConn
	logger    *log.Logger
}

// NewWSSServer создаёт WSS-сервер. TLS-сертификаты берутся из конфига.
func NewWSSServer(cfg protocol.Config, logger *log.Logger) (*WSSServer, error) {
	if cfg.TLSCertFile == "" || cfg.TLSKeyFile == "" {
		return nil, fmt.Errorf("WSS: tls_cert and tls_key required")
	}

	ws := &WSSServer{
		newPeerCh: make(chan *WSSPeerConn, 100),
		logger:    logger,
		upgrader: websocket.Upgrader{
			ReadBufferSize:    65535,
			WriteBufferSize:   65535,
			EnableCompression: false,
			CheckOrigin:       func(r *http.Request) bool { return true },
		},
	}

	mux := http.NewServeMux()
	mux.HandleFunc(cfg.AdminPath+"/ws", ws.handleWS)

	ws.server = &http.Server{
		Addr:         ":443",
		Handler:      mux,
		ReadTimeout:  60 * time.Second,
		WriteTimeout: 60 * time.Second,
		IdleTimeout:  120 * time.Second,
		TLSConfig: &tls.Config{
			MinVersion: tls.VersionTLS12,
			CipherSuites: []uint16{
				tls.TLS_AES_128_GCM_SHA256,
				tls.TLS_AES_256_GCM_SHA384,
				tls.TLS_CHACHA20_POLY1305_SHA256,
				tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256,
				tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,
				tls.TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384,
				tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
				tls.TLS_ECDHE_ECDSA_WITH_CHACHA20_POLY1305,
				tls.TLS_ECDHE_RSA_WITH_CHACHA20_POLY1305,
			},
		},
	}

	return ws, nil
}

// Start запускает HTTPS-сервер с TLS. Блокирующий; запускать в горутине.
func (ws *WSSServer) Start() {
	ws.logger.Printf("[WSS] Запуск HTTPS на :443 с TLS")
	if err := ws.server.ListenAndServeTLS(serverCfg.TLSCertFile, serverCfg.TLSKeyFile); err != nil {
		if err != http.ErrServerClosed {
			ws.logger.Fatalf("[WSS] Ошибка запуска: %v", err)
		}
	}
}

// NewPlainWSServer создаёт WS-сервер без TLS (за reverse proxy, например Caddy).
func NewPlainWSServer(port int, cfg protocol.Config, logger *log.Logger) *WSSServer {
	ws := &WSSServer{
		newPeerCh: make(chan *WSSPeerConn, 100),
		logger:    logger,
		upgrader: websocket.Upgrader{
			ReadBufferSize:    65535,
			WriteBufferSize:   65535,
			EnableCompression: false,
			CheckOrigin:       func(r *http.Request) bool { return true },
		},
	}

	mux := http.NewServeMux()
	mux.HandleFunc(cfg.AdminPath+"/ws", ws.handleWS)

	ws.server = &http.Server{
		Addr:         fmt.Sprintf(":%d", port),
		Handler:      mux,
		ReadTimeout:  60 * time.Second,
		WriteTimeout: 60 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	return ws
}

// StartPlain запускает HTTP-сервер без TLS. Для работы за Caddy/nginx.
func (ws *WSSServer) StartPlain() {
	ws.logger.Printf("[WS] Запуск HTTP на %s (plain, за reverse proxy)", ws.server.Addr)
	if err := ws.server.ListenAndServe(); err != nil {
		if err != http.ErrServerClosed {
			ws.logger.Fatalf("[WS] Ошибка запуска: %v", err)
		}
	}
}

// AcceptCh возвращает канал новых WSSPeerConn.
// Серверный читатель в main читает из него.
func (ws *WSSServer) AcceptCh() <-chan *WSSPeerConn {
	return ws.newPeerCh
}

// handleWS — HTTP handler: WebSocket upgrade + передача в канал.
// Первый пакет читается reader-циклом в server.go, ShortID извлекается там же.
func (ws *WSSServer) handleWS(w http.ResponseWriter, r *http.Request) {
	conn, err := ws.upgrader.Upgrade(w, r, nil)
	if err != nil {
		ws.logger.Printf("[WSS] Upgrade error: %v", err)
		return
	}

	// Определяем тип соединения по порту
	connType := "WSS"
	if ws.server != nil && ws.server.Addr == ":19998" {
		connType = "WS"
	}
	pc := &WSSPeerConn{conn: conn}
	ws.logger.Printf("[%s] Новое WebSocket-соединение от %s (path=%s)", connType, r.RemoteAddr, r.URL.Path)
	ws.newPeerCh <- pc
}
