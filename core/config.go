package core

import (
	"encoding/json"
	"flag"
	"os"
)

type Config struct {
	ServerIP    string `json:"server_ip"`
	Port        int    `json:"port"`
	ShortID     uint16 `json:"short_id"`
	SecretKey   string `json:"secret_key"`
	RoutingSalt string `json:"routing_salt"`
	InternalIP  string `json:"internal_ip"`
	GatewayIP   string `json:"gateway_ip"`
	DNS         string `json:"dns"`
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
		cfg.Port = 9999
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
	return cfg, nil
}

func ParseFlags() (string, bool) {
	var configFile string
	var showHelp bool
	flag.StringVar(&configFile, "config", "config.json", "Path to config.json")
	flag.BoolVar(&showHelp, "help", false, "Show help")
	flag.Parse()
	return configFile, showHelp
}
