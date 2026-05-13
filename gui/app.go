package main

import (
	"context"
	_ "embed"
	"os"
	"path/filepath"

	"hasta-vaquet/core"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

//go:embed wintun.dll
var wintunDLL []byte

type App struct {
	ctx context.Context
	vpn *core.VPN
}

func NewApp() *App {
	return &App{}
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx

	exeDir := filepath.Dir(os.Args[0])
	dllPath := filepath.Join(exeDir, "wintun.dll")
	if _, err := os.Stat(dllPath); os.IsNotExist(err) {
		os.WriteFile(dllPath, wintunDLL, 0755)
	}
}

type ConfigResult struct {
	Error       string `json:"error,omitempty"`
	ServerIP    string `json:"server_ip"`
	Port        int    `json:"port"`
	ShortID     uint16 `json:"short_id"`
	SecretKey   string `json:"secret_key"`
	InternalIP  string `json:"internal_ip"`
	RoutingSalt string `json:"routing_salt"`
	GatewayIP   string `json:"gateway_ip"`
	DNS         string `json:"dns"`
}

func (a *App) LoadDefaultConfig() *ConfigResult {
	exeDir := filepath.Dir(os.Args[0])
	paths := []string{
		filepath.Join(exeDir, "config.json"),
		"config.json",
	}
	for _, p := range paths {
		cfg, err := core.LoadConfig(p)
		if err != nil {
			continue
		}
		return &ConfigResult{
			ServerIP:    cfg.ServerIP,
			Port:        cfg.Port,
			ShortID:     cfg.ShortID,
			SecretKey:   cfg.SecretKey,
			InternalIP:  cfg.InternalIP,
			RoutingSalt: cfg.RoutingSalt,
			GatewayIP:   cfg.GatewayIP,
			DNS:         cfg.DNS,
		}
	}
	return nil
}

func (a *App) ImportConfig(path string) *ConfigResult {
	cfg, err := core.LoadConfig(path)
	if err != nil {
		return &ConfigResult{Error: err.Error()}
	}
	return &ConfigResult{
		ServerIP:    cfg.ServerIP,
		Port:        cfg.Port,
		ShortID:     cfg.ShortID,
		SecretKey:   cfg.SecretKey,
		InternalIP:  cfg.InternalIP,
		RoutingSalt: cfg.RoutingSalt,
		GatewayIP:   cfg.GatewayIP,
		DNS:         cfg.DNS,
	}
}

func (a *App) DoConnect(serverIP, secretKey, routingSalt, internalIP, gatewayIP, dns string, port int, shortID uint16) string {
	if a.vpn != nil && a.vpn.IsRunning() {
		return "already connected"
	}
	cfg := core.Config{
		ServerIP:    serverIP,
		Port:        port,
		ShortID:     shortID,
		SecretKey:   secretKey,
		RoutingSalt: routingSalt,
		InternalIP:  internalIP,
		GatewayIP:   gatewayIP,
		DNS:         dns,
	}
	vpn := core.New(cfg, func(status string, tx, rx int64) {
		if status == "connected" || status == "disconnected" {
			runtime.EventsEmit(a.ctx, "status", status)
		}
		if tx > 0 || rx > 0 {
			runtime.EventsEmit(a.ctx, "traffic", map[string]int64{"tx": tx, "rx": rx})
		}
	})
	if err := vpn.Start(); err != nil {
		return err.Error()
	}
	a.vpn = vpn
	return "connected"
}

func (a *App) DoDisconnect() string {
	if a.vpn == nil || !a.vpn.IsRunning() {
		return "not connected"
	}
	a.vpn.Stop()
	a.vpn = nil
	runtime.EventsEmit(a.ctx, "status", "disconnected")
	return "disconnected"
}

func (a *App) IsConnected() bool {
	return a.vpn != nil && a.vpn.IsRunning()
}
