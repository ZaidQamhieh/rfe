package security

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// listDir returns the names in dir, so a test can prove nothing was created.
func listDir(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names
}

// With agent.db present and the key missing, LoadOrCreateCert must refuse and create no file, because a
// new identity would make every paired phone reject the host.
func TestLoadOrCreateCertRefusesWhenDBExistsAndKeyMissing(t *testing.T) {
	dir := t.TempDir()
	if _, err := LoadOrCreateCert(dir); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "agent.db"), []byte("db"), 0o600); err != nil {
		t.Fatal(err)
	}
	certBefore, err := os.ReadFile(filepath.Join(dir, CertFile))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(dir, KeyFile)); err != nil {
		t.Fatal(err)
	}
	before := listDir(t, dir)

	_, err = LoadOrCreateCert(dir)
	if !errors.Is(err, ErrIdentityMissing) {
		t.Fatalf("err = %v, want ErrIdentityMissing", err)
	}
	if after := listDir(t, dir); len(after) != len(before) {
		t.Fatalf("files changed: before %v, after %v", before, after)
	}
	if _, err := os.Stat(filepath.Join(dir, KeyFile)); !os.IsNotExist(err) {
		t.Fatalf("key file was created: %v", err)
	}
	certAfter, _ := os.ReadFile(filepath.Join(dir, CertFile))
	if string(certAfter) != string(certBefore) {
		t.Fatal("certificate file was rewritten")
	}
}

// Both files missing next to an existing database is the same refusal.
func TestLoadOrCreateCertRefusesWhenDBExistsAndBothMissing(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "agent.db"), []byte("db"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadOrCreateCert(dir); !errors.Is(err, ErrIdentityMissing) {
		t.Fatalf("err = %v, want ErrIdentityMissing", err)
	}
	if got := listDir(t, dir); len(got) != 1 {
		t.Fatalf("files after refusal = %v, want only agent.db", got)
	}
}

// First run (no database) still creates the identity.
func TestLoadOrCreateCertFirstRunStillCreates(t *testing.T) {
	dir := t.TempDir()
	if _, err := LoadOrCreateCert(dir); err != nil {
		t.Fatalf("first run: %v", err)
	}
	for _, f := range []string{CertFile, KeyFile} {
		if _, err := os.Stat(filepath.Join(dir, f)); err != nil {
			t.Fatalf("%s not created: %v", f, err)
		}
	}
}

// LoadCert is read-only: it reports the missing identity and never writes, with or without a database.
func TestLoadCertNeverCreates(t *testing.T) {
	for _, withDB := range []bool{false, true} {
		dir := t.TempDir()
		if withDB {
			if err := os.WriteFile(filepath.Join(dir, "agent.db"), []byte("db"), 0o600); err != nil {
				t.Fatal(err)
			}
		}
		before := listDir(t, dir)
		if _, err := LoadCert(dir); !errors.Is(err, ErrIdentityMissing) {
			t.Fatalf("withDB=%v: err = %v, want ErrIdentityMissing", withDB, err)
		}
		if after := listDir(t, dir); len(after) != len(before) {
			t.Fatalf("withDB=%v: LoadCert created files: %v -> %v", withDB, before, after)
		}
	}
}

// LoadCert returns the same certificate LoadOrCreateCert wrote.
func TestLoadCertReadsExistingIdentity(t *testing.T) {
	dir := t.TempDir()
	created, err := LoadOrCreateCert(dir)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadCert(dir)
	if err != nil {
		t.Fatal(err)
	}
	if Fingerprint(created) != Fingerprint(loaded) {
		t.Fatal("fingerprints differ")
	}
}
