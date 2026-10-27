//go:build linux || darwin

package fsops

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

// renameAcrossRoots renames src to dst when at most one side is jailed. The
// jailed side's parent directory is opened through its os.Root (so a symlink in
// an ancestor cannot lead outside the jail, even if swapped after the jail
// check) and the rename is done with renameat on directory descriptors, which
// does not re-resolve the paths. The unjailed side is the agent-managed trash
// store, opened by path.
func renameAcrossRoots(src, dst *securePath) error {
	for _, side := range []*securePath{src, dst} {
		if side.root != nil && side.name == "." {
			return fmt.Errorf("cannot move the jail root")
		}
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
		derr := ddir.Control(func(dfd uintptr) {
			// os.Rename refuses an existing directory target; renameat would replace an empty one.
			var st unix.Stat_t
			if unix.Fstatat(int(dfd), dbase, &st, unix.AT_SYMLINK_NOFOLLOW) == nil {
				rerr = unix.EEXIST
				return
			}
			if afterNoReplaceCheck != nil {
				afterNoReplaceCheck()
			}
			rerr = renameNoReplace(int(sfd), sbase, int(dfd), dbase)
		})
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

// errNoReplaceUnsupported is returned when the file system has no atomic
// no-replace rename and no safe substitute either. It is the exported
// ErrNoAtomicRename (errors_norename.go): it matches errors.ErrUnsupported and
// is never fs.ErrExist; the caller must not read it as "destination exists",
// and nothing was renamed or replaced.
var errNoReplaceUnsupported = ErrNoAtomicRename

// Test seams: the syscalls the fallback uses, and a hook that runs between the
// existence check in renameAcrossRoots and the rename. Production code never
// sets them.
var (
	linkatFn            = unix.Linkat
	unlinkatFn          = unix.Unlinkat
	renameatFn          = unix.Renameat
	afterNoReplaceCheck func()
)

// renameNoReplace renames sbase in sfd to dbase in dfd without replacing an
// existing target; it fails with EEXIST if dbase exists or appeared. On the
// fallback a folder source can still replace an empty folder created in the
// window (see renameNoReplaceFallback).
//
// It uses the kernel's atomic no-replace rename. A file system without one
// (NFS, some FUSE, old kernels) takes renameNoReplaceFallback instead, whose
// no-replace check is atomic for non-directories (linkat), and which refuses
// with errNoReplaceUnsupported when it cannot link. The fallback's move as a
// whole is not atomic; see its comment for the residual windows.
func renameNoReplace(sfd int, sbase string, dfd int, dbase string) error {
	err := noReplacePrimitive(sfd, sbase, dfd, dbase)
	if !noReplaceUnsupported(err) {
		return err
	}
	return renameNoReplaceFallback(sfd, sbase, dfd, dbase)
}

// renameNoReplaceFallback is renameNoReplace without a native primitive.
//
// A non-directory source (a regular file, or a symlink, which is moved as the
// link itself) is hard-linked to the new name, which fails with EEXIST
// atomically when the name is taken, and the old name is then removed. If the
// old name cannot be removed the new link is removed again so the source stays
// as it was. The old name is only unlinked while it still is the linked entry
// (see the checks below), and the new name only removed while it still is too.
//
// Guarantees: an existing file or symlink destination is never replaced, and
// this rename never itself removes the renamed entry's last name. Another
// process deleting the new name while both exist has the same effect as
// deleting it right after a native rename: the content is gone, as that
// process asked. Not guaranteed: (1) both names are
// visible between the link and the unlink; (2) if another process renames a
// different entry onto the old name (or, when undoing, onto the new name) after
// the identity check and before the unlink, that entry's name is removed, and
// its content is lost if that was its only name. The check and the unlink are
// two syscalls and no kernel primitive closes that window on a file system
// without RENAME_NOREPLACE (see unlinkIfSame).
// A file system that cannot hard-link gets errNoReplaceUnsupported:
// refusing is the only atomic-safe answer, a plain rename would replace a file
// that appeared after the caller's check.
//
// A directory source cannot be hard-linked, so it uses the plain rename after
// a check that the target is free. rename(2) of a directory only ever replaces
// an EMPTY directory (a non-empty one gives ENOTEMPTY, a file ENOTDIR). The
// residual is therefore an empty directory created in the window between the
// check and the rename; nothing with content can be lost.
func renameNoReplaceFallback(sfd int, sbase string, dfd int, dbase string) error {
	var sst unix.Stat_t
	if err := unix.Fstatat(sfd, sbase, &sst, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		return err
	}
	if sst.Mode&unix.S_IFMT == unix.S_IFDIR {
		var dst unix.Stat_t
		if unix.Fstatat(dfd, dbase, &dst, unix.AT_SYMLINK_NOFOLLOW) == nil {
			return unix.EEXIST
		}
		return renameatFn(sfd, sbase, dfd, dbase)
	}
	// Flags 0: link the entry itself, not what a symlink points to.
	if err := linkatFn(sfd, sbase, dfd, dbase, 0); err != nil {
		if linkUnsupported(err) {
			return fmt.Errorf("%w: %w", errNoReplaceUnsupported, err)
		}
		return err
	}
	// The new name must be the very entry that was asked for. A platform whose
	// linkat followed a symlink would have linked its target instead.
	var nst unix.Stat_t
	if err := unix.Fstatat(dfd, dbase, &nst, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		// The new name is not there (or unreadable): there is nothing of ours to
		// remove, and the source was not touched.
		return errNoReplaceUnsupported
	}
	if nst.Dev != sst.Dev || nst.Ino != sst.Ino {
		// linkat linked something else (a followed symlink's target). Remove the
		// new name only if it is not that entry's last name; never delete data.
		var tst unix.Stat_t
		if nst.Nlink > 1 && unix.Fstatat(sfd, sbase, &tst, 0) == nil &&
			tst.Dev == nst.Dev && tst.Ino == nst.Ino {
			_ = unlinkIfSame(dfd, dbase, &nst)
		}
		return errNoReplaceUnsupported
	}

	// Both names now point at the entry. Remove the old one only while it still
	// is that entry: if it was removed meanwhile, the content lives at dbase and
	// the move is done; if another entry took its name, unlinking it would
	// delete something that is not ours.
	var cur unix.Stat_t
	switch err := unix.Fstatat(sfd, sbase, &cur, unix.AT_SYMLINK_NOFOLLOW); {
	case err == unix.ENOENT:
		return nil
	case err != nil:
		// Cannot tell what is at the old name: keep both names.
		return fmt.Errorf("could not verify the source after linking %q: %w", sbase, err)
	case cur.Dev != sst.Dev || cur.Ino != sst.Ino:
		return errSourceChanged
	}
	if err := unlinkatFn(sfd, sbase, 0); err != nil {
		if err == unix.ENOENT {
			return nil // removed concurrently; the content lives at dbase
		}
		// Undo the new name, but only while it is still our link and the source
		// still holds the entry, so the last name of the content is never removed.
		if sameEntryAt(sfd, sbase, &sst) {
			if rerr := unlinkIfSame(dfd, dbase, &sst); rerr != nil {
				return errors.Join(err, fmt.Errorf("could not remove the new link %q: %w", dbase, rerr))
			}
		}
		return err
	}
	return nil
}

// errSourceChanged means another entry took the source's name while it was
// being renamed. Both names are left as they are, so nothing is lost; it is
// neither fs.ErrExist nor ErrNoAtomicRename.
var errSourceChanged = errors.New("source changed during rename")

// sameEntryAt reports whether name in fd is, without following a symlink, the
// entry st describes.
func sameEntryAt(fd int, name string, st *unix.Stat_t) bool {
	var cur unix.Stat_t
	return unix.Fstatat(fd, name, &cur, unix.AT_SYMLINK_NOFOLLOW) == nil &&
		cur.Dev == st.Dev && cur.Ino == st.Ino
}

// unlinkIfSame removes name in fd only if it is still the entry st describes,
// so an entry this rename did not create is never deleted. Fstatat and unlinkat
// are two syscalls: another process can still swap the name in between. That
// window cannot be closed without a kernel primitive, which is the one missing
// on a file system that takes this fallback.
func unlinkIfSame(fd int, name string, st *unix.Stat_t) error {
	if !sameEntryAt(fd, name, st) {
		return nil
	}
	return unlinkatFn(fd, name, 0)
}

// linkUnsupported reports whether a linkat error means the file system cannot
// hard-link this entry at all, as opposed to a real failure such as EEXIST,
// ENOENT or EACCES.
func linkUnsupported(err error) bool {
	// ENOTSUP and EOPNOTSUPP are the same number on Linux, so they are
	// compared with || rather than as switch cases.
	return err == unix.EPERM || err == unix.ENOTSUP || err == unix.EOPNOTSUPP ||
		err == unix.ENOSYS || err == unix.EMLINK || err == unix.EXDEV || err == unix.EINVAL
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

// dirHandle is an open parent directory.
type dirHandle struct {
	f *os.File
}

func (d dirHandle) Close() { _ = d.f.Close() }

func (d dirHandle) Control(fn func(fd uintptr)) error {
	sc, err := d.f.SyscallConn()
	if err != nil {
		return err
	}
	return sc.Control(fn)
}

// openParentDir opens p's parent directory (through p's root when jailed) and
// returns it with p's final path element.
func openParentDir(p *securePath) (dirHandle, string, error) {
	if p.root != nil {
		f, err := p.root.Open(filepath.Dir(p.name))
		if err != nil {
			return dirHandle{}, "", err
		}
		return dirHandle{f}, filepath.Base(p.name), nil
	}
	f, err := os.Open(filepath.Dir(p.full))
	if err != nil {
		return dirHandle{}, "", err
	}
	return dirHandle{f}, filepath.Base(p.full), nil
}
