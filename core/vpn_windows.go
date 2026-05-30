//go:build windows

package core

import (
	"context"
	"fmt"
	"log"
	"net"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"

	"golang.org/x/sys/windows"
	"golang.zx2c4.com/wintun"
)

// tunBufPool — reusable буферы для чтения пакетов из сокета.
var tunBufPool = sync.Pool{
	New: func() any { return make([]byte, 65535) },
}

// platform-специфичные поля VPN
type vpnPlatform struct {
	session      *wintun.Session
	adapter      *wintun.Adapter
	ifIndex      string
	realGateway  string
	ipSet        bool
	logPerfCount int
}

func getDefaultGateway() string {
	// Ищем default-маршруты через физические интерфейсы (не Wintun)
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
		// Дополнительный фильтр: исключаем 10.0.0.0/8 (совпадает с туннелем)
		if ip.IsPrivate() && ip.To4() != nil && ip.To4()[0] == 10 {
			return ""
		}
		return gw
	}
	return ""
}

func (p *vpnPlatform) openTunnel(v *VPN) error {
	log.Printf("[ROUTE] openTunnel: detecting default gateway")

	// Transport dial must happen BEFORE route setup so we know the
	// server IP is resolvable and the connection is established.
	ctx := context.Background()
	conn, transportType, err := v.transport.Dial(ctx)
	if err != nil {
		return fmt.Errorf("transport dial: %w", err)
	}
	v.conn = conn
	v.transportType = transportType
	log.Printf("[TRANSPORT] Connected via %s", transportType)
	for i := 0; i < 3; i++ {
		p.realGateway = getDefaultGateway()
		if p.realGateway != "" {
			break
		}
		if i < 2 {
			log.Printf("[ROUTE] openTunnel: gateway not found, retry %d/3 in 1s", i+1)
			time.Sleep(1 * time.Second)
		}
	}
	if p.realGateway == "" {
		return fmt.Errorf("no internet connection — no default gateway detected after 3 attempts")
	}
	log.Printf("[ROUTE] openTunnel: detected gateway=%s", p.realGateway)

	// Если адаптер уже открыт (предыдущий Stop не убил его) — переиспользуем
	if p.adapter == nil {
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
	} else {
		log.Printf("[ROUTE] openTunnel: adapter already open, reusing existing handle")
	}

	// Гарантированное закрытие при ошибках (только session, adapter живёт)
	closeOnErr := true
	defer func() {
		if closeOnErr {
			if p.session != nil {
				p.session.End()
				p.session = nil
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

	// netsh только при первом запуске
	if !p.ipSet {
		run("netsh", "interface", "ip", "set", "address", "name=HastaVaquet", "static", v.config.InternalIP, "255.255.255.0")
		run("netsh", "interface", "ipv4", "set", "subinterface", "name=HastaVaquet", "mtu=1300")
		run("netsh", "interface", "ip", "set", "dns", "name=HastaVaquet", "static", v.config.DNS)
		run("netsh", "interface", "ipv6", "add", "route", "::/0", "name=HastaVaquet", v.config.InternalIP, "metric=1")
		// Set low interface metric so VPN default route beats Ethernet
		run("powershell", "-NoProfile", "-Command",
			fmt.Sprintf("Set-NetIPInterface -InterfaceIndex %s -InterfaceMetric 1 -ErrorAction SilentlyContinue", index))
		p.ipSet = true
	}
	// Set VPN interface metric to 1, physical interfaces to 1000 — VPN becomes primary
	run("powershell", "-NoProfile", "-Command",
		fmt.Sprintf("Set-NetIPInterface -InterfaceIndex %s -InterfaceMetric 1 -ErrorAction SilentlyContinue", index))
	run("powershell", "-NoProfile", "-Command",
		"Get-NetIPInterface -InterfaceAlias 'Ethernet' -ErrorAction SilentlyContinue | Set-NetIPInterface -InterfaceMetric 1000")
	run("powershell", "-NoProfile", "-Command",
		"Get-NetIPInterface -InterfaceAlias 'Wi-Fi' -ErrorAction SilentlyContinue | Set-NetIPInterface -InterfaceMetric 1000")

	// Wintun = layer 3 TUN, gateway 0.0.0.0 = on-link (без ARP).
	// writerLoop фильтрует только IPv4, ARP не обрабатывается.
	// Используем on-link маршрут, как WireGuard.
	run("route", "delete", v.config.ServerIP)
	run("route", "add", v.config.ServerIP, "mask", "255.255.255.255", p.realGateway)
	if index != "" {
		run("route", "delete", "0.0.0.0", "mask", "0.0.0.0", "0.0.0.0", "if", index)
	}
	run("route", "delete", "0.0.0.0", v.config.InternalIP)
	if index != "" {
		run("route", "add", "0.0.0.0", "mask", "0.0.0.0", "0.0.0.0", "metric", "1", "if", index)
	}
	log.Printf("[ROUTE] openTunnel done: ifIndex=%s, internal=%s, gateway=%s, server=%s",
		p.ifIndex, v.config.InternalIP, p.realGateway, v.config.ServerIP)

	sess, err := p.adapter.StartSession(0x800000)
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
		gw = getDefaultGateway()
	}
	hide := func(cmd string, args ...string) {
		c := exec.Command(cmd, args...)
		c.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
		c.Run()
	}

	if gw != "" {
		log.Printf("[ROUTE] closeTunnel: restoring default via %s (no iface pinning)", gw)
		// НЕ указываем if= — пусть Windows сама выберет правильный физический интерфейс
		hide("route", "add", "0.0.0.0", "mask", "0.0.0.0", gw, "metric", "10")
	} else {
		log.Printf("[ROUTE] closeTunnel: no gateway detected, skipping route restore")
	}
	if p.ifIndex != "" {
		hide("route", "delete", "0.0.0.0", "mask", "0.0.0.0", "0.0.0.0", "if", p.ifIndex)
	}
	hide("route", "delete", "0.0.0.0", v.config.InternalIP) // старая запись со шлюзом 10.0.0.x
	hide("powershell", "-NoProfile", "-Command",
		"Get-NetIPInterface -InterfaceAlias 'Ethernet' -ErrorAction SilentlyContinue | Set-NetIPInterface -InterfaceMetric 'auto'")
	hide("powershell", "-NoProfile", "-Command",
		"Get-NetIPInterface -InterfaceAlias 'Wi-Fi' -ErrorAction SilentlyContinue | Set-NetIPInterface -InterfaceMetric 'auto'")
	hide("netsh", "interface", "ipv6", "delete", "route", "::/0", "name=HastaVaquet")

	// Сессию закрываем — иначе следующий StartSession не сможет создать новую.
	// Адаптер НЕ закрываем — он остаётся в Windows.
	if p.session != nil {
		p.session.End()
		p.session = nil
	}
	log.Printf("[ROUTE] closeTunnel done (adapter kept, session reset)")
}

// destroyTunnel — полное уничтожение Wintun-адаптера (только при выходе из программы).
func (p *vpnPlatform) destroyTunnel() {
	if p.session != nil {
		p.session.End()
		p.session = nil
	}
	if p.adapter != nil {
		p.adapter.Close()
		p.adapter = nil
	}
	p.ipSet = false
	log.Printf("[ROUTE] destroyTunnel: adapter + session destroyed")
}

type readResult struct {
	n   int
	err error
}

func (p *vpnPlatform) readerLoop(v *VPN) {
	ch := make(chan readResult, 5)

	for {
		conn := v.conn
		if conn == nil {
			time.Sleep(100 * time.Millisecond)
			continue
		}

		buf := tunBufPool.Get().([]byte)
		go func() {
			n, err := conn.Read(buf)
			ch <- readResult{n, err}
		}()

		select {
		case <-v.stopCh:
			return
		case r := <-ch:
			if r.err != nil {
				tunBufPool.Put(buf[:cap(buf)])
				if v.stopping.Load() {
					return
				}
				continue
			}
			v.lastPacketRx.Store(time.Now().UnixMilli())

			if r.n < 4+2+12 {
				tunBufPool.Put(buf[:cap(buf)])
				continue
			}
			decrypted, err := v.decCP.Decrypt(buf[:r.n])
			tunBufPool.Put(buf[:cap(buf)])
			if err != nil {
				continue
			}
			if len(decrypted) == 0 {
				continue
			}
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

		case <-time.After(10 * time.Second):
			continue
		}
	}
}

func platformDumpRoutes() {
	c := exec.Command("route", "print", "0.0.0.0")
	c.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	out, _ := c.Output()
	log.Printf("[ROUTE] DUMP:\n%s", string(out))
}

func (p *vpnPlatform) writerLoop(v *VPN) {
	// Один синхронный writerLoop — без параллельных воркеров.
	// Параллельные воркеры вызывают packet reordering → TCP duplicate ACK → slowdown.
	for {
		select {
		case <-v.stopCh:
			return
		default:
		}
		sess := p.session
		if sess == nil {
			time.Sleep(100 * time.Millisecond)
			continue
		}
		packet, err := sess.ReceivePacket()
		if err == nil {
			if len(packet) >= 20 && (packet[0]>>4) == 4 {
				encrypted, err := v.encCP.Encrypt(packet, v.config.ShortID, v.config.RoutingSalt)
				if err == nil && v.conn != nil {
					fec := v.config.FEC
					if fec < 1 {
						fec = 1
					}
					if fec > 5 {
						fec = 5
					}
					for i := 0; i < fec; i++ {
						v.conn.Write(encrypted)
					}
					v.txBytes.Add(int64(len(encrypted)))
					v.sessionTotalTx.Add(uint64(len(encrypted)))
				}
			}
			sess.ReleaseReceivePacket(packet)
		} else if err == windows.ERROR_NO_MORE_ITEMS {
			if sess != nil {
				windows.WaitForSingleObject(sess.ReadWaitEvent(), windows.INFINITE)
			}
		}
	}
}

// refreshServerRoute переопределяет route до сервера через текущий шлюз ОС.
// Если шлюз временно недоступен — использует последний известный (WireGuard-style).
func (p *vpnPlatform) refreshServerRoute(v *VPN) {
	newGw := strings.TrimSpace(getDefaultGateway())
	current := strings.TrimSpace(p.realGateway)
	if newGw == "" {
		// Нет шлюза сейчас — пробуем последний известный, он может заработать
		newGw = current
	}
	if newGw == "" {
		log.Printf("[ROUTE] refreshServerRoute: no gateway at all, removing stale server route")
		hide := func(cmd string, args ...string) {
			c := exec.Command(cmd, args...)
			c.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
			c.CombinedOutput()
		}
		hide("route", "delete", v.config.ServerIP)
		return
	}
	if newGw == current {
		return
	}
	log.Printf("[ROUTE] refreshServerRoute: gateway %s → %s", current, newGw)
	hide := func(cmd string, args ...string) {
		c := exec.Command(cmd, args...)
		c.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
		c.CombinedOutput()
	}
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
	// Без PowerShell — проверяем кэшированный шлюз
	return true
}

// reconnectSession — вычищает буфер TUN без пересоздания сессии.
// Читает и дропает все накопившиеся пакеты, чтобы старый мусор
// не уходил в новый сокет. Не трогает session handle (безопасно для writerLoop).
func (p *vpnPlatform) reconnectSession(v *VPN) {
	if p.session == nil {
		return
	}
	dropped := 0
	for i := 0; i < 2000; i++ {
		pkt, err := p.session.ReceivePacket()
		if err != nil {
			break
		}
		p.session.ReleaseReceivePacket(pkt)
		dropped++
	}
	if dropped > 0 {
		log.Printf("[VPN] reconnectSession: drained %d stale packets from TUN buffer", dropped)
	} else {
		log.Printf("[VPN] reconnectSession: TUN buffer clean")
	}
}

func (p *vpnPlatform) reconnectSocket(v *VPN) {
	if v.conn != nil {
		old := v.conn
		v.conn = nil
		old.Close()
	}

	ctx := context.Background()
	newConn, transportType, err := v.transport.Dial(ctx)
	if err != nil {
		log.Printf("[VPN] reconnectSocket: all transports failed: %v, will retry next cycle", err)
		return
	}
	v.conn = newConn
	v.transportType = transportType
	log.Printf("[VPN] reconnectSocket: reconnected via %s (%s:%d)", transportType, v.config.ServerIP, v.config.Port)

	// Маршрут до сервера мог пропасть при переподключении сети — передобавляем
	gw := strings.TrimSpace(p.realGateway)
	if gw != "" {
		hide := func(cmd string, args ...string) {
			c := exec.Command(cmd, args...)
			c.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
			c.CombinedOutput()
		}
		hide("route", "delete", v.config.ServerIP)
		hide("route", "add", v.config.ServerIP, "mask", "255.255.255.255", gw, "metric", "1")
	}
}

// ─── Глобальный экземпляр платформы для хуков ────────────────────

var plat vpnPlatform

func platformOpenTunnel(v *VPN) error    { return plat.openTunnel(v) }
func platformCloseTunnel(v *VPN)         { plat.closeTunnel(v) }
func platformDestroyTunnel(v *VPN)       { plat.destroyTunnel() }
func platformReaderLoop(v *VPN)          { plat.readerLoop(v) }
func platformWriterLoop(v *VPN)          { plat.writerLoop(v) }
func platformRefreshServerRoute(v *VPN)  { plat.refreshServerRoute(v) }
func platformGatewayIsValid(v *VPN) bool { return plat.gatewayIsValid(v) }
func platformReconnectSocket(v *VPN)     { plat.reconnectSocket(v) }
func platformReconnectSession(v *VPN)    { plat.reconnectSession(v) }

func getInterfaceIndex(name string) string {
	cmd := exec.Command("powershell", "-Command",
		fmt.Sprintf("Get-NetAdapter -Name '%s' | Select-Object -ExpandProperty InterfaceIndex", name))
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	out, _ := cmd.Output()
	return strings.TrimSpace(string(out))
}
