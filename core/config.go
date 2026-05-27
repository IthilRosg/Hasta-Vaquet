package core

import (
	"encoding/json"
	"os"
)

type Config struct {
	ProfileName string `json:"profile_name"`
	ServerIP    string `json:"server_ip"`
	Port        int    `json:"port"`
	ShortID     uint16 `json:"short_id"`
	SecretKey   string `json:"secret_key"`
	RoutingSalt string `json:"routing_salt"`
	InternalIP  string `json:"internal_ip"`
	GatewayIP   string `json:"gateway_ip"`
	DNS         string `json:"dns"`
	FEC         int    `json:"fec"` // packet duplication: 1=off, 2=2x, 3=3x...
}

func LoadConfig(path string) (Config, error) {
	cfg := Config{}
	if f, err := os.Open(path); err == nil {
		defer f.Close()
		json.NewDecoder(f).Decode(&cfg)
	} else {
		return cfg, err
	}
	if cfg.Port == 0 {
		cfg.Port = 19999
	}
	if cfg.RoutingSalt == "" {
		cfg.RoutingSalt = "HastaVaquetGlobal"
	}
	if cfg.GatewayIP == "" {
		cfg.GatewayIP = "192.168.100.1"
	}
	if cfg.DNS == "" {
		cfg.DNS = "1.1.1.1"
	}
	if cfg.FEC < 1 || cfg.FEC > 5 {
		cfg.FEC = 1 // default: off
	}
	return cfg, nil
}
