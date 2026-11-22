package server

import (
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/ZaidQamhieh/rfe/agent/internal/fsops"
)

// twoRootFixture returns an Ops that may see two sibling temp directories, a and b, each holding one file.
func twoRootFixture(t *testing.T) (ops *fsops.Ops, a, b string) {
	t.Helper()
	a, b = t.TempDir(), t.TempDir()
	for _, p := range []string{filepath.Join(a, "inside.txt"), filepath.Join(b, "outside.txt")} {
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return fsops.New([]string{a, b}, false), a, b
}

// A path-form q must stay inside the requested root: an entry that the jail allows but that lies outside the
// searched root (absolute, or reached through "..") is not a hit.
func TestPathFormSearchHonoursRequestedRoot(t *testing.T) {
	ops, a, b := twoRootFixture(t)
	rel, err := filepath.Rel(a, filepath.Join(b, "outside.txt"))
	if err != nil {
		t.Fatal(err)
	}
	for name, q := range map[string]string{"absolute": filepath.Join(b, "outside.txt"), "dotdot": rel} {
		t.Run(name, func(t *testing.T) {
			rr, entries := doSearch(t, ops, "q="+url.QueryEscape(q)+"&root="+url.QueryEscape(a))
			if rr.Code != 200 {
				t.Fatalf("status %d", rr.Code)
			}
			if len(entries) != 0 {
				t.Fatalf("entry outside the requested root was returned: %+v", entries)
			}
		})
	}
	// Control: the same lookup inside the root still works.
	_, entries := doSearch(t, ops, "q="+url.QueryEscape(filepath.Join(a, "inside.txt"))+"&root="+url.QueryEscape(a))
	if len(entries) != 1 || entries[0].Name != "inside.txt" {
		t.Fatalf("got %+v, want inside.txt", entries)
	}
}

// Without a root param the search scope is the configured roots; an entry outside them is not returned either.
func TestPathFormSearchWithoutRootStaysInConfiguredRoots(t *testing.T) {
	a := t.TempDir()
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "o.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	// An unrestricted agent (no roots) searches the home directory; point it at a so the test needs no real home.
	t.Setenv("HOME", a)
	t.Setenv("USERPROFILE", a)
	ops := fsops.New(nil, false)
	_, entries := doSearch(t, ops, "q="+url.QueryEscape(filepath.Join(outside, "o.txt")))
	if len(entries) != 0 {
		t.Fatalf("entry outside the searched roots was returned: %+v", entries)
	}
}

// The live walk prunes hidden and dependency-cache directories; a path-form q must not reach into them.
func TestPathFormSearchDoesNotEnterPrunedDirs(t *testing.T) {
	root := t.TempDir()
	for _, rel := range []string{".hid/secret.txt", "node_modules/pkg/x.js", "vis/.dotfile", "vis/.cache/deep.txt", "vis/ok.txt"} {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	ops := fsops.New([]string{root}, false)
	search := func(q, rootParam string) []fsops.Entry {
		t.Helper()
		_, entries := doSearch(t, ops, "q="+url.QueryEscape(q)+"&root="+url.QueryEscape(rootParam))
		return entries
	}
	for _, q := range []string{".hid/secret.txt", "node_modules/pkg/x.js", "vis/.cache/deep.txt", ".hid", filepath.Join(root, ".hid", "secret.txt")} {
		if got := search(q, root); len(got) != 0 {
			t.Errorf("q=%q returned an entry the walk would prune: %+v", q, got)
		}
	}
	// Controls: what the walk returns is still found by path - a hidden file in a visible directory, a visible
	// file, and a path below an explicitly requested hidden root (the root itself is never pruned).
	for _, c := range []struct{ q, root, want string }{
		{"vis/.dotfile", root, ".dotfile"},
		{"vis/ok.txt", root, "ok.txt"},
	} {
		got := search(c.q, c.root)
		if len(got) != 1 || got[0].Name != c.want {
			t.Errorf("q=%q root=%q: got %+v, want %s", c.q, c.root, got, c.want)
		}
	}
	if got := search("x/../secret.txt", filepath.Join(root, ".hid")); len(got) != 1 || got[0].Name != "secret.txt" {
		t.Errorf("path below an explicit hidden root: got %+v", got)
	}
}

// A glob q is matched as a glob against names, never looked up as a literal path, even when it holds a separator.
func TestPathFormSearchSkipsGlobQueries(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("'*' is not valid in a Windows file name")
	}
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "d"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "d", "x*y.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	ops := fsops.New([]string{root}, false)
	_, entries := doSearch(t, ops, "q="+url.QueryEscape("d/x*y.txt")+"&root="+url.QueryEscape(root))
	if len(entries) != 0 {
		t.Fatalf("glob query was served as a literal path: %+v", entries)
	}
}

// The non-name filters still apply to a path-form hit.
func TestPathFormSearchAppliesFilters(t *testing.T) {
	ops, root := newSearchFixture(t)
	q := "Subfolder/nested.jpg"
	if _, got := doSearch(t, ops, "q="+url.QueryEscape(q)+"&root="+url.QueryEscape(root)); len(got) != 1 {
		t.Fatalf("no filter: got %+v", got)
	}
	if _, got := doSearch(t, ops, "q="+url.QueryEscape(q)+"&types=video&root="+url.QueryEscape(root)); len(got) != 0 {
		t.Fatalf("types=video kept an image: %+v", got)
	}
	if _, got := doSearch(t, ops, "q="+url.QueryEscape(q)+"&minSize=5000&root="+url.QueryEscape(root)); len(got) != 0 {
		t.Fatalf("minSize kept a 1500-byte file: %+v", got)
	}
}
