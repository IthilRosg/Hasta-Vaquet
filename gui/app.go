package main

import (
	"context"
	"encoding/json"
	_ "embed"
	"net"
	"os"
	"path/filepath"
	"sort"
	"time"

	"hasta-vaquet/core"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

//go:embed wintun.dll
var wintunDLL []byte

type App struct {
	ctx      context.Context
	vpn      *core.VPN
	profiles []string
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
	vpn := core.New(cfg, func(status string, txSpeed, rxSpeed int64, totalTx, totalRx uint64) {
		if status == "connected" || status == "disconnected" {
			runtime.EventsEmit(a.ctx, "status", status)
		}
		if txSpeed > 0 || rxSpeed > 0 {
			runtime.EventsEmit(a.ctx, "traffic", map[string]interface{}{
				"tx_speed": txSpeed, "rx_speed": rxSpeed,
				"total_tx": totalTx, "total_rx": totalRx,
			})
		}
	})
	if err := vpn.Start(); err != nil {
		return err.Error()
	}
	a.vpn = vpn
	a.startPinging()
	return "connected"
}

func (a *App) DoDisconnect() string {
	if a.vpn == nil || !a.vpn.IsRunning() {
		return "not connected"
	}
	a.stopPinging()
	a.vpn.Stop()
	a.vpn = nil
	runtime.EventsEmit(a.ctx, "status", "disconnected")
	return "disconnected"
}

func (a *App) IsConnected() bool {
	return a.vpn != nil && a.vpn.IsRunning()
}

// --- Ping sliding window ---
var (
	pingBuffer [1000]int
	pingIndex  int
	pingFilled int
	pingCancel chan struct{}
)

func (a *App) startPinging() {
	if pingCancel != nil {
		close(pingCancel)
	}
	pingCancel = make(chan struct{})
	pingIndex = 0
	pingFilled = 0
	for i := range pingBuffer {
		pingBuffer[i] = -1
	}

	go func() {
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()

		doPing := func() {
			rtt := -1
			targets := []string{"8.8.8.8:443", "1.1.1.1:443", "10.0.0.2:9999"}
			for _, t := range targets {
				start := time.Now()
				conn, err := net.DialTimeout("tcp", t, 1*time.Second)
				if err == nil {
					conn.Close()
					rtt = int(time.Since(start).Milliseconds())
					if rtt < 1 { rtt = 1 }
					break
				}
			}
			pingBuffer[pingIndex%1000] = rtt
			pingIndex++
			if pingFilled < 1000 { pingFilled++ }
		}
		emitPing := func() {
			var sum, count, lossCount int
			for i := 0; i < pingFilled; i++ {
				v := pingBuffer[i]
				if v >= 0 { sum += v; count++ }
				if v == -1 { lossCount++ }
			}
			total := count + lossCount
			avgRT := 0
			if count > 0 { avgRT = sum / count }
			lossPct := 0
			if total > 0 { lossPct = lossCount * 100 / total }
			runtime.EventsEmit(a.ctx, "ping", map[string]int{"rtt": avgRT, "loss": lossPct})
		}

		doPing()
		emitPing()

		for {
			select {
			case <-pingCancel:
				return
			case <-ticker.C:
				doPing()
				emitPing()
			}
		}
	}()
}

func (a *App) stopPinging() {
	if pingCancel != nil {
		close(pingCancel)
		pingCancel = nil
	}
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
