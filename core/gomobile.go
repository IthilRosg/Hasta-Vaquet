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
	"sync"
)

// Protector — интерфейс, который будет реализован в Kotlin (VpnService).
type Protector interface {
	Protect(fd int) bool
}

var globalVPN *VPN
var globalProtector Protector

// StartVPN запускает VPN-соединение.
func StartVPN(configJSON string, tunFd int, p Protector) string {
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

	// Сохраняем протектор и дескриптор туннеля
	globalProtector = p
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
	loss := globalVPN.echoCalcLoss()
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
// Статус сохраняется для опроса из Kotlin через GetStatus().
type androidListener struct {
	mu       sync.RWMutex
	status   string
	txSpeed  int64
	rxSpeed  int64
	totalTx  uint64
	totalRx  uint64
	pingMs   int
	lossPct  float64
}

var androidState androidListener

func (l *androidListener) OnStatus(status string, txSpeed, rxSpeed int64, totalTx, totalRx uint64, pingMs int, lossPct float64) {
	l.mu.Lock()
	l.status = status
	l.txSpeed = txSpeed
	l.rxSpeed = rxSpeed
	l.totalTx = totalTx
	l.totalRx = totalRx
	l.pingMs = pingMs
	l.lossPct = lossPct
	l.mu.Unlock()
}

// GetStatus возвращает JSON с текущим статусом VPN.
// Статусы: "connecting", "connected", "reconnecting", "traffic", "disconnected"
func GetStatus() string {
	androidState.mu.RLock()
	defer androidState.mu.RUnlock()
	data, _ := json.Marshal(map[string]interface{}{
		"status":   androidState.status,
		"tx_speed": androidState.txSpeed,
		"rx_speed": androidState.rxSpeed,
		"total_tx": androidState.totalTx,
		"total_rx": androidState.totalRx,
		"ping_ms":  androidState.pingMs,
		"loss_pct": androidState.lossPct,
	})
	return string(data)
}
