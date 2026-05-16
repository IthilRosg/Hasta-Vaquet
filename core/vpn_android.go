//go:build android

package core

import (
	"log"
	"net"
	"os"
	"time"
)

// vpnPlatform — Android-специфичная реализация туннеля.
// TUN-интерфейс приходит из Java VpnService как FileDescriptor.
// fd — неблокирующий → используем os.File (runtime poller) вместо syscall.
type vpnPlatform struct {
	tunFile       *os.File // TUN-интерфейс
	protectedConn *os.File // UDP-сокет, защищённый VpnService.protect()
}

func (p *vpnPlatform) openTunnel(v *VPN) error {
	if p.tunFile == nil || p.protectedConn == nil {
		log.Printf("[ANDROID] openTunnel: tunFile=%v protectedConn=%v", p.tunFile != nil, p.protectedConn != nil)
		return nil
	}

	// Создаём net.UDPConn из защищённого fd (уже protect'нут VpnService)
	// Этот сокет идёт в обход TUN → нет петли маршрутизации
	f := p.protectedConn
	pc, err := net.FileConn(f)
	if err != nil {
		log.Printf("[ANDROID] openTunnel: FileConn FAILED: %v", err)
		return err
	}
	v.conn = pc.(*net.UDPConn)
	log.Printf("[ANDROID] openTunnel: UDP connected OK (protected), local=%v", v.conn.LocalAddr())
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
	if p.protectedConn != nil {
		p.protectedConn.Close()
		p.protectedConn = nil
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

func platformOpenTunnel(v *VPN) error { return plat.openTunnel(v) }
func platformCloseTunnel(v *VPN)      { plat.closeTunnel(v) }
func platformReaderLoop(v *VPN)       { plat.readerLoop(v) }
func platformWriterLoop(v *VPN)       { plat.writerLoop(v) }
