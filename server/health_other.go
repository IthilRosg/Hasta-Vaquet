//go:build !linux

package main

func getSystemHealth() (cpuPct int, memPct int, diskPct int) {
	return 0, 0, 0
}

func getSystemHealthDetail() (cpuCores int, memTotalGB float64, memUsedGB float64, diskTotalGB float64, diskUsedGB float64) {
	return 0, 0, 0, 0, 0
}
