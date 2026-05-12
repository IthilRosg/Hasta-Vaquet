package main

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
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

var (
	key        [32]byte
	clientAddr *net.UDPAddr
	addrMutex  sync.RWMutex
	logger     *log.Logger
	byteOut    atomic.Int64
	byteIn     atomic.Int64
	bloom      [8192]uint64
)

func fnv1a(data []byte, seed uint64) uint64 {
	hash := seed ^ 14695981039346656037
	for _, b := range data {
		hash ^= uint64(b)
		hash *= 1099511628211
	}
	return hash
}

func bloomCheck(nonce []byte) bool {
	totalBits := uint64(len(bloom) * 64)
	idx := [3]uint64{
		fnv1a(nonce, 0x1234567890ABCDEF) % totalBits,
		fnv1a(nonce, 0xFEDCBA0987654321) % totalBits,
		fnv1a(nonce, 0xA1B2C3D4E5F60708) % totalBits,
	}
	w0, b0 := idx[0]/64, idx[0]%64
	w1, b1 := idx[1]/64, idx[1]%64
	w2, b2 := idx[2]/64, idx[2]%64
	m0, m1, m2 := uint64(1)<<b0, uint64(1)<<b1, uint64(1)<<b2
	return !((bloom[w0]&m0) != 0 && (bloom[w1]&m1) != 0 && (bloom[w2]&m2) != 0)
}

func bloomSet(nonce []byte) {
	totalBits := uint64(len(bloom) * 64)
	idx := [3]uint64{
		fnv1a(nonce, 0x1234567890ABCDEF) % totalBits,
		fnv1a(nonce, 0xFEDCBA0987654321) % totalBits,
		fnv1a(nonce, 0xA1B2C3D4E5F60708) % totalBits,
	}
	bloom[idx[0]/64] |= 1 << (idx[0] % 64)
	bloom[idx[1]/64] |= 1 << (idx[1] % 64)
	bloom[idx[2]/64] |= 1 << (idx[2] % 64)
}

func encrypt(plaintext []byte) ([]byte, error) {
	nonce := make([]byte, 12)
	io.ReadFull(rand.Reader, nonce)

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

	mac := hmac.New(sha256.New, key[:])
	mac.Write(nonce)
	marker := mac.Sum(nil)[:4]
	marker[0] |= 0x40

	block, _ := aes.NewCipher(key[:])
	gcm, _ := cipher.NewGCM(block)
	ciphertext := gcm.Seal(nil, nonce, inner, nil)

	buf := make([]byte, 4+12+len(ciphertext))
	copy(buf[:4], marker)
	copy(buf[4:16], nonce)
	copy(buf[16:], ciphertext)
	return buf, nil
}

func decrypt(packet []byte) ([]byte, error) {
	if len(packet) < 4+12 {
		return nil, fmt.Errorf("packet too short")
	}

	marker := make([]byte, 4)
	copy(marker, packet[:4])
	marker[0] &= 0xBF
	nonce := packet[4:16]
	ciphertext := packet[16:]

	mac := hmac.New(sha256.New, key[:])
	mac.Write(nonce)
	if !hmac.Equal(marker, mac.Sum(nil)[:4]) {
		return nil, fmt.Errorf("HMAC mismatch")
	}

	block, _ := aes.NewCipher(key[:])
	gcm, _ := cipher.NewGCM(block)
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

type Config struct {
	Port      int    `json:"port"`
	SecretKey string `json:"secret_key"`
}

func loadConfig() Config {
	var secretKey, configFile string
	var port int

	flag.IntVar(&port, "port", 0, "Порт")
	flag.StringVar(&secretKey, "key", "", "Секретный ключ")
	flag.StringVar(&configFile, "config", "config.json", "Путь к config.json")
	flag.Parse()

	cfg := Config{
		Port:      port,
		SecretKey: secretKey,
	}

	if cfg.Port == 0 || cfg.SecretKey == "" {
		if f, err := os.Open(configFile); err == nil {
			json.NewDecoder(f).Decode(&cfg)
			f.Close()
		}
	}

	if cfg.Port == 0 { cfg.Port = 9999 }
	if cfg.SecretKey == "" { cfg.SecretKey = "HastaVaquetSecret2026" }

	return cfg
}

func main() {
	cfg := loadConfig()
	key = sha256.Sum256([]byte(cfg.SecretKey))

	f, _ := os.OpenFile("/root/hasvaq/server.log", os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0666)
	defer f.Close()
	logger = log.New(f, "", log.LstdFlags)

	ifce, _ := water.New(water.Config{DeviceType: water.TUN})
	exec.Command("ip", "addr", "add", "10.0.0.2/24", "dev", ifce.Name()).Run()
	exec.Command("ip", "link", "set", "dev", ifce.Name(), "up").Run()

	conn, _ := net.ListenUDP("udp", &net.UDPAddr{Port: cfg.Port})
	logger.Printf("[ЗАПУСК] Сервер слушает порт %d\n", cfg.Port)

	go func() {
		for {
			time.Sleep(30 * time.Second)
			bo := byteOut.Swap(0)
			bi := byteIn.Swap(0)
			if bo > 0 || bi > 0 {
				logger.Printf("[СТАТИСТИКА] Отправлено сервером: %d байт | Получено от клиента: %d байт\n", bo, bi)
			}
		}
	}()

	go func() {
		for {
			time.Sleep(60 * time.Second)
			for i := range bloom {
				bloom[i] = 0
			}
		}
	}()

	go func() {
		packet := make([]byte, 65535)
		for {
			n, _ := ifce.Read(packet)
			addrMutex.RLock()
			addr := clientAddr
			addrMutex.RUnlock()
			if addr != nil {
				enc, _ := encrypt(packet[:n])
				conn.WriteToUDP(enc, addr)
				byteOut.Add(int64(n))
			}
		}
	}()

	buffer := make([]byte, 65535)
	for {
		n, addr, err := conn.ReadFromUDP(buffer)
		if err != nil || n < 20 { continue }

		addrMutex.Lock()
		clientAddr = addr
		addrMutex.Unlock()

		if !bloomCheck(buffer[4:16]) { continue }

		decrypted, err := decrypt(buffer[:n])
		if err == nil {
			bloomSet(buffer[4:16])
			if len(decrypted) == 0 {
				logger.Printf("[KEEP-ALIVE] получено\n")
				continue
			}
			ifce.Write(decrypted)
			byteIn.Add(int64(len(decrypted)))
		}
	}
}
