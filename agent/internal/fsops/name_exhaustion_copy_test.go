package fsops

// Pass-2 regression tests for the keep-both name search and the exclusive
// copy. Platform-independent; the no-replace rename and permission cases are in
// pass2_regression_unix_test.go.

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func pass2Write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func pass2Read(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%s is no longer readable: %v", path, err)
	}
	return string(b)
}

// P2-1: Compress with every "x (N).zip" taken fails with a conflict and keeps
// the entry at the last candidate name.
func TestPass2_CompressAtNameExhaustionKeepsLastName(t *testing.T) {
	ops, root := setupJail(t)
	src := filepath.Join(root, "in", "f.txt")
	pass2Write(t, src, "payload")
	dest := filepath.Join(root, "out", "x.zip")
	pass2Write(t, dest, "base")
	for i := 1; i <= maxAutoRename; i++ {
		pass2Write(t, filepath.Join(root, "out", fmt.Sprintf("x (%d).zip", i)), "keep")
	}

	if _, err := ops.Compress([]string{src}, dest); !errors.Is(err, ErrConflict) {
		t.Fatalf("Compress at exhaustion: err = %v, want ErrConflict", err)
	}
	last := filepath.Join(root, "out", fmt.Sprintf("x (%d).zip", maxAutoRename))
	if got := pass2Read(t, last); got != "keep" {
		t.Fatalf("Compress replaced %s (now %q)", last, got)
	}
	if got := pass2Read(t, dest); got != "base" {
		t.Fatalf("Compress replaced %s (now %q)", dest, got)
	}
}

// P2-1: autoRenameSecure and uniqueTrashName report exhaustion as a conflict.
func TestPass2_AutoRenameExhaustionIsConflict(t *testing.T) {
	dir := t.TempDir()
	for i := 1; i <= maxAutoRename; i++ {
		pass2Write(t, filepath.Join(dir, fmt.Sprintf("a (%d).txt", i)), "x")
	}
	pass2Write(t, filepath.Join(dir, "a.txt"), "x")
	got, err := autoRenameSecure(&securePath{full: filepath.Join(dir, "a.txt")})
	if !errors.Is(err, ErrConflict) || got != nil {
		t.Fatalf("autoRenameSecure = (%v, %v), want (nil, ErrConflict)", got, err)
	}
	// One free name below the limit is still found.
	if err := os.Remove(filepath.Join(dir, "a (7).txt")); err != nil {
		t.Fatal(err)
	}
	got, err = autoRenameSecure(&securePath{full: filepath.Join(dir, "a.txt")})
	if err != nil || filepath.Base(got.full) != "a (7).txt" {
		t.Fatalf("autoRenameSecure = (%v, %v), want a (7).txt", got, err)
	}
}

// P2-3: a copy never writes into a destination that exists, and reports that it
// created nothing so no caller removes the occupant.
func TestPass2_CopyTreeNeverTouchesExistingDestination(t *testing.T) {
	base := t.TempDir()
	srcFile := filepath.Join(base, "src.txt")
	pass2Write(t, srcFile, "new")
	srcDir := filepath.Join(base, "srcdir")
	pass2Write(t, filepath.Join(srcDir, "f.txt"), "new")

	occupantFile := filepath.Join(base, "occ.txt")
	pass2Write(t, occupantFile, "occupant")
	occupantDir := filepath.Join(base, "occdir")
	pass2Write(t, filepath.Join(occupantDir, "f.txt"), "occupant")

	cases := []struct {
		name     string
		src, dst string
		check    string
	}{
		{"file onto file", srcFile, occupantFile, occupantFile},
		{"folder onto folder", srcDir, occupantDir, filepath.Join(occupantDir, "f.txt")},
		{"file onto folder", srcFile, occupantDir, filepath.Join(occupantDir, "f.txt")},
	}
	for _, c := range cases {
		made, err := copyTree(&securePath{full: c.src}, &securePath{full: c.dst})
		if made || !errors.Is(err, ErrConflict) {
			t.Errorf("%s: copyTree = (%v, %v), want (false, ErrConflict)", c.name, made, err)
		}
		if got := pass2Read(t, c.check); got != "occupant" {
			t.Errorf("%s: occupant now %q", c.name, got)
		}
	}
}
