package bundle

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func writeFiles(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	for name, body := range files {
		p := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o755); err != nil {
			t.Fatal(err)
		}
	}
}

func TestBuildVerifyRoundTrip(t *testing.T) {
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{"rfe": "agent", "rfe-indexd": "indexd", "sub/x.txt": "x"})
	m, err := Build(dir, "1.2.3", "linux", "amd64")
	if err != nil {
		t.Fatal(err)
	}
	if m.Version != "1.2.3" || m.GOOS != "linux" || m.GOARCH != "amd64" || len(m.Files) != 3 {
		t.Fatalf("manifest = %+v", m)
	}
	if m.Files[0].Path != "rfe" || m.Files[0].Size != 5 || len(m.Files[0].SHA256) != 64 {
		t.Fatalf("first entry = %+v", m.Files[0])
	}
	if _, err := Verify(dir); err != nil {
		t.Fatal(err)
	}
	// Building again over an existing manifest must not list the manifest itself.
	if m2, err := Build(dir, "1.2.3", "linux", "amd64"); err != nil || len(m2.Files) != 3 {
		t.Fatalf("rebuild: %v %+v", err, m2)
	}
}

func TestVerifyDetectsTamperingAndMissing(t *testing.T) {
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{"rfe": "agent", "helper": "h"})
	if _, err := Build(dir, "v1", "linux", "amd64"); err != nil {
		t.Fatal(err)
	}
	writeFiles(t, dir, map[string]string{"helper": "H"}) // same size, different bytes
	if _, err := Verify(dir); !errors.Is(err, ErrMismatch) {
		t.Fatalf("tampered: %v", err)
	}
	if err := VerifyFile(dir, "rfe"); err != nil {
		t.Fatalf("untouched file must still verify: %v", err)
	}
	if err := VerifyFile(dir, "helper"); !errors.Is(err, ErrMismatch) {
		t.Fatalf("VerifyFile tampered: %v", err)
	}
	os.Remove(filepath.Join(dir, "helper"))
	if err := VerifyFile(dir, "helper"); !errors.Is(err, ErrMismatch) {
		t.Fatalf("VerifyFile missing: %v", err)
	}
	if err := VerifyFile(dir, "unlisted"); !errors.Is(err, ErrMismatch) {
		t.Fatalf("VerifyFile unlisted: %v", err)
	}
}

func TestVerifyFileRejectsEscapingPaths(t *testing.T) {
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{"rfe": "a"})
	if _, err := Build(dir, "v1", "linux", "amd64"); err != nil {
		t.Fatal(err)
	}
	for _, rel := range []string{"../rfe", "/etc/passwd", "a/../rfe", "", ".", "a\\b"} {
		if err := VerifyFile(dir, rel); err == nil {
			t.Errorf("VerifyFile(%q) succeeded", rel)
		}
	}
}

func TestReadRejectsBadManifests(t *testing.T) {
	dir := t.TempDir()
	if _, err := Read(dir); !errors.Is(err, ErrNoManifest) || !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing: %v", err)
	}
	for name, body := range map[string]string{
		"corrupt":   "{nope",
		"noversion": `{"files":[]}`,
		"escape":    `{"version":"v","files":[{"path":"../x","sha256":"00","size":0,"mode":0}]}`,
	} {
		os.WriteFile(filepath.Join(dir, ManifestName), []byte(body), 0o600)
		if _, err := Read(dir); err == nil {
			t.Errorf("%s: Read succeeded", name)
		}
	}
}

func TestBuildRefusesSymlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks need privileges on windows")
	}
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{"rfe": "a"})
	if err := os.Symlink(filepath.Join(dir, "rfe"), filepath.Join(dir, "link")); err != nil {
		t.Fatal(err)
	}
	if _, err := Build(dir, "v1", "linux", "amd64"); err == nil {
		t.Fatal("Build accepted a symlink")
	}
}

func TestVerifyFileRefusesSymlinkReplacement(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks need privileges on windows")
	}
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{"rfe": "a", "other": "a"})
	if _, err := Build(dir, "v1", "linux", "amd64"); err != nil {
		t.Fatal(err)
	}
	os.Remove(filepath.Join(dir, "rfe"))
	if err := os.Symlink(filepath.Join(dir, "other"), filepath.Join(dir, "rfe")); err != nil {
		t.Fatal(err)
	}
	if err := VerifyFile(dir, "rfe"); !errors.Is(err, ErrMismatch) {
		t.Fatalf("symlinked file with identical bytes verified: %v", err)
	}
}
