//go:build linux || darwin

package fsops

import (
	"errors"
	"fmt"
	"os"
	"syscall"

	"golang.org/x/sys/unix"
)

// sameDevice reports whether two files live on the same filesystem (equal device ids).
func sameDevice(a, b os.FileInfo) bool {
	sa, okA := a.Sys().(*syscall.Stat_t)
	sb, okB := b.Sys().(*syscall.Stat_t)
	return okA && okB && uint64(sa.Dev) == uint64(sb.Dev)
}

// errUnsupportedMove reports whether err means the move cannot be done here (another filesystem, or one with
// neither an atomic no-replace rename nor hard links) and the caller should copy instead.
func errUnsupportedMove(err error) bool {
	return errors.Is(err, ErrNoAtomicRename) || errors.Is(err, unix.EXDEV)
}

// moveFileAcrossRoots moves the file src to dst with renameat on directory descriptors, so neither side's path
// is looked up again after the parents are opened (a jailed side is opened through its os.Root). Without
// overwrite it never replaces an existing name (EEXIST); with it an existing file at dst is replaced atomically.
func moveFileAcrossRoots(src, dst *securePath, overwrite bool) error {
	if !overwrite {
		return renameAcrossRoots(src, dst)
	}
	if dst.root != nil && dst.name == "." {
		return fmt.Errorf("cannot replace the jail root")
	}
	sdir, sbase, err := openParentDir(src)
	if err != nil {
		return &os.LinkError{Op: "rename", Old: src.full, New: dst.full, Err: err}
	}
	defer sdir.Close()
	ddir, dbase, err := openParentDir(dst)
	if err != nil {
		return &os.LinkError{Op: "rename", Old: src.full, New: dst.full, Err: err}
	}
	defer ddir.Close()
	var rerr error
	cerr := sdir.Control(func(sfd uintptr) {
		derr := ddir.Control(func(dfd uintptr) { rerr = renameatFn(int(sfd), sbase, int(dfd), dbase) })
		if derr != nil {
			rerr = derr
		}
	})
	if cerr != nil {
		rerr = cerr
	}
	if rerr != nil {
		return &os.LinkError{Op: "rename", Old: src.full, New: dst.full, Err: rerr}
	}
	return nil
}
