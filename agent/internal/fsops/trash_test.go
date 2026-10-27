package fsops

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// setupTrash returns an Ops jailed to a temp root plus a separate trash dir.
func setupTrash(t *testing.T) (ops *Ops, root, trashDir string) {
	t.Helper()
	ops, root = setupJail(t)
	trashDir = filepath.Join(t.TempDir(), "Trash")
	return ops, root, trashDir
}

// TestTrash_MoveListRestore covers the full lifecycle: a file moves to trash
// (gone from its original location, present in the store + listing), then
// restores back to where it came from.
func TestTrash_MoveListRestore(t *testing.T) {
	ops, root, trashDir := setupTrash(t)
	src := filepath.Join(root, "doc.txt")
	if err := os.WriteFile(src, []byte("keep me"), 0o644); err != nil {
		t.Fatal(err)
	}

	res := ops.MoveToTrash([]string{src}, trashDir)
	if len(res) != 1 || !res[0].OK {
		t.Fatalf("MoveToTrash: %+v", res)
	}
	if _, err := os.Stat(src); !os.IsNotExist(err) {
		t.Fatalf("original still present after trashing")
	}

	items, err := ListTrash(trashDir)
	if err != nil {
		t.Fatalf("ListTrash: %v", err)
	}
	if len(items) != 1 || items[0].OriginalPath != src || items[0].Name != "doc.txt" {
		t.Fatalf("unexpected trash listing: %+v", items)
	}

	rr := ops.RestoreFromTrash([]string{items[0].ID}, trashDir)
	if len(rr) != 1 || !rr[0].OK {
		t.Fatalf("RestoreFromTrash: %+v", rr)
	}
	if got, _ := os.ReadFile(src); string(got) != "keep me" {
		t.Fatalf("restored content = %q, want 'keep me'", got)
	}
	// Trash is now empty.
	if items, _ := ListTrash(trashDir); len(items) != 0 {
		t.Fatalf("trash not empty after restore: %+v", items)
	}
}

// TestTrash_RestoreAutoRenamesOnCollision verifies a restore doesn't clobber a
// file that reappeared at the original path.
func TestTrash_RestoreAutoRenamesOnCollision(t *testing.T) {
	ops, root, trashDir := setupTrash(t)
	src := filepath.Join(root, "note.txt")
	if err := os.WriteFile(src, []byte("v1"), 0o644); err != nil {
		t.Fatal(err)
	}
	ops.MoveToTrash([]string{src}, trashDir)
	// A new file takes the original name before we restore.
	if err := os.WriteFile(src, []byte("v2"), 0o644); err != nil {
		t.Fatal(err)
	}

	items, _ := ListTrash(trashDir)
	rr := ops.RestoreFromTrash([]string{items[0].ID}, trashDir)
	if !rr[0].OK {
		t.Fatalf("restore failed: %+v", rr)
	}
	if rr[0].Path == src {
		t.Fatalf("restore clobbered the occupant instead of auto-renaming")
	}
	if got, _ := os.ReadFile(src); string(got) != "v2" {
		t.Fatalf("occupant was overwritten: %q", got)
	}
	if got, _ := os.ReadFile(rr[0].Path); string(got) != "v1" {
		t.Fatalf("restored copy = %q, want v1", got)
	}
}

// TestTrash_Empty removes everything from the store.
func TestTrash_Empty(t *testing.T) {
	ops, root, trashDir := setupTrash(t)
	for _, n := range []string{"a.txt", "b.txt"} {
		p := filepath.Join(root, n)
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		ops.MoveToTrash([]string{p}, trashDir)
	}
	if items, _ := ListTrash(trashDir); len(items) != 2 {
		t.Fatalf("expected 2 trashed items, got %d", len(items))
	}
	if err := ops.EmptyTrash(trashDir, nil); err != nil {
		t.Fatalf("EmptyTrash: %v", err)
	}
	if items, _ := ListTrash(trashDir); len(items) != 0 {
		t.Fatalf("trash not empty: %+v", items)
	}
}

// TestTrash_ReadOnly verifies trashing is blocked in read-only mode.
func TestTrash_ReadOnly(t *testing.T) {
	root := t.TempDir()
	ro := New([]string{root}, true)
	src := filepath.Join(root, "x.txt")
	if err := os.WriteFile(src, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	res := ro.MoveToTrash([]string{src}, filepath.Join(t.TempDir(), "Trash"))
	if res[0].OK || res[0].Error == nil || res[0].Error.Code != "READ_ONLY" {
		t.Fatalf("expected READ_ONLY, got %+v", res[0])
	}
}

// TestTrash_PathWithSpacesRoundTrips trashes a file whose original path
// contains a space, then checks the listing reports the exact original path
// (the .trashinfo encoding round-trips) and restore puts it back there.
func TestTrash_PathWithSpacesRoundTrips(t *testing.T) {
	ops, root, trashDir := setupTrash(t)
	dir := filepath.Join(root, "My Photos")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	src := filepath.Join(dir, "a b.txt")
	if err := os.WriteFile(src, []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}

	if res := ops.MoveToTrash([]string{src}, trashDir); !res[0].OK {
		t.Fatalf("trash: %+v", res[0])
	}
	items, _ := ListTrash(trashDir)
	if len(items) != 1 || items[0].OriginalPath != src {
		t.Fatalf("OriginalPath = %q, want %q", items[0].OriginalPath, src)
	}
	// The on-disk .trashinfo should keep '/' literal (XDG-compatible). Windows
	// paths use '\', which is percent-encoded (%5C); the OriginalPath check
	// above and the restore below cover the round trip there.
	infoBytes, _ := os.ReadFile(
		filepath.Join(trashInfoDir(trashDir), items[0].ID+trashInfoExt),
	)
	if runtime.GOOS != "windows" && (!strings.Contains(string(infoBytes), "Path=/") ||
		strings.Contains(string(infoBytes), "%2F")) {
		t.Fatalf("trashinfo should keep '/' literal:\n%s", infoBytes)
	}

	if res := ops.RestoreFromTrash([]string{items[0].ID}, trashDir); !res[0].OK {
		t.Fatalf("restore: %+v", res[0])
	}
	if got, _ := os.ReadFile(src); string(got) != "hi" {
		t.Fatalf("restored content = %q, want hi", got)
	}
}

// TestTrash_MoveOutsideJail verifies a path outside the jail can't be trashed.
func TestTrash_MoveOutsideJail(t *testing.T) {
	ops, _, trashDir := setupTrash(t)
	outside := filepath.Join(t.TempDir(), "secret.txt")
	if err := os.WriteFile(outside, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	res := ops.MoveToTrash([]string{outside}, trashDir)
	if res[0].OK || res[0].Error == nil || res[0].Error.Code != "FORBIDDEN" {
		t.Fatalf("expected FORBIDDEN, got %+v", res[0])
	}
}

// TestTrash_TraversalRejected is the PR-01 regression: a client-supplied trash
// id that escapes the store (separators, dot names, absolute) must not delete
// or restore anything outside files/.
func TestTrash_TraversalRejected(t *testing.T) {
	ops, root, trashDir := setupTrash(t)

	// A victim file outside the trash store that traversal would target.
	victim := filepath.Join(root, "victim.txt")
	if err := os.WriteFile(victim, []byte("do not delete"), 0o644); err != nil {
		t.Fatal(err)
	}
	rel, err := filepath.Rel(trashFilesDir(trashDir), victim)
	if err != nil {
		t.Fatal(err)
	}

	bad := []string{rel, "../../../../etc/passwd", "..", ".", "a/b", "/abs", ""}
	for _, id := range bad {
		if err := ops.EmptyTrash(trashDir, []string{id}); err == nil {
			t.Fatalf("EmptyTrash accepted traversal id %q", id)
		}
		res := ops.RestoreFromTrash([]string{id}, trashDir)
		if len(res) != 1 || res[0].OK {
			t.Fatalf("RestoreFromTrash accepted traversal id %q: %+v", id, res)
		}
	}
	if _, err := os.Stat(victim); err != nil {
		t.Fatalf("victim was deleted through trash traversal: %v", err)
	}
}

// TestTrash_JailNarrowsGlobalStore is the PR-61 regression: the trash store
// has no per-device partitioning (everything anyone deletes lands in the same
// on-disk store), so a jailed device's view of it must be narrowed to its own
// jail rather than exposing every other device's deleted files.
func TestTrash_JailNarrowsGlobalStore(t *testing.T) {
	rootA := t.TempDir()
	rootB := t.TempDir()
	trashDir := filepath.Join(t.TempDir(), "Trash")

	unjailed := New(nil, false)
	fileA := filepath.Join(rootA, "a.txt")
	fileB := filepath.Join(rootB, "b.txt")
	if err := os.WriteFile(fileA, []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fileB, []byte("b"), 0o644); err != nil {
		t.Fatal(err)
	}
	if res := unjailed.MoveToTrash([]string{fileA}, trashDir); !res[0].OK {
		t.Fatalf("trash a: %+v", res[0])
	}
	if res := unjailed.MoveToTrash([]string{fileB}, trashDir); !res[0].OK {
		t.Fatalf("trash b: %+v", res[0])
	}

	jailedA := New([]string{rootA}, false)

	items, err := jailedA.ListTrash(trashDir)
	if err != nil {
		t.Fatalf("ListTrash: %v", err)
	}
	if len(items) != 1 || items[0].OriginalPath != fileA {
		t.Fatalf("expected only rootA's item, got %+v", items)
	}

	// EmptyTrash with no ids, while jailed, must only remove the item(s)
	// within the jail — not fall back to wiping the entire global store.
	if err := jailedA.EmptyTrash(trashDir, nil); err != nil {
		t.Fatalf("EmptyTrash: %v", err)
	}
	remaining, _ := unjailed.ListTrash(trashDir)
	if len(remaining) != 1 || remaining[0].OriginalPath != fileB {
		t.Fatalf("expected only rootB's item to survive, got %+v", remaining)
	}
}

// TestTrash_EmptyTrash_SkipsIDsOutsideJail: an explicit id list must not let
// a jailed device delete an item outside its jail either.
func TestTrash_EmptyTrash_SkipsIDsOutsideJail(t *testing.T) {
	rootA := t.TempDir()
	rootB := t.TempDir()
	trashDir := filepath.Join(t.TempDir(), "Trash")

	unjailed := New(nil, false)
	fileB := filepath.Join(rootB, "b.txt")
	if err := os.WriteFile(fileB, []byte("b"), 0o644); err != nil {
		t.Fatal(err)
	}
	res := unjailed.MoveToTrash([]string{fileB}, trashDir)
	if !res[0].OK {
		t.Fatalf("trash b: %+v", res[0])
	}
	items, _ := unjailed.ListTrash(trashDir)
	if len(items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(items))
	}

	jailedA := New([]string{rootA}, false)
	if err := jailedA.EmptyTrash(trashDir, []string{items[0].ID}); err != nil {
		t.Fatalf("EmptyTrash: %v", err)
	}
	remaining, _ := unjailed.ListTrash(trashDir)
	if len(remaining) != 1 {
		t.Fatalf("item outside the jail was deleted despite an explicit id: %+v", remaining)
	}
}

// A trash id that names a symlink must delete the link, never its target.
func TestTrash_EmptyDoesNotFollowSymlinkEntry(t *testing.T) {
	ops, _, trashDir := setupTrash(t)
	outside := t.TempDir()
	keep := filepath.Join(outside, "keep.txt")
	if err := os.WriteFile(keep, []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(trashFilesDir(trashDir), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(trashFilesDir(trashDir), "link")); err != nil {
		t.Skip("symlinks unavailable:", err)
	}
	_ = ops.EmptyTrash(trashDir, []string{"link"})
	if _, err := os.Stat(keep); err != nil {
		t.Fatalf("symlink target was deleted: %v", err)
	}
}

// If the store's files/ directory was swapped for a symlink, ids must not
// resolve through it into other directories.
func TestTrash_EmptyRefusesSymlinkedStore(t *testing.T) {
	ops, _, trashDir := setupTrash(t)
	outside := t.TempDir()
	victim := filepath.Join(outside, "victim.txt")
	if err := os.WriteFile(victim, []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(trashDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, trashFilesDir(trashDir)); err != nil {
		t.Skip("symlinks unavailable:", err)
	}
	if err := ops.EmptyTrash(trashDir, []string{"victim.txt"}); err == nil {
		t.Fatal("EmptyTrash accepted a symlinked store")
	}
	if _, err := os.Stat(victim); err != nil {
		t.Fatalf("victim deleted through symlinked store: %v", err)
	}
}

// TestTrash_JailedUsesRenameNotCopy verifies a jailed trash on the same
// filesystem renames (inode preserved) instead of copying the tree.
func TestTrash_JailedUsesRenameNotCopy(t *testing.T) {
	ops, root, trashDir := setupTrash(t)
	dir := filepath.Join(root, "tree")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Compare opened handles: on Windows a plain Stat carries no file index, so SameFile on two Stat results is always false.
	statOpen := func(p string) os.FileInfo {
		t.Helper()
		f, err := os.Open(p)
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		fi, err := f.Stat()
		if err != nil {
			t.Fatal(err)
		}
		return fi
	}
	before := statOpen(dir)
	res := ops.MoveToTrash([]string{dir}, trashDir)
	if len(res) != 1 || !res[0].OK {
		t.Fatalf("MoveToTrash: %+v", res)
	}
	after := statOpen(filepath.Join(trashFilesDir(trashDir), "tree"))
	if !os.SameFile(before, after) {
		t.Fatalf("trash entry is a copy, expected a rename")
	}
}

// TestTrash_JailedRootCannotBeTrashed verifies the jail root is refused, not moved into the trash.
func TestTrash_JailedRootCannotBeTrashed(t *testing.T) {
	ops, root, trashDir := setupTrash(t)
	res := ops.MoveToTrash([]string{root}, trashDir)
	if len(res) != 1 || res[0].OK {
		t.Fatalf("expected the jail root to be refused: %+v", res)
	}
	if _, err := os.Stat(root); err != nil {
		t.Fatalf("jail root gone: %v", err)
	}
}
