package sidecar

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func installFake(t *testing.T, manifestBody func(sum string) string) string {
	t.Helper()
	dir := t.TempDir()
	exe := FileName("indexd")
	if runtime.GOOS == "windows" {
		exe += windowsExeSuffix
	}
	body := []byte("fake sidecar")
	if err := os.WriteFile(filepath.Join(dir, exe), body, 0o755); err != nil {
		t.Fatal(err)
	}
	if manifestBody != nil {
		sum := sha256.Sum256(body)
		text := manifestBody(hex.EncodeToString(sum[:]))
		if err := os.WriteFile(filepath.Join(dir, ManifestName), []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv(DirEnv, dir)
	return dir
}

func TestLocateVerifiesListedHash(t *testing.T) {
	installFake(t, func(sum string) string {
		return "rfe-sidecars 1\nversion 1.4.0\nsha256 " + strings.ToUpper(sum) + " rfe-indexd\n"
	})
	loc, ok, err := Locate("indexd")
	if err != nil || !ok || !loc.Verified || loc.Version != "1.4.0" {
		t.Fatalf("got %+v ok=%v err=%v", loc, ok, err)
	}
}

func TestLocateRefusesMismatchAndUnlisted(t *testing.T) {
	installFake(t, func(string) string {
		return "version 1\nsha256 " + strings.Repeat("0", 64) + " rfe-indexd\n"
	})
	if _, ok, err := Locate("indexd"); ok || err == nil || !strings.Contains(err.Error(), "does not match") {
		t.Fatalf("mismatch accepted: ok=%v err=%v", ok, err)
	}
	installFake(t, func(string) string { return "version 1\n" })
	if _, ok, err := Locate("indexd"); ok || err == nil || !strings.Contains(err.Error(), "not listed") {
		t.Fatalf("unlisted accepted: ok=%v err=%v", ok, err)
	}
}

func TestLocateWithoutManifestIsUnverified(t *testing.T) {
	installFake(t, nil)
	loc, ok, err := Locate("indexd")
	if err != nil || !ok || loc.Verified {
		t.Fatalf("got %+v ok=%v err=%v", loc, ok, err)
	}
}

// A relative RFE_SIDECAR_DIR must yield an absolute path, or exec would look the bare name up on PATH.
func TestLocateReturnsAbsolutePath(t *testing.T) {
	dir := installFake(t, nil)
	t.Chdir(dir)
	t.Setenv(DirEnv, ".")
	loc, ok, err := Locate("indexd")
	if err != nil || !ok || !filepath.IsAbs(loc.Path) {
		t.Fatalf("got %+v ok=%v err=%v", loc, ok, err)
	}
}
