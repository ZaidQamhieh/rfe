package server

import (
	"os"
	"strconv"
	"strings"
	"sync"
)

// CPU/RAM are read from /proc — Linux-only, zero dependencies. The
// non-Linux build (metrics_other.go) returns 0, matching how /agent/restart
// returns 501 off-Linux; the owner's agent runs Linux.

var cpuSampleMu sync.Mutex
var cpuLastIdle, cpuLastTotal uint64

// cpuPercent returns system-wide CPU utilization since the previous call,
// diffed from /proc/stat's aggregate "cpu" line. The very first call has no
// prior sample so it reports utilization since boot; every call after is the
// real interval delta (the web companion polls every 2s).
func cpuPercent() float64 {
	data, err := os.ReadFile("/proc/stat")
	if err != nil {
		return 0
	}
	line := string(data)
	if nl := strings.IndexByte(line, '\n'); nl > 0 {
		line = line[:nl]
	}
	fields := strings.Fields(line)
	if len(fields) < 5 || fields[0] != "cpu" {
		return 0
	}
	var total, idle uint64
	// fields[1:] = user nice system idle iowait irq softirq steal ...
	for i, f := range fields[1:] {
		v, err := strconv.ParseUint(f, 10, 64)
		if err != nil {
			continue
		}
		total += v
		if i == 3 || i == 4 { // idle + iowait count as not-busy
			idle += v
		}
	}

	cpuSampleMu.Lock()
	defer cpuSampleMu.Unlock()
	dTotal := total - cpuLastTotal
	dIdle := idle - cpuLastIdle
	cpuLastTotal, cpuLastIdle = total, idle
	if dTotal == 0 {
		return 0
	}
	busy := (1 - float64(dIdle)/float64(dTotal)) * 100
	if busy < 0 {
		busy = 0
	}
	return busy
}

// ramPercent returns used memory as a percentage of total, from
// /proc/meminfo (MemTotal minus MemAvailable, falling back to MemFree on the
// rare kernel that omits MemAvailable).
func ramPercent() float64 {
	data, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return 0
	}
	var memTotal, memAvail, memFree uint64
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		v, _ := strconv.ParseUint(fields[1], 10, 64)
		switch fields[0] {
		case "MemTotal:":
			memTotal = v
		case "MemAvailable:":
			memAvail = v
		case "MemFree:":
			memFree = v
		}
	}
	if memTotal == 0 {
		return 0
	}
	avail := memAvail
	if avail == 0 {
		avail = memFree
	}
	return float64(memTotal-avail) / float64(memTotal) * 100
}
