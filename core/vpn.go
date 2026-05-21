package core

import (
	"fmt"
	mathrand "math/rand"
	"net"
	"sync"
	"sync/atomic"
	"time"
)

const echoWindowSize = 100

// echoSlot — один слот в кольцевом буфере для трекинга потерь.
type echoSlot struct {
	time  time.Time
	acked bool
}

// StatusListener — интерфейс для колбеков состояния VPN.
type StatusListener interface {
	OnStatus(status string, txSpeed, rxSpeed int64, totalTx, totalRx uint64, pingMs int, lossPct float64)
}

// VPN — клиентский VPN-движок.
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

	// echoRing — кольцевой буфер потерь (без синхронизации — всё в одной горутине keepAliveLoop)
	echoRing   [echoWindowSize]echoSlot
	echoPos    int // следующая свободная позиция в кольце
	echoMu     sync.Mutex

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
	v.echoReset()
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

func (v *VPN) callback(status string, txSpeed, rxSpeed int64, totalTx, totalRx uint64, pingMs int, lossPct float64) {
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
	v.mu.Lock()
	v.reconnecting.Swap(true)
	if !v.running.Load() {
		v.reconnecting.Store(false)
		v.mu.Unlock()
		return
	}
	v.running.Store(false)
	close(v.stopCh)
	v.mu.Unlock()

	v.platformCloseTunnel()
	v.platformActivateKillSwitch()

	// Kill Switch timeout: если reconnect не удался за 120с — отключаем блокировку сами
	// чтобы пользователь не остался без интернета при фатальном обрыве.
	go func() {
		time.Sleep(120 * time.Second)
		if v.reconnecting.Load() {
			v.platformDeactivateKillSwitch()
		}
	}()

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

		v.mu.Lock()
		v.stopCh = make(chan struct{})
		v.mu.Unlock()
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

// echoPush добавляет слот отправленного keep-alive в кольцевой буфер.
func (v *VPN) echoPush() {
	v.echoMu.Lock()
	v.echoRing[v.echoPos] = echoSlot{time: time.Now(), acked: false}
	v.echoPos = (v.echoPos + 1) % echoWindowSize
	v.echoMu.Unlock()
}

// echoAck помечает последний неотвеченный слот как полученный.
func (v *VPN) echoAck() {
	v.echoMu.Lock()
	// Ищем с конца — самый свежий ещё не acked слот
	for i := echoWindowSize - 1; i >= 0; i-- {
		idx := (v.echoPos - 1 - i + echoWindowSize) % echoWindowSize
		if !v.echoRing[idx].acked && !v.echoRing[idx].time.IsZero() {
			v.echoRing[idx].acked = true
			break
		}
	}
	v.echoMu.Unlock()
}

// echoCalcLoss вычисляет процент потерь в скользящем окне (последние 30 сек).
// Слоты старше 10 сек без ack считаются потерянными.
func (v *VPN) echoCalcLoss() float64 {
	v.echoMu.Lock()
	defer v.echoMu.Unlock()
	now := time.Now()
	windowDur := 30 * time.Second
	lostDur := 10 * time.Second
	var total, lost int
	for i := 0; i < echoWindowSize; i++ {
		sl := v.echoRing[i]
		if sl.time.IsZero() {
			continue
		}
		if now.Sub(sl.time) > windowDur {
			continue
		}
		total++
		if !sl.acked && now.Sub(sl.time) > lostDur {
			lost++
		}
	}
	if total == 0 {
		return 0
	}
	return float64(lost) * 100 / float64(total)
}

// echoReset очищает кольцевой буфер (при переподключении).
func (v *VPN) echoReset() {
	v.echoMu.Lock()
	v.echoRing = [echoWindowSize]echoSlot{}
	v.echoPos = 0
	v.echoMu.Unlock()
}

// ─── Общие циклы ─────────────────────────────────────────────────

func (v *VPN) keepAliveLoop() {
	for {
		packet, err := Encrypt([]byte{}, v.key[:], v.config.ShortID, v.config.RoutingSalt)
		if err == nil {
			v.conn.Write(packet)
			v.lastAliveMs.Store(time.Now().UnixMilli())
			v.echoPush()
		}
		select {
		case <-v.stopCh:
			return
		case <-time.After(time.Duration(5+mathrand.Intn(11)) * time.Second):
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
			pingMs := int(v.echoRtt.Load())
			lossPct := v.echoCalcLoss()
			v.callback("traffic", txSpeed, rxSpeed, v.sessionTotalTx.Load(), v.sessionTotalRx.Load(), pingMs, lossPct)
		}
	}
}
