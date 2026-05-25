//go:build !windows && !android

package core

import "fmt"

// vpnPlatform — заглушка для Linux/неклиентских платформ.
// Сервер использует только crypto.go (Encrypt/Decrypt/Fnv1a) и не нуждается
// в VPN-клиентском функционале. Но Go требует, чтобы все методы типа VPN
// были определены на всех платформах при компиляции пакета.
type vpnPlatform struct{}

func (p *vpnPlatform) openTunnel(v *VPN) error {
	return fmt.Errorf("unsupported platform")
}
func (p *vpnPlatform) closeTunnel(v *VPN)      {}
func (p *vpnPlatform) readerLoop(v *VPN)       {}
func (p *vpnPlatform) writerLoop(v *VPN)       {}
func (p *vpnPlatform) activateKillSwitch(v *VPN)   {}
func (p *vpnPlatform) deactivateKillSwitch(v *VPN) {}

func platformDumpRoutes() {}

var plat vpnPlatform

func platformOpenTunnel(v *VPN) error            { return plat.openTunnel(v) }
func platformCloseTunnel(v *VPN)                 { plat.closeTunnel(v) }
func platformReaderLoop(v *VPN)                  { plat.readerLoop(v) }
func platformWriterLoop(v *VPN)                  { plat.writerLoop(v) }
func platformActivateKillSwitch(v *VPN)          { plat.activateKillSwitch(v) }
func platformDeactivateKillSwitch(v *VPN)        { plat.deactivateKillSwitch(v) }
func platformRefreshServerRoute(v *VPN)          { plat.refreshServerRoute(v) }
func platformGatewayIsValid(v *VPN) bool         { return plat.gatewayIsValid(v) }
func platformReconnectSocket(v *VPN)             { plat.reconnectSocket(v) }
func platformReconnectSession(v *VPN)            { plat.reconnectSession(v) }
func platformDestroyTunnel(v *VPN)               { plat.destroyTunnel() }

func (p *vpnPlatform) refreshServerRoute(v *VPN)       {}
func (p *vpnPlatform) gatewayIsValid(v *VPN) bool      { return true }
func (p *vpnPlatform) reconnectSocket(v *VPN)          {}
func (p *vpnPlatform) destroyTunnel()                  {}
