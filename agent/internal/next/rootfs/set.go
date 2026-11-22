package rootfs

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
	"syscall"
)

// Options configures a new Set.
type Options struct {
	// Protected lists folders (the agent's data directory) that no operation
	// may reach, whatever the roots are. Each must exist, because a folder
	// that cannot be resolved cannot be guarded.
	Protected []string
}

// rootEntry is one allowed folder: the os.Root opened on its resolved
// location and every spelling of its path a caller may use (as configured and
// with symlinks resolved).
type rootEntry struct {
	names []string
	root  *os.Root
}

// state is the part of a Set that views (ReadOnly) share with their parent.
type state struct {
	entries      []*rootEntry
	unrestricted bool
	protected    []string // resolved, absolute

	mu   sync.Mutex
	vols map[string]*os.Root // unrestricted only: roots opened per volume
}

// Set is the group of folders a caller may use. It is safe for concurrent
// use. Close releases the open roots.
type Set struct {
	*state
	readOnly bool
	owner    bool // Close releases the shared state only for its owner
}

// New builds a Set limited to roots. Each root must be an absolute path to an
// existing folder; symlinks in it are resolved once here, so a root that is a
// symlink, or lives below one, works. With no roots the Set denies every
// path; unrestricted access must be asked for with Unrestricted.
func New(roots []string, opts Options) (*Set, error) {
	st, err := newState(opts)
	if err != nil {
		return nil, err
	}
	for _, r := range roots {
		clean, err := cleanAbs(r)
		if err != nil {
			st.closeRoots()
			return nil, err
		}
		real, err := filepath.EvalSymlinks(clean)
		if err != nil {
			st.closeRoots()
			return nil, fmt.Errorf("rootfs: resolve root: %w", err)
		}
		if st.isProtectedPath(real) {
			st.closeRoots()
			return nil, fmt.Errorf("%w: root is inside a protected folder", ErrProtected)
		}
		root, err := os.OpenRoot(real)
		if err != nil {
			st.closeRoots()
			return nil, fmt.Errorf("rootfs: open root: %w", err)
		}
		st.entries = append(st.entries, &rootEntry{names: uniqueNames(clean, real), root: root})
	}
	return &Set{state: st, owner: true}, nil
}

// Unrestricted builds a Set that reaches every path of the machine except
// protected folders. It exists so that "no roots configured" is a deliberate
// choice by the caller and never the result of an empty list.
func Unrestricted(opts Options) (*Set, error) {
	st, err := newState(opts)
	if err != nil {
		return nil, err
	}
	st.unrestricted = true
	st.vols = make(map[string]*os.Root)
	return &Set{state: st, owner: true}, nil
}

// newState resolves the protected folders into a fresh state.
func newState(opts Options) (*state, error) {
	st := &state{}
	for _, p := range opts.Protected {
		clean, err := cleanAbs(p)
		if err != nil {
			return nil, err
		}
		real, err := filepath.EvalSymlinks(clean)
		if err != nil {
			return nil, fmt.Errorf("rootfs: resolve protected folder: %w", err)
		}
		st.protected = append(st.protected, real)
	}
	return st, nil
}

// uniqueNames returns the given spellings without duplicates.
func uniqueNames(names ...string) []string {
	out := make([]string, 0, len(names))
	for _, n := range names {
		dup := false
		for _, o := range out {
			dup = dup || samePath(o, n)
		}
		if !dup {
			out = append(out, n)
		}
	}
	return out
}

// closeRoots closes every os.Root the state holds.
func (st *state) closeRoots() {
	st.mu.Lock()
	defer st.mu.Unlock()
	for _, e := range st.entries {
		_ = e.root.Close()
	}
	st.entries = nil
	for k, r := range st.vols {
		_ = r.Close()
		delete(st.vols, k)
	}
}

// isProtectedPath reports whether the resolved path is at or under a
// protected folder.
func (st *state) isProtectedPath(real string) bool {
	for _, p := range st.protected {
		if isUnder(real, p) {
			return true
		}
	}
	return false
}

// Close releases the open roots. Views made by ReadOnly do nothing; closing
// the Set they came from invalidates them. Narrowed Sets are independent.
func (s *Set) Close() error {
	if s.owner {
		s.closeRoots()
	}
	return nil
}

// ReadOnly returns a view of the Set whose changing operations fail with
// ErrReadOnly. It shares the open roots, so it must not outlive the Set.
func (s *Set) ReadOnly() *Set {
	return &Set{state: s.state, readOnly: true}
}

// IsReadOnly reports whether changing operations on this Set fail with
// ErrReadOnly.
func (s *Set) IsReadOnly() bool {
	return s.readOnly
}

// Narrow returns a stricter Set limited to the folder at path, which must be
// inside this Set and must be a folder (a per-device jail). The new Set owns
// its own root and keeps this Set's protected folders and read-only flag; it
// can never reach anything this Set could not.
func (s *Set) Narrow(path string) (*Set, error) {
	h, err := s.Open(path)
	if err != nil {
		return nil, err
	}
	if err := h.check(access{follow: true}); err != nil {
		return nil, err
	}
	root, rel, err := h.at(true)
	if err != nil {
		return nil, err
	}
	info, err := root.Stat(rel)
	if err != nil {
		return nil, mapErr(err)
	}
	if !info.IsDir() {
		return nil, &fs.PathError{Op: "narrow", Path: h.full, Err: syscall.ENOTDIR}
	}
	sub, err := root.OpenRoot(rel)
	if err != nil {
		return nil, mapErr(err)
	}
	real := resolvePhysical(h.full, true)
	st := &state{
		entries:   []*rootEntry{{names: uniqueNames(h.full, real), root: sub}},
		protected: s.protected,
	}
	return &Set{state: st, readOnly: s.readOnly, owner: true}, nil
}

// Open prepares a handle for a path without touching the disk. The path must
// be absolute and inside the Set, or it fails with ErrOutsideRoots. All
// operations on the handle go through the root that contains the path.
func (s *Set) Open(path string) (*Handle, error) {
	clean, err := cleanAbs(path)
	if err != nil {
		return nil, err
	}
	root, rel, err := s.locate(clean)
	if err != nil {
		return nil, err
	}
	return &Handle{set: s, root: root, rel: rel, full: clean}, nil
}

// locate finds the root holding a clean absolute path and the path relative to
// it.
func (s *Set) locate(clean string) (*os.Root, string, error) {
	if s.unrestricted {
		base := filepath.VolumeName(clean) + pathSep
		root, err := s.volumeRoot(base)
		if err != nil {
			return nil, "", err
		}
		rel, _ := underRel(clean, base)
		return root, rel, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, e := range s.entries {
		for _, name := range e.names {
			if rel, ok := underRel(clean, name); ok {
				return e.root, rel, nil
			}
		}
	}
	return nil, "", fmt.Errorf("%w: %s", ErrOutsideRoots, clean)
}

// volumeRoot returns the os.Root of a volume root ("/" or "C:\"), opening it
// on first use.
func (s *Set) volumeRoot(base string) (*os.Root, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if r, ok := s.vols[base]; ok {
		return r, nil
	}
	r, err := os.OpenRoot(base)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, fmt.Errorf("%w: %s", ErrOutsideRoots, base)
		}
		return nil, err
	}
	s.vols[base] = r
	return r, nil
}
