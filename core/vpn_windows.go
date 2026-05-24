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
	session     *wintun.Session
	adapter     *wintun.Adapter
	ifIndex     string // сохранённый индекс интерфейса для восстановления маршрута после закрытия адаптера
	realGateway string // реальный шлюз, определённый при старте
}

func getDefaultGateway() string {
	// Используем netsh вместо route+findstr — так можно скрыть окно консоли
	cmd := exec.Command("powershell", "-Command",
		"Get-NetRoute -DestinationPrefix '0.0.0.0/0' | Select-Object -First 1 -ExpandProperty NextHop")
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

func (p *vpnPlatform) openTunnel(v *VPN) error {
	log.Printf("[ROUTE] openTunnel: detecting default gateway")
	p.realGateway = getDefaultGateway()
	if p.realGateway == "" {
		return fmt.Errorf("no default gateway detected — cannot set up routes, ensure network is connected")
	}
	log.Printf("[ROUTE] openTunnel: detected gateway=%s", p.realGateway)

	// Пытаемся открыть существующий адаптер, чтобы не плодить лишние
	adapter, err := wintun.OpenAdapter("HastaVaquet")
	if err != nil {
		log.Printf("[ROUTE] openTunnel: no existing adapter (%v), creating new one", err)
		adapter, err = wintun.CreateAdapter("HastaVaquet", "HastaVaquet", nil)
		if err != nil {
			return fmt.Errorf("adapter: %w", err)
		}
	} else {
		log.Printf("[ROUTE] openTunnel: reused existing adapter")
	}
	p.adapter = adapter

	// Гарантированное закрытие адаптера при ошибках ниже
	closeOnErr := true
	defer func() {
		if closeOnErr {
			if p.session != nil {
				p.session.End()
				p.session = nil
			}
			if p.adapter != nil {
				p.adapter.Close()
				p.adapter = nil
			}
		}
	}()

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
	run("route", "add", v.config.ServerIP, "mask", "255.255.255.255", p.realGateway)
	run("route", "delete", "0.0.0.0", "mask", "0.0.0.0", v.config.InternalIP)
	run("route", "delete", "0.0.0.0", v.config.InternalIP)
	run("route", "add", "0.0.0.0", "mask", "0.0.0.0", v.config.InternalIP, "metric", "1", "if", index)
	run("netsh", "interface", "ipv6", "add", "route", "::/0", "name=HastaVaquet", v.config.InternalIP, "metric=1")
	log.Printf("[ROUTE] openTunnel done: ifIndex=%s, internal=%s, gateway=%s, server=%s",
		p.ifIndex, v.config.InternalIP, p.realGateway, v.config.ServerIP)

	conn, err := net.DialUDP("udp", nil, &net.UDPAddr{
		IP:   net.ParseIP(v.config.ServerIP),
		Port: v.config.Port,
	})
	if err != nil {
		return fmt.Errorf("dial: %w", err)
	}
	v.conn = conn

	sess, err := adapter.StartSession(0x800000)
	if err != nil {
		return fmt.Errorf("session: %w", err)
	}
	p.session = &sess
	closeOnErr = false
	return nil
}

func (p *vpnPlatform) closeTunnel(v *VPN) {
	gw := p.realGateway
	if gw == "" {
		gw = getDefaultGateway() // динамическое автоопределение на момент закрытия
	}
	hide := func(cmd string, args ...string) {
		c := exec.Command(cmd, args...)
		c.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
		c.Run()
	}

	if gw != "" {
		log.Printf("[ROUTE] closeTunnel: restoring default via %s, removing tunnel %s", gw, v.config.InternalIP)
		// 1. Сначала восстанавливаем default route через реальный шлюз — чтобы интернет не пропал
		hide("route", "add", "0.0.0.0", "mask", "0.0.0.0",
			gw, "metric", "10")
	} else {
		log.Printf("[ROUTE] closeTunnel: no gateway detected, skipping route restore")
	}
	// 2. Только потом удаляем туннельный route
	hide("route", "delete", "0.0.0.0", "mask", "0.0.0.0", v.config.InternalIP)
	hide("route", "delete", "0.0.0.0", v.config.InternalIP) // запасной вариант без mask

	hide("netsh", "interface", "ipv6", "delete", "route", "::/0", "name=HastaVaquet")

	if p.session != nil {
		p.session.End()
		p.session = nil
	}
	if p.adapter != nil {
		p.adapter.Close()
		p.adapter = nil
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

		if v.conn == nil {
			time.Sleep(100 * time.Millisecond)
			continue
		}
		v.conn.SetReadDeadline(time.Now().Add(10 * time.Second))

		n, err := v.conn.Read(buf)
		if err != nil {
			if v.stopping.Load() {
				return
			}
			if strings.Contains(err.Error(), "use of closed") {
				continue
			}
			continue
		}
		// Любой успешный пакет = сервер жив
		v.lastPacketRx.Store(time.Now().UnixMilli())

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
		// Echo response — обновляем RTT
		if len(decrypted) == 1 && decrypted[0] == 0x01 {
			v.echoAck()
			last := v.lastAliveMs.Load()
			if last > 0 {
				rtt := time.Now().UnixMilli() - last
				if rtt > 0 && rtt < 10000 {
					v.echoRtt.Store(rtt)
				}
			}
			continue
		}
		if v.stopping.Load() || p.session == nil {
			return
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
	gw := p.realGateway
	if gw == "" {
		gw = getDefaultGateway()
	}
	if gw == "" {
		log.Printf("[ROUTE] activateKillSwitch: no gateway, skipping")
		return
	}
	log.Printf("[ROUTE] activateKillSwitch: adding server route %s via %s", v.config.ServerIP, gw)
	hide := func(cmd string, args ...string) {
		c := exec.Command(cmd, args...)
		c.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
		c.Run()
	}
	// 1. Добавляем маршрут до сервера через реальный шлюз — чтобы reconnect мог до него достучаться
	hide("route", "add", v.config.ServerIP, "mask", "255.255.255.255", gw, "metric", "1")
	// 2. Удаляем default route — блокируем весь остальной трафик
	hide("route", "delete", "0.0.0.0", "mask", "0.0.0.0")
}

func (p *vpnPlatform) deactivateKillSwitch(v *VPN) {
	gw := p.realGateway
	if gw == "" {
		gw = getDefaultGateway()
	}
	hide := func(cmd string, args ...string) {
		c := exec.Command(cmd, args...)
		c.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
		c.Run()
	}
	if gw == "" {
		log.Printf("[ROUTE] deactivateKillSwitch: no gateway, skipping route restore")
		return
	}
	if p.ifIndex == "" {
		log.Printf("[ROUTE] deactivateKillSwitch: no ifIndex, fallback to gateway %s", gw)
		hide("route", "add", "0.0.0.0", "mask", "0.0.0.0", gw, "metric", "1")
		return
	}
	log.Printf("[ROUTE] deactivateKillSwitch: restoring default route via %s if=%s", v.config.InternalIP, p.ifIndex)
	hide("route", "add", "0.0.0.0", "mask", "0.0.0.0",
		v.config.InternalIP, "metric", "1", "if", p.ifIndex)
}

func platformDumpRoutes() {
	c := exec.Command("route", "print", "0.0.0.0")
	c.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	out, _ := c.Output()
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

// refreshServerRoute переопределяет route до сервера через текущий шлюз ОС.
// Шлюз мог измениться при смене сети (WiFi → Ethernet, переезд).
func (p *vpnPlatform) refreshServerRoute(v *VPN) {
	newGw := getDefaultGateway()
	if newGw == "" {
		log.Printf("[ROUTE] refreshServerRoute: no gateway detected, route NOT updated")
		return
	}
	log.Printf("[ROUTE] refreshServerRoute: force update %s → %s", p.realGateway, newGw)
	hide := func(cmd string, args ...string) {
		c := exec.Command(cmd, args...)
		c.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
		c.CombinedOutput()
	}
	// Безусловно удаляем старый route и пишем через актуальный шлюз ОС
	hide("route", "delete", v.config.ServerIP)
	hide("route", "add", v.config.ServerIP, "mask", "255.255.255.255", newGw, "metric", "1")
	p.realGateway = newGw
	log.Printf("[ROUTE] refreshServerRoute: updated route to %s via %s", v.config.ServerIP, newGw)
}

func (p *vpnPlatform) gatewayIsValid(v *VPN) bool {
	gw := p.realGateway
	if gw == "" || strings.HasPrefix(gw, "0.0") {
		log.Printf("[ROUTE] gatewayIsValid: false (gateway=%q)", gw)
		return false
	}
	return true
}

// ─── Глобальный экземпляр платформы для хуков ────────────────────

var plat vpnPlatform

func platformOpenTunnel(v *VPN) error            { return plat.openTunnel(v) }
func platformCloseTunnel(v *VPN)                 { plat.closeTunnel(v) }
func platformReaderLoop(v *VPN)                  { plat.readerLoop(v) }
func platformWriterLoop(v *VPN)                  { plat.writerLoop(v) }
func platformActivateKillSwitch(v *VPN)          { plat.activateKillSwitch(v) }
func platformDeactivateKillSwitch(v *VPN)        { plat.deactivateKillSwitch(v) }
func (p *vpnPlatform) reconnectSocket(v *VPN) {
	if v.conn == nil {
		return
	}
	// Закрываем старый сокет — readerLoop поймает "use of closed" и встанет на nil-guard
	old := v.conn
	v.conn = nil
	old.Close()

	// Создаём новый сокет — он привяжется к актуальному сетевому интерфейсу
	newConn, err := net.DialUDP("udp", nil, &net.UDPAddr{
		IP:   net.ParseIP(v.config.ServerIP),
		Port: v.config.Port,
	})
	if err != nil {
		log.Printf("[VPN] reconnectSocket: dial failed: %v, will retry next cycle", err)
		// НЕ восстанавливаем старый — он закрыт. readerLoop подождёт через nil-guard.
		return
	}
	v.conn = newConn
	log.Printf("[VPN] reconnectSocket: socket recreated (%s:%d)", v.config.ServerIP, v.config.Port)
}

func platformRefreshServerRoute(v *VPN)          { plat.refreshServerRoute(v) }
func platformGatewayIsValid(v *VPN) bool         { return plat.gatewayIsValid(v) }
func platformReconnectSocket(v *VPN)             { plat.reconnectSocket(v) }

func getInterfaceIndex(name string) string {
	cmd := exec.Command("powershell", "-Command",
		fmt.Sprintf("Get-NetAdapter -Name '%s' | Select-Object -ExpandProperty InterfaceIndex", name))
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	out, _ := cmd.Output()
	return strings.TrimSpace(string(out))
}
