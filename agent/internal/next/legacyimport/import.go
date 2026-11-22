// Package legacyimport turns an old agent.db (PRAGMA user_version 0 to 9) into
// the v3 database agent-v3.db, once, safely. The old file is never written:
// it is snapshotted to a scratch copy, the old store's own migrations run on
// that copy so every version is read in the latest old shape, the rows are
// converted and written to agent-v3.db.importing, verified, fsynced and only
// then renamed into place. Any failure before the rename leaves nothing behind
// except the untouched old files, so the import can simply run again.
package legacyimport

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"time"

	nstore "github.com/ZaidQamhieh/rfe/agent/internal/next/store"
)

// File names inside the data directory.
const (
	OldDBName = "agent.db"
	NewDBName = "agent-v3.db"
)

// Import stages, in order. Options.Hook is called as each stage begins, so a
// test can make any stage fail.
const (
	StageSnapshot = "snapshot" // copy the old database to a scratch directory
	StageMigrate  = "migrate"  // run the old store's migrations on the copy
	StageRead     = "read"     // read and convert every row
	StageWrite    = "write"    // write agent-v3.db.importing
	StageVerify   = "verify"   // read it back and compare with the source rows
	StageSync     = "sync"     // fold the journal in, close, fsync
	StageRename   = "rename"   // move it to agent-v3.db and fsync the directory
)

// Stages lists the stages in the order they run.
var Stages = []string{StageSnapshot, StageMigrate, StageRead, StageWrite, StageVerify, StageSync, StageRename}

// ErrAlreadyImported is returned when agent-v3.db already exists.
var ErrAlreadyImported = errors.New("agent-v3.db already exists")

// Options tunes an import. The zero value is valid.
type Options struct {
	Now  func() time.Time                 // clock for "import time"; default time.Now
	Logf func(format string, args ...any) // receives each warning as it happens; may be nil
	Hook func(stage string) error         // called as each stage begins; an error aborts the import
}

// Report says what an import wrote, or for Check would write.
type Report struct {
	OldUserVersion int            // PRAGMA user_version of the old file as found
	Counts         map[string]int // rows in each new table (keys as nstore.DB.Counts)
	NotImported    map[string]int // old rows deliberately left behind: pairing_codes, pair_requests
	DroppedChunks  int            // chunk rows removed because the temp file is too short
	Warnings       []string       // everything that was adjusted or dropped, in plain words
}

// Import converts oldPath (the old agent.db) into newPath (agent-v3.db).
// dataDir is the agent data directory; its transfers/ folder holds the upload
// temp files whose sizes decide which chunk rows survive. It returns
// ErrAlreadyImported if newPath exists, and on any error leaves no new file.
func Import(oldPath, newPath, dataDir string, opts Options) (*Report, error) {
	return run(oldPath, newPath, dataDir, opts, true)
}

// Check is the dry run of Import: it does every step up to and including
// conversion and returns the report, but writes nothing in the data directory.
func Check(oldPath, dataDir string, opts Options) (*Report, error) {
	return run(oldPath, "", dataDir, opts, false)
}

// importer carries the options and the report through one run.
type importer struct {
	opts Options
	rep  *Report
}

// now returns the import time in unix seconds.
func (im *importer) now() int64 {
	if im.opts.Now != nil {
		return im.opts.Now().Unix()
	}
	return time.Now().Unix()
}

// stage announces a stage to the test hook and returns its error, if any.
func (im *importer) stage(name string) error {
	if im.opts.Hook == nil {
		return nil
	}
	if err := im.opts.Hook(name); err != nil {
		return fmt.Errorf("stage %s: %w", name, err)
	}
	return nil
}

// warnf records a warning in the report and passes it to the logger.
func (im *importer) warnf(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	im.rep.Warnings = append(im.rep.Warnings, msg)
	if im.opts.Logf != nil {
		im.opts.Logf("legacy import: %s", msg)
	}
}

// run is the shared body of Import (write=true) and Check (write=false).
func run(oldPath, newPath, dataDir string, opts Options, write bool) (*Report, error) {
	im := &importer{opts: opts, rep: &Report{NotImported: map[string]int{}}}
	if write {
		if _, err := os.Lstat(newPath); err == nil {
			return nil, fmt.Errorf("%w: %s", ErrAlreadyImported, newPath)
		} else if !errors.Is(err, fs.ErrNotExist) {
			return nil, err
		}
	}
	scratch, err := os.MkdirTemp("", "rfe-import-*")
	if err != nil {
		return nil, fmt.Errorf("scratch dir: %w", err)
	}
	defer os.RemoveAll(scratch)

	if err := im.stage(StageSnapshot); err != nil {
		return nil, err
	}
	if im.rep.OldUserVersion, err = snapshot(oldPath, filepath.Join(scratch, OldDBName)); err != nil {
		return nil, fmt.Errorf("snapshot %s: %w", oldPath, err)
	}
	if err := im.stage(StageMigrate); err != nil {
		return nil, err
	}
	if err := migrateCopy(scratch); err != nil {
		return nil, fmt.Errorf("migrate copy of %s: %w", oldPath, err)
	}
	if err := im.stage(StageRead); err != nil {
		return nil, err
	}
	data, err := im.readLegacy(filepath.Join(scratch, OldDBName), dataDir)
	if err != nil {
		return nil, err
	}
	im.rep.Counts = countsOf(data)
	if !write {
		return im.rep, nil
	}
	if err := im.publish(newPath, data); err != nil {
		return nil, err
	}
	return im.rep, nil
}

// publish builds newPath+".importing", verifies it and renames it to newPath.
// On any failure the .importing files are removed again.
func (im *importer) publish(newPath string, data nstore.ImportData) (err error) {
	importing := newPath + ".importing"
	if err := removeImporting(importing); err != nil {
		return fmt.Errorf("remove stale %s: %w", importing, err)
	}
	defer func() {
		if err != nil {
			if cerr := removeImporting(importing); cerr != nil {
				err = errors.Join(err, fmt.Errorf("clean up %s: %w", importing, cerr))
			}
		}
	}()
	if err := im.build(importing, data); err != nil {
		return err
	}
	if err := im.stage(StageRename); err != nil {
		return err
	}
	if err := os.Rename(importing, newPath); err != nil {
		return fmt.Errorf("rename into place: %w", err)
	}
	if err := syncDir(filepath.Dir(newPath)); err != nil {
		// The new file is already in place and complete; only its directory
		// entry may not be durable yet, which a rerun would not repair.
		im.warnf("could not fsync %s: %v", filepath.Dir(newPath), err)
	}
	return nil
}

// build writes data into the file at importing (created private, 0600),
// verifies it by reading it back, folds the journal in, closes and fsyncs it.
func (im *importer) build(importing string, data nstore.ImportData) error {
	// Create the file first so it is private to the agent's user: it holds
	// password hashes and token hashes. SQLite adopts an empty existing file.
	f, err := os.OpenFile(importing, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0o600)
	if err != nil {
		return fmt.Errorf("create %s: %w", importing, err)
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := im.stage(StageWrite); err != nil {
		return err
	}
	db, err := nstore.Open(importing)
	if err != nil {
		return err
	}
	closed := false
	defer func() {
		if !closed {
			db.Close()
		}
	}()
	if err := db.ImportAll(data); err != nil {
		return err
	}
	if err := im.stage(StageVerify); err != nil {
		return err
	}
	if err := verify(db, data); err != nil {
		return fmt.Errorf("verify: %w", err)
	}
	if err := im.stage(StageSync); err != nil {
		return err
	}
	if err := db.PrepareForCopy(); err != nil {
		return err
	}
	closed = true
	if err := db.Close(); err != nil {
		return fmt.Errorf("close: %w", err)
	}
	return fsyncFile(importing)
}

// removeImporting deletes the in-progress file and any SQLite side files
// (a missing file is not an error).
func removeImporting(importing string) error {
	var errs []error
	for _, suffix := range []string{"", "-wal", "-shm", "-journal"} {
		if err := os.Remove(importing + suffix); err != nil && !errors.Is(err, fs.ErrNotExist) {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// fsyncFile flushes a file's contents to disk.
func fsyncFile(path string) error {
	f, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

// syncDir flushes a directory entry (a rename) to disk. Windows cannot open
// a directory for syncing, so there the call is skipped.
func syncDir(dir string) error {
	if runtime.GOOS == "windows" {
		return nil
	}
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	if err := d.Sync(); err != nil {
		d.Close()
		return err
	}
	return d.Close()
}

// metaFor builds the meta rows recording where the import came from.
func metaFor(oldUserVersion int, now int64) map[string]string {
	return map[string]string{
		"schema_version":             strconv.Itoa(nstore.SchemaVersion),
		"imported_from_user_version": strconv.Itoa(oldUserVersion),
		"imported_at":                strconv.FormatInt(now, 10),
	}
}

// countsOf returns the row count of each new table for data.
func countsOf(d nstore.ImportData) map[string]int {
	chunks := 0
	for _, u := range d.Uploads {
		chunks += len(u.Chunks)
	}
	return map[string]int{
		"users": len(d.Users), "devices": len(d.Devices), "share_links": len(d.ShareLinks),
		"share_events": len(d.ShareEvents), "uploads": len(d.Uploads), "upload_chunks": chunks,
		"audit_log": len(d.Audit), "settings": len(d.Settings),
	}
}
