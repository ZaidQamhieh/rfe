package transfer

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// On one filesystem, complete publishes by moving the session temp, not by copying it: the published file is
// the very inode the chunks were written to.
func TestH6FastPathPublishMovesTheSessionTemp(t *testing.T) {
	for name, ops := range h6Ops(t.TempDir()) {
		t.Run(name, func(t *testing.T) {
			tm, _, dataDir := setupManager(t)
			dest := filepath.Join(t.TempDir(), "out")
			if name == "jailed" {
				dest = filepath.Join(dataDir, "out")
			}
			ops := ops
			if name == "jailed" {
				ops = h6Ops(dataDir)["jailed"]
			}
			target := filepath.Join(dest, "sub", "file.bin")
			data := []byte("0123456789abcdef-tail")
			temp := h6Upload(t, tm, "h6-move", target, data, false, h6InOrder(data))
			before, err := os.Stat(temp)
			if err != nil {
				t.Fatal(err)
			}
			if before.Mode().Perm() != 0o600 {
				t.Fatalf("session temp mode before publish = %v, want 0600", before.Mode().Perm())
			}
			if err := h6Complete(tm, ops, "h6-move"); err != nil {
				t.Fatalf("complete: %v", err)
			}
			after, err := os.Stat(target)
			if err != nil {
				t.Fatal(err)
			}
			if !os.SameFile(before, after) {
				t.Errorf("published file is a copy: it is not the session temp's inode")
			}
			if after.Mode().Perm() != 0o644 {
				t.Errorf("published mode = %v, want 0644", after.Mode().Perm())
			}
			if !bytes.Equal(h6Read(t, target), data) {
				t.Errorf("published content differs")
			}
			if _, err := os.Stat(temp); !errors.Is(err, os.ErrNotExist) {
				t.Errorf("session temp still exists after publish: %v", err)
			}
			h6NoPublishTemps(t, filepath.Dir(target))
		})
	}
}

// An overwriting upload replaces the existing file with the session temp and keeps the old file's mode.
func TestH6FastPathOverwriteKeepsModeAndReplacesContent(t *testing.T) {
	tm, _, dataDir := setupManager(t)
	ops := h6Ops(dataDir)["jailed"]
	target := filepath.Join(dataDir, "out", "file.bin")
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("old"), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(target, 0o640); err != nil {
		t.Fatal(err)
	}
	data := []byte("new content here")
	temp := h6Upload(t, tm, "h6-over", target, data, true, h6InOrder(data))
	before, _ := os.Stat(temp)
	if err := h6Complete(tm, ops, "h6-over"); err != nil {
		t.Fatalf("complete: %v", err)
	}
	after, err := os.Stat(target)
	if err != nil {
		t.Fatal(err)
	}
	if !os.SameFile(before, after) {
		t.Errorf("overwrite published a copy, not the session temp")
	}
	if after.Mode().Perm() != 0o640 {
		t.Errorf("overwrite mode = %v, want the old file's 0640", after.Mode().Perm())
	}
	if !bytes.Equal(h6Read(t, target), data) {
		t.Errorf("content not replaced")
	}
}

// A file that appeared at the target during the upload is never replaced by a no-overwrite publish; the
// session temp stays private and in place.
func TestH6FastPathNeverReplacesALateDestination(t *testing.T) {
	tm, _, dataDir := setupManager(t)
	ops := h6Ops(dataDir)["jailed"]
	target := filepath.Join(dataDir, "out", "late.bin")
	data := []byte("uploaded bytes!!")
	temp := h6Upload(t, tm, "h6-late", target, data, false, h6InOrder(data))
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("someone else"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := h6Complete(tm, ops, "h6-late"); !errors.Is(err, ErrDestinationExists) {
		t.Fatalf("complete = %v, want ErrDestinationExists", err)
	}
	if got := h6Read(t, target); string(got) != "someone else" {
		t.Errorf("destination was replaced: %q", got)
	}
	fi, err := os.Stat(temp)
	if err != nil {
		t.Fatalf("session temp gone after a refused publish: %v", err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Errorf("session temp mode = %v after a refused publish, want 0600", fi.Mode().Perm())
	}
	h6NoPublishTemps(t, filepath.Dir(target))
}
