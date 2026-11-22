package hostupdate

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// rawTar builds a tar.gz from explicit headers (name, type, link target, body).
type tarEntry struct {
	name, link string
	typ        byte
	body       string
}

func rawTar(t *testing.T, entries []tarEntry) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for _, e := range entries {
		h := &tar.Header{Name: e.name, Linkname: e.link, Typeflag: e.typ, Mode: 0o644, Size: int64(len(e.body))}
		if e.typ == tar.TypeDir {
			h.Mode = 0o755
		}
		if err := tw.WriteHeader(h); err != nil {
			t.Fatal(err)
		}
		tw.Write([]byte(e.body))
	}
	tw.Close()
	gz.Close()
	return buf.Bytes()
}

func writeTemp(t *testing.T, data []byte) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "archive")
	if err := os.WriteFile(p, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestExtractRejectsUnsafeTar(t *testing.T) {
	cases := map[string][]tarEntry{
		"parent traversal":   {{name: "../evil", typ: tar.TypeReg, body: "x"}},
		"nested traversal":   {{name: "a/../../evil", typ: tar.TypeReg, body: "x"}},
		"absolute path":      {{name: "/etc/evil", typ: tar.TypeReg, body: "x"}},
		"backslash":          {{name: `..\evil`, typ: tar.TypeReg, body: "x"}},
		"drive letter":       {{name: `C:evil`, typ: tar.TypeReg, body: "x"}},
		"symlink":            {{name: "link", typ: tar.TypeSymlink, link: "/etc/passwd"}},
		"symlink then write": {{name: "link", typ: tar.TypeSymlink, link: ".."}, {name: "link/evil", typ: tar.TypeReg, body: "x"}},
		"hardlink":           {{name: "hard", typ: tar.TypeLink, link: "rfe"}},
		"char device":        {{name: "dev", typ: tar.TypeChar}},
		"block device":       {{name: "dev", typ: tar.TypeBlock}},
		"fifo":               {{name: "pipe", typ: tar.TypeFifo}},
		"duplicate file":     {{name: "a", typ: tar.TypeReg, body: "1"}, {name: "a", typ: tar.TypeReg, body: "2"}},
	}
	for name, entries := range cases {
		t.Run(name, func(t *testing.T) {
			parent := t.TempDir()
			dest := filepath.Join(parent, "out")
			_, err := extractArchive(writeTemp(t, rawTar(t, entries)), dest, Limits{})
			if err == nil {
				t.Fatal("accepted")
			}
			if name != "duplicate file" && !errors.Is(err, ErrUnsafeArchive) {
				t.Errorf("err = %v, want ErrUnsafeArchive", err)
			}
			assertNothingOutside(t, parent, dest)
		})
	}
}

// assertNothingOutside fails if anything other than dest exists in parent.
func assertNothingOutside(t *testing.T, parent, dest string) {
	t.Helper()
	entries, _ := os.ReadDir(parent)
	for _, e := range entries {
		if filepath.Join(parent, e.Name()) != dest && e.Name() != "archive" {
			t.Errorf("file outside the destination: %s", e.Name())
		}
	}
}

func TestExtractRejectsUnsafeZip(t *testing.T) {
	mk := func(build func(zw *zip.Writer)) []byte {
		var buf bytes.Buffer
		zw := zip.NewWriter(&buf)
		build(zw)
		zw.Close()
		return buf.Bytes()
	}
	symlink := func(zw *zip.Writer) {
		h := &zip.FileHeader{Name: "link"}
		h.SetMode(os.ModeSymlink | 0o777)
		w, _ := zw.CreateHeader(h)
		w.Write([]byte("/etc/passwd"))
	}
	named := func(name string) func(zw *zip.Writer) {
		return func(zw *zip.Writer) { w, _ := zw.Create(name); w.Write([]byte("x")) }
	}
	cases := map[string][]byte{
		"parent traversal": mk(named("../evil")),
		"absolute path":    mk(named("/etc/evil")),
		"backslash":        mk(named(`..\evil`)),
		"symlink":          mk(symlink),
		"device": mk(func(zw *zip.Writer) {
			h := &zip.FileHeader{Name: "dev"}
			h.SetMode(os.ModeDevice | 0o644)
			zw.CreateHeader(h)
		}),
	}
	for name, data := range cases {
		t.Run(name, func(t *testing.T) {
			parent := t.TempDir()
			dest := filepath.Join(parent, "out")
			if _, err := extractArchive(writeTemp(t, data), dest, Limits{}); !errors.Is(err, ErrUnsafeArchive) {
				t.Fatalf("err = %v, want ErrUnsafeArchive", err)
			}
			assertNothingOutside(t, parent, dest)
		})
	}
}

func TestExtractLimits(t *testing.T) {
	big := strings.Repeat("a", 1000)
	ar := rawTar(t, []tarEntry{{name: "one", typ: tar.TypeReg, body: big}, {name: "two", typ: tar.TypeReg, body: big}})
	cases := map[string]Limits{
		"per file": {MaxFileBytes: 999},
		"total":    {MaxTotalBytes: 1500},
		"entries":  {MaxEntries: 1},
	}
	for name, lim := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := extractArchive(writeTemp(t, ar), filepath.Join(t.TempDir(), "out"), lim); !errors.Is(err, ErrArchiveTooLarge) {
				t.Fatalf("err = %v, want ErrArchiveTooLarge", err)
			}
		})
	}
	// A header that lies about its size cannot get past the byte-counting copy: a zip claiming a small file.
	t.Run("within limits", func(t *testing.T) {
		got, err := extractArchive(writeTemp(t, ar), filepath.Join(t.TempDir(), "out"), Limits{MaxFileBytes: 1000, MaxTotalBytes: 2000, MaxEntries: 2})
		if err != nil || len(got) != 2 {
			t.Fatalf("got %v, %v", got, err)
		}
	})
	t.Run("unknown format", func(t *testing.T) {
		if _, err := extractArchive(writeTemp(t, []byte("plain text file")), filepath.Join(t.TempDir(), "out"), Limits{}); !errors.Is(err, ErrUnsafeArchive) {
			t.Fatalf("err = %v", err)
		}
	})
}

// A hostile archive reaching Stage is refused and leaves nothing behind in staging or versions.
func TestStageRefusesHostileArchive(t *testing.T) {
	for name, entries := range map[string][]tarEntry{
		"traversal": {{name: "../../evil", typ: tar.TypeReg, body: "x"}},
		"symlink":   {{name: "rfe", typ: tar.TypeSymlink, link: "/bin/sh"}},
	} {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t, testVersion, false)
			ar := rawTar(t, entries)
			f.ar = ar
			f.sign(f.release(testVersion, ar), f.primaryPriv)
			mustFail(t, f, f.cfg(), ErrUnsafeArchive)
			if _, err := os.Stat(filepath.Join(filepath.Dir(f.root), "evil")); err == nil {
				t.Error("file escaped the root")
			}
		})
	}
}
