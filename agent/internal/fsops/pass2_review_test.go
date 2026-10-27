package fsops

// Pass-2 review fixes that need no POSIX permissions: a rename or move that
// fails with EXDEV on one filesystem root still completes through the copy
// fallback.

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

// forceExdev makes the first no-replace rename of Rename, Move and
// moveAcrossRootsSecure fail with EXDEV, as a rename across a mount point does.
func forceExdev(t *testing.T) *int {
	t.Helper()
	calls := 0
	prev := renameNoReplaceFn
	t.Cleanup(func() { renameNoReplaceFn = prev })
	renameNoReplaceFn = func(s, d *securePath) error {
		calls++
		return &os.LinkError{Op: "rename", Old: s.full, New: d.full, Err: syscall.EXDEV}
	}
	return &calls
}

func TestPass2Review_RenameAndMoveFallBackOnExdevSameRoot(t *testing.T) {
	jailedOps, jailedRoot := setupJail(t)
	free := t.TempDir()
	cases := []struct {
		name string
		ops  *Ops
		dir  string
	}{
		{"one shared jail root", jailedOps, jailedRoot},
		{"unjailed", New(nil, false), free},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			calls := forceExdev(t)

			// Rename of a file and of a folder.
			pass2Write(t, filepath.Join(c.dir, "r.txt"), "payload")
			pass2Write(t, filepath.Join(c.dir, "rd", "inner", "f"), "deep")
			if _, err := c.ops.Rename(filepath.Join(c.dir, "r.txt"), filepath.Join(c.dir, "r2.txt")); err != nil {
				t.Fatalf("Rename across devices: %v", err)
			}
			if _, err := c.ops.Rename(filepath.Join(c.dir, "rd"), filepath.Join(c.dir, "rd2")); err != nil {
				t.Fatalf("Rename of a folder across devices: %v", err)
			}
			if got := pass2Read(t, filepath.Join(c.dir, "r2.txt")); got != "payload" {
				t.Errorf("r2.txt now %q", got)
			}
			if got := pass2Read(t, filepath.Join(c.dir, "rd2", "inner", "f")); got != "deep" {
				t.Errorf("rd2/inner/f now %q", got)
			}
			for _, gone := range []string{"r.txt", "rd"} {
				if _, err := os.Lstat(filepath.Join(c.dir, gone)); !os.IsNotExist(err) {
					t.Errorf("%s still exists: %v", gone, err)
				}
			}

			// Move.
			pass2Write(t, filepath.Join(c.dir, "m.txt"), "moved")
			if err := os.MkdirAll(filepath.Join(c.dir, "dest"), 0o755); err != nil {
				t.Fatal(err)
			}
			res := c.ops.Move([]string{filepath.Join(c.dir, "m.txt")}, filepath.Join(c.dir, "dest"), false, false)
			if len(res) != 1 || !res[0].OK {
				t.Fatalf("Move across devices: %+v", res)
			}
			if got := pass2Read(t, filepath.Join(c.dir, "dest", "m.txt")); got != "moved" {
				t.Errorf("dest/m.txt now %q", got)
			}
			if _, err := os.Lstat(filepath.Join(c.dir, "m.txt")); !os.IsNotExist(err) {
				t.Errorf("m.txt still exists: %v", err)
			}

			// An occupied destination still wins: nothing is replaced.
			pass2Write(t, filepath.Join(c.dir, "n.txt"), "new")
			pass2Write(t, filepath.Join(c.dir, "dest", "n.txt"), "occupant")
			res = c.ops.Move([]string{filepath.Join(c.dir, "n.txt")}, filepath.Join(c.dir, "dest"), false, false)
			if len(res) != 1 || res[0].OK || res[0].Error == nil || res[0].Error.Code != "CONFLICT" {
				t.Fatalf("Move onto an occupied name: %+v", res)
			}
			if got := pass2Read(t, filepath.Join(c.dir, "dest", "n.txt")); got != "occupant" {
				t.Errorf("occupant now %q", got)
			}
			if got := pass2Read(t, filepath.Join(c.dir, "n.txt")); got != "new" {
				t.Errorf("source now %q", got)
			}
			if *calls == 0 {
				t.Error("the forced EXDEV hook was never reached")
			}
		})
	}
}
