//go:build linux || darwin

package fsops

// Pass-3 tests for the no-replace rename's fallback on a file system without an
// atomic primitive (forced here by making the primitive report EINVAL), and for
// a destination created between the existence check and the rename.
//
// The "created after the check" tests are deterministic: a hook runs exactly in
// that window. A real concurrent writer would only give timing-dependent
// confidence, and none is used.

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"golang.org/x/sys/unix"
)

// forceNoPrimitive makes the native no-replace rename report "unsupported", as
// NFS, some FUSE file systems and old kernels do.
func forceNoPrimitive(t *testing.T) {
	t.Helper()
	old := noReplacePrimitive
	noReplacePrimitive = func(int, string, int, string) error { return unix.EINVAL }
	t.Cleanup(func() { noReplacePrimitive = old })
}

func setSeams(t *testing.T) {
	t.Helper()
	ol, ou, or, oh := linkatFn, unlinkatFn, renameatFn, afterNoReplaceCheck
	t.Cleanup(func() { linkatFn, unlinkatFn, renameatFn, afterNoReplaceCheck = ol, ou, or, oh })
}

func pass3Pair(dir, src, dst string) (*securePath, *securePath) {
	return &securePath{full: filepath.Join(dir, src)}, &securePath{full: filepath.Join(dir, dst)}
}

func exists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

func TestPass3NoReplaceFallbackFileExistingDestination(t *testing.T) {
	forceNoPrimitive(t)
	dir := t.TempDir()
	pass3Write(t, filepath.Join(dir, "src"), "payload")
	pass3Write(t, filepath.Join(dir, "dst"), "occupant")
	s, d := pass3Pair(dir, "src", "dst")
	if err := s.renameNoReplaceTo(d); !errors.Is(err, fs.ErrExist) {
		t.Fatalf("err = %v, want fs.ErrExist", err)
	}
	if got := pass3Read(t, filepath.Join(dir, "dst")); got != "occupant" {
		t.Errorf("dst replaced: %q", got)
	}
	if got := pass3Read(t, filepath.Join(dir, "src")); got != "payload" {
		t.Errorf("src = %q", got)
	}
}

// The same through renameNoReplace itself, which reports the bare errno.
func TestPass3NoReplaceFallbackBareEEXIST(t *testing.T) {
	forceNoPrimitive(t)
	dir := t.TempDir()
	pass3Write(t, filepath.Join(dir, "a"), "a")
	pass3Write(t, filepath.Join(dir, "b"), "b")
	d, err := os.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	fd := int(d.Fd())
	if err := renameNoReplace(fd, "a", fd, "b"); err != unix.EEXIST {
		t.Fatalf("err = %v, want EEXIST", err)
	}
	if got := pass3Read(t, filepath.Join(dir, "b")); got != "b" {
		t.Errorf("b = %q", got)
	}
}

func TestPass3NoReplaceFallbackFileFreeDestination(t *testing.T) {
	forceNoPrimitive(t)
	dir := t.TempDir()
	pass3Write(t, filepath.Join(dir, "src"), "payload")
	s, d := pass3Pair(dir, "src", "dst")
	if err := s.renameNoReplaceTo(d); err != nil {
		t.Fatal(err)
	}
	if got := pass3Read(t, filepath.Join(dir, "dst")); got != "payload" {
		t.Errorf("dst = %q", got)
	}
	if exists(filepath.Join(dir, "src")) {
		t.Error("src still exists")
	}
}

func TestPass3NoReplaceFallbackDirectory(t *testing.T) {
	forceNoPrimitive(t)
	dir := t.TempDir()
	pass3Write(t, filepath.Join(dir, "sdir", "f"), "F")
	pass3Write(t, filepath.Join(dir, "full", "g"), "G")
	pass3Write(t, filepath.Join(dir, "file"), "plain")
	if err := os.Mkdir(filepath.Join(dir, "empty"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, occupied := range []string{"full", "file", "empty"} {
		s, d := pass3Pair(dir, "sdir", occupied)
		if err := s.renameNoReplaceTo(d); !errors.Is(err, fs.ErrExist) {
			t.Errorf("onto %s: err = %v, want fs.ErrExist", occupied, err)
		}
	}
	if got := pass3Read(t, filepath.Join(dir, "full", "g")); got != "G" {
		t.Errorf("full/g = %q", got)
	}
	if got := pass3Read(t, filepath.Join(dir, "sdir", "f")); got != "F" {
		t.Errorf("sdir/f = %q", got)
	}
	s, d := pass3Pair(dir, "sdir", "moved")
	if err := s.renameNoReplaceTo(d); err != nil {
		t.Fatal(err)
	}
	if got := pass3Read(t, filepath.Join(dir, "moved", "f")); got != "F" {
		t.Errorf("moved/f = %q", got)
	}
	if exists(filepath.Join(dir, "sdir")) {
		t.Error("sdir still exists")
	}
}

func TestPass3NoReplaceFallbackLinkUnsupported(t *testing.T) {
	forceNoPrimitive(t)
	setSeams(t)
	for _, errno := range []syscall.Errno{unix.EPERM, unix.ENOTSUP, unix.ENOSYS, unix.EMLINK} {
		linkatFn = func(int, string, int, string, int) error { return errno }
		dir := t.TempDir()
		pass3Write(t, filepath.Join(dir, "src"), "payload")
		s, d := pass3Pair(dir, "src", "dst")
		err := s.renameNoReplaceTo(d)
		if err == nil || !errors.Is(err, errNoReplaceUnsupported) || !errors.Is(err, errors.ErrUnsupported) {
			t.Errorf("%v: err = %v, want errNoReplaceUnsupported", errno, err)
		}
		if errors.Is(err, fs.ErrExist) {
			t.Errorf("%v: unsupported must not look like fs.ErrExist: %v", errno, err)
		}
		if !errors.Is(err, errno) {
			t.Errorf("%v: cause lost: %v", errno, err)
		}
		if got := pass3Read(t, filepath.Join(dir, "src")); got != "payload" {
			t.Errorf("%v: src = %q", errno, got)
		}
		if exists(filepath.Join(dir, "dst")) {
			t.Errorf("%v: dst was created", errno)
		}
	}
}

// A failure of linkat that is not "unsupported" is passed through unchanged.
func TestPass3NoReplaceFallbackLinkOtherErrorPassesThrough(t *testing.T) {
	forceNoPrimitive(t)
	setSeams(t)
	linkatFn = func(int, string, int, string, int) error { return unix.EACCES }
	dir := t.TempDir()
	pass3Write(t, filepath.Join(dir, "src"), "payload")
	s, d := pass3Pair(dir, "src", "dst")
	err := s.renameNoReplaceTo(d)
	if !errors.Is(err, unix.EACCES) || errors.Is(err, errors.ErrUnsupported) {
		t.Fatalf("err = %v, want plain EACCES", err)
	}
}

func TestPass3NoReplaceFallbackUnlinkFailureKeepsSource(t *testing.T) {
	forceNoPrimitive(t)
	setSeams(t)
	unlinkatFn = func(fd int, name string, flags int) error {
		if name == "src" {
			return unix.EBUSY
		}
		return unix.Unlinkat(fd, name, flags)
	}
	dir := t.TempDir()
	pass3Write(t, filepath.Join(dir, "src"), "payload")
	s, d := pass3Pair(dir, "src", "dst")
	if err := s.renameNoReplaceTo(d); !errors.Is(err, unix.EBUSY) {
		t.Fatalf("err = %v, want EBUSY", err)
	}
	if got := pass3Read(t, filepath.Join(dir, "src")); got != "payload" {
		t.Errorf("src = %q", got)
	}
	if exists(filepath.Join(dir, "dst")) {
		t.Error("the new link was left behind")
	}
}

// A destination that appears after the existence check must survive, with the
// real primitive and with the forced fallback, for file and folder sources.
func TestPass3NoReplaceDestinationCreatedAfterCheck(t *testing.T) {
	for _, mode := range []string{"native", "fallback"} {
		for _, kind := range []string{"file", "dir"} {
			t.Run(mode+"/"+kind, func(t *testing.T) {
				if mode == "fallback" {
					forceNoPrimitive(t)
				}
				setSeams(t)
				dir := t.TempDir()
				if kind == "file" {
					pass3Write(t, filepath.Join(dir, "src"), "payload")
				} else {
					pass3Write(t, filepath.Join(dir, "src", "f"), "payload")
				}
				calls := 0
				afterNoReplaceCheck = func() {
					calls++
					if kind == "file" {
						pass3Write(t, filepath.Join(dir, "dst"), "occupant")
					} else {
						// An EMPTY folder is the one thing rename(2) may replace
						// (documented residual of the directory fallback), so the
						// racing entry has content.
						pass3Write(t, filepath.Join(dir, "dst", "g"), "occupant")
					}
				}
				s, d := pass3Pair(dir, "src", "dst")
				err := s.renameNoReplaceTo(d)
				if calls != 1 {
					t.Fatalf("hook ran %d times, want 1", calls)
				}
				if err == nil {
					t.Fatal("rename succeeded over an entry created after the check")
				}
				if kind == "file" {
					if !errors.Is(err, fs.ErrExist) {
						t.Errorf("err = %v, want fs.ErrExist", err)
					}
					if got := pass3Read(t, filepath.Join(dir, "dst")); got != "occupant" {
						t.Errorf("dst replaced: %q", got)
					}
					if got := pass3Read(t, filepath.Join(dir, "src")); got != "payload" {
						t.Errorf("src = %q", got)
					}
				} else {
					if got := pass3Read(t, filepath.Join(dir, "dst", "g")); got != "occupant" {
						t.Errorf("dst/g = %q", got)
					}
					if got := pass3Read(t, filepath.Join(dir, "src", "f")); got != "payload" {
						t.Errorf("src/f = %q", got)
					}
				}
			})
		}
	}
}

func TestPass3NoReplaceFallbackSymlinkSourceKeepsTheLink(t *testing.T) {
	forceNoPrimitive(t)
	dir := t.TempDir()
	pass3Write(t, filepath.Join(dir, "target"), "T")
	if err := os.Symlink("target", filepath.Join(dir, "ln")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("missing-target", filepath.Join(dir, "dangling")); err != nil {
		t.Fatal(err)
	}
	for _, c := range [][3]string{{"ln", "ln2", "target"}, {"dangling", "dangling2", "missing-target"}} {
		s, d := pass3Pair(dir, c[0], c[1])
		if err := s.renameNoReplaceTo(d); err != nil {
			t.Fatalf("%s: %v", c[0], err)
		}
		info, err := os.Lstat(filepath.Join(dir, c[1]))
		if err != nil || info.Mode()&os.ModeSymlink == 0 {
			t.Fatalf("%s: new name is not a symlink: %v, %v", c[1], info, err)
		}
		if got, _ := os.Readlink(filepath.Join(dir, c[1])); got != c[2] {
			t.Errorf("%s: readlink = %q, want %q", c[1], got, c[2])
		}
		if exists(filepath.Join(dir, c[0])) {
			t.Errorf("%s still exists", c[0])
		}
	}
	if got := pass3Read(t, filepath.Join(dir, "target")); got != "T" {
		t.Errorf("target = %q", got)
	}
}

func TestPass3NoReplaceFallbackHardLinkSourceLeavesOtherLink(t *testing.T) {
	forceNoPrimitive(t)
	dir := t.TempDir()
	pass3Write(t, filepath.Join(dir, "a"), "shared")
	if err := os.Link(filepath.Join(dir, "a"), filepath.Join(dir, "a2")); err != nil {
		t.Fatal(err)
	}
	s, d := pass3Pair(dir, "a", "b")
	if err := s.renameNoReplaceTo(d); err != nil {
		t.Fatal(err)
	}
	if exists(filepath.Join(dir, "a")) {
		t.Error("a still exists")
	}
	if got := pass3Read(t, filepath.Join(dir, "a2")); got != "shared" {
		t.Errorf("a2 = %q", got)
	}
	bi, err1 := os.Stat(filepath.Join(dir, "b"))
	ai, err2 := os.Stat(filepath.Join(dir, "a2"))
	if err1 != nil || err2 != nil || !os.SameFile(bi, ai) {
		t.Errorf("b and a2 are not the same file: %v %v", err1, err2)
	}
	if st, ok := ai.Sys().(*syscall.Stat_t); !ok || st.Nlink != 2 {
		t.Errorf("link count = %v, want 2", ai.Sys())
	}
}

// End to end through Ops.Rename: a file at the destination is a CONFLICT with
// the fallback too, and a file system that cannot hard-link surfaces the
// explicit unsupported error, not CONFLICT and not a replacement.
func TestPass3OpsRenameFallback(t *testing.T) {
	forceNoPrimitive(t)
	setSeams(t)
	ops, root := setupJail(t)
	pass3Write(t, filepath.Join(root, "src.txt"), "payload")
	pass3Write(t, filepath.Join(root, "dst.txt"), "occupant")
	if _, err := ops.Rename(filepath.Join(root, "src.txt"), filepath.Join(root, "dst.txt")); !errors.Is(err, ErrConflict) {
		t.Errorf("existing destination: err = %v, want ErrConflict", err)
	}
	if got := pass3Read(t, filepath.Join(root, "dst.txt")); got != "occupant" {
		t.Errorf("dst.txt replaced: %q", got)
	}
	linkatFn = func(int, string, int, string, int) error { return unix.EPERM }
	_, err := ops.Rename(filepath.Join(root, "src.txt"), filepath.Join(root, "free.txt"))
	if !errors.Is(err, errors.ErrUnsupported) || errors.Is(err, ErrConflict) {
		t.Errorf("no hard links: err = %v, want errors.ErrUnsupported and not ErrConflict", err)
	}
	if got := pass3Read(t, filepath.Join(root, "src.txt")); got != "payload" {
		t.Errorf("src.txt = %q", got)
	}
	if exists(filepath.Join(root, "free.txt")) {
		t.Error("free.txt was created")
	}
}
