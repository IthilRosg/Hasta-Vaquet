//go:build windows

package core

import (
	"log"
	"net"
	"os/exec"
	"strings"
	"syscall"
)

func newKillSwitch() KillSwitch {
	return &windowsKillSwitch{}
}

type windowsKillSwitch struct {
	realGateway string
	ifIndex     string
}

func (w *windowsKillSwitch) Activate(serverIP, gateway string) error {
	gw := gateway
	if gw == "" {
		return nil
	}
	log.Printf("[KILLSWITCH] Activate: server=%s via %s, blocking all other traffic", serverIP, gw)
	hide := func(args ...string) {
		c := exec.Command("route", args...)
		c.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
		c.Run()
	}
	hide("add", serverIP, "mask", "255.255.255.255", gw, "metric", "1")
	hide("delete", "0.0.0.0", "mask", "0.0.0.0")
	return nil
}

func (w *windowsKillSwitch) Deactivate() error {
	gw := w.realGateway
	if gw == "" {
		gw = w.detectGateway()
	}
	if gw == "" {
		return nil
	}
	hide := func(args ...string) {
		c := exec.Command("route", args...)
		c.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
		c.Run()
	}
	log.Printf("[KILLSWITCH] Deactivate: restoring default via %s", gw)
	if w.ifIndex != "" {
		hide("add", "0.0.0.0", "mask", "0.0.0.0", gw, "metric", "1", "if", w.ifIndex)
	} else {
		hide("add", "0.0.0.0", "mask", "0.0.0.0", gw, "metric", "1")
	}
	return nil
}

func (w *windowsKillSwitch) detectGateway() string {
	cmd := exec.Command("powershell", "-Command",
		"Get-NetRoute -DestinationPrefix '0.0.0.0/0' | Where-Object { $_.InterfaceAlias -ne 'HastaVaquet' } | Sort-Object RouteMetric | Select-Object -First 1 -ExpandProperty NextHop")
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	gw := strings.TrimSpace(string(out))
	ip := net.ParseIP(gw)
	if ip != nil && !ip.IsLoopback() && !ip.IsUnspecified() {
		return gw
	}
	return ""
}
