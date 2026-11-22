package files

import (
	"archive/zip"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"
)

// Permission bits the mode tests expect, matching what the old agent gives.
const (
	modeNewUpload  = fs.FileMode(0o600) // a published upload and a compress output
	modeNewContent = fs.FileMode(0o644) // CreateFile and WriteContent of a new file
	modeKeptExec   = fs.FileMode(0o755) // an existing file's mode an overwrite keeps
	modeKeptTight  = fs.FileMode(0o640) // an existing file's mode WriteContent keeps
)

// TestNewFileModes checks the permission bits each creator in the service
// gives a new file, under a fixed umask so the result does not depend on the
// shell that ran the tests.
func TestNewFileModes(t *testing.T) {
	fxSkipOnWindows(t)
	pinUmask(t)
	e := newEnv(t, false)

	t.Run("upload publish of a new file is owner-only", func(t *testing.T) {
		p := filepath.Join(e.root, "up.bin")
		if _, err := e.svc.PublishReader(p, strings.NewReader("data"), false); err != nil {
			t.Fatal(err)
		}
		if got := fxPerm(t, p); got != modeNewUpload {
			t.Errorf("mode = %v, want %v", got, modeNewUpload)
		}
	})
	t.Run("upload publish with overwrite of a missing name is owner-only", func(t *testing.T) {
		p := filepath.Join(e.root, "up2.bin")
		if _, err := e.svc.PublishReader(p, strings.NewReader("data"), true); err != nil {
			t.Fatal(err)
		}
		if got := fxPerm(t, p); got != modeNewUpload {
			t.Errorf("mode = %v, want %v", got, modeNewUpload)
		}
	})
	t.Run("upload overwrite keeps the existing file's mode", func(t *testing.T) {
		p := write(t, filepath.Join(e.root, "run.sh"), "old")
		if err := chmodPath(p, modeKeptExec); err != nil {
			t.Fatal(err)
		}
		if _, err := e.svc.PublishReader(p, strings.NewReader("new"), true); err != nil {
			t.Fatal(err)
		}
		if got := fxPerm(t, p); got != modeKeptExec {
			t.Errorf("mode = %v, want %v", got, modeKeptExec)
		}
	})
	t.Run("compress output is owner-only", func(t *testing.T) {
		src := write(t, filepath.Join(e.root, "pack", "a.txt"), "a")
		dest := filepath.Join(e.root, "out.zip")
		if _, err := e.svc.Compress([]string{filepath.Dir(src)}, dest); err != nil {
			t.Fatal(err)
		}
		if got := fxPerm(t, dest); got != modeNewUpload {
			t.Errorf("mode = %v, want %v", got, modeNewUpload)
		}
		zr, err := zip.OpenReader(dest)
		if err != nil {
			t.Fatalf("output is not a readable zip: %v", err)
		}
		_ = zr.Close()
	})
	t.Run("create file is world-readable", func(t *testing.T) {
		p := filepath.Join(e.root, "empty.txt")
		if _, err := e.svc.CreateFile(p); err != nil {
			t.Fatal(err)
		}
		if got := fxPerm(t, p); got != modeNewContent {
			t.Errorf("mode = %v, want %v", got, modeNewContent)
		}
	})
	t.Run("write content of a new file is world-readable", func(t *testing.T) {
		p := filepath.Join(e.root, "note.txt")
		if _, err := e.svc.WriteContent(p, []byte("hi"), nil); err != nil {
			t.Fatal(err)
		}
		if got := fxPerm(t, p); got != modeNewContent {
			t.Errorf("mode = %v, want %v", got, modeNewContent)
		}
	})
	t.Run("write content keeps an existing file's mode", func(t *testing.T) {
		p := write(t, filepath.Join(e.root, "keep.txt"), "old")
		if err := chmodPath(p, modeKeptTight); err != nil {
			t.Fatal(err)
		}
		if _, err := e.svc.WriteContent(p, []byte("new"), nil); err != nil {
			t.Fatal(err)
		}
		if got := fxPerm(t, p); got != modeKeptTight {
			t.Errorf("mode = %v, want %v", got, modeKeptTight)
		}
	})
}
