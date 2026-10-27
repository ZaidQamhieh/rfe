//go:build !windows

package fsops

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// Entry operations (delete, trash, rename, move) act on a symlink itself,
// never on what it points to. Before the fix each of them resolved the final
// path component, so deleting a shortcut deleted the real folder.

func symlinkFixture(t *testing.T) (ops *Ops, root, target, link string) {
	t.Helper()
	ops, root = setupJail(t)
	target = filepath.Join(root, "real")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target, "keep.txt"), []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}
	link = filepath.Join(root, "shortcut")
	if err := os.Symlink("real", link); err != nil {
		t.Fatal(err)
	}
	return ops, root, target, link
}

func assertTargetIntact(t *testing.T, target string) {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(target, "keep.txt"))
	if err != nil || string(b) != "keep" {
		t.Fatalf("symlink target was modified: %v %q", err, b)
	}
}

func assertGone(t *testing.T, p string) {
	t.Helper()
	if _, err := os.Lstat(p); !os.IsNotExist(err) {
		t.Fatalf("%s still exists (err=%v)", p, err)
	}
}

func TestSymlinkEntry_DeleteRemovesLinkNotTarget(t *testing.T) {
	ops, root, target, link := symlinkFixture(t)
	fileLink := filepath.Join(root, "filelink")
	if err := os.Symlink("real/keep.txt", fileLink); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{link, fileLink} {
		res := ops.Delete([]string{p})
		if len(res) != 1 || !res[0].OK {
			t.Fatalf("Delete(%s) = %+v", p, res)
		}
		assertGone(t, p)
	}
	assertTargetIntact(t, target)
}

func TestSymlinkEntry_DeleteEscapingAndDanglingLinks(t *testing.T) {
	ops, root := setupJail(t)
	outside := t.TempDir()
	secret := filepath.Join(outside, "secret.txt")
	if err := os.WriteFile(secret, []byte("s"), 0o644); err != nil {
		t.Fatal(err)
	}
	escape := filepath.Join(root, "escape")
	dangling := filepath.Join(root, "dangling")
	if err := os.Symlink(outside, escape); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("nowhere", dangling); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{escape, dangling} {
		res := ops.Delete([]string{p})
		if len(res) != 1 || !res[0].OK {
			t.Fatalf("Delete(%s) = %+v", p, res)
		}
		assertGone(t, p)
	}
	if _, err := os.Stat(secret); err != nil {
		t.Fatalf("file outside the root was touched: %v", err)
	}
	// Reads through the escaping link stay forbidden.
	if err := os.Symlink(outside, escape); err != nil {
		t.Fatal(err)
	}
	if f, err := ops.Open(filepath.Join(escape, "secret.txt")); err == nil {
		f.Close()
		t.Fatal("read through an escaping symlink was allowed")
	}
}

func TestSymlinkEntry_TrashMovesLinkNotTarget(t *testing.T) {
	ops, _, target, link := symlinkFixture(t)
	trashDir := filepath.Join(t.TempDir(), "Trash")
	res := ops.MoveToTrash([]string{link}, trashDir)
	if len(res) != 1 || !res[0].OK {
		t.Fatalf("MoveToTrash = %+v", res)
	}
	assertGone(t, link)
	assertTargetIntact(t, target)
	entries, err := ops.ListTrash(trashDir)
	if err != nil || len(entries) != 1 || entries[0].OriginalPath != link {
		t.Fatalf("trash listing = %+v, %v; want the link's own path", entries, err)
	}
	if r := ops.RestoreFromTrash([]string{entries[0].ID}, trashDir); len(r) != 1 || !r[0].OK {
		t.Fatalf("restore = %+v", r)
	}
	if dest, err := os.Readlink(link); err != nil || dest != "real" {
		t.Fatalf("restored link = %q, %v", dest, err)
	}
}

func TestSymlinkEntry_RenameRenamesLink(t *testing.T) {
	ops, root, target, link := symlinkFixture(t)
	renamed := filepath.Join(root, "renamed")
	if _, err := ops.Rename(link, renamed); err != nil {
		t.Fatalf("Rename: %v", err)
	}
	assertGone(t, link)
	if dest, err := os.Readlink(renamed); err != nil || dest != "real" {
		t.Fatalf("renamed entry is not the link: %q, %v", dest, err)
	}
	assertTargetIntact(t, target)
}

func TestSymlinkEntry_RenameOntoDanglingLinkConflicts(t *testing.T) {
	ops, root := setupJail(t)
	src := filepath.Join(root, "a.txt")
	if err := os.WriteFile(src, []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}
	dangling := filepath.Join(root, "b")
	if err := os.Symlink("nowhere", dangling); err != nil {
		t.Fatal(err)
	}
	if _, err := ops.Rename(src, dangling); err != ErrConflict {
		t.Fatalf("Rename onto an existing link = %v, want ErrConflict", err)
	}
	if _, err := os.Lstat(filepath.Join(root, "nowhere")); !os.IsNotExist(err) {
		t.Fatal("rename followed the dangling link and created its target")
	}
}

func TestSymlinkEntry_MoveMovesLink(t *testing.T) {
	ops, root, target, link := symlinkFixture(t)
	destDir := filepath.Join(root, "dest")
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		t.Fatal(err)
	}
	res := ops.Move([]string{link}, destDir, false, false)
	if len(res) != 1 || !res[0].OK {
		t.Fatalf("Move = %+v", res)
	}
	assertGone(t, link)
	if dest, err := os.Readlink(filepath.Join(destDir, "shortcut")); err != nil || dest != "real" {
		t.Fatalf("moved entry is not the link: %q, %v", dest, err)
	}
	assertTargetIntact(t, target)
}

// The configured root is still not deletable or renamable through the
// entry path, and ordinary files behave as before.
func TestSymlinkEntry_RootAndPlainFilesUnchanged(t *testing.T) {
	ops, root := setupJail(t)
	if res := ops.Delete([]string{root}); len(res) != 1 || res[0].OK {
		t.Fatalf("Delete(root) = %+v, want failure", res)
	}
	if _, err := os.Stat(root); err != nil {
		t.Fatalf("root removed: %v", err)
	}
	f := filepath.Join(root, "plain.txt")
	if err := os.WriteFile(f, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if res := ops.Delete([]string{filepath.Join(root, "sub", "..", "plain.txt")}); len(res) != 1 || !res[0].OK {
		t.Fatalf("Delete(plain) = %+v", res)
	}
	assertGone(t, f)
}

func TestSymlinkEntry_RenameReturnsLinkEntry(t *testing.T) {
	ops, root, _, link := symlinkFixture(t)
	e, err := ops.Rename(link, filepath.Join(root, "alias"))
	if err != nil {
		t.Fatal(err)
	}
	if e.Name != "alias" || e.Path != filepath.Join(root, "alias") || !e.IsSymlink {
		t.Fatalf("rename response describes %+v, want the link itself", e)
	}
}

func TestSymlinkEntry_CopyKeepsLinkName(t *testing.T) {
	ops, root := setupJail(t)
	if err := os.WriteFile(filepath.Join(root, "report.pdf"), []byte("r"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("report.pdf", filepath.Join(root, "Shortcut")); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(root, "b")
	if err := os.MkdirAll(dest, 0o755); err != nil {
		t.Fatal(err)
	}
	existing := filepath.Join(dest, "report.pdf")
	if err := os.WriteFile(existing, []byte("other"), 0o644); err != nil {
		t.Fatal(err)
	}
	res := ops.Copy([]string{filepath.Join(root, "Shortcut")}, dest, false, true)
	if len(res) != 1 || !res[0].OK {
		t.Fatalf("Copy = %+v", res)
	}
	if b, _ := os.ReadFile(existing); string(b) != "other" {
		t.Fatalf("copy of a link overwrote the target-named file: %q", b)
	}
	if _, err := os.Lstat(filepath.Join(dest, "Shortcut")); err != nil {
		t.Fatalf("copy is not named after the link: %v", err)
	}
}

func TestRename_IntoOwnSubtreeConflictsWithoutSideEffects(t *testing.T) {
	ops, root := setupJail(t)
	src := filepath.Join(root, "a")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := ops.Rename(src, filepath.Join(src, "b", "c")); err != ErrConflict {
		t.Fatalf("Rename into own subtree = %v, want ErrConflict", err)
	}
	if _, err := os.Lstat(filepath.Join(src, "b")); !os.IsNotExist(err) {
		t.Fatal("rename created the destination's parents inside the source")
	}
}

func TestChmod_UnreadableItemCanBeRestored(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores permission bits")
	}
	ops, root := setupJail(t)
	f := filepath.Join(root, "lock.txt")
	if err := os.WriteFile(f, []byte("z"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(f, 0o644) })
	if err := ops.Chmod(f, 0); err != nil {
		t.Fatal(err)
	}
	if err := ops.Chmod(f, 0o644); err != nil {
		t.Fatalf("chmod back from 0000 failed: %v", err)
	}
	if st, _ := os.Stat(f); st.Mode().Perm() != 0o644 {
		t.Fatalf("mode = %v", st.Mode().Perm())
	}
}

// A parent that is itself an escaping link must not let the leaf be
// resolved through it (review finding on resolveEntry's fallback).
func TestSymlinkEntry_ParentOutsideJailIsRefused(t *testing.T) {
	ops, root := setupJail(t)
	secret := filepath.Join(root, "secret.txt")
	if err := os.WriteFile(secret, []byte("s"), 0o644); err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	if err := os.Symlink(secret, filepath.Join(outside, "evil")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "out")); err != nil {
		t.Fatal(err)
	}
	res := ops.Delete([]string{filepath.Join(root, "out", "evil")})
	if len(res) != 1 || res[0].OK {
		t.Fatalf("Delete through an escaping parent = %+v, want refusal", res)
	}
	if _, err := os.Stat(secret); err != nil {
		t.Fatalf("in-jail target deleted through an outside link: %v", err)
	}
}

// A NUL byte cannot name a file; it is refused as a bad path instead of
// surfacing as an internal error from the syscall.
func TestResolve_RejectsNULByte(t *testing.T) {
	ops, root := setupJail(t)
	for _, p := range []string{root + "/a\x00b", root + "/\x00"} {
		if _, err := ops.Resolve(p); !errors.Is(err, ErrForbidden) {
			t.Fatalf("Resolve(%q) = %v, want ErrForbidden", p, err)
		}
		if _, err := ops.CreateFolder(p); !errors.Is(err, ErrForbidden) {
			t.Fatalf("CreateFolder(%q) = %v, want ErrForbidden", p, err)
		}
		if res := ops.Delete([]string{p}); len(res) != 1 || res[0].OK || res[0].Error.Code != "FORBIDDEN" {
			t.Fatalf("Delete(%q) = %+v, want FORBIDDEN", p, res)
		}
		if _, err := ops.Rename(filepath.Join(root, "x"), p); !errors.Is(err, ErrForbidden) {
			t.Fatalf("Rename(_, %q) = %v, want ErrForbidden", p, err)
		}
	}
}

// Restoring next to a symlink that now occupies the original path keeps the
// item in its original folder (review finding R7).
func TestSymlinkEntry_RestoreDoesNotFollowLinkAtOriginalPath(t *testing.T) {
	ops, root := setupJail(t)
	trashDir := filepath.Join(t.TempDir(), "Trash")
	docs := filepath.Join(root, "docs")
	if err := os.MkdirAll(docs, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(docs, "report.pdf"), []byte("r"), 0o644); err != nil {
		t.Fatal(err)
	}
	item := filepath.Join(root, "item")
	if err := os.WriteFile(item, []byte("orig"), 0o644); err != nil {
		t.Fatal(err)
	}
	if res := ops.MoveToTrash([]string{item}, trashDir); !res[0].OK {
		t.Fatalf("trash = %+v", res)
	}
	if err := os.Symlink("docs/report.pdf", item); err != nil {
		t.Fatal(err)
	}
	entries, err := ops.ListTrash(trashDir)
	if err != nil || len(entries) != 1 {
		t.Fatalf("list = %+v, %v", entries, err)
	}
	res := ops.RestoreFromTrash([]string{entries[0].ID}, trashDir)
	if len(res) != 1 || !res[0].OK {
		t.Fatalf("restore = %+v", res)
	}
	if filepath.Dir(res[0].Path) != root {
		t.Fatalf("restored into %s, want the original folder %s", res[0].Path, root)
	}
	if b, err := os.ReadFile(res[0].Path); err != nil || string(b) != "orig" {
		t.Fatalf("restored content = %q, %v", b, err)
	}
}
