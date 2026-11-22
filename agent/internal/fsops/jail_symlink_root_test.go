//go:build !windows

package fsops

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// A root configured as a symlink to a directory must allow the files that
// directory holds, while a symlink inside it that points elsewhere stays denied.
func TestResolve_SymlinkedRoot(t *testing.T) {
	base := t.TempDir()
	data := filepath.Join(base, "data", "x")
	outside := filepath.Join(base, "outside")
	for _, d := range []string{data, outside} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	link := filepath.Join(base, "link")
	if err := os.Symlink(data, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if err := os.WriteFile(filepath.Join(data, "a.txt"), []byte("in"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("out"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(data, "escape")); err != nil {
		t.Fatal(err)
	}
	ops := New([]string{link}, false)

	t.Run("file inside the symlinked root is allowed", func(t *testing.T) {
		got, err := ops.Resolve(filepath.Join(link, "a.txt"))
		if err != nil {
			t.Fatalf("Resolve: %v", err)
		}
		if want := filepath.Join(data, "a.txt"); got != want {
			t.Fatalf("Resolve = %q, want %q", got, want)
		}
	})

	t.Run("file inside the symlinked root can be opened", func(t *testing.T) {
		f, err := ops.Open(filepath.Join(link, "a.txt"))
		if err != nil {
			t.Fatalf("Open: %v", err)
		}
		f.Close()
	})

	t.Run("symlink inside the root pointing outside is denied", func(t *testing.T) {
		_, err := ops.Resolve(filepath.Join(link, "escape", "secret.txt"))
		if !errors.Is(err, ErrForbidden) {
			t.Fatalf("Resolve through escaping symlink = %v, want ErrForbidden", err)
		}
		if _, err := ops.Open(filepath.Join(link, "escape", "secret.txt")); !errors.Is(err, ErrForbidden) {
			t.Fatalf("Open through escaping symlink = %v, want ErrForbidden", err)
		}
	})

	t.Run("sibling of the real root directory is denied", func(t *testing.T) {
		// Only the root itself is accepted in its resolved form, not its parent.
		_, err := ops.Resolve(filepath.Join(outside, "secret.txt"))
		if !errors.Is(err, ErrForbidden) {
			t.Fatalf("Resolve outside = %v, want ErrForbidden", err)
		}
	})
}
