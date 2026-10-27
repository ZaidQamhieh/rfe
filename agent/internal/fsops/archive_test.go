package fsops

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// TestCompress_AndExtract_RoundTrip zips a file + a directory, then extracts
// the archive into a fresh dir and checks every entry survived intact.
func TestCompress_AndExtract_RoundTrip(t *testing.T) {
	ops, root := setupJail(t)

	// Lay out sources: a loose file and a directory with a nested file.
	loose := filepath.Join(root, "notes.txt")
	if err := os.WriteFile(loose, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(root, "data")
	if err := os.MkdirAll(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	nested := filepath.Join(dir, "sub", "deep.txt")
	if err := os.WriteFile(nested, []byte("world"), 0o644); err != nil {
		t.Fatal(err)
	}

	archive := filepath.Join(root, "bundle.zip")
	entry, err := ops.Compress([]string{loose, dir}, archive)
	if err != nil {
		t.Fatalf("Compress: %v", err)
	}
	if entry.Path != archive {
		t.Fatalf("expected archive at %s, got %s", archive, entry.Path)
	}
	if _, err := os.Stat(archive); err != nil {
		t.Fatalf("archive not created: %v", err)
	}

	out := filepath.Join(root, "out")
	if _, err := ops.Extract(archive, out); err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if got, _ := os.ReadFile(filepath.Join(out, "notes.txt")); string(got) != "hello" {
		t.Fatalf("notes.txt = %q, want hello", got)
	}
	if got, _ := os.ReadFile(filepath.Join(out, "data", "sub", "deep.txt")); string(got) != "world" {
		t.Fatalf("deep.txt = %q, want world", got)
	}
}

// TestCompress_AutoRenamesOnConflict verifies an existing destination is
// auto-renamed rather than clobbered.
func TestCompress_AutoRenamesOnConflict(t *testing.T) {
	ops, root := setupJail(t)
	src := filepath.Join(root, "a.txt")
	if err := os.WriteFile(src, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	archive := filepath.Join(root, "z.zip")
	if err := os.WriteFile(archive, []byte("preexisting"), 0o644); err != nil {
		t.Fatal(err)
	}
	entry, err := ops.Compress([]string{src}, archive)
	if err != nil {
		t.Fatalf("Compress: %v", err)
	}
	if entry.Path == archive {
		t.Fatalf("expected auto-rename, got original path %s", entry.Path)
	}
	if got, _ := os.ReadFile(archive); string(got) != "preexisting" {
		t.Fatalf("original archive was clobbered: %q", got)
	}
}

// TestCompress_ReadOnly verifies compression is blocked in read-only mode.
func TestCompress_ReadOnly(t *testing.T) {
	root := t.TempDir()
	ops := New([]string{root}, true) // read-only
	src := filepath.Join(root, "a.txt")
	if err := os.WriteFile(src, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := ops.Compress([]string{src}, filepath.Join(root, "z.zip")); !errors.Is(err, ErrReadOnly) {
		t.Fatalf("expected ErrReadOnly, got %v", err)
	}
}

// TestCompress_SourceOutsideJail verifies a source outside the jail is rejected.
func TestCompress_SourceOutsideJail(t *testing.T) {
	ops, root := setupJail(t)
	outside := filepath.Join(t.TempDir(), "secret.txt")
	if err := os.WriteFile(outside, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := ops.Compress([]string{outside}, filepath.Join(root, "z.zip")); !errors.Is(err, ErrForbidden) {
		t.Fatalf("expected ErrForbidden, got %v", err)
	}
}

// TestExtract_ZipSlipBlocked crafts a malicious zip whose entry name climbs
// out of the destination ("../escaped.txt") and verifies it is rejected and
// nothing is written outside destDir.
func TestExtract_ZipSlipBlocked(t *testing.T) {
	ops, root := setupJail(t)

	archive := filepath.Join(root, "evil.zip")
	f, err := os.Create(archive)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	w, err := zw.Create("../escaped.txt")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte("pwned")); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	f.Close()

	dest := filepath.Join(root, "unpack")
	_, err = ops.Extract(archive, dest)
	if !errors.Is(err, ErrForbidden) {
		t.Fatalf("expected ErrForbidden (zip-slip), got %v", err)
	}
	// The escaped file must not exist at the climbed-to location.
	if _, statErr := os.Stat(filepath.Join(root, "escaped.txt")); !os.IsNotExist(statErr) {
		t.Fatalf("zip-slip wrote a file outside destDir")
	}
}

// TestExtract_TarGzRoundTrip builds a .tar.gz in-memory-to-disk and extracts it.
func TestExtract_TarGzRoundTrip(t *testing.T) {
	ops, root := setupJail(t)

	archive := filepath.Join(root, "bundle.tar.gz")
	f, err := os.Create(archive)
	if err != nil {
		t.Fatal(err)
	}
	gz := gzip.NewWriter(f)
	tw := tar.NewWriter(gz)
	body := []byte("tar-content")
	if err := tw.WriteHeader(&tar.Header{
		Name:     "dir/file.txt",
		Mode:     0o644,
		Size:     int64(len(body)),
		Typeflag: tar.TypeReg,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(body); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	f.Close()

	dest := filepath.Join(root, "out")
	if _, err := ops.Extract(archive, dest); err != nil {
		t.Fatalf("Extract tar.gz: %v", err)
	}
	if got, _ := os.ReadFile(filepath.Join(dest, "dir", "file.txt")); string(got) != "tar-content" {
		t.Fatalf("file.txt = %q, want tar-content", got)
	}
}

// TestExtract_UnsupportedFormat verifies an unknown extension is rejected.
func TestExtract_UnsupportedFormat(t *testing.T) {
	ops, root := setupJail(t)
	archive := filepath.Join(root, "thing.rar")
	if err := os.WriteFile(archive, []byte("not really a rar"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := ops.Extract(archive, filepath.Join(root, "out")); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("expected ErrUnsupported, got %v", err)
	}
}

// TestExtract_MissingArchive verifies a non-existent archive yields ErrNotFound.
func TestExtract_MissingArchive(t *testing.T) {
	ops, root := setupJail(t)
	if _, err := ops.Extract(filepath.Join(root, "nope.zip"), filepath.Join(root, "out")); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

// TestExtract_SymlinkParentBlocked is the PR-06 regression: entry names alone
// are checked lexically, so an entirely innocent-looking "sub/f.txt" used to be
// written straight through a symlink already sitting in the destination —
// clobbering a file outside the jail without any "../" ever appearing.
func TestExtract_SymlinkParentBlocked(t *testing.T) {
	ops, root := setupJail(t)

	outsideDir := t.TempDir()
	victim := filepath.Join(outsideDir, "f.txt")
	if err := os.WriteFile(victim, []byte("ORIGINAL"), 0o600); err != nil {
		t.Fatal(err)
	}

	archive := filepath.Join(root, "innocent.zip")
	f, err := os.Create(archive)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	w, err := zw.Create("sub/f.txt") // no traversal in the name at all
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte("pwned")); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	f.Close()

	// The destination already contains sub -> /outside.
	dest := filepath.Join(root, "unpack")
	if err := os.MkdirAll(dest, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outsideDir, filepath.Join(dest, "sub")); err != nil {
		t.Fatal(err)
	}

	if _, err := ops.Extract(archive, dest); !errors.Is(err, ErrForbidden) {
		t.Fatalf("expected ErrForbidden (symlink parent), got %v", err)
	}
	b, err := os.ReadFile(victim)
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != "ORIGINAL" {
		t.Fatalf("extraction wrote through a symlink parent, clobbering a file outside the jail: %q", b)
	}
}
