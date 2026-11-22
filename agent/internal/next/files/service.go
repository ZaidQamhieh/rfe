package files

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
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/ZaidQamhieh/rfe/agent/internal/next/api"
	"github.com/ZaidQamhieh/rfe/agent/internal/next/rootfs"
)

// Listing limits: a page holds DefaultListLimit entries unless the caller asks
// for another size, and never more than MaxListLimit.
const (
	DefaultListLimit = 200
	MaxListLimit     = 1000
)

// Child counts: at most maxCountedDirs folders per page get a childCount, and a
// folder with more than MaxChildCount items reports MaxChildCount.
const (
	maxCountedDirs = 60
	MaxChildCount  = 1000
)

// Checksum algorithm names accepted by Checksum ("" means AlgoSHA256).
const (
	AlgoSHA256 = "sha256"
	AlgoSHA1   = "sha1"
	AlgoMD5    = "md5"
)

// staleGranularity is the precision at which the modification time a client
// read is compared with the file's current one (JSON dates and some file
// systems differ below it).
const staleGranularity = time.Second

// Hidden names used while a change is being prepared. They are fixed length
// (prefix plus hex of tempNameBytes random bytes) so they never exceed a file
// name limit, whatever the target's name is.
const (
	tempPrefix    = ".rfe-tmp-"
	asidePrefix   = ".rfe-old-"
	tempNameBytes = 6
	tempTries     = 8
)

// tempFilePerm is the permission of a staged file until it is published.
const tempFilePerm fs.FileMode = 0o600

// newFilePerm is the permission of a file the service makes from bytes it
// received or built (a published upload, a compress output): owner only, as the
// old agent gives them, since its content may be private. An upload that
// overwrites a regular file keeps that file's permission instead.
const newFilePerm fs.FileMode = 0o600

// Service performs file operations inside the folders of one rootfs.Set. A
// per-device jail or read-only mode is expressed by the Set the caller passes
// (Set.Narrow, Set.ReadOnly), never by a flag here.
type Service struct {
	set   *rootfs.Set
	trash *trashStore // nil until WithTrash
}

// New returns a Service that works inside set, which must not be nil.
func New(set *rootfs.Set) *Service { return &Service{set: set} }

// open prepares a handle for path, with the error already classified.
func (s *Service) open(path string) (*rootfs.Handle, error) {
	h, err := s.set.Open(path)
	return h, classify(err)
}

// probe looks at the entry itself (no symlink followed). found is false when
// the name is free; any other failure is returned.
func probe(h *rootfs.Handle) (info fs.FileInfo, found bool, err error) {
	info, err = h.Lstat()
	switch {
	case err == nil:
		return info, true, nil
	case isMissing(err):
		return nil, false, nil
	}
	return nil, false, classify(err)
}

// List lists the folder at path in name order, one page at a time: entries
// whose name sorts after cursor, at most limit of them. NextCursor is the last
// returned name when more entries follow. Hidden (dot) files are listed like
// any other; protected entries are left out by rootfs.
func (s *Service) List(path, cursor string, limit int) (*Listing, error) {
	h, err := s.open(path)
	if err != nil {
		return nil, err
	}
	info, err := h.Stat()
	if err != nil {
		return nil, classify(err)
	}
	if !info.IsDir() {
		if info.Mode().IsRegular() {
			return nil, fmt.Errorf("%w: not a folder", api.ErrBadRequest)
		}
		return nil, fmt.Errorf("%w: not a folder", api.ErrNotAFile)
	}
	entries, err := h.ReadDir()
	if err != nil {
		return nil, classify(err)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })

	if limit <= 0 {
		limit = DefaultListLimit
	}
	limit = min(limit, MaxListLimit)
	start := 0
	if cursor != "" {
		start = sort.Search(len(entries), func(i int) bool { return entries[i].Name() > cursor })
	}
	page := entries[start:]
	var next *string
	if len(page) > limit {
		page = page[:limit]
		last := page[len(page)-1].Name()
		next = &last
	}

	out := make([]Entry, 0, len(page))
	counted := 0
	for _, de := range page {
		fi, err := de.Info()
		if isMissing(err) {
			continue // removed since the folder was read
		}
		if err != nil {
			return nil, classify(err)
		}
		child, err := s.open(filepath.Join(h.Path(), de.Name()))
		if err != nil {
			return nil, err
		}
		e := entryFor(child, fi)
		if e.IsDir && !e.IsSymlink && counted < maxCountedDirs {
			counted++
			e.ChildCount = countChildren(child)
		}
		out = append(out, e)
	}
	return &Listing{Path: h.Path(), Entries: out, NextCursor: next}, nil
}

// Meta describes the entry at path itself: a symlink is described, not its
// target.
func (s *Service) Meta(path string) (*Entry, error) {
	h, err := s.open(path)
	if err != nil {
		return nil, err
	}
	info, err := h.Lstat()
	if err != nil {
		return nil, classify(err)
	}
	e := entryFor(h, info)
	return &e, nil
}

// Checksum returns the hex digest of the regular file at path. algo is one of
// the Algo constants ("" means sha256); anything else is refused before the
// file is opened.
func (s *Service) Checksum(path, algo string) (string, error) {
	var h hash.Hash
	switch algo {
	case "", AlgoSHA256:
		h = sha256.New()
	case AlgoSHA1:
		h = sha1.New()
	case AlgoMD5:
		h = md5.New()
	default:
		return "", fmt.Errorf("%w: unsupported algorithm (use sha256, sha1 or md5)", api.ErrBadRequest)
	}
	fh, err := s.open(path)
	if err != nil {
		return "", err
	}
	info, err := fh.Stat()
	if err != nil {
		return "", classify(err)
	}
	if info.IsDir() {
		return "", fmt.Errorf("%w: cannot checksum a folder", api.ErrBadRequest)
	}
	f, err := fh.Open()
	if err != nil {
		return "", classify(err)
	}
	defer f.Close()
	if _, err := io.Copy(h, f); err != nil {
		return "", classify(err)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// CreateFolder creates the folder at path and any missing parents. A name that
// is already taken (even by a dangling symlink) is a conflict.
func (s *Service) CreateFolder(path string) (*Entry, error) {
	h, err := s.open(path)
	if err != nil {
		return nil, err
	}
	if _, found, err := probe(h); err != nil {
		return nil, err
	} else if found {
		return nil, fmt.Errorf("%w: name already exists", api.ErrConflict)
	}
	if err := s.ensureParent(h); err != nil {
		return nil, err
	}
	if err := h.Mkdir(rootfs.DirMode); err != nil {
		return nil, classify(err)
	}
	return s.Meta(h.Path())
}

// CreateFile creates an empty file at path, and any missing parent folders. A
// name that is already taken is a conflict, decided by the exclusive create
// itself so two callers cannot both succeed.
func (s *Service) CreateFile(path string) (*Entry, error) {
	h, err := s.open(path)
	if err != nil {
		return nil, err
	}
	if err := s.ensureParent(h); err != nil {
		return nil, err
	}
	f, err := h.OpenFile(os.O_WRONLY|os.O_CREATE|os.O_EXCL, rootfs.FileMode)
	if err != nil {
		return nil, classify(err)
	}
	if err := f.Close(); err != nil {
		return nil, classify(err)
	}
	return s.Meta(h.Path())
}

// WriteContent replaces (or creates) the file at path with data, atomically:
// the bytes go to a hidden temporary file next to it which is then renamed over
// it, so a reader sees the old or the new content, never a mix. When
// baseModified is set it must equal the file's current modification time to the
// second, else api.ErrStale and nothing is written; the check is repeated just
// before the rename. An existing file keeps its permission bits.
func (s *Service) WriteContent(path string, data []byte, baseModified *time.Time) (*Entry, error) {
	h, err := s.open(path)
	if err != nil {
		return nil, err
	}
	mode := rootfs.FileMode
	info, err := h.Stat()
	switch {
	case err == nil:
		if !info.Mode().IsRegular() {
			return nil, fmt.Errorf("%w: not a regular file", api.ErrNotAFile)
		}
		if isStale(info, baseModified) {
			return nil, api.ErrStale
		}
		mode = info.Mode().Perm()
	case isMissing(err) && baseModified == nil:
		// A new file: created below.
	default:
		return nil, classify(err)
	}
	if err := s.ensureParent(h); err != nil {
		return nil, err
	}
	tmp, err := s.stage(filepath.Dir(h.Path()), mode, func(w io.Writer) error {
		_, err := w.Write(data)
		return err
	})
	if err != nil {
		return nil, err
	}
	if baseModified != nil {
		again, err := h.Stat()
		if err != nil {
			return nil, s.discard(tmp, err)
		}
		if isStale(again, baseModified) {
			return nil, s.discard(tmp, api.ErrStale)
		}
	}
	if err := tmp.Rename(h); err != nil {
		return nil, s.discard(tmp, err)
	}
	return s.Meta(h.Path())
}

// isStale reports whether the file's modification time differs from the one the
// caller read, to the second. A nil base means "no check".
func isStale(info fs.FileInfo, base *time.Time) bool {
	return base != nil && !info.ModTime().Truncate(staleGranularity).Equal(base.Truncate(staleGranularity))
}

// PublishReader streams src into a hidden temporary file next to path and then
// publishes it under its final name. With overwrite the file is renamed over
// an existing one; without it the rename fails with api.ErrConflict if the name
// is taken, and an existing entry is never touched. A new file is owner-only
// (newFilePerm); a replaced regular file keeps its permission bits. A failed
// copy leaves no temporary file behind.
func (s *Service) PublishReader(path string, src io.Reader, overwrite bool) (*Entry, error) {
	if src == nil {
		return nil, fmt.Errorf("%w: no content to publish", api.ErrBadRequest)
	}
	h, err := s.open(path)
	if err != nil {
		return nil, err
	}
	mode := newFilePerm
	if info, found, err := probe(h); err != nil {
		return nil, err
	} else if found {
		if info.IsDir() {
			return nil, fmt.Errorf("%w: a folder has this name", api.ErrConflict)
		}
		if overwrite && info.Mode().IsRegular() {
			mode = info.Mode().Perm()
		}
	}
	if err := s.ensureParent(h); err != nil {
		return nil, err
	}
	tmp, err := s.stage(filepath.Dir(h.Path()), mode, func(w io.Writer) error {
		_, err := io.Copy(w, src)
		return err
	})
	if err != nil {
		return nil, err
	}
	if overwrite {
		err = tmp.Rename(h)
	} else {
		err = tmp.RenameNoReplace(h)
	}
	if err != nil {
		return nil, s.discard(tmp, err)
	}
	return s.Meta(h.Path())
}

// ensureParent creates the missing parent folders of h.
func (s *Service) ensureParent(h *rootfs.Handle) error {
	parent, err := s.open(filepath.Dir(h.Path()))
	if err != nil {
		return err
	}
	return classify(parent.MkdirAll(rootfs.DirMode))
}

// randomName returns prefix plus tempNameBytes random bytes in hex.
func randomName(prefix string) (string, error) {
	var b [tempNameBytes]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return prefix + hex.EncodeToString(b[:]), nil
}

// reserve makes a new hidden entry in dir: it picks a random name with prefix
// and calls create on it, trying another name when create reports the name is
// taken. create does the exclusive creation (or the move) itself, so a name is
// never used twice.
func (s *Service) reserve(dir, prefix string, create func(*rootfs.Handle) error) (*rootfs.Handle, error) {
	for range tempTries {
		name, err := randomName(prefix)
		if err != nil {
			return nil, err
		}
		h, err := s.open(filepath.Join(dir, name))
		if err != nil {
			return nil, err
		}
		err = create(h)
		if errors.Is(err, fs.ErrExist) {
			continue
		}
		if err != nil {
			return nil, classify(err)
		}
		return h, nil
	}
	return nil, fmt.Errorf("%w: no free temporary name", api.ErrConflict)
}

// stage writes content into a new hidden file in dir through fill, flushes it
// to disk and gives it mode. The file is removed again if any step fails.
func (s *Service) stage(dir string, mode fs.FileMode, fill func(io.Writer) error) (*rootfs.Handle, error) {
	var f *os.File
	tmp, err := s.reserve(dir, tempPrefix, func(h *rootfs.Handle) (err error) {
		f, err = h.OpenFile(os.O_WRONLY|os.O_CREATE|os.O_EXCL, tempFilePerm)
		return err
	})
	if err != nil {
		return nil, err
	}
	err = fill(f)
	if err == nil {
		err = f.Sync()
	}
	if err == nil {
		err = f.Chmod(mode)
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return nil, s.discard(tmp, err)
	}
	return tmp, nil
}

// discard removes the hidden file h after a failure and returns err, classified.
// A failed removal is recorded behind err, not shown in its text.
func (s *Service) discard(h *rootfs.Handle, err error) error {
	return s.discardWith(h, err, h.Remove)
}

// discardTree is discard for a folder tree.
func (s *Service) discardTree(h *rootfs.Handle, err error) error {
	return s.discardWith(h, err, h.RemoveAll)
}

// discardWith classifies err and records a failed remove of h, made by remove,
// behind it.
func (s *Service) discardWith(h *rootfs.Handle, err error, remove func() error) error {
	err = classify(err)
	if rmErr := remove(); rmErr != nil && !isMissing(rmErr) {
		err = withCause(err, rmErr)
	}
	return err
}
