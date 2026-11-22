// Package upgrade holds the agent-side half of an upgrade: switching over to a staged version (Apply), going back
// to an older one (Downgrade), restoring the pre-migration snapshot into a new data directory (RestoreSnapshot) and
// the trial run of a staged bundle against a copy of the live data (TrialFunc). The launcher imports none of it, so
// the launcher stays free of the database driver; the health protocol lives in the health subpackage.
package upgrade

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/ZaidQamhieh/rfe/agent/internal/backup"
	"github.com/ZaidQamhieh/rfe/agent/internal/store"
)

// DefaultBackupSubdir is the folder inside a data dir that holds its backups (same as "rfe backup").
const DefaultBackupSubdir = "backups"

// ErrNoSnapshot means there is no premigration snapshot to restore.
var ErrNoSnapshot = errors.New("no premigration snapshot found")

// RestoreOptions says what RestoreSnapshot restores and how it checks the result.
type RestoreOptions struct {
	// DataDir is the live data directory that is replaced (by rename; it is kept under a new name).
	DataDir string
	// BackupDir holds the snapshots; default <DataDir>/backups.
	BackupDir string
	// Validate checks the restored directory before it is swapped in; an error aborts and removes it.
	Validate func(restoredDir string) error
	// Now is the clock for the directory names (default time.Now).
	Now func() time.Time
}

// RestoreResult names what RestoreSnapshot did.
type RestoreResult struct {
	Archive     string // the snapshot used
	OldDir      string // the previous data dir, kept untouched under this name
	Revocations int    // devices revoked after the snapshot whose revocation was re-applied
}

// RestoreSnapshot replaces DataDir with the newest premigration snapshot. It restores into a new directory beside
// DataDir (never into it), replays the revocation log kept in DataDir, runs Validate, and only then renames DataDir to
// a unique ".before-restore-<time>" name and the new directory to DataDir. Nothing is ever overwritten or deleted
// except the new directory when a step before the swap fails. The agent must not be running. If the process is killed
// between the two renames DataDir does not exist for a moment; the old directory is then under its new name.
func RestoreSnapshot(opt RestoreOptions) (*RestoreResult, error) {
	data, err := filepath.Abs(opt.DataDir)
	if err != nil {
		return nil, err
	}
	backups := opt.BackupDir
	if backups == "" {
		backups = filepath.Join(data, DefaultBackupSubdir)
	}
	archive, err := backup.Newest(backups, backup.LabelPremigration)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, fmt.Errorf("%w in %s", ErrNoSnapshot, backups)
		}
		return nil, err
	}
	now := opt.Now
	if now == nil {
		now = time.Now
	}
	stamp := now().UTC().Format("20060102T150405Z")
	newDir, oldDir := data+".restored-"+stamp, data+".before-restore-"+stamp
	for _, p := range []string{newDir, oldDir} {
		if _, err := os.Lstat(p); err == nil {
			return nil, fmt.Errorf("%s already exists; refusing to overwrite it", p)
		}
	}
	_, n, err := backup.RestoreWithRevocations(archive, newDir, filepath.Join(data, store.RevocationsLogFile))
	if err != nil {
		os.RemoveAll(newDir)
		return nil, fmt.Errorf("restore %s: %w", archive, err)
	}
	if opt.Validate != nil {
		if err := opt.Validate(newDir); err != nil {
			os.RemoveAll(newDir)
			return nil, fmt.Errorf("restored snapshot %s failed validation: %w", archive, err)
		}
	}
	if err := os.Rename(data, oldDir); err != nil {
		os.RemoveAll(newDir)
		return nil, fmt.Errorf("move the old data dir aside: %w", err)
	}
	if err := os.Rename(newDir, data); err != nil {
		if rerr := os.Rename(oldDir, data); rerr != nil {
			return nil, fmt.Errorf("swap failed (%v) and the old data dir could not be put back: %w; it is at %s, the restored copy at %s", err, rerr, oldDir, newDir)
		}
		os.RemoveAll(newDir)
		return nil, fmt.Errorf("move the restored data dir into place: %w", err)
	}
	return &RestoreResult{Archive: archive, OldDir: oldDir, Revocations: n}, nil
}
