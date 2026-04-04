//go:build linux

package main

import (
	"bufio"
	"os"
	"strconv"
	"strings"
	"time"
)

// getCPUPercent returns aggregate CPU usage as a percentage by reading
// /proc/stat twice with a 200 ms gap.
func getCPUPercent() float64 {
	read := func() (idle, total uint64) {
		f, err := os.Open("/proc/stat")
		if err != nil {
			return 0, 0
		}
		defer f.Close()
		scanner := bufio.NewScanner(f)
		for scanner.Scan() {
			line := scanner.Text()
			if !strings.HasPrefix(line, "cpu ") {
				continue
			}
			fields := strings.Fields(line)
			// fields[0]="cpu", fields[1..10] are user,nice,system,idle,iowait,irq,softirq,steal,guest,guest_nice
			var vals [10]uint64
			for i := 1; i <= 10 && i < len(fields); i++ {
				v, _ := strconv.ParseUint(fields[i], 10, 64)
				vals[i-1] = v
				total += v
			}
			idle = vals[3] + vals[4] // idle + iowait
			return
		}
		return 0, 0
	}

	idle1, total1 := read()
	time.Sleep(200 * time.Millisecond)
	idle2, total2 := read()

	deltaTotal := total2 - total1
	deltaIdle := idle2 - idle1
	if deltaTotal == 0 {
		return 0.0
	}
	return float64(deltaTotal-deltaIdle) / float64(deltaTotal) * 100.0
}

// getRAMPercent returns RAM usage as a percentage by reading /proc/meminfo.
func getRAMPercent() float64 {
	f, err := os.Open("/proc/meminfo")
	if err != nil {
		return 0.0
	}
	defer f.Close()

	vals := make(map[string]uint64)
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		parts := strings.Fields(line)
		if len(parts) < 2 {
			continue
		}
		key := strings.TrimSuffix(parts[0], ":")
		v, _ := strconv.ParseUint(parts[1], 10, 64)
		vals[key] = v
	}

	total := vals["MemTotal"]
	available := vals["MemAvailable"]
	if total == 0 {
		return 0.0
	}
	used := total - available
	return float64(used) / float64(total) * 100.0
}
