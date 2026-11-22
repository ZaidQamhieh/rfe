package server

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/ZaidQamhieh/rfe/agent/internal/sidecar"
)

func sidecarExeName(name string) string {
	if runtime.GOOS == "windows" {
		return name + ".exe"
	}
	return name
}

func writeSidecar(t *testing.T, dir, name, content string) string {
	t.Helper()
	p := filepath.Join(dir, sidecarExeName(name))
	if err := os.WriteFile(p, []byte(content), 0o755); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte(content))
	return hex.EncodeToString(sum[:])
}

func writeManifest(t *testing.T, dir, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, sidecar.ManifestName), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestLocateSidecarVerifiesManifest(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("RFE_SIDECAR_DIR", dir)
	t.Setenv("RFE_SIDECARS", "")
	sum := writeSidecar(t, dir, "rfe-indexd", "binary-one")
	writeManifest(t, dir, "rfe-sidecars 1\nversion 1.4.0\nsha256 "+strings.ToUpper(sum)+" "+sidecarExeName("rfe-indexd")+"\n")

	loc, ok, err := locateSidecar("indexd")
	if err != nil || !ok || !loc.Verified || loc.Version != "1.4.0" {
		t.Fatalf("locate = %+v ok=%v err=%v", loc, ok, err)
	}
	cfg, ok := sidecarConfig("indexd")
	if !ok || cfg.ExpectVersion != "1.4.0" {
		t.Fatalf("config = %+v ok=%v", cfg, ok)
	}
	if !sidecarEnabled("indexd") {
		t.Fatal("a verified packaged sidecar should be on by default")
	}
	if sidecarEnabled("thumbd") {
		t.Fatal("thumbd is not installed")
	}
	t.Setenv("RFE_SIDECARS", "off")
	if sidecarEnabled("indexd") {
		t.Fatal("RFE_SIDECARS=off must disable")
	}
}

func TestLocateSidecarRefusesMismatch(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("RFE_SIDECAR_DIR", dir)
	t.Setenv("RFE_SIDECARS", "")
	sum := writeSidecar(t, dir, "rfe-indexd", "binary-one")
	writeManifest(t, dir, "version 1.4.0\nsha256 "+sum+" "+sidecarExeName("rfe-indexd")+"\n")
	writeSidecar(t, dir, "rfe-indexd", "tampered")

	if _, ok, err := locateSidecar("indexd"); ok || err == nil || !strings.Contains(err.Error(), "does not match") {
		t.Fatalf("mismatch: ok=%v err=%v", ok, err)
	}
	if _, ok := sidecarConfig("indexd"); ok {
		t.Fatal("config must refuse a tampered sidecar")
	}
	if sidecarEnabled("indexd") {
		t.Fatal("a tampered sidecar must not be enabled by default")
	}
	t.Setenv("RFE_SIDECARS", "indexd")
	if _, ok := sidecarConfig("indexd"); ok {
		t.Fatal("even an explicit RFE_SIDECARS must not run a tampered sidecar")
	}
}

func TestLocateSidecarUnlistedAndNoManifest(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("RFE_SIDECAR_DIR", dir)
	t.Setenv("RFE_SIDECARS", "")
	writeSidecar(t, dir, "rfe-thumbd", "x")

	loc, ok, err := locateSidecar("thumbd")
	if err != nil || !ok || loc.Verified {
		t.Fatalf("no manifest: %+v ok=%v err=%v", loc, ok, err)
	}
	if sidecarEnabled("thumbd") {
		t.Fatal("an unverified sidecar is not on by default")
	}
	t.Setenv("RFE_SIDECARS", "thumbd")
	if !sidecarEnabled("thumbd") {
		t.Fatal("explicit opt-in should enable an unverified sidecar")
	}
	if cfg, ok := sidecarConfig("thumbd"); !ok || cfg.ExpectVersion != "" {
		t.Fatalf("unverified config = %+v ok=%v", cfg, ok)
	}

	writeManifest(t, dir, "version 1.4.0\n")
	if _, ok, err := locateSidecar("thumbd"); ok || err == nil || !strings.Contains(err.Error(), "not listed") {
		t.Fatalf("unlisted: ok=%v err=%v", ok, err)
	}
	writeManifest(t, dir, "sha256 abc rfe-thumbd\n")
	if _, _, err := locateSidecar("thumbd"); err == nil {
		t.Fatal("a manifest without a version is invalid")
	}
}

// A packaged install whose manifest is gone must stay off even with RFE_SIDECARS naming the sidecar: the file sits
// beside the agent executable, not in $RFE_SIDECAR_DIR.
func TestSidecarConfigRefusesManifestlessSidecarBesideTheAgent(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	if resolved, e := filepath.EvalSymlinks(exe); e == nil {
		exe = resolved
	}
	t.Setenv("RFE_SIDECAR_DIR", "")
	p := filepath.Join(filepath.Dir(exe), sidecarExeName("rfe-thumbd"))
	if err := os.WriteFile(p, []byte("packaged"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Remove(p) })

	t.Setenv("RFE_SIDECARS", "thumbd")
	if _, ok := sidecarConfig("thumbd"); ok {
		t.Fatal("a sidecar beside the agent with no manifest must not start, even when RFE_SIDECARS names it")
	}
	if got := strings.Join(SidecarReport(), "\n"); !strings.Contains(got, "NOT used") {
		t.Fatalf("report does not say the sidecar is refused:\n%s", got)
	}
}

// RFE_SIDECARS without a manifest is the dev-build mode only for a sidecar in $RFE_SIDECAR_DIR, and only when set.
func TestSidecarConfigDevBuildNeedsExplicitSetting(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("RFE_SIDECAR_DIR", dir)
	writeSidecar(t, dir, "rfe-thumbd", "dev")
	t.Setenv("RFE_SIDECARS", "")
	if _, ok := sidecarConfig("thumbd"); ok {
		t.Fatal("an unverified dev sidecar must not start unless RFE_SIDECARS is set")
	}
	t.Setenv("RFE_SIDECARS", "thumbd")
	if _, ok := sidecarConfig("thumbd"); !ok {
		t.Fatal("explicit RFE_SIDECARS with RFE_SIDECAR_DIR keeps dev builds working")
	}
}

// The config of a verified sidecar carries the hash, so every restart re-checks the file.
func TestSidecarConfigCarriesVerifiedHash(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("RFE_SIDECAR_DIR", dir)
	t.Setenv("RFE_SIDECARS", "")
	sum := writeSidecar(t, dir, "rfe-indexd", "binary-one")
	writeManifest(t, dir, "version 1.4.0\nsha256 "+strings.ToUpper(sum)+" "+sidecarExeName("rfe-indexd")+"\n")
	cfg, ok := sidecarConfig("indexd")
	if !ok || cfg.SHA256 != sum {
		t.Fatalf("config = %+v ok=%v, want SHA256 %s", cfg, ok, sum)
	}
}

func TestSidecarReport(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("RFE_SIDECAR_DIR", dir)
	sum := writeSidecar(t, dir, "rfe-indexd", "b")
	writeManifest(t, dir, "version 2.0.0\nsha256 "+sum+" "+sidecarExeName("rfe-indexd")+"\n")
	got := strings.Join(SidecarReport(), "\n")
	if !strings.Contains(got, "rfe-indexd: verified, version 2.0.0") || !strings.Contains(got, "rfe-thumbd: ") {
		t.Fatalf("report:\n%s", got)
	}
}
