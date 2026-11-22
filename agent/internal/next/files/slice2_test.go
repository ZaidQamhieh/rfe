package files

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/ZaidQamhieh/rfe/agent/internal/next/api"
	"github.com/ZaidQamhieh/rfe/agent/internal/next/authz"
	"github.com/ZaidQamhieh/rfe/agent/internal/next/rootfs"
)

// zipEntry and tarEntry describe an archive member for the builders below.
type zipEntry struct {
	name, body string
	mode       fs.FileMode // 0 means a plain file
}

type tarEntry struct {
	name, body, link string
	typ              byte // 0 means a regular file
}

// makeZip writes a zip with the given raw entry names (no sanitising).
func makeZip(t *testing.T, p string, entries ...zipEntry) string {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, e := range entries {
		hdr := &zip.FileHeader{Name: e.name, Method: zip.Deflate}
		if e.mode != 0 {
			hdr.SetMode(e.mode)
		}
		w, err := zw.CreateHeader(hdr)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(e.body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return write(t, p, buf.String())
}

// makeTarGz writes a .tar.gz with the given raw entry names.
func makeTarGz(t *testing.T, p string, entries ...tarEntry) string {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for _, e := range entries {
		typ := e.typ
		if typ == 0 {
			typ = tar.TypeReg
		}
		hdr := &tar.Header{Name: e.name, Typeflag: typ, Mode: 0o644, Linkname: e.link}
		if typ == tar.TypeReg {
			hdr.Size = int64(len(e.body))
		}
		if typ == tar.TypeDir {
			hdr.Mode = 0o755
		}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatal(err)
		}
		if typ == tar.TypeReg {
			if _, err := tw.Write([]byte(e.body)); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return write(t, p, buf.String())
}

// zipNames lists the member names of a zip file, sorted.
func zipNames(t *testing.T, p string) []string {
	t.Helper()
	zr, err := zip.OpenReader(p)
	if err != nil {
		t.Fatal(err)
	}
	defer zr.Close()
	var out []string
	for _, f := range zr.File {
		out = append(out, f.Name)
	}
	sort.Strings(out)
	return out
}

// lowerLimits lowers the extraction limits for one test.
func lowerLimits(t *testing.T, entries int, bytes int64) {
	t.Helper()
	oe, ob := archiveEntryLimit, archiveByteLimit
	archiveEntryLimit, archiveByteLimit = entries, bytes
	t.Cleanup(func() { archiveEntryLimit, archiveByteLimit = oe, ob })
}

func TestCompressExtractRoundTrip(t *testing.T) {
	e := newEnv(t, false)
	write(t, filepath.Join(e.root, "src", "a.txt"), "AAA")
	write(t, filepath.Join(e.root, "src", "sub", "b.txt"), "BBB")
	if err := os.MkdirAll(filepath.Join(e.root, "src", "empty"), 0o755); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(e.root, "single.txt"), "S")
	zipPath := filepath.Join(e.root, "out.zip")

	ent, err := e.svc.Compress([]string{filepath.Join(e.root, "src"), filepath.Join(e.root, "single.txt")}, zipPath)
	if err != nil || ent.Path != zipPath || ent.IsDir {
		t.Fatalf("Compress = %+v, %v", ent, err)
	}
	want := []string{"single.txt", "src/", "src/a.txt", "src/empty/", "src/sub/", "src/sub/b.txt"}
	if got := zipNames(t, zipPath); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("zip members = %v, want %v", got, want)
	}
	noHidden(t, e.root)

	t.Run("a taken archive name is kept and the new one is numbered", func(t *testing.T) {
		ent, err := e.svc.Compress([]string{filepath.Join(e.root, "single.txt")}, zipPath)
		if err != nil || filepath.Base(ent.Path) != "out (1).zip" {
			t.Fatalf("Compress = %+v, %v", ent, err)
		}
		if got := zipNames(t, zipPath); len(got) != len(want) {
			t.Errorf("the existing archive was changed: %v", got)
		}
	})

	t.Run("zip extracts to the same tree", func(t *testing.T) {
		dest := filepath.Join(e.other, "x", "y") // missing parents are created
		ent, err := e.svc.Extract(zipPath, dest, false)
		if err != nil || ent.Path != dest || !ent.IsDir {
			t.Fatalf("Extract = %+v, %v", ent, err)
		}
		if read(t, filepath.Join(dest, "src", "sub", "b.txt")) != "BBB" || read(t, filepath.Join(dest, "single.txt")) != "S" {
			t.Errorf("content differs: %v", names(t, dest))
		}
		if !exists(filepath.Join(dest, "src", "empty")) {
			t.Error("empty folder lost")
		}
		noHidden(t, dest)
	})

	t.Run("tar.gz and tgz extract", func(t *testing.T) {
		for _, name := range []string{"a.tar.gz", "B.TGZ"} {
			arc := makeTarGz(t, filepath.Join(e.root, name),
				tarEntry{name: "d/", typ: tar.TypeDir}, tarEntry{name: "d/f.txt", body: "F"}, tarEntry{name: "./g.txt", body: "G"})
			dest := filepath.Join(e.other, "tar-"+name)
			if _, err := e.svc.Extract(arc, dest, false); err != nil {
				t.Fatal(err)
			}
			if read(t, filepath.Join(dest, "d", "f.txt")) != "F" || read(t, filepath.Join(dest, "g.txt")) != "G" {
				t.Errorf("%s: content differs: %v", name, names(t, dest))
			}
		}
	})

	t.Run("errors", func(t *testing.T) {
		_, err := e.svc.Compress(nil, filepath.Join(e.root, "n.zip"))
		wantCode(t, err, api.CodeBadRequest)
		_, err = e.svc.Compress([]string{filepath.Join(e.root, "missing")}, filepath.Join(e.root, "n.zip"))
		wantCode(t, err, api.CodePathNotFound)
		_, err = e.svc.Compress([]string{filepath.Join(e.root, "src"), filepath.Join(e.other, "src")}, filepath.Join(e.root, "n.zip"))
		wantCode(t, err, api.CodeBadRequest) // two sources both called "src"
		_, err = e.svc.Compress([]string{filepath.Join(e.root, "single.txt")}, filepath.Join(t.TempDir(), "n.zip"))
		wantCode(t, err, authz.CodeForbidden)
		_, err = e.svc.Extract(filepath.Join(e.root, "single.txt"), e.other, false)
		wantCode(t, err, api.CodeUnsupported)
		_, err = e.svc.Extract(filepath.Join(e.root, "gone.zip"), e.other, false)
		wantCode(t, err, api.CodePathNotFound)
		_, err = e.readOnly().Compress([]string{filepath.Join(e.root, "single.txt")}, filepath.Join(e.root, "n.zip"))
		wantCode(t, err, authz.CodeReadOnly)
		_, err = e.readOnly().Extract(zipPath, e.other, false)
		wantCode(t, err, authz.CodeReadOnly)
		if exists(filepath.Join(e.root, "n.zip")) {
			t.Error("a failed Compress left an archive")
		}
		noHidden(t, e.root)
	})
}

func TestCompressProtectedAndSelf(t *testing.T) {
	t.Run("a parent of the protected data folder is refused", func(t *testing.T) {
		e := newEnv(t, true)
		out := filepath.Join(e.other, "all.zip")
		_, err := e.svc.Compress([]string{e.root}, out)
		wantCode(t, err, authz.CodeForbidden)
		if exists(out) || len(names(t, e.other)) != 0 {
			t.Errorf("an archive was left behind: %v", names(t, e.other))
		}
	})
	t.Run("the archive inside its own source, and links, are left out", func(t *testing.T) {
		e := newEnv(t, false)
		write(t, filepath.Join(e.root, "src", "a.txt"), "A")
		symlink(t, "a.txt", filepath.Join(e.root, "src", "lnk"))
		if _, err := e.svc.Compress([]string{filepath.Join(e.root, "src")}, filepath.Join(e.root, "src", "self.zip")); err != nil {
			t.Fatal(err)
		}
		got := strings.Join(zipNames(t, filepath.Join(e.root, "src", "self.zip")), ",")
		if got != "src/,src/a.txt" {
			t.Errorf("members = %s", got)
		}
	})
	t.Run("a link named as a source is refused", func(t *testing.T) {
		e := newEnv(t, false)
		write(t, filepath.Join(e.root, "a.txt"), "A")
		symlink(t, "a.txt", filepath.Join(e.root, "lnk"))
		_, err := e.svc.Compress([]string{filepath.Join(e.root, "lnk")}, filepath.Join(e.root, "o.zip"))
		wantCode(t, err, api.CodeNotAFile)
		noHidden(t, e.root)
	})
}

func TestExtractRefusesEscapes(t *testing.T) {
	for _, name := range []string{"../evil.txt", "a/../../evil.txt", "/abs/evil.txt", `\\srv\share\evil.txt`, `C:\evil.txt`, `C:evil.txt`, `..\evil.txt`} {
		t.Run(name, func(t *testing.T) {
			e := newEnv(t, false)
			zipArc := makeZip(t, filepath.Join(e.root, "z.zip"), zipEntry{name: "fine.txt", body: "ok"}, zipEntry{name: name, body: "x"})
			tarArc := makeTarGz(t, filepath.Join(e.root, "t.tar.gz"), tarEntry{name: "fine.txt", body: "ok"}, tarEntry{name: name, body: "x"})
			for _, arc := range []string{zipArc, tarArc} {
				dest := filepath.Join(e.other, "d")
				_, err := e.svc.Extract(arc, dest, false)
				wantCode(t, err, authz.CodeForbidden)
				if exists(dest) {
					t.Errorf("%s: the destination folder (created by the call) and fine.txt remain: %v", filepath.Base(arc), names(t, e.other))
				}
				if exists(filepath.Join(e.root, "evil.txt")) || exists(filepath.Join(e.other, "evil.txt")) {
					t.Error("an entry escaped the destination")
				}
			}
		})
	}
	t.Run("a folder that existed keeps its content and loses the partial extraction", func(t *testing.T) {
		e := newEnv(t, false)
		write(t, filepath.Join(e.other, "d", "mine.txt"), "mine")
		arc := makeZip(t, filepath.Join(e.root, "z.zip"), zipEntry{name: "fine.txt", body: "ok"}, zipEntry{name: "../evil", body: "x"})
		_, err := e.svc.Extract(arc, filepath.Join(e.other, "d"), false)
		wantCode(t, err, authz.CodeForbidden)
		if got := strings.Join(names(t, filepath.Join(e.other, "d")), ","); got != "mine.txt" {
			t.Errorf("destination = %s", got)
		}
	})
}

func TestExtractLinks(t *testing.T) {
	e := newEnv(t, false)
	outside := t.TempDir()
	write(t, filepath.Join(outside, "secret.txt"), "S")
	arc := makeTarGz(t, filepath.Join(e.root, "l.tar.gz"),
		tarEntry{name: "a.txt", body: "A"},
		tarEntry{name: "abs", typ: tar.TypeSymlink, link: outside},
		tarEntry{name: "up", typ: tar.TypeSymlink, link: "../../x"},
		tarEntry{name: "d/ok", typ: tar.TypeSymlink, link: "../a.txt"},
		tarEntry{name: "hard", typ: tar.TypeLink, link: "a.txt"})
	dest := filepath.Join(e.other, "d")
	if _, err := e.svc.Extract(arc, dest, false); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(names(t, dest), ","); got != "a.txt,d" {
		t.Errorf("destination = %s (unsafe links and hard links must be skipped)", got)
	}
	target, err := os.Readlink(filepath.Join(dest, "d", "ok"))
	if err != nil || target != filepath.Join("..", "a.txt") || read(t, filepath.Join(dest, "d", "ok")) != "A" {
		t.Errorf("in-tree link = %q, %v", target, err)
	}

	t.Run("an entry written through an earlier link is refused", func(t *testing.T) {
		arc := makeTarGz(t, filepath.Join(e.root, "l2.tar.gz"),
			tarEntry{name: "ln", typ: tar.TypeSymlink, link: "."}, tarEntry{name: "ln/evil.txt", body: "x"})
		_, err := e.svc.Extract(arc, filepath.Join(e.other, "d2"), false)
		wantCode(t, err, authz.CodeForbidden)
		zipArc := makeZip(t, filepath.Join(e.root, "l2.zip"),
			zipEntry{name: "ln", body: ".", mode: fs.ModeSymlink | 0o777}, zipEntry{name: "ln/evil.txt", body: "x"})
		_, err = e.svc.Extract(zipArc, filepath.Join(e.other, "d2"), false)
		wantCode(t, err, authz.CodeForbidden)
	})

	t.Run("a link already in the destination is never written through", func(t *testing.T) {
		out2 := t.TempDir()
		dest := filepath.Join(e.other, "d3")
		if err := os.MkdirAll(dest, 0o755); err != nil {
			t.Fatal(err)
		}
		symlink(t, out2, filepath.Join(dest, "lnk"))
		arc := makeZip(t, filepath.Join(e.root, "l3.zip"), zipEntry{name: "lnk/f.txt", body: "F"})
		if _, err := e.svc.Extract(arc, dest, true); err != nil {
			wantCode(t, err, api.CodeConflict) // overwrite never turns a link into a folder
		}
		if len(names(t, out2)) != 0 {
			t.Errorf("written outside through a link: %v", names(t, out2))
		}
		if _, err := e.svc.Extract(arc, dest, false); err != nil {
			t.Fatal(err)
		}
		if len(names(t, out2)) != 0 || read(t, filepath.Join(dest, "lnk (1)", "f.txt")) != "F" {
			t.Errorf("outside = %v, dest = %v", names(t, out2), names(t, dest))
		}
	})
}

func TestExtractBombLimits(t *testing.T) {
	t.Run("entry count", func(t *testing.T) {
		e := newEnv(t, false)
		lowerLimits(t, 3, archiveByteLimit)
		var ents []zipEntry
		var tents []tarEntry
		for _, n := range []string{"1", "2", "3", "4"} {
			ents = append(ents, zipEntry{name: n, body: n})
			tents = append(tents, tarEntry{name: n, body: n})
		}
		for _, arc := range []string{
			makeZip(t, filepath.Join(e.root, "z.zip"), ents...), makeTarGz(t, filepath.Join(e.root, "t.tgz"), tents...),
		} {
			_, err := e.svc.Extract(arc, filepath.Join(e.other, "d"), false)
			wantCode(t, err, api.CodePayloadTooLarge)
			if exists(filepath.Join(e.other, "d")) {
				t.Errorf("%s: partial extraction remains: %v", filepath.Base(arc), names(t, e.other))
			}
		}
	})
	t.Run("expanded bytes, counted on the stream", func(t *testing.T) {
		e := newEnv(t, false)
		lowerLimits(t, archiveEntryLimit, 10)
		body := strings.Repeat("x", 6)
		tarArc := makeTarGz(t, filepath.Join(e.root, "t.tgz"), tarEntry{name: "a", body: body}, tarEntry{name: "b", body: body})
		_, err := e.svc.Extract(tarArc, filepath.Join(e.other, "d"), false)
		wantCode(t, err, api.CodePayloadTooLarge)
		zipArc := makeZip(t, filepath.Join(e.root, "z.zip"), zipEntry{name: "big", body: strings.Repeat("x", 11)})
		_, err = e.svc.Extract(zipArc, filepath.Join(e.other, "d"), false)
		wantCode(t, err, api.CodePayloadTooLarge)
		if exists(filepath.Join(e.other, "d")) {
			t.Errorf("partial extraction remains: %v", names(t, e.other))
		}
	})
	t.Run("the real limits are the documented ones", func(t *testing.T) {
		if maxArchiveEntries != 100_000 || maxArchiveTotalBytes != 2<<30 {
			t.Errorf("limits = %d, %d", maxArchiveEntries, maxArchiveTotalBytes)
		}
	})
}

func TestExtractCollisions(t *testing.T) {
	setup := func(t *testing.T) (*env, string, string) {
		e := newEnv(t, false)
		dest := filepath.Join(e.other, "d")
		write(t, filepath.Join(dest, "a.txt"), "old")
		write(t, filepath.Join(dest, "sub", "keep.txt"), "keep")
		arc := makeZip(t, filepath.Join(e.root, "z.zip"),
			zipEntry{name: "a.txt", body: "new"}, zipEntry{name: "sub/in.txt", body: "in"})
		return e, arc, dest
	}
	t.Run("without the flag an existing file is kept", func(t *testing.T) {
		e, arc, dest := setup(t)
		if _, err := e.svc.Extract(arc, dest, false); err != nil {
			t.Fatal(err)
		}
		if read(t, filepath.Join(dest, "a.txt")) != "old" || read(t, filepath.Join(dest, "a (1).txt")) != "new" {
			t.Errorf("destination = %v", names(t, dest))
		}
		if read(t, filepath.Join(dest, "sub", "keep.txt")) != "keep" || read(t, filepath.Join(dest, "sub", "in.txt")) != "in" {
			t.Error("folders were not merged")
		}
		noHidden(t, dest)
	})
	t.Run("with the flag it replaces files", func(t *testing.T) {
		e, arc, dest := setup(t)
		if _, err := e.svc.Extract(arc, dest, true); err != nil {
			t.Fatal(err)
		}
		if read(t, filepath.Join(dest, "a.txt")) != "new" || len(names(t, dest)) != 2 {
			t.Errorf("destination = %v", names(t, dest))
		}
		noHidden(t, dest)
	})
	t.Run("the flag never replaces a folder by a file", func(t *testing.T) {
		e, _, dest := setup(t)
		arc := makeZip(t, filepath.Join(e.root, "z2.zip"), zipEntry{name: "sub", body: "file"})
		_, err := e.svc.Extract(arc, dest, true)
		wantCode(t, err, api.CodeConflict)
		if read(t, filepath.Join(dest, "sub", "keep.txt")) != "keep" {
			t.Error("the folder was damaged")
		}
		noHidden(t, dest)
	})
}

// trashEnv is an env whose service has a trash folder outside the roots.
func trashEnv(t *testing.T, protected bool) (*env, string) {
	t.Helper()
	e := newEnv(t, protected)
	dir := filepath.Join(t.TempDir(), "Trash")
	if err := e.svc.WithTrash(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = e.svc.Close() })
	tick := time.Date(2026, 1, 2, 3, 4, 5, 0, time.Local)
	e.svc.trash.now = func() time.Time { tick = tick.Add(time.Second); return tick }
	return e, dir
}

func TestTrashRoundTrip(t *testing.T) {
	e, dir := trashEnv(t, false)
	write(t, filepath.Join(e.root, "docs", "a.txt"), "A")
	write(t, filepath.Join(e.root, "tree", "f.txt"), "F")
	write(t, filepath.Join(e.root, "tree", "sub", "g.txt"), "G")
	symlink(t, "f.txt", filepath.Join(e.root, "tree", "lnk"))
	write(t, filepath.Join(e.other, "a.txt"), "A2")

	res := e.svc.Trash([]string{
		filepath.Join(e.root, "docs", "a.txt"), filepath.Join(e.root, "tree"),
		filepath.Join(e.other, "a.txt"), filepath.Join(e.root, "missing"),
	})
	wantItem(t, res[0], "")
	wantItem(t, res[1], "")
	wantItem(t, res[2], "")
	wantItem(t, res[3], api.CodePathNotFound)
	if exists(filepath.Join(e.root, "tree")) || exists(filepath.Join(e.root, "docs", "a.txt")) {
		t.Error("trashed items are still in place")
	}
	// The on-disk layout is the old agent's: files/<id> and info/<id>.trashinfo.
	if got := strings.Join(names(t, filepath.Join(dir, "files")), ","); got != "a.txt,a_1.txt,tree" {
		t.Errorf("files/ = %s", got)
	}
	info := read(t, filepath.Join(dir, "info", "a.txt.trashinfo"))
	if !strings.HasPrefix(info, "[Trash Info]\nPath="+e.root) || !strings.Contains(info, "DeletionDate=2026-01-02T03:04:") {
		t.Errorf("sidecar = %q", info)
	}
	noHidden(t, filepath.Join(dir, "files"))

	list, err := e.svc.ListTrash()
	if err != nil || len(list) != 3 {
		t.Fatalf("ListTrash = %+v, %v", list, err)
	}
	if list[0].OriginalPath != filepath.Join(e.other, "a.txt") || list[0].ID != "a_1.txt" || list[0].Name != "a.txt" || list[0].Size != 2 || list[0].IsDir {
		t.Errorf("newest = %+v", list[0])
	}
	if list[1].ID != "tree" || !list[1].IsDir || list[2].ID != "a.txt" {
		t.Errorf("order = %+v", list)
	}

	back := e.svc.Restore([]string{"tree", "a.txt", "a_1.txt"})
	for _, r := range back {
		if !r.OK {
			t.Fatalf("Restore = %+v", back)
		}
	}
	if back[0].Path != filepath.Join(e.root, "tree") || back[2].Path != filepath.Join(e.other, "a.txt") {
		t.Errorf("restored paths = %v, %v", back[0].Path, back[2].Path)
	}
	if read(t, filepath.Join(e.root, "tree", "sub", "g.txt")) != "G" || read(t, filepath.Join(e.root, "docs", "a.txt")) != "A" || read(t, filepath.Join(e.other, "a.txt")) != "A2" {
		t.Error("content differs after restore")
	}
	if target, err := os.Readlink(filepath.Join(e.root, "tree", "lnk")); err != nil || target != "f.txt" {
		t.Errorf("link after restore = %q, %v", target, err)
	}
	if left, _ := e.svc.ListTrash(); len(left) != 0 || len(names(t, filepath.Join(dir, "files"))) != 0 || len(names(t, filepath.Join(dir, "info"))) != 0 {
		t.Errorf("the trash is not empty: %v", left)
	}
	noHidden(t, e.root)
}

func TestTrashRefusals(t *testing.T) {
	t.Run("not configured, read-only, symlink, protected", func(t *testing.T) {
		e := newEnv(t, true)
		p := write(t, filepath.Join(e.root, "a.txt"), "A")
		wantItem(t, e.svc.Trash([]string{p})[0], api.CodeNotImplemented)
		wantItem(t, e.svc.Restore([]string{"a.txt"})[0], api.CodeNotImplemented)
		_, err := e.svc.ListTrash()
		wantCode(t, err, api.CodeNotImplemented)
		te, _ := trashEnv(t, true)
		p = write(t, filepath.Join(te.root, "a.txt"), "A")
		prot := te.svc.Trash([]string{filepath.Join(te.root, dataDirName), te.root})
		wantItem(t, prot[0], authz.CodeForbidden)
		wantItem(t, prot[1], authz.CodeForbidden)
		if !exists(filepath.Join(te.root, dataDirName, "secret.key")) {
			t.Fatal("the protected folder was trashed")
		}
		symlink(t, "a.txt", filepath.Join(te.root, "lnk"))
		wantItem(t, te.svc.Trash([]string{filepath.Join(te.root, "lnk")})[0], api.CodeNotImplemented)
		ro := New(te.set.ReadOnly())
		if err := ro.WithTrash(te.svc.trash.dir); err != nil {
			t.Fatal(err)
		}
		defer ro.Close()
		wantItem(t, ro.Trash([]string{p})[0], authz.CodeReadOnly)
		wantItem(t, ro.Restore([]string{"a.txt"})[0], authz.CodeReadOnly)
		_, err = ro.EmptyTrash(nil)
		wantCode(t, err, authz.CodeReadOnly)
		if read(t, p) != "A" {
			t.Error("a refused item was changed")
		}
	})
	t.Run("a trash folder inside the protected data folder works", func(t *testing.T) {
		e := newEnv(t, true)
		if err := e.svc.WithTrash(filepath.Join(e.root, dataDirName, "trash")); err != nil {
			t.Fatal(err)
		}
		defer e.svc.Close()
		p := write(t, filepath.Join(e.root, "a.txt"), "A")
		wantItem(t, e.svc.Trash([]string{p})[0], "")
		wantItem(t, e.svc.Restore([]string{"a.txt"})[0], "")
		if read(t, p) != "A" {
			t.Error("not restored")
		}
	})
	t.Run("a relative trash folder is refused", func(t *testing.T) {
		wantCode(t, newEnv(t, false).svc.WithTrash("trash"), api.CodeBadRequest)
	})
}

func TestRestoreCollisionAndIDs(t *testing.T) {
	e, dir := trashEnv(t, false)
	p := write(t, filepath.Join(e.root, "gone", "a.txt"), "OLD")
	wantItem(t, e.svc.Trash([]string{p})[0], "")
	if err := os.RemoveAll(filepath.Join(e.root, "gone")); err != nil {
		t.Fatal(err)
	}

	t.Run("missing parents are created, a taken name gets a number", func(t *testing.T) {
		write(t, p, "NEW")
		r := e.svc.Restore([]string{"a.txt"})[0]
		if !r.OK || r.Path != filepath.Join(e.root, "gone", "a (1).txt") {
			t.Fatalf("Restore = %+v", r)
		}
		if read(t, p) != "NEW" || read(t, r.Path) != "OLD" {
			t.Error("the existing file was replaced or the content is wrong")
		}
		if left, _ := e.svc.ListTrash(); len(left) != 0 {
			t.Errorf("trash still lists %v", left)
		}
	})
	t.Run("bad, unknown and outside ids fail alone", func(t *testing.T) {
		q := write(t, filepath.Join(e.root, "b.txt"), "B")
		wantItem(t, e.svc.Trash([]string{q})[0], "")
		write(t, filepath.Join(dir, "info", "evil.trashinfo"), "[Trash Info]\nPath="+filepath.Join(t.TempDir(), "x")+"\n")
		write(t, filepath.Join(dir, "files", "evil"), "E")
		res := e.svc.Restore([]string{"../b.txt", "", "..", "a/b", `a\b`, "nope", "evil", "b.txt"})
		for i, code := range []string{api.CodeBadRequest, api.CodeBadRequest, api.CodeBadRequest, api.CodeBadRequest, api.CodeBadRequest, api.CodePathNotFound, authz.CodeForbidden, ""} {
			wantItem(t, res[i], code)
		}
		if res[0].Path != "../b.txt" || res[7].Path != q || read(t, q) != "B" {
			t.Errorf("results = %+v", res)
		}
		if !exists(filepath.Join(dir, "files", "evil")) {
			t.Error("an item whose destination was refused left the trash")
		}
	})
	t.Run("a payload missing from files/ is not found", func(t *testing.T) {
		write(t, filepath.Join(dir, "info", "orphan.trashinfo"), "[Trash Info]\nPath="+filepath.Join(e.root, "orphan")+"\n")
		wantItem(t, e.svc.Restore([]string{"orphan"})[0], api.CodePathNotFound)
	})
	t.Run("an item from the old agent stays restorable", func(t *testing.T) {
		write(t, filepath.Join(dir, "files", "old file.txt"), "O")
		write(t, filepath.Join(dir, "info", "old file.txt.trashinfo"),
			"[Trash Info]\nPath="+e.root+"/with%20space/old%20file.txt\nDeletionDate=2020-05-06T07:08:09\n")
		wantItem(t, e.svc.Restore([]string{"old file.txt"})[0], "")
		if read(t, filepath.Join(e.root, "with space", "old file.txt")) != "O" {
			t.Error("old-layout item not restored")
		}
	})
}

func TestEmptyTrash(t *testing.T) {
	e, dir := trashEnv(t, false)
	var ids []string
	for _, n := range []string{"a", "b", "c"} {
		p := write(t, filepath.Join(e.root, n+".txt"), n)
		wantItem(t, e.svc.Trash([]string{p})[0], "")
		ids = append(ids, n+".txt")
	}
	res, err := e.svc.EmptyTrash([]string{"a.txt", "missing", "../x", "a.txt"})
	if err != nil {
		t.Fatal(err)
	}
	for i, code := range []string{"", api.CodePathNotFound, api.CodeBadRequest, api.CodePathNotFound} {
		wantItem(t, res[i], code)
	}
	if got := strings.Join(names(t, filepath.Join(dir, "files")), ","); got != "b.txt,c.txt" {
		t.Errorf("files/ = %s", got)
	}
	if got := strings.Join(names(t, filepath.Join(dir, "info")), ","); got != "b.txt.trashinfo,c.txt.trashinfo" {
		t.Errorf("info/ = %s", got)
	}
	res, err = e.svc.EmptyTrash(nil)
	if err != nil || len(res) != 2 || !res[0].OK || !res[1].OK {
		t.Fatalf("EmptyTrash(nil) = %+v, %v", res, err)
	}
	if len(names(t, filepath.Join(dir, "files"))) != 0 || len(names(t, filepath.Join(dir, "info"))) != 0 {
		t.Error("the trash is not empty")
	}
	if res, err = e.svc.EmptyTrash(nil); err != nil || len(res) != 0 {
		t.Errorf("empty trash: %+v, %v", res, err)
	}

	t.Run("a jailed service sees and empties only its own items", func(t *testing.T) {
		in := write(t, filepath.Join(e.root, "in.txt"), "I")
		out := write(t, filepath.Join(e.other, "out.txt"), "O")
		e.svc.Trash([]string{in, out})
		jail, err := e.set.Narrow(e.root)
		if err != nil {
			t.Fatal(err)
		}
		defer jail.Close()
		js := New(jail)
		if err := js.WithTrash(dir); err != nil {
			t.Fatal(err)
		}
		defer js.Close()
		list, err := js.ListTrash()
		if err != nil || len(list) != 1 || list[0].ID != "in.txt" {
			t.Fatalf("jailed list = %+v, %v", list, err)
		}
		res, err := js.EmptyTrash([]string{"out.txt"})
		if err != nil {
			t.Fatal(err)
		}
		wantItem(t, res[0], api.CodePathNotFound)
		if res, _ = js.EmptyTrash(nil); len(res) != 1 || !res[0].OK {
			t.Errorf("jailed empty = %+v", res)
		}
		if got := strings.Join(names(t, filepath.Join(dir, "files")), ","); got != "out.txt" {
			t.Errorf("files/ = %s, want only the other device's item", got)
		}
	})
	_ = ids
}

func TestDrivesFilteredByRoots(t *testing.T) {
	e := newEnv(t, false)
	sub := filepath.Join(e.root, "mnt")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	parent := filepath.Dir(e.root)
	old := drivesFn
	drivesFn = func() ([]Drive, error) {
		return []Drive{
			{Path: parent, Label: "parent", IsOS: true}, {Path: e.root, Label: "root"},
			{Path: sub, Label: "nested"}, {Path: e.other, Label: "other"}, {Path: outside, Label: "outside"},
		}, nil
	}
	t.Cleanup(func() { drivesFn = old })

	got, err := e.svc.Drives()
	if err != nil {
		t.Fatal(err)
	}
	var labels []string
	for _, d := range got {
		labels = append(labels, d.Label)
	}
	if strings.Join(labels, ",") != "root,nested,other" {
		t.Errorf("jailed drives = %v (a volume that only contains a root, or lies elsewhere, must not be listed)", labels)
	}
	un, err := rootfs.Unrestricted(rootfs.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer un.Close()
	if all, err := New(un).Drives(); err != nil || len(all) != 5 {
		t.Errorf("unrestricted drives = %v, %v", all, err)
	}
	empty, err := rootfs.New(nil, rootfs.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer empty.Close()
	if none, err := New(empty).Drives(); err != nil || len(none) != 0 {
		t.Errorf("a service with no folders lists %v, %v", none, err)
	}
}

func TestCopyAndMoveRecreateLinks(t *testing.T) {
	t.Run("a link inside a copied folder is recreated", func(t *testing.T) {
		e := newEnv(t, false)
		write(t, filepath.Join(e.root, "t", "f"), "F")
		symlink(t, "f", filepath.Join(e.root, "t", "lnk"))
		wantItem(t, e.svc.Copy([]string{filepath.Join(e.root, "t")}, e.other, false, false)[0], "")
		target, err := os.Readlink(filepath.Join(e.other, "t", "lnk"))
		if err != nil || target != "f" || read(t, filepath.Join(e.other, "t", "lnk")) != "F" {
			t.Errorf("copied link = %q, %v", target, err)
		}
		if !exists(filepath.Join(e.root, "t", "lnk")) {
			t.Error("the source changed")
		}
	})
	t.Run("a link whose target leaves the roots fails the item and leaves nothing", func(t *testing.T) {
		e := newEnv(t, false)
		write(t, filepath.Join(e.root, "t", "f"), "F")
		symlink(t, filepath.Join("..", "..", "..", "..", "..", "..", "..", "..", "etc"), filepath.Join(e.root, "t", "up"))
		wantItem(t, e.svc.Copy([]string{filepath.Join(e.root, "t")}, e.other, false, false)[0], api.CodeNotImplemented)
		if len(names(t, e.other)) != 0 {
			t.Errorf("a partial copy remains: %v", names(t, e.other))
		}
	})
	t.Run("moving a folder with a link across folders keeps the link", func(t *testing.T) {
		e := newEnv(t, false)
		write(t, filepath.Join(e.root, "t", "f"), "F")
		symlink(t, "f", filepath.Join(e.root, "t", "lnk"))
		wantItem(t, e.svc.Move([]string{filepath.Join(e.root, "t")}, e.other, false, false)[0], "")
		if target, err := os.Readlink(filepath.Join(e.other, "t", "lnk")); err != nil || target != "f" || exists(filepath.Join(e.root, "t")) {
			t.Errorf("moved link = %q, %v", target, err)
		}
	})
	t.Run("moving a link itself across folders recreates it", func(t *testing.T) {
		e := newEnv(t, false)
		write(t, filepath.Join(e.root, "real"), "R")
		symlink(t, "real", filepath.Join(e.root, "lnk"))
		wantItem(t, e.svc.Move([]string{filepath.Join(e.root, "lnk")}, e.other, false, false)[0], "")
		if target, err := os.Readlink(filepath.Join(e.other, "lnk")); err != nil || target != "real" || exists(filepath.Join(e.root, "lnk")) {
			t.Errorf("moved link = %q, %v", target, err)
		}
		if read(t, filepath.Join(e.root, "real")) != "R" {
			t.Error("the target was touched")
		}
	})
}
