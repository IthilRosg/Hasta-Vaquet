package main

import (
	"crypto/sha256"
	_ "embed"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	qrcode "github.com/skip2/go-qrcode"
)

//go:embed static/index.html
var indexHTML []byte

// adminPath хранит URL-префикс панели (например "/hasta-vaquet").
// Используется хендлерами для корректной маршрутизации.
var adminPath string
var (
	prevBytesIn  int64
	prevBytesOut int64
	lastSpeedAt  time.Time
	speedMu      sync.Mutex
)

// startWebPanel запускает HTTP-сервер панели управления.
// Вызывается только если admin_token задан в конфиге.
func startWebPanel() {
	adminPath = serverCfg.AdminPath
	p := adminPath // удобный alias

	// HTML с подставленным API_BASE
	servedHTML := strings.ReplaceAll(string(indexHTML), "{{API_BASE}}", p)

	mux := http.NewServeMux()

	// Корень — отдаём панель
	mux.HandleFunc(p+"/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte(servedHTML))
	})
	// Редирект с голого пути без трейлинг-слэша
	mux.HandleFunc(p, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, p+"/", http.StatusMovedPermanently)
	})
	mux.HandleFunc(p+"/api/stats", withAuth(handleStats))
	mux.HandleFunc(p+"/api/health/detail", withAuth(handleHealthDetail))
	mux.HandleFunc(p+"/api/users", withAuth(handleUsersRoute))
	mux.HandleFunc(p+"/api/users/", withAuth(handleUserRoute))
	mux.HandleFunc(p+"/api/reset", withAuth(handleReset))

	addr := fmt.Sprintf(":%d", serverCfg.AdminPort)
	logger.Printf("[WEB] Панель запущена: http://localhost%s%s/\n", addr, p)
	if err := http.ListenAndServe(addr, mux); err != nil {
		logger.Printf("[WEB] Ошибка запуска: %v\n", err)
	}
}

// withAuth — middleware: проверяет X-Admin-Token header или ?token= query.
func withAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := r.Header.Get("X-Admin-Token")
		if token == "" {
			token = r.URL.Query().Get("token")
		}
		if token != serverCfg.AdminToken {
			logger.Printf("[WEB] 401 %s %s from %s", r.Method, r.URL.Path, r.RemoteAddr)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			w.Write([]byte(`{"error":"unauthorized"}`))
			return
		}
		// Content-Type по умолчанию JSON, кроме /qr и /config — они перезапишут сами
		w.Header().Set("Content-Type", "application/json")
		logger.Printf("[WEB] %s %s from %s", r.Method, r.URL.Path, r.RemoteAddr)
		next(w, r)
	}
}

// ─── GET /api/stats ──────────────────────────────────────────────────────────

func handleStats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	cutoff := time.Now().Add(-90 * time.Second).Unix()
	peersMu.RLock()
	online := 0
	var totalIn, totalOut int64
	for _, p := range peers {
		totalIn += p.CumRx.Load()
		totalOut += p.CumTx.Load()
		if p.LastSeen.Load() > cutoff {
			online++
		}
	}
	total := len(peers)
	peersMu.RUnlock()
	cpuPct, memPct, diskPct := getSystemHealth()
	now := time.Now()
	speedMu.Lock()
	txSpeed := int64(0)
	rxSpeed := int64(0)
	if !lastSpeedAt.IsZero() {
		elapsed := int64(now.Sub(lastSpeedAt).Seconds())
		if elapsed > 0 {
			txSpeed = (totalOut - prevBytesOut) / elapsed
			rxSpeed = (totalIn - prevBytesIn) / elapsed
		}
	}
	prevBytesOut = totalOut
	prevBytesIn = totalIn
	lastSpeedAt = now
	speedMu.Unlock()

	json.NewEncoder(w).Encode(map[string]any{
		"uptime_sec":   int64(time.Since(serverStartTime).Seconds()),
		"peers_total":  total,
		"peers_online": online,
		"total_in":     totalIn,
		"cpu_pct":      cpuPct,
		"mem_pct":      memPct,
		"disk_pct":     diskPct,
		"total_out":    totalOut,
		"tx_speed":     txSpeed,
		"rx_speed":     rxSpeed,
	})
}

// ─── GET /api/users  POST /api/users ─────────────────────────────────────────

type UserResp struct {
	ShortID  uint16 `json:"short_id"`
	Name     string `json:"name"`
	IP       string `json:"ip"`
	Online   bool   `json:"online"`
	LastSeen int64  `json:"last_seen"`
	ByteIn   int64  `json:"byte_in"`
	ByteOut  int64  `json:"byte_out"`
}

func handleUsersRoute(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		listUsers(w, r)
	case http.MethodPost:
		createUser(w, r)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func listUsers(w http.ResponseWriter, _ *http.Request) {
	cutoff := time.Now().Add(-90 * time.Second).Unix()
	peersMu.RLock()
	list := make([]UserResp, 0, len(peers))
	for _, p := range peers {
		list = append(list, UserResp{
			ShortID:  p.ShortID,
			Name:     p.Name,
			IP:       p.Internal,
			Online:   p.LastSeen.Load() > cutoff,
			LastSeen: p.LastSeen.Load(),
			ByteIn:   p.CumRx.Load(),
			ByteOut:  p.CumTx.Load(),
		})
	}
	peersMu.RUnlock()
	sort.Slice(list, func(i, j int) bool { return list[i].ShortID < list[j].ShortID })
	json.NewEncoder(w).Encode(map[string]any{"users": list})
}

func createUser(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ShortID   uint16 `json:"short_id"`
		Name      string `json:"name"`
		SecretKey string `json:"secret_key"`
		IP        string `json:"ip"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"error":"invalid json"}`))
		return
	}
	if req.ShortID == 0 || req.SecretKey == "" || req.IP == "" {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"error":"short_id, secret_key and ip are required"}`))
		return
	}

	peersMu.Lock()
	if _, exists := peers[req.ShortID]; exists {
		peersMu.Unlock()
		logger.Printf("[WEB] Ошибка создания: short_id=%d уже существует\n", req.ShortID)
		w.WriteHeader(http.StatusConflict)
		w.Write([]byte(`{"error":"short_id already exists"}`))
		return
	}
	if _, exists := ipToPeer[req.IP]; exists {
		peersMu.Unlock()
		logger.Printf("[WEB] Ошибка создания: ip=%s уже используется\n", req.IP)
		w.WriteHeader(http.StatusConflict)
		w.Write([]byte(`{"error":"ip already exists"}`))
		return
	}
	p := &Peer{
		ShortID:  req.ShortID,
		Name:     req.Name,
		KeyRaw:   req.SecretKey,
		Key:      sha256.Sum256([]byte(req.SecretKey)),
		Internal: req.IP,
	}
	peers[req.ShortID] = p
	ipToPeer[req.IP] = p
	peersMu.Unlock()

	if err := saveConfig(); err != nil {
		logger.Printf("[WEB] Ошибка сохранения конфига: %v\n", err)
	}
	logger.Printf("[WEB] Добавлен пользователь ShortID=%d Name=%q IP=%s\n", req.ShortID, req.Name, req.IP)
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]string{"status": "created"})
}

// ─── /api/users/{id}[/config|/qr] ────────────────────────────────────────────

func handleUserRoute(w http.ResponseWriter, r *http.Request) {
	// Парсим путь: /api/users/{id}  или  /api/users/{id}/config  или  /api/users/{id}/qr
	trimmed := strings.TrimPrefix(r.URL.Path, adminPath+"/api/users/")
	parts := strings.SplitN(trimmed, "/", 2)

	id64, err := strconv.ParseUint(parts[0], 10, 16)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"error":"invalid id"}`))
		return
	}
	shortID := uint16(id64)

	sub := ""
	if len(parts) == 2 {
		sub = parts[1]
	}

	switch sub {
	case "":
		switch r.Method {
		case http.MethodDelete:
			deleteUser(w, shortID)
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	case "config":
		serveConfig(w, shortID)
	case "qr":
		serveQR(w, shortID)
	default:
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"error":"not found"}`))
	}
}

func deleteUser(w http.ResponseWriter, shortID uint16) {
	peersMu.Lock()
	p, exists := peers[shortID]
	if !exists {
		peersMu.Unlock()
		logger.Printf("[WEB] Ошибка удаления: short_id=%d не найден\n", shortID)
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"error":"user not found"}`))
		return
	}
	delete(peers, shortID)
	delete(ipToPeer, p.Internal)
	peersMu.Unlock()

	if err := saveConfig(); err != nil {
		logger.Printf("[WEB] Ошибка сохранения конфига: %v\n", err)
	}
	logger.Printf("[WEB] Удалён пользователь ShortID=%d\n", shortID)
	json.NewEncoder(w).Encode(map[string]string{"status": "deleted"})
}

// ─── Генерация клиентского конфига ───────────────────────────────────────────

type ClientCfg struct {
	ProfileName string `json:"profile_name"`
	ServerIP    string `json:"server_ip"`
	Port        int    `json:"port"`
	ShortID     uint16 `json:"short_id"`
	SecretKey   string `json:"secret_key"`
	RoutingSalt string `json:"routing_salt"`
	InternalIP  string `json:"internal_ip"`
	GatewayIP   string `json:"gateway_ip"`
	DNS         string `json:"dns"`
}

func buildClientCfg(p *Peer) ClientCfg {
	name := p.Name
	if name == "" {
		name = fmt.Sprintf("User-%d", p.ShortID)
	}
	return ClientCfg{
		ProfileName: name,
		ServerIP:    serverCfg.ServerIP,
		Port:        serverCfg.Port,
		ShortID:     p.ShortID,
		SecretKey:   p.KeyRaw,
		RoutingSalt: serverCfg.RoutingSalt,
		InternalIP:  p.Internal,
		GatewayIP:   serverCfg.GatewayIP,
		DNS:         serverCfg.DNS,
	}
}

func serveConfig(w http.ResponseWriter, shortID uint16) {
	peersMu.RLock()
	p, exists := peers[shortID]
	peersMu.RUnlock()
	if !exists {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"error":"user not found"}`))
		return
	}
	data, _ := json.MarshalIndent(buildClientCfg(p), "", "  ")
	filename := p.Name
	if filename == "" {
		filename = fmt.Sprintf("user-%d", shortID)
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s.json"`, filename))
	w.Write(data)
}

func serveQR(w http.ResponseWriter, shortID uint16) {
	peersMu.RLock()
	p, exists := peers[shortID]
	peersMu.RUnlock()
	if !exists {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"error":"user not found"}`))
		return
	}
	data, _ := json.Marshal(buildClientCfg(p))
	png, err := qrcode.Encode(string(data), qrcode.Medium, 512)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"error":"qr generation failed"}`))
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.Write(png)
}

// ─── Сохранение конфига на диск ──────────────────────────────────────────────

func saveConfig() error {
	configMu.Lock()
	defer configMu.Unlock()

	peersMu.RLock()
	users := make([]ConfigUser, 0, len(peers))
	for _, p := range peers {
		users = append(users, ConfigUser{
			ShortID:   p.ShortID,
			Name:      p.Name,
			SecretKey: p.KeyRaw,
			IP:        p.Internal,
		})
	}
	peersMu.RUnlock()

	sort.Slice(users, func(i, j int) bool { return users[i].ShortID < users[j].ShortID })
	serverCfg.Users = users

	data, err := json.MarshalIndent(serverCfg, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}
	if err := os.WriteFile(configFilePath, data, 0644); err != nil {
		return fmt.Errorf("write: %w", err)
	}
	return nil
}

// ─── GET /api/health/detail ──────────────────────────────────────────────────

func handleHealthDetail(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	cpuPct, memPct, diskPct := getSystemHealth()
	cpuCores, memTotal, memUsed, diskTotal, diskUsed := getSystemHealthDetail()
	json.NewEncoder(w).Encode(map[string]any{
		"cpu_pct":       cpuPct,
		"mem_pct":       memPct,
		"disk_pct":      diskPct,
		"cpu_cores":     cpuCores,
		"mem_total_gb":  memTotal,
		"mem_used_gb":   memUsed,
		"disk_total_gb": diskTotal,
		"disk_used_gb":  diskUsed,
	})
}

// ─── POST /api/reset ─────────────────────────────────────────────────────────

func handleReset(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	peersMu.Lock()
	for _, p := range peers {
		p.ByteIn.Store(0)
		p.ByteOut.Store(0)
	}
	peersMu.Unlock()
	prevBytesIn = 0
	prevBytesOut = 0
	json.NewEncoder(w).Encode(map[string]string{"status": "reset"})
}
