// Package fsops — trash (move-to-trash / restore) operations.
//
// The store follows the XDG Trash layout: a `files/` directory holding the
// trashed items and an `info/` directory holding one `<id>.trashinfo` sidecar
// per item recording its original path and deletion time, so items can be
// restored to where they came from. On Linux the store lives in the user's
// real desktop trash (`~/.local/share/Trash`), so app-side deletes show up in
// the desktop's Trash too; on other platforms it falls back to a managed
// directory under the agent data dir.
//
// User-facing paths (the thing being trashed, and a restore destination) go
// through Resolve so the path jail + read-only flag apply; the store itself is
// agent-managed and not jail-checked.
package fsops

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// TrashEntry describes one item in the trash store.
type TrashEntry struct {
	ID           string    `json:"id"` // unique key = basename under files/
	Name         string    `json:"name"`
	OriginalPath string    `json:"originalPath"`
	DeletedAt    time.Time `json:"deletedAt"`
	Size         int64     `json:"size"`
	IsDir        bool      `json:"isDir"`
}

const trashInfoExt = ".trashinfo"

// xdgDeletionDate is the XDG trashinfo DeletionDate layout (local time, no zone).
const xdgDeletionDate = "2006-01-02T15:04:05"

func trashFilesDir(trashDir string) string { return filepath.Join(trashDir, "files") }
func trashInfoDir(trashDir string) string  { return filepath.Join(trashDir, "info") }

// ErrBadTrashID rejects a client-supplied trash id that is not a single opaque
// basename. Trash ids are server-generated basenames under files/; anything
// with a separator, volume prefix, or dot name could join out of the store and
// let DELETE/restore reach arbitrary paths.
var ErrBadTrashID = errors.New("invalid trash id")

// ErrTrashStoreUnsafe is returned when the trash files directory is not a real
// directory (for example a symlink swapped in to redirect deletions).
var ErrTrashStoreUnsafe = errors.New("trash store is not a plain directory")

// openTrashFiles opens files/ as a root, refusing a symlinked store.
func openTrashFiles(trashDir string) (*os.Root, error) {
	dir := trashFilesDir(trashDir)
	fi, err := os.Lstat(dir)
	if err != nil {
		return nil, err
	}
	if !fi.IsDir() {
		return nil, ErrTrashStoreUnsafe
	}
	return os.OpenRoot(dir)
}

func validTrashID(id string) bool {
	if id == "" || id == "." || id == ".." {
		return false
	}
	if id != filepath.Base(id) || strings.ContainsRune(id, '/') || strings.ContainsRune(id, '\\') {
		return false
	}
	if filepath.VolumeName(id) != "" || filepath.IsAbs(id) {
		return false
	}
	return true
}

// MoveToTrash moves each path into the trash store at trashDir, writing a
// .trashinfo sidecar so it can be restored. Each path is jail-checked via
// Resolve. Cross-filesystem moves fall back to copy+remove. Returns a
// per-path BatchResult, mirroring Delete/Copy/Move.
func (o *Ops) MoveToTrash(paths []string, trashDir string) []BatchResult {
	if o.settings.IsReadOnly() {
		return readOnlyResults(paths)
	}
	if err := os.MkdirAll(trashFilesDir(trashDir), 0o700); err != nil {
		return errResults(paths, "TRASH_FAILED", err)
	}
	if err := os.MkdirAll(trashInfoDir(trashDir), 0o700); err != nil {
		return errResults(paths, "TRASH_FAILED", err)
	}

	results := make([]BatchResult, len(paths))
	for i, p := range paths {
		resolved, err := o.accessEntry(p)
		if err != nil {
			results[i] = BatchResult{Path: p, Error: apiErr("FORBIDDEN", err.Error())}
			continue
		}
		if _, err := resolved.lstat(); err != nil {
			resolved.close()
			if os.IsNotExist(err) {
				results[i] = BatchResult{Path: p, Error: apiErr("PATH_NOT_FOUND", err.Error())}
			} else {
				results[i] = BatchResult{Path: p, Error: apiErr("TRASH_FAILED", err.Error())}
			}
			continue
		}

		results[i] = trashOne(p, resolved, trashDir)
		resolved.close()
	}
	return results
}

// trashMu serializes picking a trash name through writing its sidecar, so two
// concurrent MoveToTrash calls for the same basename cannot both pick one name
// and overwrite each other's payload.
var trashMu sync.Mutex

// writeTrashInfoFn is the sidecar writer; a variable so tests can fail it.
var writeTrashInfoFn = writeTrashInfo

// trashOne moves one resolved path into the store. The caller closes resolved.
func trashOne(p string, resolved *securePath, trashDir string) BatchResult {
	trashMu.Lock()
	defer trashMu.Unlock()

	id, err := uniqueTrashName(trashDir, filepath.Base(resolved.full))
	if err != nil {
		return BatchResult{Path: p, Error: apiErr("TRASH_FAILED", err.Error())}
	}
	dest := filepath.Join(trashFilesDir(trashDir), id)
	destPath := &securePath{full: dest}
	if err := moveAcrossRootsSecure(resolved, destPath); err != nil {
		return BatchResult{Path: p, Error: apiErr("TRASH_FAILED", err.Error())}
	}
	if err := writeTrashInfoFn(trashDir, id, resolved.full); err != nil {
		// Roll the file back so a sidecar failure doesn't orphan the data. If
		// the rollback fails too, the data is stranded in the store: say where.
		if rbErr := moveAcrossRootsSecure(destPath, resolved); rbErr != nil {
			return BatchResult{Path: p, Error: apiErr("TRASH_FAILED", fmt.Sprintf(
				"%s; rollback failed (%s), the item is stranded in the trash files directory at %s",
				err.Error(), rbErr.Error(), dest))}
		}
		return BatchResult{Path: p, Error: apiErr("TRASH_FAILED", err.Error())}
	}
	return BatchResult{Path: p, OK: true}
}

// ListTrash enumerates the trash store, newest deletion first. A missing
// store is reported as empty, not an error.
func ListTrash(trashDir string) ([]TrashEntry, error) {
	ents, err := os.ReadDir(trashInfoDir(trashDir))
	if err != nil {
		if os.IsNotExist(err) {
			return []TrashEntry{}, nil
		}
		return nil, err
	}
	out := make([]TrashEntry, 0, len(ents))
	for _, e := range ents {
		if e.IsDir() || !strings.HasSuffix(e.Name(), trashInfoExt) {
			continue
		}
		id := strings.TrimSuffix(e.Name(), trashInfoExt)
		orig, delAt, err := readTrashInfo(filepath.Join(trashInfoDir(trashDir), e.Name()))
		if err != nil {
			continue // skip malformed sidecars rather than failing the whole list
		}
		var size int64
		var isDir bool
		if fi, statErr := os.Lstat(filepath.Join(trashFilesDir(trashDir), id)); statErr == nil {
			size = fi.Size()
			isDir = fi.IsDir()
		}
		out = append(out, TrashEntry{
			ID:           id,
			Name:         filepath.Base(orig),
			OriginalPath: orig,
			DeletedAt:    delAt,
			Size:         size,
			IsDir:        isDir,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].DeletedAt.After(out[j].DeletedAt) })
	return out, nil
}

// ListTrash enumerates the trash store visible to o: every item when o has
// no configured roots (no jail — unchanged), or only items whose recorded
// original path falls within one of o's roots otherwise. The global trash
// store has no per-device partitioning — everything any device deletes lands
// in the same store — so without this a jailed device could see the names,
// paths, and sizes of every other device's deleted files (PR-61).
func (o *Ops) ListTrash(trashDir string) ([]TrashEntry, error) {
	if o.denyAll {
		return []TrashEntry{}, nil
	}
	all, err := ListTrash(trashDir)
	if err != nil {
		return nil, err
	}
	roots := o.Roots()
	if len(roots) == 0 {
		return all, nil
	}
	out := make([]TrashEntry, 0, len(all))
	for _, e := range all {
		if _, err := o.Resolve(e.OriginalPath); err == nil {
			out = append(out, e)
		}
	}
	return out, nil
}

// RestoreFromTrash moves each id back to its recorded original path (jail-
// checked via Resolve). If the original location is now occupied the restore
// is auto-renamed ("keep both") rather than clobbering. Returns a per-id
// BatchResult whose Path is the restored location on success.
func (o *Ops) RestoreFromTrash(ids []string, trashDir string) []BatchResult {
	if o.settings.IsReadOnly() {
		return readOnlyResults(ids)
	}
	results := make([]BatchResult, len(ids))
	for i, id := range ids {
		if !validTrashID(id) {
			results[i] = BatchResult{Path: id, Error: apiErr("BAD_REQUEST", ErrBadTrashID.Error())}
			continue
		}
		infoPath := filepath.Join(trashInfoDir(trashDir), id+trashInfoExt)
		orig, _, err := readTrashInfo(infoPath)
		if err != nil {
			results[i] = BatchResult{Path: id, Error: apiErr("PATH_NOT_FOUND", "no such trash item")}
			continue
		}
		// The original path is a name to restore into: a symlink now sitting
		// there is a conflict to rename around, not a directory to follow.
		dest, err := o.accessEntry(orig)
		if err != nil {
			results[i] = BatchResult{Path: id, Error: apiErr("FORBIDDEN", err.Error())}
			continue
		}
		src := filepath.Join(trashFilesDir(trashDir), id)
		srcPath := &securePath{full: src}
		if _, err := srcPath.lstat(); err != nil {
			dest.close()
			results[i] = BatchResult{Path: id, Error: apiErr("PATH_NOT_FOUND", "trash payload missing")}
			continue
		}
		// lstat, not stat: a dangling symlink at the original path is an
		// occupied name and must be kept, not replaced by the rename.
		if dest.root != nil && dest.name == "." {
			// The jail root itself: its keep-both name would be a sibling of
			// the root (outside the jail) while its root-relative name lands
			// inside it. Refuse; the entry stays in the trash.
			dest.close()
			results[i] = BatchResult{Path: id, Error: apiErr("RESTORE_FAILED",
				"cannot restore to the jail root itself: "+orig)}
			continue
		}
		if _, err := dest.lstat(); err == nil {
			renamed, rerr := autoRenameSecure(dest)
			if rerr != nil {
				dest.close()
				results[i] = BatchResult{Path: id, Error: apiErr("CONFLICT", rerr.Error())}
				continue
			}
			dest = renamed
			if dest.root != nil && !filepath.IsLocal(dest.name) {
				dest.close()
				results[i] = BatchResult{Path: id, Error: apiErr("RESTORE_FAILED",
					"cannot restore: the keep-both name would be outside the jail: "+orig)}
				continue
			}
		} else if !os.IsNotExist(err) {
			dest.close()
			results[i] = BatchResult{Path: id, Error: apiErr("RESTORE_FAILED", err.Error())}
			continue
		}
		if err := dest.parent().mkdirAll(0o755); err != nil {
			dest.close()
			results[i] = BatchResult{Path: id, Error: apiErr("RESTORE_FAILED", err.Error())}
			continue
		}
		if err := moveAcrossRootsSecure(srcPath, dest); err != nil {
			dest.close()
			results[i] = BatchResult{Path: id, Error: apiErr(batchCode(err, "RESTORE_FAILED"), batchMessage(err, err.Error()))}
			continue
		}
		_ = os.Remove(infoPath)
		results[i] = BatchResult{Path: dest.full, OK: true}
		dest.close()
	}
	return results
}

// EmptyTrash permanently removes trash items visible to o. With no ids and no
// jail, the whole (global) store is emptied — unchanged fast path. Otherwise
// each candidate id is checked against o's jail (via its recorded original
// path) and skipped, not deleted, if it falls outside — mirroring
// RestoreFromTrash's per-id jail check. Without this, a jailed device could
// pass specific ids (or omit ids entirely) to permanently delete every other
// device's trashed files, including ones far outside its own jail (PR-61).
func (o *Ops) EmptyTrash(trashDir string, ids []string) error {
	if o.settings.IsReadOnly() {
		return ErrReadOnly
	}
	// Jailed views created from an invalid/out-of-scope jail root deliberately
	// have no roots and deny every path. Do not mistake that state for the
	// unjailed fast path below.
	if o.denyAll {
		return ErrForbidden
	}
	roots := o.Roots()
	if len(ids) == 0 && len(roots) == 0 {
		trashMu.Lock()
		defer trashMu.Unlock()
		if err := os.RemoveAll(trashFilesDir(trashDir)); err != nil {
			return err
		}
		return os.RemoveAll(trashInfoDir(trashDir))
	}
	if len(ids) == 0 {
		entries, err := ListTrash(trashDir)
		if err != nil {
			return err
		}
		for _, e := range entries {
			ids = append(ids, e.ID)
		}
	}
	for _, id := range ids {
		if !validTrashID(id) {
			return ErrBadTrashID
		}
	}
	store, err := openTrashFiles(trashDir)
	if os.IsNotExist(err) {
		return nil // empty store: nothing to delete
	}
	if err != nil {
		return err
	}
	defer store.Close()
	var firstErr error
	for _, id := range ids {
		if !validTrashID(id) {
			return ErrBadTrashID
		}
		if len(roots) > 0 {
			orig, _, err := readTrashInfo(filepath.Join(trashInfoDir(trashDir), id+trashInfoExt))
			if err != nil {
				continue // no trustworthy origin path: skip rather than delete
			}
			if _, resolveErr := o.Resolve(orig); resolveErr != nil {
				continue // outside the jail: skip rather than fail the whole batch
			}
		}
		// Descriptor-relative and no-follow: id cannot leave the store.
		// One stubborn item must not leave the rest undeleted while the caller is told nothing
		// changed: keep going and report the first failure at the end (audit F-159).
		if err := store.RemoveAll(id); err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		if err := os.Remove(filepath.Join(trashInfoDir(trashDir), id+trashInfoExt)); err != nil && !os.IsNotExist(err) && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// --------- helpers ---------

func readOnlyResults(paths []string) []BatchResult {
	res := make([]BatchResult, len(paths))
	for i, p := range paths {
		res[i] = BatchResult{Path: p, Error: &Error{Code: "READ_ONLY", Message: ErrReadOnly.Error()}}
	}
	return res
}

func errResults(paths []string, code string, err error) []BatchResult {
	res := make([]BatchResult, len(paths))
	for i, p := range paths {
		res[i] = BatchResult{Path: p, Error: apiErr(code, err.Error())}
	}
	return res
}

// renameAcrossRootsFn and removeAllFn are variables so tests can force the
// cross-device fallback and a failing source removal.
var (
	renameAcrossRootsFn = renameAcrossRoots
	removeAllFn         = func(p *securePath) error { return p.removeAll() }
	// renameNoReplaceFn is the no-replace rename Rename, Move and
	// moveAcrossRootsSecure try first; tests force EXDEV on one shared root.
	renameNoReplaceFn = func(src, dst *securePath) error { return src.renameNoReplaceTo(dst) }
)

// moveAcrossRootsSecure uses descriptor-relative rename when both paths share
// a root. When one side is the agent-managed trash store outside the user's
// jail, it copies through the rooted source/destination handles, then removes
// the source through its handle.
func moveAcrossRootsSecure(src, dst *securePath) error {
	err := renameNoReplaceFn(src, dst)
	if err != nil && src.root != dst.root {
		// Different roots (jailed source, unrooted trash): the jail check has
		// already run, so rename without the root handle and copy only when the
		// rename is cross-device (EXDEV, or ERROR_NOT_SAME_DEVICE on Windows).
		err = renameAcrossRootsFn(src, dst)
	}
	if err == nil {
		return nil
	}
	if !isCrossDevice(err) {
		return err
	}
	if made, err := copyTree(src, dst); err != nil {
		// Do not leave a half-copied payload behind; the source is still whole.
		// Only what this copy created is removed: an entry that was already (or
		// meanwhile) at dst is not ours.
		if made {
			if rmErr := dst.removeAll(); rmErr != nil {
				err = fmt.Errorf("%w; a partial copy remains at %s", err, dst.full)
			}
		}
		return err
	}
	rmErr := removeAllFn(src)
	if rmErr == nil {
		return nil
	}
	// The copy is complete but the source could not be fully removed. If the
	// source is in fact gone the move succeeded; otherwise report both places.
	// The copy is kept: it holds the data.
	if _, lerr := src.lstat(); os.IsNotExist(lerr) {
		return nil
	}
	return &copyKeptError{err: fmt.Errorf("copied to %s but could not remove the source %s (it may be partly deleted): %w; "+
		"the complete copy remains at %s", dst.full, src.full, rmErr, dst.full)}
}

// copyKeptError marks a cross-device move whose copy is complete but whose source could not be fully removed.
// The destination holds the data, so a caller must keep it (and not restore whatever it replaced).
type copyKeptError struct{ err error }

func (e *copyKeptError) Error() string { return e.err.Error() }
func (e *copyKeptError) Unwrap() error { return e.err }

// uniqueTrashName returns a name not already used by either files/ or info/.
// When every candidate is taken it fails with an error wrapping ErrConflict
// rather than return a name that exists.
func uniqueTrashName(trashDir, base string) (string, error) {
	candidate := base
	ext := filepath.Ext(base)
	stem := strings.TrimSuffix(base, ext)
	for i := 1; i <= maxAutoRename; i++ {
		_, e1 := os.Lstat(filepath.Join(trashFilesDir(trashDir), candidate))
		_, e2 := os.Lstat(filepath.Join(trashInfoDir(trashDir), candidate+trashInfoExt))
		// Stop on any error other than "exists" (e.g. name too long) so a
		// failing lstat cannot loop forever; the caller's write reports it.
		if e1 != nil && e2 != nil {
			return candidate, nil
		}
		candidate = fmt.Sprintf("%s_%d%s", stem, i, ext)
	}
	return "", fmt.Errorf("no free trash name left for %q: %w", base, ErrConflict)
}

func writeTrashInfo(trashDir, id, originalPath string) error {
	content := fmt.Sprintf(
		"[Trash Info]\nPath=%s\nDeletionDate=%s\n",
		encodeTrashPath(originalPath),
		time.Now().Format(xdgDeletionDate),
	)
	return os.WriteFile(filepath.Join(trashInfoDir(trashDir), id+trashInfoExt), []byte(content), 0o600)
}

// encodeTrashPath percent-encodes an absolute path the XDG-spec way: each path
// segment is escaped (spaces, unicode, reserved chars) but the '/' separators
// are kept literal, so a desktop file manager can also parse + restore items
// the app trashed (the store is the user's real ~/.local/share/Trash). The
// inverse is plain url.PathUnescape (used by readTrashInfo), which leaves the
// literal '/' untouched and decodes the %xx within each segment.
func encodeTrashPath(p string) string {
	parts := strings.Split(p, "/")
	for i, seg := range parts {
		parts[i] = url.PathEscape(seg)
	}
	return strings.Join(parts, "/")
}

func readTrashInfo(path string) (originalPath string, deletedAt time.Time, err error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", time.Time{}, err
	}
	for _, line := range strings.Split(string(data), "\n") {
		if v, ok := strings.CutPrefix(line, "Path="); ok {
			v = strings.TrimSpace(v)
			dec, derr := url.PathUnescape(v)
			if derr != nil {
				return "", time.Time{}, fmt.Errorf("malformed trashinfo path: %s", path)
			}
			originalPath = dec
		} else if v, ok := strings.CutPrefix(line, "DeletionDate="); ok {
			deletedAt, _ = time.ParseInLocation(xdgDeletionDate, strings.TrimSpace(v), time.Local)
		}
	}
	if originalPath == "" || !filepath.IsAbs(originalPath) {
		return "", time.Time{}, fmt.Errorf("malformed trashinfo: %s", path)
	}
	return originalPath, deletedAt, nil
}
