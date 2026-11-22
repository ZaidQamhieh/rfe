package rootfs

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

// base returns a fresh temp dir with symlinks resolved, so expected paths
// match what the package computes.
func base(t *testing.T) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return dir
}

func mkdir(t *testing.T, p string) string {
	t.Helper()
	if err := os.MkdirAll(p, 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

func write(t *testing.T, p, content string) string {
	t.Helper()
	mkdir(t, filepath.Dir(p))
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func link(t *testing.T, target, name string) string {
	t.Helper()
	mkdir(t, filepath.Dir(name))
	if err := os.Symlink(target, name); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	return name
}

func newSet(t *testing.T, roots []string, protected ...string) *Set {
	t.Helper()
	s, err := New(roots, Options{Protected: protected})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

// open is Set.Open that fails the test on error.
func open(t *testing.T, s *Set, p string) *Handle {
	t.Helper()
	h, err := s.Open(p)
	if err != nil {
		t.Fatalf("Open(%s): %v", p, err)
	}
	return h
}

func exists(p string) bool {
	_, err := os.Lstat(p)
	return err == nil
}

func readFile(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// ops are the handle operations the table tests drive, each reporting only
// the error.
var ops = map[string]func(h, other *Handle) error{
	"Stat":  func(h, _ *Handle) error { _, err := h.Stat(); return err },
	"Lstat": func(h, _ *Handle) error { _, err := h.Lstat(); return err },
	"Open":  func(h, _ *Handle) error { f, err := h.Open(); closeIf(f); return err },
	"Create": func(h, _ *Handle) error {
		f, err := h.OpenFile(os.O_WRONLY|os.O_CREATE, FileMode)
		closeIf(f)
		return err
	},
	"Mkdir": func(h, _ *Handle) error { return h.Mkdir(DirMode) },
	"MkdirAll": func(h, _ *Handle) error {
		return h.MkdirAll(DirMode)
	},
	"Remove":    func(h, _ *Handle) error { return h.Remove() },
	"RemoveAll": func(h, _ *Handle) error { return h.RemoveAll() },
	"Rename":    func(h, o *Handle) error { return h.Rename(o) },
	"RenameNoReplace": func(h, o *Handle) error {
		return h.RenameNoReplace(o)
	},
	"Readlink": func(h, _ *Handle) error { _, err := h.Readlink(); return err },
	"ReadDir":  func(h, _ *Handle) error { _, err := h.ReadDir(); return err },
}

func closeIf(f *os.File) {
	if f != nil {
		_ = f.Close()
	}
}

func TestSymlinkEscape(t *testing.T) {
	tmp := base(t)
	root := mkdir(t, filepath.Join(tmp, "root"))
	outside := mkdir(t, filepath.Join(tmp, "outside"))
	secret := write(t, filepath.Join(outside, "secret.txt"), "secret")
	write(t, filepath.Join(root, "inside.txt"), "inside")
	fileLink := link(t, secret, filepath.Join(root, "filelink"))
	dirLink := link(t, outside, filepath.Join(root, "dirlink"))
	relLink := link(t, filepath.Join("..", "..", "outside"), filepath.Join(root, "sub", "rellink"))
	absInside := link(t, filepath.Join(root, "inside.txt"), filepath.Join(root, "absinside"))
	s := newSet(t, []string{root})

	cases := []struct {
		name string
		path string
		op   string
	}{
		{"file link stat", fileLink, "Stat"},
		{"file link open", fileLink, "Open"},
		{"file link write", fileLink, "Create"},
		{"dir link stat", dirLink, "Stat"},
		{"dir link list", dirLink, "ReadDir"},
		{"through dir link read", filepath.Join(dirLink, "secret.txt"), "Open"},
		{"through dir link create", filepath.Join(dirLink, "new.txt"), "Create"},
		{"through dir link mkdir", filepath.Join(dirLink, "newdir"), "Mkdir"},
		{"through dir link mkdirall", filepath.Join(dirLink, "a", "b"), "MkdirAll"},
		{"through dir link remove", filepath.Join(dirLink, "secret.txt"), "Remove"},
		{"through dir link removeall", filepath.Join(dirLink, "secret.txt"), "RemoveAll"},
		{"through relative link read", filepath.Join(relLink, "secret.txt"), "Open"},
		{"through relative link create", filepath.Join(relLink, "new.txt"), "Create"},
		// os.Root treats every absolute symlink as an escape, even one that
		// points back inside the root. Pinned so a change is noticed.
		{"absolute link back inside is refused", absInside, "Open"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := ops[c.op](open(t, s, c.path), nil)
			if !errors.Is(err, ErrOutsideRoots) {
				t.Fatalf("%s(%s) = %v, want ErrOutsideRoots", c.op, c.path, err)
			}
		})
	}
	for _, name := range []string{"new.txt", "newdir", "a"} {
		if exists(filepath.Join(outside, name)) {
			t.Errorf("%s was created outside the root", name)
		}
	}
	if got := readFile(t, secret); got != "secret" {
		t.Errorf("outside file changed: %q", got)
	}

	t.Run("rename through link", func(t *testing.T) {
		src := open(t, s, filepath.Join(root, "inside.txt"))
		dst := open(t, s, filepath.Join(dirLink, "moved.txt"))
		for _, op := range []string{"Rename", "RenameNoReplace"} {
			if err := ops[op](src, dst); !errors.Is(err, ErrOutsideRoots) {
				t.Errorf("%s = %v, want ErrOutsideRoots", op, err)
			}
		}
		if exists(filepath.Join(outside, "moved.txt")) || !exists(filepath.Join(root, "inside.txt")) {
			t.Error("rename crossed the root")
		}
	})

	t.Run("link entries themselves are reachable", func(t *testing.T) {
		h := open(t, s, dirLink)
		info, err := h.Lstat()
		if err != nil || info.Mode()&fs.ModeSymlink == 0 {
			t.Fatalf("Lstat = %v, %v", info, err)
		}
		if got, err := h.Readlink(); err != nil || got != outside {
			t.Fatalf("Readlink = %q, %v", got, err)
		}
		if err := h.Remove(); err != nil {
			t.Fatalf("removing the link itself: %v", err)
		}
		if !exists(secret) {
			t.Fatal("removing the link removed the target")
		}
	})
}

func TestTraversalAndInvalidPaths(t *testing.T) {
	tmp := base(t)
	root := mkdir(t, filepath.Join(tmp, "root"))
	mkdir(t, filepath.Join(tmp, "root2"))
	write(t, filepath.Join(tmp, "outside.txt"), "x")
	write(t, filepath.Join(root, "b.txt"), "b")
	mkdir(t, filepath.Join(root, "a"))
	s := newSet(t, []string{root})

	outsideCases := map[string]string{
		"dotdot out":      root + "/../outside.txt",
		"nested dotdot":   root + "/a/../../outside.txt",
		"sibling prefix":  filepath.Join(tmp, "root2"),
		"parent":          tmp,
		"filesystem root": string(filepath.Separator),
	}
	for name, p := range outsideCases {
		t.Run("outside/"+name, func(t *testing.T) {
			if _, err := s.Open(p); !errors.Is(err, ErrOutsideRoots) {
				t.Fatalf("Open(%q) = %v, want ErrOutsideRoots", p, err)
			}
		})
	}
	invalidCases := map[string]string{
		"relative": "a/b.txt",
		"empty":    "",
		"nul":      root + "/b.txt\x00.png",
	}
	for name, p := range invalidCases {
		t.Run("invalid/"+name, func(t *testing.T) {
			if _, err := s.Open(p); !errors.Is(err, ErrInvalidPath) {
				t.Fatalf("Open(%q) = %v, want ErrInvalidPath", p, err)
			}
		})
	}
	t.Run("dotdot inside root works", func(t *testing.T) {
		if _, err := open(t, s, root+"/a/../b.txt").Stat(); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("root itself", func(t *testing.T) {
		if _, err := open(t, s, root).Stat(); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("path through a regular file is a plain error", func(t *testing.T) {
		_, err := open(t, s, root+"/b.txt/x").Stat()
		if !errors.Is(err, syscall.ENOTDIR) || errors.Is(err, ErrOutsideRoots) || errors.Is(err, ErrProtected) {
			t.Fatalf("Stat = %v, want a plain ENOTDIR", err)
		}
	})
}

func TestUNCVolume(t *testing.T) {
	cases := map[string]bool{
		`\\host\share`: true,
		`\\?\C:`:       true,
		`\\.\pipe`:     true,
		`C:`:           false,
		``:             false,
	}
	for vol, want := range cases {
		if got := isUNCVolume(vol); got != want {
			t.Errorf("isUNCVolume(%q) = %v, want %v", vol, got, want)
		}
	}
}

func TestRootWithSymlinkInPath(t *testing.T) {
	tmp := base(t)
	real := mkdir(t, filepath.Join(tmp, "real", "data"))
	write(t, filepath.Join(real, "f.txt"), "f")
	linkParent := link(t, filepath.Join(tmp, "real"), filepath.Join(tmp, "linkparent"))
	linkRoot := link(t, real, filepath.Join(tmp, "linkroot"))

	for name, root := range map[string]string{
		"parent is a link": filepath.Join(linkParent, "data"),
		"root is a link":   linkRoot,
	} {
		t.Run(name, func(t *testing.T) {
			s := newSet(t, []string{root})
			for _, p := range []string{filepath.Join(root, "f.txt"), filepath.Join(real, "f.txt")} {
				if got := mustRead(t, open(t, s, p)); got != "f" {
					t.Errorf("%s read %q", p, got)
				}
			}
			h := open(t, s, filepath.Join(root, "new.txt"))
			f, err := h.OpenFile(os.O_WRONLY|os.O_CREATE|os.O_EXCL, FileMode)
			if err != nil {
				t.Fatal(err)
			}
			_ = f.Close()
			if !exists(filepath.Join(real, "new.txt")) {
				t.Error("file not created at the resolved location")
			}
			_ = os.Remove(filepath.Join(real, "new.txt"))
		})
	}
}

func mustRead(t *testing.T, h *Handle) string {
	t.Helper()
	f, err := h.Open()
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	buf := make([]byte, 64)
	n, _ := f.Read(buf)
	return string(buf[:n])
}

func TestNewRejectsBadRoots(t *testing.T) {
	tmp := base(t)
	if _, err := New([]string{filepath.Join(tmp, "missing")}, Options{}); err == nil {
		t.Error("missing root accepted")
	}
	if _, err := New([]string{"relative"}, Options{}); !errors.Is(err, ErrInvalidPath) {
		t.Errorf("relative root: %v", err)
	}
	if _, err := New([]string{tmp}, Options{Protected: []string{filepath.Join(tmp, "missing")}}); err == nil {
		t.Error("unresolvable protected folder accepted")
	}
	data := mkdir(t, filepath.Join(tmp, "data"))
	in := mkdir(t, filepath.Join(data, "sub"))
	if _, err := New([]string{in}, Options{Protected: []string{data}}); !errors.Is(err, ErrProtected) {
		t.Errorf("root inside protected: %v, want ErrProtected", err)
	}
}

func TestNoRootsDeniesEverything(t *testing.T) {
	tmp := base(t)
	s := newSet(t, nil)
	if _, err := s.Open(tmp); !errors.Is(err, ErrOutsideRoots) {
		t.Fatalf("Open = %v, want ErrOutsideRoots", err)
	}
}

func TestProtected(t *testing.T) {
	tmp := base(t)
	home := mkdir(t, filepath.Join(tmp, "home"))
	data := mkdir(t, filepath.Join(home, ".rfe"))
	key := write(t, filepath.Join(data, "key.pem"), "KEY")
	write(t, filepath.Join(home, "doc.txt"), "doc")
	deepData := mkdir(t, filepath.Join(home, "x", "y", "data"))
	write(t, filepath.Join(deepData, "db"), "DB")
	alias := link(t, data, filepath.Join(home, "alias"))
	dangling := link(t, filepath.Join(data, "planted.txt"), filepath.Join(home, "dangling"))
	relAlias := link(t, filepath.Join("..", ".rfe"), filepath.Join(home, "sub", "relalias"))
	s := newSet(t, []string{home}, data, deepData)

	t.Run("target at or under protected, every operation", func(t *testing.T) {
		targets := []string{data, key, filepath.Join(data, "new"), filepath.Join(data, "a", "b"),
			filepath.Join(alias, "key.pem"), alias + "/new", dangling, filepath.Join(relAlias, "key.pem")}
		other := open(t, s, filepath.Join(home, "other"))
		for _, p := range targets {
			for name, op := range ops {
				switch name {
				case "Lstat", "Readlink", "Mkdir", "Remove", "RemoveAll", "Rename", "RenameNoReplace":
					// Operate on the entry itself; a link entry is not the
					// protected folder. Checked separately below.
					if p == alias || p == dangling {
						continue
					}
				}
				err := op(open(t, s, p), other)
				if !errors.Is(err, ErrProtected) {
					t.Errorf("%s(%s) = %v, want ErrProtected", name, p, err)
				}
			}
		}
		if exists(filepath.Join(data, "planted.txt")) || exists(filepath.Join(data, "new")) {
			t.Fatal("a file was created in the protected folder")
		}
		if readFile(t, key) != "KEY" {
			t.Fatal("protected file changed")
		}
	})

	t.Run("rename into protected", func(t *testing.T) {
		src := open(t, s, filepath.Join(home, "doc.txt"))
		for _, op := range []string{"Rename", "RenameNoReplace"} {
			if err := ops[op](src, open(t, s, filepath.Join(data, "doc.txt"))); !errors.Is(err, ErrProtected) {
				t.Errorf("%s into data dir = %v, want ErrProtected", op, err)
			}
		}
	})

	t.Run("ancestor of protected: tree operations refused", func(t *testing.T) {
		parent := filepath.Join(home, "x")
		if err := open(t, s, parent).RemoveAll(); !errors.Is(err, ErrProtected) {
			t.Errorf("RemoveAll(parent) = %v", err)
		}
		if err := open(t, s, parent).Rename(open(t, s, filepath.Join(home, "moved"))); !errors.Is(err, ErrProtected) {
			t.Errorf("Rename(parent) = %v", err)
		}
		if err := open(t, s, parent).RenameNoReplace(open(t, s, filepath.Join(home, "moved"))); !errors.Is(err, ErrProtected) {
			t.Errorf("RenameNoReplace(parent) = %v", err)
		}
		if err := open(t, s, home).CheckTree(); !errors.Is(err, ErrProtected) {
			t.Errorf("CheckTree(home) = %v, want ErrProtected", err)
		}
		if err := open(t, s, filepath.Join(home, "doc.txt")).CheckTree(); err != nil {
			t.Errorf("CheckTree(unrelated file) = %v", err)
		}
		if !exists(deepData) || exists(filepath.Join(home, "moved")) {
			t.Fatal("protected folder was removed or moved")
		}
	})

	t.Run("listing hides protected entries", func(t *testing.T) {
		entries, err := open(t, s, home).ReadDir()
		if err != nil {
			t.Fatal(err)
		}
		got := map[string]bool{}
		for _, e := range entries {
			got[e.Name()] = true
		}
		if got[".rfe"] || !got["doc.txt"] || !got["x"] {
			t.Fatalf("listing = %v", got)
		}
		inner, err := open(t, s, filepath.Join(home, "x", "y")).ReadDir()
		if err != nil || len(inner) != 0 {
			t.Fatalf("inner listing = %v, %v", inner, err)
		}
	})

	t.Run("link entries to protected can be removed", func(t *testing.T) {
		for _, l := range []string{alias, dangling} {
			if err := open(t, s, l).Remove(); err != nil {
				t.Errorf("Remove(%s) = %v", l, err)
			}
		}
		if !exists(key) {
			t.Fatal("removing the link removed protected data")
		}
	})

	t.Run("unrelated paths work", func(t *testing.T) {
		if got := mustRead(t, open(t, s, filepath.Join(home, "doc.txt"))); got != "doc" {
			t.Errorf("read %q", got)
		}
	})
}

func TestProtectedWhenRootIsItsParentOrUnrestricted(t *testing.T) {
	tmp := base(t)
	data := mkdir(t, filepath.Join(tmp, "rfe-data"))
	write(t, filepath.Join(data, "key.pem"), "KEY")
	write(t, filepath.Join(tmp, "doc.txt"), "doc")

	un, err := Unrestricted(Options{Protected: []string{data}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = un.Close() })
	for name, s := range map[string]*Set{
		"root is parent": newSet(t, []string{tmp}, data),
		"unrestricted":   un,
	} {
		t.Run(name, func(t *testing.T) {
			if err := open(t, s, tmp).CheckTree(); !errors.Is(err, ErrProtected) {
				t.Errorf("CheckTree(parent) = %v, want ErrProtected", err)
			}
			if err := open(t, s, filepath.Join(data, "key.pem")).Remove(); !errors.Is(err, ErrProtected) {
				t.Errorf("Remove key = %v, want ErrProtected", err)
			}
			if err := open(t, s, tmp).RemoveAll(); err == nil {
				t.Error("RemoveAll(parent) succeeded")
			}
			if readFile(t, filepath.Join(data, "key.pem")) != "KEY" || !exists(filepath.Join(tmp, "doc.txt")) {
				t.Fatal("data changed")
			}
		})
	}
}

func TestNarrow(t *testing.T) {
	tmp := base(t)
	home := mkdir(t, filepath.Join(tmp, "home"))
	alice := mkdir(t, filepath.Join(home, "alice"))
	write(t, filepath.Join(alice, "a.txt"), "A")
	write(t, filepath.Join(home, "bob", "b.txt"), "B")
	write(t, filepath.Join(tmp, "outside.txt"), "O")
	data := mkdir(t, filepath.Join(home, "data"))
	toBob := link(t, filepath.Join(home, "bob"), filepath.Join(alice, "tobob"))
	toOutside := link(t, tmp, filepath.Join(home, "toout"))
	aliasAlice := link(t, "alice", filepath.Join(home, "alicelink"))
	s := newSet(t, []string{home}, data)

	jail, err := s.Narrow(alice)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = jail.Close() })

	if got := mustRead(t, open(t, jail, filepath.Join(alice, "a.txt"))); got != "A" {
		t.Errorf("read inside jail: %q", got)
	}
	for _, p := range []string{filepath.Join(home, "bob", "b.txt"), home, tmp, filepath.Join(tmp, "outside.txt")} {
		if _, err := jail.Open(p); !errors.Is(err, ErrOutsideRoots) {
			t.Errorf("jail.Open(%s) = %v, want ErrOutsideRoots", p, err)
		}
	}
	if _, err := open(t, jail, filepath.Join(toBob, "b.txt")).Stat(); !errors.Is(err, ErrOutsideRoots) {
		t.Errorf("link out of the jail: %v, want ErrOutsideRoots", err)
	}
	if _, err := jail.Narrow(home); !errors.Is(err, ErrOutsideRoots) {
		t.Errorf("widening: %v, want ErrOutsideRoots", err)
	}
	if _, err := jail.Narrow(filepath.Join(alice, "a.txt")); !errors.Is(err, syscall.ENOTDIR) {
		t.Errorf("narrow to a file: %v, want ENOTDIR", err)
	}
	if _, err := jail.Narrow(filepath.Join(alice, "nope")); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("narrow to a missing folder: %v, want not exist", err)
	}
	// The jail still sees the data dir as protected only if it could reach
	// it; narrowing to the protected folder itself is refused.
	if _, err := s.Narrow(data); !errors.Is(err, ErrProtected) {
		t.Errorf("Narrow(protected) = %v, want ErrProtected", err)
	}
	if _, err := s.Narrow(tmp); !errors.Is(err, ErrOutsideRoots) {
		t.Errorf("Narrow(outside) = %v, want ErrOutsideRoots", err)
	}
	if _, err := s.Narrow(toOutside); !errors.Is(err, ErrOutsideRoots) {
		t.Errorf("Narrow(link out) = %v, want ErrOutsideRoots", err)
	}

	t.Run("narrow through an in-root link resolves it", func(t *testing.T) {
		j, err := s.Narrow(aliasAlice)
		if err != nil {
			t.Fatal(err)
		}
		defer j.Close()
		for _, p := range []string{filepath.Join(aliasAlice, "a.txt"), filepath.Join(alice, "a.txt")} {
			if got := mustRead(t, open(t, j, p)); got != "A" {
				t.Errorf("read %s: %q", p, got)
			}
		}
	})

	t.Run("jail is writable and closing it leaves the parent usable", func(t *testing.T) {
		j, err := s.Narrow(alice)
		if err != nil {
			t.Fatal(err)
		}
		if err := open(t, j, filepath.Join(alice, "made")).Mkdir(DirMode); err != nil {
			t.Fatal(err)
		}
		_ = j.Close()
		if _, err := open(t, s, filepath.Join(alice, "made")).Stat(); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("jail keeps protected folders and read-only", func(t *testing.T) {
		parentJail, err := s.Narrow(home)
		if err != nil {
			t.Fatal(err)
		}
		defer parentJail.Close()
		if err := open(t, parentJail, home).CheckTree(); !errors.Is(err, ErrProtected) {
			t.Errorf("jail lost the protected folder: %v", err)
		}
		roJail, err := s.ReadOnly().Narrow(alice)
		if err != nil {
			t.Fatal(err)
		}
		defer roJail.Close()
		if err := open(t, roJail, filepath.Join(alice, "x")).Mkdir(DirMode); !errors.Is(err, ErrReadOnly) {
			t.Errorf("narrowed read-only Set wrote: %v", err)
		}
	})
}

func TestReadOnly(t *testing.T) {
	tmp := base(t)
	root := mkdir(t, filepath.Join(tmp, "root"))
	write(t, filepath.Join(root, "f.txt"), "f")
	mkdir(t, filepath.Join(root, "d"))
	s := newSet(t, []string{root})
	ro := s.ReadOnly()

	f := filepath.Join(root, "f.txt")
	changing := []struct {
		name string
		run  func() error
	}{
		{"Create", func() error { return ops["Create"](open(t, ro, filepath.Join(root, "new")), nil) }},
		{"OpenFile RDWR", func() error {
			g, err := open(t, ro, f).OpenFile(os.O_RDWR, 0)
			closeIf(g)
			return err
		}},
		{"OpenFile TRUNC", func() error {
			g, err := open(t, ro, f).OpenFile(os.O_RDONLY|os.O_TRUNC, 0)
			closeIf(g)
			return err
		}},
		{"Mkdir", func() error { return open(t, ro, filepath.Join(root, "n")).Mkdir(DirMode) }},
		{"MkdirAll", func() error { return open(t, ro, filepath.Join(root, "n", "m")).MkdirAll(DirMode) }},
		{"Remove", func() error { return open(t, ro, f).Remove() }},
		{"RemoveAll", func() error { return open(t, ro, filepath.Join(root, "d")).RemoveAll() }},
		{"Rename", func() error { return open(t, ro, f).Rename(open(t, ro, filepath.Join(root, "g"))) }},
		{"RenameNoReplace", func() error { return open(t, ro, f).RenameNoReplace(open(t, ro, filepath.Join(root, "g"))) }},
	}
	for _, c := range changing {
		t.Run(c.name, func(t *testing.T) {
			if err := c.run(); !errors.Is(err, ErrReadOnly) {
				t.Fatalf("got %v, want ErrReadOnly", err)
			}
		})
	}
	for _, n := range []string{"new", "n", "g"} {
		if exists(filepath.Join(root, n)) {
			t.Errorf("%s was created", n)
		}
	}
	if readFile(t, f) != "f" || !exists(filepath.Join(root, "d")) {
		t.Fatal("read-only Set changed the tree")
	}

	t.Run("reads work", func(t *testing.T) {
		if got := mustRead(t, open(t, ro, f)); got != "f" {
			t.Errorf("read %q", got)
		}
		if _, err := open(t, ro, root).ReadDir(); err != nil {
			t.Error(err)
		}
		g, err := open(t, ro, f).OpenFile(os.O_RDONLY, 0)
		if err != nil {
			t.Error(err)
		}
		closeIf(g)
	})
	t.Run("the original Set stays writable and closing the view is harmless", func(t *testing.T) {
		_ = ro.Close()
		if err := open(t, s, filepath.Join(root, "ok")).Mkdir(DirMode); err != nil {
			t.Fatal(err)
		}
	})
}

func TestRename(t *testing.T) {
	tmp := base(t)
	root := mkdir(t, filepath.Join(tmp, "root"))
	other := mkdir(t, filepath.Join(tmp, "other"))
	s := newSet(t, []string{root, other})

	for _, impl := range []string{"Handle", "generic"} {
		t.Run(impl+"/no replace keeps both files", func(t *testing.T) {
			src := write(t, filepath.Join(root, impl+"-src.txt"), "src")
			dst := write(t, filepath.Join(root, impl+"-dst.txt"), "dst")
			err := noReplace(t, s, impl, src, dst)
			if !errors.Is(err, fs.ErrExist) {
				t.Fatalf("got %v, want fs.ErrExist", err)
			}
			if readFile(t, src) != "src" || readFile(t, dst) != "dst" {
				t.Fatal("a file changed")
			}
		})
		t.Run(impl+"/no replace moves when free", func(t *testing.T) {
			src := write(t, filepath.Join(root, impl+"-a.txt"), "a")
			dst := filepath.Join(root, "sub-"+impl, "b.txt")
			mkdir(t, filepath.Dir(dst))
			if err := noReplace(t, s, impl, src, dst); err != nil {
				t.Fatal(err)
			}
			if exists(src) || readFile(t, dst) != "a" {
				t.Fatal("not moved")
			}
		})
		t.Run(impl+"/no replace moves a folder and refuses an existing folder", func(t *testing.T) {
			src := mkdir(t, filepath.Join(root, impl+"-dir"))
			write(t, filepath.Join(src, "in.txt"), "in")
			taken := mkdir(t, filepath.Join(root, impl+"-taken"))
			if err := noReplace(t, s, impl, src, taken); !errors.Is(err, fs.ErrExist) {
				t.Fatalf("onto folder: %v", err)
			}
			dst := filepath.Join(root, impl+"-dir-moved")
			if err := noReplace(t, s, impl, src, dst); err != nil {
				t.Fatal(err)
			}
			if readFile(t, filepath.Join(dst, "in.txt")) != "in" {
				t.Fatal("folder content lost")
			}
		})
		t.Run(impl+"/missing source", func(t *testing.T) {
			err := noReplace(t, s, impl, filepath.Join(root, "ghost-"+impl), filepath.Join(root, "ghost2-"+impl))
			if !errors.Is(err, fs.ErrNotExist) {
				t.Fatalf("got %v, want not exist", err)
			}
		})
	}

	t.Run("Rename replaces a file", func(t *testing.T) {
		src := write(t, filepath.Join(root, "r-src"), "new")
		dst := write(t, filepath.Join(root, "r-dst"), "old")
		if err := open(t, s, src).Rename(open(t, s, dst)); err != nil {
			t.Fatal(err)
		}
		if readFile(t, dst) != "new" || exists(src) {
			t.Fatal("not replaced")
		}
	})
	t.Run("across roots", func(t *testing.T) {
		src := write(t, filepath.Join(root, "x-src"), "x")
		dst := filepath.Join(other, "x-dst")
		for _, op := range []string{"Rename", "RenameNoReplace"} {
			if err := ops[op](open(t, s, src), open(t, s, dst)); !errors.Is(err, ErrCrossRoot) {
				t.Errorf("%s = %v, want ErrCrossRoot", op, err)
			}
		}
		if !exists(src) || exists(dst) {
			t.Fatal("tree changed")
		}
	})
	t.Run("an allowed folder itself", func(t *testing.T) {
		file := write(t, filepath.Join(root, "y"), "y")
		for _, c := range []struct{ a, b string }{{root, filepath.Join(root, "z")}, {file, root}} {
			if err := open(t, s, c.a).Rename(open(t, s, c.b)); !errors.Is(err, ErrRootTarget) {
				t.Errorf("Rename(%s -> %s) = %v, want ErrRootTarget", c.a, c.b, err)
			}
		}
		if err := open(t, s, root).Remove(); !errors.Is(err, ErrRootTarget) {
			t.Errorf("Remove(root) = %v", err)
		}
		if err := open(t, s, root).RemoveAll(); !errors.Is(err, ErrRootTarget) {
			t.Errorf("RemoveAll(root) = %v", err)
		}
	})
	t.Run("nil destination", func(t *testing.T) {
		if err := open(t, s, filepath.Join(root, "y")).Rename(nil); !errors.Is(err, ErrInvalidPath) {
			t.Errorf("got %v", err)
		}
	})
	t.Run("two Sets cannot rename into each other", func(t *testing.T) {
		jail, err := s.Narrow(root)
		if err != nil {
			t.Fatal(err)
		}
		defer jail.Close()
		src := write(t, filepath.Join(root, "j-src"), "j")
		if err := open(t, s, src).Rename(open(t, jail, filepath.Join(root, "j-dst"))); !errors.Is(err, ErrCrossRoot) {
			t.Errorf("got %v, want ErrCrossRoot", err)
		}
	})
}

// noReplace runs either the Handle method or the portable fallback.
func noReplace(t *testing.T, s *Set, impl, src, dst string) error {
	t.Helper()
	if impl == "Handle" {
		return open(t, s, src).RenameNoReplace(open(t, s, dst))
	}
	h := open(t, s, src)
	return renameNoReplaceGeneric(h.root, h.rel, open(t, s, dst).rel)
}

func TestUnrestricted(t *testing.T) {
	tmp := base(t)
	file := write(t, filepath.Join(tmp, "d", "f.txt"), "f")
	out := mkdir(t, filepath.Join(tmp, "out"))
	toOut := link(t, out, filepath.Join(tmp, "d", "lnk"))
	s, err := Unrestricted(Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })

	if got := mustRead(t, open(t, s, file)); got != "f" {
		t.Errorf("read %q", got)
	}
	if err := open(t, s, filepath.Join(tmp, "d", "new")).Mkdir(DirMode); err != nil {
		t.Fatal(err)
	}
	if _, err := open(t, s, string(filepath.Separator)).Stat(); err != nil {
		t.Errorf("Stat(/) = %v", err)
	}
	if _, err := open(t, s, filepath.Join(toOut, "nothing")).Stat(); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("through a link: %v, want not exist", err)
	}
	write(t, filepath.Join(out, "o.txt"), "o")
	if got := mustRead(t, open(t, s, filepath.Join(toOut, "o.txt"))); got != "o" {
		t.Errorf("read through an absolute link: %q", got)
	}
	if err := open(t, s, filepath.Join(toOut, "made")).Mkdir(DirMode); err != nil || !exists(filepath.Join(out, "made")) {
		t.Errorf("mkdir through an absolute link: %v", err)
	}
	if info, err := open(t, s, toOut).Lstat(); err != nil || info.Mode()&fs.ModeSymlink == 0 {
		t.Errorf("Lstat on the link: %v, %v", info, err)
	}
	if _, err := s.Open("relative/path"); !errors.Is(err, ErrInvalidPath) {
		t.Errorf("relative: %v", err)
	}
	if _, err := open(t, s, tmp).ReadDir(); err != nil {
		t.Error(err)
	}
	if err := open(t, s.ReadOnly(), filepath.Join(tmp, "x")).Mkdir(DirMode); !errors.Is(err, ErrReadOnly) {
		t.Errorf("read-only unrestricted: %v", err)
	}
	jail, err := s.Narrow(filepath.Join(tmp, "d"))
	if err != nil {
		t.Fatal(err)
	}
	defer jail.Close()
	if _, err := jail.Open(tmp); !errors.Is(err, ErrOutsideRoots) {
		t.Errorf("narrowed unrestricted Set reached outside: %v", err)
	}
}

// TestEscapeMessagePinned fails when a Go release changes the text os.Root
// uses for an escape, which mapErr depends on.
func TestEscapeMessagePinned(t *testing.T) {
	tmp := base(t)
	root := mkdir(t, filepath.Join(tmp, "root"))
	out := write(t, filepath.Join(tmp, "out.txt"), "o")
	link(t, out, filepath.Join(root, "l"))
	r, err := os.OpenRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	if _, err := r.Stat("l"); err == nil || !errors.Is(mapErr(err), ErrOutsideRoots) {
		t.Fatalf("os.Root escape error %v is not recognised", err)
	}
	if mapErr(nil) != nil {
		t.Fatal("mapErr(nil) != nil")
	}
	plain := &fs.PathError{Op: "open", Path: "x", Err: fs.ErrNotExist}
	if errors.Is(mapErr(plain), ErrOutsideRoots) || !errors.Is(mapErr(plain), fs.ErrNotExist) {
		t.Fatal("plain errors must pass through")
	}
}

func TestResolvePhysical(t *testing.T) {
	tmp := base(t)
	real := mkdir(t, filepath.Join(tmp, "real"))
	l1 := link(t, "real", filepath.Join(tmp, "l1"))
	l2 := link(t, l1, filepath.Join(tmp, "l2"))
	loop := link(t, "loop", filepath.Join(tmp, "loop"))
	cases := []struct {
		name, in string
		follow   bool
		want     string
	}{
		{"plain", real, true, real},
		{"chain", l2, true, real},
		{"missing tail", filepath.Join(l2, "a", "b"), true, filepath.Join(real, "a", "b")},
		{"not followed last", l1, false, l1},
		{"followed middle when not last", filepath.Join(l1, "x"), false, filepath.Join(real, "x")},
		{"loop does not hang", filepath.Join(loop, "x"), true, filepath.Join(loop, "x")},
	}
	for _, c := range cases {
		if got := resolvePhysical(c.in, c.follow); got != c.want {
			t.Errorf("%s: resolvePhysical(%s) = %s, want %s", c.name, c.in, got, c.want)
		}
	}
}
