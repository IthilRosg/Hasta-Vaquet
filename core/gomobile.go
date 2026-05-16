//go:build android

// Package core — экспорт для gomobile bind.
//
// Сборка AAR (Android):
//
//	gomobile bind -target android -o hasta-vaquet.aar hasta-vaquet/core
//
// Затем импортировать .aar в Android-проект и вызывать:
//
//	HastaVaquet.start("config-json", fd);
//	HastaVaquet.stop();
//	String stats = HastaVaquet.getStats();
package core

import (
	"encoding/json"
	"os"
)

var globalVPN *VPN

// StartVPN запускает VPN-соединение.
// configJSON — JSON с настройками (server_ip, port, short_id, secret_key, ...).
// tunFd — файловый дескриптор TUN-интерфейса от VpnService.establish().
// Возвращает "ok" или сообщение об ошибке.
func StartVPN(configJSON string, tunFd int) string {
	if globalVPN != nil && globalVPN.IsRunning() {
		return "already running"
	}

	var cfg Config
	if err := json.Unmarshal([]byte(configJSON), &cfg); err != nil {
		return "bad config: " + err.Error()
	}

	// Устанавливаем дефолты (как в LoadConfig)
	if cfg.Port == 0 {
		cfg.Port = 9999
	}
	if cfg.RoutingSalt == "" {
		cfg.RoutingSalt = "HastaVaquetGlobal"
	}
	if cfg.InternalIP == "" {
		cfg.InternalIP = "10.0.0.10"
	}
	if cfg.GatewayIP == "" {
		cfg.GatewayIP = "192.168.100.1"
	}
	if cfg.DNS == "" {
		cfg.DNS = "1.1.1.1"
	}

	// Создаём os.File из fd (runtime poller корректно ждёт на неблокирующем fd)
	plat.tunFile = os.NewFile(uintptr(tunFd), "tun")

	vpn := New(cfg, &androidListener{})
	if err := vpn.Start(); err != nil {
		return err.Error()
	}
	globalVPN = vpn
	return "ok"
}

// StopVPN останавливает VPN-соединение.
func StopVPN() {
	if globalVPN != nil {
		globalVPN.Stop()
		globalVPN = nil
	}
}

// GetStats возвращает JSON со статистикой.
func GetStats() string {
	if globalVPN == nil || !globalVPN.IsRunning() {
		return `{"online":false}`
	}
	tx := globalVPN.txBytes.Load()
	rx := globalVPN.rxBytes.Load()
	ttx := globalVPN.sessionTotalTx.Load()
	trx := globalVPN.sessionTotalRx.Load()
	ping := globalVPN.echoRtt.Load()
	loss := int64(0)
	if sent := globalVPN.echoSent.Load(); sent > 0 {
		loss = (globalVPN.echoSent.Load() - globalVPN.echoAcked.Load()) * 100 / sent
	}
	data, _ := json.Marshal(map[string]interface{}{
		"online":   true,
		"tx_speed": tx,
		"rx_speed": rx,
		"total_tx": ttx,
		"total_rx": trx,
		"ping_ms":  ping,
		"loss_pct": loss,
	})
	return string(data)
}

// androidListener реализует StatusListener для Android.
// Заглушка — на Android события логики будут передаваться через callback в Kotlin.
type androidListener struct{}

func (l *androidListener) OnStatus(status string, txSpeed, rxSpeed int64, totalTx, totalRx uint64, pingMs, lossPct int) {
	// TODO: Phase 8 — передавать события в Kotlin через gomobile callback
	_ = status
	_ = txSpeed
	_ = rxSpeed
	_ = totalTx
	_ = totalRx
	_ = pingMs
	_ = lossPct
}
