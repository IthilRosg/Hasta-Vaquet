package main

import (
	"context"

	"hasta-vaquet/core"
)

type App struct {
	ctx context.Context
	vpn *core.VPN
}

func NewApp() *App {
	return &App{}
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
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
		SecretKey:   cfg.SecretKey[:8] + "********",
		InternalIP:  cfg.InternalIP,
		RoutingSalt: cfg.RoutingSalt,
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
}

type StatusUpdate struct {
	Status  string `json:"status"`
	TXBytes int64  `json:"tx_bytes"`
	RXBytes int64  `json:"rx_bytes"`
}

func (a *App) Connect(cfgJSON string) error {
	return nil
}

func (a *App) DoConnect(serverIP, secretKey, routingSalt, internalIP, gatewayIP string, port int, shortID uint16) string {
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
	}

	vpn := core.New(cfg, func(status string, tx, rx int64) {})
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
	return "disconnected"
}

func (a *App) IsConnected() bool {
	return a.vpn != nil && a.vpn.IsRunning()
}
