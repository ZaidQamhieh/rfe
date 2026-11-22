//go:build unix

package rootfs

import (
	"errors"
	"path/filepath"
	"syscall"
	"testing"
)

// A named pipe must be refused, not waited on.
func TestOpenRefusesNamedPipe(t *testing.T) {
	tmp := base(t)
	root := mkdir(t, filepath.Join(tmp, "root"))
	fifo := filepath.Join(root, "pipe")
	if err := syscall.Mkfifo(fifo, 0o600); err != nil {
		t.Skipf("mkfifo unavailable: %v", err)
	}
	s := newSet(t, []string{root})
	if f, err := open(t, s, fifo).Open(); !errors.Is(err, ErrNotRegular) {
		closeIf(f)
		t.Fatalf("Open(fifo) = %v, want ErrNotRegular", err)
	}
	if _, err := open(t, s, root).ReadDir(); err != nil {
		t.Fatal(err)
	}
}

// A link inside the root is created with the given text.
func TestSymlinkInsideRoot(t *testing.T) {
	tmp := base(t)
	root := mkdir(t, filepath.Join(tmp, "root"))
	write(t, filepath.Join(root, "a.txt"), "x")
	s := newSet(t, []string{root})
	ln := filepath.Join(root, "ln")
	if err := open(t, s, ln).Symlink("a.txt"); err != nil {
		t.Fatalf("Symlink(a.txt) = %v", err)
	}
	got, err := open(t, s, ln).Readlink()
	if err != nil || got != "a.txt" {
		t.Fatalf("Readlink = %q, %v; want a.txt", got, err)
	}
}

// Targets that leave the Set are refused and no link is left behind. The
// last case reaches the outside through a link already in the root.
func TestSymlinkTargetOutsideRefused(t *testing.T) {
	tmp := base(t)
	root := mkdir(t, filepath.Join(tmp, "root"))
	outside := mkdir(t, filepath.Join(tmp, "outside"))
	link(t, outside, filepath.Join(root, "out"))
	s := newSet(t, []string{root})
	ln := filepath.Join(root, "ln")
	cases := []struct{ name, target string }{
		{"relative escape", "../outside"},
		{"absolute target", outside},
		{"through link out", "out/x"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := open(t, s, ln).Symlink(c.target)
			if !errors.Is(err, ErrOutsideRoots) {
				t.Fatalf("Symlink(%q) = %v, want ErrOutsideRoots", c.target, err)
			}
			if exists(ln) {
				t.Fatalf("link was created for refused target %q", c.target)
			}
		})
	}
}

// A link to a protected folder, or placed in one, is refused.
func TestSymlinkProtectedRefused(t *testing.T) {
	tmp := base(t)
	root := mkdir(t, filepath.Join(tmp, "root"))
	data := mkdir(t, filepath.Join(root, "data"))
	write(t, filepath.Join(data, "secret.txt"), "x")
	s := newSet(t, []string{root}, data)
	ln := filepath.Join(root, "ln")
	if err := open(t, s, ln).Symlink("data/secret.txt"); !errors.Is(err, ErrProtected) {
		t.Fatalf("Symlink into protected = %v, want ErrProtected", err)
	}
	if exists(ln) {
		t.Fatal("link to protected folder was created")
	}
	if err := open(t, s, filepath.Join(data, "ln")).Symlink("secret.txt"); !errors.Is(err, ErrProtected) {
		t.Fatalf("Symlink inside protected = %v, want ErrProtected", err)
	}
}

// A read-only Set refuses to create links and reports itself as read-only.
func TestSymlinkReadOnlyRefused(t *testing.T) {
	tmp := base(t)
	root := mkdir(t, filepath.Join(tmp, "root"))
	write(t, filepath.Join(root, "a.txt"), "x")
	s := newSet(t, []string{root})
	if s.IsReadOnly() {
		t.Fatal("writable Set reports IsReadOnly")
	}
	ro := s.ReadOnly()
	if !ro.IsReadOnly() {
		t.Fatal("ReadOnly view reports !IsReadOnly")
	}
	ln := filepath.Join(root, "ln")
	if err := open(t, ro, ln).Symlink("a.txt"); !errors.Is(err, ErrReadOnly) {
		t.Fatalf("Symlink on read-only = %v, want ErrReadOnly", err)
	}
	if exists(ln) {
		t.Fatal("link was created on a read-only Set")
	}
}
