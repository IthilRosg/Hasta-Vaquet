//go:build !linux

package main

func getSystemHealth() (cpuPct int, memPct int, diskPct int) {
	return 0, 0, 0
}
