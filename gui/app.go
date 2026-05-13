package main

import (
	"context"
	"encoding/json"
	_ "embed"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"

	"hasta-vaquet/core"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

//go:embed wintun.dll
var wintunDLL []byte

type App struct {
	ctx       context.Context
	vpn       *core.VPN
	profiles  []string
	pingStop  chan struct{}
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
	cfg, err := core.LoadConfig(path)
	if err != nil {
		return &ConfigResult{ProfileName: "error:" + err.Error()}
	}
	return toResult(cfg)
}

func toResult(cfg core.Config) *ConfigResult {
	name := cfg.ProfileName
	if name == "" {
		name = "Default"
	}
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
	Name        string `json:"name"`
	ServerIP    string `json:"server_ip"`
	Port        int    `json:"port"`
	ShortID     uint16 `json:"short_id"`
	InternalIP  string `json:"internal_ip"`
	DNS         string `json:"dns"`
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

func (a *App) DoPing() map[string]int {
	cmd := exec.Command("ping", "-n", "1", "-w", "3000", "8.8.8.8")
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	out, _ := cmd.Output()
	for _, l := range strings.Split(string(out), "\n") {
		if idx := strings.Index(l, "time="); idx >= 0 {
			after := l[idx+5:]
			if end := strings.Index(after, "ms"); end > 0 {
				v, _ := strconv.Atoi(strings.TrimSpace(after[:end]))
				if v > 0 {
					return map[string]int{"rtt": v, "loss": 0}
				}
			}
		}
	}
	return map[string]int{"rtt": 0, "loss": 100}
}
