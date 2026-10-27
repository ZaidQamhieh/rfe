package fsops

import (
	"archive/zip"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Audit F-05: compressing a folder into a zip inside that folder must not pack the
// half-written archive into itself.
func TestCompressIntoOwnFolderSkipsTempArchive(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "docs")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	ops := New([]string{root}, false)
	entry, err := ops.Compress([]string{dir}, filepath.Join(dir, "out.zip"))
	if err != nil {
		t.Fatalf("Compress: %v", err)
	}
	zr, err := zip.OpenReader(entry.Path)
	if err != nil {
		t.Fatal(err)
	}
	defer zr.Close()
	for _, f := range zr.File {
		if strings.Contains(f.Name, "rfe-tmp") {
			t.Fatalf("archive contains its own temp file: %s", f.Name)
		}
	}
}
