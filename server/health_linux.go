//go:build linux

package main

import (
	"bufio"
	"os"
	"strconv"
	"strings"
	"syscall"
	"time"
)

var (
	prevIdle  uint64
	prevTotal uint64
	prevTime  time.Time
)

func getSystemHealth() (cpuPct int, memPct int, diskPct int) {
	// CPU: читаем /proc/stat, считаем разницу idle/total между вызовами
	f, err := os.Open("/proc/stat")
	if err == nil {
		defer f.Close()
		sc := bufio.NewScanner(f)
		for sc.Scan() {
			line := sc.Text()
			if strings.HasPrefix(line, "cpu ") {
				fields := strings.Fields(line)
				var idle, total uint64
				for i := 1; i < len(fields); i++ {
					v, _ := strconv.ParseUint(fields[i], 10, 64)
					total += v
					if i == 4 {
						idle = v
					}
				}
				if prevTotal > 0 {
					diffIdle := idle - prevIdle
					diffTotal := total - prevTotal
					if diffTotal > 0 {
						cpuPct = int(100 - (diffIdle*100)/diffTotal)
					}
				}
				prevIdle = idle
				prevTotal = total
				prevTime = time.Now()
				break
			}
		}
	}

	// RAM: /proc/meminfo
	f2, err := os.Open("/proc/meminfo")
	if err == nil {
		defer f2.Close()
		var total, available uint64
		sc := bufio.NewScanner(f2)
		for sc.Scan() {
			line := sc.Text()
			fields := strings.Fields(line)
			if len(fields) < 2 {
				continue
			}
			v, _ := strconv.ParseUint(fields[1], 10, 64)
			switch fields[0] {
			case "MemTotal:":
				total = v
			case "MemAvailable:":
				available = v
			}
		}
		if total > 0 {
			memPct = int(100 - (available*100)/total)
		}
	}

	// Disk: где лежит лог-файл
	var stat syscall.Statfs_t
	if err := syscall.Statfs(serverCfg.LogFile, &stat); err == nil {
		total := stat.Blocks * uint64(stat.Bsize)
		free := stat.Bfree * uint64(stat.Bsize)
		if total > 0 {
			diskPct = int(100 - (free*100)/total)
		}
	}

	return
}
