package system

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const (
	// defaultProcRoot is where Linux exposes kernel statistics.
	defaultProcRoot = "/proc"
	procStatFile    = "stat"
	procMeminfoFile = "meminfo"

	cpuLabel = "cpu"
	// In the aggregate "cpu" line of /proc/stat the counters after the label
	// are: user nice system idle iowait irq softirq steal ...
	cpuIdleIndex   = 3
	cpuIOWaitIndex = 4
	cpuMinFields   = 5 // label plus the first four counters (through idle)

	memTotalKey     = "MemTotal:"
	memAvailableKey = "MemAvailable:"
	memFreeKey      = "MemFree:"
	// /proc/meminfo reports kibibytes.
	kibibyte = 1024
)

// procSampler reads CPU and memory figures from a /proc directory.
type procSampler struct{ root string }

// NewHostSampler returns the sampler for this platform: Linux /proc.
func NewHostSampler() HostSampler { return newProcSampler(defaultProcRoot) }

// newProcSampler returns a sampler reading stat and meminfo under root, so
// tests can point it at a temp directory.
func newProcSampler(root string) procSampler { return procSampler{root: root} }

// CPU returns the cumulative CPU counters from the aggregate line of stat.
func (p procSampler) CPU() (CPUTimes, error) {
	b, err := os.ReadFile(filepath.Join(p.root, procStatFile))
	if err != nil {
		return CPUTimes{}, err
	}
	return parseCPUStat(b)
}

// Memory returns total and available memory from meminfo.
func (p procSampler) Memory() (MemInfo, error) {
	b, err := os.ReadFile(filepath.Join(p.root, procMeminfoFile))
	if err != nil {
		return MemInfo{}, err
	}
	return parseMemInfo(b)
}

// parseCPUStat reads the first line of /proc/stat ("cpu user nice system idle
// iowait ...") and sums every counter into Total; idle plus iowait count as idle.
func parseCPUStat(data []byte) (CPUTimes, error) {
	line, _, _ := bytes.Cut(data, []byte{'\n'})
	fields := strings.Fields(string(line))
	if len(fields) < cpuMinFields || fields[0] != cpuLabel {
		return CPUTimes{}, fmt.Errorf("system: unexpected cpu line %q", line)
	}
	var t CPUTimes
	for i, f := range fields[1:] {
		v, err := strconv.ParseUint(f, 10, 64)
		if err != nil {
			return CPUTimes{}, fmt.Errorf("system: cpu counter %q: %w", f, err)
		}
		t.Total += v
		if i == cpuIdleIndex || i == cpuIOWaitIndex {
			t.Idle += v
		}
	}
	return t, nil
}

// parseMemInfo reads MemTotal and MemAvailable (kB) from /proc/meminfo,
// falling back to MemFree on a kernel that omits MemAvailable.
func parseMemInfo(data []byte) (MemInfo, error) {
	var total, avail, free uint64
	var haveTotal, haveAvail bool
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		v, err := strconv.ParseUint(fields[1], 10, 64)
		if err != nil {
			continue
		}
		switch fields[0] {
		case memTotalKey:
			total, haveTotal = v*kibibyte, true
		case memAvailableKey:
			avail, haveAvail = v*kibibyte, true
		case memFreeKey:
			free = v * kibibyte
		}
	}
	if !haveTotal {
		return MemInfo{}, fmt.Errorf("system: %s missing from meminfo", memTotalKey)
	}
	if !haveAvail {
		avail = free
	}
	return MemInfo{Total: total, Available: avail}, nil
}
