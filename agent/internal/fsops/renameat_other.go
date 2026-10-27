//go:build !linux && !darwin && !windows

package fsops

import (
	"fmt"
	"os"
	"path/filepath"
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
	for _, side := range []*securePath{src, dst} {
		if side.root == nil {
			continue
		}
		if side.name == "." {
			return fmt.Errorf("cannot move the jail root")
		}
		parent := filepath.Dir(side.full)
		if real, rerr := filepath.EvalSymlinks(parent); rerr != nil || real != parent {
			return fmt.Errorf("path changed during the operation")
		}
	}
	if _, err := os.Lstat(dst.full); err == nil {
		return &os.LinkError{Op: "rename", Old: src.full, New: dst.full, Err: syscall.EEXIST}
	}
	return os.Rename(src.full, dst.full)
}

// renameNoReplaceTo renames p to dst and fails with an error that matches
// fs.ErrExist when dst exists right before the rename. There is no atomic
// no-replace primitive here, so this is best effort: it is not atomic against
// an entry created at dst concurrently (see renameAcrossRoots).
func (p *securePath) renameNoReplaceTo(dst *securePath) error {
	if (p.root != nil && p.root == dst.root) || (p.root == nil && dst.root == nil) {
		return renameAcrossRoots(p, dst)
	}
	return fmt.Errorf("cannot atomically rename across filesystem jail roots")
}
