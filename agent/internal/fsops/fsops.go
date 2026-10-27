// Package fsops implements filesystem operations with a path-jail.
// Traversal attempts (../) and symlink escapes are rejected when
// allowed_root_paths is configured.
package fsops

import (
	"crypto/md5"
	"crypto/rand"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"io"
	"io/fs"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// ErrForbidden is returned when a path is outside the jail.
var ErrForbidden = errors.New("path is outside allowed root")

// ErrNotRegular is returned when a path is neither a regular file nor a
// directory (a FIFO, socket or device), which the agent never opens.
var ErrNotRegular = errors.New("not a regular file or directory")

// ErrNotDir is returned when a directory operation names something else.
var ErrNotDir = errors.New("not a directory")

// ErrReadOnly is returned for write operations when in read-only mode.
var ErrReadOnly = errors.New("agent is in read-only mode")

// ErrNotFound is returned when a path doesn't exist.
var ErrNotFound = errors.New("path not found")

// ErrIsDir is returned when a file-only operation (Checksum) names a directory.
var ErrIsDir = errors.New("path is a directory")

// ErrConflict is returned when a destination already exists.
var ErrConflict = errors.New("destination already exists")

// ErrUnsupported is returned by Extract for an archive whose extension is
// not a supported format (.zip/.tar.gz/.tgz).
var ErrUnsupported = errors.New("unsupported archive format")

// ErrStale is returned by WriteContent when the on-disk file's mtime no
// longer matches the baseModified the caller last read, indicating the
// file changed since then (optimistic-concurrency conflict).
var ErrStale = errors.New("file changed since last read")

// The path-jail and access-control model (SettingsView, the read-only/jailed
// wrappers, Resolve, resolveReal, isUnder) lives in jail.go — the security
// boundary that every operation below passes through.

// Ops performs filesystem operations with an optional path jail.
type Ops struct {
	settings SettingsView
	// denyAll, when true, makes Resolve reject every path regardless of
	// settings/roots. Set only by Jailed when a device's jailRoot falls
	// outside the agent's configured roots — see Jailed for why this can't
	// simply be represented as an empty roots slice (empty roots means "no
	// jail" / allow everything).
	denyAll bool
}

// New creates an Ops with optional allowedRoots.
// If allowedRoots is empty every path is allowed.
func New(allowedRoots []string, readOnly bool) *Ops {
	roots := make([]string, 0, len(allowedRoots))
	for _, r := range allowedRoots {
		clean := filepath.Clean(r)
		if clean != "" && clean != "." {
			roots = append(roots, clean)
		}
	}
	return &Ops{settings: staticSettings{readOnly: readOnly, roots: roots}}
}

// NewWithSettings builds an Ops backed by a live SettingsView.
func NewWithSettings(v SettingsView) *Ops {
	return &Ops{settings: v}
}

// IsReadOnly reports whether writes are currently rejected. Handlers that
// mutate outside the batch Ops (e.g. chmod) must consult this so read-only
// policy is enforced uniformly (PR-04).
func (o *Ops) IsReadOnly() bool { return o.settings.IsReadOnly() }

// Roots returns the configured allowed roots (a copy). An empty slice
// means there is no jail (anything is allowed) — callers that need a
// concrete starting point in that case should fall back to something
// sensible (e.g. the user's home directory or filesystem drives).
func (o *Ops) Roots() []string {
	if o.denyAll {
		return nil
	}
	src := o.settings.Roots()
	roots := make([]string, len(src))
	copy(roots, src)
	return roots
}

// --------- Entry type ---------

const (
	maxCountedDirs = 60   // directories per page that get a childCount
	maxChildCount  = 1000 // a directory with more items reports this value
)

// countChildren counts the items directly inside dir through the jailed path, reading at most
// maxChildCount+1 names. It returns nil when the directory cannot be read.
func countChildren(dir *securePath) *int {
	f, err := dir.open()
	if err != nil {
		return nil
	}
	defer f.Close()
	names, err := f.Readdirnames(maxChildCount + 1)
	if err != nil && err != io.EOF {
		return nil
	}
	n := len(names)
	if n > maxChildCount {
		n = maxChildCount
	}
	return &n
}

// Entry is the JSON representation of a filesystem item.
type Entry struct {
	Name          string    `json:"name"`
	Path          string    `json:"path"`
	IsDir         bool      `json:"isDir"`
	Size          int64     `json:"size"`
	MimeType      string    `json:"mimeType,omitempty"`
	Mode          string    `json:"mode"`
	Modified      time.Time `json:"modified"`
	Created       time.Time `json:"created"`
	IsSymlink     bool      `json:"isSymlink"`
	SymlinkTarget string    `json:"symlinkTarget,omitempty"`
	// ChildCount is the number of items directly inside a directory, capped at
	// maxChildCount (a capped count means "at least"). Only ListDir fills it, and
	// only for the first maxCountedDirs directories of a page; nil otherwise.
	ChildCount *int `json:"childCount,omitempty"`
}

// Listing is a paginated directory listing.
type Listing struct {
	Path       string  `json:"path"`
	Entries    []Entry `json:"entries"`
	NextCursor *string `json:"nextCursor"`
}

// Drive represents a filesystem mount point.
type Drive struct {
	Path       string `json:"path"`
	Label      string `json:"label"`
	TotalBytes int64  `json:"totalBytes"`
	FreeBytes  int64  `json:"freeBytes"`
	IsOS       bool   `json:"isOS"`
}

// --------- ListDir ---------

// maxListLimit caps how many entries one ListDir page may return, regardless
// of the client-requested limit (PR-48).
const maxListLimit = 1000

// ListDir lists a directory with cursor-based pagination by name.
func (o *Ops) ListDir(path, cursor string, limit int) (*Listing, error) {
	resolved, err := o.access(path)
	if err != nil {
		return nil, err
	}
	defer resolved.close()
	f, err := resolved.open()
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	defer f.Close()
	if st, err := f.Stat(); err == nil && !st.IsDir() {
		return nil, ErrNotDir
	}

	infos, err := f.Readdir(-1)
	if err != nil {
		return nil, err
	}
	sort.Slice(infos, func(i, j int) bool {
		return infos[i].Name() < infos[j].Name()
	})

	if limit <= 0 {
		limit = 200
	}
	// PR-48: bound the per-page response regardless of what the client asks,
	// so one request can't demand an enormous page. The full-directory read
	// and sort above is inherent to name-ordered pagination.
	if limit > maxListLimit {
		limit = maxListLimit
	}

	var nextCursor *string

	// Filter by cursor first (skip entries at or before the cursor).
	var filtered []os.FileInfo
	for _, info := range infos {
		if cursor != "" && info.Name() <= cursor {
			continue
		}
		filtered = append(filtered, info)
	}

	// Cap at limit; if there are more, record the last-included name as cursor.
	end := len(filtered)
	if end > limit {
		end = limit
	}
	entries := make([]Entry, 0, end)
	counted := 0
	for _, info := range filtered[:end] {
		child := resolved.child(info.Name())
		e := entryFromSecureInfo(info, child, true)
		if e.IsDir && !e.IsSymlink && counted < maxCountedDirs {
			counted++
			e.ChildCount = countChildren(child)
		}
		entries = append(entries, e)
	}
	if end < len(filtered) {
		c := entries[end-1].Name
		nextCursor = &c
	}

	return &Listing{Path: resolved.full, Entries: entries, NextCursor: nextCursor}, nil
}

// --------- Meta ---------

// Meta returns detailed metadata for a single entry.
func (o *Ops) Meta(path string) (*Entry, error) {
	resolved, err := o.access(path)
	if err != nil {
		return nil, err
	}
	defer resolved.close()
	// Use Lstat to detect symlinks.
	info, err := resolved.lstat()
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	e := entryFromSecureInfo(info, resolved, true)
	return &e, nil
}

// --------- Checksum ---------

// Checksum computes a hex-encoded hash of the file at path.
// Supported algorithms: "sha256" (default), "sha1", "md5".
func (o *Ops) Checksum(path, algo string) (string, error) {
	resolved, err := o.access(path)
	if err != nil {
		return "", err
	}
	defer resolved.close()
	info, err := resolved.lstat()
	if err != nil {
		if os.IsNotExist(err) {
			return "", ErrNotFound
		}
		return "", err
	}
	if info.IsDir() {
		return "", fmt.Errorf("cannot checksum a directory: %w", ErrIsDir)
	}
	f, err := resolved.open()
	if err != nil {
		return "", err
	}
	defer f.Close()

	var h hash.Hash
	switch algo {
	case "md5":
		h = md5.New()
	case "sha1":
		h = sha1.New()
	case "", "sha256":
		h = sha256.New()
	default:
		return "", fmt.Errorf("unsupported algorithm %q (use sha256, sha1, or md5)", algo)
	}
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// --------- Drives ---------

// Drives returns the available mount points / drives.
func Drives() ([]Drive, error) {
	return platformDrives()
}

// Drives returns the mount points/drives visible through o: every drive when
// o has no configured roots (no jail — today's default, unchanged), or only
// the drives that contain or are contained by one of o's roots otherwise
// (PR-61: a per-device jail must narrow /system/drives the same way it
// narrows every other listing, instead of always exposing the full host
// topology regardless of jail).
func (o *Ops) Drives() ([]Drive, error) {
	if o.IsDenyAll() {
		// Roots() intentionally returns an empty slice for denyAll, but the
		// generic empty-roots behavior below means "no jail". Keep these
		// states separate so an invalid per-device jail cannot expose the
		// host's drive topology.
		return []Drive{}, nil
	}
	all, err := platformDrives()
	if err != nil {
		return nil, err
	}
	return filterDrivesByRoots(all, o.Roots()), nil
}

// filterDrivesByRoots keeps only the drives at or nested within one of roots;
// roots empty (no jail) returns all unchanged. A jailed Ops's entire browsable
// surface is exactly its root(s) (see Jailed) — a drive the jail root sits
// *inside* of (an ancestor mount) can never actually be browsed to, so
// listing it would only leak that it exists; a drive nested *inside* the
// jail (e.g. a second filesystem mounted under it) is real and reachable, so
// it stays. Split out from Drives so the filtering logic is testable without
// a real platformDrives().
func filterDrivesByRoots(all []Drive, roots []string) []Drive {
	if len(roots) == 0 {
		return all
	}
	filtered := make([]Drive, 0, len(all))
	for _, d := range all {
		for _, root := range roots {
			if isUnder(d.Path, root) {
				filtered = append(filtered, d)
				break
			}
		}
	}
	return filtered
}

// --------- Create ---------

// CreateFolder creates a directory (and parents).
func (o *Ops) CreateFolder(path string) (*Entry, error) {
	if o.settings.IsReadOnly() {
		return nil, ErrReadOnly
	}
	resolved, err := o.access(path)
	if err != nil {
		return nil, err
	}
	defer resolved.close()
	if _, err := resolved.stat(); err == nil {
		return nil, ErrConflict
	}
	if err := resolved.mkdirAll(0o755); err != nil {
		return nil, err
	}
	return o.Meta(resolved.full)
}

// CreateFile creates an empty file (creates parent dirs as needed).
func (o *Ops) CreateFile(path string) (*Entry, error) {
	if o.settings.IsReadOnly() {
		return nil, ErrReadOnly
	}
	resolved, err := o.access(path)
	if err != nil {
		return nil, err
	}
	defer resolved.close()
	if _, err := resolved.stat(); err == nil {
		return nil, ErrConflict
	}
	parent := resolved.parent()
	if err := parent.mkdirAll(0o755); err != nil {
		return nil, err
	}
	f, err := resolved.openFile(os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, err
	}
	if err := f.Close(); err != nil {
		return nil, err
	}
	return o.Meta(resolved.full)
}

// --------- WriteContent ---------

// WriteContent writes (or replaces) the content of a file at path with data,
// atomically. If baseModified is non-nil, the existing file's mtime
// (truncated to the second) must match baseModified (also truncated to the
// second) or ErrStale is returned — this gives the caller optimistic
// concurrency: a write based on a stale read is rejected instead of silently
// clobbering newer content.
//
// The write is performed by creating a temp file in the same directory as
// the target, writing+fsyncing it, then renaming it over the target. If the
// target already exists its file mode is preserved; otherwise the new file
// is created with mode 0644.
func (o *Ops) WriteContent(path string, data []byte, baseModified *time.Time) (*Entry, error) {
	if o.settings.IsReadOnly() {
		return nil, ErrReadOnly
	}
	resolved, err := o.access(path)
	if err != nil {
		return nil, err
	}
	defer resolved.close()

	mode := os.FileMode(0o644)
	if baseModified != nil {
		info, statErr := resolved.stat()
		if statErr != nil {
			if os.IsNotExist(statErr) {
				return nil, ErrNotFound
			}
			return nil, statErr
		}
		if !info.ModTime().Truncate(time.Second).Equal(baseModified.Truncate(time.Second)) {
			return nil, ErrStale
		}
		mode = info.Mode()
	} else if info, statErr := resolved.stat(); statErr == nil {
		mode = info.Mode()
	} else if !os.IsNotExist(statErr) {
		return nil, statErr
	}
	// Only a regular file (or a new name) can take content; replacing a
	// folder or a special file through the temp-file rename would fail late.
	if mode&(os.ModeDir|os.ModeType) != 0 {
		return nil, ErrNotRegular
	}

	if err := resolved.parent().mkdirAll(0o755); err != nil {
		return nil, err
	}

	tmpPath, tmp, err := resolved.createTemp("." + filepath.Base(resolved.full) + ".rfe-tmp-*")
	if err != nil {
		return nil, err
	}
	cleanup := func() {
		_ = tmpPath.remove()
	}

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		cleanup()
		return nil, err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		cleanup()
		return nil, err
	}
	if err := tmp.Chmod(mode); err != nil {
		tmp.Close()
		cleanup()
		return nil, err
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return nil, err
	}
	if err := tmpPath.renameTo(resolved); err != nil {
		cleanup()
		return nil, err
	}

	return o.Meta(resolved.full)
}

// PublishReader streams src into a same-directory temporary file and publishes
// it at path with rooted operations. Overwrite uses an atomic rename. Without
// overwrite it first uses a hard link for atomic no-replace publication. If
// the destination filesystem does not support hard links, it falls back to an
// O_EXCL copy, preserving the no-replace behavior of older transfer
// publication. The temporary and destination names are operated on through
// the same os.Root when a jail is configured.
func (o *Ops) PublishReader(path string, src io.Reader, overwrite bool) (os.FileInfo, error) {
	if o.settings.IsReadOnly() {
		return nil, ErrReadOnly
	}
	if src == nil {
		return nil, fmt.Errorf("publish source is nil")
	}
	resolved, err := o.access(path)
	if err != nil {
		return nil, err
	}
	defer resolved.close()

	if err := resolved.parent().mkdirAll(0o755); err != nil {
		return nil, err
	}
	tmpPath, tmp, err := resolved.createTemp(".rfe-upload-*")
	if err != nil {
		return nil, err
	}
	cleanup := func() { _ = tmpPath.remove() }
	if _, err := io.Copy(tmp, src); err != nil {
		_ = tmp.Close()
		cleanup()
		return nil, err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		cleanup()
		return nil, err
	}
	info, err := tmp.Stat()
	if err != nil {
		_ = tmp.Close()
		cleanup()
		return nil, err
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return nil, err
	}

	if overwrite {
		if err := tmpPath.renameTo(resolved); err != nil {
			cleanup()
			return nil, err
		}
		return info, nil
	}
	if err := tmpPath.linkTo(resolved); err != nil {
		if errors.Is(err, os.ErrExist) {
			cleanup()
			return nil, ErrConflict
		}
		if copyErr := copyNoReplaceRooted(tmpPath, resolved); copyErr != nil {
			cleanup()
			if errors.Is(copyErr, os.ErrExist) {
				return nil, ErrConflict
			}
			return nil, copyErr
		}
	}
	// The destination now refers to the fully-written inode. Removing the
	// temporary name is cleanup only; publication has already succeeded.
	_ = tmpPath.remove()
	return info, nil
}

// copyNoReplaceRooted is the compatibility fallback for filesystems that
// reject hard links. O_EXCL still makes destination creation race-safe, and
// both opens/removal stay rooted; as with the prior fallback, the destination
// may be visible while its bytes are being copied.
func copyNoReplaceRooted(src, dst *securePath) error {
	in, err := src.open()
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := dst.openFile(os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	cleanup := func() {
		_ = out.Close()
		_ = dst.remove()
	}
	if _, err := io.Copy(out, in); err != nil {
		cleanup()
		return err
	}
	if err := out.Sync(); err != nil {
		cleanup()
		return err
	}
	if err := out.Close(); err != nil {
		_ = dst.remove()
		return err
	}
	return nil
}

// --------- Rename ---------

// Rename moves src to dst.
func (o *Ops) Rename(src, dst string) (*Entry, error) {
	if o.settings.IsReadOnly() {
		return nil, ErrReadOnly
	}
	// dst might not exist yet; Resolve handles non-existent paths by
	// resolving symlinks on the deepest existing ancestor. Both names are
	// entries: renaming a symlink renames the link, not its target.
	resSrc, resDst, err := o.accessPairWith(o.resolveEntry, o.resolveEntry, src, dst)
	if err != nil {
		return nil, err
	}
	defer resSrc.close()
	if resDst.root != resSrc.root {
		defer resDst.close()
	}
	// A rename never replaces an existing file or folder. This check gives the
	// early answer; the rename itself is no-replace, so a file created between
	// the check and the rename is refused too. Renaming an entry onto itself
	// stays the no-op it was.
	sameEntry := false
	if dstInfo, derr := resDst.lstat(); derr == nil {
		srcInfo, serr := resSrc.lstat()
		if serr != nil {
			if os.IsNotExist(serr) {
				return nil, errSrcMissing // a vanished source is not found, not a conflict
			}
			return nil, serr
		}
		if !os.SameFile(srcInfo, dstInfo) {
			return nil, ErrConflict
		}
		sameEntry = true
	} else if !os.IsNotExist(derr) {
		return nil, derr
	}
	if sameEntry && resSrc.full == resDst.full {
		// The same entry under the same path: nothing to move. (os.Root.Rename
		// of a folder onto itself fails with "file exists"; a file was a no-op.)
		info, err := resDst.lstat()
		if err != nil {
			return nil, err
		}
		e := entryFromSecureInfo(info, resDst, true)
		return &e, nil
	}
	if !sameEntry {
		if _, serr := resSrc.lstat(); serr != nil {
			if os.IsNotExist(serr) {
				return nil, errSrcMissing
			}
			return nil, serr
		}
	}
	// A folder cannot move into its own subtree; refuse before creating the
	// destination's parents, which would land inside the source.
	if resDst.full != resSrc.full && isUnder(resDst.full, resSrc.full) {
		return nil, ErrConflict
	}
	if err := resDst.parent().mkdirAll(0o755); err != nil {
		return nil, err
	}
	rename := func(d *securePath) error { return renameNoReplaceFn(resSrc, d) }
	if sameEntry {
		rename = resSrc.renameTo
	}
	if err := rename(resDst); err != nil {
		if os.IsNotExist(err) {
			// The source vanished between the check and the rename.
			if _, serr := resSrc.lstat(); os.IsNotExist(serr) {
				return nil, errSrcMissing
			}
		}
		if resSrc.root != resDst.root || isCrossDevice(err) {
			err = moveAcrossRootsSecure(resSrc, resDst)
		}
		if err != nil {
			if errors.Is(err, ErrConflict) || errors.Is(err, fs.ErrExist) {
				return nil, ErrConflict
			}
			return nil, err
		}
	}
	// Describe the renamed entry itself: Meta follows a symlink to its target.
	info, err := resDst.lstat()
	if err != nil {
		return nil, err
	}
	e := entryFromSecureInfo(info, resDst, true)
	return &e, nil
}

// --------- Copy ---------

// BatchResult is the per-source outcome of a batch Copy/Move/Delete.
type BatchResult struct {
	Path  string `json:"path"`
	OK    bool   `json:"ok"`
	Error *Error `json:"error,omitempty"`
}

// Error is the JSON error envelope from the contract.
type Error struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// Copy copies sources into destDir.
//
// Per-source destination collisions are resolved by precedence:
//  1. duplicate=true: auto-rename the destination (keep-both); overwrite is
//     ignored in this case.
//  2. else overwrite=true: replace the existing destination (remove it, then
//     copy the source over it).
//  3. else: a CONFLICT BatchResult for that source (unchanged).
//
// Two guards apply regardless of duplicate/overwrite, to avoid destroying
// data:
//   - If the resolved source and computed destination are the same path, the
//     copy is a no-op (reported as OK) rather than removing-then-copying a
//     path onto itself.
//   - The destination is never removed if it is an ancestor of (or equal to)
//     the source, since deleting it would delete the source itself; this
//     falls back to a CONFLICT result.
func (o *Ops) Copy(sources []string, destDir string, duplicate, overwrite bool) []BatchResult {
	if o.settings.IsReadOnly() {
		res := make([]BatchResult, len(sources))
		for i, s := range sources {
			res[i] = BatchResult{Path: s, Error: &Error{Code: "READ_ONLY", Message: ErrReadOnly.Error()}}
		}
		return res
	}
	results := make([]BatchResult, len(sources))
	for i, src := range sources {
		resSrc, dstDir, err := o.accessPair(src, destDir)
		if err != nil {
			results[i] = BatchResult{Path: src, Error: apiErr("FORBIDDEN", err.Error())}
			continue
		}
		// Name the copy after the entry the caller named: a copied symlink
		// keeps the link's name, not its target's.
		dst := dstDir.child(filepath.Base(resSrc.full))
		if name := filepath.Base(filepath.Clean(src)); name != string(filepath.Separator) && name != "." {
			dst = dstDir.child(name)
		}

		// Never copy a directory into itself or its own subtree: each level
		// would copy the tree again until the path length limit.
		if info, lerr := resSrc.lstat(); lerr == nil && info.IsDir() && isUnder(dstDir.full, resSrc.full) {
			results[i] = BatchResult{Path: src, Error: apiErr("CONFLICT", "cannot copy a folder into itself")}
			resSrc.close()
			if dstDir.root != resSrc.root {
				dstDir.close()
			}
			continue
		}

		// Same-path guard: copying a path onto itself is a no-op — UNLESS
		// duplicate is requested, in which case copying into the source's own
		// directory means "make a renamed copy" (duplicate-in-place), handled
		// by the auto-rename below.
		if resSrc.full == dst.full && !duplicate {
			results[i] = BatchResult{Path: src, OK: true}
			resSrc.close()
			if dstDir.root != resSrc.root {
				dstDir.close()
			}
			continue
		}

		var backup *securePath // the replaced destination, kept until the copy has succeeded
		// lstat: a dangling symlink at the destination is an existing name, not a free one.
		if _, err := dst.lstat(); err == nil {
			switch {
			case duplicate:
				renamed, rerr := autoRenameSecure(dst)
				if rerr != nil {
					results[i] = BatchResult{Path: src, Error: apiErr("CONFLICT", rerr.Error())}
					resSrc.close()
					if dstDir.root != resSrc.root {
						dstDir.close()
					}
					continue
				}
				dst = renamed
			case overwrite:
				// Ancestor guard: never remove a destination that contains
				// the source — that would delete the source itself.
				if isUnder(resSrc.full, dst.full) {
					results[i] = BatchResult{Path: src, Error: apiErr("CONFLICT", "destination already exists")}
					resSrc.close()
					if dstDir.root != resSrc.root {
						dstDir.close()
					}
					continue
				}
				var err error
				backup, err = moveAside(dstDir, dst)
				if err != nil {
					results[i] = BatchResult{Path: src, Error: apiErr("COPY_FAILED", err.Error())}
					resSrc.close()
					if dstDir.root != resSrc.root {
						dstDir.close()
					}
					continue
				}
			default:
				results[i] = BatchResult{Path: src, Error: apiErr("CONFLICT", "destination already exists")}
				resSrc.close()
				if dstDir.root != resSrc.root {
					dstDir.close()
				}
				continue
			}
		}
		if made, err := copyTree(resSrc, dst); err != nil {
			msg := err.Error()
			switch {
			case !made:
				// The copy created nothing (for instance the destination was taken by
				// someone else after the checks): there is nothing of ours to remove.
				msg += restoreAsideKeep(dst, backup)
			case backup != nil:
				msg += restoreAside(dst, backup)
			default:
				// Nothing was replaced, so the failed copy must leave no partial tree behind.
				if rmErr := dst.removeAll(); rmErr != nil {
					msg += "; a partial copy remains at " + dst.full
				}
			}
			results[i] = BatchResult{Path: src, Error: apiErr(batchCode(err, "COPY_FAILED"), batchMessage(err, msg))}
		} else {
			results[i] = BatchResult{Path: src, OK: true}
			discardAside(backup)
		}
		resSrc.close()
		if dstDir.root != resSrc.root {
			dstDir.close()
		}
	}
	return results
}

// Move moves sources into destDir (batch).
//
// Per-source destination collisions follow the same precedence as Copy:
// duplicate (auto-rename) wins, else overwrite replaces the existing
// destination (os.RemoveAll then os.Rename), else CONFLICT. The same
// same-path and ancestor guards apply (see Copy).
func (o *Ops) Move(sources []string, destDir string, duplicate, overwrite bool) []BatchResult {
	if o.settings.IsReadOnly() {
		res := make([]BatchResult, len(sources))
		for i, s := range sources {
			res[i] = BatchResult{Path: s, Error: &Error{Code: "READ_ONLY", Message: ErrReadOnly.Error()}}
		}
		return res
	}
	results := make([]BatchResult, len(sources))
	for i, src := range sources {
		resSrc, dstDir, err := o.accessPairWith(o.resolveEntry, o.Resolve, src, destDir)
		if err != nil {
			results[i] = BatchResult{Path: src, Error: apiErr("FORBIDDEN", err.Error())}
			continue
		}
		dst := dstDir.child(filepath.Base(resSrc.full))

		// Same-path guard: moving a path onto itself is a no-op.
		if resSrc.full == dst.full {
			results[i] = BatchResult{Path: src, OK: true}
			resSrc.close()
			if dstDir.root != resSrc.root {
				dstDir.close()
			}
			continue
		}

		// Containment guard: a folder can never move into itself or one of its
		// own subfolders. Without it, overwrite would delete a subtree of the
		// source before the rename fails.
		if isUnder(dst.full, resSrc.full) {
			results[i] = BatchResult{Path: src, Error: apiErr("MOVE_FAILED", "cannot move a folder into itself")}
			resSrc.close()
			if dstDir.root != resSrc.root {
				dstDir.close()
			}
			continue
		}

		var backup *securePath // the replaced destination, kept until the move has succeeded
		if _, err := dst.lstat(); err == nil {
			switch {
			case duplicate:
				renamed, rerr := autoRenameSecure(dst)
				if rerr != nil {
					results[i] = BatchResult{Path: src, Error: apiErr("CONFLICT", rerr.Error())}
					resSrc.close()
					if dstDir.root != resSrc.root {
						dstDir.close()
					}
					continue
				}
				dst = renamed
			case overwrite:
				// Ancestor guard: never remove a destination that contains
				// the source — that would delete the source itself.
				if isUnder(resSrc.full, dst.full) {
					results[i] = BatchResult{Path: src, Error: apiErr("CONFLICT", "destination already exists")}
					resSrc.close()
					if dstDir.root != resSrc.root {
						dstDir.close()
					}
					continue
				}
				var err error
				backup, err = moveAside(dstDir, dst)
				if err != nil {
					results[i] = BatchResult{Path: src, Error: apiErr("MOVE_FAILED", err.Error())}
					resSrc.close()
					if dstDir.root != resSrc.root {
						dstDir.close()
					}
					continue
				}
			default:
				results[i] = BatchResult{Path: src, Error: apiErr("CONFLICT", "destination already exists")}
				resSrc.close()
				if dstDir.root != resSrc.root {
					dstDir.close()
				}
				continue
			}
		}
		if err := dstDir.mkdirAll(0o755); err != nil {
			results[i] = BatchResult{Path: src, Error: apiErr("MOVE_FAILED", err.Error()+restoreAside(dst, backup))}
			resSrc.close()
			if dstDir.root != resSrc.root {
				dstDir.close()
			}
			continue
		}
		if err := renameNoReplaceFn(resSrc, dst); err != nil {
			if resSrc.root != dst.root || isCrossDevice(err) {
				err = moveAcrossRootsSecure(resSrc, dst)
			}
			if err != nil {
				msg := err.Error()
				var kept *copyKeptError
				if errors.As(err, &kept) {
					// The complete copy is at dst; restoring the replaced file would delete it.
					discardAside(backup)
				} else {
					// A failed move leaves nothing of ours at dst (a partial copy was already
					// removed), so whatever is there now was not created by this move.
					msg += restoreAsideKeep(dst, backup)
				}
				results[i] = BatchResult{Path: src, Error: apiErr(batchCode(err, "MOVE_FAILED"), batchMessage(err, msg))}
			} else {
				results[i] = BatchResult{Path: src, OK: true}
				discardAside(backup)
			}
		} else {
			results[i] = BatchResult{Path: src, OK: true}
			discardAside(backup)
		}
		resSrc.close()
		if dstDir.root != resSrc.root {
			dstDir.close()
		}
	}
	return results
}

// Delete deletes one or more paths.
func (o *Ops) Delete(paths []string) []BatchResult {
	if o.settings.IsReadOnly() {
		res := make([]BatchResult, len(paths))
		for i, p := range paths {
			res[i] = BatchResult{Path: p, Error: &Error{Code: "READ_ONLY", Message: ErrReadOnly.Error()}}
		}
		return res
	}
	results := make([]BatchResult, len(paths))
	for i, p := range paths {
		resolved, err := o.accessEntry(p)
		if err != nil {
			results[i] = BatchResult{Path: p, Error: apiErr("FORBIDDEN", err.Error())}
			continue
		}
		if err := resolved.removeAll(); err != nil {
			results[i] = BatchResult{Path: p, Error: apiErr("DELETE_FAILED", err.Error())}
		} else {
			results[i] = BatchResult{Path: p, OK: true}
		}
		resolved.close()
	}
	return results
}

// --------- helpers ---------

// EntryFromInfo builds an Entry from a FileInfo and its full resolved path,
// using the same name/size/mime/mode/timestamp logic as ListDir and Meta.
// Exported so other packages (e.g. the search handler) can build Entry
// values consistently while walking the tree themselves.
func EntryFromInfo(info os.FileInfo, fullPath string) Entry {
	return entryFromInfo(info, fullPath, true)
}

// EntryFromInfoNoSniff is EntryFromInfo without content sniffing: the MIME
// type comes from the extension alone, and an extensionless file reports
// "application/octet-stream" instead of being opened and read.
//
// For one entry the sniff is nothing; for the search index it is the whole
// cost model. That walk visits every file under the roots on every rebuild,
// so sniffing turns a directory walk into an open+read storm across the tree
// (PR-47). Callers showing a single entry should use EntryFromInfo.
func EntryFromInfoNoSniff(info os.FileInfo, fullPath string) Entry {
	return entryFromInfo(info, fullPath, false)
}

// EntryFromRootInfo builds a no-sniff entry from metadata returned by a walk
// over root.FS(). Symlink metadata is resolved through the same open root, so
// the walk cannot stat or readlink a target outside that root. relativePath
// is the slash-separated path supplied by fs.WalkDir; rootPath is retained
// only as the API-visible full path.
func EntryFromRootInfo(root *os.Root, rootPath, relativePath string, info os.FileInfo) Entry {
	if root == nil || info == nil {
		return Entry{}
	}
	relative := filepath.FromSlash(relativePath)
	fullPath := rootPath
	if relative != "." {
		fullPath = filepath.Join(rootPath, relative)
	}
	return entryFromSecureInfo(info, &securePath{
		full: fullPath,
		root: root,
		name: relative,
	}, false)
}

func entryFromInfo(info os.FileInfo, fullPath string, sniff bool) Entry {
	isSymlink := info.Mode()&os.ModeSymlink != 0
	// info comes from Lstat/Readdir, which never follows symlinks — a symlink
	// to a directory would otherwise report IsDir=false and become permanently
	// unnavigable in the UI. Stat (follows) to get the real target type;
	// a broken link falls back to the symlink's own (non-dir) info.
	isDir := info.IsDir()
	if isSymlink {
		if target, err := os.Stat(fullPath); err == nil {
			isDir = target.IsDir()
		}
	}
	mtype := ""
	if !isDir {
		mtype = mimeForPath(fullPath, info, sniff)
	}
	e := Entry{
		Name:      info.Name(),
		Path:      fullPath,
		IsDir:     isDir,
		Size:      info.Size(),
		MimeType:  mtype,
		Mode:      info.Mode().String(),
		Modified:  info.ModTime(),
		Created:   birthTime(info),
		IsSymlink: isSymlink,
	}
	if isSymlink {
		if target, err := os.Readlink(fullPath); err == nil {
			e.SymlinkTarget = target
		}
	}
	return e
}

// entryFromSecureInfo mirrors entryFromInfo while opening the item relative to
// an already-open jail root. Directory listings use this variant so a link
// swapped between Readdir and metadata/sniffing cannot redirect a read outside
// the jail.
func entryFromSecureInfo(info os.FileInfo, path *securePath, sniff bool) Entry {
	isSymlink := info.Mode()&os.ModeSymlink != 0
	isDir := info.IsDir()
	if isSymlink {
		if target, err := path.stat(); err == nil {
			isDir = target.IsDir()
		}
	}
	mtype := ""
	if !isDir {
		mtype = mimeForSecurePath(path, info, sniff)
	}
	e := Entry{
		Name:      info.Name(),
		Path:      path.full,
		IsDir:     isDir,
		Size:      info.Size(),
		MimeType:  mtype,
		Mode:      info.Mode().String(),
		Modified:  info.ModTime(),
		Created:   birthTime(info),
		IsSymlink: isSymlink,
	}
	if isSymlink {
		if target, err := path.readlink(); err == nil {
			e.SymlinkTarget = target
		}
	}
	return e
}

func mimeForSecurePath(path *securePath, info os.FileInfo, sniff bool) string {
	if ext := filepath.Ext(path.full); ext != "" {
		if m := mime.TypeByExtension(ext); m != "" {
			return m
		}
	}
	if sniff && info.Size() > 0 && !info.IsDir() {
		f, err := path.open()
		if err == nil {
			defer f.Close()
			buf := make([]byte, 512)
			n, _ := f.Read(buf)
			if n > 0 {
				return http.DetectContentType(buf[:n])
			}
		}
	}
	return "application/octet-stream"
}

func mimeForPath(path string, info os.FileInfo, sniff bool) string {
	// First try extension.
	if ext := filepath.Ext(path); ext != "" {
		if m := mime.TypeByExtension(ext); m != "" {
			return m
		}
	}
	// Sniff up to 512 bytes.
	if sniff && info.Size() > 0 && !info.IsDir() {
		f, err := os.Open(path)
		if err == nil {
			defer f.Close()
			buf := make([]byte, 512)
			n, _ := f.Read(buf)
			if n > 0 {
				return http.DetectContentType(buf[:n])
			}
		}
	}
	return "application/octet-stream"
}

// maxAutoRename bounds the "name (N)" search.
const maxAutoRename = 10000

// autoRenameSecure returns a free "name (N)" sibling of dst. When all
// maxAutoRename candidates are taken it fails with an error wrapping
// ErrConflict rather than return a name that exists.
func autoRenameSecure(dst *securePath) (*securePath, error) {
	dir := dst.parent()
	base := filepath.Base(dst.full)
	ext := filepath.Ext(base)
	stem := strings.TrimSuffix(base, ext)
	for i := 1; i <= maxAutoRename; i++ {
		candidate := dir.child(fmt.Sprintf("%s (%d)%s", stem, i, ext))
		// Any lstat failure other than "exists" ends the search: the caller's
		// next operation reports the real error instead of this looping.
		if _, err := candidate.lstat(); err != nil {
			return candidate, nil
		}
	}
	return nil, fmt.Errorf("no free name left for %q: %w", base, ErrConflict)
}

// notCreatedError means the copy's top-level destination already existed (or
// appeared) and so was not created by the copy: it must not be removed.
type notCreatedError struct{ path string }

func (e *notCreatedError) Error() string { return ErrConflict.Error() + ": " + e.path }
func (e *notCreatedError) Unwrap() error { return ErrConflict }

// batchCode picks the batch error code: a destination that exists or appeared
// is a CONFLICT, a file system with no atomic no-replace rename is
// NOT_IMPLEMENTED, anything else is def.
func batchCode(err error, def string) string {
	if errors.Is(err, ErrConflict) || errors.Is(err, fs.ErrExist) {
		return "CONFLICT"
	}
	if errors.Is(err, ErrNoAtomicRename) {
		return "NOT_IMPLEMENTED"
	}
	return def
}

// batchMessage is the per-item message for err: fixed text for
// ErrNoAtomicRename (the raw error carries absolute paths, PR-53), msg
// otherwise. msg is err.Error() plus an optional note on where a replaced
// entry was kept; that note stays, so the owner can find the entry.
func batchMessage(err error, msg string) string {
	if errors.Is(err, ErrNoAtomicRename) {
		rest := strings.TrimPrefix(msg, err.Error())
		if rest == "" {
			return NoAtomicRenameMessage
		}
		// A note means a replaced entry could not be put back, so "nothing was
		// changed" would be false: drop that clause and let the note speak.
		return strings.TrimSuffix(NoAtomicRenameMessage, "; nothing was changed") + rest
	}
	return msg
}

// restoreAsideKeep puts a replaced destination back without ever replacing or
// deleting what is at dst now. If dst is occupied, the replaced entry stays at
// its hidden name and the returned note says where.
func restoreAsideKeep(dst, aside *securePath) string {
	if aside == nil {
		return ""
	}
	err := aside.renameNoReplaceTo(dst)
	if err == nil {
		return ""
	}
	if errors.Is(err, ErrNoAtomicRename) {
		// No atomic no-replace rename and no hard links here. The aside entry is
		// the user's data and must come back, so use a plain rename, but only
		// while dst is verified free right before. BEST EFFORT, not atomic: an
		// entry created in that window would be replaced. dst was emptied by this
		// operation moments ago, so that is the lesser risk.
		if _, serr := dst.lstat(); os.IsNotExist(serr) && aside.renameTo(dst) == nil {
			return ""
		}
	}
	return keptNote(aside)
}

// keptNote says that a replaced entry stays under its hidden name. It gives the
// name only: the folder is the one the owner already named, and an absolute
// path must not reach the client (PR-53).
func keptNote(aside *securePath) string {
	return "; the replaced item was kept under the hidden name " + filepath.Base(aside.full) + " in the same folder"
}

// copyTree copies a tree using path operations rooted at each source/destination
// jail handle. It recreates symlinks without dereferencing them and only ever
// creates: every directory, file and link is made exclusively, so an entry that
// already exists (or appears meanwhile) is never truncated, replaced or
// redirected. An occupied top-level destination fails with a *notCreatedError.
// created reports whether the top-level destination was made by this copy: only
// then may the caller remove it after a failure.
//
// A source folder's restrictive mode (for instance 0555) is applied only after the
// whole tree has been copied, deepest folder first: a read-only folder made earlier
// would stop the caller from removing the partial copy after a later failure.
func copyTree(src, dst *securePath) (created bool, err error) {
	var modes []dirMode
	err = copyEntry(src, dst, &created, &modes)
	if err != nil {
		return created, err
	}
	for _, m := range modes {
		if err := m.apply(); err != nil {
			return created, err
		}
	}
	return created, nil
}

// dirMode is a copied folder whose source mode is applied once the copy is complete.
type dirMode struct {
	dir  *securePath
	perm fs.FileMode
}

func (m dirMode) apply() error {
	d, err := m.dir.open()
	if err != nil {
		return err
	}
	chmodErr := d.Chmod(m.perm)
	closeErr := d.Close()
	if chmodErr != nil {
		return chmodErr
	}
	return closeErr
}

// copyEntry copies one entry. made is non-nil only for the top-level call and is
// set once that entry exists because this call created it. modes collects the
// folder modes to apply after the copy, children before their parent.
func copyEntry(src, dst *securePath, made *bool, modes *[]dirMode) error {
	created := func(err error) error {
		switch {
		case made == nil:
		case err == nil:
			*made = true
		case errors.Is(err, fs.ErrExist):
			return &notCreatedError{path: dst.full}
		}
		return err
	}
	info, err := src.lstat()
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		target, err := src.readlink()
		if err != nil {
			return err
		}
		if err := dst.parent().mkdirAll(0o755); err != nil {
			return err
		}
		if dst.root != nil {
			return created(dst.root.Symlink(target, dst.name))
		}
		return created(os.Symlink(target, dst.full))
	}
	if info.IsDir() {
		// The owner can always write while the children are copied; the source mode is applied after,
		// so a read-only source folder (0555) can still be copied whole (audit F-144).
		if err := dst.parent().mkdirAll(0o755); err != nil {
			return err
		}
		if err := created(dst.mkdir(info.Mode().Perm() | 0o700)); err != nil {
			return err
		}
		dir, err := src.open()
		if err != nil {
			return err
		}
		entries, readErr := dir.Readdir(-1)
		closeErr := dir.Close()
		if readErr != nil {
			return readErr
		}
		if closeErr != nil {
			return closeErr
		}
		for _, entry := range entries {
			if err := copyEntry(src.child(entry.Name()), dst.child(entry.Name()), nil, modes); err != nil {
				return err
			}
		}
		if info.Mode().Perm()|0o700 != info.Mode().Perm() {
			*modes = append(*modes, dirMode{dir: dst, perm: info.Mode().Perm()})
		}
		return nil
	}
	in, err := src.open()
	if err != nil {
		return err
	}
	defer in.Close()
	if err := dst.parent().mkdirAll(0o755); err != nil {
		return err
	}
	out, err := dst.openFile(os.O_CREATE|os.O_EXCL|os.O_WRONLY, info.Mode())
	if err := created(err); err != nil {
		return err
	}
	_, copyErr := io.Copy(out, in)
	closeErr := out.Close()
	if copyErr != nil {
		return copyErr
	}
	return closeErr
}

func apiErr(code, msg string) *Error {
	return &Error{Code: code, Message: msg}
}

// moveAside renames an existing destination out of the way (to a hidden sibling) so an overwrite
// can put the new content in place first and only then delete the old one. Deleting first lost the
// destination whenever the copy or move then failed (audit F-41, F-120).
func moveAside(dir, dst *securePath) (*securePath, error) {
	var b [6]byte
	if _, err := rand.Read(b[:]); err != nil {
		return nil, err
	}
	// The name is fixed-length: appending the old name would overflow NAME_MAX for a long destination name.
	aside := dir.child(".rfe-old-" + hex.EncodeToString(b[:]))
	if err := dst.renameTo(aside); err != nil {
		return nil, err
	}
	return aside, nil
}

// restoreAside undoes moveAside after a failed overwrite: the partial result is removed and the
// original destination put back. The returned note (empty on success) says where a partial copy
// or the replaced entry was left when either step failed.
func restoreAside(dst, aside *securePath) string {
	if aside == nil {
		return ""
	}
	if err := dst.removeAll(); err != nil {
		return "; a partial copy remains at " + dst.full + " and the replaced entry was kept at " + aside.full
	}
	if beforeAsidePutBack != nil {
		beforeAsidePutBack()
	}
	// Put back without replacing: an entry created at dst since the removal
	// stays, and the replaced entry stays at its hidden name.
	return restoreAsideKeep(dst, aside)
}

// beforeAsidePutBack is a test seam: it runs between restoreAside removing the
// partial result and putting the replaced entry back.
var beforeAsidePutBack func()

// discardAside deletes the replaced destination once its replacement is complete.
func discardAside(aside *securePath) {
	if aside != nil {
		_ = aside.removeAll()
	}
}

// errSrcMissing is Rename's vanished-source error: it matches ErrNotFound (the
// server maps it to 404) and fs.ErrNotExist (callers that test for a raw
// not-exist error keep working).
var errSrcMissing = fmt.Errorf("%w: %w", ErrNotFound, fs.ErrNotExist)
