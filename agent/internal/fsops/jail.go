package fsops

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
)

// This file holds the agent's path-jail and access-control model — the
// security boundary for every filesystem operation. Resolve performs logical
// path authorization; Ops methods then use operation-scoped os.Root handles
// (access/accessPair below) so filesystem use remains inside the jail even if
// names are swapped after authorization. The SettingsView wrappers (read-only
// / per-device jail) compose the live permission state.

// SettingsView supplies the live read-only flag and jail roots. fsops reads
// through it on every operation so changes apply without reconstructing Ops.
type SettingsView interface {
	IsReadOnly() bool
	Roots() []string
}

// staticSettings is an immutable SettingsView for callers (and tests) that
// pass fixed values via New.
type staticSettings struct {
	readOnly bool
	roots    []string
}

func (s staticSettings) IsReadOnly() bool { return s.readOnly }
func (s staticSettings) Roots() []string  { return s.roots }

// jailedSettings wraps a base SettingsView, overriding Roots() with a fixed
// set while delegating IsReadOnly() to the base (so live read-only toggles
// still apply to a jailed Ops).
type jailedSettings struct {
	base  SettingsView
	roots []string
}

func (s jailedSettings) IsReadOnly() bool { return s.base.IsReadOnly() }
func (s jailedSettings) Roots() []string  { return s.roots }

// roSettings forces IsReadOnly() to true while delegating Roots() to the base
// view, so a per-device read-only request reuses every existing fsops write
// guard (CreateFolder/WriteContent/Rename/Delete/…) without changing the live
// global read-only flag.
type roSettings struct{ base SettingsView }

func (s roSettings) IsReadOnly() bool { return true }
func (s roSettings) Roots() []string  { return s.base.Roots() }

// ReadOnly returns a view of o that rejects all write operations regardless of
// the live global read-only flag — used to enforce a per-device read-only flag
// (Device.ReadOnly, #8). Reads (ListDir, Meta, downloads) are unaffected. The
// returned Ops keeps o's roots and denyAll, so it composes after Jailed.
func (o *Ops) ReadOnly() *Ops {
	return &Ops{settings: roSettings{base: o.settings}, denyAll: o.denyAll}
}

// IsDenyAll reports whether this Ops has been explicitly configured to deny
// every path. This is distinct from Roots() returning an empty slice: an
// empty root list normally means an unjailed Ops that permits all paths.
// Callers that derive a browsable surface from Roots() must check this before
// treating an empty slice as unrestricted access or applying a fallback root.
func (o *Ops) IsDenyAll() bool { return o.denyAll }

// Jailed returns an Ops whose effective roots are the intersection of o's
// base roots and extraRoot (a per-device path jail, e.g. Device.JailRoot).
//
//   - If extraRoot is empty, o is returned unchanged — no per-device
//     restriction (today's behavior).
//   - If o has no configured roots (no global jail), the effective roots
//     become exactly []string{extraRoot}.
//   - If o has configured roots and extraRoot is within (or equal to) one of
//     them, the effective roots become exactly []string{extraRoot} — since
//     extraRoot is already a subset of that root, it IS the intersection.
//   - If extraRoot is outside every configured root, the returned Ops denies
//     ALL paths (a misconfigured/widening jailRoot must never grant access
//     beyond the global roots, so it is treated as "no access" rather than
//     silently falling back to the global roots).
//
// The returned Ops shares the read-only flag (live) with o but has its own
// fixed root set, so callers can safely use it for the lifetime of a single
// request.
func (o *Ops) Jailed(extraRoot string) *Ops {
	if extraRoot == "" {
		return o
	}
	clean := filepath.Clean(extraRoot)

	baseRoots := o.settings.Roots()
	if len(baseRoots) == 0 {
		// No global jail: the device's jailRoot becomes the sole root.
		return &Ops{settings: jailedSettings{base: o.settings, roots: []string{clean}}}
	}
	for _, root := range baseRoots {
		if isUnder(clean, root) {
			return &Ops{settings: jailedSettings{base: o.settings, roots: []string{clean}}}
		}
	}
	// extraRoot is outside every global root — deny everything rather than
	// widen access by falling back to the global roots.
	return &Ops{settings: jailedSettings{base: o.settings, roots: nil}, denyAll: true}
}

// Resolve cleans p and checks it against the jail, returning its resolved
// absolute path. The returned string is an authorization result, not a
// race-resistant filesystem handle: callers doing filesystem I/O must use an
// Ops method. A path can change between Resolve and a later os.* call.
// It also resolves symlinks to prevent symlink-escape attacks:
// if the resolved real path is outside every allowed root the request is
// rejected. When allowedRoots is empty any clean absolute path is accepted.
func (o *Ops) Resolve(p string) (string, error) {
	if o.denyAll {
		return "", fmt.Errorf("%w: %s", ErrForbidden, p)
	}
	if !filepath.IsAbs(p) {
		return "", fmt.Errorf("%w: path must be absolute", ErrForbidden)
	}
	if strings.IndexByte(p, 0) >= 0 {
		return "", fmt.Errorf("%w: path contains a NUL byte", ErrForbidden)
	}
	clean := filepath.Clean(p)

	roots := o.settings.Roots()
	// Authorize the lexical path before touching the disk: resolving a
	// path outside the roots would leak whether it exists (and, on Windows,
	// make the OS contact a UNC host with the user's credentials).
	if !lexicallyAllowed(clean, roots) {
		return "", fmt.Errorf("%w: %s", ErrForbidden, p)
	}

	real, err := resolveReal(clean)
	if err != nil {
		return "", err
	}

	if isProtected(real) {
		return "", fmt.Errorf("%w: %s", ErrForbidden, p)
	}

	if len(roots) == 0 {
		return real, nil
	}

	for _, root := range roots {
		if isUnder(real, root) {
			return real, nil
		}
	}
	return "", fmt.Errorf("%w: %s", ErrForbidden, p)
}

var (
	protectedMu   sync.RWMutex
	protectedDirs []string
)

// ProtectDir makes every path at or under dir unreachable through any Ops,
// whatever the roots are. The agent registers its own data folder (TLS key,
// database, in-progress uploads) so a home-wide or unrestricted root cannot
// expose it.
func ProtectDir(dir string) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return
	}
	if real, err := filepath.EvalSymlinks(abs); err == nil {
		abs = real
	}
	protectedMu.Lock()
	protectedDirs = append(protectedDirs, abs)
	protectedMu.Unlock()
}

func isProtected(real string) bool {
	protectedMu.RLock()
	defer protectedMu.RUnlock()
	for _, d := range protectedDirs {
		if isUnder(real, d) {
			return true
		}
	}
	return false
}

// lexicallyAllowed reports whether clean could be inside the roots without
// reading the path itself: it is under a root as written, or under a root's
// symlink-resolved form (the roots are the owner's configuration, so
// resolving them is safe). With no roots only UNC and device paths are
// refused.
func lexicallyAllowed(clean string, roots []string) bool {
	if len(roots) == 0 {
		return !isUNCPath(clean)
	}
	for _, root := range roots {
		if isUnder(clean, root) {
			return true
		}
		if realRoot, err := filepath.EvalSymlinks(root); err == nil && isUnder(clean, realRoot) {
			return true
		}
	}
	return false
}

// isUNCPath reports a Windows network or device path (\\host\share,
// \\?\, \\.\). It is always false on other platforms.
func isUNCPath(p string) bool {
	return runtime.GOOS == "windows" && strings.HasPrefix(filepath.VolumeName(p), `\\`)
}

// resolveReal returns the symlink-free form of a cleaned, absolute path.
//
// If the path exists it is simply EvalSymlinks'd. If it (or any part of it)
// doesn't exist yet — e.g. create/rename/upload destinations — symlinks are
// resolved on the deepest existing ancestor only, and the non-existent
// suffix is re-joined onto that resolved ancestor. This prevents a symlink
// placed inside the jail (e.g. jail/link -> /etc) from letting a
// not-yet-created path (jail/link/newfile) pass the jail check while the
// later os.MkdirAll/os.Create follows the symlink outside the jail.
func resolveReal(clean string) (string, error) {
	real, err := filepath.EvalSymlinks(clean)
	if err == nil {
		return real, nil
	}
	if !os.IsNotExist(err) {
		// Anything other than "doesn't exist yet" (e.g. ENOTDIR because a
		// path component is a regular file, or a permission error) is a
		// real problem the caller's filesystem op would hit anyway —
		// surface it instead of guessing at a fallback path.
		return "", err
	}

	// Walk up to the deepest existing ancestor.
	dir := clean
	var suffix []string
	for {
		parent := filepath.Dir(dir)
		if parent == dir {
			// Reached the filesystem root without finding an existing
			// ancestor; nothing to resolve against.
			return clean, nil
		}
		suffix = append([]string{filepath.Base(dir)}, suffix...)
		dir = parent
		if _, statErr := os.Lstat(dir); statErr == nil {
			break
		} else if !os.IsNotExist(statErr) {
			return "", statErr
		}
	}

	realDir, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return "", err
	}

	// Re-join the non-existent suffix onto the resolved ancestor and clean
	// the result so any ".." segments in the suffix are normalized before
	// the jail check.
	return filepath.Clean(filepath.Join(append([]string{realDir}, suffix...)...)), nil
}

// isUnder returns true if p is equal to or a descendant of root.
func isUnder(p, root string) bool {
	root = filepath.Clean(root)
	p = filepath.Clean(p)
	if p == root {
		return true
	}
	// A filesystem root ("/", `D:\`) already ends in a separator; adding
	// another would match nothing below it.
	prefix := root
	if !strings.HasSuffix(prefix, string(filepath.Separator)) {
		prefix += string(filepath.Separator)
	}
	return strings.HasPrefix(p, prefix)
}

// securePath is an operation-scoped view of a path. When an Ops instance is
// jailed, all filesystem access is relative to an os.Root handle opened on the
// jail directory. This keeps symlink and rename races inside that directory
// tree. The full path is retained only for API responses and MIME decisions.
// Unjailed Ops retain their historical path-based behavior because they have
// no jail boundary to escape.
type securePath struct {
	full string
	root *os.Root
	name string
}

func (p *securePath) close() {
	if p != nil && p.root != nil {
		_ = p.root.Close()
	}
}

func (p *securePath) child(name string) *securePath {
	if p == nil {
		return nil
	}
	return &securePath{
		full: filepath.Join(p.full, name),
		root: p.root,
		name: filepath.Join(p.name, name),
	}
}

func (p *securePath) parent() *securePath {
	if p == nil {
		return nil
	}
	return &securePath{
		full: filepath.Dir(p.full),
		root: p.root,
		name: filepath.Dir(p.name),
	}
}

// open opens the path read-only without ever blocking on it: O_NONBLOCK keeps a
// FIFO from waiting for a writer, and anything that is neither a regular file
// nor a directory (FIFO, socket, device) is refused with ErrNotRegular.
func (p *securePath) open() (*os.File, error) {
	var (
		f   *os.File
		err error
	)
	if p.root != nil {
		f, err = p.root.OpenFile(p.name, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	} else {
		f, err = os.OpenFile(p.full, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	}
	if err != nil {
		// A unix socket cannot be opened (ENXIO): it is not a regular file.
		if errors.Is(err, syscall.ENXIO) {
			return nil, ErrNotRegular
		}
		return nil, err
	}
	st, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return nil, err
	}
	if !st.Mode().IsRegular() && !st.IsDir() {
		_ = f.Close()
		return nil, ErrNotRegular
	}
	return f, nil
}

func (p *securePath) openFile(flag int, perm os.FileMode) (*os.File, error) {
	var f *os.File
	var err error
	if p.root != nil {
		f, err = p.root.OpenFile(p.name, flag, perm)
	} else {
		f, err = os.OpenFile(p.full, flag, perm)
	}
	if err != nil && flag&(os.O_CREATE|os.O_EXCL) == os.O_CREATE|os.O_EXCL && errors.Is(err, syscall.EISDIR) {
		// An exclusive create over a folder fails with EEXIST on Unix but
		// "is a directory" on Windows; both mean the name is taken.
		err = &fs.PathError{Op: "open", Path: p.full, Err: fs.ErrExist}
	}
	return f, err
}

func (p *securePath) stat() (os.FileInfo, error) {
	if p.root != nil {
		return p.root.Stat(p.name)
	}
	return os.Stat(p.full)
}

func (p *securePath) lstat() (os.FileInfo, error) {
	if p.root != nil {
		return p.root.Lstat(p.name)
	}
	return os.Lstat(p.full)
}

func (p *securePath) readlink() (string, error) {
	if p.root != nil {
		return p.root.Readlink(p.name)
	}
	return os.Readlink(p.full)
}

func (p *securePath) mkdirAll(perm os.FileMode) error {
	if p.root != nil {
		return p.root.MkdirAll(p.name, perm)
	}
	return os.MkdirAll(p.full, perm)
}

// mkdir creates exactly one directory and fails if the name is already taken.
func (p *securePath) mkdir(perm os.FileMode) error {
	if p.root != nil {
		return p.root.Mkdir(p.name, perm)
	}
	return os.Mkdir(p.full, perm)
}

func (p *securePath) remove() error {
	if p.root != nil {
		return p.root.Remove(p.name)
	}
	return os.Remove(p.full)
}

func (p *securePath) removeAll() error {
	if p.root != nil {
		return p.root.RemoveAll(p.name)
	}
	return os.RemoveAll(p.full)
}

func (p *securePath) renameTo(dst *securePath) error {
	if p.root != nil && p.root == dst.root {
		return p.root.Rename(p.name, dst.name)
	}
	if p.root == nil && dst.root == nil {
		return os.Rename(p.full, dst.full)
	}
	return fmt.Errorf("cannot atomically rename across filesystem jail roots")
}

func (p *securePath) linkTo(dst *securePath) error {
	if p.root != nil && p.root == dst.root {
		return p.root.Link(p.name, dst.name)
	}
	if p.root == nil && dst.root == nil {
		return os.Link(p.full, dst.full)
	}
	return fmt.Errorf("cannot hard-link across filesystem jail roots")
}

// createTemp creates an exclusive temporary file in the same directory as p
// and returns both its secure handle and the open file. The returned path
// shares p's root handle so cleanup and rename remain descriptor-relative.
func (p *securePath) createTemp(pattern string) (*securePath, *os.File, error) {
	if p.root == nil {
		f, err := os.CreateTemp(filepath.Dir(p.full), pattern)
		if err != nil {
			return nil, nil, err
		}
		return &securePath{full: f.Name()}, f, nil
	}
	dir := filepath.Dir(p.name)
	for range 20 {
		var b [16]byte
		if _, err := rand.Read(b[:]); err != nil {
			return nil, nil, err
		}
		base := strings.ReplaceAll(pattern, "*", hex.EncodeToString(b[:]))
		name := filepath.Join(dir, base)
		f, err := p.root.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if errors.Is(err, fs.ErrExist) {
			continue
		}
		if err != nil {
			return nil, nil, err
		}
		return &securePath{full: filepath.Join(filepath.Dir(p.full), base), root: p.root, name: name}, f, nil
	}
	return nil, nil, fmt.Errorf("could not create a unique temporary file")
}

// access opens the configured jail root and produces a path relative to its
// handle. The root's identity is checked before and after OpenRoot so replacing
// the configured directory with a symlink or another directory during setup
// fails closed. Once opened, os.Root pins the directory even if its name is
// concurrently renamed.
func (o *Ops) access(path string) (*securePath, error) {
	return o.accessWith(o.Resolve, path)
}

// accessEntry is access for operations on the directory entry itself rather
// than what it points to (delete, trash, rename and move sources): a symlink
// names the link, so removing or renaming it never touches its target.
func (o *Ops) accessEntry(path string) (*securePath, error) {
	return o.accessWith(o.resolveEntry, path)
}

// resolveEntry is Resolve without following a symlink in the final path
// component. The parent directory is resolved and jail-checked as usual and
// the leaf name is kept as written, so a link inside a root resolves to the
// link itself, even when its target is outside the roots or missing.
func (o *Ops) resolveEntry(p string) (string, error) {
	if o.denyAll {
		return "", fmt.Errorf("%w: %s", ErrForbidden, p)
	}
	if !filepath.IsAbs(p) {
		return "", fmt.Errorf("%w: path must be absolute", ErrForbidden)
	}
	if strings.IndexByte(p, 0) >= 0 {
		return "", fmt.Errorf("%w: path contains a NUL byte", ErrForbidden)
	}
	clean := filepath.Clean(p)
	dir, base := filepath.Split(clean)
	if base == "" {
		return o.Resolve(clean)
	}
	parent, err := o.Resolve(filepath.Clean(dir))
	if err != nil {
		// Never fall back to resolving the leaf: that would follow it again.
		// A configured root (whose parent is outside the jail) is refused
		// here, which entry operations could not perform on it anyway.
		return "", err
	}
	full := filepath.Join(parent, base)
	if isProtected(full) {
		return "", fmt.Errorf("%w: %s", ErrForbidden, p)
	}
	return full, nil
}

func (o *Ops) accessWith(resolve func(string) (string, error), path string) (*securePath, error) {
	resolved, err := resolve(path)
	if err != nil {
		return nil, err
	}
	rootName := o.allowedRootFor(resolved)
	if rootName == "" {
		return &securePath{full: resolved}, nil
	}
	root, err := openJailRoot(rootName)
	if err != nil {
		return nil, fmt.Errorf("open jail root: %w", err)
	}
	name, err := filepath.Rel(rootName, resolved)
	if err != nil || name == ".." || strings.HasPrefix(name, ".."+string(filepath.Separator)) {
		root.Close()
		return nil, fmt.Errorf("%w: %s", ErrForbidden, path)
	}
	if name == "" {
		name = "."
	}
	return &securePath{full: resolved, root: root, name: name}, nil
}

func (o *Ops) allowedRootFor(resolved string) string {
	for _, root := range o.settings.Roots() {
		if isUnder(resolved, root) {
			return root
		}
	}
	return ""
}

func openJailRoot(name string) (*os.Root, error) {
	before, err := os.Lstat(name)
	if err != nil {
		return nil, err
	}
	// Pin the identity now: on Windows os.SameFile would otherwise load it
	// from the path at comparison time and miss a swap in between.
	if !os.SameFile(before, before) {
		return nil, fmt.Errorf("configured jail root changed while it was opened")
	}
	if !before.IsDir() || before.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("configured jail root is not a real directory")
	}
	root, err := os.OpenRoot(name)
	if err != nil {
		return nil, err
	}
	after, err := root.Stat(".")
	if err != nil {
		root.Close()
		return nil, err
	}
	if !os.SameFile(before, after) {
		root.Close()
		return nil, fmt.Errorf("configured jail root changed while it was opened")
	}
	return root, nil
}

// accessPair shares one root descriptor when both paths are authorized by the
// same configured jail. This is required for race-resistant Root.Rename and
// also anchors both sides of copy/move to one directory identity.
func (o *Ops) accessPair(src, dst string) (*securePath, *securePath, error) {
	return o.accessPairWith(o.Resolve, o.Resolve, src, dst)
}

func (o *Ops) accessPairWith(resolveSrc, resolveDst func(string) (string, error), src, dst string) (*securePath, *securePath, error) {
	resolvedSrc, err := resolveSrc(src)
	if err != nil {
		return nil, nil, err
	}
	resolvedDst, err := resolveDst(dst)
	if err != nil {
		return nil, nil, err
	}
	srcRoot := o.allowedRootFor(resolvedSrc)
	dstRoot := o.allowedRootFor(resolvedDst)
	if srcRoot == dstRoot {
		if srcRoot == "" {
			return &securePath{full: resolvedSrc}, &securePath{full: resolvedDst}, nil
		}
		root, err := openJailRoot(srcRoot)
		if err != nil {
			return nil, nil, fmt.Errorf("open jail root: %w", err)
		}
		srcName, err := filepath.Rel(srcRoot, resolvedSrc)
		if err != nil {
			root.Close()
			return nil, nil, err
		}
		dstName, err := filepath.Rel(srcRoot, resolvedDst)
		if err != nil {
			root.Close()
			return nil, nil, err
		}
		if srcName == "" {
			srcName = "."
		}
		if dstName == "" {
			dstName = "."
		}
		return &securePath{full: resolvedSrc, root: root, name: srcName}, &securePath{full: resolvedDst, root: root, name: dstName}, nil
	}
	s, err := o.accessWith(resolveSrc, resolvedSrc)
	if err != nil {
		return nil, nil, err
	}
	d, err := o.accessWith(resolveDst, resolvedDst)
	if err != nil {
		s.close()
		return nil, nil, err
	}
	return s, d, nil
}

// Open opens path for reading while keeping access inside the configured jail.
// Callers must close the returned file. Use this instead of Resolve followed
// by os.Open so a path or symlink swap cannot redirect the read.
func (o *Ops) Open(path string) (*os.File, error) {
	p, err := o.access(path)
	if err != nil {
		return nil, err
	}
	f, err := p.open()
	p.close()
	return f, err
}

// OpenFile opens path with the supplied flags relative to the configured jail
// when one is active. Callers must close the returned file. This protects the
// opened path, but a later rename by the caller must also be rooted; prefer an
// Ops method such as WriteContent for atomic replacements.
func (o *Ops) OpenFile(path string, flag int, perm os.FileMode) (*os.File, error) {
	p, err := o.access(path)
	if err != nil {
		return nil, err
	}
	f, err := p.openFile(flag, perm)
	p.close()
	return f, err
}

// OpenDir opens path as a new directory root. Every path used with the
// returned handle is confined beneath this directory, and callers must close
// it. This is intended for safe recursive walks of a browsed directory.
func (o *Ops) OpenDir(path string) (*os.Root, error) {
	p, err := o.access(path)
	if err != nil {
		return nil, err
	}
	defer p.close()
	if p.root != nil {
		return p.root.OpenRoot(p.name)
	}
	return os.OpenRoot(p.full)
}

// Stat returns metadata for path through the active jail root.
func (o *Ops) Stat(path string) (os.FileInfo, error) {
	p, err := o.access(path)
	if err != nil {
		return nil, err
	}
	defer p.close()
	return p.stat()
}

// Lstat returns metadata for path without following the final symlink.
func (o *Ops) Lstat(path string) (os.FileInfo, error) {
	p, err := o.access(path)
	if err != nil {
		return nil, err
	}
	defer p.close()
	return p.lstat()
}

// Chmod changes a file's mode relative to the open jail root, after access
// has canonicalized and authorized the target. It does not open the file, so
// an item the agent cannot read (mode 0000) can still be made readable again.
func (o *Ops) Chmod(path string, mode os.FileMode) error {
	p, err := o.access(path)
	if err != nil {
		return err
	}
	defer p.close()
	if p.root != nil {
		return p.root.Chmod(p.name, mode)
	}
	return os.Chmod(p.full, mode)
}
