package launcher

import (
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/ZaidQamhieh/rfe/agent/internal/bundle"
	"github.com/ZaidQamhieh/rfe/agent/internal/upgrade/health"
)

const (
	fakeAgentEnv    = "RFE_TEST_FAKE_AGENT"
	fakeBehaviorEnv = "RFE_TEST_FAKE_BEHAVIOR"
	fakeLogEnv      = "RFE_TEST_FAKE_LOG"
)

// TestMain lets the tests start this very binary as the agent of a bundle (see fakeAgentMain).
func TestMain(m *testing.M) {
	if os.Getenv(fakeAgentEnv) != "" {
		fakeAgentMain()
		return
	}
	os.Exit(m.Run())
}

// fakeAgentMain behaves as the bundle version in RFE_BUNDLE_VERSION is told to by RFE_TEST_FAKE_BEHAVIOR, a
// comma list like "v1=pend:v2,v2=exit:3". It first appends the version to the log file. exit:N exits with N;
// pend:V stages V as pending in state.json and exits 75; wait blocks until SIGTERM, logs "term" and exits 0.
func fakeAgentMain() {
	version := os.Getenv(VersionEnv)
	behavior := "exit:0"
	for _, kv := range strings.Split(os.Getenv(fakeBehaviorEnv), ",") {
		if k, v, ok := strings.Cut(kv, "="); ok && k == version {
			behavior = v
		}
	}
	logLine := func(s string) {
		f, err := os.OpenFile(os.Getenv(fakeLogEnv), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
		if err == nil {
			f.WriteString(s + "\n")
			f.Close()
		}
	}
	if len(os.Args) > 1 && (os.Args[1] == "schema-info" || os.Args[1] == "rollback-data") {
		logLine("subcommand " + os.Args[1])
		fmt.Print(os.Getenv("RFE_TEST_SCHEMA_JSON"))
		os.Exit(0)
	}
	kind, arg, _ := strings.Cut(behavior, ":")
	switch kind {
	case "healthy", "ready-once", "migrexit":
		// healthy: migrating, then ready, then a ready beat every 2 ms until SIGTERM (exit 0) or until the file
		// <log>.crash exists (exit 9). ready-once: one ready beat, then silence. migrexit:N: migrating, exit N.
		rep, _ := health.FromEnv(os.Getenv, nil)
		interval := 2 * time.Millisecond
		if kind == "ready-once" {
			interval = time.Hour
		}
		rep.Start(interval)
		rep.SetState(health.Migrating)
		logLine(version)
		time.Sleep(20 * time.Millisecond)
		if kind == "migrexit" {
			n, _ := strconv.Atoi(arg)
			os.Exit(n)
		}
		rep.SetState(health.Ready)
		ch := make(chan os.Signal, 1)
		signal.Notify(ch, syscall.SIGTERM)
		for deadline := time.Now().Add(60 * time.Second); time.Now().Before(deadline); {
			select {
			case <-ch:
				logLine("term")
				os.Exit(0)
			case <-time.After(3 * time.Millisecond):
				if _, err := os.Stat(os.Getenv(fakeLogEnv) + ".crash"); err == nil {
					os.Exit(9)
				}
			}
		}
		os.Exit(8)
	case "wait":
		ch := make(chan os.Signal, 1)
		signal.Notify(ch, syscall.SIGTERM)
		logLine(version)
		<-ch
		logLine("term")
		os.Exit(0)
	case "pend":
		logLine(version)
		if _, err := NewStore(os.Getenv(RootEnv)).Apply(EvSetPending, arg); err != nil {
			logLine("apply error: " + err.Error())
			os.Exit(2)
		}
		os.Exit(ExitRestartIntoPending)
	default:
		logLine(version)
		n, _ := strconv.Atoi(arg)
		os.Exit(n)
	}
}

type rig struct {
	t     *testing.T
	root  string
	log   string
	store *Store
	env   []string
}

// newRig creates an install root whose versions each hold a copy of this test binary as the agent.
func newRig(t *testing.T, behavior string, versions ...string) *rig {
	t.Helper()
	root := t.TempDir()
	r := &rig{t: t, root: root, log: filepath.Join(t.TempDir(), "log"), store: NewStore(root)}
	r.env = append(os.Environ(), fakeAgentEnv+"=1", fakeBehaviorEnv+"="+behavior, fakeLogEnv+"="+r.log)
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(self)
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range versions {
		dir := filepath.Join(root, VersionsDir, v)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, AgentFileName()), data, 0o755); err != nil {
			t.Fatal(err)
		}
		if _, err := bundle.Build(dir, v, runtime.GOOS, runtime.GOARCH); err != nil {
			t.Fatal(err)
		}
	}
	return r
}

func (r *rig) setState(s State) {
	r.t.Helper()
	if err := r.store.Write(s); err != nil {
		r.t.Fatal(err)
	}
}

func (r *rig) run(signals <-chan os.Signal) (int, error) {
	return Run(Config{Root: r.root, Env: r.env, Signals: signals, Logf: func(f string, a ...any) { r.t.Logf(f, a...) }})
}

func (r *rig) started() string {
	data, _ := os.ReadFile(r.log)
	return strings.Join(strings.Fields(string(data)), " ")
}

func (r *rig) state() State {
	r.t.Helper()
	s, err := r.store.Read()
	if err != nil {
		r.t.Fatal(err)
	}
	return s
}

func TestRunStartsCurrent(t *testing.T) {
	r := newRig(t, "", "v1", "v2")
	r.setState(State{Current: "v1"})
	code, err := r.run(nil)
	if err != nil || code != 0 || r.started() != "v1" {
		t.Fatalf("code %d err %v started %q", code, err, r.started())
	}
}

func TestRunWithoutStateFails(t *testing.T) {
	r := newRig(t, "")
	if code, err := r.run(nil); err == nil || code != ExitLauncherError || !strings.Contains(err.Error(), "no current version") {
		t.Fatalf("code %d err %v", code, err)
	}
}

func TestRunReturnsExitCodeOfCurrentVersion(t *testing.T) {
	r := newRig(t, "v1=exit:7", "v1")
	r.setState(State{Current: "v1"})
	before := r.state()
	code, err := r.run(nil)
	if err != nil || code != 7 || r.started() != "v1" {
		t.Fatalf("code %d err %v started %q", code, err, r.started())
	}
	if !sameState(before, r.state()) {
		t.Fatalf("state changed: %+v", r.state())
	}
}

func TestExit75StartsPending(t *testing.T) {
	r := newRig(t, "v1=pend:v2,v2=exit:0", "v1", "v2")
	r.setState(State{Current: "v1"})
	code, err := r.run(nil)
	if err != nil || code != 0 || r.started() != "v1 v2" {
		t.Fatalf("code %d err %v started %q", code, err, r.started())
	}
	if s := r.state(); s.Pending != "v2" || s.Current != "v1" {
		t.Fatalf("a clean pending run must not be promoted before probation: %+v", s)
	}
}

func TestPendingFailingTwiceIsMarkedBadAndReverted(t *testing.T) {
	r := newRig(t, "v1=exit:0,v2=exit:3", "v1", "v2")
	r.setState(State{Current: "v1", Pending: "v2"})
	code, err := r.run(nil)
	if err != nil || code != 3 || r.started() != "v2" {
		t.Fatalf("first run: code %d err %v started %q", code, err, r.started())
	}
	if s := r.state(); s.Pending != "v2" || s.Attempts["v2"] != 1 || s.IsBad("v2") {
		t.Fatalf("after first failure: %+v", s)
	}
	code, err = r.run(nil)
	if err != nil || code != 0 || r.started() != "v2 v2 v1" {
		t.Fatalf("second run: code %d err %v started %q", code, err, r.started())
	}
	s := r.state()
	if s.Pending != "" || s.Current != "v1" || !s.IsBad("v2") || s.Attempts["v2"] != 2 {
		t.Fatalf("after second failure: %+v", s)
	}
	// A third launch no longer touches v2.
	if code, _ := r.run(nil); code != 0 || r.started() != "v2 v2 v1 v1" {
		t.Fatalf("third run: code %d started %q", code, r.started())
	}
}

func TestTamperedCurrentFallsBackToPrevious(t *testing.T) {
	r := newRig(t, "", "v1", "v2")
	r.setState(State{Current: "v2", Previous: "v1"})
	f, err := os.OpenFile(filepath.Join(r.root, VersionsDir, "v2", AgentFileName()), os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	f.WriteString("tampered")
	f.Close()
	code, err := r.run(nil)
	if err != nil || code != 0 || r.started() != "v1" {
		t.Fatalf("code %d err %v started %q", code, err, r.started())
	}
	if s := r.state(); s.Current != "v1" || !s.IsBad("v2") {
		t.Fatalf("state = %+v", s)
	}
}

func TestTamperedPendingIsSkipped(t *testing.T) {
	r := newRig(t, "", "v1", "v2")
	r.setState(State{Current: "v1", Pending: "v2"})
	os.WriteFile(filepath.Join(r.root, VersionsDir, "v2", AgentFileName()), []byte("evil"), 0o755)
	code, err := r.run(nil)
	if err != nil || code != 0 || r.started() != "v1" {
		t.Fatalf("code %d err %v started %q", code, err, r.started())
	}
	if s := r.state(); s.Pending != "" || !s.IsBad("v2") {
		t.Fatalf("state = %+v", s)
	}
}

func TestTamperedCurrentWithoutPreviousIsAnError(t *testing.T) {
	r := newRig(t, "", "v1")
	r.setState(State{Current: "v1"})
	os.WriteFile(filepath.Join(r.root, VersionsDir, "v1", AgentFileName()), []byte("evil"), 0o755)
	if code, err := r.run(nil); err == nil || code != ExitLauncherError || r.started() != "" {
		t.Fatalf("code %d err %v started %q", code, err, r.started())
	}
}

func TestRunRefusesWhileAnotherLauncherHoldsTheLock(t *testing.T) {
	r := newRig(t, "", "v1")
	r.setState(State{Current: "v1"})
	l, err := AcquireLock(r.store.LockPath())
	if err != nil {
		t.Fatal(err)
	}
	defer l.Release()
	if code, err := r.run(nil); err == nil || code != ExitLauncherError || r.started() != "" {
		t.Fatalf("code %d err %v started %q", code, err, r.started())
	}
}

func TestRunRecoversStaleLockAndReleasesItOnExit(t *testing.T) {
	r := newRig(t, "", "v1")
	r.setState(State{Current: "v1"})
	if err := os.WriteFile(r.store.LockPath(), []byte(fmt.Sprintf("%d\n", deadPID(t))), 0o600); err != nil {
		t.Fatal(err)
	}
	if code, err := r.run(nil); err != nil || code != 0 || r.started() != "v1" {
		t.Fatalf("code %d err %v started %q", code, err, r.started())
	}
	if _, err := os.Stat(r.store.LockPath()); !os.IsNotExist(err) {
		t.Fatalf("lock file left behind: %v", err)
	}
}
