package fsops

// Pass-3 tests for the no-replace rename on every platform: an existing
// destination is refused and both entries stay as they were.

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

func pass3Write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func pass3Read(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestPass3NoReplaceRefusesExistingFileAndDir(t *testing.T) {
	ops, root := setupJail(t)
	pass3Write(t, filepath.Join(root, "src.txt"), "payload")
	pass3Write(t, filepath.Join(root, "dst.txt"), "occupant")
	pass3Write(t, filepath.Join(root, "sdir", "f"), "in-src-dir")
	pass3Write(t, filepath.Join(root, "ddir", "g"), "in-dst-dir")
	if err := os.MkdirAll(filepath.Join(root, "emptydir"), 0o755); err != nil {
		t.Fatal(err)
	}
	jailed := func(src, dst string) (*securePath, *securePath) {
		s, d, err := ops.accessPair(filepath.Join(root, src), filepath.Join(root, dst))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(s.close)
		return s, d
	}
	unjailed := func(src, dst string) (*securePath, *securePath) {
		return &securePath{full: filepath.Join(root, src)}, &securePath{full: filepath.Join(root, dst)}
	}
	type pair struct {
		name     string
		src, dst *securePath
	}
	var cases []pair
	for _, mk := range []struct {
		kind string
		fn   func(a, b string) (*securePath, *securePath)
	}{{"jailed", jailed}, {"unjailed", unjailed}} {
		s, d := mk.fn("src.txt", "dst.txt")
		cases = append(cases, pair{mk.kind + " file onto file", s, d})
		s, d = mk.fn("src.txt", "ddir")
		cases = append(cases, pair{mk.kind + " file onto folder", s, d})
		s, d = mk.fn("sdir", "ddir")
		cases = append(cases, pair{mk.kind + " folder onto non-empty folder", s, d})
		s, d = mk.fn("sdir", "emptydir")
		cases = append(cases, pair{mk.kind + " folder onto empty folder", s, d})
		s, d = mk.fn("sdir", "dst.txt")
		cases = append(cases, pair{mk.kind + " folder onto file", s, d})
	}
	for _, c := range cases {
		if err := c.src.renameNoReplaceTo(c.dst); !errors.Is(err, fs.ErrExist) {
			t.Errorf("%s: err = %v, want fs.ErrExist", c.name, err)
		}
	}
	if got := pass3Read(t, filepath.Join(root, "src.txt")); got != "payload" {
		t.Errorf("src.txt = %q", got)
	}
	if got := pass3Read(t, filepath.Join(root, "dst.txt")); got != "occupant" {
		t.Errorf("dst.txt replaced: %q", got)
	}
	if got := pass3Read(t, filepath.Join(root, "sdir", "f")); got != "in-src-dir" {
		t.Errorf("sdir/f = %q", got)
	}
	if got := pass3Read(t, filepath.Join(root, "ddir", "g")); got != "in-dst-dir" {
		t.Errorf("ddir/g = %q", got)
	}
	if ents, err := os.ReadDir(filepath.Join(root, "emptydir")); err != nil || len(ents) != 0 {
		t.Errorf("emptydir = %v, %v; want still empty", ents, err)
	}
}

func TestPass3NoReplaceFreeDestinationRenames(t *testing.T) {
	dir := t.TempDir()
	pass3Write(t, filepath.Join(dir, "a.txt"), "A")
	pass3Write(t, filepath.Join(dir, "d", "f"), "F")
	if err := (&securePath{full: filepath.Join(dir, "a.txt")}).renameNoReplaceTo(&securePath{full: filepath.Join(dir, "b.txt")}); err != nil {
		t.Fatal(err)
	}
	if err := (&securePath{full: filepath.Join(dir, "d")}).renameNoReplaceTo(&securePath{full: filepath.Join(dir, "e")}); err != nil {
		t.Fatal(err)
	}
	if got := pass3Read(t, filepath.Join(dir, "b.txt")); got != "A" {
		t.Errorf("b.txt = %q", got)
	}
	if got := pass3Read(t, filepath.Join(dir, "e", "f")); got != "F" {
		t.Errorf("e/f = %q", got)
	}
	for _, gone := range []string{"a.txt", "d"} {
		if _, err := os.Lstat(filepath.Join(dir, gone)); !os.IsNotExist(err) {
			t.Errorf("%s still exists: %v", gone, err)
		}
	}
}
