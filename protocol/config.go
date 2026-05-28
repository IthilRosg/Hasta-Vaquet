package protocol

import (
	"encoding/json"
	"os"
)

// Config — единая конфигурация для клиента и сервера.
// Все поля вычитываются из JSON, хардкод-дефолтов в коде нет.
type Config struct {
	ProfileName string `json:"profile_name,omitempty"`
	ServerIP    string `json:"server_ip"`
	Port        int    `json:"port"`
	ShortID     uint16 `json:"short_id"`
	SecretKey   string `json:"secret_key"`
	RoutingSalt string `json:"routing_salt,omitempty"`
	InternalIP  string `json:"internal_ip,omitempty"`
	GatewayIP   string `json:"gateway_ip,omitempty"`
	DNS         string `json:"dns,omitempty"`
	FEC         int    `json:"fec,omitempty"` // 1=off
	MTU         int    `json:"mtu,omitempty"`
	AdminPort   int    `json:"admin_port,omitempty"`
	AdminToken  string `json:"admin_token,omitempty"`
	AdminPath   string `json:"admin_path,omitempty"`
	NoEncrypt   bool   `json:"no_encrypt,omitempty"` // true = отключить шифрование (тесты)
	LogFile     string `json:"log_file,omitempty"`
	Users       []User `json:"users,omitempty"`
}

type User struct {
	ShortID   uint16 `json:"short_id"`
	Name      string `json:"name"`
	SecretKey string `json:"secret_key"`
	IP        string `json:"ip"`
}

// SetDefaults заполняет пропущенные поля разумными значениями.
func LoadConfig(path string) (Config, error) {
	cfg := Config{}
	if f, err := os.Open(path); err == nil {
		defer f.Close()
		json.NewDecoder(f).Decode(&cfg)
	} else {
		return cfg, err
	}
	cfg.SetDefaults()
	return cfg, nil
}

func (c *Config) SetDefaults() {
	if c.Port == 0 {
		c.Port = 19999
	}
	if c.RoutingSalt == "" {
		c.RoutingSalt = "HastaVaquetGlobal"
	}
	if c.GatewayIP == "" {
		c.GatewayIP = "192.168.100.1"
	}
	if c.DNS == "" {
		c.DNS = "1.1.1.1"
	}
	if c.MTU == 0 {
		c.MTU = 1300
	}
	if c.FEC < 1 || c.FEC > 5 {
		c.FEC = 1
	}
	if c.AdminPort == 0 {
		c.AdminPort = 9998
	}
	if c.AdminPath == "" {
		c.AdminPath = "/hasta-vaquet"
	}
	if c.LogFile == "" {
		c.LogFile = "server.log"
	}
}

// CipherSuite — выбор шифрования.
type CipherSuite int

const (
	CipherAES256GCM CipherSuite = iota
	CipherChaCha20Poly1305
)

// ProtocolConfig — настройки протокола (могут согласовываться при handshake).
type ProtocolConfig struct {
	Cipher    CipherSuite
	MimicType string // "" / "tls" / "http2" / "quic"
	MinPad    int
	MaxPad    int
	FEC       int
	MTU       int
}
