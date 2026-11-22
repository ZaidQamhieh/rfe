package fsops

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// Requirement 23 (decided at the 2026-10-26 closure): an archive entry with an
// absolute name is refused, in POSIX, Windows drive and UNC forms, whatever
// the host OS. Safe relative names still extract; traversal is still refused;
// a refusal rolls back what the extraction created and leaves existing
// destination content alone.

func closureZip(t *testing.T, path string, names ...string) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	for _, n := range names {
		// CreateHeader keeps the name byte for byte (no separator rewriting).
		w, err := zw.CreateHeader(&zip.FileHeader{Name: n, Method: zip.Store})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte("data:" + n)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestClosureR23_CheckEntryNameNotAbsolute(t *testing.T) {
	refused := []string{
		"/etc/passwd", "/abs.txt", "//server/share/x",
		`\abs.txt`, `\\server\share\x.txt`, `\\?\C:\x.txt`,
		`C:\Windows\x.txt`, "C:/x.txt", "c:x.txt", "a:b.txt", `Z:\`,
	}
	for _, n := range refused {
		if err := checkEntryNameNotAbsolute(n); !errors.Is(err, ErrForbidden) {
			t.Errorf("checkEntryNameNotAbsolute(%q) = %v, want ErrForbidden", n, err)
		}
	}
	allowed := []string{"a.txt", "dir/a.txt", "./a.txt", "dir/", "dir/c:x.txt", "..x", "a/../b.txt", "../escape.txt"}
	for _, n := range allowed {
		if err := checkEntryNameNotAbsolute(n); err != nil {
			t.Errorf("checkEntryNameNotAbsolute(%q) = %v, want nil (containment is checked separately)", n, err)
		}
	}
}

func TestClosureR23_ExtractRefusesAbsoluteNames(t *testing.T) {
	outside := t.TempDir()
	posixAbs := filepath.ToSlash(filepath.Join(outside, "posix.txt"))
	for _, name := range []string{posixAbs, "/r23-abs.txt", `C:\r23\win.txt`, "C:/r23/win.txt", `\\srv\share\unc.txt`, "//srv/share/unc.txt", `\r23-root.txt`} {
		t.Run(name, func(t *testing.T) {
			ops, root := setupJail(t)
			archive := filepath.Join(root, "abs.zip")
			closureZip(t, archive, name)
			dest := filepath.Join(root, "dest")
			if _, err := ops.Extract(archive, dest); !errors.Is(err, ErrForbidden) {
				t.Fatalf("Extract(%q) err = %v, want ErrForbidden", name, err)
			}
			if _, err := os.Lstat(filepath.Join(outside, "posix.txt")); !os.IsNotExist(err) {
				t.Errorf("entry written at its literal absolute path (err=%v)", err)
			}
			// destDir did not exist before; the rollback removes it again.
			if _, err := os.Lstat(dest); !os.IsNotExist(err) {
				t.Errorf("destDir left behind after refusal: %v (tree %v)", err, listTree(t, dest))
			}
		})
	}
}

func TestClosureR23_TarGzAbsoluteNameRefused(t *testing.T) {
	ops, root := setupJail(t)
	archive := filepath.Join(root, "abs.tar.gz")
	f, err := os.Create(archive)
	if err != nil {
		t.Fatal(err)
	}
	gz := gzip.NewWriter(f)
	tw := tar.NewWriter(gz)
	for _, n := range []string{"ok.txt", "/r23-tar-abs.txt"} {
		if err := tw.WriteHeader(&tar.Header{Name: n, Mode: 0o644, Size: 2, Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte("hi")); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	f.Close()
	dest := filepath.Join(root, "dest")
	if _, err := ops.Extract(archive, dest); !errors.Is(err, ErrForbidden) {
		t.Fatalf("Extract err = %v, want ErrForbidden", err)
	}
	if _, err := os.Lstat(dest); !os.IsNotExist(err) {
		t.Errorf("destDir left behind: %v", err)
	}
}

// Valid entries before an absolute one are rolled back; content already in
// destDir, including a file the archive names too, is left byte for byte.
func TestClosureR23_MixedArchiveRollsBackAndKeepsExisting(t *testing.T) {
	ops, root := setupJail(t)
	dest := filepath.Join(root, "dest")
	if err := os.MkdirAll(dest, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dest, "a.txt"), []byte("mine"), 0o644); err != nil {
		t.Fatal(err)
	}
	archive := filepath.Join(root, "mixed.zip")
	closureZip(t, archive, "a.txt", "sub/b.txt", "c.txt", "/r23-mixed-abs.txt")
	if _, err := ops.Extract(archive, dest); !errors.Is(err, ErrForbidden) {
		t.Fatalf("Extract err = %v, want ErrForbidden", err)
	}
	if got, want := listTree(t, dest), []string{"a.txt"}; !reflect.DeepEqual(got, want) {
		t.Errorf("dest after refused extract = %v, want %v", got, want)
	}
	if b, _ := os.ReadFile(filepath.Join(dest, "a.txt")); string(b) != "mine" {
		t.Errorf("existing a.txt changed: %q", b)
	}
}

// Safe relative names still extract, collisions still get a "(n)" sibling,
// and relative traversal is still refused.
func TestClosureR23_RelativeNamesUnchanged(t *testing.T) {
	ops, root := setupJail(t)
	dest := filepath.Join(root, "dest")
	if err := os.MkdirAll(dest, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dest, "a.txt"), []byte("mine"), 0o644); err != nil {
		t.Fatal(err)
	}
	archive := filepath.Join(root, "ok.zip")
	closureZip(t, archive, "a.txt", "./dot.txt", "x/../inside.txt", "deep/er/f.txt")
	if _, err := ops.Extract(archive, dest); err != nil {
		t.Fatalf("Extract of safe relative names: %v", err)
	}
	for _, rel := range []string{"a (1).txt", "dot.txt", "inside.txt", filepath.Join("deep", "er", "f.txt")} {
		if _, err := os.Stat(filepath.Join(dest, rel)); err != nil {
			t.Errorf("missing %s: %v (tree %v)", rel, err, listTree(t, dest))
		}
	}
	if b, _ := os.ReadFile(filepath.Join(dest, "a.txt")); string(b) != "mine" {
		t.Errorf("existing a.txt changed: %q", b)
	}

	slip := filepath.Join(root, "slip.zip")
	closureZip(t, slip, "../r23-slip.txt")
	if _, err := ops.Extract(slip, dest); !errors.Is(err, ErrForbidden) {
		t.Fatalf("traversal err = %v, want ErrForbidden", err)
	}
	if _, err := os.Lstat(filepath.Join(root, "r23-slip.txt")); !os.IsNotExist(err) {
		t.Errorf("traversal entry written: %v", err)
	}
}
