package daemon

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/ZaidQamhieh/rfe/agent/internal/sidecar"
)

// sidecarTestFile is the file name sidecar.Locate looks for given a short name.
func sidecarTestFile(short string) string {
	name := sidecar.FileName(short)
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	return name
}

// TestSidecarConfigRefusesManifestlessSidecarBesideTheAgent checks a sidecar beside the executable without a
// manifest stays off even when RFE_SIDECARS names it.
func TestSidecarConfigRefusesManifestlessSidecarBesideTheAgent(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	if resolved, e := filepath.EvalSymlinks(exe); e == nil {
		exe = resolved
	}
	t.Setenv(sidecar.DirEnv, "")
	p := filepath.Join(filepath.Dir(exe), sidecarTestFile(sidecarThumbd))
	if err := os.WriteFile(p, []byte("packaged"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Remove(p) })

	t.Setenv(envSidecars, sidecarThumbd)
	var logged int
	logf := func(string, ...any) { logged++ }
	if _, ok := sidecarConfig(sidecarThumbd, logf); ok {
		t.Fatal("a manifest-less sidecar beside the agent started although RFE_SIDECARS names it")
	}
	if logged != 1 {
		t.Fatalf("the refusal was logged %d times, want once", logged)
	}
}

// TestSidecarConfigCarriesVerifiedHash checks a verified sidecar's config holds the hash, so each restart re-checks it.
func TestSidecarConfigCarriesVerifiedHash(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(sidecar.DirEnv, dir)
	t.Setenv(envSidecars, "")
	body := []byte("daemon sidecar")
	if err := os.WriteFile(filepath.Join(dir, sidecarTestFile(sidecarIndexd)), body, 0o755); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(body)
	want := hex.EncodeToString(sum[:])
	manifest := "version 1.4.0\nsha256 " + want + " " + sidecarTestFile(sidecarIndexd) + "\n"
	if err := os.WriteFile(filepath.Join(dir, sidecar.ManifestName), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, ok := sidecarConfig(sidecarIndexd, t.Logf)
	if !ok || cfg.SHA256 != want {
		t.Fatalf("config = %+v ok=%v, want SHA256 %s", cfg, ok, want)
	}
}
