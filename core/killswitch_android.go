//go:build android

package core

import "log"

func newKillSwitch() KillSwitch {
	return &androidKillSwitch{}
}

type androidKillSwitch struct{}

func (a *androidKillSwitch) Activate(_, _ string) error {
	log.Printf("[KILLSWITCH] Android: blocking is handled by VpnService.Builder.setBlocking(true)")
	return nil
}

func (a *androidKillSwitch) Deactivate() error {
	return nil
}
