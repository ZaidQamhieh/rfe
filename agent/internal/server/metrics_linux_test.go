package server

import "testing"

// Sanity-checks the /proc parsers against this machine's real /proc. Not a
// mock — if the parsing breaks, these bounds fail on real data.
func TestRAMPercentInRange(t *testing.T) {
	p := ramPercent()
	if p <= 0 || p > 100 {
		t.Fatalf("ramPercent()=%v, want (0,100]", p)
	}
}

func TestCPUPercentNonNegative(t *testing.T) {
	cpuPercent() // seed the first sample (since-boot)
	p := cpuPercent()
	if p < 0 || p > 100 {
		t.Fatalf("cpuPercent()=%v, want [0,100]", p)
	}
}
