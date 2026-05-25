package core

import (
	"fmt"
	"log"
	"net"
	"sync"
	"sync/atomic"
	"time"
)

const echoWindowSize = 100

type echoSlot struct {
	time  time.Time
	acked bool
}

const (
	reconnectTimeout = 6 * time.Second // без ответа 6с → UI "reconnecting"
	pingInterval     = 3 * time.Second // интервал отправки ping
	routeCheckInterval = 3 * time.Second // интервал проверки шлюза
)

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
	stopping       atomic.Bool
	lastAliveMs    atomic.Int64  // unix ms последнего отправленного пакета
	lastPacketRx   atomic.Int64  // unix ms последнего полученного пакета
	echoRtt        atomic.Int64  // latest RTT in ms
	echoRing       [echoWindowSize]echoSlot
	echoPos        int
	echoMu         sync.Mutex
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
	v.lastAliveMs.Store(time.Now().UnixMilli())
	v.lastPacketRx.Store(time.Now().UnixMilli())

	v.callback("connecting", 0, 0, 0, 0, 0, 0)

	if err := v.platformOpenTunnel(); err != nil {
		return err
	}

	v.running.Store(true)
	v.echoReset()
	v.callback("connected", 0, 0, 0, 0, 0, 0)

	go v.persistentPingLoop()
	go v.routeMonitorLoop()
	go v.platformReaderLoop()
	go v.platformWriterLoop()
	go v.statsLoop()

	return nil
}

func (v *VPN) Stop() {
	v.stopping.Store(true)
	v.mu.Lock()
	defer v.mu.Unlock()
	if !v.running.Load() {
		v.stopping.Store(false)
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
	v.stopping.Store(false)
}

func (v *VPN) IsRunning() bool {
	return v.running.Load()
}

// Destroy — полное уничтожение Wintun-адаптера (вызывать только при выходе).
func (v *VPN) Destroy() {
	v.Stop()
	v.mu.Lock()
	v.platformDestroyTunnel()
	v.mu.Unlock()
}

func (v *VPN) callback(status string, txSpeed, rxSpeed int64, totalTx, totalRx uint64, pingMs int, lossPct float64) {
	if v.listener != nil {
		v.listener.OnStatus(status, txSpeed, rxSpeed, totalTx, totalRx, pingMs, lossPct)
	}
}

// ─── Платформозависимые хуки ─────────────────────────────────────

func (v *VPN) platformOpenTunnel() error            { return platformOpenTunnel(v) }
func (v *VPN) platformCloseTunnel()                 { platformCloseTunnel(v) }
func (v *VPN) platformDestroyTunnel()               { platformDestroyTunnel(v) }
func (v *VPN) platformReaderLoop()                  { platformReaderLoop(v) }
func (v *VPN) platformWriterLoop()                  { platformWriterLoop(v) }
func (v *VPN) platformActivateKillSwitch()          { platformActivateKillSwitch(v) }
func (v *VPN) platformDeactivateKillSwitch()        { platformDeactivateKillSwitch(v) }
func (v *VPN) platformRefreshServerRoute()          { platformRefreshServerRoute(v) }
func (v *VPN) platformReconnectSocket()             { platformReconnectSocket(v) }
func (v *VPN) platformReconnectSession()            { platformReconnectSession(v) }

// ─── Stateless Persistent Ping ───────────────────────────────────

// persistentPingLoop — «тупой» NAT-puncher: отправляет зашифрованный пустой
// пакет (тот же, что и первичный handshake). Первый пакет — немедленно,
// затем каждые 3s. Игнорирует ошибки. Если conn nil — ждёт следующего тика.
func (v *VPN) persistentPingLoop() {
	ticker := time.NewTicker(pingInterval)
	defer ticker.Stop()
	var wasDead bool
	// Отправляем первый handshake сразу, без ожидания ticker.C
	for first := true; ; first = false {
		if v.stopping.Load() {
			return
		}
		if v.conn != nil {
			// Стандартный keep-alive / handshake: Encrypt([]byte{})
			// Сервер на пустой пакет обновляет UDP-адрес пира и шлёт echo 0x01.
			pkt, err := Encrypt([]byte{}, v.key[:], v.config.ShortID, v.config.RoutingSalt)
			if err == nil {
				n, _ := v.conn.Write(pkt)
				if n > 0 {
					log.Printf("[VPN] persistentPingLoop: sent handshake ping (%d bytes)", n)
				}
				v.lastAliveMs.Store(time.Now().UnixMilli())
				v.echoPush()
			}
		}
		// Passive reconnect: если пакетов нет 6+ секунд — шлём UI и закрываем сокет,
		// чтобы routeMonitorLoop пересоздал его через reconnectSocket().
		isDead := time.Since(time.UnixMilli(v.lastPacketRx.Load())) > reconnectTimeout
		if isDead && !wasDead {
			wasDead = true
			v.callback("reconnecting", 0, 0, 0, 0, 0, 0)
			if v.conn != nil {
				v.conn.Close()
				v.conn = nil
			}
		} else if !isDead && wasDead {
			wasDead = false
			v.callback("connected", 0, 0, 0, 0, 0, 0)
		}
		if first {
			continue // первый пакет уже отправлен, без ожидания
		}
		select {
		case <-v.stopCh:
			return
		case <-ticker.C:
		}
	}
}

// ─── Route Monitor ──────────────────────────────────────────────

// routeMonitorLoop — проверяет шлюз ОС с адаптивным интервалом.
// При обнаружении потери связи сбрасывает Wintun-сессию и сокет.
func (v *VPN) routeMonitorLoop() {
	for {
		if v.stopping.Load() || !v.running.Load() {
			return
		}
		v.platformRefreshServerRoute()
		if v.conn == nil {
			v.platformReconnectSession() // чистим буфер TUN перед новым сокетом
			v.platformReconnectSocket()
		}
		interval := time.Second
		if v.conn != nil {
			interval = routeCheckInterval // 3s
		}
		select {
		case <-v.stopCh:
			return
		case <-time.After(interval):
		}
	}
}

// ─── ECHO Stats (кольцевой буфер для расчёта loss) ─────────────

func (v *VPN) echoPush() {
	v.echoMu.Lock()
	v.echoRing[v.echoPos] = echoSlot{time: time.Now(), acked: false}
	v.echoPos = (v.echoPos + 1) % echoWindowSize
	v.echoMu.Unlock()
}

func (v *VPN) echoAck() {
	v.echoMu.Lock()
	for i := echoWindowSize - 1; i >= 0; i-- {
		idx := (v.echoPos - 1 - i + echoWindowSize) % echoWindowSize
		if !v.echoRing[idx].acked && !v.echoRing[idx].time.IsZero() {
			v.echoRing[idx].acked = true
			break
		}
	}
	v.echoMu.Unlock()
}

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

func (v *VPN) echoReset() {
	v.echoMu.Lock()
	v.echoRing = [echoWindowSize]echoSlot{}
	v.echoPos = 0
	v.echoMu.Unlock()
}

// ─── Stats + Passive UI State ───────────────────────────────────

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
