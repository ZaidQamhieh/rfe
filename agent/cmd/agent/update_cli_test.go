package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ZaidQamhieh/rfe/agent/internal/store"
)

func TestGuardUpdateRootRefusesLiveDataDirs(t *testing.T) {
	t.Setenv("RFE_DATA_DIR", filepath.Join(t.TempDir(), "data"))
	for _, name := range liveDataMarkers {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := guardUpdateRoot(dir); err == nil || !strings.Contains(err.Error(), "live data dir") {
			t.Errorf("%s: err = %v", name, err)
		}
	}
	if _, err := guardUpdateRoot(defaultDataDir()); err == nil {
		t.Error("the default data dir was accepted")
	}
	if _, err := guardUpdateRoot(filepath.Join(defaultDataDir(), "install")); err == nil {
		t.Error("a folder inside the default data dir was accepted")
	}
	if _, err := guardUpdateRoot(""); err == nil {
		t.Error("an empty root was accepted")
	}
	if got, err := guardUpdateRoot(filepath.Join(t.TempDir(), "install")); err != nil || !filepath.IsAbs(got) {
		t.Errorf("fresh root: %q, %v", got, err)
	}
	if store.DBFile != "agent.db" {
		t.Errorf("store.DBFile changed to %q: update the live-data guard's expectations", store.DBFile)
	}
}

func TestUpdateCommandArguments(t *testing.T) {
	for _, args := range [][]string{nil, {"bogus"}, {"--zz-bogus"}} {
		if err := cmdUpdate(args); err == nil || !strings.Contains(err.Error(), "usage: rfe update") {
			t.Errorf("cmdUpdate(%v) = %v, want a usage error", args, err)
		}
	}
	if err := cmdUpdate([]string{"stage", "-url", "https://example.invalid/m.json"}); err == nil || !strings.Contains(err.Error(), "-root is required") {
		t.Errorf("stage without -root = %v", err)
	}
	// With no key compiled in, an update can never be accepted; the failure must come before any download.
	root := filepath.Join(t.TempDir(), "install")
	if err := cmdUpdate([]string{"check", "-url", "http://127.0.0.1:1/m.json", "-root", root}); err == nil {
		t.Error("check against a plain-http url succeeded")
	}
	if _, err := os.Stat(root); err == nil {
		t.Error("check created the root")
	}
}
