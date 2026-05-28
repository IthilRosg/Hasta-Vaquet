package main

import (
	"crypto/sha256"
	"encoding/binary"
	"flag"
	"log"
	"net"
	"os"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	vpncore "hasta-vaquet/core"
	"hasta-vaquet/protocol"

	"github.com/songgao/water"
)

// ─── Transport-agnostic writer interface ─────────────────────────

type PacketWriter interface {
	WritePacket(data []byte, fec int) error
	TransportType() string // "udp" | "wss"
}

// ─── UDP writer (existing transport) ─────────────────────────────

type UDPWriter struct {
	addr *net.UDPAddr
	conn *net.UDPConn
	mu   sync.Mutex
}

func (w *UDPWriter) WritePacket(data []byte, fec int) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if fec < 1 {
		fec = 1
	}
	if fec > 5 {
		fec = 5
	}
	for i := 0; i < fec; i++ {
		if _, err := w.conn.WriteToUDP(data, w.addr); err != nil {
			return err
		}
	}
	return nil
}

func (w *UDPWriter) TransportType() string { return "udp" }

// ─── Incoming packet from any transport ──────────────────────────

type IncomingPacket struct {
	Data    []byte
	Writer  PacketWriter // write responses back through this
	ShortID uint16       // extracted DynamicID
	IsQUIC  bool         // true if packet is QUIC Short Header format
}

// ─── Peer ────────────────────────────────────────────────────────

type Peer struct {
	ShortID  uint16
	Name     string
	KeyRaw   string // оригинальный ключ для генерации клиентских конфигов
	Key      [32]byte
	CP       *vpncore.CipherPack
	cpMu     sync.Mutex // защита CP.Encrypt/Decrypt от data race (TUN reader + main loop)
	Internal string
	UDPAddr  *net.UDPAddr
	udpMu    sync.Mutex
	Writer   PacketWriter // transport-agnostic response writer
	ByteOut  atomic.Int64 // сбрасываемые каждые 30с (для лога)
	ByteIn   atomic.Int64 // сбрасываемые каждые 30с (для лога)
	CumTx    atomic.Int64 // кумулятивный TX — никогда не сбрасывается
	CumRx    atomic.Int64 // кумулятивный RX — никогда не сбрасывается
	LastSeen atomic.Int64 // unix timestamp последнего пакета
}

// aggregateStats — счётчики ошибок для агрегированного логирования.
// Чтобы не спамить лог на каждый пакет, логируем раз в 60 сек.
var (
	dropNoPeer    atomic.Int64 // пакеты с неизвестным ShortID
	dropReplay    atomic.Int64 // bloom-фильтр отклонил (replay)
	dropHMAC      atomic.Int64 // HMAC mismatch
	dropDecrypt   atomic.Int64 // ошибка расшифровки
	lastDropLogAt time.Time
	dropLogMu     sync.Mutex
)

var (
	routingSalt     string
	peers           map[uint16]*Peer
	ipToPeer        map[string]*Peer
	peersMu         sync.RWMutex
	logger          *log.Logger
	bloom           [8192]uint64
	bloomMu         sync.RWMutex
	bloomCount      atomic.Int64 // кол-во уникальных записей в bloom
	configFilePath  string
	serverStartTime time.Time
	serverCfg       protocol.Config
	configMu        sync.Mutex // защита saveConfig от concurrent writes
)

func bloomKey(shortID uint16, nonce []byte) []byte {
	b := make([]byte, 2+len(nonce))
	binary.BigEndian.PutUint16(b[:2], shortID)
	copy(b[2:], nonce)
	return b
}

func bloomCheck(key []byte) bool {
	bloomMu.RLock()
	defer bloomMu.RUnlock()
	totalBits := uint64(len(bloom) * 64)
	idx := [3]uint64{
		vpncore.Fnv1a64(key, 0x1234567890ABCDEF) % totalBits,
		vpncore.Fnv1a64(key, 0xFEDCBA0987654321) % totalBits,
		vpncore.Fnv1a64(key, 0xA1B2C3D4E5F60708) % totalBits,
	}
	w0, b0 := idx[0]/64, idx[0]%64
	w1, b1 := idx[1]/64, idx[1]%64
	w2, b2 := idx[2]/64, idx[2]%64
	return !((bloom[w0]&(1<<b0)) != 0 && (bloom[w1]&(1<<b1)) != 0 && (bloom[w2]&(1<<b2)) != 0)
}

func bloomSet(key []byte) {
	bloomMu.Lock()
	defer bloomMu.Unlock()
	totalBits := uint64(len(bloom) * 64)
	for _, seed := range []uint64{0x1234567890ABCDEF, 0xFEDCBA0987654321, 0xA1B2C3D4E5F60708} {
		h := vpncore.Fnv1a64(key, seed) % totalBits
		bloom[h/64] |= 1 << (h % 64)
	}
}

// logDrops — агрегированное логирование отброшенных пакетов (раз в 60 сек).
func logDrops() {
	dropLogMu.Lock()
	defer dropLogMu.Unlock()
	if time.Since(lastDropLogAt) < 60*time.Second {
		return
	}
	lastDropLogAt = time.Now()
	noPeer := dropNoPeer.Swap(0)
	replay := dropReplay.Swap(0)
	hmac := dropHMAC.Swap(0)
	decrypt := dropDecrypt.Swap(0)
	total := noPeer + replay + hmac + decrypt
	if total > 0 {
		logger.Printf("[DROP] Пакеты отброшены: всего=%d (shortID=%d replay=%d HMAC=%d decrypt=%d)\n",
			total, noPeer, replay, hmac, decrypt)
	}
}

func loadConfig() protocol.Config {
	var port int
	var configFile string

	flag.IntVar(&port, "port", 0, "Порт")
	flag.StringVar(&configFile, "config", "server_config.json", "Путь к server_config.json")
	flag.Parse()

	cfg, err := protocol.LoadConfig(configFile)
	if err != nil {
		log.Printf("[ОШИБКА] Не удалось загрузить %s: %v — использую defaults", configFile, err)
	}
	if port > 0 {
		cfg.Port = port
	}
	cfg.SetDefaults()
	if len(cfg.Users) == 0 {
		log.Fatal("[ОШИБКА] Нет пользователей в конфиге")
	}

	configFilePath = configFile
	return cfg
}

// processPacket — декрипт, replay-защита, keep-alive, TUN write.
// Вызывается из основного цикла для пакетов любого транспорта.
func processPacket(pkt IncomingPacket, ifce *water.Interface) {
	peersMu.RLock()
	peer := peers[pkt.ShortID]
	peersMu.RUnlock()
	if peer == nil {
		dropNoPeer.Add(1)
		logDrops()
		return
	}

	// Устанавливаем Writer для этого пира (транспорт, через который отвечать)
	// Делаем это до блокировки cpMu, чтобы TUN writer мог отправить ответ
	if pkt.Writer != nil {
		peer.udpMu.Lock()
		peer.Writer = pkt.Writer
		// Для UDP также сохраняем addr для совместимости со старым кодом
		if uw, ok := pkt.Writer.(*UDPWriter); ok {
			peer.UDPAddr = uw.addr
		}
		peer.udpMu.Unlock()
	}

	var bkey []byte
	shortID := pkt.ShortID
	packet := pkt.Data

	peer.cpMu.Lock()
	var decrypted []byte
	var err error

	if pkt.IsQUIC {
		// QUIC режим: временно переключаем CipherPack для декрипта
		oldMode := peer.CP.Mode
		peer.CP.Mode = protocol.CipherModeQUIC
		decrypted, err = peer.CP.Decrypt(packet)
		peer.CP.Mode = oldMode
	} else {
		// Standard режим: bloom-проверка + декрипт
		if len(packet) >= 4+2+12 && !serverCfg.NoEncrypt {
			nonce := packet[6:18]
			bkey = bloomKey(shortID, nonce)
			if !bloomCheck(bkey) {
				peer.cpMu.Unlock()
				dropReplay.Add(1)
				logDrops()
				return
			}
		}
		decrypted, err = peer.CP.Decrypt(packet)
	}

	if err != nil {
		peer.cpMu.Unlock()
		if strings.Contains(err.Error(), "HMAC") {
			dropHMAC.Add(1)
		} else {
			dropDecrypt.Add(1)
		}
		logDrops()
		return
	}

	// Bloom set (только standard-пакеты — QUIC имеет встроенную replay-защиту)
	if !pkt.IsQUIC && !serverCfg.NoEncrypt && bkey != nil {
		bloomSet(bkey)
		bloomCount.Add(1)
	}
	peer.LastSeen.Store(time.Now().Unix())

	// Keep-alive (пустой payload)
	if len(decrypted) == 0 {
		peer.cpMu.Unlock()
		logger.Printf("[KEEP-ALIVE] ShortID=%d\n", shortID)

		peer.cpMu.Lock()
		var enc []byte
		var encErr error
		if pkt.IsQUIC {
			oldMode := peer.CP.Mode
			peer.CP.Mode = protocol.CipherModeQUIC
			enc, encErr = peer.CP.Encrypt([]byte{0x01}, peer.ShortID, routingSalt)
			peer.CP.Mode = oldMode
		} else {
			enc, encErr = peer.CP.Encrypt([]byte{0x01}, peer.ShortID, routingSalt)
		}
		peer.cpMu.Unlock()

		if encErr != nil {
			logger.Printf("[ОШИБКА] echo encrypt: %v", encErr)
			return
		}
		if peer.Writer != nil {
			go func(w PacketWriter, data []byte) {
				if err := w.WritePacket(data, serverCfg.FEC); err != nil {
					logger.Printf("[ОШИБКА] echo write: %v", err)
				}
			}(peer.Writer, enc)
		}
		return
	}
	peer.cpMu.Unlock()

	// Пишем в TUN
	if _, err := ifce.Write(decrypted); err != nil {
		logger.Printf("[ОШИБКА] TUN Write: %v", err)
		return
	}
	peer.ByteIn.Add(int64(len(decrypted)))
	peer.CumRx.Add(int64(len(decrypted)))
}

func main() {
	cfg := loadConfig()
	serverCfg = cfg
	serverStartTime = time.Now()
	routingSalt = cfg.RoutingSalt

	f, err := os.OpenFile(cfg.LogFile, os.O_RDWR|os.O_CREATE|os.O_APPEND, 0666)
	if err != nil {
		log.Printf("[ОШИБКА] Не удалось открыть лог-файл %s: %v — используем stdout", cfg.LogFile, err)
		logger = log.New(os.Stdout, "", log.LstdFlags)
	} else {
		defer f.Close()
		logger = log.New(f, "", log.LstdFlags)
	}

	peers = make(map[uint16]*Peer)
	ipToPeer = make(map[string]*Peer)
	for _, u := range cfg.Users {
		key := sha256.Sum256([]byte(u.SecretKey))
		var cp *vpncore.CipherPack
		if serverCfg.NoEncrypt {
			cp = vpncore.NewNoopCipherPack()
		} else {
			var err error
			cp, err = vpncore.NewCipherPack(key[:])
			if err != nil {
				logger.Fatalf("[ОШИБКА] cipher pack for user %s: %v", u.Name, err)
			}
		}
		p := &Peer{
			ShortID:  u.ShortID,
			Name:     u.Name,
			KeyRaw:   u.SecretKey,
			Key:      key,
			CP:       cp,
			Internal: u.IP,
		}
		if p.ShortID == 0 {
			log.Fatalf("[ОШИБКА] User %s: short_id не может быть 0", u.IP)
		}
		if _, exists := peers[p.ShortID]; exists {
			log.Fatalf("[ОШИБКА] Дубликат short_id %d", p.ShortID)
		}
		peers[p.ShortID] = p
		ipToPeer[u.IP] = p
		logger.Printf("[ПИР] ShortID=%d IP=%s зарегистрирован", p.ShortID, p.Internal)
	}

	logger.Printf("[ЗАПУСК] Сервер v%s, порт %d, пиров: %d\n", vpncore.Version, cfg.Port, len(peers))

	if cfg.AdminToken != "" {
		go startWebPanel()
	} else {
		logger.Printf("[WEB] admin_token не задан — панель отключена\n")
	}

	ifce, err := water.New(water.Config{DeviceType: water.TUN})
	if err != nil {
		logger.Fatalf("[ОШИБКА] Не удалось создать TUN: %v", err)
	}
	exec.Command("ip", "addr", "add", "10.0.0.2/24", "dev", ifce.Name()).Run()
	exec.Command("ip", "link", "set", "dev", ifce.Name(), "up").Run()
	exec.Command("ip", "link", "set", "dev", ifce.Name(), "mtu", "1300").Run()
	exec.Command("ip", "link", "set", "dev", ifce.Name(), "txqueuelen", "10000").Run()

	// ─── Единый канал входящих пакетов от всех транспортов ─────
	incomingCh := make(chan IncomingPacket, 1000)

	// ─── WSS сервер (TLS) ──────────────────────────────────────
	if cfg.TLSCertFile != "" && cfg.TLSKeyFile != "" {
		wssServer, err := NewWSSServer(cfg, logger)
		if err != nil {
			logger.Fatalf("[ОШИБКА] WSS сервер: %v", err)
		}
		go wssServer.Start()
		logger.Printf("[WSS] WebSocket Secure сервер запущен на :443%s\n", cfg.AdminPath+"/ws")

		// Горутина: принимаем новые WSS-соединения → читаем пакеты → incomingCh
		go func() {
			for peerConn := range wssServer.AcceptCh() {
				go func(pc *WSSPeerConn) {
					defer func() {
						if r := recover(); r != nil {
							logger.Printf("[RECOVER] WSS reader: %v", r)
						}
					}()
					var firstPacket = true
					for {
						_, msg, err := pc.ReadMessage()
						if err != nil {
							logger.Printf("[WSS] ShortID=%d read error: %v", pc.ShortID, err)
							return
						}
						if len(msg) < 4+2+12 {
							continue
						}
						dynamicID := binary.BigEndian.Uint16(msg[4:6])
						nonce := msg[6:18]
						routeMask := vpncore.Fnv1a16(append([]byte(routingSalt), nonce...))
						shortID := dynamicID ^ routeMask

						if firstPacket {
							firstPacket = false
							pc.ShortID = shortID
							logger.Printf("[WSS] ShortID=%d соединён", shortID)
						}

						incomingCh <- IncomingPacket{
							Data:    msg,
							Writer:  pc,
							ShortID: shortID,
						}
					}
				}(peerConn)
			}
		}()
	} else {
		logger.Printf("[WSS] TLS не настроен — WSS сервер отключён\n")
	}

	// ─── Plain WS сервер (за Caddy/nginx) — всегда включён ─────
	wsPlain := NewPlainWSServer(19998, cfg, logger)
	go wsPlain.StartPlain()
	logger.Printf("[WS] Plain WebSocket сервер запущен на :19998%s\n", cfg.AdminPath+"/ws")

	// Горутина: читаем plain WS → incomingCh
	go func() {
		for peerConn := range wsPlain.AcceptCh() {
			go func(pc *WSSPeerConn) {
				defer func() {
					if r := recover(); r != nil {
						logger.Printf("[RECOVER] Plain WS reader: %v", r)
					}
				}()
				var firstPacket = true
				for {
					_, msg, err := pc.ReadMessage()
					if err != nil {
						logger.Printf("[WS] ShortID=%d read error: %v", pc.ShortID, err)
						return
					}
					if len(msg) < 4+2+12 {
						continue
					}
					dynamicID := binary.BigEndian.Uint16(msg[4:6])
					nonce := msg[6:18]
					routeMask := vpncore.Fnv1a16(append([]byte(routingSalt), nonce...))
					shortID := dynamicID ^ routeMask

					if firstPacket {
						firstPacket = false
						pc.ShortID = shortID
						logger.Printf("[WS] ShortID=%d соединён", shortID)
					}

					incomingCh <- IncomingPacket{
						Data:    msg,
						Writer:  pc,
						ShortID: shortID,
					}
				}
			}(peerConn)
		}
	}()

	// ─── UDP слушатель ─────────────────────────────────────────
	conn, err := net.ListenUDP("udp", &net.UDPAddr{Port: cfg.Port})
	if err != nil {
		logger.Fatalf("[ОШИБКА] Не удалось открыть UDP порт %d: %v", cfg.Port, err)
	}
	logger.Printf("[СЕТЬ] Слушаем UDP порт %d\n", cfg.Port)

	// Горутина: читаем UDP → incomingCh
	go func() {
		defer func() {
			if r := recover(); r != nil {
				logger.Printf("[RECOVER] UDP reader: %v", r)
			}
		}()
		buffer := make([]byte, 65535)
		for {
			n, addr, err := conn.ReadFromUDP(buffer)
			if err != nil {
				continue
			}

			// QUIC Short Header detection: Byte0 & 0xC0 == 0x40
			if n >= 7+4+2+12 && (buffer[0]&0xC0 == 0x40) && buffer[1] == 0 && buffer[2] == 0 {
				pkt := make([]byte, n)
				copy(pkt, buffer[:n])

				// Extract shortID from QUIC header (bytes 3-4, plain, not XOR'd)
				shortID := binary.BigEndian.Uint16(pkt[3:5])

				// Strip 7-byte QUIC header → inner standard format
				inner := pkt[7:]

				incomingCh <- IncomingPacket{
					Data:    inner,
					Writer:  &UDPWriter{addr: addr, conn: conn},
					ShortID: shortID,
				}
				continue
			}

			// Standard format
			if n < 4+2+12 {
				continue
			}
			pkt := make([]byte, n)
			copy(pkt, buffer[:n])

			dynamicID := binary.BigEndian.Uint16(pkt[4:6])
			nonce := pkt[6:18]
			routeMask := vpncore.Fnv1a16(append([]byte(routingSalt), nonce...))
			shortID := dynamicID ^ routeMask

			incomingCh <- IncomingPacket{
				Data:    pkt,
				Writer:  &UDPWriter{addr: addr, conn: conn},
				ShortID: shortID,
			}
		}
	}()

	// ─── Статистика (каждые 30с) ────────────────────────────────
	go func() {
		var prevOut, prevIn int64
		for {
			time.Sleep(30 * time.Second)
			var totalOut, totalIn int64
			peersMu.RLock()
			for _, p := range peers {
				totalOut += p.CumTx.Load()
				totalIn += p.CumRx.Load()
			}
			peersMu.RUnlock()
			deltaOut := totalOut - prevOut
			deltaIn := totalIn - prevIn
			if deltaOut > 0 || deltaIn > 0 {
				logger.Printf("[СТАТИСТИКА] Интервал: отпр %d байт | пол %d байт | Всего TX %d RX %d\n",
					deltaOut, deltaIn, totalOut, totalIn)
			}
			prevOut = totalOut
			prevIn = totalIn
		}
	}()

	// ─── Bloom filter reset ──────────────────────────────────────
	go func() {
		const bloomCapacity int64 = 85000
		ticker := time.NewTicker(60 * time.Second)
		checkTicker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		defer checkTicker.Stop()
		for {
			reset := false
			select {
			case <-ticker.C:
				reset = true
			case <-checkTicker.C:
				if bloomCount.Load() >= bloomCapacity {
					reset = true
				}
			}
			if reset {
				count := bloomCount.Load()
				bloomMu.Lock()
				for i := range bloom {
					bloom[i] = 0
				}
				bloomMu.Unlock()
				bloomCount.Store(0)
				logger.Printf("[BLOOM] Фильтр сброшен, было записей: %d\n", count)
			}
		}
	}()

	// ─── TUN reader → encrypt → transport write ─────────────────
	go func() {
		defer func() {
			if r := recover(); r != nil {
				logger.Printf("[RECOVER] TUN reader: %v", r)
			}
		}()
		packet := make([]byte, 65535)
		for {
			n, err := ifce.Read(packet)
			if err != nil {
				logger.Printf("[ОШИБКА TUN] Read: %v", err)
				continue
			}
			if n < 20 || (packet[0]>>4) != 4 {
				continue
			}
			dstIP := net.IP(packet[16:20]).String()
			if dstIP == "10.0.0.2" {
				continue
			}
			peersMu.RLock()
			peer := ipToPeer[dstIP]
			peersMu.RUnlock()
			if peer == nil {
				continue
			}

			// Получаем Writer (транспорт-нейтральный)
			peer.udpMu.Lock()
			writer := peer.Writer
			peer.udpMu.Unlock()
			if writer == nil {
				continue
			}

			peer.cpMu.Lock()
			enc, err := peer.CP.Encrypt(packet[:n], peer.ShortID, routingSalt)
			peer.cpMu.Unlock()
			if err != nil {
				logger.Printf("[ОШИБКА] encrypt: %v", err)
				continue
			}
			fec := serverCfg.FEC
			if fec < 1 {
				fec = 1
			}
			if fec > 5 {
				fec = 5
			}
			if err := writer.WritePacket(enc, fec); err != nil {
				logger.Printf("[ОШИБКА] WritePacket: %v", err)
			}
			peer.ByteOut.Add(int64(n) * int64(fec))
			peer.CumTx.Add(int64(n) * int64(fec))
		}
	}()

	// ─── Основной цикл обработки пакетов ────────────────────────
	logger.Printf("[ГОТОВ] Ожидание клиентов\n")
	for pkt := range incomingCh {
		processPacket(pkt, ifce)
	}
}
