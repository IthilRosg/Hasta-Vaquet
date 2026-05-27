package main

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
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

	"github.com/songgao/water"
)

type Peer struct {
	ShortID   uint16
	Name      string
	KeyRaw    string // оригинальный ключ для генерации клиентских конфигов
	Key       [32]byte
	CP        *vpncore.CipherPack
	Internal  string
	UDPAddr   *net.UDPAddr
	udpMu     sync.Mutex
	ByteOut   atomic.Int64 // сбрасываемые каждые 30с (для лога)
	ByteIn    atomic.Int64 // сбрасываемые каждые 30с (для лога)
	CumTx     atomic.Int64 // кумулятивный TX — никогда не сбрасывается
	CumRx     atomic.Int64 // кумулятивный RX — никогда не сбрасывается
	LastSeen  atomic.Int64 // unix timestamp последнего пакета
}

// aggregateStats — счётчики ошибок для агрегированного логирования.
// Чтобы не спамить лог на каждый пакет, логируем раз в 60 сек.
var (
	dropNoPeer      atomic.Int64 // пакеты с неизвестным ShortID
	dropReplay      atomic.Int64 // bloom-фильтр отклонил (replay)
	dropHMAC        atomic.Int64 // HMAC mismatch
	dropDecrypt     atomic.Int64 // ошибка расшифровки
	lastDropLogAt   time.Time
	dropLogMu       sync.Mutex
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
	serverCfg       Config
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

type ConfigUser struct {
	ShortID   uint16 `json:"short_id"`
	Name      string `json:"name"`
	SecretKey string `json:"secret_key"`
	IP        string `json:"ip"`
}

type Config struct {
	Port        int          `json:"port"`
	AdminPort   int          `json:"admin_port"`
	AdminToken  string       `json:"admin_token"`
	AdminPath   string       `json:"admin_path"`
	ServerIP    string       `json:"server_ip"`
	GatewayIP   string       `json:"gateway_ip"`
	DNS         string       `json:"dns"`
	RoutingSalt string       `json:"routing_salt"`
	LogFile     string       `json:"log_file"`
	FEC         int          `json:"fec"` // server→client FEC (0/1=off, 2=2x)
	Users       []ConfigUser `json:"users"`
}

func loadConfig() Config {
	var port int
	var configFile string

	flag.IntVar(&port, "port", 0, "Порт")
	flag.StringVar(&configFile, "config", "server_config.json", "Путь к server_config.json")
	flag.Parse()

	cfg := Config{}
	if f, err := os.Open(configFile); err == nil {
		if decErr := json.NewDecoder(f).Decode(&cfg); decErr != nil {
			log.Fatalf("[ОШИБКА] Невалидный JSON в %s: %v", configFile, decErr)
		}
		f.Close()
	}

	if cfg.Port == 0 {
		cfg.Port = 9999
	}
	if cfg.AdminPort == 0 {
		cfg.AdminPort = 9998
	}
	if cfg.AdminPath == "" {
		cfg.AdminPath = "/hasta-vaquet"
	}
	if cfg.GatewayIP == "" {
		cfg.GatewayIP = "192.168.100.1"
	}
	if cfg.DNS == "" {
		cfg.DNS = "1.1.1.1"
	}
	if cfg.RoutingSalt == "" {
		cfg.RoutingSalt = "HastaVaquetGlobal"
	}
	if cfg.LogFile == "" {
		cfg.LogFile = "server.log"
	}
	if len(cfg.Users) == 0 {
		log.Fatal("[ОШИБКА] Нет пользователей в конфиге")
	}

	configFilePath = configFile
	return cfg
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
		cp, err := vpncore.NewCipherPack(key[:])
		if err != nil {
			logger.Fatalf("[ОШИБКА] cipher pack for user %s: %v", u.Name, err)
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

	conn, err := net.ListenUDP("udp", &net.UDPAddr{Port: cfg.Port})
	if err != nil {
		logger.Fatalf("[ОШИБКА] Не удалось открыть UDP порт %d: %v", cfg.Port, err)
	}
	logger.Printf("[СЕТЬ] Слушаем порт %d\n", cfg.Port)

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

	go func() {
		// Сброс bloom filter: каждые 60 сек ИЛИ при заполнении >70%.
		// 8192 слов * 64 бита = 524288 бит; при 3 хешах ёмкость ~121000 записей.
		// 70% от 121000 ≈ 85000 — безопасный порог до роста ложных срабатываний.
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
			peer.udpMu.Lock()
			addr := peer.UDPAddr
			peer.udpMu.Unlock()
			if addr == nil {
				continue
			}
			enc, err := peer.CP.Encrypt(packet[:n], peer.ShortID, routingSalt)
			if err != nil {
				logger.Printf("[ОШИБКА] encrypt: %v", err)
				continue
			}
			fec := serverCfg.FEC
			if fec < 1 { fec = 1 }
			if fec > 5 { fec = 5 }
			for i := 0; i < fec; i++ {
				if _, err := conn.WriteToUDP(enc, addr); err != nil {
					logger.Printf("[ОШИБКА] WriteToUDP: %v", err)
				}
			}
			peer.ByteOut.Add(int64(n) * int64(fec))
			peer.CumTx.Add(int64(n) * int64(fec))
		}
	}()

	logger.Printf("[ГОТОВ] Ожидание клиентов\n")
	buffer := make([]byte, 65535)
	for {
		n, addr, err := conn.ReadFromUDP(buffer)
		if err != nil || n < 4+2+12 {
			continue
		}

		dynamicID := binary.BigEndian.Uint16(buffer[4:6])
		nonce := buffer[6:18]
		routeMask := vpncore.Fnv1a16(append([]byte(routingSalt), nonce...))
		shortID := dynamicID ^ routeMask

		peersMu.RLock()
		peer := peers[shortID]
		peersMu.RUnlock()
		if peer == nil {
			dropNoPeer.Add(1)
			logDrops()
			continue
		}

		bkey := bloomKey(shortID, nonce)
		if !bloomCheck(bkey) {
			dropReplay.Add(1)
			logDrops()
			continue
		}

		decrypted, err := peer.CP.Decrypt(buffer[:n])
		if err != nil {
			if strings.Contains(err.Error(), "HMAC") {
				dropHMAC.Add(1)
			} else {
				dropDecrypt.Add(1)
			}
			logDrops()
			continue
		}
		bloomSet(bkey)
		bloomCount.Add(1)
		peer.LastSeen.Store(time.Now().Unix())
		if len(decrypted) == 0 {
			logger.Printf("[KEEP-ALIVE] ShortID=%d\n", shortID)
			peer.udpMu.Lock()
			peer.UDPAddr = addr
			peer.udpMu.Unlock()
			// Echo back for tunnel latency measurement
			enc, err := peer.CP.Encrypt([]byte{0x01}, peer.ShortID, routingSalt)
			if err != nil {
				logger.Printf("[ОШИБКА] echo encrypt: %v", err)
				continue
			}
			fec := serverCfg.FEC
			if fec < 1 { fec = 1 }
			if fec > 5 { fec = 5 }
			for i := 0; i < fec; i++ {
				conn.WriteToUDP(enc, addr)
			}
			continue
		}
		peer.udpMu.Lock()
		peer.UDPAddr = addr
		peer.udpMu.Unlock()
		if _, err := ifce.Write(decrypted); err != nil {
			logger.Printf("[ОШИБКА] TUN Write: %v", err)
			continue
		}
		peer.ByteIn.Add(int64(len(decrypted)))
		peer.CumRx.Add(int64(len(decrypted)))
	}
}
