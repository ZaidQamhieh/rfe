package files

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/ZaidQamhieh/rfe/agent/internal/next/api"
	"github.com/ZaidQamhieh/rfe/agent/internal/next/authz"
	"github.com/ZaidQamhieh/rfe/agent/internal/next/rootfs"
)

// Well-known digests of the three bytes "abc" (FIPS 180 and RFC 1321 vectors).
const (
	abcSHA256 = "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad"
	abcSHA1   = "a9993e364706816aba3e25717850c26c9cd0d89d"
	abcMD5    = "900150983cd24fb0d6963f7d28e17f72"
)

// dataDirName is the protected folder some tests create inside the root.
const dataDirName = ".rfe-data"

// env is a test Service over one allowed folder (root) and, optionally, a
// second one (other) and a protected folder inside root.
type env struct {
	root, other string
	set         *rootfs.Set
	svc         *Service
}

// newEnv builds a Set over two fresh temp folders; protected makes
// root/.rfe-data (created with a secret file) a protected folder.
func newEnv(t *testing.T, protected bool) *env {
	t.Helper()
	e := &env{root: t.TempDir(), other: t.TempDir()}
	var opts rootfs.Options
	if protected {
		dir := filepath.Join(e.root, dataDirName)
		write(t, filepath.Join(dir, "secret.key"), "secret")
		opts.Protected = []string{dir}
	}
	set, err := rootfs.New([]string{e.root, e.other}, opts)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = set.Close() })
	e.set = set
	e.svc = New(set)
	return e
}

// readOnly returns a Service over a read-only view of the same folders.
func (e *env) readOnly() *Service { return New(e.set.ReadOnly()) }

func write(t *testing.T, p, content string) string {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func read(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func exists(p string) bool {
	_, err := os.Lstat(p)
	return err == nil
}

// names lists the entry names of a folder, sorted.
func names(t *testing.T, dir string) []string {
	t.Helper()
	des, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, d := range des {
		out = append(out, d.Name())
	}
	sort.Strings(out)
	return out
}

// noHidden fails when dir holds a leftover hidden working entry.
func noHidden(t *testing.T, dir string) {
	t.Helper()
	for _, n := range names(t, dir) {
		if strings.HasPrefix(n, tempPrefix) || strings.HasPrefix(n, asidePrefix) {
			t.Errorf("leftover working entry %q in %s", n, dir)
		}
	}
}

// wantCode fails unless err maps to the given API code.
func wantCode(t *testing.T, err error, code string) {
	t.Helper()
	if err == nil {
		t.Fatalf("want code %s, got no error", code)
	}
	if got := api.Map(err).Code; got != code {
		t.Fatalf("want code %s, got %s (%v)", code, got, err)
	}
}

// wantItem fails unless the batch item has the given code ("" means ok).
func wantItem(t *testing.T, r api.ItemResult, code string) {
	t.Helper()
	if code == "" {
		if !r.OK || r.Error != nil {
			t.Fatalf("want ok, got %+v", r.Error)
		}
		return
	}
	if r.OK || r.Error == nil || r.Error.Code != code {
		t.Fatalf("want code %s, got ok=%v error=%+v", code, r.OK, r.Error)
	}
}

// symlink makes a link or skips the test where links are not allowed.
func symlink(t *testing.T, target, name string) {
	t.Helper()
	if err := os.Symlink(target, name); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
}

func TestListPaging(t *testing.T) {
	e := newEnv(t, false)
	for _, n := range []string{"e.txt", "b.txt", "a.txt", "d.txt", "c.txt", ".hidden"} {
		write(t, filepath.Join(e.root, n), n)
	}
	want := []string{".hidden", "a.txt", "b.txt", "c.txt", "d.txt", "e.txt"} // dot files are listed like any other

	tests := []struct {
		name   string
		limit  int
		pages  int
		cursor string
	}{
		{"one page", 100, 1, ""},
		{"pages of two", 2, 3, ""},
		{"pages of one", 1, 6, ""},
		{"default limit", 0, 1, ""},
		{"negative limit uses default", -5, 1, ""},
		{"limit above max is capped", MaxListLimit * 10, 1, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var got []string
			cursor, pages := tc.cursor, 0
			for {
				l, err := e.svc.List(e.root, cursor, tc.limit)
				if err != nil {
					t.Fatal(err)
				}
				pages++
				for _, en := range l.Entries {
					got = append(got, en.Name)
				}
				if l.NextCursor == nil {
					break
				}
				if want := l.Entries[len(l.Entries)-1].Name; *l.NextCursor != want {
					t.Fatalf("cursor %q is not the last name %q", *l.NextCursor, want)
				}
				cursor = *l.NextCursor
			}
			if pages != tc.pages {
				t.Errorf("pages = %d, want %d", pages, tc.pages)
			}
			if strings.Join(got, ",") != strings.Join(want, ",") {
				t.Errorf("entries = %v, want %v", got, want)
			}
		})
	}

	t.Run("cursor skips names up to and including it", func(t *testing.T) {
		l, err := e.svc.List(e.root, "b.txt", 100)
		if err != nil {
			t.Fatal(err)
		}
		if len(l.Entries) != 3 || l.Entries[0].Name != "c.txt" {
			t.Errorf("entries after cursor = %+v", l.Entries)
		}
	})
	t.Run("cursor past the end gives an empty page", func(t *testing.T) {
		l, err := e.svc.List(e.root, "zzz", 100)
		if err != nil || len(l.Entries) != 0 || l.NextCursor != nil {
			t.Errorf("got %+v, %v", l, err)
		}
	})
	t.Run("empty folder lists as empty, not null", func(t *testing.T) {
		l, err := e.svc.List(e.other, "", 10)
		if err != nil || l.Entries == nil || len(l.Entries) != 0 {
			t.Errorf("got %+v, %v", l, err)
		}
	})
}

func TestListDetailsAndErrors(t *testing.T) {
	e := newEnv(t, true)
	write(t, filepath.Join(e.root, "dir", "x.txt"), "x")
	write(t, filepath.Join(e.root, "dir", "y.txt"), "y")
	write(t, filepath.Join(e.root, "note.txt"), "hello")
	write(t, filepath.Join(e.root, "blob"), "<html><body>hi</body></html>")

	l, err := e.svc.List(e.root, "", 0)
	if err != nil {
		t.Fatal(err)
	}
	by := map[string]Entry{}
	for _, en := range l.Entries {
		by[en.Name] = en
	}
	if _, ok := by[dataDirName]; ok {
		t.Error("the protected folder is listed")
	}
	if d := by["dir"]; !d.IsDir || d.ChildCount == nil || *d.ChildCount != 2 || d.MimeType != "" {
		t.Errorf("dir entry = %+v", d)
	}
	if n := by["note.txt"]; n.IsDir || n.Size != 5 || !strings.HasPrefix(n.MimeType, "text/plain") || n.Path != filepath.Join(e.root, "note.txt") {
		t.Errorf("file entry = %+v", n)
	}
	if b := by["blob"]; !strings.HasPrefix(b.MimeType, "text/html") {
		t.Errorf("sniffed type = %q", b.MimeType)
	}

	t.Run("errors", func(t *testing.T) {
		_, err := e.svc.List(filepath.Join(e.root, "nope"), "", 0)
		wantCode(t, err, api.CodePathNotFound)
		_, err = e.svc.List(filepath.Join(e.root, "note.txt"), "", 0)
		wantCode(t, err, api.CodeBadRequest) // a file is not a folder
		_, err = e.svc.List(filepath.Join(e.root, "note.txt", "x"), "", 0)
		wantCode(t, err, api.CodePathNotFound) // through a file: 404, not 500
		_, err = e.svc.List(t.TempDir(), "", 0)
		wantCode(t, err, authz.CodeForbidden)
		_, err = e.svc.List("relative/path", "", 0)
		wantCode(t, err, api.CodeBadRequest)
		_, err = e.svc.List(filepath.Join(e.root, dataDirName), "", 0)
		wantCode(t, err, authz.CodeForbidden)
	})
}

func TestListSymlinks(t *testing.T) {
	e := newEnv(t, false)
	write(t, filepath.Join(e.root, "real", "f.txt"), "f")
	symlink(t, "real", filepath.Join(e.root, "dirlink"))
	symlink(t, filepath.Join(e.root, "missing"), filepath.Join(e.root, "broken"))
	symlink(t, e.other, filepath.Join(e.root, "escape"))

	l, err := e.svc.List(e.root, "", 0)
	if err != nil {
		t.Fatal(err)
	}
	by := map[string]Entry{}
	for _, en := range l.Entries {
		by[en.Name] = en
	}
	if d := by["dirlink"]; !d.IsSymlink || !d.IsDir || d.SymlinkTarget == "" || d.ChildCount != nil {
		t.Errorf("link to a folder = %+v", d)
	}
	if b := by["broken"]; !b.IsSymlink || b.IsDir {
		t.Errorf("broken link = %+v", b)
	}
	// Listing through the link works while it stays in the roots; the link to
	// the second root is allowed too (both are roots), but not one to outside.
	if _, err := e.svc.List(filepath.Join(e.root, "dirlink"), "", 0); err != nil {
		t.Errorf("list through an inside link: %v", err)
	}
	outside := t.TempDir()
	symlink(t, outside, filepath.Join(e.root, "out"))
	_, err = e.svc.List(filepath.Join(e.root, "out"), "", 0)
	wantCode(t, err, authz.CodeForbidden)
}

func TestMeta(t *testing.T) {
	e := newEnv(t, true)
	write(t, filepath.Join(e.root, "a.txt"), "abc")
	symlink(t, filepath.Join(e.root, "a.txt"), filepath.Join(e.root, "lnk"))

	m, err := e.svc.Meta(filepath.Join(e.root, "a.txt"))
	if err != nil || m.Name != "a.txt" || m.Size != 3 || m.IsDir || m.IsSymlink || m.Created.IsZero() {
		t.Errorf("file meta = %+v, %v", m, err)
	}
	if l, err := e.svc.Meta(filepath.Join(e.root, "lnk")); err != nil || !l.IsSymlink || l.SymlinkTarget != filepath.Join(e.root, "a.txt") {
		t.Errorf("link meta = %+v, %v", l, err)
	}
	if d, err := e.svc.Meta(e.root); err != nil || !d.IsDir || d.Name != filepath.Base(e.root) {
		t.Errorf("root meta = %+v, %v", d, err)
	}
	_, err = e.svc.Meta(filepath.Join(e.root, "zzz"))
	wantCode(t, err, api.CodePathNotFound)
	_, err = e.svc.Meta(filepath.Join(e.root, "a.txt", "deeper"))
	wantCode(t, err, api.CodePathNotFound)
	_, err = e.svc.Meta(filepath.Join(e.root, dataDirName, "secret.key"))
	wantCode(t, err, authz.CodeForbidden)
}

func TestChecksum(t *testing.T) {
	e := newEnv(t, false)
	abc := write(t, filepath.Join(e.root, "abc"), "abc")
	if err := os.Mkdir(filepath.Join(e.root, "d"), 0o755); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name, path, algo, want, code string
	}{
		{"default is sha256", abc, "", abcSHA256, ""},
		{"sha256", abc, AlgoSHA256, abcSHA256, ""},
		{"sha1", abc, AlgoSHA1, abcSHA1, ""},
		{"md5", abc, AlgoMD5, abcMD5, ""},
		{"unsupported algorithm", abc, "crc32", "", api.CodeBadRequest},
		{"unsupported algorithm on a missing file is still bad request", filepath.Join(e.root, "nope"), "crc32", "", api.CodeBadRequest},
		{"folder", filepath.Join(e.root, "d"), "", "", api.CodeBadRequest},
		{"missing", filepath.Join(e.root, "nope"), "", "", api.CodePathNotFound},
		{"outside the roots", filepath.Join(t.TempDir(), "x"), "", "", authz.CodeForbidden},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := e.svc.Checksum(tc.path, tc.algo)
			if tc.code != "" {
				wantCode(t, err, tc.code)
				return
			}
			if err != nil || got != tc.want {
				t.Errorf("got %q, %v; want %q", got, err, tc.want)
			}
		})
	}
}

func TestCreate(t *testing.T) {
	e := newEnv(t, true)
	t.Run("folder with parents", func(t *testing.T) {
		p := filepath.Join(e.root, "a", "b", "c")
		en, err := e.svc.CreateFolder(p)
		if err != nil || !en.IsDir || en.Path != p {
			t.Fatalf("got %+v, %v", en, err)
		}
	})
	t.Run("folder that exists is a conflict", func(t *testing.T) {
		_, err := e.svc.CreateFolder(filepath.Join(e.root, "a"))
		wantCode(t, err, api.CodeConflict)
		write(t, filepath.Join(e.root, "taken"), "x")
		_, err = e.svc.CreateFolder(filepath.Join(e.root, "taken"))
		wantCode(t, err, api.CodeConflict)
	})
	t.Run("file with parents", func(t *testing.T) {
		p := filepath.Join(e.root, "x", "y", "new.txt")
		en, err := e.svc.CreateFile(p)
		if err != nil || en.IsDir || en.Size != 0 {
			t.Fatalf("got %+v, %v", en, err)
		}
	})
	t.Run("file that exists is a conflict and is not truncated", func(t *testing.T) {
		p := write(t, filepath.Join(e.root, "keep.txt"), "data")
		_, err := e.svc.CreateFile(p)
		wantCode(t, err, api.CodeConflict)
		if read(t, p) != "data" {
			t.Error("existing file was changed")
		}
	})
	t.Run("through a file is not found", func(t *testing.T) {
		_, err := e.svc.CreateFile(filepath.Join(e.root, "keep.txt", "x"))
		if err == nil {
			t.Fatal("want an error")
		}
		if c := api.Map(err).Code; c == api.CodeInternal {
			t.Errorf("code = %s, want a client error", c)
		}
	})
	t.Run("protected folder", func(t *testing.T) {
		_, err := e.svc.CreateFile(filepath.Join(e.root, dataDirName, "new"))
		wantCode(t, err, authz.CodeForbidden)
	})
	t.Run("read-only", func(t *testing.T) {
		ro := e.readOnly()
		_, err := ro.CreateFolder(filepath.Join(e.root, "ro"))
		wantCode(t, err, authz.CodeReadOnly)
		_, err = ro.CreateFile(filepath.Join(e.root, "ro.txt"))
		wantCode(t, err, authz.CodeReadOnly)
		if exists(filepath.Join(e.root, "ro")) || exists(filepath.Join(e.root, "ro.txt")) {
			t.Error("a read-only service created something")
		}
	})
}

func TestWriteContent(t *testing.T) {
	e := newEnv(t, true)
	p := filepath.Join(e.root, "doc.txt")
	old := time.Date(2020, 1, 2, 3, 4, 5, 0, time.UTC)

	t.Run("creates a file", func(t *testing.T) {
		en, err := e.svc.WriteContent(p, []byte("one"), nil)
		if err != nil || en.Size != 3 || read(t, p) != "one" {
			t.Fatalf("got %+v, %v", en, err)
		}
		noHidden(t, e.root)
	})
	t.Run("replaces without a base time", func(t *testing.T) {
		if _, err := e.svc.WriteContent(p, []byte("two"), nil); err != nil || read(t, p) != "two" {
			t.Fatalf("err %v content %q", err, read(t, p))
		}
	})
	t.Run("matching base time to the second is accepted", func(t *testing.T) {
		if err := os.Chtimes(p, old.Add(300*time.Millisecond), old.Add(300*time.Millisecond)); err != nil {
			t.Fatal(err)
		}
		base := old.Add(700 * time.Millisecond) // same second, different fraction
		if _, err := e.svc.WriteContent(p, []byte("three"), &base); err != nil || read(t, p) != "three" {
			t.Fatalf("err %v content %q", err, read(t, p))
		}
	})
	t.Run("stale base time is refused and nothing is written", func(t *testing.T) {
		if err := os.Chtimes(p, old, old); err != nil {
			t.Fatal(err)
		}
		base := old.Add(-time.Hour)
		_, err := e.svc.WriteContent(p, []byte("lost update"), &base)
		wantCode(t, err, api.CodeStaleWrite)
		if !errors.Is(err, api.ErrStale) || read(t, p) != "three" {
			t.Errorf("err %v content %q", err, read(t, p))
		}
		noHidden(t, e.root)
	})
	t.Run("base time for a missing file is not found", func(t *testing.T) {
		base := old
		_, err := e.svc.WriteContent(filepath.Join(e.root, "gone.txt"), []byte("x"), &base)
		wantCode(t, err, api.CodePathNotFound)
	})
	t.Run("a folder cannot take content", func(t *testing.T) {
		_, err := e.svc.WriteContent(e.root, []byte("x"), nil)
		wantCode(t, err, api.CodeNotAFile)
	})
	t.Run("keeps the permission bits of an existing file", func(t *testing.T) {
		if runtime.GOOS == "windows" {
			t.Skip("permission bits are not POSIX here")
		}
		q := write(t, filepath.Join(e.root, "private.txt"), "a")
		if err := os.Chmod(q, 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := e.svc.WriteContent(q, []byte("b"), nil); err != nil {
			t.Fatal(err)
		}
		if info, _ := os.Stat(q); info.Mode().Perm() != 0o600 {
			t.Errorf("mode = %v, want 0600", info.Mode().Perm())
		}
	})
	t.Run("creates missing parents", func(t *testing.T) {
		q := filepath.Join(e.root, "n1", "n2", "f.txt")
		if _, err := e.svc.WriteContent(q, []byte("deep"), nil); err != nil || read(t, q) != "deep" {
			t.Fatalf("err %v", err)
		}
	})
	t.Run("protected and read-only", func(t *testing.T) {
		_, err := e.svc.WriteContent(filepath.Join(e.root, dataDirName, "secret.key"), []byte("x"), nil)
		wantCode(t, err, authz.CodeForbidden)
		_, err = e.readOnly().WriteContent(p, []byte("ro"), nil)
		wantCode(t, err, authz.CodeReadOnly)
		if read(t, p) != "three" {
			t.Error("a read-only write changed the file")
		}
		noHidden(t, e.root)
	})
}

func TestPublishReader(t *testing.T) {
	e := newEnv(t, false)
	p := filepath.Join(e.root, "sub", "up.bin")

	if en, err := e.svc.PublishReader(p, strings.NewReader("v1"), false); err != nil || en.Size != 2 || read(t, p) != "v1" {
		t.Fatalf("first publish: %+v, %v", en, err)
	}
	t.Run("name taken without overwrite is a conflict and untouched", func(t *testing.T) {
		_, err := e.svc.PublishReader(p, strings.NewReader("v2"), false)
		wantCode(t, err, api.CodeConflict)
		if read(t, p) != "v1" {
			t.Error("existing file was replaced")
		}
		noHidden(t, filepath.Dir(p))
	})
	t.Run("overwrite replaces", func(t *testing.T) {
		if _, err := e.svc.PublishReader(p, strings.NewReader("v3"), true); err != nil || read(t, p) != "v3" {
			t.Fatalf("err %v content %q", err, read(t, p))
		}
		noHidden(t, filepath.Dir(p))
	})
	t.Run("a failing source leaves no temp file and keeps the target", func(t *testing.T) {
		_, err := e.svc.PublishReader(p, io.MultiReader(strings.NewReader("partial"), failingReader{}), true)
		if err == nil || read(t, p) != "v3" {
			t.Fatalf("err %v content %q", err, read(t, p))
		}
		noHidden(t, filepath.Dir(p))
	})
	t.Run("a folder with the name is a conflict", func(t *testing.T) {
		_, err := e.svc.PublishReader(filepath.Join(e.root, "sub"), strings.NewReader("x"), true)
		wantCode(t, err, api.CodeConflict)
	})
	t.Run("nil source and read-only", func(t *testing.T) {
		_, err := e.svc.PublishReader(p, nil, false)
		wantCode(t, err, api.CodeBadRequest)
		_, err = e.readOnly().PublishReader(filepath.Join(e.root, "ro.bin"), strings.NewReader("x"), false)
		wantCode(t, err, authz.CodeReadOnly)
		noHidden(t, e.root)
	})
}

type failingReader struct{}

func (failingReader) Read([]byte) (int, error) { return 0, errors.New("disk on fire") }

func TestRename(t *testing.T) {
	e := newEnv(t, true)
	src := write(t, filepath.Join(e.root, "a.txt"), "A")
	other := write(t, filepath.Join(e.root, "b.txt"), "B")
	if err := os.MkdirAll(filepath.Join(e.root, "dir", "sub"), 0o755); err != nil {
		t.Fatal(err)
	}

	t.Run("renames and creates the parent", func(t *testing.T) {
		dst := filepath.Join(e.root, "new", "dir", "c.txt")
		en, err := e.svc.Rename(src, dst)
		if err != nil || en.Path != dst || read(t, dst) != "A" || exists(src) {
			t.Fatalf("got %+v, %v", en, err)
		}
		if _, err := e.svc.Rename(dst, src); err != nil { // and back
			t.Fatal(err)
		}
	})
	t.Run("never replaces an existing entry", func(t *testing.T) {
		_, err := e.svc.Rename(src, other)
		wantCode(t, err, api.CodeConflict)
		if read(t, other) != "B" || read(t, src) != "A" {
			t.Error("a conflicting rename changed a file")
		}
	})
	t.Run("onto itself is a no-op", func(t *testing.T) {
		en, err := e.svc.Rename(src, src)
		if err != nil || en.Path != src || read(t, src) != "A" {
			t.Errorf("got %+v, %v", en, err)
		}
		d := filepath.Join(e.root, "dir")
		if _, err := e.svc.Rename(d, d); err != nil {
			t.Errorf("folder onto itself: %v", err)
		}
	})
	t.Run("a folder cannot move into its own subtree", func(t *testing.T) {
		_, err := e.svc.Rename(filepath.Join(e.root, "dir"), filepath.Join(e.root, "dir", "sub", "dir"))
		wantCode(t, err, api.CodeConflict)
		if exists(filepath.Join(e.root, "dir", "sub", "dir")) {
			t.Error("created inside the source")
		}
	})
	t.Run("missing source", func(t *testing.T) {
		_, err := e.svc.Rename(filepath.Join(e.root, "ghost"), filepath.Join(e.root, "x"))
		wantCode(t, err, api.CodePathNotFound)
	})
	t.Run("protected, read-only, outside", func(t *testing.T) {
		_, err := e.svc.Rename(filepath.Join(e.root, dataDirName, "secret.key"), filepath.Join(e.root, "stolen"))
		wantCode(t, err, authz.CodeForbidden)
		_, err = e.svc.Rename(src, filepath.Join(e.root, dataDirName, "planted"))
		wantCode(t, err, authz.CodeForbidden)
		_, err = e.readOnly().Rename(src, filepath.Join(e.root, "ro"))
		wantCode(t, err, authz.CodeReadOnly)
		_, err = e.svc.Rename(src, filepath.Join(t.TempDir(), "out"))
		wantCode(t, err, authz.CodeForbidden)
		if !exists(src) {
			t.Error("source disappeared")
		}
	})
	t.Run("renames a symlink itself", func(t *testing.T) {
		symlink(t, e.other, filepath.Join(e.root, "ln"))
		en, err := e.svc.Rename(filepath.Join(e.root, "ln"), filepath.Join(e.root, "ln2"))
		if err != nil || !en.IsSymlink || !exists(e.other) {
			t.Fatalf("got %+v, %v", en, err)
		}
	})
}

func TestCopy(t *testing.T) {
	t.Run("file", func(t *testing.T) {
		e := newEnv(t, false)
		src := write(t, filepath.Join(e.root, "a.txt"), "A")
		res := e.svc.Copy([]string{src}, e.other, false, false)
		wantItem(t, res[0], "")
		if read(t, filepath.Join(e.other, "a.txt")) != "A" || read(t, src) != "A" {
			t.Error("copy wrong")
		}
		noHidden(t, e.other)
	})
	t.Run("tree keeps content and restrictive folder modes", func(t *testing.T) {
		e := newEnv(t, false)
		write(t, filepath.Join(e.root, "t", "f1"), "1")
		write(t, filepath.Join(e.root, "t", "d", "f2"), "2")
		if err := os.Mkdir(filepath.Join(e.root, "t", "empty"), 0o755); err != nil {
			t.Fatal(err)
		}
		if runtime.GOOS != "windows" {
			if err := os.Chmod(filepath.Join(e.root, "t", "d"), 0o500); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = os.Chmod(filepath.Join(e.root, "t", "d"), 0o700) })
		}
		res := e.svc.Copy([]string{filepath.Join(e.root, "t")}, e.other, false, false)
		wantItem(t, res[0], "")
		got := filepath.Join(e.other, "t")
		if read(t, filepath.Join(got, "f1")) != "1" || read(t, filepath.Join(got, "d", "f2")) != "2" || !exists(filepath.Join(got, "empty")) {
			t.Error("tree not copied")
		}
		if runtime.GOOS != "windows" {
			info, err := os.Stat(filepath.Join(got, "d"))
			if err != nil || info.Mode().Perm() != 0o500 {
				t.Errorf("mode of copied folder = %v, %v", info.Mode().Perm(), err)
			}
			t.Cleanup(func() { _ = os.Chmod(filepath.Join(got, "d"), 0o700) })
		}
		noHidden(t, e.other)
	})
	t.Run("collisions", func(t *testing.T) {
		tests := []struct {
			name               string
			duplicate, over    bool
			wantCode           string
			wantDst, wantExtra string // content of dst, and of "a (1).txt" ("" = absent)
		}{
			{"conflict", false, false, api.CodeConflict, "OLD", ""},
			{"overwrite", false, true, "", "NEW", ""},
			{"duplicate keeps both", true, false, "", "OLD", "NEW"},
			{"duplicate wins over overwrite", true, true, "", "OLD", "NEW"},
		}
		for _, tc := range tests {
			t.Run(tc.name, func(t *testing.T) {
				e := newEnv(t, false)
				src := write(t, filepath.Join(e.root, "a.txt"), "NEW")
				dst := write(t, filepath.Join(e.other, "a.txt"), "OLD")
				res := e.svc.Copy([]string{src}, e.other, tc.duplicate, tc.over)
				wantItem(t, res[0], tc.wantCode)
				if read(t, dst) != tc.wantDst {
					t.Errorf("dst = %q, want %q", read(t, dst), tc.wantDst)
				}
				extra := filepath.Join(e.other, "a (1).txt")
				if tc.wantExtra == "" && exists(extra) {
					t.Error("unexpected duplicate")
				}
				if tc.wantExtra != "" && read(t, extra) != tc.wantExtra {
					t.Errorf("duplicate = %q", read(t, extra))
				}
				noHidden(t, e.other)
			})
		}
	})
	t.Run("duplicate in place numbers the copies", func(t *testing.T) {
		e := newEnv(t, false)
		src := write(t, filepath.Join(e.root, "a.txt"), "A")
		for i := 0; i < 2; i++ {
			wantItem(t, e.svc.Copy([]string{src}, e.root, true, false)[0], "")
		}
		if got := strings.Join(names(t, e.root), ","); got != "a (1).txt,a (2).txt,a.txt" {
			t.Errorf("names = %s", got)
		}
	})
	t.Run("onto itself without duplicate is a no-op", func(t *testing.T) {
		e := newEnv(t, false)
		src := write(t, filepath.Join(e.root, "a.txt"), "A")
		wantItem(t, e.svc.Copy([]string{src}, e.root, false, true)[0], "")
		if read(t, src) != "A" || len(names(t, e.root)) != 1 {
			t.Error("self copy changed something")
		}
	})
	t.Run("a folder into itself or its subtree is refused", func(t *testing.T) {
		e := newEnv(t, false)
		write(t, filepath.Join(e.root, "t", "sub", "f"), "1")
		for _, dest := range []string{filepath.Join(e.root, "t"), filepath.Join(e.root, "t", "sub")} {
			wantItem(t, e.svc.Copy([]string{filepath.Join(e.root, "t")}, dest, false, false)[0], api.CodeConflict)
			wantItem(t, e.svc.Copy([]string{filepath.Join(e.root, "t")}, dest, true, true)[0], api.CodeConflict)
		}
		if got := strings.Join(names(t, filepath.Join(e.root, "t")), ","); got != "sub" {
			t.Errorf("folder changed: %s", got)
		}
	})
	t.Run("a folder into itself through a link does not recurse", func(t *testing.T) {
		e := newEnv(t, false)
		write(t, filepath.Join(e.root, "t", "sub", "f"), "1")
		symlink(t, filepath.Join("t", "sub"), filepath.Join(e.root, "alias"))
		wantItem(t, e.svc.Copy([]string{filepath.Join(e.root, "t")}, filepath.Join(e.root, "alias"), false, false)[0], "")
		if !exists(filepath.Join(e.root, "t", "sub", "t", "sub", "f")) {
			t.Error("copy missing")
		}
		if exists(filepath.Join(e.root, "t", "sub", "t", "sub", "t")) {
			t.Error("the copy was copied into itself")
		}
		noHidden(t, filepath.Join(e.root, "t", "sub"))
	})
	t.Run("overwrite never removes a destination that contains the source", func(t *testing.T) {
		e := newEnv(t, false)
		// src is root/x/x; copying it into root would replace root/x, its parent.
		src := write(t, filepath.Join(e.root, "x", "x", "f"), "I")
		wantItem(t, e.svc.Copy([]string{filepath.Dir(src)}, e.root, false, true)[0], api.CodeConflict)
		if got := read(t, src); got != "I" {
			t.Errorf("source = %q", got)
		}
		noHidden(t, e.root)
	})
	t.Run("missing source and bad destination", func(t *testing.T) {
		e := newEnv(t, false)
		write(t, filepath.Join(e.root, "f"), "1")
		wantItem(t, e.svc.Copy([]string{filepath.Join(e.root, "ghost")}, e.other, false, false)[0], api.CodePathNotFound)
		wantItem(t, e.svc.Copy([]string{filepath.Join(e.root, "f")}, filepath.Join(e.root, "f"), false, false)[0], api.CodeBadRequest)
		wantItem(t, e.svc.Copy([]string{filepath.Join(e.root, "f")}, t.TempDir(), false, false)[0], authz.CodeForbidden)
		wantItem(t, e.svc.Copy([]string{string(filepath.Separator)}, e.other, false, false)[0], authz.CodeForbidden)
	})
	t.Run("batch reports each item", func(t *testing.T) {
		e := newEnv(t, false)
		a := write(t, filepath.Join(e.root, "a"), "A")
		b := write(t, filepath.Join(e.root, "b"), "B")
		res := e.svc.Copy([]string{a, filepath.Join(e.root, "ghost"), b}, e.other, false, false)
		wantItem(t, res[0], "")
		wantItem(t, res[1], api.CodePathNotFound)
		wantItem(t, res[2], "")
		if res[1].Path != filepath.Join(e.root, "ghost") {
			t.Errorf("item path = %q", res[1].Path)
		}
	})
	t.Run("protected data folder", func(t *testing.T) {
		e := newEnv(t, true)
		write(t, filepath.Join(e.root, "ok.txt"), "ok")
		out := filepath.Join(e.other, "out")
		// a file inside it, the folder itself, and an ancestor of it
		wantItem(t, e.svc.Copy([]string{filepath.Join(e.root, dataDirName, "secret.key")}, out, false, false)[0], authz.CodeForbidden)
		wantItem(t, e.svc.Copy([]string{filepath.Join(e.root, dataDirName)}, out, false, false)[0], authz.CodeForbidden)
		wantItem(t, e.svc.Copy([]string{e.root}, out, false, false)[0], authz.CodeForbidden)
		if exists(out) {
			t.Errorf("protected data reached the destination: %v", names(t, out))
		}
		// destination inside the protected folder
		wantItem(t, e.svc.Copy([]string{filepath.Join(e.root, "ok.txt")}, filepath.Join(e.root, dataDirName), false, false)[0], authz.CodeForbidden)
		wantItem(t, e.svc.Copy([]string{filepath.Join(e.root, "ok.txt")}, out, false, false)[0], "")
	})
	t.Run("read-only changes nothing", func(t *testing.T) {
		e := newEnv(t, false)
		src := write(t, filepath.Join(e.root, "a.txt"), "A")
		write(t, filepath.Join(e.other, "a.txt"), "OLD")
		ro := e.readOnly()
		wantItem(t, ro.Copy([]string{src}, filepath.Join(e.other, "new"), false, false)[0], authz.CodeReadOnly)
		wantItem(t, ro.Copy([]string{src}, e.other, false, true)[0], authz.CodeReadOnly)
		wantItem(t, ro.Copy([]string{src}, e.other, true, false)[0], authz.CodeReadOnly)
		if exists(filepath.Join(e.other, "new")) || read(t, filepath.Join(e.other, "a.txt")) != "OLD" || len(names(t, e.other)) != 1 {
			t.Errorf("read-only copy changed %v", names(t, e.other))
		}
	})
}

func TestCopySymlinks(t *testing.T) {
	t.Run("a source link that leaves the roots is refused", func(t *testing.T) {
		e := newEnv(t, false)
		outside := t.TempDir()
		write(t, filepath.Join(outside, "secret.txt"), "S")
		symlink(t, outside, filepath.Join(e.root, "out"))
		symlink(t, filepath.Join(outside, "secret.txt"), filepath.Join(e.root, "out.txt"))
		wantItem(t, e.svc.Copy([]string{filepath.Join(e.root, "out")}, e.other, false, false)[0], authz.CodeForbidden)
		wantItem(t, e.svc.Copy([]string{filepath.Join(e.root, "out.txt")}, e.other, false, false)[0], authz.CodeForbidden)
		if len(names(t, e.other)) != 0 {
			t.Errorf("data from outside was copied: %v", names(t, e.other))
		}
	})
	t.Run("a link inside a copied folder never copies what it points to", func(t *testing.T) {
		e := newEnv(t, false)
		outside := t.TempDir()
		write(t, filepath.Join(outside, "secret.txt"), "S")
		write(t, filepath.Join(e.root, "t", "f"), "1")
		symlink(t, outside, filepath.Join(e.root, "t", "escape"))
		res := e.svc.Copy([]string{filepath.Join(e.root, "t")}, e.other, false, false)
		wantItem(t, res[0], api.CodeNotImplemented)
		if len(names(t, e.other)) != 0 {
			t.Errorf("a partial copy remains: %v", names(t, e.other))
		}
	})
	t.Run("a link to a folder inside the roots is copied as its content", func(t *testing.T) {
		e := newEnv(t, false)
		write(t, filepath.Join(e.root, "real", "f"), "F")
		symlink(t, "real", filepath.Join(e.root, "lnk"))
		wantItem(t, e.svc.Copy([]string{filepath.Join(e.root, "lnk")}, e.other, false, false)[0], "")
		info, err := os.Lstat(filepath.Join(e.other, "lnk"))
		if err != nil || info.Mode()&fs.ModeSymlink != 0 || read(t, filepath.Join(e.other, "lnk", "f")) != "F" {
			t.Errorf("link copy = %v, %v", info, err)
		}
	})
}

func TestMove(t *testing.T) {
	t.Run("file and folder", func(t *testing.T) {
		e := newEnv(t, false)
		f := write(t, filepath.Join(e.root, "a.txt"), "A")
		write(t, filepath.Join(e.root, "d", "x"), "X")
		res := e.svc.Move([]string{f, filepath.Join(e.root, "d")}, filepath.Join(e.root, "dest"), false, false)
		wantItem(t, res[0], "")
		wantItem(t, res[1], "")
		if exists(f) || exists(filepath.Join(e.root, "d")) || read(t, filepath.Join(e.root, "dest", "a.txt")) != "A" || read(t, filepath.Join(e.root, "dest", "d", "x")) != "X" {
			t.Error("move wrong")
		}
	})
	t.Run("collisions", func(t *testing.T) {
		tests := []struct {
			name            string
			duplicate, over bool
			wantCode        string
			wantSrcGone     bool
			wantDst, extra  string
		}{
			{"conflict leaves the source", false, false, api.CodeConflict, false, "OLD", ""},
			{"overwrite replaces", false, true, "", true, "NEW", ""},
			{"duplicate keeps both", true, false, "", true, "OLD", "NEW"},
		}
		for _, tc := range tests {
			t.Run(tc.name, func(t *testing.T) {
				e := newEnv(t, false)
				src := write(t, filepath.Join(e.root, "a.txt"), "NEW")
				dst := write(t, filepath.Join(e.root, "dest", "a.txt"), "OLD")
				wantItem(t, e.svc.Move([]string{src}, filepath.Dir(dst), tc.duplicate, tc.over)[0], tc.wantCode)
				if exists(src) == tc.wantSrcGone {
					t.Errorf("source present = %v", exists(src))
				}
				if read(t, dst) != tc.wantDst {
					t.Errorf("dst = %q", read(t, dst))
				}
				extra := filepath.Join(filepath.Dir(dst), "a (1).txt")
				if (tc.extra == "") == exists(extra) || (tc.extra != "" && read(t, extra) != tc.extra) {
					t.Errorf("duplicate state wrong, names %v", names(t, filepath.Dir(dst)))
				}
				noHidden(t, filepath.Dir(dst))
			})
		}
	})
	t.Run("across the two allowed folders is a copy then delete", func(t *testing.T) {
		e := newEnv(t, false)
		write(t, filepath.Join(e.root, "t", "d", "f"), "F")
		wantItem(t, e.svc.Move([]string{filepath.Join(e.root, "t")}, e.other, false, false)[0], "")
		if exists(filepath.Join(e.root, "t")) || read(t, filepath.Join(e.other, "t", "d", "f")) != "F" {
			t.Error("cross-root move wrong")
		}
		noHidden(t, e.other)
	})
	t.Run("across roots, a failed copy keeps the source", func(t *testing.T) {
		e := newEnv(t, false)
		outside := t.TempDir()
		write(t, filepath.Join(e.root, "t", "f"), "F")
		symlink(t, outside, filepath.Join(e.root, "t", "escape"))
		wantItem(t, e.svc.Move([]string{filepath.Join(e.root, "t")}, e.other, false, false)[0], api.CodeNotImplemented)
		if read(t, filepath.Join(e.root, "t", "f")) != "F" || len(names(t, e.other)) != 0 {
			t.Errorf("source or destination wrong: %v", names(t, e.other))
		}
	})
	t.Run("a folder into itself, onto itself", func(t *testing.T) {
		e := newEnv(t, false)
		write(t, filepath.Join(e.root, "t", "sub", "f"), "1")
		wantItem(t, e.svc.Move([]string{filepath.Join(e.root, "t")}, filepath.Join(e.root, "t", "sub"), false, true)[0], api.CodeConflict)
		wantItem(t, e.svc.Move([]string{filepath.Join(e.root, "t")}, e.root, false, false)[0], "")
		if read(t, filepath.Join(e.root, "t", "sub", "f")) != "1" {
			t.Error("folder damaged")
		}
	})
	t.Run("moves a symlink as the link", func(t *testing.T) {
		e := newEnv(t, false)
		write(t, filepath.Join(e.other, "target.txt"), "T")
		symlink(t, filepath.Join(e.other, "target.txt"), filepath.Join(e.root, "ln"))
		wantItem(t, e.svc.Move([]string{filepath.Join(e.root, "ln")}, filepath.Join(e.root, "dest"), false, false)[0], "")
		info, err := os.Lstat(filepath.Join(e.root, "dest", "ln"))
		if err != nil || info.Mode()&fs.ModeSymlink == 0 || read(t, filepath.Join(e.other, "target.txt")) != "T" {
			t.Errorf("link move = %v, %v", info, err)
		}
	})
	t.Run("missing source, root, outside", func(t *testing.T) {
		e := newEnv(t, false)
		write(t, filepath.Join(e.root, "f"), "1")
		wantItem(t, e.svc.Move([]string{filepath.Join(e.root, "ghost")}, e.other, false, false)[0], api.CodePathNotFound)
		wantItem(t, e.svc.Move([]string{filepath.Join(e.root, "f")}, t.TempDir(), false, false)[0], authz.CodeForbidden)
		wantItem(t, e.svc.Move([]string{e.root}, e.other, false, false)[0], authz.CodeForbidden)
		if !exists(e.root) || read(t, filepath.Join(e.root, "f")) != "1" {
			t.Error("root was touched")
		}
	})
	t.Run("protected data folder", func(t *testing.T) {
		e := newEnv(t, true)
		write(t, filepath.Join(e.root, "ok.txt"), "ok")
		out := filepath.Join(e.other, "out")
		wantItem(t, e.svc.Move([]string{filepath.Join(e.root, dataDirName, "secret.key")}, out, false, false)[0], authz.CodeForbidden)
		wantItem(t, e.svc.Move([]string{filepath.Join(e.root, dataDirName)}, out, false, false)[0], authz.CodeForbidden)
		wantItem(t, e.svc.Move([]string{filepath.Join(e.root, "ok.txt")}, filepath.Join(e.root, dataDirName), false, false)[0], authz.CodeForbidden)
		if read(t, filepath.Join(e.root, dataDirName, "secret.key")) != "secret" || !exists(filepath.Join(e.root, "ok.txt")) {
			t.Error("protected move changed the tree")
		}
	})
	t.Run("read-only changes nothing", func(t *testing.T) {
		e := newEnv(t, false)
		src := write(t, filepath.Join(e.root, "a.txt"), "A")
		write(t, filepath.Join(e.other, "a.txt"), "OLD")
		ro := e.readOnly()
		wantItem(t, ro.Move([]string{src}, filepath.Join(e.other, "new"), false, false)[0], authz.CodeReadOnly)
		wantItem(t, ro.Move([]string{src}, e.other, false, true)[0], authz.CodeReadOnly)
		if !exists(src) || read(t, filepath.Join(e.other, "a.txt")) != "OLD" || len(names(t, e.other)) != 1 {
			t.Errorf("read-only move changed %v", names(t, e.other))
		}
	})
}

func TestDelete(t *testing.T) {
	t.Run("files, folders and a missing path", func(t *testing.T) {
		e := newEnv(t, false)
		f := write(t, filepath.Join(e.root, "a.txt"), "A")
		write(t, filepath.Join(e.root, "d", "deep", "x"), "X")
		ghost := filepath.Join(e.root, "ghost")
		res := e.svc.Delete([]string{f, ghost, filepath.Join(e.root, "d")})
		wantItem(t, res[0], "")
		wantItem(t, res[1], api.CodePathNotFound) // contract change C1
		wantItem(t, res[2], "")
		if res[1].Path != ghost {
			t.Errorf("path = %q", res[1].Path)
		}
		if len(names(t, e.root)) != 0 {
			t.Errorf("left behind: %v", names(t, e.root))
		}
	})
	t.Run("deleting twice reports the second as missing", func(t *testing.T) {
		e := newEnv(t, false)
		f := write(t, filepath.Join(e.root, "a.txt"), "A")
		res := e.svc.Delete([]string{f, f})
		wantItem(t, res[0], "")
		wantItem(t, res[1], api.CodePathNotFound)
	})
	t.Run("through a file is missing, not an internal error", func(t *testing.T) {
		e := newEnv(t, false)
		f := write(t, filepath.Join(e.root, "a.txt"), "A")
		wantItem(t, e.svc.Delete([]string{filepath.Join(f, "x")})[0], api.CodePathNotFound)
	})
	t.Run("a symlink is removed, its target is not", func(t *testing.T) {
		e := newEnv(t, false)
		write(t, filepath.Join(e.other, "keep", "f"), "K")
		symlink(t, filepath.Join(e.other, "keep"), filepath.Join(e.root, "ln"))
		symlink(t, filepath.Join(e.root, "missing"), filepath.Join(e.root, "dangling"))
		res := e.svc.Delete([]string{filepath.Join(e.root, "ln"), filepath.Join(e.root, "dangling")})
		wantItem(t, res[0], "")
		wantItem(t, res[1], "")
		if exists(filepath.Join(e.root, "ln")) || read(t, filepath.Join(e.other, "keep", "f")) != "K" {
			t.Error("link or target state wrong")
		}
	})
	t.Run("allowed folder itself, outside, protected", func(t *testing.T) {
		e := newEnv(t, true)
		write(t, filepath.Join(e.root, "ok.txt"), "ok")
		res := e.svc.Delete([]string{
			e.other,
			filepath.Join(t.TempDir(), "x"),
			filepath.Join(e.root, dataDirName, "secret.key"),
			filepath.Join(e.root, dataDirName),
			e.root, // contains the protected folder
		})
		for i, r := range res {
			wantItem(t, r, authz.CodeForbidden)
			_ = i
		}
		if !exists(e.other) || !exists(e.root) || read(t, filepath.Join(e.root, dataDirName, "secret.key")) != "secret" || !exists(filepath.Join(e.root, "ok.txt")) {
			t.Error("a refused delete removed something")
		}
	})
	t.Run("read-only", func(t *testing.T) {
		e := newEnv(t, false)
		f := write(t, filepath.Join(e.root, "a.txt"), "A")
		wantItem(t, e.readOnly().Delete([]string{f})[0], authz.CodeReadOnly)
		if !exists(f) {
			t.Error("a read-only delete removed the file")
		}
	})
}

// TestErrorTextHasNoHostPath checks that what a client would see never carries
// a path of this machine, for a sample of failures.
func TestErrorTextHasNoHostPath(t *testing.T) {
	e := newEnv(t, true)
	f := write(t, filepath.Join(e.root, "f"), "1")
	var errs []error
	_, err := e.svc.Meta(filepath.Join(e.root, "ghost"))
	errs = append(errs, err)
	_, err = e.svc.List(f, "", 0)
	errs = append(errs, err)
	_, err = e.svc.List(t.TempDir(), "", 0)
	errs = append(errs, err)
	_, err = e.svc.Rename(f, filepath.Join(e.root, dataDirName, "x"))
	errs = append(errs, err)
	_, err = e.svc.CreateFile(f)
	errs = append(errs, err)
	for _, err := range errs {
		if err == nil {
			t.Fatal("want an error")
		}
		if msg := api.Map(err).Message; strings.Contains(msg, e.root) || strings.Contains(msg, e.other) || strings.Contains(msg, os.TempDir()) {
			t.Errorf("message %q carries a host path", fmt.Sprint(msg))
		}
	}
}
