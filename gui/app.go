package main

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"

	"hasta-vaquet/core"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

//go:embed wintun.dll
var wintunDLL []byte

type App struct {
	ctx      context.Context
	vpn      *core.VPN
	profiles []string
	logger   *log.Logger
}

// vpnListener реализует core.StatusListener для отправки событий в UI.
type vpnListener struct {
	ctx context.Context
}

func (l *vpnListener) OnStatus(status string, txSpeed, rxSpeed int64, totalTx, totalRx uint64, pingMs int, lossPct float64) {
	runtime.EventsEmit(l.ctx, "status", map[string]interface{}{
		"status":      status,
		"attempt":     txSpeed,
		"tx_speed":    txSpeed,
		"rx_speed":    rxSpeed,
		"total_tx":    totalTx,
		"total_rx":    totalRx,
		"ping_ms":     pingMs,
		"loss_pct":    lossPct,
	})
	// Специализированные события для фронтенда
	switch status {
	case "reconnecting", "connected", "disconnected":
		runtime.EventsEmit(l.ctx, "connection_status", status)
	}
	if status == "traffic" {
		runtime.EventsEmit(l.ctx, "traffic", map[string]interface{}{
			"tx_speed": txSpeed, "rx_speed": rxSpeed,
			"total_tx": totalTx, "total_rx": totalRx,
		})
		runtime.EventsEmit(l.ctx, "ping", map[string]interface{}{"rtt": pingMs, "loss": lossPct})
	}
}

func NewApp() *App {
	return &App{}
}

func (a *App) shutdown(ctx context.Context) {
	if a.logger != nil {
		a.logger.Println("GUI shutting down — destroying VPN adapter")
	}
	if a.vpn != nil {
		a.vpn.Destroy()
	}
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	exeDir := filepath.Dir(os.Args[0])
	dllPath := filepath.Join(exeDir, "wintun.dll")
	if _, err := os.Stat(dllPath); os.IsNotExist(err) {
		os.WriteFile(dllPath, wintunDLL, 0755)
	}
	logPath := filepath.Join(exeDir, "gui.log")
	f, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err == nil {
		a.logger = log.New(f, "", log.LstdFlags)
		a.logger.Println("GUI started")
	}
}

type ConfigResult struct {
	ProfileName string `json:"profile_name"`
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
		return toResult(cfg)
	}
	return nil
}

func (a *App) ListProfiles() []string {
	exeDir := filepath.Dir(os.Args[0])
	profilesDir := filepath.Join(exeDir, "profiles")
	entries, err := os.ReadDir(profilesDir)
	if err != nil {
		return nil
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() && filepath.Ext(e.Name()) == ".json" {
			names = append(names, e.Name()[:len(e.Name())-5])
		}
	}
	sort.Strings(names)
	return names
}

func (a *App) LoadProfile(name string) *ConfigResult {
	exeDir := filepath.Dir(os.Args[0])
	p := filepath.Join(exeDir, "profiles", name+".json")
	cfg, err := core.LoadConfig(p)
	if err != nil {
		p = name + ".json"
		cfg, err = core.LoadConfig(p)
		if err != nil {
			return nil
		}
	}
	return toResult(cfg)
}

func (a *App) ImportConfig(path string) *ConfigResult {
	return importToProfiles(path)
}

// ImportConfigFromDialog открывает проводник Windows для выбора .json файла
// с конфигом и копирует его в папку profiles/.
func (a *App) ImportConfigFromDialog() *ConfigResult {
	path, err := runtime.OpenFileDialog(a.ctx, runtime.OpenDialogOptions{
		Title: "Выберите config.json",
		Filters: []runtime.FileFilter{
			{DisplayName: "JSON Files (*.json)", Pattern: "*.json"},
		},
	})
	if err != nil || path == "" {
		if err != nil {
			return &ConfigResult{ProfileName: "error: " + err.Error()}
		}
		return nil
	}
	return importToProfiles(path)
}

// importToProfiles читает конфиг и копирует его в profiles/<имя>.json
func importToProfiles(path string) *ConfigResult {
	cfg, err := core.LoadConfig(path)
	if err != nil {
		return &ConfigResult{ProfileName: "error: " + err.Error()}
	}
	exeDir := filepath.Dir(os.Args[0])
	profilesDir := filepath.Join(exeDir, "profiles")
	os.MkdirAll(profilesDir, 0755)

	name := cfg.ProfileName
	if name == "" {
		name = fmt.Sprintf("profile-%d", cfg.ShortID)
	}
	dst := filepath.Join(profilesDir, name+".json")
	data, _ := json.MarshalIndent(cfg, "", "  ")
	os.WriteFile(dst, data, 0644)
	return toResult(cfg)
}

// DeleteProfile удаляет профиль из папки profiles/.
func (a *App) DeleteProfile(name string) string {
	exeDir := filepath.Dir(os.Args[0])
	p := filepath.Join(exeDir, "profiles", name+".json")
	if err := os.Remove(p); err != nil {
		return "error: " + err.Error()
	}
	return "deleted"
}

func toResult(cfg core.Config) *ConfigResult {
	name := cfg.ProfileName
	/*if name == "" {
		name = "Default"
	}*/
	return &ConfigResult{
		ProfileName: name,
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

type ProfileItem struct {
	Name       string `json:"name"`
	ServerIP   string `json:"server_ip"`
	Port       int    `json:"port"`
	ShortID    uint16 `json:"short_id"`
	InternalIP string `json:"internal_ip"`
	DNS        string `json:"dns"`
}

func (a *App) ListProfileItems() []ProfileItem {
	exeDir := filepath.Dir(os.Args[0])
	profilesDir := filepath.Join(exeDir, "profiles")
	entries, _ := os.ReadDir(profilesDir)
	var items []ProfileItem
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
			continue
		}
		data, _ := os.ReadFile(filepath.Join(profilesDir, e.Name()))
		var cfg core.Config
		json.Unmarshal(data, &cfg)
		name := e.Name()[:len(e.Name())-5]
		items = append(items, ProfileItem{
			Name:       name,
			ServerIP:   cfg.ServerIP,
			Port:       cfg.Port,
			ShortID:    cfg.ShortID,
			InternalIP: cfg.InternalIP,
			DNS:        cfg.DNS,
		})
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Name < items[j].Name })
	return items
}

func (a *App) DoConnect(serverIP, secretKey, routingSalt, internalIP, gatewayIP, dns string, port int, shortID uint16) string {
	if a.vpn != nil && a.vpn.IsRunning() {
		if a.logger != nil {
			a.logger.Println("DoConnect: already running, returning 'already connected'")
		}
		return "already connected"
	}
	if a.logger != nil {
		a.logger.Printf("DoConnect: server=%s:%d shortID=%d", serverIP, port, shortID)
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
	vpn := core.New(cfg, &vpnListener{ctx: a.ctx})
	if err := vpn.Start(); err != nil {
		if a.logger != nil {
			a.logger.Printf("DoConnect: Start() error: %v", err)
		}
		return err.Error()
	}
	a.vpn = vpn
	if a.logger != nil {
		a.logger.Println("DoConnect: connected successfully")
	}
	return "connected"
}

func (a *App) DoDisconnect() string {
	if a.vpn == nil || !a.vpn.IsRunning() {
		if a.logger != nil {
			a.logger.Println("DoDisconnect: not connected")
		}
		return "not connected"
	}
	if a.logger != nil {
		a.logger.Println("DoDisconnect: stopping VPN")
	}
	a.vpn.Stop()
	a.vpn = nil
	runtime.EventsEmit(a.ctx, "status", "disconnected")
	return "disconnected"
}

func (a *App) IsConnected() bool {
	return a.vpn != nil && a.vpn.IsRunning()
}

func (a *App) SaveLastProfile(name string) {
	exeDir := filepath.Dir(os.Args[0])
	os.WriteFile(filepath.Join(exeDir, "last_profile.txt"), []byte(name), 0644)
}

func (a *App) LoadLastProfile() string {
	exeDir := filepath.Dir(os.Args[0])
	data, err := os.ReadFile(filepath.Join(exeDir, "last_profile.txt"))
	if err != nil {
		exeDir = "."
		data, err = os.ReadFile(filepath.Join(exeDir, "last_profile.txt"))
	}
	if err != nil {
		return ""
	}
	return string(data)
}
