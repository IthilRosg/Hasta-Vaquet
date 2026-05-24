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
	log.Printf("[ROUTE] openTunnel: creating adapter + routes")
	// Определяем реальный шлюз ДО изменения таблицы маршрутизации
	p.realGateway = getDefaultGateway()
	if p.realGateway == "" {
		p.realGateway = v.config.GatewayIP // fallback на конфиг, если не смогли определить
	}
	log.Printf("[ROUTE] openTunnel: detected gateway=%s (config=%s)", p.realGateway, v.config.GatewayIP)

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
		p.ifIndex, v.config.InternalIP, v.config.GatewayIP, v.config.ServerIP)

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
		gw = v.config.GatewayIP
	}
	log.Printf("[ROUTE] closeTunnel: restoring default via %s, removing tunnel %s", gw, v.config.InternalIP)
	hide := func(cmd string, args ...string) {
		c := exec.Command(cmd, args...)
		c.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
		c.Run()
	}

	// 1. Сначала восстанавливаем default route через реальный шлюз — чтобы интернет не пропал
	hide("route", "add", "0.0.0.0", "mask", "0.0.0.0",
		gw, "metric", "10")
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

		// При reconnect — короткий таймаут для быстрой реакции на ответ сервера
		if v.reconnecting.Load() {
			v.conn.SetReadDeadline(time.Now().Add(3 * time.Second))
		} else {
			v.conn.SetReadDeadline(time.Now().Add(10 * time.Second))
		}

		n, err := v.conn.Read(buf)
		if err != nil {
			// Плановый останов — не трогаем reconnect
			if v.stopping.Load() || strings.Contains(err.Error(), "use of closed") {
				log.Printf("[VPN] readerLoop: stopping, err=%v — exit", err)
				return
			}
			// Во время reconnect ошибки ожидаемы — не триггерим закрытие TUN
			if !v.reconnecting.Load() {
				fails := v.readFails.Add(1)
				isTimeout := false
				if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
					isTimeout = true
				}
				trigger := fails >= 2
				if !isTimeout {
					trigger = fails >= 1
				}
				log.Printf("[VPN] readerLoop: fail #%d timeout=%v trigger=%v err=%v", fails, isTimeout, trigger, err)
				if trigger {
					v.enterReconnecting()
				}
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

		// Любой валидный пакет во время reconnect — запускаем burst-подтверждение
		if v.reconnecting.Load() {
			v.tryConfirmReconnect()
		}

		if len(decrypted) == 0 {
			continue
		}
		// Server echo response (1-byte marker for RTT measurement)
		if len(decrypted) == 1 && decrypted[0] == 0x01 {
			v.echoAck()
			if v.reconnecting.Load() && v.confirming == 1 {
				v.confirmOk.Add(1)
			}
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
		// Защита от паники: session мог быть обнулён в closeTunnel после Stop()
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
		gw = v.config.GatewayIP
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
		gw = v.config.GatewayIP
	}
	hide := func(cmd string, args ...string) {
		c := exec.Command(cmd, args...)
		c.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
		c.Run()
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
		log.Printf("[ROUTE] refreshServerRoute: no gateway detected, keeping %s", p.realGateway)
		return
	}
	currentIsBad := p.realGateway == "" || strings.HasPrefix(p.realGateway, "0.0")
	if newGw == p.realGateway && !currentIsBad {
		return
	}
	log.Printf("[ROUTE] refreshServerRoute: gateway %s → %s", p.realGateway, newGw)
	hide := func(cmd string, args ...string) {
		c := exec.Command(cmd, args...)
		c.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
		c.CombinedOutput()
	}
	// Принудительно удаляем старый route (даже если висит на 0.0.0.0) и пишем новый
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
func platformRefreshServerRoute(v *VPN)          { plat.refreshServerRoute(v) }
func platformGatewayIsValid(v *VPN) bool         { return plat.gatewayIsValid(v) }

func getInterfaceIndex(name string) string {
	cmd := exec.Command("powershell", "-Command",
		fmt.Sprintf("Get-NetAdapter -Name '%s' | Select-Object -ExpandProperty InterfaceIndex", name))
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	out, _ := cmd.Output()
	return strings.TrimSpace(string(out))
}
