//go:build !linux && !darwin && !windows

package fsops

import (
	"os"
	"syscall"
)

// renameAcrossRoots renames src to dst when at most one side is jailed. There
// is no descriptor-relative rename here, so it renames by absolute path. That
// follows symlinks in the parents, so first check that the jailed side's parent
// is still its own canonical path and that the jail root itself is not being
// moved.
//
// BEST EFFORT, NOT ATOMIC: no atomic no-replace rename primitive exists on
// these platforms, so the destination is checked right before os.Rename. An
// entry created at dst between that check and the rename can still be
// replaced. Callers must not rely on this for more than that.
func renameAcrossRoots(src, dst *securePath) error {
	if err := checkJailParents(src, dst); err != nil {
		return err
	}
	if _, err := os.Lstat(dst.full); err == nil {
		return &os.LinkError{Op: "rename", Old: src.full, New: dst.full, Err: syscall.EEXIST}
	}
	return os.Rename(src.full, dst.full)
}
