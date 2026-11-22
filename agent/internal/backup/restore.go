package backup

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/ZaidQamhieh/rfe/agent/internal/store"
)

// ErrRestoreTargetNotEmpty means the restore destination exists and is not an empty directory; restore
// never overwrites or merges into existing data.
var ErrRestoreTargetNotEmpty = errors.New("restore target exists and is not an empty directory")

// checkRestoreTarget accepts a path that does not exist, or an existing empty directory.
func checkRestoreTarget(path string) error {
	info, err := os.Lstat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("%w: %s is not a directory", ErrRestoreTargetNotEmpty, path)
	}
	entries, err := os.ReadDir(path)
	if err != nil {
		return err
	}
	if len(entries) > 0 {
		return fmt.Errorf("%w: %s has %d entries", ErrRestoreTargetNotEmpty, path, len(entries))
	}
	return nil
}

// Restore unpacks archive into newDataDir, which must not exist or must be an empty directory. The
// archive is extracted and fully verified in a staging directory next to the target and only then
// renamed into place, so a failed restore leaves nothing behind. No existing directory is ever removed
// or overwritten (an empty target directory is replaced by the staged one).
func Restore(archive, newDataDir string) (*Manifest, error) {
	target, err := filepath.Abs(newDataDir)
	if err != nil {
		return nil, err
	}
	if err := checkRestoreTarget(target); err != nil {
		return nil, err
	}
	parent := filepath.Dir(target)
	if err := os.MkdirAll(parent, 0o700); err != nil {
		return nil, err
	}
	staging, err := os.MkdirTemp(parent, ".rfe-restore-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(staging) // nothing left after a successful rename; cleans up after a failure
	m, got, err := extract(archive, staging)
	if err != nil {
		return nil, err
	}
	if err := checkExtracted(staging, m, got); err != nil {
		return nil, err
	}
	if err := syncDir(staging); err != nil {
		return nil, err
	}
	// Re-check just before the rename: removing an empty directory cannot delete data, and the rename
	// fails if something non-empty appeared in the meantime.
	if err := checkRestoreTarget(target); err != nil {
		return nil, err
	}
	if err := os.Remove(target); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}
	if err := os.Rename(staging, target); err != nil {
		return nil, err
	}
	if err := syncDir(parent); err != nil {
		return m, fmt.Errorf("restored, but the parent directory could not be synced: %w", err)
	}
	return m, nil
}

// ReplayRevocations re-applies the revocations in logPath that happened at or after since to db. Entries
// older than since are already part of a backup taken at that time, and replaying one could undo a
// later re-pairing of the same device. It returns how many entries were applied; a missing log is none.
func ReplayRevocations(db *store.DB, logPath string, since time.Time) (int, error) {
	entries, err := store.ReadRevocations(logPath)
	if err != nil {
		return 0, err
	}
	applied := 0
	for _, e := range entries {
		if e.Time.Before(since) {
			continue
		}
		if err := db.ReplayRevocation(e.DeviceID, e.Kind); err != nil {
			return applied, fmt.Errorf("replay %s of %s: %w", e.Kind, e.DeviceID, err)
		}
		applied++
	}
	return applied, nil
}

// ApplyRevocationLog opens the restored data dir, replays logPath into it (see ReplayRevocations) and
// then copies the log into the dir as revocations.log, so the history keeps growing from there. An empty
// or missing logPath does nothing.
func ApplyRevocationLog(dataDir, logPath string, since time.Time) (int, error) {
	if logPath == "" {
		return 0, nil
	}
	raw, err := os.ReadFile(logPath)
	if errors.Is(err, fs.ErrNotExist) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	db, err := store.Open(dataDir)
	if err != nil {
		return 0, fmt.Errorf("open restored database: %w", err)
	}
	applied, rerr := ReplayRevocations(db, logPath, since)
	if cerr := db.Close(); rerr == nil {
		rerr = cerr
	}
	if rerr != nil {
		return applied, rerr
	}
	dst := filepath.Join(dataDir, store.RevocationsLogFile)
	if sameFile(logPath, dst) {
		return applied, nil
	}
	if _, err := os.Lstat(dst); err == nil {
		return applied, nil // never overwrite a log the restored dir already has
	}
	return applied, writeFileAtomic(dst, raw, 0o600)
}

// sameFile reports whether two paths name the same existing file.
func sameFile(a, b string) bool {
	ia, err := os.Stat(a)
	if err != nil {
		return false
	}
	ib, err := os.Stat(b)
	if err != nil {
		return false
	}
	return os.SameFile(ia, ib)
}

// writeFileAtomic writes data to path through a temporary file in the same directory, fsyncs it and
// renames it into place.
func writeFileAtomic(path string, data []byte, mode os.FileMode) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".tmp*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	fail := func(err error) error {
		tmp.Close()
		os.Remove(name)
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		return fail(err)
	}
	if err := tmp.Chmod(mode); err != nil {
		return fail(err)
	}
	if err := tmp.Sync(); err != nil {
		return fail(err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(name)
		return err
	}
	if err := os.Rename(name, path); err != nil {
		os.Remove(name)
		return err
	}
	return nil
}

// RestoreWithRevocations restores archive into newDataDir and then replays the revocation log at
// revLogPath (when it exists) for everything that happened since the backup was taken.
// It returns the manifest and how many revocations were applied.
func RestoreWithRevocations(archive, newDataDir, revLogPath string) (*Manifest, int, error) {
	m, err := Restore(archive, newDataDir)
	if err != nil {
		return nil, 0, err
	}
	n, err := ApplyRevocationLog(newDataDir, revLogPath, m.Created)
	if err != nil {
		return m, n, fmt.Errorf("restored to %s, but replaying revocations failed: %w", newDataDir, err)
	}
	return m, n, nil
}
