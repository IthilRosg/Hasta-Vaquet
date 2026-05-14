package core

import (
	"fmt"
	mathrand "math/rand"
	"net"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"golang.org/x/sys/windows"
	"golang.zx2c4.com/wintun"
)

type StatusCallback func(status string, txSpeed, rxSpeed int64, totalTx, totalRx uint64, pingMs int, lossPct int)

type VPN struct {
	config         Config
	key            [32]byte
	conn           *net.UDPConn
	session        *wintun.Session
	adapter        *wintun.Adapter
	running        atomic.Bool
	stopCh         chan struct{}
	txBytes        atomic.Int64
	rxBytes        atomic.Int64
	sessionTotalTx atomic.Uint64
	sessionTotalRx atomic.Uint64
	onStatus       StatusCallback
	mu             sync.Mutex
	lastAliveMs    atomic.Int64 // unix milli of last keep-alive sent
	echoReceived   atomic.Bool  // true if at least one echo came back
	echoRtt        atomic.Int64 // latest RTT in ms
	echoSent       atomic.Int64 // keep-alives sent
	echoAcked      atomic.Int64 // echos received
}

func New(cfg Config, cb StatusCallback) *VPN {
	return &VPN{
		config:   cfg,
		key:      DeriveKey(cfg.SecretKey),
		onStatus: cb,
	}
}

func (v *VPN) Start() error {
	v.mu.Lock()
	defer v.mu.Unlock()

	if v.running.Load() {
		return fmt.Errorf("already running")
	}
	v.stopCh = make(chan struct{})

	v.callback("connecting", 0, 0, 0, 0, 0, 0)

	adapter, err := wintun.CreateAdapter("HastaVaquet", "HastaVaquet", nil)
	if err != nil {
		return fmt.Errorf("adapter: %w", err)
	}
	v.adapter = adapter

	index := getInterfaceIndex("HastaVaquet")
	run := func(cmd string, args ...string) {
		c := exec.Command(cmd, args...)
		c.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
		c.CombinedOutput()
	}

	run("netsh", "interface", "ip", "set", "address", "name=HastaVaquet", "static", v.config.InternalIP, "255.255.255.0")
	run("netsh", "interface", "ip", "set", "dns", "name=HastaVaquet", "static", v.config.DNS)
	run("route", "delete", v.config.ServerIP)
	run("route", "add", v.config.ServerIP, "mask", "255.255.255.255", v.config.GatewayIP)
	run("route", "delete", "0.0.0.0", v.config.InternalIP)
	run("route", "add", "0.0.0.0", "mask", "0.0.0.0", v.config.InternalIP, "metric", "1", "if", index)
	// Blackhole IPv6: route ::/0 through Wintun (our code ignores IPv6 = drop = no leak)
	run("netsh", "interface", "ipv6", "add", "route", "::/0", "name=HastaVaquet", v.config.InternalIP, "metric=1")

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
	v.session = &sess

	v.running.Store(true)
	v.callback("connected", 0, 0, 0, 0, 0, 0)

	go v.keepAliveLoop()
	go v.readerLoop()
	go v.writerLoop()
	go v.statsLoop()

	return nil
}

func (v *VPN) Stop() {
	v.mu.Lock()
	defer v.mu.Unlock()

	if !v.running.Load() {
		return
	}
	v.running.Store(false)
	close(v.stopCh)

	hide := func(cmd string, args ...string) {
		c := exec.Command(cmd, args...)
		c.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
		c.Run()
	}
	hide("route", "delete", "0.0.0.0", v.config.InternalIP)
	hide("netsh", "interface", "ipv6", "delete", "route", "::/0", "name=HastaVaquet")

	if v.conn != nil {
		v.conn.Close()
	}
	if v.session != nil {
		v.session.End()
	}
	if v.adapter != nil {
		v.adapter.Close()
	}

	v.callback("disconnected", 0, 0, 0, 0, 0, 0)
}

func (v *VPN) IsRunning() bool {
	return v.running.Load()
}

func (v *VPN) callback(status string, txSpeed, rxSpeed int64, totalTx, totalRx uint64, pingMs, lossPct int) {
	if v.onStatus != nil {
		v.onStatus(status, txSpeed, rxSpeed, totalTx, totalRx, pingMs, lossPct)
	}
}

func (v *VPN) keepAliveLoop() {
	for {
		packet, err := Encrypt([]byte{}, v.key[:], v.config.ShortID, v.config.RoutingSalt)
		if err == nil {
			v.conn.Write(packet)
			v.lastAliveMs.Store(time.Now().UnixMilli())
			v.echoSent.Add(1)
		}
		select {
		case <-v.stopCh:
			return
		case <-time.After(time.Duration(10+mathrand.Intn(21)) * time.Second):
		}
	}
}

func (v *VPN) readerLoop() {
	buf := make([]byte, 65535)
	for {
		select {
		case <-v.stopCh:
			return
		default:
		}
		n, err := v.conn.Read(buf)
		if err != nil {
			continue
		}
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
			v.echoAcked.Add(1)
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
		packet, err := v.session.AllocateSendPacket(len(decrypted))
		if err != nil {
			continue
		}
		copy(packet, decrypted)
		v.session.SendPacket(packet)
		v.rxBytes.Add(int64(len(decrypted)))
		v.sessionTotalRx.Add(uint64(len(decrypted)))
	}
}

func (v *VPN) writerLoop() {
	for {
		select {
		case <-v.stopCh:
			return
		default:
		}
		packet, err := v.session.ReceivePacket()
		if err == nil {
			if len(packet) >= 20 && (packet[0]>>4) == 4 {
				encrypted, err := Encrypt(packet, v.key[:], v.config.ShortID, v.config.RoutingSalt)
				if err == nil {
					v.conn.Write(encrypted)
					v.txBytes.Add(int64(len(encrypted)))
					v.sessionTotalTx.Add(uint64(len(encrypted)))
				}
			}
			v.session.ReleaseReceivePacket(packet)
		} else if err == windows.ERROR_NO_MORE_ITEMS {
			windows.WaitForSingleObject(v.session.ReadWaitEvent(), windows.INFINITE)
		}
	}
}

func (v *VPN) statsLoop() {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-v.stopCh:
			return
		case <-ticker.C:
			txSpeed := v.txBytes.Swap(0)
			rxSpeed := v.rxBytes.Swap(0)
			var pingMs, lossPct int
			if v.echoReceived.Load() {
				pingMs = int(v.echoRtt.Load())
				sent := v.echoSent.Load()
				acked := v.echoAcked.Load()
				if sent > 0 {
					lossPct = int((sent - acked) * 100 / sent)
				}
			}
			if txSpeed > 0 || rxSpeed > 0 || pingMs > 0 {
				v.callback("traffic", txSpeed, rxSpeed, v.sessionTotalTx.Load(), v.sessionTotalRx.Load(), pingMs, lossPct)
			}
		}
	}
}

func getInterfaceIndex(name string) string {
	cmd := exec.Command("powershell", "-Command",
		fmt.Sprintf("Get-NetAdapter -Name '%s' | Select-Object -ExpandProperty InterfaceIndex", name))
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	out, _ := cmd.Output()
	return strings.TrimSpace(string(out))
}
