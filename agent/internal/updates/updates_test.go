package updates

import (
	"os"
	"path/filepath"
	"testing"
)

func writeAPK(t *testing.T, dir, name string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte("dummy"), 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
}

func TestLatest_PicksHighestVersionCode(t *testing.T) {
	dir := t.TempDir()
	writeAPK(t, dir, "rfe-1.0.0-1.apk")
	writeAPK(t, dir, "rfe-1.2.0-12.apk")
	writeAPK(t, dir, "rfe-1.1.0-9.apk")
	writeAPK(t, dir, "notes.txt") // ignored

	rel, err := Latest(dir)
	if err != nil {
		t.Fatalf("latest: %v", err)
	}
	if rel == nil {
		t.Fatal("expected a release")
	}
	if rel.VersionCode != 12 || rel.VersionName != "1.2.0" {
		t.Fatalf("expected 1.2.0/12, got %s/%d", rel.VersionName, rel.VersionCode)
	}
	if rel.Filename != "rfe-1.2.0-12.apk" {
		t.Fatalf("wrong filename: %s", rel.Filename)
	}
	if rel.Size != 5 {
		t.Fatalf("expected size 5, got %d", rel.Size)
	}
}

func TestLatest_EmptyDirReturnsNil(t *testing.T) {
	rel, err := Latest(t.TempDir())
	if err != nil {
		t.Fatalf("latest: %v", err)
	}
	if rel != nil {
		t.Fatalf("expected nil, got %+v", rel)
	}
}

func TestLatest_MissingDirReturnsNil(t *testing.T) {
	rel, err := Latest(filepath.Join(t.TempDir(), "does-not-exist"))
	if err != nil {
		t.Fatalf("expected no error for missing dir, got %v", err)
	}
	if rel != nil {
		t.Fatal("expected nil for missing dir")
	}
}
