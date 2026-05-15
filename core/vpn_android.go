//go:build android

package core

import (
	"fmt"
	"time"
)

// vpnPlatform — Android-специфичная реализация туннеля.
// TUN-интерфейс приходит из Java VpnService как FileDescriptor,
// читаем/пишем через него как через обычный файл.
type vpnPlatform struct {
	tunFd int // файловый дескриптор TUN (из VpnService.establish())
}

func (p *vpnPlatform) openTunnel(v *VPN) error {
	// TODO: Phase 8 — TUN создаётся на Java стороне (VpnService),
	// fd передаётся сюда через gomobile bind.
	return fmt.Errorf("Android tunnel not yet implemented")
}

func (p *vpnPlatform) closeTunnel(v *VPN) {
	// TODO: Phase 8 — закрыть TUN, очистить маршруты
	_ = p.tunFd
}

func (p *vpnPlatform) readerLoop(v *VPN) {
	buf := make([]byte, 65535)
	for {
		select {
		case <-v.stopCh:
			return
		default:
		}
		// Читаем расшифрованный пакет из UDP
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
		// TODO: записать decrypted[] в TUN-интерфейс (fd.Write)
		v.rxBytes.Add(int64(len(decrypted)))
		v.sessionTotalRx.Add(uint64(len(decrypted)))
	}
}

func (p *vpnPlatform) writerLoop(v *VPN) {
	for {
		select {
		case <-v.stopCh:
			return
		default:
		}
		// TODO: Phase 8 — читать пакет из TUN (fd.Read)
		// и отправлять через Encrypt + v.conn.Write
	}
}

// ─── Глобальный экземпляр платформы ──────────────────────────────

var plat vpnPlatform

func platformOpenTunnel(v *VPN) error { return plat.openTunnel(v) }
func platformCloseTunnel(v *VPN)      { plat.closeTunnel(v) }
func platformReaderLoop(v *VPN)       { plat.readerLoop(v) }
func platformWriterLoop(v *VPN)       { plat.writerLoop(v) }
