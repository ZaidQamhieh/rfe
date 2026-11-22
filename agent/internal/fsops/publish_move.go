package fsops

import (
	"errors"
	"io"
	"io/fs"
	"os"
)

// publishMode is the mode a published file ends with: the default for created files, or, for an overwrite, the
// mode of the regular file it replaces.
func publishMode(resolved *securePath, overwrite bool) fs.FileMode {
	mode := newFilePerm
	if overwrite {
		if existing, err := resolved.lstat(); err == nil && existing.Mode().IsRegular() {
			mode = existing.Mode().Perm()
		}
	}
	return mode
}

// removeLeftoverTemp removes the regular file name next to dir's entries if an earlier copy attempt of the
// same session left it, and reports whether that name is now gone (or never existed). Anything that is not a
// regular file is left alone and reported as still there.
func removeLeftoverTemp(folder *securePath, name string) bool {
	leftover := folder.child(name)
	fi, err := leftover.lstat()
	if errors.Is(err, fs.ErrNotExist) {
		return true
	}
	if err != nil || !fi.Mode().IsRegular() {
		return false
	}
	err = leftover.remove()
	return err == nil || errors.Is(err, fs.ErrNotExist)
}

// publishByMove publishes opts.MoveFrom at resolved by moving that file instead of copying src. handled is
// false when the move does not apply (see PublishOptions.MoveFrom) and nothing was changed, so the caller
// copies. When handled is true the result is final: info and err are the publish's outcome (ErrConflict when
// a no-overwrite publish found the name taken). The source keeps its private 0600 mode if the move fails.
func publishByMove(resolved *securePath, src io.Reader, overwrite bool, opts PublishOptions) (info os.FileInfo, handled bool, err error) {
	f, ok := src.(*os.File)
	if !ok || opts.MoveFrom == "" {
		return nil, false, nil
	}
	moveSrc := &securePath{full: opts.MoveFrom}
	onDisk, err := moveSrc.lstat()
	if err != nil || !onDisk.Mode().IsRegular() {
		return nil, false, nil
	}
	if opened, err := f.Stat(); err != nil || !os.SameFile(onDisk, opened) {
		return nil, false, nil
	}
	dir, err := resolved.parent().stat()
	if err != nil || !sameDevice(onDisk, dir) {
		return nil, false, nil
	}

	if err := f.Chmod(publishMode(resolved, overwrite)); err != nil {
		return nil, true, err
	}
	// The data was synced chunk by chunk; this makes the size and mode durable before the name appears.
	if err := f.Sync(); err != nil {
		_ = f.Chmod(0o600)
		return nil, true, err
	}
	info, err = f.Stat()
	if err != nil {
		_ = f.Chmod(0o600)
		return nil, true, err
	}
	if err := moveFileAcrossRoots(moveSrc, resolved, overwrite); err != nil {
		_ = f.Chmod(0o600)
		if errUnsupportedMove(err) {
			return nil, false, nil
		}
		if overwrite {
			return nil, true, err
		}
		if errors.Is(err, fs.ErrExist) {
			return nil, true, ErrConflict
		}
		return nil, true, err
	}
	// The destination name must be durable before the caller records the upload as completed.
	if err := resolved.syncParent(); err != nil {
		return nil, true, err
	}
	return info, true, nil
}
