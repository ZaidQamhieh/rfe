package fsops

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"os"
	"path/filepath"
	"sort"
	"testing"
)

func listTree(t *testing.T, dir string) []string {
	t.Helper()
	var out []string
	_ = filepath.Walk(dir, func(p string, _ os.FileInfo, err error) error {
		if err == nil && p != dir {
			rel, _ := filepath.Rel(dir, p)
			out = append(out, rel)
		}
		return nil
	})
	sort.Strings(out)
	return out
}

// A rejected archive leaves no partial output: entries written before the
// bad one are removed, and only directories the extraction created go.
func TestExtract_FailureRollsBackCreatedEntries(t *testing.T) {
	ops, root := setupJail(t)
	zipPath := filepath.Join(root, "slip.zip")
	f, err := os.Create(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	for _, name := range []string{"good1.txt", "dir/good2.txt", "keep/new.txt", "../evil.txt"} {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = w.Write([]byte("data"))
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	f.Close()

	// destDir already holds user content, including a folder the archive
	// also writes into.
	dest := filepath.Join(root, "dest")
	if err := os.MkdirAll(filepath.Join(dest, "keep"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dest, "keep", "mine.txt"), []byte("mine"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dest, "emptyuserdir"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := ops.Extract(zipPath, dest); err == nil {
		t.Fatal("Extract accepted an escaping entry")
	}
	got := listTree(t, dest)
	want := []string{"emptyuserdir", "keep", filepath.Join("keep", "mine.txt")}
	if len(got) != len(want) {
		t.Fatalf("after failed extract dest = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("after failed extract dest = %v, want %v", got, want)
		}
	}
	if _, err := os.Lstat(filepath.Join(root, "evil.txt")); !os.IsNotExist(err) {
		t.Fatal("escaping entry was written")
	}

	// A destination the extraction created itself is removed too.
	fresh := filepath.Join(root, "fresh", "nested")
	if _, err := ops.Extract(zipPath, fresh); err == nil {
		t.Fatal("Extract accepted an escaping entry")
	}
	if _, err := os.Lstat(filepath.Join(root, "fresh")); !os.IsNotExist(err) {
		t.Fatalf("created destination left behind: %v", listTree(t, root))
	}
}

func TestExtract_TarGzFailureRollsBack(t *testing.T) {
	ops, root := setupJail(t)
	p := filepath.Join(root, "a.tar.gz")
	f, err := os.Create(p)
	if err != nil {
		t.Fatal(err)
	}
	gz := gzip.NewWriter(f)
	tw := tar.NewWriter(gz)
	for _, name := range []string{"ok/a.txt", "../../evil.txt"} {
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o644, Size: 1, Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		_, _ = tw.Write([]byte("x"))
	}
	tw.Close()
	gz.Close()
	f.Close()
	dest := filepath.Join(root, "out")
	if _, err := ops.Extract(p, dest); err == nil {
		t.Fatal("Extract accepted an escaping entry")
	}
	if _, err := os.Lstat(dest); !os.IsNotExist(err) {
		t.Fatalf("partial output left: %v", listTree(t, dest))
	}
}

// Compress never replaces an existing entry at dest, including a dangling
// symlink (review finding R6).
func TestCompress_KeepsDanglingSymlinkAtDest(t *testing.T) {
	ops, root := setupJail(t)
	src := filepath.Join(root, "a.txt")
	if err := os.WriteFile(src, []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(root, "out.zip")
	if err := os.Symlink("nowhere.zip", dest); err != nil {
		t.Fatal(err)
	}
	e, err := ops.Compress([]string{src}, dest)
	if err != nil {
		t.Fatal(err)
	}
	if e.Path == dest {
		t.Fatal("archive was published over the existing symlink")
	}
	if target, err := os.Readlink(dest); err != nil || target != "nowhere.zip" {
		t.Fatalf("symlink at dest lost: %q, %v", target, err)
	}
}

// Unrestricted mode (no roots): rollback still removes every ancestor of
// destDir that the extraction created (review finding on the parent walk).
func TestExtract_UnjailedRollbackRemovesCreatedAncestors(t *testing.T) {
	base := t.TempDir()
	ops := New(nil, false)
	zipPath := filepath.Join(base, "slip.zip")
	f, err := os.Create(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	for _, name := range []string{"ok.txt", "../evil.txt"} {
		w, _ := zw.Create(name)
		_, _ = w.Write([]byte("x"))
	}
	zw.Close()
	f.Close()
	dest := filepath.Join(base, "new", "sub", "dest")
	if _, err := ops.Extract(zipPath, dest); err == nil {
		t.Fatal("Extract accepted an escaping entry")
	}
	if _, err := os.Lstat(filepath.Join(base, "new")); !os.IsNotExist(err) {
		t.Fatalf("created ancestor left behind: %v", listTree(t, base))
	}
}

// Rollback removes only the file the extraction created: a file written
// over that name afterwards survives (review finding on concurrent writers).
func TestExtractLog_RollbackKeepsReplacedFile(t *testing.T) {
	_, root := setupJail(t)
	p := filepath.Join(root, "f.txt")
	if err := os.WriteFile(p, []byte("ours"), 0o644); err != nil {
		t.Fatal(err)
	}
	info, err := os.Lstat(p)
	if err != nil {
		t.Fatal(err)
	}
	x := &extractLog{}
	x.recordFile(&securePath{full: p}, info)
	// A concurrent writer replaces the file (new inode) before rollback.
	tmp := p + ".new"
	if err := os.WriteFile(tmp, []byte("theirs"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(tmp, p); err != nil {
		t.Fatal(err)
	}
	x.rollback()
	if b, err := os.ReadFile(p); err != nil || string(b) != "theirs" {
		t.Fatalf("rollback removed a file it did not create: %q, %v", b, err)
	}
}

// A folder entry whose name is taken by an existing file fails the extract (as
// a plain MkdirAll does) instead of being skipped, and the file is untouched.
func TestExtract_FolderEntryOverExistingFileFails(t *testing.T) {
	ops, root := setupJail(t)
	dest := filepath.Join(root, "out")
	if err := os.MkdirAll(dest, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dest, "d"), []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}
	zp := filepath.Join(root, "a.zip")
	f, err := os.Create(zp)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	if _, err := zw.Create("d/"); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	f.Close()
	if _, err := ops.Extract(zp, dest); err == nil {
		t.Fatal("extract succeeded although a file is in the way of folder d/")
	}
	if b, err := os.ReadFile(filepath.Join(dest, "d")); err != nil || string(b) != "keep" {
		t.Fatalf("file in the way changed: %q, %v", b, err)
	}
}
