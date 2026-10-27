package fsops

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

// setupJail creates a temp directory as the jail root and returns
// an Ops restricted to it, plus a cleanup function.
func setupJail(t *testing.T) (*Ops, string) {
	t.Helper()
	root := t.TempDir()
	ops := New([]string{root}, false)
	return ops, root
}

// TestResolve_AllowsInsideJail verifies a normal path inside the jail is accepted.
func TestResolve_AllowsInsideJail(t *testing.T) {
	ops, root := setupJail(t)
	path := filepath.Join(root, "subdir", "file.txt")
	got, err := ops.Resolve(path)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	// Clean path should be returned.
	if filepath.Clean(got) != filepath.Clean(path) {
		t.Fatalf("expected %s, got %s", path, got)
	}
}

// TestResolve_BlocksTraversal verifies ../ escapes are rejected.
func TestResolve_BlocksTraversal(t *testing.T) {
	ops, root := setupJail(t)
	// Construct a path that looks inside but traverses out.
	evil := filepath.Join(root, "subdir", "..", "..", "etc", "passwd")
	_, err := ops.Resolve(evil)
	if err == nil {
		t.Fatal("expected error for traversal path, got nil")
	}
}

// TestResolve_BlocksSymlinkEscape verifies a symlink pointing outside the
// jail is rejected.
func TestResolve_BlocksSymlinkEscape(t *testing.T) {
	ops, root := setupJail(t)

	// Create a target outside the jail.
	outside := t.TempDir()

	// Create a symlink inside the jail pointing to outside.
	link := filepath.Join(root, "escape-link")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	// Attempting to resolve through the symlink should be rejected.
	_, err := ops.Resolve(link)
	if err == nil {
		t.Fatal("expected error for symlink escape, got nil")
	}
}

// TestResolve_BlocksSymlinkEscapeForNewPath verifies that a not-yet-existing
// path whose parent is a symlink pointing outside the jail is rejected, even
// though the full path itself doesn't exist (so EvalSymlinks alone can't
// catch it).
func TestResolve_BlocksSymlinkEscapeForNewPath(t *testing.T) {
	ops, root := setupJail(t)

	// Create a target outside the jail.
	outside := t.TempDir()

	// Create a symlink inside the jail pointing to outside.
	link := filepath.Join(root, "escape-link")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	// "newfile" doesn't exist yet, but its parent (escape-link) resolves
	// outside the jail — must still be rejected.
	_, err := ops.Resolve(filepath.Join(link, "newfile"))
	if err == nil {
		t.Fatal("expected error for symlink escape via non-existent path, got nil")
	}
}

// TestResolve_RelativePathRejected verifies relative paths are always rejected.
func TestResolve_RelativePathRejected(t *testing.T) {
	ops, _ := setupJail(t)
	_, err := ops.Resolve("relative/path")
	if err == nil {
		t.Fatal("expected error for relative path, got nil")
	}
}

// TestResolve_NoJailAllowsAnything verifies that an empty allowedRoots
// permits any absolute path. The path is absolute on the host OS ("/tmp" is
// not absolute on Windows, where it needs a volume).
func TestResolve_NoJailAllowsAnything(t *testing.T) {
	ops := New(nil, false)
	abs := t.TempDir()
	if _, err := ops.Resolve(abs); err != nil {
		t.Fatalf("expected no error for %s with no jail, got: %v", abs, err)
	}
	if _, err := ops.Resolve("relative/path"); err == nil {
		t.Fatal("a relative path must still be refused with no jail")
	}
}

// TestCreateAndDelete exercises CreateFolder, CreateFile, ListDir, and Delete.
func TestCreateAndDelete(t *testing.T) {
	ops, root := setupJail(t)

	// Create a folder.
	dirPath := filepath.Join(root, "mydir")
	entry, err := ops.CreateFolder(dirPath)
	if err != nil {
		t.Fatalf("CreateFolder: %v", err)
	}
	if !entry.IsDir {
		t.Fatal("expected isDir=true")
	}

	// Create a file inside.
	filePath := filepath.Join(root, "mydir", "hello.txt")
	fileEntry, err := ops.CreateFile(filePath)
	if err != nil {
		t.Fatalf("CreateFile: %v", err)
	}
	if fileEntry.IsDir {
		t.Fatal("expected isDir=false")
	}

	// List the directory.
	listing, err := ops.ListDir(dirPath, "", 100)
	if err != nil {
		t.Fatalf("ListDir: %v", err)
	}
	if len(listing.Entries) != 1 || listing.Entries[0].Name != "hello.txt" {
		t.Fatalf("unexpected listing: %+v", listing.Entries)
	}

	// Delete the file.
	results := ops.Delete([]string{filePath})
	if len(results) != 1 || !results[0].OK {
		t.Fatalf("Delete failed: %+v", results)
	}
}

// TestCreateFile_BlocksSymlinkEscapeViaParent verifies that creating a file
// whose parent directory is a symlink escaping the jail is rejected, even
// though the file itself doesn't exist yet.
func TestCreateFile_BlocksSymlinkEscapeViaParent(t *testing.T) {
	ops, root := setupJail(t)

	// Create a target outside the jail.
	outside := t.TempDir()

	// Create a symlink inside the jail pointing to outside.
	link := filepath.Join(root, "escape-link")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	// Creating "escape-link/newfile" must be rejected, and must not create
	// anything outside the jail.
	target := filepath.Join(link, "newfile")
	if _, err := ops.CreateFile(target); err == nil {
		t.Fatal("expected error creating file through symlinked parent, got nil")
	}
	if _, err := os.Stat(filepath.Join(outside, "newfile")); !os.IsNotExist(err) {
		t.Fatalf("expected no file created outside the jail, stat err: %v", err)
	}
}

// TestRename_BlocksSymlinkEscapeViaDestParent verifies that renaming into a
// destination whose parent directory is a symlink escaping the jail is
// rejected, even though the destination itself doesn't exist yet.
func TestRename_BlocksSymlinkEscapeViaDestParent(t *testing.T) {
	ops, root := setupJail(t)

	// Create a target outside the jail.
	outside := t.TempDir()

	// Create a symlink inside the jail pointing to outside.
	link := filepath.Join(root, "escape-link")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	// A legitimate source file inside the jail.
	src := filepath.Join(root, "source.txt")
	if _, err := ops.CreateFile(src); err != nil {
		t.Fatalf("CreateFile(src): %v", err)
	}

	// Renaming into "escape-link/moved.txt" must be rejected, and must not
	// move the file outside the jail.
	dst := filepath.Join(link, "moved.txt")
	if _, err := ops.Rename(src, dst); err == nil {
		t.Fatal("expected error renaming into symlinked parent, got nil")
	}
	if _, err := os.Stat(src); err != nil {
		t.Fatalf("expected source to remain in place, stat err: %v", err)
	}
	if _, err := os.Stat(filepath.Join(outside, "moved.txt")); !os.IsNotExist(err) {
		t.Fatalf("expected no file created outside the jail, stat err: %v", err)
	}
}

// TestRename_WithinJailWorks verifies a legitimate rename to a new path
// inside the jail (including a not-yet-existing destination directory)
// still succeeds.
func TestRename_WithinJailWorks(t *testing.T) {
	ops, root := setupJail(t)

	src := filepath.Join(root, "source.txt")
	if _, err := ops.CreateFile(src); err != nil {
		t.Fatalf("CreateFile(src): %v", err)
	}

	// Destination lives in a subdirectory that doesn't exist yet.
	dst := filepath.Join(root, "subdir", "renamed.txt")
	entry, err := ops.Rename(src, dst)
	if err != nil {
		t.Fatalf("Rename: %v", err)
	}
	if entry.Name != "renamed.txt" {
		t.Fatalf("unexpected entry name: %s", entry.Name)
	}
	if _, err := os.Stat(dst); err != nil {
		t.Fatalf("expected destination to exist, stat err: %v", err)
	}
	if _, err := os.Stat(src); !os.IsNotExist(err) {
		t.Fatalf("expected source to be gone, stat err: %v", err)
	}
}

// TestRename_ExistingDestinationConflicts verifies a rename never replaces
// an existing file or folder.
func TestRename_ExistingDestinationConflicts(t *testing.T) {
	ops, root := setupJail(t)

	src := filepath.Join(root, "a.txt")
	dst := filepath.Join(root, "b.txt")
	if err := os.WriteFile(src, []byte("source"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dst, []byte("keep me"), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := ops.Rename(src, dst); !errors.Is(err, ErrConflict) {
		t.Fatalf("expected ErrConflict, got %v", err)
	}
	if got, err := os.ReadFile(dst); err != nil || string(got) != "keep me" {
		t.Fatalf("destination was changed: %q (err %v)", got, err)
	}
	if got, err := os.ReadFile(src); err != nil || string(got) != "source" {
		t.Fatalf("source was changed: %q (err %v)", got, err)
	}

	// An existing folder is a conflict too.
	dir := filepath.Join(root, "dir")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := ops.Rename(src, dir); !errors.Is(err, ErrConflict) {
		t.Fatalf("expected ErrConflict for a folder destination, got %v", err)
	}
}

// TestRename_ToSelfIsNoConflict keeps renaming a path to itself harmless.
func TestRename_ToSelfIsNoConflict(t *testing.T) {
	ops, root := setupJail(t)
	p := filepath.Join(root, "a.txt")
	if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := ops.Rename(p, p); err != nil {
		t.Fatalf("rename to self: %v", err)
	}
	if _, err := os.Stat(p); err != nil {
		t.Fatalf("file gone after rename to self: %v", err)
	}
}

func TestRename_MissingSourceIsNotAConflict(t *testing.T) {
	ops, root := setupJail(t)
	dst := filepath.Join(root, "exists.txt")
	if err := os.WriteFile(dst, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := ops.Rename(filepath.Join(root, "gone.txt"), dst)
	if err == nil || errors.Is(err, ErrConflict) || !os.IsNotExist(err) && !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("want a not-found error, got %v", err)
	}
}

// TestReadOnly verifies that write ops are rejected when readOnly=true.
func TestReadOnly(t *testing.T) {
	root := t.TempDir()
	ops := New([]string{root}, true)

	_, err := ops.CreateFolder(filepath.Join(root, "nope"))
	if err == nil {
		t.Fatal("expected read-only error")
	}

	_, err = ops.CreateFile(filepath.Join(root, "nope.txt"))
	if err == nil {
		t.Fatal("expected read-only error")
	}

	results := ops.Delete([]string{filepath.Join(root, "x")})
	if results[0].OK || results[0].Error == nil {
		t.Fatal("expected read-only batch error")
	}
}

// TestReadOnlyView verifies Ops.ReadOnly() rejects writes on an otherwise
// writable Ops while still allowing reads (per-device read-only, #8).
func TestReadOnlyView(t *testing.T) {
	root := t.TempDir()
	rw := New([]string{root}, false)

	// A folder created via the writable ops...
	if _, err := rw.CreateFolder(filepath.Join(root, "sub")); err != nil {
		t.Fatalf("setup CreateFolder: %v", err)
	}

	ro := rw.ReadOnly()

	// Reads still work through the read-only view.
	if _, err := ro.ListDir(root, "", 100); err != nil {
		t.Fatalf("ListDir should work read-only: %v", err)
	}

	// Writes are rejected.
	if _, err := ro.CreateFolder(filepath.Join(root, "nope")); !errors.Is(err, ErrReadOnly) {
		t.Fatalf("CreateFolder: expected ErrReadOnly, got %v", err)
	}
	if _, err := ro.CreateFile(filepath.Join(root, "nope.txt")); !errors.Is(err, ErrReadOnly) {
		t.Fatalf("CreateFile: expected ErrReadOnly, got %v", err)
	}

	// The original writable ops is unaffected by deriving a read-only view.
	if _, err := rw.CreateFolder(filepath.Join(root, "sub2")); err != nil {
		t.Fatalf("original ops should still be writable: %v", err)
	}
}

// TestListDir_Pagination verifies cursor-based pagination.
func TestListDir_Pagination(t *testing.T) {
	ops, root := setupJail(t)

	// Create 5 files: a.txt through e.txt.
	for _, name := range []string{"a.txt", "b.txt", "c.txt", "d.txt", "e.txt"} {
		f, err := os.Create(filepath.Join(root, name))
		if err != nil {
			t.Fatalf("create %s: %v", name, err)
		}
		f.Close()
	}

	// First page: limit 2.
	listing, err := ops.ListDir(root, "", 2)
	if err != nil {
		t.Fatalf("ListDir page 1: %v", err)
	}
	if len(listing.Entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(listing.Entries))
	}
	if listing.NextCursor == nil {
		t.Fatal("expected nextCursor to be set")
	}

	// Second page.
	listing2, err := ops.ListDir(root, *listing.NextCursor, 2)
	if err != nil {
		t.Fatalf("ListDir page 2: %v", err)
	}
	if len(listing2.Entries) != 2 {
		t.Fatalf("expected 2 entries on page 2, got %d", len(listing2.Entries))
	}

	// Third (last) page.
	listing3, err := ops.ListDir(root, *listing2.NextCursor, 2)
	if err != nil {
		t.Fatalf("ListDir page 3: %v", err)
	}
	if len(listing3.Entries) != 1 {
		t.Fatalf("expected 1 entry on last page, got %d", len(listing3.Entries))
	}
	if listing3.NextCursor != nil {
		t.Fatal("expected nextCursor to be nil on last page")
	}
}

// fakeSettings is a mutable SettingsView for testing live config.
type fakeSettings struct {
	ro    bool
	roots []string
}

func (f *fakeSettings) IsReadOnly() bool { return f.ro }
func (f *fakeSettings) Roots() []string  { return f.roots }

// noTempFilesLeft verifies the jail root contains only the expected file
// (i.e. no leftover .rfe-tmp-* temp files from an atomic write).
func noTempFilesLeft(t *testing.T, dir string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	for _, e := range entries {
		if filepath.Ext(e.Name()) != "" && filepath.Base(e.Name())[0] == '.' {
			t.Fatalf("leftover temp file found: %s", e.Name())
		}
	}
}

// TestWriteContent_Create verifies writing a brand-new file works and the
// returned Entry reflects the new content, with no leftover temp files.
func TestWriteContent_Create(t *testing.T) {
	ops, root := setupJail(t)

	target := filepath.Join(root, "note.txt")
	entry, err := ops.WriteContent(target, []byte("hello world"), nil)
	if err != nil {
		t.Fatalf("WriteContent: %v", err)
	}
	if entry.Size != int64(len("hello world")) {
		t.Fatalf("unexpected size: %d", entry.Size)
	}
	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(got) != "hello world" {
		t.Fatalf("unexpected content: %q", got)
	}
	noTempFilesLeft(t, root)
}

// TestWriteContent_Overwrite verifies overwriting an existing file replaces
// its content, preserves its mode, and returns the fresh Entry.
func TestWriteContent_Overwrite(t *testing.T) {
	ops, root := setupJail(t)

	target := filepath.Join(root, "note.txt")
	if err := os.WriteFile(target, []byte("old content"), 0o640); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	entry, err := ops.WriteContent(target, []byte("new content!"), nil)
	if err != nil {
		t.Fatalf("WriteContent: %v", err)
	}
	if entry.Size != int64(len("new content!")) {
		t.Fatalf("unexpected size: %d", entry.Size)
	}

	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(got) != "new content!" {
		t.Fatalf("unexpected content: %q", got)
	}

	info, err := os.Stat(target)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	// Windows has no POSIX permission bits: Go reports 0666, or 0444 for the
	// read-only attribute, so the check there is that the file stays writable.
	if runtime.GOOS == "windows" {
		if info.Mode().Perm()&0o200 == 0 {
			t.Fatalf("file became read-only after overwrite: %v", info.Mode().Perm())
		}
	} else if info.Mode().Perm() != 0o640 {
		t.Fatalf("expected mode 0640 preserved, got %v", info.Mode().Perm())
	}
	noTempFilesLeft(t, root)
}

// TestWriteContent_ReadOnly verifies ErrReadOnly is returned in read-only mode.
func TestWriteContent_ReadOnly(t *testing.T) {
	root := t.TempDir()
	ops := New([]string{root}, true)

	_, err := ops.WriteContent(filepath.Join(root, "note.txt"), []byte("x"), nil)
	if err != ErrReadOnly {
		t.Fatalf("expected ErrReadOnly, got %v", err)
	}
}

// TestWriteContent_OutsideJail verifies ErrForbidden is returned for a path
// outside the configured jail.
func TestWriteContent_OutsideJail(t *testing.T) {
	ops, _ := setupJail(t)
	outside := t.TempDir()

	_, err := ops.WriteContent(filepath.Join(outside, "note.txt"), []byte("x"), nil)
	if err == nil {
		t.Fatal("expected error for path outside jail, got nil")
	}
	if !errors.Is(err, ErrForbidden) {
		t.Fatalf("expected ErrForbidden, got %v", err)
	}
}

// TestWriteContent_BaseModifiedMismatch verifies a stale baseModified is
// rejected with ErrStale and does not modify the file on disk.
func TestWriteContent_BaseModifiedMismatch(t *testing.T) {
	ops, root := setupJail(t)

	target := filepath.Join(root, "note.txt")
	if err := os.WriteFile(target, []byte("original"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	stale := time.Now().Add(-1 * time.Hour)
	_, err := ops.WriteContent(target, []byte("clobber"), &stale)
	if !errors.Is(err, ErrStale) {
		t.Fatalf("expected ErrStale, got %v", err)
	}

	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(got) != "original" {
		t.Fatalf("file should be unchanged, got %q", got)
	}
	noTempFilesLeft(t, root)
}

// TestWriteContent_BaseModifiedMatch verifies a matching baseModified
// (truncated to the second) allows the write to proceed.
func TestWriteContent_BaseModifiedMatch(t *testing.T) {
	ops, root := setupJail(t)

	target := filepath.Join(root, "note.txt")
	if err := os.WriteFile(target, []byte("original"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	info, err := os.Stat(target)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	base := info.ModTime()

	entry, err := ops.WriteContent(target, []byte("updated"), &base)
	if err != nil {
		t.Fatalf("WriteContent with matching baseModified: %v", err)
	}
	if entry.Size != int64(len("updated")) {
		t.Fatalf("unexpected size: %d", entry.Size)
	}

	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(got) != "updated" {
		t.Fatalf("unexpected content: %q", got)
	}
	noTempFilesLeft(t, root)
}

// TestWriteContent_BaseModifiedNotFound verifies that supplying baseModified
// for a file that doesn't exist yet returns ErrNotFound.
func TestWriteContent_BaseModifiedNotFound(t *testing.T) {
	ops, root := setupJail(t)

	target := filepath.Join(root, "missing.txt")
	base := time.Now()
	_, err := ops.WriteContent(target, []byte("x"), &base)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestOps_LiveReadOnlyToggle(t *testing.T) {
	root := t.TempDir()
	fs := &fakeSettings{ro: false, roots: []string{root}}
	ops := NewWithSettings(fs)

	if _, err := ops.CreateFolder(filepath.Join(root, "ok")); err != nil {
		t.Fatalf("write should succeed when not read-only: %v", err)
	}
	// Flip read-only live — no reconstruction of ops.
	fs.ro = true
	if _, err := ops.CreateFolder(filepath.Join(root, "blocked")); err == nil {
		t.Fatal("expected write to be rejected after read-only toggled on")
	}
}

// --------- Copy/Move overwrite ---------

// TestCopy_OverwriteReplacesExistingFile verifies that Copy with
// overwrite=true replaces an existing destination file with the source's
// content.
func TestCopy_OverwriteReplacesExistingFile(t *testing.T) {
	ops, root := setupJail(t)

	srcDir := filepath.Join(root, "src")
	dstDir := filepath.Join(root, "dst")
	if err := os.MkdirAll(srcDir, 0o755); err != nil {
		t.Fatalf("MkdirAll src: %v", err)
	}
	if err := os.MkdirAll(dstDir, 0o755); err != nil {
		t.Fatalf("MkdirAll dst: %v", err)
	}

	srcFile := filepath.Join(srcDir, "note.txt")
	if err := os.WriteFile(srcFile, []byte("new content"), 0o644); err != nil {
		t.Fatalf("WriteFile src: %v", err)
	}
	dstFile := filepath.Join(dstDir, "note.txt")
	if err := os.WriteFile(dstFile, []byte("old content"), 0o644); err != nil {
		t.Fatalf("WriteFile dst: %v", err)
	}

	results := ops.Copy([]string{srcFile}, dstDir, false, true)
	if len(results) != 1 || !results[0].OK {
		t.Fatalf("Copy with overwrite failed: %+v", results)
	}

	got, err := os.ReadFile(dstFile)
	if err != nil {
		t.Fatalf("ReadFile dst: %v", err)
	}
	if string(got) != "new content" {
		t.Fatalf("expected dst to be overwritten, got %q", got)
	}
	// Source must be untouched.
	if _, err := os.Stat(srcFile); err != nil {
		t.Fatalf("expected source to remain, stat err: %v", err)
	}
}

// TestCopy_OverwriteReplacesExistingDir verifies that Copy with
// overwrite=true replaces an existing destination directory (including its
// old contents) with a copy of the source directory.
func TestCopy_OverwriteReplacesExistingDir(t *testing.T) {
	ops, root := setupJail(t)

	srcDir := filepath.Join(root, "srcdir")
	if err := os.MkdirAll(srcDir, 0o755); err != nil {
		t.Fatalf("MkdirAll srcdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(srcDir, "new.txt"), []byte("from source"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	destDir := filepath.Join(root, "dest")
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		t.Fatalf("MkdirAll dest: %v", err)
	}
	existing := filepath.Join(destDir, "srcdir")
	if err := os.MkdirAll(existing, 0o755); err != nil {
		t.Fatalf("MkdirAll existing: %v", err)
	}
	if err := os.WriteFile(filepath.Join(existing, "old.txt"), []byte("from old dest"), 0o644); err != nil {
		t.Fatalf("WriteFile old: %v", err)
	}

	results := ops.Copy([]string{srcDir}, destDir, false, true)
	if len(results) != 1 || !results[0].OK {
		t.Fatalf("Copy with overwrite failed: %+v", results)
	}

	// The old file from the previous destination dir must be gone.
	if _, err := os.Stat(filepath.Join(existing, "old.txt")); !os.IsNotExist(err) {
		t.Fatalf("expected old.txt to be gone after overwrite, stat err: %v", err)
	}
	// The new file from the source must be present.
	got, err := os.ReadFile(filepath.Join(existing, "new.txt"))
	if err != nil {
		t.Fatalf("ReadFile new.txt: %v", err)
	}
	if string(got) != "from source" {
		t.Fatalf("unexpected content: %q", got)
	}
}

// TestMove_OverwriteReplacesExistingFile verifies that Move with
// overwrite=true replaces an existing destination file and removes the
// source.
func TestMove_OverwriteReplacesExistingFile(t *testing.T) {
	ops, root := setupJail(t)

	srcDir := filepath.Join(root, "src")
	dstDir := filepath.Join(root, "dst")
	if err := os.MkdirAll(srcDir, 0o755); err != nil {
		t.Fatalf("MkdirAll src: %v", err)
	}
	if err := os.MkdirAll(dstDir, 0o755); err != nil {
		t.Fatalf("MkdirAll dst: %v", err)
	}

	srcFile := filepath.Join(srcDir, "note.txt")
	if err := os.WriteFile(srcFile, []byte("new content"), 0o644); err != nil {
		t.Fatalf("WriteFile src: %v", err)
	}
	dstFile := filepath.Join(dstDir, "note.txt")
	if err := os.WriteFile(dstFile, []byte("old content"), 0o644); err != nil {
		t.Fatalf("WriteFile dst: %v", err)
	}

	results := ops.Move([]string{srcFile}, dstDir, false, true)
	if len(results) != 1 || !results[0].OK {
		t.Fatalf("Move with overwrite failed: %+v", results)
	}

	got, err := os.ReadFile(dstFile)
	if err != nil {
		t.Fatalf("ReadFile dst: %v", err)
	}
	if string(got) != "new content" {
		t.Fatalf("expected dst to be overwritten, got %q", got)
	}
	// Source must be gone.
	if _, err := os.Stat(srcFile); !os.IsNotExist(err) {
		t.Fatalf("expected source to be removed, stat err: %v", err)
	}
}

// TestMove_OverwriteReplacesExistingDir verifies that Move with
// overwrite=true replaces an existing destination directory with the source
// directory.
func TestMove_OverwriteReplacesExistingDir(t *testing.T) {
	ops, root := setupJail(t)

	srcDir := filepath.Join(root, "srcdir")
	if err := os.MkdirAll(srcDir, 0o755); err != nil {
		t.Fatalf("MkdirAll srcdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(srcDir, "new.txt"), []byte("from source"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	destDir := filepath.Join(root, "dest")
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		t.Fatalf("MkdirAll dest: %v", err)
	}
	existing := filepath.Join(destDir, "srcdir")
	if err := os.MkdirAll(existing, 0o755); err != nil {
		t.Fatalf("MkdirAll existing: %v", err)
	}
	if err := os.WriteFile(filepath.Join(existing, "old.txt"), []byte("from old dest"), 0o644); err != nil {
		t.Fatalf("WriteFile old: %v", err)
	}

	results := ops.Move([]string{srcDir}, destDir, false, true)
	if len(results) != 1 || !results[0].OK {
		t.Fatalf("Move with overwrite failed: %+v", results)
	}

	// Source must be gone.
	if _, err := os.Stat(srcDir); !os.IsNotExist(err) {
		t.Fatalf("expected source dir to be removed, stat err: %v", err)
	}
	// The old file from the previous destination dir must be gone.
	if _, err := os.Stat(filepath.Join(existing, "old.txt")); !os.IsNotExist(err) {
		t.Fatalf("expected old.txt to be gone after overwrite, stat err: %v", err)
	}
	// The new file from the source must be present.
	got, err := os.ReadFile(filepath.Join(existing, "new.txt"))
	if err != nil {
		t.Fatalf("ReadFile new.txt: %v", err)
	}
	if string(got) != "from source" {
		t.Fatalf("unexpected content: %q", got)
	}
}

// TestCopy_ConflictWithoutOverwriteOrDuplicate is a regression check: when
// both duplicate and overwrite are false, a destination collision is still
// reported as a CONFLICT and the existing destination is left untouched.
func TestCopy_ConflictWithoutOverwriteOrDuplicate(t *testing.T) {
	ops, root := setupJail(t)

	srcFile := filepath.Join(root, "src.txt")
	if err := os.WriteFile(srcFile, []byte("new"), 0o644); err != nil {
		t.Fatalf("WriteFile src: %v", err)
	}
	dstDir := filepath.Join(root, "dst")
	if err := os.MkdirAll(dstDir, 0o755); err != nil {
		t.Fatalf("MkdirAll dst: %v", err)
	}
	dstFile := filepath.Join(dstDir, "src.txt")
	if err := os.WriteFile(dstFile, []byte("old"), 0o644); err != nil {
		t.Fatalf("WriteFile dst: %v", err)
	}

	results := ops.Copy([]string{srcFile}, dstDir, false, false)
	if len(results) != 1 || results[0].OK || results[0].Error == nil || results[0].Error.Code != "CONFLICT" {
		t.Fatalf("expected CONFLICT, got: %+v", results)
	}

	got, err := os.ReadFile(dstFile)
	if err != nil {
		t.Fatalf("ReadFile dst: %v", err)
	}
	if string(got) != "old" {
		t.Fatalf("expected dst to remain unchanged, got %q", got)
	}
}

// Duplicate-in-place: copying a file into its OWN directory with duplicate=true
// must make an auto-renamed sibling rather than hitting the same-path no-op.
func TestCopy_DuplicateInPlace(t *testing.T) {
	ops, root := setupJail(t)

	srcFile := filepath.Join(root, "note.txt")
	if err := os.WriteFile(srcFile, []byte("hello"), 0o644); err != nil {
		t.Fatalf("WriteFile src: %v", err)
	}

	results := ops.Copy([]string{srcFile}, root, true, false)
	if len(results) != 1 || !results[0].OK {
		t.Fatalf("expected OK duplicate, got: %+v", results)
	}

	// Original is untouched.
	if got, err := os.ReadFile(srcFile); err != nil || string(got) != "hello" {
		t.Fatalf("original changed: got %q err %v", got, err)
	}
	// An auto-renamed sibling now exists with the source's content.
	dup := filepath.Join(root, "note (1).txt")
	if got, err := os.ReadFile(dup); err != nil || string(got) != "hello" {
		t.Fatalf("expected duplicate %q with content 'hello', got %q err %v", dup, got, err)
	}
}

// TestMove_ConflictWithoutOverwriteOrDuplicate mirrors
// TestCopy_ConflictWithoutOverwriteOrDuplicate for Move.
func TestMove_ConflictWithoutOverwriteOrDuplicate(t *testing.T) {
	ops, root := setupJail(t)

	srcFile := filepath.Join(root, "src.txt")
	if err := os.WriteFile(srcFile, []byte("new"), 0o644); err != nil {
		t.Fatalf("WriteFile src: %v", err)
	}
	dstDir := filepath.Join(root, "dst")
	if err := os.MkdirAll(dstDir, 0o755); err != nil {
		t.Fatalf("MkdirAll dst: %v", err)
	}
	dstFile := filepath.Join(dstDir, "src.txt")
	if err := os.WriteFile(dstFile, []byte("old"), 0o644); err != nil {
		t.Fatalf("WriteFile dst: %v", err)
	}

	results := ops.Move([]string{srcFile}, dstDir, false, false)
	if len(results) != 1 || results[0].OK || results[0].Error == nil || results[0].Error.Code != "CONFLICT" {
		t.Fatalf("expected CONFLICT, got: %+v", results)
	}

	// Both files remain as they were.
	if _, err := os.Stat(srcFile); err != nil {
		t.Fatalf("expected source to remain, stat err: %v", err)
	}
	got, err := os.ReadFile(dstFile)
	if err != nil {
		t.Fatalf("ReadFile dst: %v", err)
	}
	if string(got) != "old" {
		t.Fatalf("expected dst to remain unchanged, got %q", got)
	}
}

// TestCopy_DuplicateWinsOverOverwrite verifies that when duplicate=true,
// Copy auto-renames on collision and overwrite is ignored — the existing
// destination is left untouched and a new "(1)" sibling is created.
func TestCopy_DuplicateWinsOverOverwrite(t *testing.T) {
	ops, root := setupJail(t)

	srcFile := filepath.Join(root, "src.txt")
	if err := os.WriteFile(srcFile, []byte("new"), 0o644); err != nil {
		t.Fatalf("WriteFile src: %v", err)
	}
	dstDir := filepath.Join(root, "dst")
	if err := os.MkdirAll(dstDir, 0o755); err != nil {
		t.Fatalf("MkdirAll dst: %v", err)
	}
	dstFile := filepath.Join(dstDir, "src.txt")
	if err := os.WriteFile(dstFile, []byte("old"), 0o644); err != nil {
		t.Fatalf("WriteFile dst: %v", err)
	}

	// duplicate=true AND overwrite=true: duplicate must win.
	results := ops.Copy([]string{srcFile}, dstDir, true, true)
	if len(results) != 1 || !results[0].OK {
		t.Fatalf("Copy with duplicate failed: %+v", results)
	}

	// Original destination untouched.
	got, err := os.ReadFile(dstFile)
	if err != nil {
		t.Fatalf("ReadFile dst: %v", err)
	}
	if string(got) != "old" {
		t.Fatalf("expected original dst to remain unchanged, got %q", got)
	}

	// Auto-renamed sibling exists with the source's content.
	renamed := filepath.Join(dstDir, "src (1).txt")
	got2, err := os.ReadFile(renamed)
	if err != nil {
		t.Fatalf("ReadFile renamed: %v", err)
	}
	if string(got2) != "new" {
		t.Fatalf("unexpected renamed content: %q", got2)
	}
}

// TestMove_DuplicateWinsOverOverwrite mirrors
// TestCopy_DuplicateWinsOverOverwrite for Move.
func TestMove_DuplicateWinsOverOverwrite(t *testing.T) {
	ops, root := setupJail(t)

	srcFile := filepath.Join(root, "src.txt")
	if err := os.WriteFile(srcFile, []byte("new"), 0o644); err != nil {
		t.Fatalf("WriteFile src: %v", err)
	}
	dstDir := filepath.Join(root, "dst")
	if err := os.MkdirAll(dstDir, 0o755); err != nil {
		t.Fatalf("MkdirAll dst: %v", err)
	}
	dstFile := filepath.Join(dstDir, "src.txt")
	if err := os.WriteFile(dstFile, []byte("old"), 0o644); err != nil {
		t.Fatalf("WriteFile dst: %v", err)
	}

	results := ops.Move([]string{srcFile}, dstDir, true, true)
	if len(results) != 1 || !results[0].OK {
		t.Fatalf("Move with duplicate failed: %+v", results)
	}

	// Original destination untouched.
	got, err := os.ReadFile(dstFile)
	if err != nil {
		t.Fatalf("ReadFile dst: %v", err)
	}
	if string(got) != "old" {
		t.Fatalf("expected original dst to remain unchanged, got %q", got)
	}

	// Auto-renamed sibling exists with the source's content.
	renamed := filepath.Join(dstDir, "src (1).txt")
	got2, err := os.ReadFile(renamed)
	if err != nil {
		t.Fatalf("ReadFile renamed: %v", err)
	}
	if string(got2) != "new" {
		t.Fatalf("unexpected renamed content: %q", got2)
	}

	// Source must be gone (moved).
	if _, err := os.Stat(srcFile); !os.IsNotExist(err) {
		t.Fatalf("expected source to be removed, stat err: %v", err)
	}
}

// TestCopy_SamePathIsNoOp verifies that copying a source onto itself (the
// resolved source equals the computed destination, e.g. copying a file into
// its own containing directory) is treated as a safe no-op rather than
// removing-then-copying the file onto itself.
func TestCopy_SamePathIsNoOp(t *testing.T) {
	ops, root := setupJail(t)

	file := filepath.Join(root, "self.txt")
	if err := os.WriteFile(file, []byte("contents"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	// Copying "self.txt" into root (its own parent) with overwrite=true would
	// compute dst == resSrc.
	results := ops.Copy([]string{file}, root, false, true)
	if len(results) != 1 || !results[0].OK {
		t.Fatalf("expected OK no-op, got: %+v", results)
	}

	got, err := os.ReadFile(file)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(got) != "contents" {
		t.Fatalf("expected file contents preserved, got %q", got)
	}
}

// TestMove_SamePathIsNoOp mirrors TestCopy_SamePathIsNoOp for Move.
func TestMove_SamePathIsNoOp(t *testing.T) {
	ops, root := setupJail(t)

	file := filepath.Join(root, "self.txt")
	if err := os.WriteFile(file, []byte("contents"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	results := ops.Move([]string{file}, root, false, true)
	if len(results) != 1 || !results[0].OK {
		t.Fatalf("expected OK no-op, got: %+v", results)
	}

	got, err := os.ReadFile(file)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(got) != "contents" {
		t.Fatalf("expected file contents preserved, got %q", got)
	}
}

// TestCopy_OverwriteAncestorGuard verifies that Copy never removes a
// destination that is an ancestor of the source.
//
// Setup: root/foo/bar/foo is the source (a directory named "foo" nested
// inside root/foo/bar/). Copying it into destDir=root computes
// dst = root/foo — which already exists and is an ancestor of the source
// (root/foo/bar/foo is inside root/foo). A naive overwrite would
// os.RemoveAll(dst), destroying the source along with it. The guard must
// instead report CONFLICT and leave everything in place.
func TestCopy_OverwriteAncestorGuard(t *testing.T) {
	ops, root := setupJail(t)

	// root/foo/bar/foo is the source.
	srcDir := filepath.Join(root, "foo", "bar", "foo")
	if err := os.MkdirAll(srcDir, 0o755); err != nil {
		t.Fatalf("MkdirAll srcDir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(srcDir, "child.txt"), []byte("child data"), 0o644); err != nil {
		t.Fatalf("WriteFile child: %v", err)
	}
	// Marker file directly under root/foo, to prove root/foo survives.
	marker := filepath.Join(root, "foo", "marker.txt")
	if err := os.WriteFile(marker, []byte("marker"), 0o644); err != nil {
		t.Fatalf("WriteFile marker: %v", err)
	}

	// dst = root/foo (== filepath.Join(root, filepath.Base(srcDir))), which
	// is an ancestor of srcDir.
	results := ops.Copy([]string{srcDir}, root, false, true)
	if len(results) != 1 || results[0].OK || results[0].Error == nil || results[0].Error.Code != "CONFLICT" {
		t.Fatalf("expected CONFLICT (ancestor guard), got: %+v", results)
	}

	// Nothing was destroyed: source, its child, and the marker all survive.
	if _, err := os.Stat(filepath.Join(srcDir, "child.txt")); err != nil {
		t.Fatalf("expected source child.txt to survive, stat err: %v", err)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("expected marker.txt to survive, stat err: %v", err)
	}
}

// TestMove_OverwriteAncestorGuard mirrors TestCopy_OverwriteAncestorGuard for
// Move: the source must not be deleted via os.RemoveAll(dst) when dst is one
// of its own ancestors.
func TestMove_OverwriteAncestorGuard(t *testing.T) {
	ops, root := setupJail(t)

	srcDir := filepath.Join(root, "foo", "bar", "foo")
	if err := os.MkdirAll(srcDir, 0o755); err != nil {
		t.Fatalf("MkdirAll srcDir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(srcDir, "child.txt"), []byte("child data"), 0o644); err != nil {
		t.Fatalf("WriteFile child: %v", err)
	}
	marker := filepath.Join(root, "foo", "marker.txt")
	if err := os.WriteFile(marker, []byte("marker"), 0o644); err != nil {
		t.Fatalf("WriteFile marker: %v", err)
	}

	results := ops.Move([]string{srcDir}, root, false, true)
	if len(results) != 1 || results[0].OK || results[0].Error == nil || results[0].Error.Code != "CONFLICT" {
		t.Fatalf("expected CONFLICT (ancestor guard), got: %+v", results)
	}

	if _, err := os.Stat(filepath.Join(srcDir, "child.txt")); err != nil {
		t.Fatalf("expected source child.txt to survive, stat err: %v", err)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("expected marker.txt to survive, stat err: %v", err)
	}
}

// --------- Jailed (H2 per-device path jail) ---------

// TestJailed_EmptyExtraRootIsNoOp verifies that Jailed("") returns the same
// Ops unchanged — an empty jailRoot must not regress a device's full
// (global-root) access.
func TestJailed_EmptyExtraRootIsNoOp(t *testing.T) {
	ops, root := setupJail(t)

	jailed := ops.Jailed("")
	if jailed != ops {
		t.Fatalf("expected Jailed(\"\") to return the same *Ops, got a different instance")
	}

	// Sanity: full access within the global root still works.
	if _, err := jailed.Resolve(filepath.Join(root, "anything")); err != nil {
		t.Fatalf("expected access within global root, got: %v", err)
	}
}

// TestJailed_RestrictsToSubtree sets up root/sub as a per-device jail inside
// a global root and verifies: paths inside root/sub resolve fine, while a
// sibling path, a parent path, and a "../" traversal out of root/sub are all
// rejected with ErrForbidden — even though some of those paths are still
// within the agent's GLOBAL root.
func TestJailed_RestrictsToSubtree(t *testing.T) {
	ops, root := setupJail(t)

	sub := filepath.Join(root, "sub")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatalf("MkdirAll sub: %v", err)
	}
	sibling := filepath.Join(root, "sibling")
	if err := os.MkdirAll(sibling, 0o755); err != nil {
		t.Fatalf("MkdirAll sibling: %v", err)
	}

	jailed := ops.Jailed(sub)

	// Inside the jail: OK.
	if _, err := jailed.Resolve(filepath.Join(sub, "file.txt")); err != nil {
		t.Fatalf("expected access inside jail, got: %v", err)
	}

	// Sibling path (within the global root, but outside the device jail): forbidden.
	if _, err := jailed.Resolve(filepath.Join(sibling, "file.txt")); !errors.Is(err, ErrForbidden) {
		t.Fatalf("expected ErrForbidden for sibling path, got: %v", err)
	}

	// Parent path (the global root itself): forbidden.
	if _, err := jailed.Resolve(root); !errors.Is(err, ErrForbidden) {
		t.Fatalf("expected ErrForbidden for parent path, got: %v", err)
	}

	// "../" traversal out of the jail subtree: forbidden.
	traversal := filepath.Join(sub, "..", "sibling", "file.txt")
	if _, err := jailed.Resolve(traversal); !errors.Is(err, ErrForbidden) {
		t.Fatalf("expected ErrForbidden for traversal out of jail, got: %v", err)
	}
}

// TestJailed_OutsideGlobalRootsDeniesEverything verifies that requesting a
// per-device jailRoot OUTSIDE the agent's configured global roots results in
// an Ops that denies ALL paths — including paths that would otherwise be
// allowed by the global root. This is the "never widen access" guarantee:
// fsops itself refuses to honor a jail it can't intersect.
func TestJailed_OutsideGlobalRootsDeniesEverything(t *testing.T) {
	ops, root := setupJail(t)
	outside := t.TempDir()

	jailed := ops.Jailed(outside)

	// The extraRoot itself is denied.
	if _, err := jailed.Resolve(filepath.Join(outside, "file.txt")); !errors.Is(err, ErrForbidden) {
		t.Fatalf("expected ErrForbidden for jailRoot outside global roots, got: %v", err)
	}
	// Even paths inside the (now-irrelevant) global root are denied.
	if _, err := jailed.Resolve(filepath.Join(root, "file.txt")); !errors.Is(err, ErrForbidden) {
		t.Fatalf("expected ErrForbidden for global-root path under a deny-all jail, got: %v", err)
	}
}

// TestJailed_NoGlobalRootsUsesExtraRootAsSoleRoot verifies that when the base
// Ops has NO configured roots (no global jail — New(nil, false)), Jailed
// restricts access to exactly the given extraRoot subtree.
func TestJailed_NoGlobalRootsUsesExtraRootAsSoleRoot(t *testing.T) {
	ops := New(nil, false)
	sub := t.TempDir()
	outside := t.TempDir()

	jailed := ops.Jailed(sub)

	if _, err := jailed.Resolve(filepath.Join(sub, "file.txt")); err != nil {
		t.Fatalf("expected access inside jailRoot, got: %v", err)
	}
	if _, err := jailed.Resolve(filepath.Join(outside, "file.txt")); !errors.Is(err, ErrForbidden) {
		t.Fatalf("expected ErrForbidden outside jailRoot, got: %v", err)
	}
}

// TestJailed_CopyMoveDestOutsideJailForbidden verifies that Copy and Move
// report FORBIDDEN (not OK) when either the source or the destDir is outside
// a per-device jail — covering the "copy/move whose destDir is outside the
// jail" and "source outside the jail" cases required for H2.
func TestJailed_CopyMoveDestOutsideJailForbidden(t *testing.T) {
	ops, root := setupJail(t)

	// Jail subtree: root/jailX/sub.
	jailSub := filepath.Join(root, "jailX", "sub")
	if err := os.MkdirAll(jailSub, 0o755); err != nil {
		t.Fatalf("MkdirAll jailSub: %v", err)
	}
	// A file inside the jail, to use as a source for the "dest outside jail" case.
	insideFile := filepath.Join(jailSub, "file.txt")
	if err := os.WriteFile(insideFile, []byte("data"), 0o644); err != nil {
		t.Fatalf("WriteFile insideFile: %v", err)
	}
	// A directory outside the jail (but inside the global root) — used both
	// as a forbidden destDir and as a forbidden source.
	outsideDir := filepath.Join(root, "outside")
	if err := os.MkdirAll(outsideDir, 0o755); err != nil {
		t.Fatalf("MkdirAll outsideDir: %v", err)
	}
	outsideFile := filepath.Join(outsideDir, "secret.txt")
	if err := os.WriteFile(outsideFile, []byte("secret"), 0o644); err != nil {
		t.Fatalf("WriteFile outsideFile: %v", err)
	}

	jailed := ops.Jailed(jailSub)

	// Source inside jail, destDir outside jail: FORBIDDEN.
	copyResults := jailed.Copy([]string{insideFile}, outsideDir, false, false)
	if len(copyResults) != 1 || copyResults[0].OK || copyResults[0].Error == nil || copyResults[0].Error.Code != "FORBIDDEN" {
		t.Fatalf("expected FORBIDDEN for copy with destDir outside jail, got: %+v", copyResults)
	}
	moveResults := jailed.Move([]string{insideFile}, outsideDir, false, false)
	if len(moveResults) != 1 || moveResults[0].OK || moveResults[0].Error == nil || moveResults[0].Error.Code != "FORBIDDEN" {
		t.Fatalf("expected FORBIDDEN for move with destDir outside jail, got: %+v", moveResults)
	}

	// Source outside jail, destDir inside jail: FORBIDDEN.
	copyResults2 := jailed.Copy([]string{outsideFile}, jailSub, false, false)
	if len(copyResults2) != 1 || copyResults2[0].OK || copyResults2[0].Error == nil || copyResults2[0].Error.Code != "FORBIDDEN" {
		t.Fatalf("expected FORBIDDEN for copy with source outside jail, got: %+v", copyResults2)
	}
	moveResults2 := jailed.Move([]string{outsideFile}, jailSub, false, false)
	if len(moveResults2) != 1 || moveResults2[0].OK || moveResults2[0].Error == nil || moveResults2[0].Error.Code != "FORBIDDEN" {
		t.Fatalf("expected FORBIDDEN for move with source outside jail, got: %+v", moveResults2)
	}

	// Sanity: the outside file/dir are untouched.
	if _, err := os.Stat(outsideFile); err != nil {
		t.Fatalf("expected outsideFile to survive, stat err: %v", err)
	}
	if _, err := os.Stat(insideFile); err != nil {
		t.Fatalf("expected insideFile to survive, stat err: %v", err)
	}
}

// TestCopy_NestedSymlinkNotDereferenced is the PR-05 regression: only the
// top-level source is jail-resolved, so a symlink *inside* a copied directory
// used to be opened and its bytes copied out — an arbitrary-file read for a
// jailed caller. The link must be recreated as a link, never followed.
func TestCopy_NestedSymlinkNotDereferenced(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "secret.txt")
	if err := os.WriteFile(outside, []byte("TOP SECRET"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	srcDir := filepath.Join(root, "src")
	if err := os.MkdirAll(srcDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	// A crafted link inside the tree the caller asks to copy.
	if err := os.Symlink(outside, filepath.Join(srcDir, "leak")); err != nil {
		t.Fatalf("Symlink: %v", err)
	}
	destDir := filepath.Join(root, "dest")
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	ops := New([]string{root}, false)
	for _, r := range ops.Copy([]string{srcDir}, destDir, false, false) {
		if r.Error != nil {
			t.Fatalf("Copy: %+v", r.Error)
		}
	}

	copied := filepath.Join(destDir, "src", "leak")
	fi, err := os.Lstat(copied)
	if err != nil {
		t.Fatalf("Lstat copied: %v", err)
	}
	// Dereferencing is the bug: it would materialise the secret's BYTES as a
	// regular file inside the jail, where Resolve has nothing left to reject.
	if fi.Mode()&os.ModeSymlink == 0 {
		if b, readErr := os.ReadFile(copied); readErr == nil && string(b) == "TOP SECRET" {
			t.Fatal("copy dereferenced a nested symlink and leaked a file from outside the jail")
		}
		t.Fatal("nested symlink was dereferenced into a regular file")
	}
	// Preserved as a link, the escape is still refused at read time: the link
	// points outside, and Resolve rejects on the real path, not the name.
	if _, err := ops.Resolve(copied); !errors.Is(err, ErrForbidden) {
		t.Fatalf("copied symlink should not resolve inside the jail, got err=%v", err)
	}
}

// TestCopy_DoesNotWriteThroughDestinationSymlink is the write-side half of
// PR-05: O_CREATE|O_TRUNC on a destination that is already a symlink follows
// it and truncates the target, outside the jail included.
func TestCopy_DoesNotWriteThroughDestinationSymlink(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "victim.txt")
	if err := os.WriteFile(outside, []byte("ORIGINAL"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	srcDir := filepath.Join(root, "src")
	if err := os.MkdirAll(srcDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(srcDir, "f.txt"), []byte("ATTACKER"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	// A link planted where the copy will land.
	destDir := filepath.Join(root, "dest", "src")
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.Symlink(outside, filepath.Join(destDir, "f.txt")); err != nil {
		t.Fatalf("Symlink: %v", err)
	}

	ops := New([]string{root}, false)
	ops.Copy([]string{srcDir}, filepath.Join(root, "dest"), false, true)

	b, err := os.ReadFile(outside)
	if err != nil {
		t.Fatalf("ReadFile outside: %v", err)
	}
	if string(b) != "ORIGINAL" {
		t.Fatalf("copy wrote through a destination symlink, clobbering a file outside the jail: %q", b)
	}
}

// TestFilterDrivesByRoots is the PR-61 regression: /system/drives must not
// expose every mount point to a jailed device regardless of its jail.
func TestFilterDrivesByRoots(t *testing.T) {
	all := []Drive{
		{Path: "/home/alice", Label: "alice-home"},
		{Path: "/home/alice/usb", Label: "usb-inside-jail"},
		{Path: "/mnt/other-user", Label: "unrelated"},
		{Path: "/", Label: "root"},
	}

	// No jail: unchanged.
	if got := filterDrivesByRoots(all, nil); len(got) != len(all) {
		t.Fatalf("expected all drives with no jail, got %+v", got)
	}

	// Jailed exactly to a subdirectory of a drive (not the drive itself): the
	// ancestor drive ("/home/alice", "/") can never actually be browsed to
	// (Resolve rejects anything outside the jail), so it must not be listed —
	// only a mount genuinely nested inside the jail is real and reachable.
	got := filterDrivesByRoots(all, []string{"/home/alice/Documents"})
	if len(got) != 0 {
		t.Fatalf("expected no drives (jail is a subdir of all of them), got %+v", got)
	}

	// Jailed to a path that IS one of the drives exactly, with a nested
	// mount below it: both the exact match and the nested one are reachable.
	got = filterDrivesByRoots(all, []string{"/home/alice"})
	if len(got) != 2 {
		t.Fatalf("expected /home/alice and its nested usb mount, got %+v", got)
	}
}

// TestListDir_ChildCount verifies directories carry a child count and files do not.
func TestListDir_ChildCount(t *testing.T) {
	ops, root := setupJail(t)
	if err := os.Mkdir(filepath.Join(root, "full"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, n := range []string{"1", "2", "3"} {
		if err := os.WriteFile(filepath.Join(root, "full", n), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Mkdir(filepath.Join(root, "empty"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "f.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	listing, err := ops.ListDir(root, "", 50)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]*int{}
	for _, e := range listing.Entries {
		got[e.Name] = e.ChildCount
	}
	if got["full"] == nil || *got["full"] != 3 {
		t.Fatalf("full: want 3, got %v", got["full"])
	}
	if got["empty"] == nil || *got["empty"] != 0 {
		t.Fatalf("empty: want 0, got %v", got["empty"])
	}
	if got["f.txt"] != nil {
		t.Fatalf("file must have no childCount, got %d", *got["f.txt"])
	}
}
