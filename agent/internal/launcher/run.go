package launcher

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/ZaidQamhieh/rfe/agent/internal/bundle"
	"github.com/ZaidQamhieh/rfe/agent/internal/upgrade/health"
)

// Environment variables the launcher sets for the agent it starts, and reads to find its root.
const (
	// RootEnv names the install root; the launcher reads it (default: the launcher executable's directory) and
	// passes it to the agent.
	RootEnv = "RFE_BUNDLE_ROOT"
	// VersionEnv tells the agent which bundle version it was started from.
	VersionEnv = "RFE_BUNDLE_VERSION"

	// ExitLauncherError is returned when the launcher itself cannot do its job (no state, lock held, no usable bundle).
	ExitLauncherError = 70
	// maxStarts bounds the start/restart loop of one launcher run; past it the last exit code goes to the OS supervisor.
	maxStarts = 16
)

// Config describes one launcher run.
type Config struct {
	// Root is the install root holding versions/, state.json and staging/.
	Root string
	// Args are passed to the agent unchanged (the data dir flags among them).
	Args []string
	// Env is the agent's environment; nil means the launcher's own. The launcher adds RootEnv, VersionEnv and the
	// health variables.
	Env []string
	// Stdin, Stdout and Stderr connect the agent; nil means the launcher's own.
	Stdin          io.Reader
	Stdout, Stderr io.Writer
	// Signals delivers termination signals to forward to the running agent.
	Signals <-chan os.Signal
	// AgentName overrides the agent file name inside a bundle (default AgentFileName()).
	AgentName string
	// Logf receives one line per decision; nil discards them.
	Logf func(format string, args ...any)

	// StartTimeout, Probation, HealthGap, PollInterval and ProbeInterval tune the watcher of a pending version (see
	// the Default* constants for the meaning and the default of each).
	StartTimeout, Probation, HealthGap, PollInterval, ProbeInterval time.Duration
	// Clock is the watcher's time source (default: the wall clock).
	Clock Clock
	// ProbeAddr is the agent's TLS address for the certificate check; empty means the address the agent reports in
	// its heartbeats.
	ProbeAddr string
	// ProbeFingerprint returns the leaf-certificate fingerprint presented at an address (default TLSFingerprint).
	ProbeFingerprint func(addr string) (string, error)
	// DataDir is the agent's data directory, needed only to roll the database back; empty skips that step.
	DataDir string
	// Data does the database work of a rollback (default ExecDataTool running the agent of the previous version).
	Data DataTool

	storeHook func(step string) error // tests only: see Store.hook
}

func (c *Config) logf(format string, args ...any) {
	if c.Logf != nil {
		c.Logf(format, args...)
	}
}

// setDefaults fills every zero tuning field.
func (c *Config) setDefaults(agentName string) {
	for _, d := range []struct {
		p *time.Duration
		v time.Duration
	}{{&c.StartTimeout, DefaultStartTimeout}, {&c.Probation, DefaultProbation}, {&c.HealthGap, DefaultHealthGap},
		{&c.PollInterval, DefaultPollInterval}, {&c.ProbeInterval, DefaultProbeInterval}} {
		if *d.p <= 0 {
			*d.p = d.v
		}
	}
	if c.Clock == nil {
		c.Clock = realClock{}
	}
	if c.ProbeFingerprint == nil {
		c.ProbeFingerprint = TLSFingerprint
	}
	if c.Data == nil && c.DataDir != "" {
		c.Data = ExecDataTool{AgentName: agentName, Env: c.Env}
	}
}

// Run takes the root's lock and keeps one agent running per state.json. It starts the pending version if there is
// one that is not bad and has not failed twice, else the current version; refuses a version whose agent binary no
// longer matches its bundle manifest (marking it bad and falling back to the previous one); restarts when the
// agent exits with 75; and otherwise returns the agent's exit code for the OS supervisor to act on.
//
// A pending version is on trial: it must send a "ready" heartbeat within StartTimeout, then stay healthy and keep
// presenting the recorded certificate for Probation, after which it is promoted. A crash, a silent hang, a health
// gap or a changed certificate kills it and counts as one failed attempt; the second failure marks it bad and rolls
// back (see rollbackData). Run never opens the agent's database or creates certificates.
func Run(cfg Config) (int, error) {
	store := NewStore(cfg.Root)
	store.hook = cfg.storeHook
	if err := os.MkdirAll(cfg.Root, 0o700); err != nil {
		return ExitLauncherError, err
	}
	lock, err := AcquireLock(store.LockPath())
	if err != nil {
		return ExitLauncherError, err
	}
	defer lock.Release()
	name := cfg.AgentName
	if name == "" {
		name = AgentFileName()
	}
	cfg.setDefaults(name)
	last := 0
	for starts := 0; starts < maxStarts; starts++ {
		st, err := store.Read()
		if err != nil {
			return ExitLauncherError, err
		}
		if st.Current == "" {
			return ExitLauncherError, fmt.Errorf("%s has no current version: install a bundle first (rfe-launcher bundle)", store.path(StateFile))
		}
		version, isPending := Pick(st)
		dir, err := store.VersionDir(version)
		if err != nil {
			return ExitLauncherError, err
		}
		if err := bundle.VerifyFile(dir, name); err != nil {
			cfg.logf("refusing version %s: %v", version, err)
			if _, err := store.Apply(EvMarkBad, version); err != nil {
				return ExitLauncherError, fmt.Errorf("version %s failed verification: %w", version, err)
			}
			continue
		}
		res := runChild(&cfg, store, st, dir, name, version, isPending)
		last = res.code
		if res.err != nil {
			return ExitLauncherError, res.err
		}
		if res.stopped {
			return res.code, nil
		}
		if res.startErr != nil {
			cfg.logf("could not start version %s: %v", version, res.startErr)
			if _, err := store.Apply(EvMarkBad, version); err != nil {
				return ExitLauncherError, fmt.Errorf("version %s did not start: %w", version, errors.Join(res.startErr, err))
			}
			continue
		}
		onTrial := isPending && !res.promoted
		switch {
		case onTrial && res.failure != "":
			cfg.logf("pending version %s failed: %s", version, res.failure)
		case res.code == 0:
			return 0, nil
		case res.code == ExitRestartIntoPending:
			cfg.logf("version %s asked for a restart", version)
			continue
		case onTrial:
			cfg.logf("pending version %s exited with code %d before it was promoted", version, res.code)
		default:
			return res.code, nil
		}
		next, err := failPending(&cfg, store, version)
		if err != nil {
			return ExitLauncherError, err
		}
		if next.Pending == "" {
			cfg.logf("version %s is bad; reverting to %s", version, next.Current)
			continue
		}
		return res.code, nil
	}
	return last, nil
}

// failPending records one failed attempt of the pending version. On the attempt that marks it bad, the database is
// first rolled back if the previous version needs that, so that a failure while doing so leaves the version still
// pending and the whole rollback is tried again at the next launch.
func failPending(cfg *Config, store *Store, version string) (State, error) {
	cur, err := store.Read()
	if err != nil {
		return State{}, err
	}
	if cur.Pending == version && cur.Attempts[version]+1 >= MaxAttempts {
		if err := rollbackData(cfg, store, cur.Current); err != nil {
			return State{}, fmt.Errorf("rolling the database back for %s: %w", cur.Current, err)
		}
	}
	return store.Apply(EvFailPending, version)
}

// rollbackData makes the database usable by the previous version. It asks that version's own agent binary what
// the database now needs: when its min_compatible is above what that binary supports (the failed version ran a
// breaking migration), the newest premigration snapshot is restored into a new directory; otherwise the pointer
// flip alone is enough, because additive migrations leave older agents able to read the database.
func rollbackData(cfg *Config, store *Store, prev string) error {
	if cfg.DataDir == "" || cfg.Data == nil {
		cfg.logf("no data dir configured: not checking whether the database needs a rollback")
		return nil
	}
	dir, err := store.VersionDir(prev)
	if err != nil {
		return err
	}
	ctx := context.Background()
	info, err := cfg.Data.Schema(ctx, dir, cfg.DataDir)
	if err != nil {
		return fmt.Errorf("ask %s about the database: %w", prev, err)
	}
	if info.CanOpen() {
		cfg.logf("database (min_compatible %d) is still readable by %s (level %d): pointer flip only", info.MinCompatible, prev, info.SupportedLevel)
		return nil
	}
	cfg.logf("database min_compatible %d is above %s's level %d: restoring the premigration snapshot", info.MinCompatible, prev, info.SupportedLevel)
	return cfg.Data.RestoreSnapshot(ctx, dir, cfg.DataDir)
}

// childResult is how one run of the agent ended.
type childResult struct {
	code     int    // exit code (1 when killed by a signal)
	stopped  bool   // a signal was forwarded: the launcher is shutting down
	promoted bool   // the watcher promoted this version while it ran
	failure  string // why the watcher killed it, if it did
	startErr error  // the process could not be started
	err      error  // a launcher failure (health listener, state write)
}

// runChild starts dir/name with the configured args and environment and a fresh health listener, forwards signals
// to it, applies the watcher's verdicts (kill, promote, record the certificate fingerprint) and waits for it to exit.
// Every state write happens on this goroutine. A panic (a simulated kill in tests) kills the child first.
func runChild(cfg *Config, store *Store, st State, dir, name, version string, pending bool) (res childResult) {
	if abs, aerr := filepath.Abs(dir); aerr == nil {
		dir = abs // an absolute path is never looked up on PATH
	}
	srv, err := health.Listen(cfg.Clock.Now)
	if err != nil {
		res.err = fmt.Errorf("health listener: %w", err)
		return res
	}
	defer srv.Close()
	cmd := exec.Command(filepath.Join(dir, name), cfg.Args...)
	env := cfg.Env
	if env == nil {
		env = os.Environ()
	}
	cmd.Env = append(filterEnv(env, RootEnv, VersionEnv, health.EnvURL, health.EnvToken), RootEnv+"="+cfg.Root, VersionEnv+"="+version)
	cmd.Env = append(cmd.Env, srv.Env()...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = cfg.Stdin, cfg.Stdout, cfg.Stderr
	if cmd.Stdin == nil {
		cmd.Stdin = os.Stdin
	}
	if cmd.Stdout == nil {
		cmd.Stdout = os.Stdout
	}
	if cmd.Stderr == nil {
		cmd.Stderr = os.Stderr
	}
	if err := cmd.Start(); err != nil {
		res.startErr = err
		return res
	}
	if err := bindToLauncher(cmd); err != nil {
		cfg.logf("the agent is not tied to the launcher (a hard kill of the launcher would leave it running): %v", err)
	}
	defer func() {
		if rec := recover(); rec != nil {
			cmd.Process.Kill()
			panic(rec)
		}
	}()
	mon := newMonitor(cfg, srv, pending, st.ExpectedCertFingerprint)
	defer mon.close()
	waitCh := make(chan error, 1)
	go func() { waitCh <- cmd.Wait() }()
	kill := func() { cmd.Process.Kill() }
	var werr error
wait:
	for {
		select {
		case werr = <-waitCh:
			break wait
		case sig := <-cfg.Signals:
			res.stopped = true
			// Windows cannot deliver a signal to another process; Kill is its closest equivalent.
			if err := cmd.Process.Signal(sig); err != nil {
				kill()
			}
		case v := <-mon.events:
			switch v.kind {
			case verdictFail:
				if res.failure == "" {
					res.failure = v.reason
					kill()
				}
			case verdictPromote:
				if res.failure != "" {
					break
				}
				if _, err := store.Apply(EvPromote, version); err != nil {
					res.err = fmt.Errorf("promote %s: %w", version, err)
					kill()
				} else {
					res.promoted = true
					cfg.logf("version %s passed probation and is now current", version)
				}
			case verdictFingerprint:
				if _, err := store.Apply(EvSetFingerprint, v.fingerprint); err != nil {
					cfg.logf("could not record the certificate fingerprint: %v", err)
				}
			}
		}
	}
	if werr != nil {
		var ee *exec.ExitError
		if !errors.As(werr, &ee) {
			res.startErr = werr
			return res
		}
		if res.code = ee.ExitCode(); res.code < 0 {
			res.code = 1 // killed by a signal: no exit code to pass on
		}
	}
	return res
}

// filterEnv returns env without entries for the given variable names.
func filterEnv(env []string, drop ...string) []string {
	out := make([]string, 0, len(env)+len(drop))
next:
	for _, kv := range env {
		for _, d := range drop {
			if strings.HasPrefix(kv, d+"=") {
				continue next
			}
		}
		out = append(out, kv)
	}
	return out
}
