//go:build unix

package updates

import (
	"os"
	"path/filepath"
	"testing"
)

// F-57: a linked APK reports the target's size, and a dangling link does not fail the listing.
func TestLatest_LinkedAndDanglingAPKs(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(t.TempDir(), "real.bin")
	if err := os.WriteFile(target, []byte("0123456789"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(dir, "rfe-1.0.0-5.apk")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(dir, "gone"), filepath.Join(dir, "rfe-2.0.0-9.apk")); err != nil {
		t.Fatal(err)
	}
	rel, err := Latest(dir)
	if err != nil || rel == nil {
		t.Fatalf("Latest = %v, %v", rel, err)
	}
	if rel.VersionCode != 5 || rel.Size != 10 {
		t.Fatalf("release = %+v, want code 5 and the target's size 10", rel)
	}
}
