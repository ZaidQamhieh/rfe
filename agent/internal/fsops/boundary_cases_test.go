package fsops

// Break tests (qa2) for keep-both name selection at the auto-rename search
// boundary. Written by qa-break; not run by it.
//
// Run: cd /home/zaid/Projects/rfe-rn/agent && go test ./internal/fsops/ -run TestQA2Break -count=1 -v -timeout 300s
//
// autoRenameSecure (fsops.go) and uniqueTrashName (trash.go) stop searching at
// maxAutoRename (fsops.go, 10000) and return the candidate they are on, even if
// that name is taken. Each test below fills every earlier candidate, puts an
// existing entry on the name returned at exhaustion, and asserts only that the
// existing entry keeps its bytes. The result the operation reports at exhaustion
// is not asserted: the requirements leave it undefined. Each test creates about
// maxAutoRename small files.
//
// Helpers qa2BreakWriteFile and qa2BreakRead are defined here and used by the
// other qa2_break files in this package.

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// qa2BreakWriteFile writes content to path, creating parent directories.
func qa2BreakWriteFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// qa2BreakRead returns the bytes at path and fails the test if the entry is gone.
func qa2BreakRead(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("entry %s is no longer readable: %v", path, err)
	}
	return string(b)
}

// TestQA2Break_CopyDuplicateBoundaryNeverReplacesEntry attacks FR-14 (with
// duplicate=true the item is auto-renamed and the existing destination entry is
// left unchanged) at the boundary of the auto-rename search (fsops.go:1089-1102).
// Before the copy, destDir holds a.txt ("old"), "a (N).txt" for N = 1 to
// maxAutoRename-1 ("occupant"), and "a (maxAutoRename).txt" ("keep"). The copy
// runs with duplicate=true and overwrite=false. Expected: the boundary entry
// still holds "keep" and a.txt still holds "old". Label: code failure. Basis:
// FR-14 (existing destination entry left unchanged) and the keep-both rule at
// fsops.go:676-677 (duplicate never replaces). Confidence: high.
func TestQA2Break_CopyDuplicateBoundaryNeverReplacesEntry(t *testing.T) {
	ops, root := setupJail(t)
	src := filepath.Join(root, "src", "a.txt")
	qa2BreakWriteFile(t, src, "new")
	dstDir := filepath.Join(root, "dst")
	qa2BreakWriteFile(t, filepath.Join(dstDir, "a.txt"), "old")
	for i := 1; i < maxAutoRename; i++ {
		qa2BreakWriteFile(t, filepath.Join(dstDir, fmt.Sprintf("a (%d).txt", i)), "occupant")
	}
	boundary := filepath.Join(dstDir, fmt.Sprintf("a (%d).txt", maxAutoRename))
	qa2BreakWriteFile(t, boundary, "keep")

	ops.Copy([]string{src}, dstDir, true, false)

	if got := qa2BreakRead(t, boundary); got != "keep" {
		t.Fatalf("FR-14: a duplicate copy replaced the existing entry %s (now %q, was \"keep\")", boundary, got)
	}
	if got := qa2BreakRead(t, filepath.Join(dstDir, "a.txt")); got != "old" {
		t.Fatalf("FR-14: the existing destination a.txt changed to %q (was \"old\")", got)
	}
}

// TestQA2Break_MoveDuplicateBoundaryNeverReplacesEntry is the Move form of the
// boundary scenario above. Move with duplicate=true takes the same auto-rename
// result (fsops.go:835-836) and then renames the source onto it, which replaces
// an existing file. Expected: the boundary entry still holds "keep" and a.txt
// still holds "old". Label: code failure. Basis: FR-14 (existing destination
// entry left unchanged). Confidence: high.
func TestQA2Break_MoveDuplicateBoundaryNeverReplacesEntry(t *testing.T) {
	ops, root := setupJail(t)
	src := filepath.Join(root, "src", "a.txt")
	qa2BreakWriteFile(t, src, "new")
	dstDir := filepath.Join(root, "dst")
	qa2BreakWriteFile(t, filepath.Join(dstDir, "a.txt"), "old")
	for i := 1; i < maxAutoRename; i++ {
		qa2BreakWriteFile(t, filepath.Join(dstDir, fmt.Sprintf("a (%d).txt", i)), "occupant")
	}
	boundary := filepath.Join(dstDir, fmt.Sprintf("a (%d).txt", maxAutoRename))
	qa2BreakWriteFile(t, boundary, "keep")

	ops.Move([]string{src}, dstDir, true, false)

	if got := qa2BreakRead(t, boundary); got != "keep" {
		t.Fatalf("FR-14: a duplicate move replaced the existing entry %s (now %q, was \"keep\")", boundary, got)
	}
	if got := qa2BreakRead(t, filepath.Join(dstDir, "a.txt")); got != "old" {
		t.Fatalf("FR-14: the existing destination a.txt changed to %q (was \"old\")", got)
	}
}

// TestQA2Break_TrashBoundaryNeverReplacesTrashedItem attacks the trash store's
// payload invariant: a later MoveToTrash must not overwrite an earlier trashed
// item. uniqueTrashName (trash.go:441-455) returns candidate maxAutoRename-1
// when every earlier candidate is taken, and the payload is then renamed onto
// it. The unjailed Ops is used so the move is a plain os.Rename (trash.go:400-412),
// which replaces an existing file. Scenario: the store already holds an earlier
// item whose payload is "keep" at files/x_<maxAutoRename-1>.txt. Sidecars block
// every name below it, so the search reaches the boundary. Then "x.txt" is
// trashed. Expected: the earlier payload still holds "keep". Label: code
// failure. Basis: FR-6 (a purge removes only the named trash ids, so an earlier
// trashed item stays until purged) and UC-DELETE post (a trashed entry is
// listed, not destroyed). Confidence: high for the mechanism; the requirement
// mapping is an inference from FR-6.
func TestQA2Break_TrashBoundaryNeverReplacesTrashedItem(t *testing.T) {
	ops := New(nil, false)
	base := t.TempDir()
	trashDir := filepath.Join(base, "Trash")
	if err := os.MkdirAll(trashFilesDir(trashDir), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(trashInfoDir(trashDir), 0o700); err != nil {
		t.Fatal(err)
	}
	src := filepath.Join(base, "work", "x.txt")
	qa2BreakWriteFile(t, src, "new")

	// Block the base name and every candidate below the boundary with a sidecar.
	qa2BreakWriteFile(t, filepath.Join(trashInfoDir(trashDir), "x.txt"+trashInfoExt), "[Trash Info]\n")
	for i := 1; i < maxAutoRename; i++ {
		qa2BreakWriteFile(t, filepath.Join(trashInfoDir(trashDir), fmt.Sprintf("x_%d.txt%s", i, trashInfoExt)), "[Trash Info]\n")
	}
	// The earlier trashed item sits on the boundary name.
	earlier := filepath.Join(trashFilesDir(trashDir), fmt.Sprintf("x_%d.txt", maxAutoRename-1))
	qa2BreakWriteFile(t, earlier, "keep")

	ops.MoveToTrash([]string{src}, trashDir)

	if got := qa2BreakRead(t, earlier); got != "keep" {
		t.Fatalf("FR-6: trashing x.txt overwrote the earlier trashed item %s (now %q, was \"keep\")", earlier, got)
	}
}

// TestQA2Break_RestoreBoundaryNeverReplacesOccupant attacks FR-8 (a restore
// whose recorded path is occupied must keep both, leaving the occupant
// unchanged) at the auto-rename boundary (fsops.go:1089-1102). Unjailed Ops, so
// the restore is a plain os.Rename (trash.go:400-412), which replaces an existing
// file. Scenario: the recorded path home/a.txt and "home/a (N).txt" for N = 1 to
// maxAutoRename-1 are occupied, and "home/a (maxAutoRename).txt" holds "keep".
// The trashed item (payload "restored") is restored. Expected: the boundary
// entry holds "keep" and home/a.txt holds "occupant". Label: code failure. Basis:
// FR-8 Accept ("the entry at P is unchanged") and UC-TRASH-RESTORE alt ("restored
// under an auto-renamed name, occupant unchanged"). Confidence: high.
func TestQA2Break_RestoreBoundaryNeverReplacesOccupant(t *testing.T) {
	ops := New(nil, false)
	base := t.TempDir()
	trashDir := filepath.Join(base, "Trash")
	home := filepath.Join(base, "home")
	orig := filepath.Join(home, "a.txt")
	qa2BreakWriteFile(t, orig, "occupant")
	for i := 1; i < maxAutoRename; i++ {
		qa2BreakWriteFile(t, filepath.Join(home, fmt.Sprintf("a (%d).txt", i)), "occupant")
	}
	boundary := filepath.Join(home, fmt.Sprintf("a (%d).txt", maxAutoRename))
	qa2BreakWriteFile(t, boundary, "keep")

	const id = "restore-boundary-item"
	qa2BreakWriteFile(t, filepath.Join(trashFilesDir(trashDir), id), "restored")
	if err := os.MkdirAll(trashInfoDir(trashDir), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := writeTrashInfo(trashDir, id, orig); err != nil {
		t.Fatal(err)
	}

	ops.RestoreFromTrash([]string{id}, trashDir)

	if got := qa2BreakRead(t, boundary); got != "keep" {
		t.Fatalf("FR-8: restoring %s replaced the occupant %s (now %q, was \"keep\")", id, boundary, got)
	}
	if got := qa2BreakRead(t, orig); got != "occupant" {
		t.Fatalf("FR-8: the occupant of the recorded path %s changed to %q", orig, got)
	}
}
