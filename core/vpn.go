package core

import (
	"fmt"
	mathrand "math/rand"
	"net"
	"sync"
	"sync/atomic"
	"time"
)

// StatusListener — интерфейс для колбеков состояния VPN.
// Заменяет StatusCallback func, т.к. gomobile не поддерживает
// передачу Go-функций как параметров (нужен interface).
type StatusListener interface {
	OnStatus(status string, txSpeed, rxSpeed int64, totalTx, totalRx uint64, pingMs int, lossPct int)
}

// VPN — клиентский VPN-движок.
// Платформозависимая часть туннеля реализуется в
// vpn_windows.go (Wintun) и vpn_android.go (VpnService).
type VPN struct {
	config         Config
	key            [32]byte
	conn           *net.UDPConn
	running        atomic.Bool
	stopCh         chan struct{}
	txBytes        atomic.Int64
	rxBytes        atomic.Int64
	sessionTotalTx atomic.Uint64
	sessionTotalRx atomic.Uint64
	listener       StatusListener
	mu             sync.Mutex
	lastAliveMs    atomic.Int64  // unix milli of last keep-alive sent
	echoReceived   atomic.Bool   // true if at least one echo came back
	echoRtt        atomic.Int64  // latest RTT in ms
	echoSent       atomic.Int64  // keep-alives sent
	echoAcked      atomic.Int64  // echos received
	reconnecting   atomic.Bool   // true during reconnect loop
	readFails      atomic.Int64  // consecutive UDP read failures
}

func New(cfg Config, listener StatusListener) *VPN {
	return &VPN{
		config:   cfg,
		key:      DeriveKey(cfg.SecretKey),
		listener: listener,
	}
}

func (v *VPN) Start() error {
	v.mu.Lock()
	defer v.mu.Unlock()

	if v.running.Load() {
		return fmt.Errorf("already running")
	}
	v.stopCh = make(chan struct{})

	v.callback("connecting", 0, 0, 0, 0, 0, 0)

	// Платформозависимое открытие туннеля (Winton / VpnService)
	if err := v.platformOpenTunnel(); err != nil {
		return err
	}

	v.running.Store(true)
	v.callback("connected", 0, 0, 0, 0, 0, 0)

	go v.keepAliveLoop()
	go v.platformReaderLoop()
	go v.platformWriterLoop()
	go v.statsLoop()

	return nil
}

func (v *VPN) Stop() {
	v.mu.Lock()
	defer v.mu.Unlock()

	v.reconnecting.Store(false)
	if !v.running.Load() {
		return
	}
	v.running.Store(false)
	close(v.stopCh)

	v.platformCloseTunnel()
	v.platformDeactivateKillSwitch()

	if v.conn != nil {
		v.conn.Close()
		v.conn = nil
	}

	v.callback("disconnected", 0, 0, 0, 0, 0, 0)
}

func (v *VPN) IsRunning() bool {
	return v.running.Load()
}

func (v *VPN) callback(status string, txSpeed, rxSpeed int64, totalTx, totalRx uint64, pingMs, lossPct int) {
	if v.listener != nil {
		v.listener.OnStatus(status, txSpeed, rxSpeed, totalTx, totalRx, pingMs, lossPct)
	}
}

// ─── Платформозависимые хуки ─────────────────────────────────────

func (v *VPN) platformOpenTunnel() error         { return platformOpenTunnel(v) }
func (v *VPN) platformCloseTunnel()              { platformCloseTunnel(v) }
func (v *VPN) platformReaderLoop()               { platformReaderLoop(v) }
func (v *VPN) platformWriterLoop()               { platformWriterLoop(v) }
func (v *VPN) platformActivateKillSwitch()       { platformActivateKillSwitch(v) }
func (v *VPN) platformDeactivateKillSwitch()     { platformDeactivateKillSwitch(v) }

// ─── Reconnect ────────────────────────────────────────────────────

func (v *VPN) onConnectionLost() {
	if !v.running.Load() || v.reconnecting.Swap(true) {
		return
	}
	v.running.Store(false)
	close(v.stopCh)
	v.platformCloseTunnel()
	v.platformActivateKillSwitch()

	if v.conn != nil {
		v.conn.Close()
		v.conn = nil
	}

	v.callback("disconnected", 0, 0, 0, 0, 0, 0)
	go v.reconnectLoop()
}

func (v *VPN) reconnectLoop() {
	backoff := 1 * time.Second
	maxBackoff := 60 * time.Second

	for {
		select {
		case <-time.After(backoff):
		}

		// Stop() was called during reconnect
		if !v.reconnecting.Load() {
			return
		}

		v.callback("reconnecting", 0, 0, 0, 0, 0, 0)

		v.stopCh = make(chan struct{})
		if err := v.platformOpenTunnel(); err != nil {
			backoff *= 2
			if backoff > maxBackoff {
				backoff = maxBackoff
			}
			continue
		}

		v.platformDeactivateKillSwitch()
		v.readFails.Store(0)
		v.running.Store(true)
		v.reconnecting.Store(false)

		v.callback("connected", 0, 0, 0, 0, 0, 0)

		go v.keepAliveLoop()
		go v.platformReaderLoop()
		go v.platformWriterLoop()
		go v.statsLoop()
		return
	}
}

// ─── Общие циклы ─────────────────────────────────────────────────

func (v *VPN) keepAliveLoop() {
	for {
		packet, err := Encrypt([]byte{}, v.key[:], v.config.ShortID, v.config.RoutingSalt)
		if err == nil {
			v.conn.Write(packet)
			v.lastAliveMs.Store(time.Now().UnixMilli())
			v.echoSent.Add(1)
		}
		select {
		case <-v.stopCh:
			return
		case <-time.After(time.Duration(10+mathrand.Intn(21)) * time.Second):
		}
	}
}

func (v *VPN) statsLoop() {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-v.stopCh:
			return
		case <-ticker.C:
			txSpeed := v.txBytes.Swap(0)
			rxSpeed := v.rxBytes.Swap(0)
			var pingMs, lossPct int
			if v.echoReceived.Load() {
				pingMs = int(v.echoRtt.Load())
				sent := v.echoSent.Load()
				acked := v.echoAcked.Load()
				if sent > 0 {
					lossPct = int((sent - acked) * 100 / sent)
				}
			}
			v.callback("traffic", txSpeed, rxSpeed, v.sessionTotalTx.Load(), v.sessionTotalRx.Load(), pingMs, lossPct)
		}
	}
}
