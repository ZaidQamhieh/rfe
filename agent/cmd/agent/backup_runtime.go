package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ZaidQamhieh/rfe/agent/internal/backup"
	"github.com/ZaidQamhieh/rfe/agent/internal/security"
	"github.com/ZaidQamhieh/rfe/agent/internal/store"
)

// Environment variables that control the daemon's backups. The passphrase is only ever read from the environment (or a
// terminal prompt by the "rfe backup" commands): it is not stored in the database, the status file or the logs.
const (
	envBackups          = "RFE_BACKUPS"            // "off" turns the scheduler and the premigration snapshot off
	offsiteKeep         = 14                       // newest encrypted copies kept in the off-device folder (7 daily + 4 weekly + margin)
	envBackupOffsiteDir = "RFE_BACKUP_OFFSITE_DIR" // folder for encrypted copies of the daily and weekly archives
	envBackupPassphrase = "RFE_BACKUP_PASSPHRASE"  // passphrase those copies are encrypted with
	backupStopWait      = 15 * time.Second         // how long shutdown waits for a backup in progress
	premigrationStaging = ".premigration-*"        // scratch folder for the closed-database copy
	walSuffix           = "-wal"                   // SQLite write-ahead log next to the database file
)

// backupsEnabled is false only when RFE_BACKUPS is "off", "0", "false" or "no" (any case).
func backupsEnabled(getenv func(string) string) bool {
	switch strings.ToLower(strings.TrimSpace(getenv(envBackups))) {
	case "off", "0", "false", "no":
		return false
	}
	return true
}

// copyPlain copies the file src to dst (mode 0600). A missing src is reported as fs.ErrNotExist.
func copyPlain(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

// premigrationSnapshot is the hook runServe calls before store.Open migrates the database. When agent.db exists and its
// user_version is below this build's schema version it copies the closed database (and its -wal file) with the
// certificate, key and account key into a scratch folder, then writes and verifies a "premigration" archive of that
// copy (the three newest are kept). Working on a copy means the live database is not opened read-write, and so not
// changed, before the migration runs. With no database, or one already at the current version, it does nothing.
// The outcome is recorded in the status file; the caller logs a returned error and carries on.
func premigrationSnapshot(dataDir string, now func() time.Time) (err error) {
	uv, _, rerr := store.ReadSchemaState(dataDir)
	if errors.Is(rerr, fs.ErrNotExist) {
		return nil
	}
	taken := now().UTC()
	info := backup.BackupInfo{Time: taken, Label: backup.LabelPremigration}
	defer func() {
		if err != nil {
			info.Error = backup.ScrubError(err, dataDir)
		}
		info.OK = err == nil
		if serr := backup.RecordBackup(dataDir, info); serr != nil && err == nil {
			err = fmt.Errorf("record backup status: %w", serr)
		}
	}()
	if rerr != nil {
		return fmt.Errorf("read schema version: %w", rerr)
	}
	if uv >= store.SchemaVersion() {
		return nil
	}
	backups := filepath.Join(dataDir, backup.SubdirName)
	if err := os.MkdirAll(backups, 0o700); err != nil {
		return err
	}
	stage, err := os.MkdirTemp(backups, premigrationStaging)
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage)
	for _, f := range []struct {
		name     string
		optional bool
	}{{store.DBFile, false}, {store.DBFile + walSuffix, true}, {security.CertFile, false}, {security.KeyFile, false}, {store.AccountKeyFile, true}} {
		if cerr := copyPlain(filepath.Join(dataDir, f.name), filepath.Join(stage, f.name)); cerr != nil && !(f.optional && errors.Is(cerr, fs.ErrNotExist)) {
			return fmt.Errorf("copy %s: %w", f.name, cerr)
		}
	}
	archive, _, err := backup.Create(stage, backups, backup.Options{Label: backup.LabelPremigration, AgentVersion: version, Now: func() time.Time { return taken }})
	if err != nil {
		return err
	}
	info.Path = backup.RelativeArchivePath(dataDir, archive)
	if st, serr := os.Stat(archive); serr == nil {
		info.Size = st.Size()
	}
	if _, err := backup.Verify(archive); err != nil {
		return fmt.Errorf("premigration snapshot failed verification: %w", err)
	}
	if _, perr := backup.Prune(backups, backup.DefaultPolicy()); perr != nil {
		return fmt.Errorf("snapshot is safe, but pruning old backups failed: %w", perr)
	}
	return nil
}

// auditTrigger maps an audit action to the backup it should cause: a pairing or registration gives a "pair" backup, a
// device revoke or remove a "revoke" backup, anything else none. The returned func never blocks (Scheduler.Trigger
// only records the request), so it is safe on the request path.
func auditTrigger(trigger func(label string)) func(action string) {
	return func(action string) {
		switch action {
		case store.AuditPair, store.AuditRegister:
			trigger(backup.LabelPair)
		case store.AuditDeviceRevoked, store.AuditDeviceRemoved:
			trigger(backup.LabelRevoke)
		}
	}
}

// newBackupScheduler builds the daemon's scheduler from the environment: the trial restore runs the selfcheck, and
// when RFE_BACKUP_OFFSITE_DIR is set (with RFE_BACKUP_PASSPHRASE) finished daily and weekly archives are also written,
// encrypted, to that folder. A folder without a passphrase is recorded as a configuration error in the status.
func newBackupScheduler(dataDir string, getenv func(string) string, logf func(string, ...any)) *backup.Scheduler {
	cfg := backup.SchedulerConfig{DataDir: dataDir, AgentVersion: version, Check: selfcheckAsTrialCheck, Logf: logf}
	if dir := getenv(envBackupOffsiteDir); dir != "" {
		pass := []byte(getenv(envBackupPassphrase))
		cfg.OffsiteDir = dir
		if len(pass) == 0 {
			if err := backup.RecordOffsite(dataDir, time.Now(), errors.New(envBackupPassphrase+" is not set; no off-device copy is made")); err != nil {
				logf("backup status: %v", err)
			}
		} else {
			cfg.Offsite = func(archive string) error {
				if _, err := backup.CopyOffsite(archive, dir, pass); err != nil {
					return err
				}
				return backup.PruneOffsite(dir, offsiteKeep)
			}
		}
	}
	return backup.NewScheduler(cfg)
}

// startBackups starts the scheduler goroutine and hooks the audit trail so every pairing and revocation asks for a
// backup. The returned stop func cancels the scheduler and waits (at most backupStopWait) for a backup in progress.
func startBackups(dataDir string, db *store.DB, getenv func(string) string, logf func(string, ...any)) (stop func()) {
	sch := newBackupScheduler(dataDir, getenv, logf)
	db.SetAuditActionHook(auditTrigger(sch.Trigger))
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		sch.Run(ctx)
	}()
	return func() {
		cancel()
		select {
		case <-done:
		case <-time.After(backupStopWait):
			logf("backup scheduler did not stop within %s; exiting anyway", backupStopWait)
		}
	}
}
