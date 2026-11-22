package rootfs

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

// openReadFlags opens read-only without ever waiting on the file: O_NONBLOCK
// keeps a named pipe from blocking until a writer shows up.
const openReadFlags = os.O_RDONLY | syscall.O_NONBLOCK

// readDirAll asks File.ReadDir for every entry.
const readDirAll = -1

// Handle is one path inside a Set. It holds no open file; every method does
// its work relative to the os.Root of the folder that contains the path, so
// none of them can follow a symlink out of it.
type Handle struct {
	set  *Set
	root *os.Root
	rel  string // path inside root; "." for the root itself
	full string // the clean absolute path the caller gave
}

// access describes what an operation does, so check can apply the right
// read-only and protected-folder rules.
type access struct {
	write  bool // changes the file system
	follow bool // follows a symlink in the last element
	tree   bool // acts on everything below the target, or moves it as a whole
}

// Path returns the clean absolute path this handle was made for.
func (h *Handle) Path() string { return h.full }

// check refuses a changing operation on a read-only Set, and any operation
// whose target (or, for tree operations, anything below it) is a protected
// folder. Symlinks are expanded by hand so a link to the data directory is
// caught too. This is a check before the operation: only the os.Root
// containment is race-free, and a protected folder is not one.
func (h *Handle) check(a access) error {
	if a.write && h.set.readOnly {
		return ErrReadOnly
	}
	if len(h.set.protected) == 0 {
		return nil
	}
	real := resolvePhysical(h.full, a.follow)
	for _, p := range h.set.protected {
		if isUnder(real, p) || (a.tree && isUnder(p, real)) {
			return ErrProtected
		}
	}
	return nil
}

// CheckTree fails with ErrProtected when the path is, contains or lies inside
// a protected folder. Recursive operations (copy, compress, move, search)
// call it on their starting folder before walking, so they fail instead of
// silently leaving the protected part out.
func (h *Handle) CheckTree() error {
	return h.check(access{follow: true, tree: true})
}

// at returns the os.Root to use and the path inside it. In a restricted Set
// that is the root found by Open. An unrestricted Set has no boundary to keep,
// but os.Root refuses every absolute symlink, so the path is first expanded
// by hand (resolvePhysical) and the volume root is used with the expanded
// path. follow says whether a symlink in the last element is expanded.
func (h *Handle) at(follow bool) (*os.Root, string, error) {
	if !h.set.unrestricted {
		return h.root, h.rel, nil
	}
	real := resolvePhysical(h.full, follow)
	base := filepath.VolumeName(real) + pathSep
	root, err := h.set.volumeRoot(base)
	if err != nil {
		return nil, "", err
	}
	rel, _ := underRel(real, base)
	return root, rel, nil
}

// notRoot refuses an operation that would remove or move an allowed folder
// itself (in an unrestricted Set: a volume root).
func (h *Handle) notRoot() error {
	if _, rel, err := h.at(false); err != nil || rel == curDir {
		if err != nil {
			return err
		}
		return ErrRootTarget
	}
	return nil
}

// Stat returns the file's information, following a final symlink.
func (h *Handle) Stat() (fs.FileInfo, error) {
	if err := h.check(access{follow: true}); err != nil {
		return nil, err
	}
	root, rel, err := h.at(true)
	if err != nil {
		return nil, err
	}
	info, err := root.Stat(rel)
	return info, mapErr(err)
}

// Lstat returns the entry's own information without following a final
// symlink.
func (h *Handle) Lstat() (fs.FileInfo, error) {
	if err := h.check(access{}); err != nil {
		return nil, err
	}
	root, rel, err := h.at(false)
	if err != nil {
		return nil, err
	}
	info, err := root.Lstat(rel)
	return info, mapErr(err)
}

// Readlink returns the target text of a symlink.
func (h *Handle) Readlink() (string, error) {
	if err := h.check(access{}); err != nil {
		return "", err
	}
	root, rel, err := h.at(false)
	if err != nil {
		return "", err
	}
	target, err := root.Readlink(rel)
	return target, mapErr(err)
}

// openRead opens the path for reading and refuses anything that is neither a
// regular file nor a folder.
func (h *Handle) openRead() (*os.File, error) {
	root, rel, err := h.at(true)
	if err != nil {
		return nil, err
	}
	f, err := root.OpenFile(rel, openReadFlags, 0)
	if err != nil {
		// A unix socket cannot be opened (ENXIO): it is not a regular file.
		if errors.Is(err, syscall.ENXIO) {
			return nil, ErrNotRegular
		}
		return nil, mapErr(err)
	}
	info, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return nil, err
	}
	if !info.Mode().IsRegular() && !info.IsDir() {
		_ = f.Close()
		return nil, ErrNotRegular
	}
	return f, nil
}

// Open opens a regular file or folder for reading. Use ReadDir, not the
// returned file, to list a folder: only ReadDir hides protected entries.
func (h *Handle) Open() (*os.File, error) {
	if err := h.check(access{follow: true}); err != nil {
		return nil, err
	}
	return h.openRead()
}

// OpenFile opens the path with the given flags and permission. Flags that
// write, create or truncate make it a changing operation (ErrReadOnly on a
// read-only Set).
func (h *Handle) OpenFile(flag int, perm fs.FileMode) (*os.File, error) {
	if err := h.check(access{write: flag&writeFlags != 0, follow: true}); err != nil {
		return nil, err
	}
	root, rel, err := h.at(true)
	if err != nil {
		return nil, err
	}
	f, err := root.OpenFile(rel, flag, perm)
	if err != nil && flag&(os.O_CREATE|os.O_EXCL) == os.O_CREATE|os.O_EXCL && errors.Is(err, syscall.EISDIR) {
		// An exclusive create over a folder fails with EEXIST on Unix but
		// "is a directory" on Windows; both mean the name is taken.
		err = &fs.PathError{Op: "open", Path: rel, Err: fs.ErrExist}
	}
	return f, mapErr(err)
}

// Mkdir creates one folder; it fails if the name is taken.
func (h *Handle) Mkdir(perm fs.FileMode) error {
	if err := h.check(access{write: true}); err != nil {
		return err
	}
	root, rel, err := h.at(false)
	if err != nil {
		return err
	}
	return mapErr(root.Mkdir(rel, perm))
}

// MkdirAll creates the folder and any missing parents; it succeeds if the
// folder already exists.
func (h *Handle) MkdirAll(perm fs.FileMode) error {
	if err := h.check(access{write: true, follow: true}); err != nil {
		return err
	}
	root, rel, err := h.at(true)
	if err != nil {
		return err
	}
	return mapErr(root.MkdirAll(rel, perm))
}

// Symlink creates this path as a symbolic link whose text is target. os.Root
// does not check the text of a link, so target is checked here: it must be
// relative (an absolute target is refused, as every absolute link is elsewhere
// in this package) and, with the symlinks on the way followed, must resolve
// inside the Set (ErrOutsideRoots) and outside protected folders (ErrProtected).
func (h *Handle) Symlink(target string) error {
	if err := h.check(access{write: true}); err != nil {
		return err
	}
	if target == "" || strings.IndexByte(target, 0) >= 0 {
		return fmt.Errorf("%w: %q", ErrInvalidPath, target)
	}
	if filepath.IsAbs(target) || filepath.VolumeName(target) != "" || os.IsPathSeparator(target[0]) {
		return fmt.Errorf("%w: absolute link target %q", ErrOutsideRoots, target)
	}
	// A relative target is read from the folder that holds the link, as the
	// kernel does, so that folder is taken with its own symlinks resolved.
	dir := filepath.Dir(resolvePhysical(h.full, false))
	resolved := resolvePhysical(filepath.Join(dir, target), true)
	if _, _, err := h.set.locate(resolved); err != nil {
		return err
	}
	if h.set.isProtectedPath(resolved) {
		return ErrProtected
	}
	root, rel, err := h.at(false)
	if err != nil {
		return err
	}
	return mapErr(root.Symlink(target, rel))
}

// Remove deletes a file or an empty folder (a symlink itself, not its target).
func (h *Handle) Remove() error {
	if err := h.check(access{write: true}); err != nil {
		return err
	}
	if err := h.notRoot(); err != nil {
		return err
	}
	root, rel, err := h.at(false)
	if err != nil {
		return err
	}
	return mapErr(root.Remove(rel))
}

// RemoveAll deletes the path and everything below it. It fails with
// ErrProtected when a protected folder is the target or lies below it.
func (h *Handle) RemoveAll() error {
	if err := h.check(access{write: true, tree: true}); err != nil {
		return err
	}
	if err := h.notRoot(); err != nil {
		return err
	}
	root, rel, err := h.at(false)
	if err != nil {
		return err
	}
	return mapErr(root.RemoveAll(rel))
}

// ReadDir returns every entry of the folder, leaving out any entry that is a
// protected folder.
func (h *Handle) ReadDir() ([]fs.DirEntry, error) {
	if err := h.check(access{follow: true}); err != nil {
		return nil, err
	}
	f, err := h.openRead()
	if err != nil {
		return nil, err
	}
	defer f.Close()
	entries, err := f.ReadDir(readDirAll)
	if err != nil || len(h.set.protected) == 0 {
		return entries, mapErr(err)
	}
	dir := resolvePhysical(h.full, true)
	kept := entries[:0]
	for _, e := range entries {
		if !h.set.isProtectedPath(filepath.Join(dir, e.Name())) {
			kept = append(kept, e)
		}
	}
	return kept, nil
}

// renameArgs checks both ends of a rename: read-only, protected folders,
// allowed-folder targets, and that both lie in the same root. It returns that
// root and the two paths inside it.
func (h *Handle) renameArgs(dst *Handle) (root *os.Root, src, to string, err error) {
	if dst == nil {
		return nil, "", "", ErrInvalidPath
	}
	for _, p := range []*Handle{h, dst} {
		if err := p.check(access{write: true, tree: true}); err != nil {
			return nil, "", "", err
		}
		if err := p.notRoot(); err != nil {
			return nil, "", "", err
		}
	}
	root, src, err = h.at(false)
	if err != nil {
		return nil, "", "", err
	}
	toRoot, to, err := dst.at(false)
	if err != nil {
		return nil, "", "", err
	}
	if root != toRoot {
		return nil, "", "", ErrCrossRoot
	}
	return root, src, to, nil
}

// Rename moves this entry to dst, replacing a file already there. Both
// handles must be in the same allowed folder (else ErrCrossRoot).
func (h *Handle) Rename(dst *Handle) error {
	root, src, to, err := h.renameArgs(dst)
	if err != nil {
		return err
	}
	return mapErr(root.Rename(src, to))
}

// RenameNoReplace moves this entry to dst and fails with an error matching
// fs.ErrExist if dst is already taken. On Linux the check and the move are one
// atomic kernel call; elsewhere there is a short window between them.
func (h *Handle) RenameNoReplace(dst *Handle) error {
	root, src, to, err := h.renameArgs(dst)
	if err != nil {
		return err
	}
	return mapErr(renameNoReplace(root, src, to))
}

// renameNoReplaceGeneric is the portable no-replace rename: it looks for the
// target first and then renames, so another process can create the target in
// between.
func renameNoReplaceGeneric(root *os.Root, src, dst string) error {
	_, err := root.Lstat(dst)
	if err == nil {
		return &os.LinkError{Op: "rename", Old: src, New: dst, Err: fs.ErrExist}
	}
	if !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return root.Rename(src, dst)
}
