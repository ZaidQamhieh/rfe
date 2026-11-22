package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ZaidQamhieh/rfe/agent/internal/hostupdate"
	"github.com/ZaidQamhieh/rfe/agent/internal/launcher"
	"github.com/ZaidQamhieh/rfe/agent/internal/upgrade"
)

// envAutoApply switches on automatic hand-over to a staged version. It is off unless set to 1, true or on, so an
// existing install behaves exactly as before.
const envAutoApply = "RFE_AUTO_APPLY"

// autoApplyScan is how often the daemon looks for a newly staged version.
const autoApplyScan = 5 * time.Minute

// autoApplyEnabled reports whether RFE_AUTO_APPLY asks for the automatic hand-over.
func autoApplyEnabled(getenv func(string) string) bool {
	switch strings.ToLower(strings.TrimSpace(getenv(envAutoApply))) {
	case "1", "true", "on", "yes":
		return true
	}
	return false
}

// findStagedVersion looks in <root>/versions for the newest version directory that the daemon could hand over to:
// a valid version name, not listed bad, not the current or pending version, and strictly newer (semantic version
// order) than both the running build and state.json's current version. It returns "" when there is none, and also
// when a version is already pending (the next restart takes it). Whether the bundle itself is genuine is checked
// by upgrade.Apply, not here.
func findStagedVersion(root, running string) (string, error) {
	st, err := launcher.NewStore(root).Read()
	if err != nil {
		return "", err
	}
	if st.Pending != "" {
		return "", nil
	}
	entries, err := os.ReadDir(filepath.Join(root, launcher.VersionsDir))
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	best := ""
	for _, e := range entries {
		v := e.Name()
		if !e.IsDir() || !launcher.ValidVersion(v) || st.IsBad(v) || v == st.Current || v == running {
			continue
		}
		if newer, err := hostupdate.CompareVersions(v, running); err != nil || newer <= 0 {
			continue
		}
		if st.Current != "" {
			if newer, err := hostupdate.CompareVersions(v, st.Current); err != nil || newer <= 0 {
				continue
			}
		}
		if best != "" {
			if c, err := hostupdate.CompareVersions(v, best); err != nil || c <= 0 {
				continue
			}
		}
		best = v
	}
	return best, nil
}

// autoApplyConfig is everything startAutoApply needs; Exit and the clock-like durations are injectable for tests.
type autoApplyConfig struct {
	Root, DataDir, Running string
	Idle                   func(context.Context) bool
	StopListening          func(context.Context) error
	Exit                   func(code int)
	Scan, Poll             time.Duration
	Logf                   func(string, ...any)
}

// startAutoApply runs a goroutine that, every Scan, looks for a staged version and, when one exists, waits for the
// daemon to be idle and calls upgrade.Apply, which snapshots the data, records the version as pending, stops the
// listeners and exits with code 75 so the launcher starts the new version. A version whose hand-over failed is not
// retried by this process. The returned stop func ends the goroutine.
func startAutoApply(cfg autoApplyConfig) (stop func()) {
	if cfg.Scan <= 0 {
		cfg.Scan = autoApplyScan
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		failed := map[string]bool{}
		for {
			v, err := findStagedVersion(cfg.Root, cfg.Running)
			if err != nil {
				cfg.Logf("auto-apply: cannot read the launcher state: %v", err)
			} else if v != "" && !failed[v] {
				cfg.Logf("auto-apply: version %s is staged; waiting for the agent to be idle", v)
				err := upgrade.ApplyWhenIdle(ctx, upgrade.ApplyOptions{Root: cfg.Root, DataDir: cfg.DataDir, Version: v, AgentVersion: cfg.Running,
					Idle: cfg.Idle, StopListening: cfg.StopListening, Exit: cfg.Exit, Logf: cfg.Logf}, cfg.Poll)
				if err == nil {
					return
				}
				if ctx.Err() != nil {
					return
				}
				failed[v] = true
				cfg.Logf("auto-apply: version %s was not applied: %v", v, err)
			}
			select {
			case <-ctx.Done():
				return
			case <-time.After(cfg.Scan):
			}
		}
	}()
	return func() {
		cancel()
		<-done
	}
}
