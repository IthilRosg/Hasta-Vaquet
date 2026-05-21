//go:build windows

package core

import (
	"fmt"
	"log"
	"net"
	"os/exec"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sys/windows"
	"golang.zx2c4.com/wintun"
)

// platform-специфичные поля VPN
type vpnPlatform struct {
	session *wintun.Session
	adapter *wintun.Adapter
	ifIndex string // сохранённый индекс интерфейса для восстановления маршрута после закрытия адаптера
}

func (p *vpnPlatform) openTunnel(v *VPN) error {
	log.Printf("[ROUTE] openTunnel: creating adapter + routes")
	adapter, err := wintun.CreateAdapter("HastaVaquet", "HastaVaquet", nil)
	if err != nil {
		return fmt.Errorf("adapter: %w", err)
	}
	p.adapter = adapter

	index := getInterfaceIndex("HastaVaquet")
	p.ifIndex = index
	run := func(cmd string, args ...string) {
		c := exec.Command(cmd, args...)
		c.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
		c.CombinedOutput()
	}

	run("netsh", "interface", "ip", "set", "address", "name=HastaVaquet", "static", v.config.InternalIP, "255.255.255.0")
	run("netsh", "interface", "ipv4", "set", "subinterface", "name=HastaVaquet", "mtu=1300")
	run("netsh", "interface", "ip", "set", "dns", "name=HastaVaquet", "static", v.config.DNS)
	run("route", "delete", v.config.ServerIP)
	run("route", "add", v.config.ServerIP, "mask", "255.255.255.255", v.config.GatewayIP)
	run("route", "delete", "0.0.0.0", v.config.InternalIP)
	run("route", "add", "0.0.0.0", "mask", "0.0.0.0", v.config.InternalIP, "metric", "1", "if", index)
	run("netsh", "interface", "ipv6", "add", "route", "::/0", "name=HastaVaquet", v.config.InternalIP, "metric=1")
	log.Printf("[ROUTE] openTunnel done: ifIndex=%s, internal=%s, gateway=%s, server=%s",
		p.ifIndex, v.config.InternalIP, v.config.GatewayIP, v.config.ServerIP)

	conn, err := net.DialUDP("udp", nil, &net.UDPAddr{
		IP:   net.ParseIP(v.config.ServerIP),
		Port: v.config.Port,
	})
	if err != nil {
		adapter.Close()
		return fmt.Errorf("dial: %w", err)
	}
	v.conn = conn

	sess, err := adapter.StartSession(0x800000)
	if err != nil {
		conn.Close()
		adapter.Close()
		return fmt.Errorf("session: %w", err)
	}
	p.session = &sess
	return nil
}

func (p *vpnPlatform) closeTunnel(v *VPN) {
	log.Printf("[ROUTE] closeTunnel: cleaning up %s/%s", v.config.InternalIP, v.config.GatewayIP)
	hide := func(cmd string, args ...string) {
		c := exec.Command(cmd, args...)
		c.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
		c.Run()
	}
	hide("route", "delete", "0.0.0.0", v.config.InternalIP)
	hide("netsh", "interface", "ipv6", "delete", "route", "::/0", "name=HastaVaquet")

	if p.session != nil {
		p.session.End()
	}
	if p.adapter != nil {
		p.adapter.Close()
	}
	log.Printf("[ROUTE] closeTunnel done")
}

func (p *vpnPlatform) readerLoop(v *VPN) {
	buf := make([]byte, 65535)
	for {
		select {
		case <-v.stopCh:
			return
		default:
		}
		v.conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		n, err := v.conn.Read(buf)
		if err != nil {
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				fails := v.readFails.Add(1)
				if fails >= 3 {
					v.onConnectionLost()
					return
				}
			} else {
				v.readFails.Add(1)
			}
			continue
		}
		v.readFails.Store(0)
		if n < 4+2+12 {
			continue
		}
		decrypted, err := Decrypt(buf[:n], v.key[:])
		if err != nil {
			continue
		}
		if len(decrypted) == 0 {
			continue
		}
		// Server echo response (1-byte marker for RTT measurement)
		if len(decrypted) == 1 && decrypted[0] == 0x01 {
			v.echoAck()
			last := v.lastAliveMs.Load()
			if last > 0 {
				rtt := time.Now().UnixMilli() - last
				if rtt > 0 && rtt < 10000 {
					v.echoRtt.Store(rtt)
					v.echoReceived.Store(true)
				}
			}
			continue
		}
		packet, err := p.session.AllocateSendPacket(len(decrypted))
		if err != nil {
			continue
		}
		copy(packet, decrypted)
		p.session.SendPacket(packet)
		v.rxBytes.Add(int64(len(decrypted)))
		v.sessionTotalRx.Add(uint64(len(decrypted)))
	}
}

func (p *vpnPlatform) activateKillSwitch(v *VPN) {
	log.Printf("[ROUTE] activateKillSwitch: adding server route %s via %s", v.config.ServerIP, v.config.GatewayIP)
	// 1. Добавляем маршрут до сервера через реальный шлюз — чтобы reconnect мог до него достучаться
	exec.Command("route", "add", v.config.ServerIP, "mask", "255.255.255.255",
		v.config.GatewayIP, "metric", "1").Run()
	// 2. Удаляем default route — блокируем весь остальной трафик
	exec.Command("route", "delete", "0.0.0.0", "mask", "0.0.0.0").Run()
}

func (p *vpnPlatform) deactivateKillSwitch(v *VPN) {
	if p.ifIndex == "" {
		log.Printf("[ROUTE] deactivateKillSwitch: no ifIndex, fallback to gateway %s", v.config.GatewayIP)
		exec.Command("route", "add", "0.0.0.0", "mask", "0.0.0.0",
			v.config.GatewayIP, "metric", "1").Run()
		return
	}
	log.Printf("[ROUTE] deactivateKillSwitch: restoring default route via %s if=%s", v.config.InternalIP, p.ifIndex)
	exec.Command("route", "add", "0.0.0.0", "mask", "0.0.0.0",
		v.config.InternalIP, "metric", "1", "if", p.ifIndex).Run()
}

func platformDumpRoutes() {
	out, _ := exec.Command("route", "print", "0.0.0.0").Output()
	log.Printf("[ROUTE] DUMP:\n%s", string(out))
}

func (p *vpnPlatform) writerLoop(v *VPN) {
	for {
		select {
		case <-v.stopCh:
			return
		default:
		}
		packet, err := p.session.ReceivePacket()
		if err == nil {
			if len(packet) >= 20 && (packet[0]>>4) == 4 {
				encrypted, err := Encrypt(packet, v.key[:], v.config.ShortID, v.config.RoutingSalt)
				if err == nil {
					v.conn.Write(encrypted)
					v.txBytes.Add(int64(len(encrypted)))
					v.sessionTotalTx.Add(uint64(len(encrypted)))
				}
			}
			p.session.ReleaseReceivePacket(packet)
		} else if err == windows.ERROR_NO_MORE_ITEMS {
			windows.WaitForSingleObject(p.session.ReadWaitEvent(), windows.INFINITE)
		}
	}
}

// ─── Глобальный экземпляр платформы для хуков ────────────────────

var plat vpnPlatform

func platformOpenTunnel(v *VPN) error         { return plat.openTunnel(v) }
func platformCloseTunnel(v *VPN)              { plat.closeTunnel(v) }
func platformReaderLoop(v *VPN)               { plat.readerLoop(v) }
func platformWriterLoop(v *VPN)               { plat.writerLoop(v) }
func platformActivateKillSwitch(v *VPN)       { plat.activateKillSwitch(v) }
func platformDeactivateKillSwitch(v *VPN)     { plat.deactivateKillSwitch(v) }

func getInterfaceIndex(name string) string {
	cmd := exec.Command("powershell", "-Command",
		fmt.Sprintf("Get-NetAdapter -Name '%s' | Select-Object -ExpandProperty InterfaceIndex", name))
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	out, _ := cmd.Output()
	return strings.TrimSpace(string(out))
}
