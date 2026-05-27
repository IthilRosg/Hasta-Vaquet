//go:build !windows && !android

package core

func newKillSwitch() KillSwitch {
	return &defaultKillSwitch{}
}
