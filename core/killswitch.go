package core

import "fmt"

type KillSwitch interface {
	Activate(serverIP, gateway string) error
	Deactivate() error
}

type defaultKillSwitch struct{}

func (d *defaultKillSwitch) Activate(_, _ string) error {
	return fmt.Errorf("kill switch not supported on this platform")
}

func (d *defaultKillSwitch) Deactivate() error {
	return fmt.Errorf("kill switch not supported on this platform")
}
