package fsops

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// P2-5: renaming a folder onto itself returns the entry and changes nothing.
func TestPass2RenameDirOntoItself(t *testing.T) {
	ops, root := setupJail(t)
	dir := filepath.Join(root, "d")
	if err := os.MkdirAll(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "sub", "f.txt"), []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}
	e, err := ops.Rename(dir, dir)
	if err != nil {
		t.Fatalf("Rename(d, d) = %v, want success", err)
	}
	if e == nil || e.Name != "d" || !e.IsDir {
		t.Errorf("entry = %+v, want folder d", e)
	}
	b, err := os.ReadFile(filepath.Join(dir, "sub", "f.txt"))
	if err != nil || string(b) != "keep" {
		t.Errorf("contents after self-rename: %q, %v", b, err)
	}
}

// P2-5: the file case stays a no-op.
func TestPass2RenameFileOntoItself(t *testing.T) {
	ops, root := setupJail(t)
	f := filepath.Join(root, "f.txt")
	if err := os.WriteFile(f, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := ops.Rename(f, f); err != nil {
		t.Fatalf("Rename(f, f) = %v, want success", err)
	}
	if b, err := os.ReadFile(f); err != nil || string(b) != "x" {
		t.Errorf("file after self-rename: %q, %v", b, err)
	}
}

// P2-6: Checksum of a folder wraps ErrIsDir.
func TestPass2ChecksumDirIsErrIsDir(t *testing.T) {
	ops, root := setupJail(t)
	if _, err := ops.Checksum(root, "sha256"); !errors.Is(err, ErrIsDir) {
		t.Errorf("Checksum(dir) err = %v, want ErrIsDir", err)
	}
}

// P2-7: a missing source is not found even when the destination is free.
func TestPass2RenameMissingSourceNestedDst(t *testing.T) {
	ops, root := setupJail(t)
	dst := filepath.Join(root, "newdir", "x.txt")
	if _, err := ops.Rename(filepath.Join(root, "nope"), dst); !errors.Is(err, ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
	if _, err := os.Lstat(filepath.Dir(dst)); err == nil {
		t.Errorf("destination parent created for a missing source")
	}
}
