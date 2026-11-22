package system

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ZaidQamhieh/rfe/agent/internal/next/settings"
	"github.com/ZaidQamhieh/rfe/agent/internal/sidecar"
)

// fakeClock is a manually advanced clock.
type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *fakeClock) Now() time.Time { c.mu.Lock(); defer c.mu.Unlock(); return c.t }
func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	c.t = c.t.Add(d)
	c.mu.Unlock()
}

type fakeSettings struct{ s settings.Settings }

func (f fakeSettings) Get() settings.Settings { return f.s }

type fakeNet struct{ a Addresses }

func (f fakeNet) Addresses() Addresses { return f.a }

type fakeDisk struct {
	free, total uint64
	err         error
}

func (f fakeDisk) Usage() (uint64, uint64, error) { return f.free, f.total, f.err }

type fakeCounts struct {
	m   map[string]int
	err error
}

func (f fakeCounts) Counts() (map[string]int, error) { return f.m, f.err }

type fakeSidecars struct{ r sidecar.Report }

func (f fakeSidecars) Snapshot() sidecar.Report { return f.r }

// fakeHost returns scripted CPU and memory readings, one CPU reading per call.
type fakeHost struct {
	cpu    []CPUTimes
	cpuErr error
	mem    MemInfo
	memErr error
	calls  int
}

func (f *fakeHost) CPU() (CPUTimes, error) {
	if f.cpuErr != nil {
		return CPUTimes{}, f.cpuErr
	}
	i := f.calls
	if i >= len(f.cpu) {
		i = len(f.cpu) - 1
	}
	f.calls++
	return f.cpu[i], nil
}
func (f *fakeHost) Memory() (MemInfo, error) { return f.mem, f.memErr }

var t0 = time.Date(2030, 1, 2, 3, 4, 5, 0, time.UTC)

const (
	testPort        = "9911"
	testFingerprint = "ab12cd34"
	testSecretPath  = "super-secret-data-dir-name"
)

// newTestService builds a Service over fakes; tweak cfg before New via mod.
func newTestService(t *testing.T, mod func(*Config)) (*Service, *fakeClock) {
	t.Helper()
	clk := &fakeClock{t: t0}
	cfg := Config{
		Version:     "9.8.7",
		APIVersion:  4,
		Features:    []string{"zeta", "alpha"},
		GOOS:        "linux",
		GOARCH:      "amd64",
		Port:        testPort,
		Fingerprint: testFingerprint,
		Started:     t0,
		Clock:       clk,
		Settings:    fakeSettings{settings.Settings{AgentName: "den-pc", ReadOnly: true}},
		Network:     fakeNet{Addresses{LAN: "192.168.1.20", Tailscale: "100.64.0.9", MAC: "aa:bb:cc:dd:ee:ff"}},
		Disk:        fakeDisk{free: 40, total: 100},
		Counts:      fakeCounts{m: map[string]int{"devices": 3}},
		Sidecars:    fakeSidecars{sidecar.Report{At: t0, Sidecars: []sidecar.Stats{{Name: "rfe-indexd", Running: true}}, Events: map[string]uint64{"fallback": 2}}},
		Host:        &fakeHost{cpu: []CPUTimes{{Idle: 50, Total: 100}}, mem: MemInfo{Total: 1000, Available: 250}},
		Throughput:  NewThroughput(),
	}
	if mod != nil {
		mod(&cfg)
	}
	s, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return s, clk
}

func asMap(t *testing.T, v any) map[string]any {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	return m
}

func TestNewRejectsMissingRequiredSources(t *testing.T) {
	good := func() Config {
		return Config{Version: "1", APIVersion: 1, Started: t0, Settings: fakeSettings{}, Disk: fakeDisk{}}
	}
	if _, err := New(good()); err != nil {
		t.Fatalf("minimal config should be valid: %v", err)
	}
	for name, mod := range map[string]func(*Config){
		"settings":   func(c *Config) { c.Settings = nil },
		"disk":       func(c *Config) { c.Disk = nil },
		"started":    func(c *Config) { c.Started = time.Time{} },
		"apiVersion": func(c *Config) { c.APIVersion = 0 },
	} {
		c := good()
		mod(&c)
		if _, err := New(c); err == nil {
			t.Errorf("New accepted a config without %s", name)
		}
	}
}

func TestHealthUnauthenticatedIsOnlyStatus(t *testing.T) {
	s, _ := newTestService(t, nil)
	m := asMap(t, s.Health(false))
	if len(m) != 1 || m["status"] != HealthOK {
		t.Fatalf("unauthenticated health = %v, want only status=%q", m, HealthOK)
	}
	raw, _ := json.Marshal(s.Health(false))
	for _, leak := range []string{"den-pc", "192.168", "100.64", "aa:bb", "9.8.7", testFingerprint} {
		if strings.Contains(string(raw), leak) {
			t.Errorf("unauthenticated health leaks %q: %s", leak, raw)
		}
	}
}

func TestHealthAuthenticatedHasContractFields(t *testing.T) {
	s, _ := newTestService(t, nil)
	m := asMap(t, s.Health(true))
	want := map[string]any{
		"status":           HealthOK,
		"name":             "den-pc",
		"version":          "9.8.7",
		"apiVersion":       float64(4),
		"os":               "linux",
		"readOnly":         true,
		"address":          "192.168.1.20:" + testPort,
		"tailscaleAddress": "100.64.0.9:" + testPort,
		"macAddress":       "aa:bb:cc:dd:ee:ff",
	}
	for k, v := range want {
		if m[k] != v {
			t.Errorf("health[%q] = %v, want %v", k, m[k], v)
		}
	}
	feats, _ := m["features"].([]any)
	if len(feats) != 2 || feats[0] != "alpha" || feats[1] != "zeta" {
		t.Errorf("features = %v, want sorted [alpha zeta]", m["features"])
	}
	if _, ok := m["fingerprint"]; ok {
		t.Error("health must not carry the fingerprint")
	}
}

func TestHealthOmitsMACWhenUnknownAndKeepsEmptyAddresses(t *testing.T) {
	s, _ := newTestService(t, func(c *Config) { c.Network = fakeNet{} })
	m := asMap(t, s.Health(true))
	if _, ok := m["macAddress"]; ok {
		t.Error("macAddress should be absent when unknown")
	}
	if v, ok := m["address"]; !ok || v != "" {
		t.Errorf("address = %v (present=%v), want empty string present", v, ok)
	}
	if v, ok := m["tailscaleAddress"]; !ok || v != "" {
		t.Errorf("tailscaleAddress = %v (present=%v), want empty string present", v, ok)
	}
}

func TestHealthFeaturesAreCopied(t *testing.T) {
	in := []string{"b", "a"}
	s, _ := newTestService(t, func(c *Config) { c.Features = in })
	if in[0] != "b" {
		t.Fatal("New sorted the caller's slice in place")
	}
	h := s.Health(true)
	h.Features[0] = "mutated"
	if got := s.Health(true).Features[0]; got != "a" {
		t.Errorf("mutating a returned Health changed the service: %q", got)
	}
}

func TestHealthReflectsLiveSettings(t *testing.T) {
	var cur settings.Settings
	s, _ := newTestService(t, func(c *Config) { c.Settings = settingsFunc(func() settings.Settings { return cur }) })
	cur = settings.Settings{AgentName: "one"}
	if h := s.Health(true); h.Name != "one" || h.ReadOnly {
		t.Errorf("health = %+v", h)
	}
	cur = settings.Settings{AgentName: "two", ReadOnly: true}
	if h := s.Health(true); h.Name != "two" || !h.ReadOnly {
		t.Errorf("health after change = %+v", h)
	}
}

type settingsFunc func() settings.Settings

func (f settingsFunc) Get() settings.Settings { return f() }

func TestStatusFields(t *testing.T) {
	s, clk := newTestService(t, nil)
	clk.Advance(90*time.Second + 400*time.Millisecond)
	st, err := s.Status()
	if err != nil {
		t.Fatal(err)
	}
	m := asMap(t, st)
	want := map[string]any{
		"name":             "den-pc",
		"version":          "9.8.7",
		"apiVersion":       float64(4),
		"uptimeSeconds":    float64(90),
		"platform":         "linux/amd64",
		"freeBytes":        float64(40),
		"totalBytes":       float64(100),
		"address":          "192.168.1.20:" + testPort,
		"tailscaleAddress": "100.64.0.9:" + testPort,
		"macAddress":       "aa:bb:cc:dd:ee:ff",
		"fingerprint":      testFingerprint,
	}
	for k, v := range want {
		if m[k] != v {
			t.Errorf("status[%q] = %v, want %v", k, m[k], v)
		}
	}
	counts, _ := m["counts"].(map[string]any)
	if counts["devices"] != float64(3) {
		t.Errorf("counts = %v", m["counts"])
	}
	sc, _ := m["sidecars"].(map[string]any)
	list, _ := sc["sidecars"].([]any)
	events, _ := sc["events"].(map[string]any)
	if len(list) != 1 || events["fallback"] != float64(2) {
		t.Errorf("sidecars = %v", m["sidecars"])
	}
}

func TestStatusUptimeNeverNegative(t *testing.T) {
	s, clk := newTestService(t, nil)
	clk.Advance(-time.Hour)
	st, err := s.Status()
	if err != nil {
		t.Fatal(err)
	}
	if st.UptimeSeconds != 0 {
		t.Errorf("uptime = %d, want 0 when the clock is before start", st.UptimeSeconds)
	}
}

func TestStatusDoesNotLeakPathsOrSecrets(t *testing.T) {
	dir := filepath.Join(t.TempDir(), testSecretPath)
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	s, _ := newTestService(t, func(c *Config) { c.Disk = DiskAt(dir) })
	st, err := s.Status()
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(st)
	if strings.Contains(string(raw), testSecretPath) || strings.Contains(string(raw), dir) {
		t.Errorf("status leaks a host path: %s", raw)
	}
	if st.TotalBytes == 0 {
		t.Error("real disk usage should report a non-zero total")
	}
}

func TestStatusOptionalSourcesMayBeAbsent(t *testing.T) {
	s, _ := newTestService(t, func(c *Config) { c.Counts = nil; c.Sidecars = nil; c.Network = nil })
	st, err := s.Status()
	if err != nil {
		t.Fatal(err)
	}
	m := asMap(t, st)
	for _, k := range []string{"counts", "sidecars"} {
		if _, ok := m[k]; ok {
			t.Errorf("%s should be omitted when no source is configured", k)
		}
	}
}

func TestStatusSurfacesSourceErrors(t *testing.T) {
	boom := errors.New("boom")
	cases := map[string]func(*Config){
		"disk":   func(c *Config) { c.Disk = fakeDisk{err: boom} },
		"counts": func(c *Config) { c.Counts = fakeCounts{err: boom} },
	}
	for name, mod := range cases {
		s, _ := newTestService(t, mod)
		if _, err := s.Status(); !errors.Is(err, boom) {
			t.Errorf("%s: err = %v, want wrapped boom", name, err)
		}
	}
}

func TestMetricsCountersAndTimestamp(t *testing.T) {
	tp := NewThroughput()
	tp.AddRx(1500)
	tp.AddTx(700)
	s, clk := newTestService(t, func(c *Config) { c.Throughput = tp })
	clk.Advance(2 * time.Second)
	m := s.Metrics()
	if m.RxBytes != 1500 || m.TxBytes != 700 {
		t.Errorf("rx/tx = %d/%d", m.RxBytes, m.TxBytes)
	}
	if want := t0.Add(2 * time.Second).UnixMilli(); m.TsMs != want {
		t.Errorf("tsMs = %d, want %d", m.TsMs, want)
	}
	if m.RAMPercent != 75 {
		t.Errorf("ramPercent = %v, want 75", m.RAMPercent)
	}
	tp.AddRx(500)
	if got := s.Metrics().RxBytes; got != 2000 {
		t.Errorf("rx after more traffic = %d, want 2000 (cumulative)", got)
	}
	keys := asMap(t, m)
	for _, k := range []string{"rxBytes", "txBytes", "cpuPercent", "ramPercent", "tsMs"} {
		if _, ok := keys[k]; !ok {
			t.Errorf("metrics missing %q", k)
		}
	}
}

func TestMetricsCPUIsIntervalDelta(t *testing.T) {
	host := &fakeHost{cpu: []CPUTimes{
		{Idle: 50, Total: 100},  // first call: since boot, 50% busy
		{Idle: 60, Total: 200},  // delta idle 10 of 100 -> 90% busy
		{Idle: 60, Total: 200},  // no time passed -> 0
		{Idle: 160, Total: 300}, // all idle -> 0
		{Idle: 100, Total: 400}, // idle went backwards (counter reset): since-boot figure, 75% busy
	}, mem: MemInfo{Total: 1, Available: 1}}
	s, _ := newTestService(t, func(c *Config) { c.Host = host })
	want := []float64{50, 90, 0, 0, 75}
	for i, w := range want {
		if got := s.Metrics().CPUPercent; got != w {
			t.Errorf("sample %d: cpu = %v, want %v", i, got, w)
		}
	}
}

func TestMetricsUnsupportedHostReportsZeroQuietly(t *testing.T) {
	var logged []string
	host := &fakeHost{cpuErr: ErrUnsupported, memErr: ErrUnsupported}
	s, _ := newTestService(t, func(c *Config) {
		c.Host = host
		c.Logf = func(f string, a ...any) { logged = append(logged, f) }
	})
	m := s.Metrics()
	if m.CPUPercent != 0 || m.RAMPercent != 0 {
		t.Errorf("cpu/ram = %v/%v, want 0/0", m.CPUPercent, m.RAMPercent)
	}
	if len(logged) != 0 {
		t.Errorf("unsupported platform should not log: %v", logged)
	}
}

func TestMetricsRealSamplerFailureIsLogged(t *testing.T) {
	var logged int
	host := &fakeHost{cpuErr: errors.New("read failed"), memErr: errors.New("read failed")}
	s, _ := newTestService(t, func(c *Config) {
		c.Host = host
		c.Logf = func(string, ...any) { logged++ }
	})
	m := s.Metrics()
	if m.CPUPercent != 0 || m.RAMPercent != 0 || logged != 2 {
		t.Errorf("metrics = %+v, logged = %d, want zeros and 2 log lines", m, logged)
	}
}

func TestMetricsMemoryEdgeCases(t *testing.T) {
	cases := []struct {
		name string
		mem  MemInfo
		want float64
	}{
		{"zero total", MemInfo{}, 0},
		{"available above total is clamped", MemInfo{Total: 100, Available: 200}, 0},
		{"all used", MemInfo{Total: 100, Available: 0}, 100},
	}
	for _, c := range cases {
		s, _ := newTestService(t, func(cfg *Config) { cfg.Host = &fakeHost{cpu: []CPUTimes{{}}, mem: c.mem} })
		if got := s.Metrics().RAMPercent; got != c.want {
			t.Errorf("%s: ram = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestMetricsConcurrentUse(t *testing.T) {
	tp := NewThroughput()
	host := &lockedHost{}
	s, _ := newTestService(t, func(c *Config) { c.Throughput = tp; c.Host = host })
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				tp.AddRx(1)
				_ = s.Metrics()
			}
		}()
	}
	wg.Wait()
	if got := s.Metrics().RxBytes; got != 400 {
		t.Errorf("rx = %d, want 400", got)
	}
}

// lockedHost is a host sampler safe for concurrent calls.
type lockedHost struct {
	mu sync.Mutex
	n  uint64
}

func (h *lockedHost) CPU() (CPUTimes, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.n++
	return CPUTimes{Idle: h.n, Total: h.n * 2}, nil
}
func (h *lockedHost) Memory() (MemInfo, error) { return MemInfo{Total: 10, Available: 5}, nil }

func TestProcessSidecarsAdapterSeesEvents(t *testing.T) {
	sidecar.ResetEventsForTest()
	t.Cleanup(sidecar.ResetEventsForTest)
	sidecar.Note("system-test-event")
	r := ProcessSidecars{}.Snapshot()
	if r.Events["system-test-event"] != 1 {
		t.Errorf("events = %v", r.Events)
	}
}

func TestNetworkJoinsPortAndSkipsEmptyHost(t *testing.T) {
	if got := joinAddress("", testPort); got != "" {
		t.Errorf("empty host -> %q, want empty", got)
	}
	if got := joinAddress("10.0.0.1", ""); got != "10.0.0.1" {
		t.Errorf("no port -> %q, want bare host", got)
	}
	if got := joinAddress("fe80::1", testPort); got != "[fe80::1]:"+testPort {
		t.Errorf("ipv6 -> %q", got)
	}
}
