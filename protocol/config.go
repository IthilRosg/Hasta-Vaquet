package protocol

import (
	"encoding/json"
	"os"
)

const (
	TransportUDP  = "udp"
	TransportWSS  = "wss"
	TransportQUIC = "quic"
)

type Config struct {
	ProfileName string `json:"profile_name,omitempty"`
	ServerIP    string `json:"server_ip"`
	Port        int    `json:"port"`
	WSPort      int    `json:"ws_port,omitempty"`
	ShortID     uint16 `json:"short_id"`
	SecretKey   string `json:"secret_key"`
	RoutingSalt string `json:"routing_salt,omitempty"`
	InternalIP  string `json:"internal_ip,omitempty"`
	GatewayIP   string `json:"gateway_ip,omitempty"`
	DNS         string `json:"dns,omitempty"`
	FEC         int    `json:"fec,omitempty"`
	MTU         int    `json:"mtu,omitempty"`
	AdminPort   int    `json:"admin_port,omitempty"`
	AdminToken  string `json:"admin_token,omitempty"`
	AdminPath   string `json:"admin_path,omitempty"`
	NoEncrypt   bool   `json:"no_encrypt,omitempty"`
	LogFile     string `json:"log_file,omitempty"`
	Users       []User `json:"users,omitempty"`

	Transport         string   `json:"transport"`
	CDNDomain         string   `json:"cdn_domain,omitempty"`
	TLSCertFile       string   `json:"tls_cert,omitempty"`
	TLSKeyFile        string   `json:"tls_key,omitempty"`
	TransportPriority []string `json:"transport_priority,omitempty"`

	// Smart Bypass (split tunneling)
	BypassMode  string   `json:"bypass_mode,omitempty"`  // "" | "vpn_only" | "bypass"
	BypassCIDRs []string `json:"bypass_cidrs,omitempty"` // e.g. ["5.45.192.0/24"]
}

type User struct {
	ShortID   uint16 `json:"short_id"`
	Name      string `json:"name"`
	SecretKey string `json:"secret_key"`
	IP        string `json:"ip"`
}

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
	if c.WSPort == 0 {
		c.WSPort = 19998
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
	if c.Transport == "" {
		c.Transport = "auto"
	}
	if len(c.TransportPriority) == 0 {
		c.TransportPriority = []string{"wss", "udp"}
	}
}

type CipherSuite int

const (
	CipherAES256GCM CipherSuite = iota
	CipherChaCha20Poly1305
)

type ProtocolConfig struct {
	Cipher    CipherSuite
	MimicType string
	MinPad    int
	MaxPad    int
	FEC       int
	MTU       int
}
