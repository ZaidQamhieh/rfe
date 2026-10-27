//go:build !windows

package fsops

// Pass-2 regression tests that need a no-replace rename or POSIX permissions.

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

// P2-2: the no-replace rename refuses an existing destination, for files and
// folders, jailed and unjailed, and leaves both entries intact.
func TestPass2_RenameNoReplaceToRefusesExistingDestination(t *testing.T) {
	ops, root := setupJail(t)
	pass2Write(t, filepath.Join(root, "src.txt"), "payload")
	pass2Write(t, filepath.Join(root, "dst.txt"), "occupant")
	pass2Write(t, filepath.Join(root, "sdir", "f"), "payload")
	if err := os.MkdirAll(filepath.Join(root, "emptydir"), 0o755); err != nil {
		t.Fatal(err)
	}

	// accessPair gives two paths that share one jail root, as Rename and Move use.
	jailed := func(src, dst string) (*securePath, *securePath) {
		s, d, err := ops.accessPair(filepath.Join(root, src), filepath.Join(root, dst))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(s.close)
		return s, d
	}
	fileSrc, fileDst := jailed("src.txt", "dst.txt")
	dirSrc, dirDst := jailed("sdir", "emptydir")
	cases := []struct {
		name string
		src  *securePath
		dst  *securePath
	}{
		{"jailed file", fileSrc, fileDst},
		{"jailed folder onto empty folder", dirSrc, dirDst},
		{"unjailed file", &securePath{full: filepath.Join(root, "src.txt")}, &securePath{full: filepath.Join(root, "dst.txt")}},
	}
	for _, c := range cases {
		err := c.src.renameNoReplaceTo(c.dst)
		if !errors.Is(err, fs.ErrExist) {
			t.Errorf("%s: err = %v, want an exists error", c.name, err)
		}
	}
	if got := pass2Read(t, filepath.Join(root, "dst.txt")); got != "occupant" {
		t.Errorf("dst.txt now %q", got)
	}
	if got := pass2Read(t, filepath.Join(root, "src.txt")); got != "payload" {
		t.Errorf("src.txt now %q", got)
	}
	if got := pass2Read(t, filepath.Join(root, "sdir", "f")); got != "payload" {
		t.Errorf("sdir/f now %q", got)
	}
	if _, err := os.Stat(filepath.Join(root, "emptydir")); err != nil {
		t.Errorf("emptydir: %v", err)
	}

	// A free destination still renames.
	freeSrc, freeDst := jailed("src.txt", "free.txt")
	if err := freeSrc.renameNoReplaceTo(freeDst); err != nil {
		t.Fatalf("rename to a free name: %v", err)
	}
	if got := pass2Read(t, filepath.Join(root, "free.txt")); got != "payload" {
		t.Errorf("free.txt now %q", got)
	}
}

// P2-2: Rename and Move report a conflict and keep the destination, and renaming
// an entry onto itself stays a no-op.
func TestPass2_RenameAndMoveKeepExistingDestination(t *testing.T) {
	ops, root := setupJail(t)
	pass2Write(t, filepath.Join(root, "a.txt"), "payload")
	pass2Write(t, filepath.Join(root, "b.txt"), "occupant")

	if _, err := ops.Rename(filepath.Join(root, "a.txt"), filepath.Join(root, "b.txt")); !errors.Is(err, ErrConflict) {
		t.Fatalf("Rename onto an existing file: err = %v, want ErrConflict", err)
	}
	if got := pass2Read(t, filepath.Join(root, "b.txt")); got != "occupant" {
		t.Fatalf("b.txt now %q", got)
	}
	if _, err := ops.Rename(filepath.Join(root, "a.txt"), filepath.Join(root, "a.txt")); err != nil {
		t.Fatalf("Rename onto itself: %v", err)
	}

	pass2Write(t, filepath.Join(root, "sub", "b.txt"), "occupant2")
	res := ops.Move([]string{filepath.Join(root, "b.txt")}, filepath.Join(root, "sub"), false, false)
	if len(res) != 1 || res[0].OK || res[0].Error == nil || res[0].Error.Code != "CONFLICT" {
		t.Fatalf("Move onto an existing file: %+v", res)
	}
	if got := pass2Read(t, filepath.Join(root, "sub", "b.txt")); got != "occupant2" {
		t.Fatalf("sub/b.txt now %q", got)
	}
}

// P2-4: a failed Copy without overwrite leaves no partial destination.
func TestPass2_FailedCopyLeavesNoPartialDestination(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root reads a 0000 file")
	}
	ops, root := setupJail(t)
	src := filepath.Join(root, "src", "d")
	for _, n := range []string{"a", "b", "c"} {
		pass2Write(t, filepath.Join(src, n), "ok")
	}
	pass2Write(t, filepath.Join(src, "z-unreadable"), "secret")
	if err := os.Chmod(filepath.Join(src, "z-unreadable"), 0); err != nil {
		t.Fatal(err)
	}
	dstDir := filepath.Join(root, "dst")
	if err := os.MkdirAll(dstDir, 0o755); err != nil {
		t.Fatal(err)
	}

	res := ops.Copy([]string{src}, dstDir, false, false)
	if len(res) != 1 || res[0].OK || res[0].Error == nil || res[0].Error.Code != "COPY_FAILED" {
		t.Fatalf("Copy of a folder with an unreadable file: %+v", res)
	}
	if _, err := os.Lstat(filepath.Join(dstDir, "d")); !os.IsNotExist(err) {
		t.Fatalf("a partial destination remains: lstat err = %v", err)
	}
}

// P2-4: when the cross-device fallback finds the destination occupied, it
// removes nothing: the occupant and the source both stay.
func TestPass2_FailedCopyNeverRemovesEntryItDidNotCreate(t *testing.T) {
	ops, root, trashDir := setupTrash(t)
	src, err := ops.access(filepath.Join(root, "src.txt"))
	if err != nil {
		t.Fatal(err)
	}
	defer src.close()
	pass2Write(t, filepath.Join(root, "src.txt"), "payload")
	dst := &securePath{full: filepath.Join(trashDir, "occupied")}
	pass2Write(t, dst.full, "occupant")

	prev := renameAcrossRootsFn
	t.Cleanup(func() { renameAcrossRootsFn = prev })
	renameAcrossRootsFn = func(s, d *securePath) error {
		return &os.LinkError{Op: "rename", Old: s.full, New: d.full, Err: syscall.EXDEV}
	}

	err = moveAcrossRootsSecure(src, dst)
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("err = %v, want ErrConflict", err)
	}
	if got := pass2Read(t, dst.full); got != "occupant" {
		t.Fatalf("occupant now %q", got)
	}
	if got := pass2Read(t, filepath.Join(root, "src.txt")); got != "payload" {
		t.Fatalf("source now %q", got)
	}
}
