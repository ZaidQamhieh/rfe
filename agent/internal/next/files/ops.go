package files

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/ZaidQamhieh/rfe/agent/internal/next/api"
	"github.com/ZaidQamhieh/rfe/agent/internal/next/rootfs"
)

// ownerRWX is the permission a copied folder keeps while its content is being
// copied, so the copy can always write into it; the source's own (possibly
// stricter) permission is applied last.
const ownerRWX fs.FileMode = 0o700

// maxAutoRename bounds the search for a free "name (N)" for keep-both copies.
const maxAutoRename = 10000

// dirMode is a copied folder whose source permission is applied once the whole
// copy is done.
type dirMode struct {
	h    *rootfs.Handle
	perm fs.FileMode
}

// inside reports whether path is base or lies below it (compared as text, after
// cleaning).
func inside(path, base string) bool {
	rel, err := filepath.Rel(base, path)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// entryName returns the final name of an absolute path, refusing a volume or
// filesystem root, which has no name to copy or move under.
func entryName(path string) (string, error) {
	if filepath.Dir(path) == path {
		return "", fmt.Errorf("%w: a root folder cannot be copied or moved", api.ErrBadRequest)
	}
	return filepath.Base(path), nil
}

// ensureDir makes sure h is a folder, creating it (and parents) if it does not
// exist. A file at that path is a bad request.
func (s *Service) ensureDir(h *rootfs.Handle) error {
	info, err := h.Stat()
	switch {
	case err == nil && !info.IsDir():
		return fmt.Errorf("%w: destination is not a folder", api.ErrBadRequest)
	case err == nil:
		return nil
	case isMissing(err):
		return classify(h.MkdirAll(rootfs.DirMode))
	}
	return classify(err)
}

// autoName returns a handle for a free "stem (N).ext" sibling of dst.
func (s *Service) autoName(dst *rootfs.Handle) (*rootfs.Handle, error) {
	dir := filepath.Dir(dst.Path())
	base := filepath.Base(dst.Path())
	ext := filepath.Ext(base)
	stem := strings.TrimSuffix(base, ext)
	for i := 1; i <= maxAutoRename; i++ {
		cand, err := s.open(filepath.Join(dir, fmt.Sprintf("%s (%d)%s", stem, i, ext)))
		if err != nil {
			return nil, err
		}
		if _, found, err := probe(cand); err != nil {
			return nil, err
		} else if !found {
			return cand, nil
		}
	}
	return nil, fmt.Errorf("%w: no free name left", api.ErrConflict)
}

// collision decides what to do when the destination name may be taken. A free
// name is used as is. Otherwise keep-both (duplicate) picks a new name, else
// overwrite means replace, else the item is a conflict. A destination that is
// the source or contains it is never replaced, since that would delete the
// source.
func (s *Service) collision(src, dst *rootfs.Handle, duplicate, overwrite bool) (final *rootfs.Handle, replace bool, err error) {
	_, found, err := probe(dst)
	if err != nil {
		return nil, false, err
	}
	switch {
	case !found:
		return dst, false, nil
	case duplicate:
		final, err = s.autoName(dst)
		return final, false, err
	case overwrite && !inside(src.Path(), dst.Path()):
		return dst, true, nil
	}
	return nil, false, fmt.Errorf("%w: destination already exists", api.ErrConflict)
}

// swapIn runs do, which puts the new content at dst. With replace the entry
// currently at dst is first renamed to a hidden name and deleted only after do
// succeeded; if do fails it is renamed back. A failed delete of the old entry
// is ignored: the new content is in place and the leftover is a hidden
// ".rfe-old-*" entry.
func (s *Service) swapIn(dst *rootfs.Handle, replace bool, do func() error) error {
	if !replace {
		return do()
	}
	aside, err := s.reserve(filepath.Dir(dst.Path()), asidePrefix, dst.RenameNoReplace)
	if err != nil {
		return err
	}
	if err := do(); err != nil {
		if rerr := aside.RenameNoReplace(dst); rerr != nil {
			return fmt.Errorf("%w; the replaced item was kept under the hidden name %s", err, filepath.Base(aside.Path()))
		}
		return err
	}
	_ = aside.RemoveAll()
	return nil
}

// Rename moves the entry at src to dst (both full paths), never replacing an
// existing entry (api.ErrConflict). Renaming an entry onto itself does nothing.
// A folder cannot move into its own subtree. Missing parents of dst are
// created. A symlink is renamed itself, not its target.
func (s *Service) Rename(src, dst string) (*Entry, error) {
	hs, err := s.open(src)
	if err != nil {
		return nil, err
	}
	hd, err := s.open(dst)
	if err != nil {
		return nil, err
	}
	srcInfo, err := entryInfo(hs)
	if err != nil {
		return nil, err
	}
	dstInfo, found, err := probe(hd)
	if err != nil {
		return nil, err
	}
	sameEntry := found && os.SameFile(srcInfo, dstInfo)
	switch {
	case found && !sameEntry:
		return nil, fmt.Errorf("%w: destination already exists", api.ErrConflict)
	case sameEntry && hs.Path() == hd.Path():
		return s.Meta(hd.Path())
	case hs.Path() != hd.Path() && inside(hd.Path(), hs.Path()):
		return nil, fmt.Errorf("%w: cannot move a folder into itself", api.ErrConflict)
	}
	if err := s.ensureParent(hd); err != nil {
		return nil, err
	}
	if sameEntry {
		// Same entry under another spelling (a case-only rename on a
		// case-insensitive file system): the destination "exists" but is us.
		err = classify(hs.Rename(hd))
	} else {
		err = s.moveEntry(hs, srcInfo, hd)
	}
	if err != nil {
		return nil, err
	}
	return s.Meta(hd.Path())
}

// entryInfo reads the entry's own information, classified.
func entryInfo(h *rootfs.Handle) (fs.FileInfo, error) {
	info, err := h.Lstat()
	return info, classify(err)
}

// moveEntry renames hs to dst without replacing anything. When the two are in
// different allowed folders or on different devices a rename is impossible, so
// it copies and then deletes the source.
func (s *Service) moveEntry(hs *rootfs.Handle, srcInfo fs.FileInfo, dst *rootfs.Handle) error {
	err := hs.RenameNoReplace(dst)
	if errors.Is(err, rootfs.ErrCrossRoot) || errors.Is(err, syscall.EXDEV) {
		return s.moveAcross(hs, srcInfo, dst)
	}
	return classify(err)
}

// moveAcross is a move by copy: the tree is copied to a hidden name next to
// dst, renamed into place (no-replace) and only then is the source removed. If
// that removal fails both copies exist and ErrMoveIncomplete is returned.
func (s *Service) moveAcross(hs *rootfs.Handle, srcInfo fs.FileInfo, dst *rootfs.Handle) error {
	if err := hs.CheckTree(); err != nil {
		return classify(err)
	}
	removeErr, err := copyThenRemove(s, s, hs, srcInfo, filepath.Dir(dst.Path()), func() (*rootfs.Handle, error) { return dst, nil })
	if err != nil {
		return err
	}
	if removeErr != nil {
		return fmt.Errorf("%w: %w", ErrMoveIncomplete, removeErr)
	}
	return nil
}

// copyThenRemove is the move that cannot be a rename: srcSvc copies src into a hidden name inside tmpDir (on dstSvc's
// side), the copy is renamed without replacing to the handle that place() returns, and only then is src removed.
// err is a failure before the copy was in place; the hidden copy is discarded and src is untouched. removeErr is a
// failure to remove src afterwards: both copies then exist, and the caller decides how to say so.
func copyThenRemove(srcSvc, dstSvc *Service, src *rootfs.Handle, info fs.FileInfo, tmpDir string, place func() (*rootfs.Handle, error)) (removeErr, err error) {
	tmp, err := srcSvc.copyAny(dstSvc, src, info, tmpDir)
	if err != nil {
		return nil, err
	}
	dst, err := place()
	if err != nil {
		return nil, dstSvc.discardTree(tmp, err)
	}
	if err := tmp.RenameNoReplace(dst); err != nil {
		return nil, dstSvc.discardTree(tmp, err)
	}
	return src.RemoveAll(), nil
}

// Copy copies each source into destDir under its own name and returns one
// result per source, in order. A symlink named as a source is copied as its
// target's content. Collisions: duplicate keeps both (renaming the copy),
// else overwrite replaces the existing entry, else the item fails with
// api.ErrConflict. A folder cannot be copied into itself, and a folder that
// contains the protected data folder is refused. Each copy is built under a
// hidden name and renamed into place, so a failed copy leaves no partial
// destination and a replaced entry is restored.
func (s *Service) Copy(sources []string, destDir string, duplicate, overwrite bool) []api.ItemResult {
	results := make([]api.ItemResult, len(sources))
	for i, src := range sources {
		results[i] = api.Item(src, s.copyOne(src, destDir, duplicate, overwrite))
	}
	return results
}

// copyOne copies a single source; see Copy.
func (s *Service) copyOne(src, destDir string, duplicate, overwrite bool) error {
	hs, err := s.open(src)
	if err != nil {
		return err
	}
	name, err := entryName(hs.Path())
	if err != nil {
		return err
	}
	hdir, err := s.open(destDir)
	if err != nil {
		return err
	}
	info, err := hs.Stat()
	if err != nil {
		return classify(err)
	}
	if err := hs.CheckTree(); err != nil {
		return classify(err)
	}
	if info.IsDir() && inside(hdir.Path(), hs.Path()) {
		return fmt.Errorf("%w: cannot copy a folder into itself", api.ErrConflict)
	}
	dst, err := s.open(filepath.Join(hdir.Path(), name))
	if err != nil {
		return err
	}
	if hs.Path() == dst.Path() && !duplicate {
		return nil // copying an entry onto itself is a no-op
	}
	if err := s.ensureDir(hdir); err != nil {
		return err
	}
	final, replace, err := s.collision(hs, dst, duplicate, overwrite)
	if err != nil {
		return err
	}
	tmp, err := s.copyToTemp(s, hs, info, hdir.Path())
	if err != nil {
		return err
	}
	return s.swapIn(final, replace, func() error {
		if err := tmp.RenameNoReplace(final); err != nil {
			return s.discardTree(tmp, err)
		}
		return nil
	})
}

// Move moves each source into destDir under its own name and returns one
// result per source. A symlink is moved as the link. Collision rules are those
// of Copy. A folder cannot move into itself. Across devices or allowed folders
// the move is a copy followed by removing the source.
func (s *Service) Move(sources []string, destDir string, duplicate, overwrite bool) []api.ItemResult {
	results := make([]api.ItemResult, len(sources))
	for i, src := range sources {
		results[i] = api.Item(src, s.moveOne(src, destDir, duplicate, overwrite))
	}
	return results
}

// moveOne moves a single source; see Move.
func (s *Service) moveOne(src, destDir string, duplicate, overwrite bool) error {
	hs, err := s.open(src)
	if err != nil {
		return err
	}
	name, err := entryName(hs.Path())
	if err != nil {
		return err
	}
	hdir, err := s.open(destDir)
	if err != nil {
		return err
	}
	info, err := entryInfo(hs)
	if err != nil {
		return err
	}
	dst, err := s.open(filepath.Join(hdir.Path(), name))
	if err != nil {
		return err
	}
	if hs.Path() == dst.Path() {
		return nil // moving an entry onto itself is a no-op
	}
	if inside(dst.Path(), hs.Path()) {
		return fmt.Errorf("%w: cannot move a folder into itself", api.ErrConflict)
	}
	if err := s.ensureDir(hdir); err != nil {
		return err
	}
	final, replace, err := s.collision(hs, dst, duplicate, overwrite)
	if err != nil {
		return err
	}
	return s.swapIn(final, replace, func() error { return s.moveEntry(hs, info, final) })
}

// Delete permanently removes each path (a folder with everything in it; a
// symlink itself, never its target) and returns one result per path. A path
// that does not exist is a failed item with api.ErrPathNotFound; the other
// paths are still processed.
func (s *Service) Delete(paths []string) []api.ItemResult {
	results := make([]api.ItemResult, len(paths))
	for i, p := range paths {
		results[i] = api.Item(p, s.deleteOne(p))
	}
	return results
}

// deleteOne removes one path. It looks first, because RemoveAll succeeds on a
// missing path.
func (s *Service) deleteOne(path string) error {
	h, err := s.open(path)
	if err != nil {
		return err
	}
	if _, err := entryInfo(h); err != nil {
		return err
	}
	return classify(h.RemoveAll())
}

// copyAny copies the entry src (information info, taken without following a
// final link) into a new hidden entry in dir of the service dst and returns its
// handle: a symlink is recreated as a link, anything else is copied as a tree.
// The caller renames the result into place.
func (s *Service) copyAny(dst *Service, src *rootfs.Handle, info fs.FileInfo, dir string) (*rootfs.Handle, error) {
	if info.Mode()&fs.ModeSymlink == 0 {
		return s.copyToTemp(dst, src, info, dir)
	}
	return dst.reserve(dir, tempPrefix, func(h *rootfs.Handle) error { return copyLink(src, h) })
}

// copyLink recreates the symlink from at to with the same text. A link that
// rootfs refuses (absolute, or leading outside the allowed folders) is
// ErrSymlinkCopy; the link is never followed.
func copyLink(from, to *rootfs.Handle) error {
	target, err := from.Readlink()
	if err != nil {
		return classify(err)
	}
	err = to.Symlink(target)
	if errors.Is(err, rootfs.ErrOutsideRoots) || errors.Is(err, rootfs.ErrProtected) || errors.Is(err, rootfs.ErrInvalidPath) {
		return &opError{kind: ErrSymlinkCopy, cause: err}
	}
	return classify(err)
}

// copyToTemp copies the tree at src (information info, following a symlink
// source) into a new hidden entry in dir of the service dst and returns its
// handle. On any failure the partial copy is removed. The caller renames the
// result into place.
func (s *Service) copyToTemp(dst *Service, src *rootfs.Handle, info fs.FileInfo, dir string) (*rootfs.Handle, error) {
	tmp, err := dst.reserve(dir, tempPrefix, func(h *rootfs.Handle) error {
		if info.IsDir() {
			return h.Mkdir(info.Mode().Perm() | ownerRWX)
		}
		f, err := h.OpenFile(os.O_WRONLY|os.O_CREATE|os.O_EXCL, info.Mode().Perm())
		if err != nil {
			return err
		}
		return f.Close()
	})
	if err != nil {
		return nil, err
	}
	if err := s.fillTemp(dst, src, info, tmp); err != nil {
		return nil, dst.discardTree(tmp, err)
	}
	return tmp, nil
}

// fillTemp copies the content of src into the already created entry tmp, which
// belongs to the service dst.
func (s *Service) fillTemp(dst *Service, src *rootfs.Handle, info fs.FileInfo, tmp *rootfs.Handle) error {
	if !info.IsDir() {
		if !info.Mode().IsRegular() {
			return fmt.Errorf("%w: not a regular file", api.ErrNotAFile)
		}
		return copyFile(src, tmp, info.Mode().Perm(), os.O_WRONLY|os.O_TRUNC)
	}
	// The copy lives inside a folder that may itself be inside the source (a
	// destination reached through a link): never copy it into itself.
	self, err := tmp.Lstat()
	if err != nil {
		return classify(err)
	}
	var modes []dirMode
	if err := s.fillDir(dst, src, tmp, self, &modes); err != nil {
		return err
	}
	if info.Mode().Perm()&ownerRWX != ownerRWX {
		modes = append(modes, dirMode{h: tmp, perm: info.Mode().Perm()})
	}
	return applyModes(modes)
}

// fillDir copies every entry of the folder src into the existing folder to of
// the service ds. skip is the information of the copy's own top folder, left out
// if met. A symlink inside the tree is recreated when its target stays inside
// the allowed folders, else the copy fails (ErrSymlinkCopy); anything that is
// neither a file, a folder nor a link is refused.
func (s *Service) fillDir(ds *Service, src, dst *rootfs.Handle, skip fs.FileInfo, modes *[]dirMode) error {
	entries, err := src.ReadDir()
	if err != nil {
		return classify(err)
	}
	for _, de := range entries {
		info, err := de.Info()
		if isMissing(err) {
			continue // removed since the folder was read
		}
		if err != nil {
			return classify(err)
		}
		if os.SameFile(info, skip) {
			continue
		}
		from, err := s.open(filepath.Join(src.Path(), de.Name()))
		if err != nil {
			return err
		}
		to, err := ds.open(filepath.Join(dst.Path(), de.Name()))
		if err != nil {
			return err
		}
		switch {
		case info.Mode()&fs.ModeSymlink != 0:
			if err := copyLink(from, to); err != nil {
				return err
			}
		case info.IsDir():
			if err := to.Mkdir(info.Mode().Perm() | ownerRWX); err != nil {
				return classify(err)
			}
			if err := s.fillDir(ds, from, to, skip, modes); err != nil {
				return err
			}
			if info.Mode().Perm()&ownerRWX != ownerRWX {
				*modes = append(*modes, dirMode{h: to, perm: info.Mode().Perm()})
			}
		case info.Mode().IsRegular():
			if err := copyFile(from, to, info.Mode().Perm(), os.O_WRONLY|os.O_CREATE|os.O_EXCL); err != nil {
				return err
			}
		default:
			return fmt.Errorf("%w: not a regular file", api.ErrNotAFile)
		}
	}
	return nil
}

// copyFile copies the bytes of src into dst, opened with flag (exclusive
// create for new files) and perm.
func copyFile(src, dst *rootfs.Handle, perm fs.FileMode, flag int) error {
	in, err := src.Open()
	if err != nil {
		return classify(err)
	}
	defer in.Close()
	out, err := dst.OpenFile(flag, perm)
	if err != nil {
		return classify(err)
	}
	_, err = io.Copy(out, in)
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	return classify(err)
}

// applyModes sets the saved permissions, deepest folder first, so a restrictive
// folder never blocks the change of its parent.
func applyModes(modes []dirMode) error {
	for _, m := range modes {
		f, err := m.h.OpenFile(os.O_RDONLY, 0)
		if err != nil {
			return classify(err)
		}
		err = f.Chmod(m.perm)
		if cerr := f.Close(); err == nil {
			err = cerr
		}
		if err != nil {
			return classify(err)
		}
	}
	return nil
}
