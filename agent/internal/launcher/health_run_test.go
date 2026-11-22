package launcher

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

const (
	fpA = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	fpB = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
)

// fakeClock is a Clock whose time only moves when a test calls Step.
type fakeClock struct {
	mu      sync.Mutex
	now     time.Time
	waiters []fakeWaiter
}

type fakeWaiter struct {
	at time.Time
	ch chan time.Time
}

func newFakeClock() *fakeClock { return &fakeClock{now: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)} }

func (c *fakeClock) Now() time.Time { c.mu.Lock(); defer c.mu.Unlock(); return c.now }

func (c *fakeClock) After(d time.Duration) <-chan time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	w := fakeWaiter{at: c.now.Add(d), ch: make(chan time.Time, 1)}
	c.waiters = append(c.waiters, w)
	return w.ch
}

// Step waits until the watcher is waiting on the clock, moves time forward by d, wakes what is due, and then
// gives the child a few real milliseconds to send its heartbeats.
func (c *fakeClock) Step(d time.Duration) {
	for i := 0; i < 400; i++ {
		c.mu.Lock()
		n := len(c.waiters)
		c.mu.Unlock()
		if n > 0 {
			break
		}
		time.Sleep(time.Millisecond)
	}
	c.mu.Lock()
	c.now = c.now.Add(d)
	keep := c.waiters[:0]
	for _, w := range c.waiters {
		if !w.at.After(c.now) {
			w.ch <- c.now
		} else {
			keep = append(keep, w)
		}
	}
	c.waiters = keep
	c.mu.Unlock()
	time.Sleep(10 * time.Millisecond)
}

// fakeData is a DataTool that records what the launcher asked of it.
type fakeData struct {
	mu       sync.Mutex
	info     SchemaInfo
	schemaOf []string
	restores []string
}

func (f *fakeData) Schema(_ context.Context, agentDir, _ string) (SchemaInfo, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.schemaOf = append(f.schemaOf, agentDir)
	return f.info, nil
}

func (f *fakeData) RestoreSnapshot(_ context.Context, agentDir, _ string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.restores = append(f.restores, agentDir)
	return nil
}

// probeVar is a fingerprint the test can change while the launcher runs.
type probeVar struct {
	mu sync.Mutex
	fp string
}

func (p *probeVar) set(fp string) { p.mu.Lock(); p.fp = fp; p.mu.Unlock() }
func (p *probeVar) get(string) (string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.fp, nil
}

const driveStep = 5 * time.Second

// trialCfg is the Config of a watched run: a fake clock, a fake data tool and a probe the test controls.
func (r *rig) trialCfg(clk *fakeClock, data DataTool, probe *probeVar, sigs <-chan os.Signal) Config {
	return Config{Root: r.root, Env: r.env, Signals: sigs, Logf: func(f string, a ...any) { r.t.Logf(f, a...) },
		Clock: clk, PollInterval: driveStep, ProbeInterval: driveStep, HealthGap: 30 * time.Second,
		ProbeAddr: "127.0.0.1:1", ProbeFingerprint: probe.get, DataDir: r.t.TempDir(), Data: data}
}

type runResult struct {
	code int
	err  error
	kill any // a simulated kill (panic value), if one happened
}

// drive runs the launcher in the background and steps the fake clock until it returns; each is called with the
// elapsed fake time before every step.
func (r *rig) drive(cfg Config, clk *fakeClock, each func(elapsed time.Duration)) runResult {
	r.t.Helper()
	done := make(chan runResult, 1)
	go func() {
		var res runResult
		defer func() { res.kill = recover(); done <- res }()
		res.code, res.err = Run(cfg)
	}()
	start := clk.Now()
	for i := 0; i < 400; i++ {
		select {
		case res := <-done:
			return res
		default:
		}
		if each != nil {
			each(clk.Now().Sub(start))
		}
		clk.Step(driveStep)
	}
	r.t.Fatalf("launcher did not return after %d fake steps", 400)
	return runResult{}
}

func TestNewVersionExitingDuringMigrationIsRolledBackWithinTheAttempt(t *testing.T) {
	r := newRig(t, "v1=exit:0,v2=migrexit:3", "v1", "v2")
	r.setState(State{Current: "v1", Pending: "v2", ExpectedCertFingerprint: fpA})
	clk, data, probe := newFakeClock(), &fakeData{info: SchemaInfo{MinCompatible: 0, SupportedLevel: 1}}, &probeVar{fp: fpA}
	if res := r.drive(r.trialCfg(clk, data, probe, nil), clk, nil); res.err != nil || res.code != 3 {
		t.Fatalf("first attempt: %+v", res)
	}
	if s := r.state(); s.Pending != "v2" || s.Attempts["v2"] != 1 {
		t.Fatalf("after attempt 1: %+v", s)
	}
	if res := r.drive(r.trialCfg(clk, data, probe, nil), clk, nil); res.err != nil || res.code != 0 {
		t.Fatalf("second attempt: %+v", res)
	}
	s := r.state()
	if s.Current != "v1" || s.Pending != "" || !s.IsBad("v2") || r.started() != "v2 v2 v1" {
		t.Fatalf("state %+v started %q", s, r.started())
	}
}

func TestSilentNewVersionIsFailedAfterStartTimeout(t *testing.T) {
	r := newRig(t, "v1=exit:0,v2=wait", "v1", "v2")
	r.setState(State{Current: "v1", Pending: "v2", ExpectedCertFingerprint: fpA})
	clk, data, probe := newFakeClock(), &fakeData{}, &probeVar{fp: fpA}
	var killedAt time.Duration
	res := r.drive(r.trialCfg(clk, data, probe, nil), clk, func(e time.Duration) { killedAt = e })
	if res.err != nil || res.code == 0 {
		t.Fatalf("%+v", res)
	}
	if killedAt < 115*time.Second || killedAt > 135*time.Second {
		t.Fatalf("a silent start must fail at about 120 s of fake time, failed at %s", killedAt)
	}
	if s := r.state(); s.Attempts["v2"] != 1 || s.Pending != "v2" {
		t.Fatalf("%+v", s)
	}
}

func TestMigratingHeartbeatsKeepTheStartTimeoutAtBay(t *testing.T) {
	r := newRig(t, "v1=exit:0,v2=healthy", "v1", "v2")
	r.setState(State{Current: "v1", Pending: "v2", ExpectedCertFingerprint: fpA})
	clk, probe := newFakeClock(), &probeVar{fp: fpA}
	sigs := make(chan os.Signal, 1)
	res := r.drive(r.trialCfg(clk, &fakeData{}, probe, sigs), clk, func(e time.Duration) {
		if e >= 2*time.Minute && len(sigs) == 0 {
			sigs <- syscall.SIGTERM // two minutes in, still on trial and alive: stop the launcher
		}
	})
	if res.err != nil {
		t.Fatal(res.err)
	}
	if s := r.state(); s.Pending != "v2" || s.Attempts["v2"] != 0 {
		t.Fatalf("a healthy version must not be failed or promoted early: %+v", s)
	}
}

func TestCrashDuringProbationIsRolledBack(t *testing.T) {
	r := newRig(t, "v1=exit:0,v2=healthy", "v1", "v2")
	r.setState(State{Current: "v1", Pending: "v2", ExpectedCertFingerprint: fpA})
	clk, probe := newFakeClock(), &probeVar{fp: fpA}
	res := r.drive(r.trialCfg(clk, &fakeData{}, probe, nil), clk, func(e time.Duration) {
		if e >= 3*time.Minute {
			os.WriteFile(r.log+".crash", nil, 0o600)
		}
	})
	if res.err != nil || res.code != 9 {
		t.Fatalf("%+v", res)
	}
	if s := r.state(); s.Current != "v1" || s.Pending != "v2" || s.Attempts["v2"] != 1 {
		t.Fatalf("crash at minute 3: %+v", s)
	}
}

func TestCertificateFingerprintChangeIsRolledBack(t *testing.T) {
	r := newRig(t, "v1=exit:0,v2=healthy", "v1", "v2")
	r.setState(State{Current: "v1", Pending: "v2", ExpectedCertFingerprint: fpA})
	clk, probe := newFakeClock(), &probeVar{fp: fpA}
	start := time.Now()
	res := r.drive(r.trialCfg(clk, &fakeData{}, probe, nil), clk, func(e time.Duration) {
		if e >= time.Minute {
			probe.set(fpB)
		}
	})
	if res.err != nil || res.code == 0 || time.Since(start) > 30*time.Second {
		t.Fatalf("%+v", res)
	}
	if s := r.state(); s.Attempts["v2"] != 1 || s.Current != "v1" || s.ExpectedCertFingerprint != fpA {
		t.Fatalf("the recorded fingerprint must not change: %+v", s)
	}
}

func TestHealthGapDuringProbationIsRolledBack(t *testing.T) {
	r := newRig(t, "v1=exit:0,v2=ready-once", "v1", "v2")
	r.setState(State{Current: "v1", Pending: "v2", ExpectedCertFingerprint: fpA})
	clk, probe := newFakeClock(), &probeVar{fp: fpA}
	res := r.drive(r.trialCfg(clk, &fakeData{}, probe, nil), clk, nil)
	if res.err != nil || res.code == 0 {
		t.Fatalf("%+v", res)
	}
	if s := r.state(); s.Attempts["v2"] != 1 {
		t.Fatalf("%+v", s)
	}
}

func TestHealthyVersionIsPromotedAndFingerprintRecorded(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows has no SIGTERM to hand the launcher: a stop kills the agent (exit 1)")
	}
	r := newRig(t, "v1=exit:0,v2=healthy", "v1", "v2")
	r.setState(State{Current: "v1", Pending: "v2"}) // no fingerprint recorded yet
	clk, probe := newFakeClock(), &probeVar{fp: fpA}
	sigs := make(chan os.Signal, 1)
	var promotedAt time.Duration
	res := r.drive(r.trialCfg(clk, &fakeData{}, probe, sigs), clk, func(e time.Duration) {
		if s, err := r.store.Read(); err == nil && s.Current == "v2" && len(sigs) == 0 {
			promotedAt = e
			sigs <- syscall.SIGTERM
		}
	})
	if res.err != nil || res.code != 0 {
		t.Fatalf("%+v", res)
	}
	s := r.state()
	if s.Current != "v2" || s.Previous != "v1" || s.Pending != "" || s.ExpectedCertFingerprint != fpA {
		t.Fatalf("state %+v", s)
	}
	if promotedAt < 10*time.Minute || promotedAt > 11*time.Minute {
		t.Fatalf("promotion must come after the 10 minute probation, came at %s", promotedAt)
	}
}

func TestSecondFailureMarksBadAndRollbackDependsOnSchema(t *testing.T) {
	for _, tc := range []struct {
		name     string
		info     SchemaInfo
		restores int
	}{
		{"additive migration only flips the pointer", SchemaInfo{UserVersion: 5, MinCompatible: 1, SupportedLevel: 3}, 0},
		{"breaking migration restores the snapshot", SchemaInfo{UserVersion: 5, MinCompatible: 5, SupportedLevel: 3}, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newRig(t, "v1=exit:0,v2=migrexit:3", "v1", "v2")
			r.setState(State{Current: "v1", Pending: "v2", ExpectedCertFingerprint: fpA})
			clk, probe, data := newFakeClock(), &probeVar{fp: fpA}, &fakeData{info: tc.info}
			r.drive(r.trialCfg(clk, data, probe, nil), clk, nil)
			if len(data.schemaOf) != 0 || len(data.restores) != 0 {
				t.Fatalf("the first failed attempt must not touch the database: %v %v", data.schemaOf, data.restores)
			}
			r.drive(r.trialCfg(clk, data, probe, nil), clk, nil)
			if len(data.restores) != tc.restores || !strings.HasSuffix(data.schemaOf[0], "v1") {
				t.Fatalf("schema asked of %v, restores %v", data.schemaOf, data.restores)
			}
			if tc.restores == 1 && !strings.HasSuffix(data.restores[0], "v1") {
				t.Fatalf("the snapshot must be restored by the previous version: %v", data.restores)
			}
			if s := r.state(); !s.IsBad("v2") || s.Current != "v1" {
				t.Fatalf("%+v", s)
			}
		})
	}
}

// failingData fails its restore, as a broken rollback would.
type failingData struct{ fakeData }

func (f *failingData) RestoreSnapshot(context.Context, string, string) error {
	return errors.New("disk full")
}

func TestFailedDatabaseRollbackLeavesVersionPendingAndStartsNothingElse(t *testing.T) {
	r := newRig(t, "v1=exit:0,v2=migrexit:3", "v1", "v2")
	r.setState(State{Current: "v1", Pending: "v2", Attempts: map[string]int{"v2": 1}, ExpectedCertFingerprint: fpA})
	clk, probe := newFakeClock(), &probeVar{fp: fpA}
	data := &failingData{fakeData{info: SchemaInfo{MinCompatible: 9, SupportedLevel: 3}}}
	res := r.drive(r.trialCfg(clk, data, probe, nil), clk, nil)
	if res.err == nil || res.code != ExitLauncherError || r.started() != "v2" {
		t.Fatalf("%+v started %q", res, r.started())
	}
	if s := r.state(); s.IsBad("v2") || s.Pending != "v2" {
		t.Fatalf("the rollback must be retried next launch: %+v", s)
	}
}

// TestKillAtEveryStateWriteStepRecovers kills the launcher at each durable step of its state writes (the Store hook
// panics) and then lets a fresh launcher finish: state.json must always be readable and end on a good version.
func TestKillAtEveryStateWriteStepRecovers(t *testing.T) {
	scenarios := []struct {
		name, behavior string
		start          State
		probation      time.Duration
		check          func(t *testing.T, s State)
	}{
		{"failing pending is rolled back", "v1=exit:0,v2=migrexit:3", State{Current: "v1", Pending: "v2", ExpectedCertFingerprint: fpA}, 0,
			func(t *testing.T, s State) {
				if s.Current != "v1" || !s.IsBad("v2") {
					t.Fatalf("want v1 current and v2 bad, got %+v", s)
				}
			}},
		{"healthy pending is promoted", "v1=exit:0,v2=healthy", State{Current: "v1", Pending: "v2", ExpectedCertFingerprint: fpA}, 20 * time.Second,
			func(t *testing.T, s State) {
				if s.Current != "v2" || s.Pending != "" || s.IsBad("v2") {
					t.Fatalf("want v2 promoted, got %+v", s)
				}
			}},
	}
	for _, sc := range scenarios {
		t.Run(sc.name, func(t *testing.T) {
			// session runs launchers until one ends cleanly; kill, when >0, panics at that numbered write step.
			session := func(kill int) (steps int, killed bool, r *rig) {
				r = newRig(t, sc.behavior, "v1", "v2")
				r.setState(sc.start)
				clk, probe := newFakeClock(), &probeVar{fp: fpA}
				hook := func(string) error {
					steps++
					if steps == kill {
						panic("simulated kill")
					}
					return nil
				}
				for launches := 0; launches < 6; launches++ {
					sigs := make(chan os.Signal, 1)
					cfg := r.trialCfg(clk, &fakeData{info: SchemaInfo{SupportedLevel: 9}}, probe, sigs)
					cfg.Probation, cfg.storeHook = sc.probation, hook
					res := r.drive(cfg, clk, func(time.Duration) {
						if s, err := r.store.Read(); err == nil && s.Current == "v2" && len(sigs) == 0 {
							sigs <- syscall.SIGTERM
						}
					})
					if res.kill != nil {
						killed = true
						if _, err := r.store.Read(); err != nil {
							t.Fatalf("kill at step %d left an unreadable state: %v", kill, err)
						}
						continue
					}
					if res.err != nil {
						t.Fatalf("kill %d launch %d: %v", kill, launches, res.err)
					}
					if res.code == 0 && (sc.probation == 0 || r.state().Current == "v2") {
						break
					}
				}
				return steps, killed, r
			}
			total, _, r := session(0)
			sc.check(t, r.state())
			if total < 4 {
				t.Fatalf("expected several write steps, counted %d", total)
			}
			for k := 1; k <= total; k++ {
				_, killed, r := session(k)
				if !killed {
					t.Fatalf("step %d never fired", k)
				}
				sc.check(t, r.state())
			}
		})
	}
}

func TestExecDataToolRunsTheAgentSubcommands(t *testing.T) {
	r := newRig(t, "", "v1")
	tool := ExecDataTool{Env: append(r.env, "RFE_TEST_SCHEMA_JSON={\"user_version\":4,\"min_compatible\":3,\"supported_level\":2}")}
	dir := r.root + "/versions/v1"
	info, err := tool.Schema(context.Background(), dir, "/data")
	if err != nil || info != (SchemaInfo{UserVersion: 4, MinCompatible: 3, SupportedLevel: 2}) || info.CanOpen() {
		t.Fatalf("%+v %v", info, err)
	}
	if err := tool.RestoreSnapshot(context.Background(), dir, "/data"); err != nil {
		t.Fatal(err)
	}
	if got := r.started(); got != "subcommand schema-info subcommand rollback-data" {
		t.Fatalf("log %q", got)
	}
	if _, err := (ExecDataTool{Env: r.env}).Schema(context.Background(), dir, "/data"); err == nil {
		t.Fatal("output that is not JSON must be an error")
	}
}

func TestTLSFingerprintMatchesServedCertificate(t *testing.T) {
	srv := httptest.NewTLSServer(http.NotFoundHandler())
	defer srv.Close()
	sum := sha256.Sum256(srv.Certificate().Raw)
	got, err := TLSFingerprint(srv.Listener.Addr().String())
	if err != nil || got != hex.EncodeToString(sum[:]) {
		t.Fatalf("%q %v", got, err)
	}
	if _, err := TLSFingerprint("127.0.0.1:1"); err == nil {
		t.Fatal("a closed port must be an error")
	}
	_ = tls.VersionTLS12
}

func TestProbeTarget(t *testing.T) {
	for _, c := range []struct{ cfg, reported, want string }{
		{"", "", ""}, {"", ":8765", "127.0.0.1:8765"}, {"", "0.0.0.0:8765", "127.0.0.1:8765"}, {"", "[::]:9", "127.0.0.1:9"},
		{"10.0.0.2:1", ":8765", "10.0.0.2:1"}, {"", "garbage", ""},
	} {
		if got := probeTarget(c.cfg, c.reported); got != c.want {
			t.Errorf("probeTarget(%q,%q) = %q, want %q", c.cfg, c.reported, got, c.want)
		}
	}
}

func TestNewTransitionsSetFingerprintAndDowngrade(t *testing.T) {
	at := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	s := State{Current: "v2", Previous: "v1"}
	n, err := Next(s, Event{Kind: EvSetFingerprint, Version: fpA, At: at})
	if err != nil || n.ExpectedCertFingerprint != fpA {
		t.Fatalf("%+v %v", n, err)
	}
	if _, err := Next(n, Event{Kind: EvSetFingerprint, Version: fpB, At: at}); !errors.Is(err, ErrTransition) {
		t.Fatalf("a recorded fingerprint must never be replaced: %v", err)
	}
	if _, err := Next(s, Event{Kind: EvSetFingerprint, Version: "zz", At: at}); !errors.Is(err, ErrTransition) {
		t.Fatalf("%v", err)
	}
	d, err := Next(State{Current: "v2", Previous: "v1", Pending: "v3"}, Event{Kind: EvDowngrade, Version: "v1", At: at})
	if err != nil || d.Current != "v1" || d.Previous != "v2" || d.Pending != "" {
		t.Fatalf("%+v %v", d, err)
	}
	for _, bad := range []State{{Current: "v1"}, {Current: "v2", Bad: []string{"v1"}}, {}} {
		if _, err := Next(bad, Event{Kind: EvDowngrade, Version: "v1", At: at}); !errors.Is(err, ErrTransition) {
			t.Errorf("%+v: %v", bad, err)
		}
	}
}
