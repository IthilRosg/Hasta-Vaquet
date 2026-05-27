//go:build ignore

package main

import (
	_ "embed"
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
	"path/filepath"
	"strings"
	"syscall"
	"time"

	vpncore "hasta-vaquet/core"

	"golang.org/x/sys/windows"
	"golang.zx2c4.com/wintun"
)

//go:embed wintun.dll
var wintunDLL []byte


func getInterfaceIndex(name string) string {
	cmd := exec.Command("powershell", "-Command", fmt.Sprintf("Get-NetAdapter -Name '%s' | Select-Object -ExpandProperty InterfaceIndex", name))
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

type Config struct {
	ServerIP    string `json:"server_ip"`
	Port        int    `json:"port"`
	ShortID     uint16 `json:"short_id"`
	SecretKey   string `json:"secret_key"`
	RoutingSalt string `json:"routing_salt"`
	InternalIP  string `json:"internal_ip"`
	GatewayIP   string `json:"gateway_ip"`
	DNS         string `json:"dns"`
}

func loadConfig() Config {
	var serverIP, secretKey, routingSalt, configFile string
	var port int
	var shortID uint

	flag.StringVar(&serverIP, "server", "", "Адрес сервера")
	flag.IntVar(&port, "port", 0, "Порт")
	flag.UintVar(&shortID, "short-id", 0, "Short ID клиента")
	flag.StringVar(&secretKey, "key", "", "Секретный ключ")
	flag.StringVar(&routingSalt, "salt", "", "Routing salt")
	flag.StringVar(&configFile, "config", "config.json", "Путь к config.json")
	flag.Parse()

	cfg := Config{
		ServerIP:    serverIP,
		Port:        port,
		ShortID:     uint16(shortID),
		SecretKey:   secretKey,
		RoutingSalt: routingSalt,
	}

	if cfg.ServerIP == "" || cfg.Port == 0 || cfg.SecretKey == "" || cfg.RoutingSalt == "" || cfg.ShortID == 0 {
		if f, err := os.Open(configFile); err == nil {
			json.NewDecoder(f).Decode(&cfg)
			f.Close()
		}
	}

	if cfg.ServerIP == "" { cfg.ServerIP = "31.42.120.154" }
	if cfg.Port == 0 { cfg.Port = 9999 }
	if cfg.ShortID == 0 { log.Fatal("[ОШИБКА] ShortID не задан") }
	if cfg.SecretKey == "" { log.Fatal("[ОШИБКА] SecretKey не задан") }
	if cfg.RoutingSalt == "" { cfg.RoutingSalt = "HastaVaquetGlobal" }
	if cfg.GatewayIP == "" { cfg.GatewayIP = "192.168.100.1" }
	if cfg.DNS == "" { cfg.DNS = "1.1.1.1" }

	return cfg
}

func main() {
	cfg := loadConfig()
	derivedKey := vpncore.DeriveKey(cfg.SecretKey)

	lf, err := os.OpenFile("client.log", os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0666)
	if err != nil { log.Fatal(err) }
	defer lf.Close()
	log.SetOutput(io.MultiWriter(lf, os.Stdout))
	log.SetFlags(log.LstdFlags)

	log.Printf("[ЗАПУСК] Клиент Hasta-Vaquet Phase 6")
	log.Printf("[КОНФИГ] Сервер %s:%d, ShortID=%d, InternalIP=%s", cfg.ServerIP, cfg.Port, cfg.ShortID, cfg.InternalIP)

	// Развёртываем wintun.dll если нет рядом с exe
	exeDir := filepath.Dir(os.Args[0])
	dllPath := filepath.Join(exeDir, "wintun.dll")
	if _, err := os.Stat(dllPath); os.IsNotExist(err) {
		os.WriteFile(dllPath, wintunDLL, 0755)
	}

	adapter, err := wintun.CreateAdapter("HastaVaquet", "HastaVaquet", nil)
	if err != nil { log.Fatal(err) }
	defer adapter.Close()
	log.Printf("[АДАПТЕР] Wintun создан")

	index := getInterfaceIndex("HastaVaquet")
	if index == "" {
		log.Printf("[ПРЕДУПРЕЖДЕНИЕ] Не удалось получить InterfaceIndex для HastaVaquet")
	}
	log.Printf("[МАРШРУТ] InterfaceIndex = %s", index)
	run := func(cmd string, args ...string) {
		c := exec.Command(cmd, args...)
		c.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
		out, err := c.CombinedOutput()
		if err != nil {
			log.Printf("[ОШИБКА] %s %v: %s", cmd, args, strings.TrimSpace(string(out)))
		}
	}

	run("netsh", "interface", "ip", "set", "address", "name=HastaVaquet", "static", cfg.InternalIP, "255.255.255.0")
	run("netsh", "interface", "ipv4", "set", "subinterface", "name=HastaVaquet", "mtu=1300")
	run("netsh", "interface", "ip", "set", "dns", "name=HastaVaquet", "static", cfg.DNS)
	run("route", "delete", cfg.ServerIP)
	run("route", "add", cfg.ServerIP, "mask", "255.255.255.255", cfg.GatewayIP)
	run("route", "delete", "0.0.0.0", cfg.InternalIP)
	run("route", "add", "0.0.0.0", "mask", "0.0.0.0", cfg.InternalIP, "metric", "1", "if", index)
	run("netsh", "interface", "ipv6", "add", "route", "::/0", "name=HastaVaquet", cfg.InternalIP, "metric=1")
	log.Printf("[МАРШРУТ] Правила добавлены")

stopCh := make(chan struct{})
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt)
	go func() {
		<-sigCh
		log.Printf("[ОСТАНОВ] Завершение, чистка маршрутов...")
		close(stopCh)
		c := exec.Command("route", "delete", "0.0.0.0", cfg.InternalIP)
		c.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
		c.Run()
		log.Printf("[ОСТАНОВ] Маршруты очищены")
		os.Exit(0)
	}()

	conn, err := net.Dial("udp", net.JoinHostPort(cfg.ServerIP, fmt.Sprintf("%d", cfg.Port)))
	if err != nil {
		log.Fatalf("[ОШИБКА] Не удалось подключиться к серверу: %v", err)
	}
	defer conn.Close()
	log.Printf("[СОЕДИНЕНИЕ] Установлено с %s:%d", cfg.ServerIP, cfg.Port)
	session, err := adapter.StartSession(0x800000)
	if err != nil {
		log.Fatalf("[ОШИБКА] Не удалось запустить сессию Wintun: %v", err)
	}
	defer session.End()
	log.Printf("[СЕССИЯ] Wintun сессия запущена")

	go func() {
		for {
			select {
			case <-stopCh:
				return
			case <-time.After(time.Duration(5+mathrand.Intn(11)) * time.Second):
				keepAlive, err := vpncore.Encrypt([]byte{}, derivedKey[:], cfg.ShortID, cfg.RoutingSalt)
				if err != nil {
					log.Printf("[ОШИБКА KEEP-ALIVE] %v", err)
					continue
				}
				conn.Write(keepAlive)
				log.Printf("[KEEP-ALIVE] Отправлен")
			}
		}
	}()

	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("[RECOVER] readerLoop: %v", r)
			}
		}()
		buf := make([]byte, 65535)
		for {
			select {
			case <-stopCh:
				return
			default:
			}
			conn.SetReadDeadline(time.Now().Add(60 * time.Second))
			n, err := conn.Read(buf)
			if err != nil {
				if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
					continue
				}
				log.Printf("[ОШИБКА ЧТЕНИЯ] %v", err)
				continue
			}
			if n < 4+2+12 { continue }
			decrypted, err := vpncore.Decrypt(buf[:n], derivedKey[:])
			if err != nil {
				log.Printf("[ОШИБКА ДЕШИФРАЦИИ] %v", err)
				continue
			}
			if len(decrypted) == 0 {
				continue
			}
			if len(decrypted) == 1 && decrypted[0] == 0x01 {
				continue
			}
			packet, err := session.AllocateSendPacket(len(decrypted))
			if err != nil {
				log.Printf("[ОШИБКА ALLOC] %v", err)
				continue
			}
			copy(packet, decrypted)
			session.SendPacket(packet)
		}
	}()

	for {
		packet, err := session.ReceivePacket()
		if err == nil {
			if len(packet) >= 20 && (packet[0]>>4) == 4 {
				encrypted, err := vpncore.Encrypt(packet, derivedKey[:], cfg.ShortID, cfg.RoutingSalt)
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
