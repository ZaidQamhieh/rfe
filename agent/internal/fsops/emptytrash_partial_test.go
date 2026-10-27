//go:build unix

package fsops

import (
	"os"
	"path/filepath"
	"testing"
)

// F-159: one item that cannot be deleted must not stop the rest of the batch.
func TestEmptyTrash_ContinuesPastAFailure(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permissions")
	}
	ops, root, trashDir := setupTrash(t)
	hard := filepath.Join(root, "hard")
	easy := filepath.Join(root, "easy.txt")
	if err := os.MkdirAll(hard, 0o755); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(filepath.Join(hard, "f"), []byte("x"), 0o644)
	_ = os.WriteFile(easy, []byte("y"), 0o644)
	if r := ops.MoveToTrash([]string{hard, easy}, trashDir); !r[0].OK || !r[1].OK {
		t.Fatalf("trash: %+v", r)
	}
	items, _ := ListTrash(trashDir)
	var hardID string
	var ids []string
	for _, it := range items {
		ids = append(ids, it.ID)
		if it.Name == "hard" {
			hardID = it.ID
		}
	}
	locked := filepath.Join(trashFilesDir(trashDir), hardID)
	if err := os.Chmod(locked, 0o500); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(locked, 0o755)
	err := ops.EmptyTrash(trashDir, ids)
	if err == nil {
		t.Fatal("the stubborn item should be reported")
	}
	left, _ := ListTrash(trashDir)
	if len(left) != 1 || left[0].ID != hardID {
		t.Fatalf("only the stubborn item may remain, got %+v", left)
	}
}
