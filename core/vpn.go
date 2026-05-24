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

	reconnecting      atomic.Bool   // true during reconnect loop
	stopping          atomic.Bool   // true when user initiated Stop()
	readFails         atomic.Int64  // consecutive UDP read failures
	consecutiveMisses atomic.Int32  // keep-alive misses (reset on any echo)

	confirming        int32         // 1 = burst confirmation in progress (CAS gate)
	confirmOk         atomic.Int32  // echo acks received during burst
	graceUntil        atomic.Int64  // unix nano: skip miss counting before this
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
	// Флаг планового останова — самый первый сигнал всем горутинам
	v.stopping.Store(true)

	v.mu.Lock()
	defer v.mu.Unlock()

	v.reconnecting.Store(false)
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

func (v *VPN) callback(status string, txSpeed, rxSpeed int64, totalTx, totalRx uint64, pingMs int, lossPct float64) {
	if v.listener != nil {
		v.listener.OnStatus(status, txSpeed, rxSpeed, totalTx, totalRx, pingMs, lossPct)
	}
}

// ─── Платформозависимые хуки ─────────────────────────────────────

func (v *VPN) platformOpenTunnel() error            { return platformOpenTunnel(v) }
func (v *VPN) platformCloseTunnel()                 { platformCloseTunnel(v) }
func (v *VPN) platformReaderLoop()                  { platformReaderLoop(v) }
func (v *VPN) platformWriterLoop()                  { platformWriterLoop(v) }
func (v *VPN) platformActivateKillSwitch()          { platformActivateKillSwitch(v) }
func (v *VPN) platformDeactivateKillSwitch()        { platformDeactivateKillSwitch(v) }
func (v *VPN) platformRefreshServerRoute()          { platformRefreshServerRoute(v) }
func (v *VPN) platformGatewayIsValid() bool         { return platformGatewayIsValid(v) }
func (v *VPN) platformReconnectSocket()             { platformReconnectSocket(v) }

// ─── Seamless Reconnect ──────────────────────────────────────────

func (v *VPN) enterReconnecting() {
	if v.reconnecting.Load() {
		return
	}
	v.reconnecting.Store(true)

	// Безусловно обновляем route и пересоздаём UDP-сокет —
	// старый мог быть привязан к упавшему сетевому интерфейсу
	v.platformRefreshServerRoute()
	v.platformReconnectSocket()

	v.callback("reconnecting", 0, 0, 0, 0, 0, 0)
	log.Printf("[VPN] enterReconnecting: network lost, TUN kept alive, socket recreated")
}

// tryConfirmReconnect запускает burst-подтверждение вместо мгновенного выхода.
// CAS-шлюз гарантирует строго одну горутину подтверждения в момент времени.
func (v *VPN) tryConfirmReconnect() {
	if v.stopping.Load() || !v.reconnecting.Load() {
		return
	}

	// Принудительно обновляем маршрут до сервера — шлюз мог появиться
	v.platformRefreshServerRoute()

	// Не запускаем burst, пока шлюз невалидный (0.0.0.0 или пустой)
	if !v.platformGatewayIsValid() {
		log.Printf("[VPN] tryConfirmReconnect: gateway invalid — burst blocked")
		return
	}

	if !atomic.CompareAndSwapInt32(&v.confirming, 0, 1) {
		return
	}
	log.Printf("[VPN] tryConfirmReconnect: gateway OK, sending burst probes")
	go v.confirmBurst()
}

// confirmBurst отправляет 3 пинга с интервалом 400ms и проверяет min 2 ответа.
func (v *VPN) confirmBurst() {
	defer atomic.StoreInt32(&v.confirming, 0)
	v.confirmOk.Store(0)

	for i := 0; i < 3; i++ {
		packet, err := Encrypt([]byte{}, v.key[:], v.config.ShortID, v.config.RoutingSalt)
		if err == nil && v.conn != nil {
			v.conn.Write(packet)
			v.lastAliveMs.Store(time.Now().UnixMilli())
			v.echoPush()
		}
		select {
		case <-v.stopCh:
			return
		case <-time.After(400 * time.Millisecond):
		}
	}

	// Ждём последний echo-ответ
	select {
	case <-v.stopCh:
		return
	case <-time.After(400 * time.Millisecond):
	}

	acks := v.confirmOk.Load()
	if acks >= 2 {
		log.Printf("[VPN] confirmBurst: %d/3 acks — connection confirmed", acks)
		v.exitReconnecting()
		atomic.StoreInt32(&v.confirming, 0)
	} else {
		log.Printf("[VPN] confirmBurst: only %d/3 acks — cooldown 1s before next", acks)
		// Cooldown — не пускаем следующий burst сразу
		select {
		case <-v.stopCh:
			return
		case <-time.After(1 * time.Second):
		}
		atomic.StoreInt32(&v.confirming, 0)
	}
}

func (v *VPN) exitReconnecting() {
	if !v.reconnecting.Load() {
		return
	}
	v.reconnecting.Store(false)
	v.readFails.Store(0)
	v.consecutiveMisses.Store(0)
	v.graceUntil.Store(time.Now().Add(4 * time.Second).UnixNano())
	log.Printf("[VPN] exitReconnecting: network restored (grace 4s)")
	v.callback("connected", 0, 0, 0, 0, 0, 0)
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
		if v.stopping.Load() {
			return
		}

		// Каждую итерацию реконнекта пробуем обновить route до сервера
		// и пересоздать сокет — шлюз мог появиться/измениться
		if v.reconnecting.Load() {
			v.platformRefreshServerRoute()
			if v.conn == nil {
				v.platformReconnectSocket()
			}
			// NAT-punch: как только сокет появился — сразу шлём пинг,
			// чтобы сервер и NAT-роутер увидели новое подключение
			if v.conn != nil {
				pkt, _ := Encrypt([]byte{}, v.key[:], v.config.ShortID, v.config.RoutingSalt)
				if pkt != nil {
					v.conn.Write(pkt)
					v.lastAliveMs.Store(time.Now().UnixMilli())
					v.echoPush()
				}
			}
		}

		packet, err := Encrypt([]byte{}, v.key[:], v.config.ShortID, v.config.RoutingSalt)
		if err == nil && v.conn != nil {
			v.conn.Write(packet)
			v.lastAliveMs.Store(time.Now().UnixMilli())
			v.echoPush()
		}

		// 2s в норме, 1s при reconnect (агрессивный опрос)
		interval := 2 * time.Second
		if v.reconnecting.Load() {
			interval = 1 * time.Second
		}
		select {
		case <-v.stopCh:
			return
		case <-time.After(interval):
		}

		if v.stopping.Load() {
			return
		}

		// Проверяем, пришёл ли echo-ответ с прошлого раза
		if v.echoReceived.Swap(false) {
			v.consecutiveMisses.Store(0)
			// Выход из reconnect теперь только через confirmBurst
		} else {
			graceEnd := v.graceUntil.Load()
			if graceEnd > 0 && graceEnd > time.Now().UnixNano() {
				// Grace-период — не считаем пропуски
				v.consecutiveMisses.Store(0)
			} else {
				v.consecutiveMisses.Add(1)
				if v.consecutiveMisses.Load() >= 3 && !v.reconnecting.Load() {
					v.enterReconnecting()
				}
			}
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
