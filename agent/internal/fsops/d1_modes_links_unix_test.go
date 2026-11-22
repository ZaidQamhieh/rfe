//go:build !windows

package fsops

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Regression tests for the publish/rename findings: an overwriting upload keeps
// the replaced file's mode, a new file gets the default mode on every publish
// path, and renaming onto another hard link of the same file is refused.

func d1Mode(t *testing.T, p string) os.FileMode {
	t.Helper()
	st, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}
	return st.Mode().Perm()
}

func TestD1PublishOverwriteKeepsExistingMode(t *testing.T) {
	dir := t.TempDir()
	for _, mode := range []os.FileMode{0o755, 0o600} {
		target := filepath.Join(dir, "target-"+mode.String())
		if err := os.WriteFile(target, []byte("old"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(target, mode); err != nil {
			t.Fatal(err)
		}
		info, err := New([]string{dir}, false).PublishReader(target, strings.NewReader("new content"), true)
		if err != nil {
			t.Fatal(err)
		}
		if got := d1Mode(t, target); got != mode {
			t.Errorf("mode after overwrite = %v, want the existing %v", got, mode)
		}
		if info.Mode().Perm() != mode {
			t.Errorf("returned info mode = %v, want %v", info.Mode().Perm(), mode)
		}
		if b, _ := os.ReadFile(target); string(b) != "new content" {
			t.Errorf("content = %q", b)
		}
	}
}

func TestD1PublishNewFileGetsDefaultMode(t *testing.T) {
	dir := t.TempDir()
	ops := New([]string{dir}, false)
	for _, overwrite := range []bool{false, true} {
		target := filepath.Join(dir, "new-"+map[bool]string{false: "noreplace", true: "overwrite"}[overwrite])
		if _, err := ops.PublishReader(target, strings.NewReader("x"), overwrite); err != nil {
			t.Fatal(err)
		}
		if got := d1Mode(t, target); got != newFilePerm {
			t.Errorf("overwrite=%v: new file mode = %v, want %v", overwrite, got, newFilePerm)
		}
	}
}

// The O_EXCL copy fallback (no hard links) must give the same mode as the hard-link path.
func TestD1CopyNoReplaceFallbackGetsDefaultMode(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "tmp-src")
	if err := os.WriteFile(src, []byte("payload"), 0o600); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(dir, "published")
	if err := copyNoReplaceRooted(&securePath{full: src}, &securePath{full: dst}); err != nil {
		t.Fatal(err)
	}
	if got := d1Mode(t, dst); got != newFilePerm {
		t.Errorf("fallback file mode = %v, want %v", got, newFilePerm)
	}
	if b, _ := os.ReadFile(dst); string(b) != "payload" {
		t.Errorf("content = %q", b)
	}
	if err := copyNoReplaceRooted(&securePath{full: src}, &securePath{full: dst}); !errors.Is(err, os.ErrExist) {
		t.Errorf("second copy onto the existing name: err = %v, want ErrExist", err)
	}
}

// rename(2) onto another hard link of the same file does nothing and reports
// success, leaving both names; Rename must refuse it as it refuses any
// existing destination.
func TestD1RenameOntoHardLinkOfSourceIsConflict(t *testing.T) {
	for _, jailed := range []bool{true, false} {
		root := t.TempDir()
		var ops *Ops
		if jailed {
			ops = New([]string{root}, false)
		} else {
			ops = New(nil, false)
		}
		a, b := filepath.Join(root, "a.txt"), filepath.Join(root, "b.txt")
		if err := os.WriteFile(a, []byte("shared"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Link(a, b); err != nil {
			t.Skipf("hard links unavailable: %v", err)
		}
		if _, err := ops.Rename(a, b); !errors.Is(err, ErrConflict) {
			t.Errorf("jailed=%v: Rename onto a hard link of itself: err = %v, want ErrConflict", jailed, err)
		}
		for _, p := range []string{a, b} {
			if b, err := os.ReadFile(p); err != nil || string(b) != "shared" {
				t.Errorf("jailed=%v: %s = %q, %v; want it untouched", jailed, p, b, err)
			}
		}
		// Renaming an entry onto itself stays a no-op.
		if _, err := ops.Rename(a, a); err != nil {
			t.Errorf("jailed=%v: Rename onto itself: %v", jailed, err)
		}
	}
}
