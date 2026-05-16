//go:build android

package core

import (
	"log"
	"net"
	"syscall"
	"time"
)

// vpnPlatform — Android-специфичная реализация туннеля.
// TUN-интерфейс приходит из Java VpnService как FileDescriptor,
// читаем/пишем через syscall на fd.
type vpnPlatform struct {
	tunFd int // файловый дескриптор TUN (из VpnService.establish())
}

func (p *vpnPlatform) openTunnel(v *VPN) error {
	if p.tunFd <= 0 {
		log.Printf("[ANDROID] openTunnel: tunFd=%d — INVALID", p.tunFd)
		return nil
	}

	log.Printf("[ANDROID] openTunnel: connecting UDP to %s:%d", v.config.ServerIP, v.config.Port)
	conn, err := net.DialUDP("udp", nil, &net.UDPAddr{
		IP:   net.ParseIP(v.config.ServerIP),
		Port: v.config.Port,
	})
	if err != nil {
		log.Printf("[ANDROID] openTunnel: DialUDP FAILED: %v", err)
		return err
	}
	v.conn = conn
	log.Printf("[ANDROID] openTunnel: UDP connected OK, local=%v", conn.LocalAddr())
	return nil
}

func (p *vpnPlatform) closeTunnel(v *VPN) {
	log.Printf("[ANDROID] closeTunnel: closing tunnel (fd=%d)", p.tunFd)
	if v.conn != nil {
		v.conn.Close()
		v.conn = nil
	}
	if p.tunFd > 0 {
		syscall.Close(p.tunFd)
		p.tunFd = 0
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
			log.Printf("[ANDROID] readerLoop: UDP read error: %v", err)
			continue
		}
		if n < 4+2+12 {
			log.Printf("[ANDROID] readerLoop: short packet (%d bytes)", n)
			continue
		}

		decrypted, err := Decrypt(buf[:n], v.key[:])
		if err != nil {
			log.Printf("[ANDROID] readerLoop: decrypt FAILED: %v", err)
			continue
		}
		if len(decrypted) == 0 {
			pktCount++
			if pktCount%10 == 0 {
				log.Printf("[ANDROID] readerLoop: keep-alive (pkts=%d)", pktCount)
			}
			continue
		}

		// Echo-пинг (1 байт 0x01) — не пишем в TUN
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
			if pktCount%10 == 0 {
				log.Printf("[ANDROID] readerLoop: echo OK RTT=%dms", int(time.Now().UnixMilli()-v.lastAliveMs.Load()))
			}
			continue
		}

		// Пишем расшифрованный пакет в TUN-интерфейс
		if p.tunFd > 0 {
			wrote, err := syscall.Write(p.tunFd, decrypted)
			if err != nil {
				log.Printf("[ANDROID] readerLoop: TUN write error (fd=%d): %v", p.tunFd, err)
			} else if wrote != len(decrypted) {
				log.Printf("[ANDROID] readerLoop: TUN short write: %d/%d", wrote, len(decrypted))
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

		if p.tunFd <= 0 {
			continue
		}

		// Читаем пакет из TUN-интерфейса
		n, err := syscall.Read(p.tunFd, buf)
		if err != nil || n < 20 {
			if err != nil {
				log.Printf("[ANDROID] writerLoop: TUN read error (fd=%d): %v", p.tunFd, err)
			}
			continue
		}
		pktCount++

		// Только IPv4
		if (buf[0] >> 4) != 4 {
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
		} else {
			log.Printf("[ANDROID] writerLoop: encrypt FAILED: %v", err)
		}
	}
}

// ─── Глобальный экземпляр платформы ──────────────────────────────

var plat vpnPlatform

func platformOpenTunnel(v *VPN) error { return plat.openTunnel(v) }
func platformCloseTunnel(v *VPN)      { plat.closeTunnel(v) }
func platformReaderLoop(v *VPN)       { plat.readerLoop(v) }
func platformWriterLoop(v *VPN)       { plat.writerLoop(v) }
