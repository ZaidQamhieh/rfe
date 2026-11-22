package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ZaidQamhieh/rfe/agent/internal/security"
	"github.com/ZaidQamhieh/rfe/agent/internal/store"
)

// dirWithDBOnly is a data dir that has a database but lost its certificate and key.
func dirWithDBOnly(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	db, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	return dir
}

// "rfe status" only reports on a missing identity; it must never create the certificate or key.
func TestStatusNeverCreatesTheIdentity(t *testing.T) {
	dir := dirWithDBOnly(t)
	out, err := captureStdout(t, func() error { return cmdStatus([]string{"-data", dir, "-addr", "127.0.0.1:8765"}) })
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if !strings.Contains(out, "(no cert yet)") {
		t.Fatalf("status output does not report the missing certificate:\n%s", out)
	}
	for _, f := range []string{security.CertFile, security.KeyFile} {
		if _, err := os.Stat(filepath.Join(dir, f)); !os.IsNotExist(err) {
			t.Fatalf("status created %s (stat err %v)", f, err)
		}
	}
}

// "rfe pair" next to an existing database but no identity refuses, and leaves no new files.
func TestPairRefusesWithoutIdentityAndCreatesNothing(t *testing.T) {
	dir := dirWithDBOnly(t)
	err := cmdPair([]string{"-data", dir, "-addr", "127.0.0.1:8765"})
	if !errors.Is(err, security.ErrIdentityMissing) {
		t.Fatalf("pair err = %v, want ErrIdentityMissing", err)
	}
	for _, f := range []string{security.CertFile, security.KeyFile} {
		if _, err := os.Stat(filepath.Join(dir, f)); !os.IsNotExist(err) {
			t.Fatalf("pair created %s (stat err %v)", f, err)
		}
	}
}
