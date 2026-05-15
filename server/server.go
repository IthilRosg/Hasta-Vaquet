package main

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	mathrand "math/rand"
	"net"
	"os"
	"os/exec"
	"sync"
	"sync/atomic"
	"time"

	"github.com/songgao/water"
)

type Peer struct {
	ShortID  uint16
	Name     string
	KeyRaw   string // оригинальный ключ для генерации клиентских конфигов
	Key      [32]byte
	Internal string
	UDPAddr  *net.UDPAddr
	udpMu    sync.Mutex
	ByteOut  atomic.Int64
	ByteIn   atomic.Int64
	LastSeen atomic.Int64 // unix timestamp последнего пакета
}

var (
	routingSalt     string
	peers           map[uint16]*Peer
	ipToPeer        map[string]*Peer
	peersMu         sync.RWMutex
	logger          *log.Logger
	bloom           [8192]uint64
	bloomCount      atomic.Int64 // кол-во уникальных записей в bloom
	configFilePath  string
	serverStartTime time.Time
	serverCfg       Config
	configMu        sync.Mutex // защита saveConfig от concurrent writes
)

func fnv1a(data []byte, seed uint64) uint64 {
	hash := seed ^ 14695981039346656037
	for _, b := range data {
		hash ^= uint64(b)
		hash *= 1099511628211
	}
	return hash
}

func fnv1a16(data []byte) uint16 {
	return uint16(fnv1a(data, 0) & 0xFFFF)
}

func bloomKey(shortID uint16, nonce []byte) []byte {
	b := make([]byte, 2+len(nonce))
	binary.BigEndian.PutUint16(b[:2], shortID)
	copy(b[2:], nonce)
	return b
}

func bloomCheck(key []byte) bool {
	totalBits := uint64(len(bloom) * 64)
	idx := [3]uint64{
		fnv1a(key, 0x1234567890ABCDEF) % totalBits,
		fnv1a(key, 0xFEDCBA0987654321) % totalBits,
		fnv1a(key, 0xA1B2C3D4E5F60708) % totalBits,
	}
	w0, b0 := idx[0]/64, idx[0]%64
	w1, b1 := idx[1]/64, idx[1]%64
	w2, b2 := idx[2]/64, idx[2]%64
	return !((bloom[w0]&(1<<b0)) != 0 && (bloom[w1]&(1<<b1)) != 0 && (bloom[w2]&(1<<b2)) != 0)
}

func bloomSet(key []byte) {
	totalBits := uint64(len(bloom) * 64)
	for _, seed := range []uint64{0x1234567890ABCDEF, 0xFEDCBA0987654321, 0xA1B2C3D4E5F60708} {
		h := fnv1a(key, seed) % totalBits
		bloom[h/64] |= 1 << (h % 64)
	}
}

func encrypt(plaintext, peerKey []byte, peerShortID uint16) ([]byte, error) {
	if peerKey == nil {
		return nil, errors.New("nil key")
	}
	nonce := make([]byte, 12)
	io.ReadFull(rand.Reader, nonce)

	routeMask := fnv1a16(append([]byte(routingSalt), nonce...))
	dynamicID := peerShortID ^ routeMask

	var padLen int
	switch nonce[0] % 2 {
	case 0:
		padLen = 50 + mathrand.Intn(101)
	case 1:
		maxInner := 1280
		current := 2 + len(plaintext)
		if current < maxInner {
			padLen = mathrand.Intn(maxInner - current + 1)
		}
	}

	inner := make([]byte, 2+len(plaintext)+padLen)
	binary.BigEndian.PutUint16(inner[:2], uint16(len(plaintext)))
	copy(inner[2:], plaintext)
	if padLen > 0 {
		io.ReadFull(rand.Reader, inner[2+len(plaintext):])
	}

	authData := make([]byte, 14)
	binary.BigEndian.PutUint16(authData[:2], dynamicID)
	copy(authData[2:], nonce)

	mac := hmac.New(sha256.New, peerKey)
	mac.Write(authData)
	marker := mac.Sum(nil)[:4]
	marker[0] |= 0x40

	block, err := aes.NewCipher(peerKey)
	if err != nil {
		return nil, fmt.Errorf("encrypt: NewCipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("encrypt: NewGCM: %w", err)
	}
	ciphertext := gcm.Seal(nil, nonce, inner, nil)

	buf := make([]byte, 4+2+12+len(ciphertext))
	copy(buf[:4], marker)
	binary.BigEndian.PutUint16(buf[4:6], dynamicID)
	copy(buf[6:18], nonce)
	copy(buf[18:], ciphertext)
	return buf, nil
}

func decrypt(packet, peerKey []byte) ([]byte, error) {
	if peerKey == nil {
		return nil, errors.New("nil key")
	}
	if len(packet) < 4+2+12 {
		return nil, fmt.Errorf("packet too short")
	}

	marker := make([]byte, 4)
	copy(marker, packet[:4])
	marker[0] &^= 0x40
	dynamicID := binary.BigEndian.Uint16(packet[4:6])
	nonce := packet[6:18]
	ciphertext := packet[18:]

	authData := make([]byte, 14)
	binary.BigEndian.PutUint16(authData[:2], dynamicID)
	copy(authData[2:], nonce)

	mac := hmac.New(sha256.New, peerKey)
	mac.Write(authData)
	expected := mac.Sum(nil)[:4]
	expected[0] &^= 0x40
	if !hmac.Equal(marker, expected) {
		return nil, fmt.Errorf("HMAC mismatch")
	}

	block, err := aes.NewCipher(peerKey)
	if err != nil {
		return nil, fmt.Errorf("decrypt: NewCipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("decrypt: NewGCM: %w", err)
	}
	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, err
	}

	if len(plaintext) < 2 {
		return nil, fmt.Errorf("payload too short")
	}
	realLen := binary.BigEndian.Uint16(plaintext[:2])
	if int(realLen)+2 > len(plaintext) {
		return nil, fmt.Errorf("invalid length")
	}
	return plaintext[2 : 2+realLen], nil
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
		json.NewDecoder(f).Decode(&cfg)
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

	f, _ := os.OpenFile(cfg.LogFile, os.O_RDWR|os.O_CREATE|os.O_APPEND, 0666)
	defer f.Close()
	logger = log.New(f, "", log.LstdFlags)

	peers = make(map[uint16]*Peer)
	ipToPeer = make(map[string]*Peer)
	for _, u := range cfg.Users {
		p := &Peer{
			ShortID:  u.ShortID,
			Name:     u.Name,
			KeyRaw:   u.SecretKey,
			Key:      sha256.Sum256([]byte(u.SecretKey)),
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

	logger.Printf("[ЗАПУСК] Сервер Phase 6, порт %d, пиров: %d\n", cfg.Port, len(peers))

	if cfg.AdminToken != "" {
		go startWebPanel()
	} else {
		logger.Printf("[WEB] admin_token не задан — панель отключена\n")
	}

	ifce, _ := water.New(water.Config{DeviceType: water.TUN})
	exec.Command("ip", "addr", "add", "10.0.0.2/24", "dev", ifce.Name()).Run()
	exec.Command("ip", "link", "set", "dev", ifce.Name(), "up").Run()

	conn, _ := net.ListenUDP("udp", &net.UDPAddr{Port: cfg.Port})
	logger.Printf("[СЕТЬ] Слушаем порт %d\n", cfg.Port)

	go func() {
		for {
			time.Sleep(30 * time.Second)
			var bo, bi int64
			peersMu.RLock()
			for _, p := range peers {
				bo += p.ByteOut.Swap(0)
				bi += p.ByteIn.Swap(0)
			}
			peersMu.RUnlock()
			if bo > 0 || bi > 0 {
				logger.Printf("[СТАТИСТИКА] Отправлено сервером: %d байт | Получено от клиентов: %d байт\n", bo, bi)
			}
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
				for i := range bloom {
					bloom[i] = 0
				}
				bloomCount.Store(0)
				logger.Printf("[BLOOM] Фильтр сброшен, было записей: %d\n", count)
			}
		}
	}()

	go func() {
		packet := make([]byte, 65535)
		for {
			n, _ := ifce.Read(packet)
			if n < 20 {
				continue
			}
			dstIP := net.IP(packet[16:20]).String()
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
			enc, _ := encrypt(packet[:n], peer.Key[:], peer.ShortID)
			conn.WriteToUDP(enc, addr)
			peer.ByteOut.Add(int64(n))
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
		routeMask := fnv1a16(append([]byte(routingSalt), nonce...))
		shortID := dynamicID ^ routeMask

		peersMu.RLock()
		peer := peers[shortID]
		peersMu.RUnlock()
		if peer == nil {
			continue
		}

		bkey := bloomKey(shortID, nonce)
		if !bloomCheck(bkey) {
			continue
		}

		decrypted, err := decrypt(buffer[:n], peer.Key[:])
		if err == nil {
			bloomSet(bkey)
			bloomCount.Add(1)
			peer.LastSeen.Store(time.Now().Unix())
			if len(decrypted) == 0 {
				logger.Printf("[KEEP-ALIVE] ShortID=%d\n", shortID)
				peer.udpMu.Lock()
				peer.UDPAddr = addr
				peer.udpMu.Unlock()
				// Echo back for tunnel latency measurement
				enc, _ := encrypt([]byte{0x01}, peer.Key[:], peer.ShortID)
				conn.WriteToUDP(enc, addr)
				continue
			}
			peer.udpMu.Lock()
			peer.UDPAddr = addr
			peer.udpMu.Unlock()
			ifce.Write(decrypted)
			peer.ByteIn.Add(int64(len(decrypted)))
		}
	}
}
