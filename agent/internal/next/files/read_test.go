package files

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ZaidQamhieh/rfe/agent/internal/next/api"
)

// archiveStamp is the modification time written into the test archives.
var archiveStamp = time.Date(2031, 2, 3, 4, 5, 6, 0, time.UTC)

// memEntry is one entry of a test archive.
type memEntry struct {
	name, body string
	dir        bool
}

// zipBytes builds a zip holding the entries.
func zipBytes(t *testing.T, entries []memEntry) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, e := range entries {
		h := &zip.FileHeader{Name: e.name, Method: zip.Deflate, Modified: archiveStamp}
		if e.dir {
			h.Name += "/"
		}
		w, err := zw.CreateHeader(h)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := io.WriteString(w, e.body); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// tarBytes builds a plain tar holding the entries.
func tarBytes(t *testing.T, entries []memEntry) []byte {
	t.Helper()
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	for _, e := range entries {
		h := &tar.Header{Name: e.name, Mode: 0o644, Size: int64(len(e.body)), Typeflag: tar.TypeReg, ModTime: archiveStamp}
		if e.dir {
			h = &tar.Header{Name: e.name + "/", Mode: 0o755, Typeflag: tar.TypeDir, ModTime: archiveStamp}
		}
		if err := tw.WriteHeader(h); err != nil {
			t.Fatal(err)
		}
		if _, err := io.WriteString(tw, e.body); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// gzipBytes compresses b.
func gzipBytes(t *testing.T, b []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	if _, err := zw.Write(b); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// sampleEntries is a folder, a file in it and a file beside it.
var sampleEntries = []memEntry{{name: "dir", dir: true}, {name: "dir/a.txt", body: "alpha"}, {name: "b.txt", body: "bb"}}

// wantSample checks the listing of sampleEntries.
func wantSample(t *testing.T, got []api.ArchiveEntry) {
	t.Helper()
	want := []api.ArchiveEntry{
		{Path: "dir/", IsDir: true}, {Path: "dir/a.txt", Size: 5}, {Path: "b.txt", Size: 2},
	}
	if len(got) != len(want) {
		t.Fatalf("entries = %+v, want %d of them", got, len(want))
	}
	for i, w := range want {
		g := got[i]
		if g.Path != w.Path || g.Size != w.Size || g.IsDir != w.IsDir || !g.Modified.Equal(archiveStamp) {
			t.Fatalf("entry %d = %+v, want %+v modified %v", i, g, w, archiveStamp)
		}
	}
}

func TestOpenContentReadsAndSeeks(t *testing.T) {
	e := newEnv(t, false)
	p := write(t, filepath.Join(e.root, "sub", "a.txt"), "0123456789")
	f, err := e.svc.OpenContent(p)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if f.Name != "a.txt" || f.Size != 10 || f.ModTime.IsZero() {
		t.Fatalf("file = %+v", f)
	}
	if _, err := f.Content.Seek(4, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	if b, err := io.ReadAll(f.Content); err != nil || string(b) != "456789" {
		t.Fatalf("read after seek = %q, %v", b, err)
	}
}

func TestOpenContentRefusals(t *testing.T) {
	e := newEnv(t, true)
	write(t, filepath.Join(e.root, "a.txt"), "a")
	secret := write(t, filepath.Join(t.TempDir(), "out.txt"), "outside")
	symlink(t, secret, filepath.Join(e.root, "escape"))
	symlink(t, "a.txt", filepath.Join(e.root, "inside")) // relative: os.Root refuses absolute links
	symlink(t, filepath.Join(e.root, dataDirName, "secret.key"), filepath.Join(e.root, "tosecret"))
	cases := []struct {
		name, path string
		want       error
	}{
		{"missing", filepath.Join(e.root, "none.txt"), api.ErrPathNotFound},
		{"missing parent", filepath.Join(e.root, "none", "x.txt"), api.ErrPathNotFound},
		{"folder", e.root, api.ErrNotAFile},
		{"outside the roots", secret, api.ErrForbidden},
		{"dot dot out of root", filepath.Join(e.root, "..", filepath.Base(filepath.Dir(secret)), "out.txt"), api.ErrForbidden},
		{"link out of the roots", filepath.Join(e.root, "escape"), api.ErrForbidden},
		{"protected folder", filepath.Join(e.root, dataDirName, "secret.key"), api.ErrForbidden},
		{"link to protected", filepath.Join(e.root, "tosecret"), api.ErrForbidden},
		{"relative path", "a.txt", api.ErrBadRequest},
	}
	for _, c := range cases {
		f, err := e.svc.OpenContent(c.path)
		if f != nil {
			f.Close()
		}
		if !errors.Is(err, c.want) {
			t.Errorf("%s: OpenContent = %v, want %v", c.name, err, c.want)
		}
	}
	// A link that stays inside the roots is followed.
	f, err := e.svc.OpenContent(filepath.Join(e.root, "inside"))
	if err != nil {
		t.Fatalf("inside link: %v", err)
	}
	f.Close()
}

func TestOpenContentWorksOnAReadOnlyView(t *testing.T) {
	e := newEnv(t, false)
	p := write(t, filepath.Join(e.root, "a.txt"), "a")
	f, err := e.readOnly().OpenContent(p)
	if err != nil {
		t.Fatalf("read-only OpenContent: %v", err)
	}
	f.Close()
}

func TestArchiveEntriesFormats(t *testing.T) {
	e := newEnv(t, false)
	tarData := tarBytes(t, sampleEntries)
	cases := map[string][]byte{
		"a.zip":    zipBytes(t, sampleEntries),
		"a.ZIP":    zipBytes(t, sampleEntries),
		"a.tar":    tarData,
		"a.tar.gz": gzipBytes(t, tarData),
		"a.tgz":    gzipBytes(t, tarData),
	}
	for name, data := range cases {
		p := filepath.Join(e.root, name)
		if err := os.WriteFile(p, data, 0o644); err != nil {
			t.Fatal(err)
		}
		got, err := e.svc.ArchiveEntries(p, 0)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		wantSample(t, got)
	}
}

func TestArchiveEntriesLimit(t *testing.T) {
	e := newEnv(t, false)
	var many []memEntry
	for i := 0; i < MaxArchiveListLimit+10; i++ {
		many = append(many, memEntry{name: fmt.Sprintf("f%05d", i)})
	}
	p := filepath.Join(e.root, "many.zip")
	if err := os.WriteFile(p, zipBytes(t, many), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct{ limit, want int }{
		{0, DefaultArchiveListLimit}, {-3, DefaultArchiveListLimit}, {3, 3},
		{MaxArchiveListLimit + 1, MaxArchiveListLimit}, {int(^uint(0) >> 1), MaxArchiveListLimit},
	} {
		got, err := e.svc.ArchiveEntries(p, c.limit)
		if err != nil || len(got) != c.want {
			t.Errorf("limit %d: %d entries, %v; want %d", c.limit, len(got), err, c.want)
		}
	}
	tp := filepath.Join(e.root, "many.tar")
	if err := os.WriteFile(tp, tarBytes(t, many), 0o644); err != nil {
		t.Fatal(err)
	}
	if got, err := e.svc.ArchiveEntries(tp, 7); err != nil || len(got) != 7 || got[6].Path != "f00006" {
		t.Errorf("tar limit 7: %d entries, %v", len(got), err)
	}
}

func TestArchiveEntriesRefusals(t *testing.T) {
	e := newEnv(t, false)
	good := zipBytes(t, sampleEntries)
	write(t, filepath.Join(e.root, "notes.txt"), "text")
	write(t, filepath.Join(e.root, "bad.zip"), "not a zip")
	write(t, filepath.Join(e.root, "bad.tar.gz"), "not gzip")
	write(t, filepath.Join(e.root, "bad.tar"), "not a tar, but long enough to fill a header block"+string(make([]byte, 600)))
	if err := os.WriteFile(filepath.Join(e.root, "good.zip"), good, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(e.root, "dir.zip"), 0o755); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "o.zip")
	if err := os.WriteFile(outside, good, 0o644); err != nil {
		t.Fatal(err)
	}
	symlink(t, outside, filepath.Join(e.root, "link.zip"))
	cases := []struct {
		name, path string
		want       error
	}{
		{"other type", "notes.txt", api.ErrUnsupportedFormat},
		{"damaged zip", "bad.zip", api.ErrBadRequest},
		{"damaged gzip", "bad.tar.gz", api.ErrBadRequest},
		{"damaged tar", "bad.tar", api.ErrBadRequest},
		{"folder named like an archive", "dir.zip", api.ErrUnsupportedFormat},
		{"missing", "none.zip", api.ErrPathNotFound},
		{"link out of the roots", "link.zip", api.ErrForbidden},
	}
	for _, c := range cases {
		_, err := e.svc.ArchiveEntries(filepath.Join(e.root, c.path), 0)
		if !errors.Is(err, c.want) {
			t.Errorf("%s: ArchiveEntries = %v, want %v", c.name, err, c.want)
		}
	}
	if _, err := e.svc.ArchiveEntries(outside, 0); !errors.Is(err, api.ErrForbidden) {
		t.Errorf("outside the roots: %v", err)
	}
}

// A zip made by Compress from a folder with a link in it lists what Compress
// wrote (the link is skipped) and what was read back matches the files.
func TestArchiveEntriesOfCompressedFolderWithSymlink(t *testing.T) {
	e := newEnv(t, false)
	write(t, filepath.Join(e.root, "pack", "a.txt"), "alpha")
	write(t, filepath.Join(e.root, "pack", "sub", "b.txt"), "beta!")
	symlink(t, filepath.Join(e.root, "pack", "a.txt"), filepath.Join(e.root, "pack", "ln"))
	dest := filepath.Join(e.root, "pack.zip")
	if _, err := e.svc.Compress([]string{filepath.Join(e.root, "pack")}, dest); err != nil {
		t.Fatal(err)
	}
	got, err := e.svc.ArchiveEntries(dest, 0)
	if err != nil {
		t.Fatal(err)
	}
	sizes := map[string]int64{}
	for _, g := range got {
		sizes[g.Path] = g.Size
	}
	if sizes["pack/a.txt"] != 5 || sizes["pack/sub/b.txt"] != 5 || len(got) != 4 {
		t.Fatalf("entries = %+v", got)
	}
	if _, linked := sizes["pack/ln"]; linked {
		t.Fatalf("the link was archived: %+v", got)
	}
}

// A zip that holds a link entry lists it as a plain, non-folder entry.
func TestArchiveEntriesListsLinkEntries(t *testing.T) {
	e := newEnv(t, false)
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	h := &zip.FileHeader{Name: "ln", Method: zip.Store, Modified: archiveStamp}
	h.SetMode(os.ModeSymlink | 0o777)
	w, err := zw.CreateHeader(h)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.WriteString(w, "target.txt"); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(e.root, "l.zip")
	if err := os.WriteFile(p, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := e.svc.ArchiveEntries(p, 0)
	if err != nil || len(got) != 1 || got[0].Path != "ln" || got[0].IsDir || got[0].Size != int64(len("target.txt")) {
		t.Fatalf("entries = %+v, %v", got, err)
	}
}

func TestArchiveEntriesInflateBudget(t *testing.T) {
	e := newEnv(t, false)
	big := []memEntry{{name: "first", body: "x"}, {name: "second", body: string(make([]byte, 1<<16))}, {name: "third", body: "y"}}
	p := filepath.Join(e.root, "big.tar.gz")
	if err := os.WriteFile(p, gzipBytes(t, tarBytes(t, big)), 0o644); err != nil {
		t.Fatal(err)
	}
	old := listInflateBudget
	t.Cleanup(func() { listInflateBudget = old })
	listInflateBudget = 4096
	if _, err := e.svc.ArchiveEntries(p, 0); !errors.Is(err, api.ErrPayloadTooLarge) || !errors.Is(err, ErrArchiveListTooLarge) {
		t.Fatalf("over the budget: %v", err)
	}
	listInflateBudget = old
	if got, err := e.svc.ArchiveEntries(p, 0); err != nil || len(got) != 3 {
		t.Fatalf("within the budget: %d entries, %v", len(got), err)
	}
}
