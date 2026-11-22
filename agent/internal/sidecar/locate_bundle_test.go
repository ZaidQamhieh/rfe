package sidecar

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/ZaidQamhieh/rfe/agent/internal/bundle"
)

// A helper inside a launcher bundle (manifest.json, no rfe-sidecars.txt) is vouched for by that manifest, and a
// changed file is refused with an error so the agent runs without the helper instead of starting it.
func TestLocateVerifiesBundleManifest(t *testing.T) {
	dir := t.TempDir()
	exe := FileName("indexd")
	if runtime.GOOS == "windows" {
		exe += windowsExeSuffix
	}
	if err := os.WriteFile(filepath.Join(dir, exe), []byte("helper"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := bundle.Build(dir, "2.0.0", runtime.GOOS, runtime.GOARCH); err != nil {
		t.Fatal(err)
	}
	t.Setenv(DirEnv, dir)
	loc, ok, err := Locate("indexd")
	if err != nil || !ok || !loc.Verified || loc.Version != "2.0.0" || len(loc.SHA256) != 64 {
		t.Fatalf("got %+v ok=%v err=%v", loc, ok, err)
	}
	if err := VerifyFile(loc.Path, loc.SHA256); err != nil {
		t.Fatalf("start-time check of the located file: %v", err)
	}

	if err := os.WriteFile(filepath.Join(dir, exe), []byte("HELPER"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := Locate("indexd"); ok || !errors.Is(err, bundle.ErrMismatch) {
		t.Fatalf("tampered helper accepted: ok=%v err=%v", ok, err)
	}
	if _, ok, err := LocateToRun("indexd", true); ok || err == nil || !strings.Contains(err.Error(), "does not match") {
		t.Fatalf("LocateToRun: ok=%v err=%v", ok, err)
	}
}
