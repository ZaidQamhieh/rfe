package fsops

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// Renaming an entry onto its own path is a no-op: no error, the entry keeps its
// name and kind, and its contents survive (folder and file).
func TestRenameOntoItselfKeepsContents(t *testing.T) {
	cases := []struct {
		name  string
		isDir bool
	}{
		{"folder", true},
		{"file", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ops, root := setupJail(t)
			p := filepath.Join(root, "p")
			keep := p
			if c.isDir {
				keep = filepath.Join(p, "sub", "f.txt")
				if err := os.MkdirAll(filepath.Dir(keep), 0o755); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.WriteFile(keep, []byte("keep"), 0o644); err != nil {
				t.Fatal(err)
			}
			e, err := ops.Rename(p, p)
			if err != nil {
				t.Fatalf("Rename(p, p) = %v, want success", err)
			}
			if e == nil || e.Name != "p" || e.IsDir != c.isDir {
				t.Errorf("entry = %+v, want name p, IsDir %v", e, c.isDir)
			}
			if b, err := os.ReadFile(keep); err != nil || string(b) != "keep" {
				t.Errorf("contents after self-rename: %q, %v", b, err)
			}
		})
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
