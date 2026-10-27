//go:build windows

package fsops

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"golang.org/x/sys/windows"
)

// renameAcrossRoots renames src to dst when at most one side is jailed, and
// never replaces an existing dst. os.Rename on Windows is MoveFileEx with
// MOVEFILE_REPLACE_EXISTING, which silently replaces a file, so this calls
// MoveFileEx with no flags: it fails with ERROR_ALREADY_EXISTS or
// ERROR_FILE_EXISTS, atomically, when dst exists (file or directory), and with
// ERROR_NOT_SAME_DEVICE across volumes (the caller then copies).
//
// MoveFileEx takes paths, not handles, so as in the generic version the jailed
// side's parent must still be its own canonical path, and the jail root itself
// cannot be moved.
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
	from, err := windows.UTF16PtrFromString(src.full)
	if err != nil {
		return &os.LinkError{Op: "rename", Old: src.full, New: dst.full, Err: err}
	}
	to, err := windows.UTF16PtrFromString(dst.full)
	if err != nil {
		return &os.LinkError{Op: "rename", Old: src.full, New: dst.full, Err: err}
	}
	if err := windows.MoveFileEx(from, to, 0); err != nil {
		if err == windows.ERROR_ALREADY_EXISTS || err == windows.ERROR_FILE_EXISTS {
			err = fs.ErrExist
		}
		return &os.LinkError{Op: "rename", Old: src.full, New: dst.full, Err: err}
	}
	return nil
}

// renameNoReplaceTo renames p to dst and fails with an error that matches
// fs.ErrExist, instead of replacing the target, when dst exists or appears
// concurrently. Like renameTo it only renames inside one jail root, or between
// two unjailed paths.
func (p *securePath) renameNoReplaceTo(dst *securePath) error {
	if (p.root != nil && p.root == dst.root) || (p.root == nil && dst.root == nil) {
		return renameAcrossRoots(p, dst)
	}
	return fmt.Errorf("cannot atomically rename across filesystem jail roots")
}
