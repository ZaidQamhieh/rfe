package fsops

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Audit F-157: an entry that blows the expansion budget must not leave its partial file behind.
func TestWriteArchiveEntryRemovesPartialFileOverBudget(t *testing.T) {
	root := t.TempDir()
	ops := New([]string{root}, false)
	target, err := ops.access(filepath.Join(root, "bomb.bin"))
	if err != nil {
		t.Fatal(err)
	}
	defer target.close()
	remaining := int64(10)
	err = writeArchiveEntry(target, 0o644, strings.NewReader(strings.Repeat("x", 100)), &remaining, &extractLog{})
	if !errors.Is(err, ErrArchiveTooLarge) {
		t.Fatalf("err = %v, want ErrArchiveTooLarge", err)
	}
	if _, statErr := os.Lstat(filepath.Join(root, "bomb.bin")); !os.IsNotExist(statErr) {
		t.Fatalf("partial file left behind: %v", statErr)
	}
}
