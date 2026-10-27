//go:build unix

package fsops

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

// F-41/F-120: an overwrite that fails part-way must leave the old destination in place.
func TestCopyOverwrite_FailedCopyKeepsDestination(t *testing.T) {
	ops, root := setupJail(t)
	src := filepath.Join(root, "src", "item")
	dstDir := filepath.Join(root, "dst")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dstDir, "item"), 0o755); err != nil {
		t.Fatal(err)
	}
	keep := filepath.Join(dstDir, "item", "old.txt")
	if err := os.WriteFile(keep, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "a.txt"), []byte("new"), 0o644); err != nil {
		t.Fatal(err)
	}
	// A FIFO inside the source makes the copy fail after it has started.
	if err := syscall.Mkfifo(filepath.Join(src, "zz-pipe"), 0o644); err != nil {
		t.Skip("mkfifo unavailable")
	}
	res := ops.Copy([]string{src}, dstDir, false, true)
	if res[0].OK {
		t.Fatalf("copy of a tree holding a FIFO should fail: %+v", res[0])
	}
	if b, err := os.ReadFile(keep); err != nil || string(b) != "old" {
		t.Fatalf("destination lost after a failed overwrite: %v %q", err, b)
	}
	ents, _ := os.ReadDir(dstDir)
	if len(ents) != 1 {
		t.Fatalf("leftover aside/partial entries: %v", ents)
	}
}

func TestCopyOverwrite_SuccessReplacesAndCleansUp(t *testing.T) {
	ops, root := setupJail(t)
	src := filepath.Join(root, "s.txt")
	dstDir := filepath.Join(root, "d")
	_ = os.MkdirAll(dstDir, 0o755)
	_ = os.WriteFile(src, []byte("new"), 0o644)
	_ = os.WriteFile(filepath.Join(dstDir, "s.txt"), []byte("old"), 0o644)
	res := ops.Copy([]string{src}, dstDir, false, true)
	if !res[0].OK {
		t.Fatalf("copy: %+v", res[0])
	}
	if b, _ := os.ReadFile(filepath.Join(dstDir, "s.txt")); string(b) != "new" {
		t.Fatalf("not replaced: %q", b)
	}
	if ents, _ := os.ReadDir(dstDir); len(ents) != 1 {
		t.Fatalf("aside copy left behind: %v", ents)
	}
}

// F-144: a read-only source folder is copied with its contents and keeps its mode.
func TestCopy_ReadOnlySourceFolder(t *testing.T) {
	ops, root := setupJail(t)
	src := filepath.Join(root, "ro")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "a.txt"), []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(src, 0o555); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(src, 0o755)
	dstDir := filepath.Join(root, "out")
	_ = os.MkdirAll(dstDir, 0o755)
	res := ops.Copy([]string{src}, dstDir, false, false)
	if !res[0].OK {
		t.Fatalf("copy: %+v", res[0])
	}
	copied := filepath.Join(dstDir, "ro")
	defer os.Chmod(copied, 0o755)
	if b, err := os.ReadFile(filepath.Join(copied, "a.txt")); err != nil || string(b) != "a" {
		t.Fatalf("content: %v %q", err, b)
	}
	if fi, _ := os.Stat(copied); fi.Mode().Perm() != 0o555 {
		t.Fatalf("mode = %v, want 0555", fi.Mode().Perm())
	}
}
