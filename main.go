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
	"os/signal"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sys/windows"
	"golang.zx2c4.com/wintun"
)

var secretKey [32]byte

func encrypt(plaintext []byte) ([]byte, error) {
	nonce := make([]byte, 12)
	io.ReadFull(rand.Reader, nonce)

	padLen := mathrand.Intn(41)
	inner := make([]byte, 2+len(plaintext)+padLen)
	binary.BigEndian.PutUint16(inner[:2], uint16(len(plaintext)))
	copy(inner[2:], plaintext)
	if padLen > 0 {
		io.ReadFull(rand.Reader, inner[2+len(plaintext):])
	}

	mac := hmac.New(sha256.New, secretKey[:])
	mac.Write(nonce)
	marker := mac.Sum(nil)[:4]
	marker[0] |= 0x40

	block, err := aes.NewCipher(secretKey[:])
	if err != nil { return nil, err }
	gcm, err := cipher.NewGCM(block)
	if err != nil { return nil, err }
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

	mac := hmac.New(sha256.New, secretKey[:])
	mac.Write(nonce)
	if !hmac.Equal(marker, mac.Sum(nil)[:4]) {
		return nil, fmt.Errorf("HMAC mismatch")
	}

	block, err := aes.NewCipher(secretKey[:])
	if err != nil { return nil, err }
	gcm, err := cipher.NewGCM(block)
	if err != nil { return nil, err }
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

func getInterfaceIndex(name string) string {
	out, _ := exec.Command("powershell", "-Command", fmt.Sprintf("Get-NetAdapter -Name '%s' | Select-Object -ExpandProperty InterfaceIndex", name)).Output()
	return strings.TrimSpace(string(out))
}

type Config struct {
	ServerIP  string `json:"server_ip"`
	Port      int    `json:"port"`
	SecretKey string `json:"secret_key"`
}

func loadConfig() Config {
	var serverIP, secretKey, configFile string
	var port int

	flag.StringVar(&serverIP, "server", "", "Адрес сервера")
	flag.IntVar(&port, "port", 0, "Порт")
	flag.StringVar(&secretKey, "key", "", "Секретный ключ")
	flag.StringVar(&configFile, "config", "config.json", "Путь к config.json")
	flag.Parse()

	cfg := Config{
		ServerIP:  serverIP,
		Port:      port,
		SecretKey: secretKey,
	}

	if cfg.ServerIP == "" || cfg.Port == 0 || cfg.SecretKey == "" {
		if f, err := os.Open(configFile); err == nil {
			json.NewDecoder(f).Decode(&cfg)
			f.Close()
		}
	}

	if cfg.ServerIP == "" { cfg.ServerIP = "31.42.120.154" }
	if cfg.Port == 0 { cfg.Port = 9999 }
	if cfg.SecretKey == "" { cfg.SecretKey = "HastaVaquetSecret2026" }

	return cfg
}

func main() {
	cfg := loadConfig()
	secretKey = sha256.Sum256([]byte(cfg.SecretKey))

	lf, err := os.OpenFile("client.log", os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0666)
	if err != nil { log.Fatal(err) }
	defer lf.Close()
	log.SetOutput(io.MultiWriter(lf, os.Stdout))
	log.SetFlags(log.LstdFlags)

	log.Printf("[ЗАПУСК] Клиент Hasta-Vaquet Phase 5")
	log.Printf("[КОНФИГ] Сервер %s:%d, ключ %s", cfg.ServerIP, cfg.Port, cfg.SecretKey[:20]+"...")

	adapter, err := wintun.CreateAdapter("HastaVaquet", "HastaVaquet", nil)
	if err != nil { log.Fatal(err) }
	defer adapter.Close()
	log.Printf("[АДАПТЕР] Wintun создан")

	index := getInterfaceIndex("HastaVaquet")
	log.Printf("[МАРШРУТ] InterfaceIndex = %s", index)
	run := func(cmd string, args ...string) {
		out, err := exec.Command(cmd, args...).CombinedOutput()
		if err != nil {
			log.Printf("[ОШИБКА] %s %v: %s", cmd, args, strings.TrimSpace(string(out)))
		}
	}

	run("netsh", "interface", "ip", "set", "address", "name=HastaVaquet", "static", "10.0.0.1", "255.255.255.0")
	run("route", "delete", cfg.ServerIP)
	run("route", "add", cfg.ServerIP, "mask", "255.255.255.255", "192.168.100.1")
	run("route", "delete", "0.0.0.0", "10.0.0.1")
	run("route", "add", "0.0.0.0", "mask", "0.0.0.0", "10.0.0.1", "metric", "1", "if", index)
	log.Printf("[МАРШРУТ] Правила добавлены")

	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	go func() { <-c; log.Printf("[ОСТАНОВ] Завершение, чистка маршрутов..."); exec.Command("route", "delete", "0.0.0.0", "10.0.0.1").Run(); log.Printf("[ОСТАНОВ] Маршруты очищены"); os.Exit(0) }()

	conn, _ := net.Dial("udp", net.JoinHostPort(cfg.ServerIP, fmt.Sprintf("%d", cfg.Port)))
	defer conn.Close()
	log.Printf("[СОЕДИНЕНИЕ] Установлено с %s:%d", cfg.ServerIP, cfg.Port)
	session, _ := adapter.StartSession(0x800000)
	defer session.End()
	log.Printf("[СЕССИЯ] Wintun сессия запущена")

	go func() {
		for {
			time.Sleep(time.Duration(10+mathrand.Intn(21)) * time.Second)
			keepAlive, _ := encrypt([]byte{})
			conn.Write(keepAlive)
			log.Printf("[KEEP-ALIVE] Отправлен")
		}
	}()

	go func() {
		buf := make([]byte, 65535)
		for {
			n, err := conn.Read(buf)
			if err != nil {
				log.Printf("[ОШИБКА ЧТЕНИЯ] %v", err)
				continue
			}
			if n < 20 { continue }
			decrypted, err := decrypt(buf[:n])
			if err != nil {
				log.Printf("[ОШИБКА ДЕШИФРАЦИИ] %v", err)
				continue
			}
			if len(decrypted) == 0 { continue }
			packet, _ := session.AllocateSendPacket(len(decrypted))
			copy(packet, decrypted)
			session.SendPacket(packet)
		}
	}()

	for {
		packet, err := session.ReceivePacket()
		if err == nil {
			if len(packet) >= 20 && (packet[0]>>4) == 4 {
				encrypted, err := encrypt(packet)
				if err == nil {
					conn.Write(encrypted)
				} else {
					log.Printf("[ОШИБКА ШИФРАЦИИ] %v", err)
				}
			}
			session.ReleaseReceivePacket(packet)
		} else if err == windows.ERROR_NO_MORE_ITEMS {
			windows.WaitForSingleObject(session.ReadWaitEvent(), windows.INFINITE)
		} else {
			log.Printf("[ОШИБКА СЕССИИ] %v", err)
			break
		}
	}
}
