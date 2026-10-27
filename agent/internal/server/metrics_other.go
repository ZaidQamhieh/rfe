//go:build !linux

package server

// cpuPercent/ramPercent read /proc, which only exists on Linux. Elsewhere the
// web companion's CPU/RAM cards show 0 — same posture as /agent/restart's 501
// off-Linux. The owner's agent runs Linux.
func cpuPercent() float64 { return 0 }
func ramPercent() float64 { return 0 }
