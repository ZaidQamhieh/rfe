//go:build linux

package fsops

import (
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/unix"
)

// F-148: the atomic rename refuses a target that exists instead of replacing it.
func TestRenameNoReplace_RefusesExistingTarget(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a"), []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "b"), []byte("b"), 0o644); err != nil {
		t.Fatal(err)
	}
	d, err := os.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	fd := int(d.Fd())
	if err := renameNoReplace(fd, "a", fd, "b"); err != unix.EEXIST {
		t.Fatalf("err = %v, want EEXIST", err)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "b")); string(b) != "b" {
		t.Fatalf("target was replaced: %q", b)
	}
	if err := renameNoReplace(fd, "a", fd, "c"); err != nil {
		t.Fatalf("free name: %v", err)
	}
}
