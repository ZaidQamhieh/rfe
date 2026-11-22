package fsops

// Spec tests (QA2) for the trash store: DELETE to trash, permanent delete,
// GET /trash, DELETE /trash (empty with ids) and POST /trash/restore.

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// qa2specTrashItems lists the trash store, treating a missing store as empty.
func qa2specTrashItems(t *testing.T, ops *Ops, trashDir string) []TrashEntry {
	t.Helper()
	items, err := ops.ListTrash(trashDir)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("ListTrash: %v", err)
	}
	return items
}

// qa2specTrashIDs maps each listed trash item's name to its trash id.
func qa2specTrashIDs(t *testing.T, ops *Ops, trashDir string) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, it := range qa2specTrashItems(t, ops, trashDir) {
		out[it.Name] = it.ID
	}
	return out
}

// TestQA2SpecFR3_DeleteFileGoesToTrash checks the default delete for a file.
func TestQA2SpecFR3_DeleteFileGoesToTrash(t *testing.T) {
	// FR-3: "Given a file inside the device's jail, when DELETE /fs?path=<file> without permanent, then the file is absent from its original location and GET /trash lists it."
	// catches: the file is removed outright, or GET /trash does not list it.
	ops, root, trashDir := setupTrash(t)
	src := filepath.Join(root, "doc.txt")
	qa2specWrite(t, src, "keep me")

	res := ops.MoveToTrash([]string{src}, trashDir)
	if len(res) != 1 || !res[0].OK {
		t.Fatalf("MoveToTrash: %+v, want one ok result", res)
	}
	if qa2specExists(src) {
		t.Errorf("file still at %s after trashing", src)
	}
	items := qa2specTrashItems(t, ops, trashDir)
	if len(items) != 1 || items[0].Name != "doc.txt" || items[0].OriginalPath != src || items[0].IsDir {
		t.Errorf("GET /trash = %+v, want one file entry for %s", items, src)
	}
}

// TestQA2SpecFR3_DeleteFolderGoesToTrash checks the default delete for a folder with contents.
func TestQA2SpecFR3_DeleteFolderGoesToTrash(t *testing.T) {
	// FR-3: "Given a folder with contents, when DELETE /fs?path=<folder> without permanent, then the folder and its contents are absent from the original location and GET /trash lists the folder."
	// catches: the folder or its contents stay at the original location, or the trash listing omits the folder.
	ops, root, trashDir := setupTrash(t)
	dir := filepath.Join(root, "folder")
	inner := filepath.Join(dir, "inner.txt")
	qa2specWrite(t, inner, "inside")

	res := ops.MoveToTrash([]string{dir}, trashDir)
	if len(res) != 1 || !res[0].OK {
		t.Fatalf("MoveToTrash folder: %+v, want one ok result", res)
	}
	if qa2specExists(dir) || qa2specExists(inner) {
		t.Errorf("folder or its contents still at the original location")
	}
	items := qa2specTrashItems(t, ops, trashDir)
	if len(items) != 1 || items[0].Name != "folder" || !items[0].IsDir || items[0].OriginalPath != dir {
		t.Errorf("GET /trash = %+v, want one folder entry for %s", items, dir)
	}
}

// TestQA2SpecFR4_PermanentDeleteFolderNotInTrash checks that permanent delete bypasses the trash.
func TestQA2SpecFR4_PermanentDeleteFolderNotInTrash(t *testing.T) {
	// FR-4: "Given a folder with contents, when DELETE /fs?path=<folder>&permanent=true, then the folder and its contents are absent and GET /trash does not list them."
	// Delete takes no trash directory, so the check is that the trash store named by the test stays empty.
	// catches: a permanent delete moves the folder into the trash, or leaves its contents behind.
	ops, root, trashDir := setupTrash(t)
	dir := filepath.Join(root, "folder")
	inner := filepath.Join(dir, "inner.txt")
	qa2specWrite(t, inner, "inside")

	res := ops.Delete([]string{dir})
	if len(res) != 1 || !res[0].OK {
		t.Fatalf("Delete (permanent): %+v, want one ok result", res)
	}
	if qa2specExists(dir) || qa2specExists(inner) {
		t.Errorf("folder or its contents still present after permanent delete")
	}
	if items := qa2specTrashItems(t, ops, trashDir); len(items) != 0 {
		t.Errorf("GET /trash = %+v after permanent delete, want nothing listed", items)
	}
}

// TestQA2SpecFR5_TrashRefusesPathOutsideJail checks the default delete outside the jail.
func TestQA2SpecFR5_TrashRefusesPathOutsideJail(t *testing.T) {
	// FR-5: "Given a path outside the device's jail, when DELETE /fs?path=<that path>, then the result carries code FORBIDDEN and the path is unchanged."
	// This is the trash (default delete) variant.
	// catches: an outside file is moved into the trash store.
	ops, _, trashDir := setupTrash(t)
	outside := filepath.Join(t.TempDir(), "keep.txt")
	qa2specWrite(t, outside, "keep")

	res := ops.MoveToTrash([]string{outside}, trashDir)
	if len(res) != 1 || res[0].OK || qa2specCode(res[0]) != "FORBIDDEN" {
		t.Errorf("MoveToTrash outside jail: %+v, want code FORBIDDEN", res)
	}
	if got := qa2specRead(t, outside); got != "keep" {
		t.Errorf("outside file changed: %q", got)
	}
	if items := qa2specTrashItems(t, ops, trashDir); len(items) != 0 {
		t.Errorf("trash holds %+v after a refused delete, want nothing", items)
	}
}

// TestQA2SpecFR6_EmptyTrashWithIdsPurgesOnlyNamed checks that an ids list purges only those ids.
func TestQA2SpecFR6_EmptyTrashWithIdsPurgesOnlyNamed(t *testing.T) {
	// FR-6: "Given the trash holds items A and B, when DELETE /trash with body ids=[A], then A is gone and B is still listed."
	// Device scope (open question 10) is not tested; both items sit inside the jail.
	// catches: every trash item is purged, or the ids are ignored.
	ops, root, trashDir := setupTrash(t)
	qa2specWrite(t, filepath.Join(root, "a.txt"), "A")
	qa2specWrite(t, filepath.Join(root, "b.txt"), "B")
	res := ops.MoveToTrash([]string{filepath.Join(root, "a.txt"), filepath.Join(root, "b.txt")}, trashDir)
	if len(res) != 2 || !res[0].OK || !res[1].OK {
		t.Fatalf("MoveToTrash: %+v, want two ok results", res)
	}
	idA := qa2specTrashIDs(t, ops, trashDir)["a.txt"]
	if idA == "" {
		t.Fatal("a.txt is not listed in the trash")
	}

	if err := ops.EmptyTrash(trashDir, []string{idA}); err != nil {
		t.Fatalf("EmptyTrash ids=[A]: %v", err)
	}
	items := qa2specTrashItems(t, ops, trashDir)
	if len(items) != 1 || items[0].Name != "b.txt" {
		t.Errorf("GET /trash after ids=[A] = %+v, want only b.txt", items)
	}
}

// TestQA2SpecFR6_EmptyTrashWithoutIdsPurgesAll checks that no ids empties the whole trash.
func TestQA2SpecFR6_EmptyTrashWithoutIdsPurgesAll(t *testing.T) {
	// FR-6: "Given the trash holds items A and B, when DELETE /trash with no body, then GET /trash lists nothing."
	// The package treats an empty ids list as no ids; the handler maps "no body" to that.
	// catches: items survive an empty-everything request.
	ops, root, trashDir := setupTrash(t)
	qa2specWrite(t, filepath.Join(root, "a.txt"), "A")
	qa2specWrite(t, filepath.Join(root, "b.txt"), "B")
	res := ops.MoveToTrash([]string{filepath.Join(root, "a.txt"), filepath.Join(root, "b.txt")}, trashDir)
	if len(res) != 2 || !res[0].OK || !res[1].OK {
		t.Fatalf("MoveToTrash: %+v, want two ok results", res)
	}

	if err := ops.EmptyTrash(trashDir, nil); err != nil {
		t.Fatalf("EmptyTrash (no ids): %v", err)
	}
	if items := qa2specTrashItems(t, ops, trashDir); len(items) != 0 {
		t.Errorf("GET /trash after empty = %+v, want nothing listed", items)
	}
}

// TestQA2SpecFR7_InvalidTrashIDDeletesNothing checks that an invalid id refuses the whole request.
func TestQA2SpecFR7_InvalidTrashIDDeletesNothing(t *testing.T) {
	// FR-7: "Given the trash holds item A, when DELETE /trash with a body naming an invalid trash id is sent, then the response is 400 and A is still listed."
	// The package returns ErrBadTrashID; the 400 mapping is server-side. The mixed case (a valid id with an invalid one) follows FR-7's "must delete nothing".
	// Body shapes ({}, malformed JSON) are HTTP-level and not tested here.
	// catches: a valid id is purged in a request that also names an invalid id, or an invalid id is ignored.
	ops, root, trashDir := setupTrash(t)
	qa2specWrite(t, filepath.Join(root, "a.txt"), "A")
	qa2specWrite(t, filepath.Join(root, "b.txt"), "B")
	res := ops.MoveToTrash([]string{filepath.Join(root, "a.txt"), filepath.Join(root, "b.txt")}, trashDir)
	if len(res) != 2 || !res[0].OK || !res[1].OK {
		t.Fatalf("MoveToTrash: %+v, want two ok results", res)
	}
	idA := qa2specTrashIDs(t, ops, trashDir)["a.txt"]
	if idA == "" {
		t.Fatal("a.txt is not listed in the trash")
	}

	if err := ops.EmptyTrash(trashDir, []string{"../escape"}); !errors.Is(err, ErrBadTrashID) {
		t.Errorf("EmptyTrash with invalid id only: err = %v, want ErrBadTrashID", err)
	}
	if n := len(qa2specTrashItems(t, ops, trashDir)); n != 2 {
		t.Errorf("after an invalid-id-only request %d items are listed, want 2", n)
	}

	if err := ops.EmptyTrash(trashDir, []string{idA, "../escape"}); !errors.Is(err, ErrBadTrashID) {
		t.Errorf("EmptyTrash with valid id plus invalid id: err = %v, want ErrBadTrashID", err)
	}
	if n := len(qa2specTrashItems(t, ops, trashDir)); n != 2 {
		t.Errorf("after a mixed request %d items are listed, want 2 (nothing deleted)", n)
	}
}

// TestQA2SpecFR8_RestoreFreePathRestoresInPlace checks restore to a free recorded path.
func TestQA2SpecFR8_RestoreFreePathRestoresInPlace(t *testing.T) {
	// FR-8: "Given trash id T whose recorded path P is free, when POST /trash/restore with ids=[T], then P holds the item, the BatchResult path is P, and T is no longer listed by GET /trash."
	// catches: the item is restored elsewhere, the BatchResult names another path, or T stays listed.
	ops, root, trashDir := setupTrash(t)
	src := filepath.Join(root, "doc.txt")
	qa2specWrite(t, src, "trashed")
	if res := ops.MoveToTrash([]string{src}, trashDir); len(res) != 1 || !res[0].OK {
		t.Fatalf("MoveToTrash: %+v, want one ok result", res)
	}
	id := qa2specTrashIDs(t, ops, trashDir)["doc.txt"]
	if id == "" {
		t.Fatal("doc.txt is not listed in the trash")
	}

	res := ops.RestoreFromTrash([]string{id}, trashDir)
	if len(res) != 1 || !res[0].OK {
		t.Fatalf("RestoreFromTrash: %+v, want one ok result", res)
	}
	if res[0].Path != src {
		t.Errorf("BatchResult path = %q, want %q", res[0].Path, src)
	}
	if got := qa2specRead(t, src); got != "trashed" {
		t.Errorf("restored bytes = %q, want %q", got, "trashed")
	}
	for _, it := range qa2specTrashItems(t, ops, trashDir) {
		if it.ID == id {
			t.Errorf("restored id %s is still listed by GET /trash", id)
		}
	}
}

// TestQA2SpecFR8_RestoreOccupiedPathKeepsOccupant checks restore when the recorded path is taken.
func TestQA2SpecFR8_RestoreOccupiedPathKeepsOccupant(t *testing.T) {
	// FR-8: "Given trash id T whose recorded path P is occupied by another entry, when POST /trash/restore with ids=[T], then the item is restored under a new name, the entry at P is unchanged, and the BatchResult path gives the new location."
	// The new name's format is not asserted; only that it is a new path in the same directory.
	// catches: the occupant is replaced, or the restored item lands on P.
	ops, root, trashDir := setupTrash(t)
	src := filepath.Join(root, "doc.txt")
	qa2specWrite(t, src, "trashed")
	if res := ops.MoveToTrash([]string{src}, trashDir); len(res) != 1 || !res[0].OK {
		t.Fatalf("MoveToTrash: %+v, want one ok result", res)
	}
	id := qa2specTrashIDs(t, ops, trashDir)["doc.txt"]
	if id == "" {
		t.Fatal("doc.txt is not listed in the trash")
	}
	qa2specWrite(t, src, "occupant")

	res := ops.RestoreFromTrash([]string{id}, trashDir)
	if len(res) != 1 || !res[0].OK {
		t.Fatalf("RestoreFromTrash onto occupied path: %+v, want one ok result", res)
	}
	if res[0].Path == src {
		t.Errorf("restore landed on the occupied path %s", src)
	}
	if filepath.Dir(res[0].Path) != root {
		t.Errorf("restored path %q is not in %s", res[0].Path, root)
	}
	if got := qa2specRead(t, res[0].Path); got != "trashed" {
		t.Errorf("restored bytes = %q, want %q", got, "trashed")
	}
	if got := qa2specRead(t, src); got != "occupant" {
		t.Errorf("occupant bytes = %q, want unchanged %q", got, "occupant")
	}
	for _, it := range qa2specTrashItems(t, ops, trashDir) {
		if it.ID == id {
			t.Errorf("restored id %s is still listed by GET /trash", id)
		}
	}
}
