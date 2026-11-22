package fsops

// Spec tests (QA2) for archive extraction: FR-16 to FR-19 and the unsupported-format
// alternate flow of UC-EXTRACT. Archives are built in t.TempDir() fixtures.

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// qa2specEntry is one archive member. typeflag and linkname apply to tar only;
// a zero typeflag means a regular file.
type qa2specEntry struct {
	name     string
	body     string
	typeflag byte
	linkname string
}

// qa2specZipArchive writes a zip archive at path holding entries.
func qa2specZipArchive(t *testing.T, path string, entries []qa2specEntry) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create %s: %v", path, err)
	}
	zw := zip.NewWriter(f)
	for _, e := range entries {
		w, err := zw.Create(e.name)
		if err != nil {
			t.Fatalf("zip entry %q: %v", e.name, err)
		}
		if _, err := io.WriteString(w, e.body); err != nil {
			t.Fatalf("zip body %q: %v", e.name, err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("close zip: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("close %s: %v", path, err)
	}
}

// qa2specTarGzArchive writes a gzip-compressed tar archive at path holding entries.
func qa2specTarGzArchive(t *testing.T, path string, entries []qa2specEntry) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create %s: %v", path, err)
	}
	gz := gzip.NewWriter(f)
	tw := tar.NewWriter(gz)
	for _, e := range entries {
		hdr := &tar.Header{Name: e.name, Mode: 0o644, Typeflag: e.typeflag, Linkname: e.linkname}
		if hdr.Typeflag == 0 {
			hdr.Typeflag = tar.TypeReg
		}
		if hdr.Typeflag == tar.TypeReg {
			hdr.Size = int64(len(e.body))
		}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatalf("tar header %q: %v", e.name, err)
		}
		if hdr.Typeflag == tar.TypeReg {
			if _, err := io.WriteString(tw, e.body); err != nil {
				t.Fatalf("tar body %q: %v", e.name, err)
			}
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("close tar: %v", err)
	}
	if err := gz.Close(); err != nil {
		t.Fatalf("close gzip: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("close %s: %v", path, err)
	}
}

// qa2specNoEscape fails when a file named name exists under root but outside dest.
func qa2specNoEscape(t *testing.T, root, dest, name string) {
	t.Helper()
	var hits []string
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.Name() == name && !d.IsDir() {
			hits = append(hits, p)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}
	for _, h := range hits {
		rel, err := filepath.Rel(dest, h)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			t.Errorf("file %s written outside destDir %s", h, dest)
		}
	}
}

// TestQA2SpecFR16_ExtractFormatsCreateDestination checks each supported format.
func TestQA2SpecFR16_ExtractFormatsCreateDestination(t *testing.T) {
	// FR-16: "Given a zip containing one file and an absent destDir, when POST /fs/extract, then destDir exists, contains that file, and the response is 200 with the Entry of destDir."
	// The same holds for tar.gz and tgz (FR-16 names all three formats).
	// catches: a format is refused, destDir is not created, or the returned Entry is not destDir's.
	cases := []struct {
		name    string
		archive string
		write   func(*testing.T, string, []qa2specEntry)
	}{
		{"zip", "in.zip", qa2specZipArchive},
		{"tar.gz", "in.tar.gz", qa2specTarGzArchive},
		{"tgz", "in.tgz", qa2specTarGzArchive},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ops, root := setupJail(t)
			archive := filepath.Join(root, c.archive)
			c.write(t, archive, []qa2specEntry{{name: "hello.txt", body: "hi there"}})
			dest := filepath.Join(root, "out")

			e, err := ops.Extract(archive, dest)
			if err != nil {
				t.Fatalf("Extract %s: %v", c.archive, err)
			}
			if e == nil || !e.IsDir || e.Path != dest {
				t.Errorf("Extract returned %+v, want the Entry of %s", e, dest)
			}
			if got := qa2specRead(t, filepath.Join(dest, "hello.txt")); got != "hi there" {
				t.Errorf("extracted hello.txt = %q, want %q", got, "hi there")
			}
		})
	}
}

// TestQA2SpecFR17_ZipEscapeNotWrittenOutsideDest checks a zip-slip entry.
func TestQA2SpecFR17_ZipEscapeNotWrittenOutsideDest(t *testing.T) {
	// FR-17: "Given a zip with an entry named ../escape.txt, when POST /fs/extract, then no file named escape.txt is created outside destDir (the response for this case: open question 13)."
	// The response is not asserted (open question 13); only the file effect is.
	// catches: an entry resolves outside destDir and is written there.
	ops, root := setupJail(t)
	archive := filepath.Join(root, "evil.zip")
	qa2specZipArchive(t, archive, []qa2specEntry{
		{name: "../escape.txt", body: "escaped"},
		{name: "ok.txt", body: "fine"},
	})
	dest := filepath.Join(root, "dest")

	if _, err := ops.Extract(archive, dest); err != nil {
		t.Logf("Extract returned an error (response is open question 13): %v", err)
	}
	qa2specNoEscape(t, root, dest, "escape.txt")
}

// TestQA2SpecFR17_TarEscapeNotWrittenOutsideDest checks a tar.gz entry that escapes destDir.
func TestQA2SpecFR17_TarEscapeNotWrittenOutsideDest(t *testing.T) {
	// FR-17: "Extract must not write any file outside destDir." Tar form of the same entry.
	// catches: a tar entry resolves outside destDir and is written there.
	ops, root := setupJail(t)
	archive := filepath.Join(root, "evil.tgz")
	qa2specTarGzArchive(t, archive, []qa2specEntry{
		{name: "../escape.txt", body: "escaped"},
		{name: "ok.txt", body: "fine"},
	})
	dest := filepath.Join(root, "dest")

	if _, err := ops.Extract(archive, dest); err != nil {
		t.Logf("Extract returned an error (response is open question 13): %v", err)
	}
	qa2specNoEscape(t, root, dest, "escape.txt")
}

// TestQA2SpecFR17_AbsoluteEntryNotWrittenOutsideDest checks an absolute entry name.
func TestQA2SpecFR17_AbsoluteEntryNotWrittenOutsideDest(t *testing.T) {
	// FR-17: "Extract must not write any file outside destDir." An entry named by an absolute path must not be written at that path.
	// The entry may be refused or placed inside destDir; either meets FR-17.
	// catches: the entry is written at its absolute location outside destDir.
	ops, root := setupJail(t)
	outsideDir := t.TempDir()
	target := filepath.Join(outsideDir, "abs-escape.txt")
	archive := filepath.Join(root, "abs.zip")
	qa2specZipArchive(t, archive, []qa2specEntry{{name: filepath.ToSlash(target), body: "absolute"}})
	dest := filepath.Join(root, "dest")

	if _, err := ops.Extract(archive, dest); err != nil {
		t.Logf("Extract returned an error (response is open question 13): %v", err)
	}
	if qa2specExists(target) {
		t.Errorf("archive entry was written at %s, outside destDir %s", target, dest)
	}
}

// TestQA2SpecFR19_ExtractJailChecksBothPaths checks that the archive and destDir are both jail-checked.
func TestQA2SpecFR19_ExtractJailChecksBothPaths(t *testing.T) {
	// FR-19: "Given an archive path outside the device's jail, when POST /fs/extract, then the request fails with code FORBIDDEN and nothing is written."
	// The contract (openapi-HEAD.yaml:958) also says both paths are jail-checked, so a destDir outside the jail is covered too.
	// The modify grant is server-side and not tested here.
	// catches: an archive is read from outside the jail, or a destDir outside the jail is written.
	root := t.TempDir()
	jail := filepath.Join(root, "jail")
	outsideDir := filepath.Join(root, "outside")
	if err := os.MkdirAll(jail, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(outsideDir, 0o755); err != nil {
		t.Fatal(err)
	}
	ops := New([]string{root}, false).Jailed(jail)

	outsideArchive := filepath.Join(outsideDir, "a.zip")
	qa2specZipArchive(t, outsideArchive, []qa2specEntry{{name: "hello.txt", body: "hi"}})
	dest1 := filepath.Join(jail, "from-outside")
	if _, err := ops.Extract(outsideArchive, dest1); !errors.Is(err, ErrForbidden) {
		t.Errorf("Extract of archive outside jail: err = %v, want ErrForbidden", err)
	}
	if qa2specExists(dest1) {
		t.Errorf("destination %s created for a refused archive", dest1)
	}

	insideArchive := filepath.Join(jail, "a.zip")
	qa2specZipArchive(t, insideArchive, []qa2specEntry{{name: "hello.txt", body: "hi"}})
	dest2 := filepath.Join(outsideDir, "dest")
	if _, err := ops.Extract(insideArchive, dest2); !errors.Is(err, ErrForbidden) {
		t.Errorf("Extract into destDir outside jail: err = %v, want ErrForbidden", err)
	}
	if qa2specExists(dest2) {
		t.Errorf("destination %s written outside the jail", dest2)
	}

	// Boundary: both paths inside the jail succeed.
	dest3 := filepath.Join(jail, "ok")
	if _, err := ops.Extract(insideArchive, dest3); err != nil {
		t.Fatalf("Extract with both paths inside jail: %v", err)
	}
	if got := qa2specRead(t, filepath.Join(dest3, "hello.txt")); got != "hi" {
		t.Errorf("extracted hello.txt = %q, want %q", got, "hi")
	}
}

// TestQA2SpecUCExtract_UnsupportedFormatRefused checks the unsupported-format alternate flow.
func TestQA2SpecUCExtract_UnsupportedFormatRefused(t *testing.T) {
	// UC-EXTRACT alt: "unsupported format -> 400 with a code missing from the enum (see Risks)". The HTTP status and the code are documented risks and are not asserted here; the package refusal is.
	// catches: an unknown archive format is accepted as a zip or tar.
	ops, root := setupJail(t)
	archive := filepath.Join(root, "bundle.rar")
	qa2specWrite(t, archive, "not really an archive")

	if _, err := ops.Extract(archive, filepath.Join(root, "out")); !errors.Is(err, ErrUnsupported) {
		t.Errorf("Extract of .rar: err = %v, want ErrUnsupported", err)
	}
}
