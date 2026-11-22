package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ZaidQamhieh/rfe/agent/internal/backup"
	"github.com/ZaidQamhieh/rfe/agent/internal/security"
	"github.com/ZaidQamhieh/rfe/agent/internal/store"
)

func onlyArchive(t *testing.T, dir string) string {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(dir, "backups", "rfe-backup-*.tar.gz"))
	if err != nil || len(matches) != 1 {
		t.Fatalf("archives in %s/backups = %v (err %v), want exactly one", dir, matches, err)
	}
	return matches[0]
}

func TestBackupVerifyRestoreCommands(t *testing.T) {
	dir := initializeAdminData(t)
	db, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.CreateDevice("dev-1", "phone", "tok-1"); err != nil {
		t.Fatal(err)
	}
	if err := db.CreateDevice("dev-2", "tablet", "tok-2"); err != nil {
		t.Fatal(err)
	}
	db.Close()

	out, err := captureStdout(t, func() error { return runAdmin("backup", []string{"-data", dir}) })
	if err != nil {
		t.Fatalf("backup: %v\n%s", err, out)
	}
	archive := onlyArchive(t, dir)
	if !strings.Contains(out, archive) || !strings.Contains(out, "verified") {
		t.Fatalf("backup output does not name the verified archive:\n%s", out)
	}

	out, err = captureStdout(t, func() error { return runAdmin("backup", []string{"verify", archive}) })
	if err != nil || !strings.Contains(out, "OK") || !strings.Contains(out, "fingerprint") {
		t.Fatalf("backup verify = %v\n%s", err, out)
	}

	// A revocation made after the backup is re-applied by restore.
	db, err = store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.RevokeDevice("dev-2"); err != nil {
		t.Fatal(err)
	}
	db.Close()

	target := filepath.Join(t.TempDir(), "restored")
	out, err = captureStdout(t, func() error { return runAdmin("restore", []string{archive, target, "-data", dir}) })
	if err != nil {
		t.Fatalf("restore: %v\n%s", err, out)
	}
	if !strings.Contains(out, "revocations re-applied") || !strings.Contains(out, ": 1") {
		t.Fatalf("restore output:\n%s", out)
	}
	rdb, err := store.Open(target)
	if err != nil {
		t.Fatal(err)
	}
	devs, err := rdb.ListDevices()
	rdb.Close()
	if err != nil || len(devs) != 2 {
		t.Fatalf("restored devices = %v, %v", devs, err)
	}
	for _, d := range devs {
		if d.ID == "dev-2" && !d.Revoked {
			t.Fatal("dev-2 was not revoked after restore")
		}
		if d.ID == "dev-1" && d.Revoked {
			t.Fatal("dev-1 was revoked by restore")
		}
	}
	for _, name := range []string{security.CertFile, security.KeyFile} {
		a, _ := os.ReadFile(filepath.Join(dir, name))
		b, _ := os.ReadFile(filepath.Join(target, name))
		if len(a) == 0 || string(a) != string(b) {
			t.Fatalf("%s differs after restore", name)
		}
	}

	// A second restore into the now populated dir is refused and changes nothing.
	err = discardStderr(t, func() error {
		_, e := captureStdout(t, func() error { return runAdmin("restore", []string{archive, target, "-data", dir}) })
		return e
	})
	if !errors.Is(err, backup.ErrRestoreTargetNotEmpty) {
		t.Fatalf("restore into a populated dir = %v, want ErrRestoreTargetNotEmpty", err)
	}
}

func TestBackupCommandErrors(t *testing.T) {
	dir := initializeAdminData(t)
	if err := os.Remove(filepath.Join(dir, security.KeyFile)); err != nil {
		t.Fatal(err)
	}
	err := discardStderr(t, func() error {
		_, e := captureStdout(t, func() error { return runAdmin("backup", []string{"-data", dir}) })
		return e
	})
	if !errors.Is(err, security.ErrIdentityMissing) {
		t.Fatalf("backup without a key = %v, want ErrIdentityMissing", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "backups")); err == nil {
		t.Fatal("a failed backup created the backups folder")
	}

	bogus := filepath.Join(t.TempDir(), "nope.tar.gz")
	if err := os.WriteFile(bogus, []byte("not an archive"), 0o600); err != nil {
		t.Fatal(err)
	}
	err = discardStderr(t, func() error {
		_, e := captureStdout(t, func() error { return runAdmin("backup", []string{"verify", bogus}) })
		return e
	})
	if err == nil || !strings.Contains(err.Error(), bogus) {
		t.Fatalf("verify of a bad file = %v, want an error naming it", err)
	}
	err = discardStderr(t, func() error {
		_, e := captureStdout(t, func() error { return runAdmin("restore", []string{bogus}) })
		return e
	})
	if err == nil || !strings.Contains(err.Error(), "missing argument") {
		t.Fatalf("restore with one argument = %v", err)
	}
}

func TestBackupLabelAndOutFlags(t *testing.T) {
	dir := initializeAdminData(t)
	out := filepath.Join(t.TempDir(), "elsewhere")
	if _, err := captureStdout(t, func() error {
		return runAdmin("backup", []string{"-data", dir, "-out", out, "-label", "daily"})
	}); err != nil {
		t.Fatal(err)
	}
	matches, _ := filepath.Glob(filepath.Join(out, "rfe-backup-daily-*.tar.gz"))
	if len(matches) != 1 {
		t.Fatalf("archives = %v, want one daily archive in -out", matches)
	}
}
