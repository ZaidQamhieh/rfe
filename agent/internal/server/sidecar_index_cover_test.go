package server

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ZaidQamhieh/rfe/agent/internal/fsops"
)

// A ready sidecar index must not answer for a root it does not cover (outside the indexed roots, or under a
// pruned hidden directory); query must decline so the handler walks live. The nil supervisor makes any attempt
// to ask the sidecar fail loudly.
func TestSidecarIndexQueryDeclinesUncoveredRoot(t *testing.T) {
	root := t.TempDir()
	hidden := filepath.Join(root, ".cfg")
	if err := os.Mkdir(hidden, 0o755); err != nil {
		t.Fatal(err)
	}
	s := &sidecarIndex{ops: fsops.New([]string{root}, false)}
	for _, r := range []string{hidden, filepath.Join(hidden, "sub"), t.TempDir()} {
		if _, _, ok := s.query(&searchFilters{}, []string{r}, 10); ok {
			t.Fatalf("query(%s) ok=true, want decline", r)
		}
	}
	if !indexCovers([]string{root}, filepath.Join(root, "docs")) || !indexCovers([]string{root}, root) {
		t.Fatal("indexed root and visible subdirectory must be covered")
	}
}

// Coverage is judged against the roots the index was built with: after the owner widens the jail, a root that
// is only covered by the new roots must still be walked live until the index is rebuilt.
func TestSidecarIndexCoverageUsesTheBuiltRoots(t *testing.T) {
	built, widened := t.TempDir(), t.TempDir()
	s := &sidecarIndex{ops: fsops.New([]string{built, widened}, false), built: []string{built}}
	if _, _, ok := s.query(&searchFilters{}, []string{filepath.Join(widened, "docs")}, 10); ok {
		t.Fatal("a root added after the build must decline")
	}
}

// The index compares paths exactly, so a root that differs only in case is not covered.
func TestIndexCoversIsCaseSensitive(t *testing.T) {
	if indexCovers([]string{"/data/Photos"}, "/data/photos/2024") {
		t.Fatal("different case must not count as covered")
	}
	if !indexCovers([]string{"/data/Photos/"}, "/data/Photos/2024") {
		t.Fatal("trailing slash on the indexed root must still cover its children")
	}
}
