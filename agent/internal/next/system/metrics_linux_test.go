package system

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

const sampleStat = `cpu  100 5 50 800 40 3 2 0 0 0
cpu0 50 2 25 400 20 1 1 0 0 0
intr 12345
`

const sampleMeminfo = `MemTotal:       16000000 kB
MemFree:         1000000 kB
MemAvailable:    4000000 kB
Buffers:          100000 kB
`

func TestParseCPUStat(t *testing.T) {
	got, err := parseCPUStat([]byte(sampleStat))
	if err != nil {
		t.Fatal(err)
	}
	// total = 100+5+50+800+40+3+2+0+0+0 = 1000; idle = idle(800)+iowait(40).
	if got.Total != 1000 || got.Idle != 840 {
		t.Errorf("got %+v, want Total=1000 Idle=840", got)
	}
}

func TestParseCPUStatRejectsBadInput(t *testing.T) {
	for name, in := range map[string]string{
		"empty":       "",
		"wrong label": "cpu0 1 2 3 4 5\n",
		"too short":   "cpu 1 2 3\n",
		"not numeric": "cpu 1 2 x 4 5\n",
	} {
		if _, err := parseCPUStat([]byte(in)); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
}

func TestParseMemInfo(t *testing.T) {
	got, err := parseMemInfo([]byte(sampleMeminfo))
	if err != nil {
		t.Fatal(err)
	}
	if got.Total != 16000000*1024 || got.Available != 4000000*1024 {
		t.Errorf("got %+v", got)
	}
}

func TestParseMemInfoFallsBackToMemFree(t *testing.T) {
	got, err := parseMemInfo([]byte("MemTotal: 100 kB\nMemFree: 30 kB\n"))
	if err != nil {
		t.Fatal(err)
	}
	if got.Available != 30*1024 {
		t.Errorf("available = %d, want MemFree fallback", got.Available)
	}
}

func TestParseMemInfoRejectsMissingTotal(t *testing.T) {
	if _, err := parseMemInfo([]byte("MemFree: 30 kB\n")); err == nil {
		t.Error("expected an error without MemTotal")
	}
}

func TestProcSamplerReadsFilesUnderRoot(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "stat"), []byte(sampleStat), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "meminfo"), []byte(sampleMeminfo), 0o600); err != nil {
		t.Fatal(err)
	}
	s := newProcSampler(root)
	c, err := s.CPU()
	if err != nil || c.Total != 1000 {
		t.Errorf("CPU = %+v, %v", c, err)
	}
	m, err := s.Memory()
	if err != nil || m.Total == 0 {
		t.Errorf("Memory = %+v, %v", m, err)
	}
}

func TestProcSamplerMissingFilesReportError(t *testing.T) {
	s := newProcSampler(t.TempDir())
	if _, err := s.CPU(); err == nil || errors.Is(err, ErrUnsupported) {
		t.Errorf("CPU err = %v, want a real read error", err)
	}
	if _, err := s.Memory(); err == nil || errors.Is(err, ErrUnsupported) {
		t.Errorf("Memory err = %v, want a real read error", err)
	}
}

func TestDefaultHostSamplerOnLinuxReadsRealProc(t *testing.T) {
	h := NewHostSampler()
	if _, err := h.Memory(); err != nil {
		t.Skipf("no readable /proc in this environment: %v", err)
	}
	m, _ := h.Memory()
	if m.Total == 0 {
		t.Error("real MemTotal should be non-zero")
	}
}
