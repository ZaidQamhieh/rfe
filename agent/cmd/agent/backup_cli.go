package main

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/ZaidQamhieh/rfe/agent/internal/backup"
	"github.com/ZaidQamhieh/rfe/agent/internal/store"
)

// defaultBackupSubdir is the folder inside the data dir that "rfe backup" writes to by default.
const defaultBackupSubdir = "backups"

// cmdBackup implements "rfe backup" (create) and "rfe backup verify <archive>".
func cmdBackup(args []string) error {
	if len(args) > 0 {
		switch args[0] {
		case "verify":
			return cmdBackupVerify(args[1:])
		case "decrypt":
			return cmdBackupDecrypt(args[1:])
		case "offsite":
			return cmdBackupOffsite(args[1:])
		}
	}
	fs := newAdminFlags("backup", "backup [-out <dir>] [-label <name>] [-no-prune] [-data <dir>]  |  backup verify <archive>")
	data := fs.String("data", "", "agent data dir")
	out := fs.String("out", "", "folder to write the archive to (default <data dir>/backups)")
	label := fs.String("label", backup.LabelManual, "archive label: lowercase letters and digits (premigration, daily, weekly and manual are pruned by count)")
	noPrune := fs.Bool("no-prune", false, "keep every archive in the folder; skip retention")
	if _, err := fs.parse(args, 0, 0); err != nil {
		return err
	}
	dir := adminDataDir(*data)
	dest := *out
	if dest == "" {
		dest = filepath.Join(dir, defaultBackupSubdir)
	}
	archive, m, err := backup.Create(dir, dest, backup.Options{Label: *label, AgentVersion: version})
	if err != nil {
		return fmt.Errorf("backup: %w", err)
	}
	// Re-read what was just written: a backup that cannot be verified is not a backup.
	if _, err := backup.Verify(archive); err != nil {
		return fmt.Errorf("backup written to %s but it failed verification: %w", archive, err)
	}
	size := int64(-1)
	if info, err := os.Stat(archive); err == nil {
		size = info.Size()
	}
	// Best effort: the status file feeds GET /v1/system/backups; a failure to record it must not fail the backup.
	if rerr := backup.RecordBackup(dir, backup.BackupInfo{Time: time.Now().UTC(), Label: *label, Path: backup.RelativeArchivePath(dir, archive), Size: size, OK: true}); rerr != nil {
		fmt.Fprintf(os.Stderr, "warning: could not record the status: %v\n", rerr)
	}
	fmt.Printf("Backup written and verified: %s\n", archive)
	fmt.Printf("  size:         %d bytes\n", size)
	fmt.Printf("  fingerprint:  %s\n", m.CertFingerprint)
	fmt.Printf("  user_version: %d\n", m.UserVersion)
	if !*noPrune {
		removed, err := backup.Prune(dest, backup.DefaultPolicy())
		for _, p := range removed {
			fmt.Printf("  pruned:       %s\n", p)
		}
		if err != nil {
			return fmt.Errorf("backup is safe, but pruning old backups failed: %w", err)
		}
	}
	return nil
}

// cmdBackupVerify implements "rfe backup verify <archive>".
func cmdBackupVerify(args []string) error {
	fs := newAdminFlags("backup verify", "backup verify <archive>")
	pos, err := fs.parse(args, 1, 1)
	if err != nil {
		return err
	}
	m, err := backup.Verify(pos[0])
	if err != nil {
		return fmt.Errorf("%s is not a valid backup: %w", pos[0], err)
	}
	fmt.Printf("OK: %s\n", pos[0])
	printManifest(m)
	return nil
}

func printManifest(m *backup.Manifest) {
	fmt.Printf("  created:      %s\n", m.Created.Format("2006-01-02 15:04:05 MST"))
	fmt.Printf("  agent:        %s\n", m.AgentVersion)
	fmt.Printf("  fingerprint:  %s\n", m.CertFingerprint)
	fmt.Printf("  user_version: %d\n", m.UserVersion)
	for _, mem := range m.Members {
		fmt.Printf("  member:       %-20s %9d bytes  sha256 %s\n", mem.Name, mem.Size, mem.SHA256)
	}
}

// cmdRestore implements "rfe restore <archive> <newdir>": it never touches an existing data dir.
func cmdRestore(args []string) error {
	fs := newAdminFlags("restore", "restore <archive> <newdir> [-revocations <file>] [-data <dir>]")
	data := fs.String("data", "", "data dir whose revocations.log is replayed (default: the resolved data dir)")
	revLog := fs.String("revocations", "", "revocation log to replay (default <data dir>/"+store.RevocationsLogFile+"); revocations made after the backup are re-applied")
	pos, err := fs.parse(args, 2, 2)
	if err != nil {
		return err
	}
	archive, target := pos[0], pos[1]
	logPath := *revLog
	if logPath == "" {
		logPath = filepath.Join(adminDataDir(*data), store.RevocationsLogFile)
	}
	m, n, err := backup.RestoreWithRevocations(archive, target, logPath)
	if err != nil {
		return err
	}
	fmt.Printf("Restored %s to %s\n", archive, target)
	printManifest(m)
	fmt.Printf("  revocations re-applied from %s: %d\n", logPath, n)
	fmt.Println("Nothing was deleted or overwritten. Start the agent with -data", target, "when you are ready to switch.")
	return nil
}
