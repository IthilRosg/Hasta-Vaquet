//go:build android

package core

import (
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
	// TUN уже создан на Java стороне (VpnService.Builder.establish())
	// fd записан в plat.tunFd из StartVPN() в gomobile.go
	if p.tunFd <= 0 {
		return nil // будет ошибка при первом read/write
	}
	return nil
}

func (p *vpnPlatform) closeTunnel(v *VPN) {
	if p.tunFd > 0 {
		syscall.Close(p.tunFd)
		p.tunFd = 0
	}
}

func (p *vpnPlatform) readerLoop(v *VPN) {
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
			continue
		}

		// Пишем расшифрованный пакет в TUN-интерфейс
		if p.tunFd > 0 {
			syscall.Write(p.tunFd, decrypted)
		}
		v.rxBytes.Add(int64(len(decrypted)))
		v.sessionTotalRx.Add(uint64(len(decrypted)))
	}
}

func (p *vpnPlatform) writerLoop(v *VPN) {
	buf := make([]byte, 65535)
	for {
		select {
		case <-v.stopCh:
			return
		default:
		}

		if p.tunFd <= 0 {
			continue
		}

		// Читаем пакет из TUN-интерфейса
		n, err := syscall.Read(p.tunFd, buf)
		if err != nil || n < 20 {
			continue
		}

		// Только IPv4
		if (buf[0] >> 4) != 4 {
			continue
		}

		encrypted, err := Encrypt(buf[:n], v.key[:], v.config.ShortID, v.config.RoutingSalt)
		if err == nil {
			v.conn.Write(encrypted)
			v.txBytes.Add(int64(len(encrypted)))
			v.sessionTotalTx.Add(uint64(len(encrypted)))
		}
	}
}

// ─── Глобальный экземпляр платформы ──────────────────────────────

var plat vpnPlatform

func platformOpenTunnel(v *VPN) error { return plat.openTunnel(v) }
func platformCloseTunnel(v *VPN)      { plat.closeTunnel(v) }
func platformReaderLoop(v *VPN)       { plat.readerLoop(v) }
func platformWriterLoop(v *VPN)       { plat.writerLoop(v) }
