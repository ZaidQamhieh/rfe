package upgrade

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"time"

	"github.com/ZaidQamhieh/rfe/agent/internal/backup"
	"github.com/ZaidQamhieh/rfe/agent/internal/bundle"
	"github.com/ZaidQamhieh/rfe/agent/internal/launcher"
)

// ErrBusy means the agent still has work in flight, so it will not switch over now.
var ErrBusy = errors.New("agent is not idle")

// ExitApplied is the exit code that tells the launcher to restart into the pending version.
const ExitApplied = launcher.ExitRestartIntoPending

// exitApplyFailed is the exit code after a failed switch-over that already stopped the listener: the agent cannot
// carry on serving, so it exits non-zero and the supervisor restarts it on the current version.
const exitApplyFailed = 1

// ApplyOptions says how the running agent switches over to a staged version.
type ApplyOptions struct {
	Root      string // install root
	DataDir   string // live data dir to snapshot
	BackupDir string // default <DataDir>/backups
	Version   string // staged version to run next
	// AgentVersion is recorded in the snapshot's manifest; AgentName is the agent file name in a bundle.
	AgentVersion, AgentName string
	// Idle reports whether it is safe to switch now (nil means always).
	Idle func(ctx context.Context) bool
	// StopListening makes the agent stop accepting work (nil: the caller is a separate admin command that cannot).
	StopListening func(ctx context.Context) error
	// Exit ends the process with the given code (nil: Apply just returns, as the admin command does).
	Exit func(code int)
	Now  func() time.Time
	Logf func(format string, args ...any)
}

// Apply switches the agent over to a staged version: it checks the bundle, asks Idle, stops listening, takes a
// verified "premigration" snapshot of the data (the three newest are kept), records the version as pending in
// state.json and finally exits with ExitApplied so the launcher restarts into it. Snapshot and state write
// happen after the listener stopped, so the snapshot cannot miss a late write. If anything fails after the
// listener stopped, Exit is called with 1 instead, so a supervisor restarts the current version.
func Apply(ctx context.Context, opt ApplyOptions) (err error) {
	logf := opt.Logf
	if logf == nil {
		logf = func(string, ...any) {}
	}
	name := opt.AgentName
	if name == "" {
		name = launcher.AgentFileName()
	}
	st := launcher.NewStore(opt.Root)
	dir, err := st.VersionDir(opt.Version)
	if err != nil {
		return err
	}
	if _, err := bundle.Verify(dir); err != nil {
		return fmt.Errorf("staged bundle %s does not verify: %w", dir, err)
	}
	if err := bundle.VerifyFile(dir, name); err != nil {
		return fmt.Errorf("staged agent binary: %w", err)
	}
	if opt.Idle != nil && !opt.Idle(ctx) {
		return ErrBusy
	}
	stopped := false
	if opt.StopListening != nil {
		if err := opt.StopListening(ctx); err != nil {
			return fmt.Errorf("stop listening: %w", err)
		}
		stopped = true
	}
	defer func() {
		if opt.Exit == nil {
			return
		}
		if err != nil {
			if stopped {
				opt.Exit(exitApplyFailed)
			}
			return
		}
		opt.Exit(ExitApplied)
	}()
	backups := opt.BackupDir
	if backups == "" {
		backups = filepath.Join(opt.DataDir, DefaultBackupSubdir)
	}
	archive, _, err := backup.Create(opt.DataDir, backups, backup.Options{Label: backup.LabelPremigration, AgentVersion: opt.AgentVersion, Now: opt.Now})
	if err != nil {
		return fmt.Errorf("premigration snapshot: %w", err)
	}
	if _, err = backup.Verify(archive); err != nil {
		return fmt.Errorf("premigration snapshot %s failed verification: %w", archive, err)
	}
	logf("premigration snapshot %s", archive)
	if removed, perr := backup.Prune(backups, backup.DefaultPolicy()); perr != nil {
		logf("snapshot is safe, but pruning old backups failed: %v", perr)
	} else if len(removed) > 0 {
		logf("pruned %d old backups", len(removed))
	}
	if _, err = st.Apply(launcher.EvSetPending, opt.Version); err != nil {
		return fmt.Errorf("record %s as pending: %w", opt.Version, err)
	}
	logf("version %s is pending; the launcher starts it on the next restart", opt.Version)
	return nil
}

// ApplyWhenIdle calls Apply once Idle says yes, checking every poll (default 30 s) until ctx ends. It is the entry
// point a daemon uses to switch over at a quiet moment instead of interrupting a transfer.
func ApplyWhenIdle(ctx context.Context, opt ApplyOptions, poll time.Duration) error {
	if poll <= 0 {
		poll = 30 * time.Second
	}
	for {
		err := Apply(ctx, opt)
		if !errors.Is(err, ErrBusy) {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(poll):
		}
	}
}

// StoreIdle returns an Idle func that is true when the database lists no active transfers (uploads or downloads
// in progress). It reports not idle when the count cannot be read.
func StoreIdle(db interface{ CountActiveTransfers() (int, error) }) func(context.Context) bool {
	return func(context.Context) bool {
		n, err := db.CountActiveTransfers()
		return err == nil && n == 0
	}
}
