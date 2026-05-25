//go:build android

package core

import (
	"fmt"
	"log"
	"net"
	"os"
	"strings"
	"syscall"
	"time"
)

// vpnPlatform — Android-специфичная реализация туннеля.
type vpnPlatform struct {
	tunFile *os.File // TUN-интерфейс
}

func (p *vpnPlatform) openTunnel(v *VPN) error {
	if p.tunFile == nil {
		return fmt.Errorf("tunFile is nil")
	}

	if globalProtector == nil {
		return fmt.Errorf("protector is nil")
	}

	// Go сам создает сокет и просит Android его защитить через интерфейс Protector
	dialer := &net.Dialer{
		Control: func(network, address string, c syscall.RawConn) error {
			return c.Control(func(fd uintptr) {
				if !globalProtector.Protect(int(fd)) {
					log.Printf("[ANDROID] openTunnel: protect failed for fd %d", fd)
				}
			})
		},
	}

	serverAddr := fmt.Sprintf("%s:%d", v.config.ServerIP, v.config.Port)
	conn, err := dialer.Dial("udp", serverAddr)
	if err != nil {
		return fmt.Errorf("UDP dial failed: %v", err)
	}

	v.conn = conn.(*net.UDPConn)
	log.Printf("[ANDROID] openTunnel: UDP connected and PROTECTED, target=%s", serverAddr)
	return nil
}

func (p *vpnPlatform) closeTunnel(v *VPN) {
	log.Printf("[ANDROID] closeTunnel: closing tunnel")
	if v.conn != nil {
		v.conn.Close()
		v.conn = nil
	}
	if p.tunFile != nil {
		p.tunFile.Close()
		p.tunFile = nil
	}
}

func (p *vpnPlatform) readerLoop(v *VPN) {
	log.Printf("[ANDROID] readerLoop: STARTED")
	buf := make([]byte, 65535)
	pktCount := 0

	for {
		select {
		case <-v.stopCh:
			log.Printf("[ANDROID] readerLoop: STOPPED (pkts=%d)", pktCount)
			return
		default:
		}

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
			pktCount++
			if pktCount%10 == 0 {
				log.Printf("[ANDROID] readerLoop: keep-alive OK, rtt=%dms (pkts=%d)",
					int(time.Now().UnixMilli()-v.lastAliveMs.Load()), pktCount)
			}
			continue
		}

		// Echo-пинг (1 байт 0x01)
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

		// Пишем расшифрованный пакет в TUN через os.File (блокирующийся)
		if p.tunFile != nil {
			if _, err := p.tunFile.Write(decrypted); err != nil {
				log.Printf("[ANDROID] readerLoop: TUN write error: %v", err)
			}
		}
		v.rxBytes.Add(int64(len(decrypted)))
		v.sessionTotalRx.Add(uint64(len(decrypted)))
	}
}

func (p *vpnPlatform) writerLoop(v *VPN) {
	log.Printf("[ANDROID] writerLoop: STARTED")
	buf := make([]byte, 65535)
	pktCount := 0

	for {
		select {
		case <-v.stopCh:
			log.Printf("[ANDROID] writerLoop: STOPPED (pkts=%d)", pktCount)
			return
		default:
		}

		if p.tunFile == nil {
			continue
		}

		// Читаем пакет из TUN через os.File (runtime poller ждёт данные)
		n, err := p.tunFile.Read(buf)
		if err != nil || n < 20 {
			if err != nil && pktCount < 5 {
				log.Printf("[ANDROID] writerLoop: TUN read error: %v", err)
			}
			continue
		}
		pktCount++

		if (buf[0] >> 4) != 4 { // только IPv4
			continue
		}

		encrypted, err := Encrypt(buf[:n], v.key[:], v.config.ShortID, v.config.RoutingSalt)
		if err == nil {
			v.conn.Write(encrypted)
			v.txBytes.Add(int64(len(encrypted)))
			v.sessionTotalTx.Add(uint64(len(encrypted)))
			if pktCount%50 == 0 {
				log.Printf("[ANDROID] writerLoop: sent %d pkts, last=%d bytes", pktCount, n)
			}
		}
	}
}

// ─── Глобальный экземпляр платформы ──────────────────────────────

var plat vpnPlatform

func platformOpenTunnel(v *VPN) error            { return plat.openTunnel(v) }
func platformCloseTunnel(v *VPN)                 { plat.closeTunnel(v) }
func platformReaderLoop(v *VPN)                  { plat.readerLoop(v) }
func platformWriterLoop(v *VPN)                  { plat.writerLoop(v) }
func platformActivateKillSwitch(v *VPN)          {}
func platformDeactivateKillSwitch(v *VPN)        {}
func (p *vpnPlatform) reconnectSocket(v *VPN) {
	// Закрываем старый сокет если есть
	if v.conn != nil {
		old := v.conn
		v.conn = nil
		old.Close()
	}

	serverAddr := fmt.Sprintf("%s:%d", v.config.ServerIP, v.config.Port)
	dialer := &net.Dialer{
		Control: func(network, address string, c syscall.RawConn) error {
			return c.Control(func(fd uintptr) {
				if !globalProtector.Protect(int(fd)) {
					log.Printf("[ANDROID] reconnectSocket: protect failed for fd %d", fd)
				}
			})
		},
	}
	newConn, err := dialer.Dial("udp", serverAddr)
	if err != nil {
		log.Printf("[ANDROID] reconnectSocket: dial failed: %v", err)
		return
	}
	v.conn = newConn.(*net.UDPConn)
	log.Printf("[ANDROID] reconnectSocket: socket recreated (%s)", serverAddr)
}

func platformRefreshServerRoute(v *VPN)          { plat.refreshServerRoute(v) }
func platformGatewayIsValid(v *VPN) bool         { return plat.gatewayIsValid(v) }
func platformReconnectSocket(v *VPN)             { plat.reconnectSocket(v) }
func platformReconnectSession(v *VPN)            { plat.reconnectSession(v) }
func platformDestroyTunnel(v *VPN)               { plat.destroyTunnel() }

func (p *vpnPlatform) activateKillSwitch(v *VPN)       {}
func (p *vpnPlatform) deactivateKillSwitch(v *VPN)     {}
func (p *vpnPlatform) refreshServerRoute(v *VPN)       {}
func (p *vpnPlatform) gatewayIsValid(v *VPN) bool      { return true }
func (p *vpnPlatform) reconnectSocket(v *VPN)          {}
func (p *vpnPlatform) reconnectSession(v *VPN)          {}
func (p *vpnPlatform) destroyTunnel()                  {}

func platformDumpRoutes() {}
