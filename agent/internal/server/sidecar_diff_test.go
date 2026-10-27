package server

import (
	"container/heap"
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/ZaidQamhieh/rfe/agent/internal/fsops"
	"github.com/ZaidQamhieh/rfe/agent/internal/sidecar"
)

// indexdBinary locates a built rfe-indexd for the differential tests; they skip when there is none
// (build one with `cargo build --release -p rfe-indexd` in agent-rs, or set RFE_SIDECAR_DIR).
func indexdBinary(t *testing.T) string {
	t.Helper()
	if loc, ok, _ := locateSidecar("rfe-indexd"); ok {
		return loc.path
	}
	name := "rfe-indexd"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	for _, sub := range []string{"release", "debug"} {
		p := filepath.Join("..", "..", "..", "agent-rs", "target", sub, name)
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	t.Skip("rfe-indexd is not built")
	return ""
}

// diffFixture lays out a tree that exercises the walk rules: hidden and pruned directories, hidden files,
// absolute and relative symlinks, links to directories, broken links, links leaving the root.
func diffFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	base := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	n := 0
	put := func(rel string, size int) {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, make([]byte, size), 0o644); err != nil {
			t.Fatal(err)
		}
		n++
		mt := base.Add(time.Duration(n) * time.Hour)
		if err := os.Chtimes(p, mt, mt); err != nil {
			t.Fatal(err)
		}
	}
	for _, f := range []struct {
		rel  string
		size int
	}{
		{"photo.jpg", 1000}, {"Photo.PNG", 2000}, {"movie.mp4", 5000}, {"song.mp3", 500}, {"notes.txt", 10},
		{"archive.zip", 3000}, {"weird.xyz", 100}, {"noext", 7}, {".hidden-file", 3},
		{"a/b/c/deep.txt", 42}, {"a/b/photo2.jpg", 1200}, {"a/Zed.TXT", 9}, {"a/alpha.txt", 11},
		{"docs/readme.md", 300}, {"docs/spec.pdf", 90000}, {"docs/UPPER.DOC", 120},
		{".git/config", 5}, {"node_modules/pkg/index.js", 77}, {"a/.cache/x.bin", 8}, {"café/naïve.txt", 13},
		{"sp ace/file name.txt", 14}, {"unicode/日本語.txt", 15},
	} {
		put(f.rel, f.size)
	}
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	links := map[string]string{
		"link-abs-dir":     filepath.Join(root, "docs"),
		"link-rel-dir":     "docs",
		"link-abs-file":    filepath.Join(root, "photo.jpg"),
		"link-rel-file":    "a/alpha.txt",
		"link-broken":      "does-not-exist",
		"link-outside":     outside,
		"link-outside-fil": filepath.Join(outside, "secret.txt"),
		"a/link-up":        "../docs",
	}
	names := make([]string, 0, len(links))
	for name := range links {
		names = append(names, name)
	}
	sort.Strings(names)
	for i, name := range names {
		p := filepath.Join(root, filepath.FromSlash(name))
		if err := os.Symlink(links[name], p); err != nil {
			t.Skipf("symlinks unavailable: %v", err)
		}
		// Distinct mtimes: ties would make the order of equal entries differ between implementations.
		if err := setLinkTime(p, base.Add(time.Duration(100+i)*time.Hour)); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func diffQuery(t *testing.T, raw string) *searchFilters {
	t.Helper()
	q, err := url.ParseQuery(raw)
	if err != nil {
		t.Fatal(err)
	}
	f, code, msg := parseSearchFilters(q)
	if code != "" {
		t.Fatalf("%s: %s %s", raw, code, msg)
	}
	return f
}

func entryKey(e fsops.Entry) map[string]any {
	return map[string]any{
		"name": e.Name, "path": e.Path, "isDir": e.IsDir, "size": e.Size, "mime": e.MimeType, "mode": e.Mode,
		"mod": e.Modified.UnixNano(), "created": e.Created.UnixNano(), "isSymlink": e.IsSymlink, "target": e.SymlinkTarget,
	}
}

func keys(es []fsops.Entry) []map[string]any {
	out := make([]map[string]any, len(es))
	for i, e := range es {
		out[i] = entryKey(e)
	}
	return out
}

// goldenFixture materializes the tree the frozen goldens were generated from
// (agent-rs/crates/rfe-indexd/tests/data/index_golden.json, produced by the Go index before it was retired).
type goldenFixtureEntry struct {
	Rel     string `json:"rel"`
	Kind    string `json:"kind"`
	Size    int64  `json:"size"`
	Target  string `json:"target"`
	MtimeNs int64  `json:"mtimeNs"`
}

type goldenEntry struct {
	Path      string `json:"path"`
	IsDir     bool   `json:"isDir"`
	Size      int64  `json:"size"`
	MtimeNs   int64  `json:"mtimeNs"`
	IsSymlink bool   `json:"isSymlink"`
	Target    string `json:"target"`
}

type indexGolden struct {
	Fixture []goldenFixtureEntry `json:"fixture"`
	Queries []struct {
		Raw       string        `json:"raw"`
		Roots     []string      `json:"roots"`
		Limit     int           `json:"limit"`
		Truncated bool          `json:"truncated"`
		Entries   []goldenEntry `json:"entries"`
	} `json:"queries"`
}

func loadGolden(t *testing.T) indexGolden {
	t.Helper()
	b, err := os.ReadFile("../../../agent-rs/crates/rfe-indexd/tests/data/index_golden.json")
	if err != nil {
		t.Fatal(err)
	}
	var g indexGolden
	if err := json.Unmarshal(b, &g); err != nil {
		t.Fatal(err)
	}
	return g
}

func materializeGolden(t *testing.T, g indexGolden) (root, outside string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the golden fixture needs symlink timestamps")
	}
	base := t.TempDir()
	root, outside = filepath.Join(base, "root"), filepath.Join(base, "outside")
	for _, d := range []string{root, outside} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	sub := func(s string) string { return strings.NewReplacer("$ROOT", root, "$OUTSIDE", outside).Replace(s) }
	for _, f := range g.Fixture {
		if f.Kind == "dir" {
			if err := os.MkdirAll(filepath.Join(root, filepath.FromSlash(f.Rel)), 0o755); err != nil {
				t.Fatal(err)
			}
		}
	}
	for _, f := range g.Fixture {
		p := filepath.Join(root, filepath.FromSlash(f.Rel))
		switch f.Kind {
		case "file":
			if err := os.WriteFile(p, make([]byte, f.Size), 0o644); err != nil {
				t.Fatal(err)
			}
		case "link":
			if err := os.Symlink(sub(f.Target), p); err != nil {
				t.Skipf("symlinks unavailable: %v", err)
			}
		}
	}
	for _, f := range g.Fixture {
		if f.Kind == "dir" {
			continue
		}
		mt := time.Unix(0, f.MtimeNs)
		p := filepath.Join(root, filepath.FromSlash(f.Rel))
		if f.Kind == "link" {
			if err := setLinkTime(p, mt); err != nil {
				t.Fatal(err)
			}
		} else if err := os.Chtimes(p, mt, mt); err != nil {
			t.Fatal(err)
		}
	}
	for i := len(g.Fixture) - 1; i >= 0; i-- { // parents are listed before children: set children first
		if f := g.Fixture[i]; f.Kind == "dir" {
			mt := time.Unix(0, f.MtimeNs)
			if err := os.Chtimes(filepath.Join(root, filepath.FromSlash(f.Rel)), mt, mt); err != nil {
				t.Fatal(err)
			}
		}
	}
	return root, outside
}

// TestSidecarIndexMatchesFrozenGoIndex runs the whole Go adapter (wire types, root scoping, path re-check) against
// rfe-indexd and compares with the answers the retired Go index gave for the same tree.
func TestSidecarIndexMatchesFrozenGoIndex(t *testing.T) {
	bin := indexdBinary(t)
	g := loadGolden(t)
	root, outside := materializeGolden(t, g)
	sub := func(s string) string { return strings.NewReplacer("$ROOT", root, "$OUTSIDE", outside).Replace(s) }
	ops := fsops.New([]string{root}, false)

	si := newSidecarIndex(ops, sidecar.Config{Name: "rfe-indexd", Path: bin, Logf: t.Logf})
	t.Cleanup(si.sup.Stop)
	deadline := time.Now().Add(10 * time.Second)
	for {
		if _, _, ok := si.query(diffQuery(t, "q=photo"), []string{root}, 10); ok {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("sidecar index never became ready")
		}
		time.Sleep(20 * time.Millisecond)
	}
	if len(g.Queries) < 200 {
		t.Fatalf("golden looks truncated: %d queries", len(g.Queries))
	}
	for _, q := range g.Queries {
		roots := make([]string, len(q.Roots))
		for i, r := range q.Roots {
			roots[i] = sub(r)
		}
		got, trunc, ok := si.query(diffQuery(t, q.Raw), roots, q.Limit)
		allCovered := true
		for _, r := range roots {
			allCovered = allCovered && indexCovers([]string{root}, r)
		}
		if !allCovered {
			// An uncovered root is no longer answered (empty) from the index; the handler walks live.
			if ok || len(q.Entries) != 0 {
				t.Fatalf("%s roots=%v: ok=%v golden=%d entries, want decline of an uncovered root", q.Raw, roots, ok, len(q.Entries))
			}
			continue
		}
		if !ok {
			t.Fatalf("%s: sidecar not ready", q.Raw)
		}
		want := make([]goldenEntry, len(q.Entries))
		for i, e := range q.Entries {
			e.Path, e.Target = sub(e.Path), sub(e.Target)
			want[i] = e
		}
		have := make([]goldenEntry, len(got))
		for i, e := range got {
			size := e.Size
			if e.IsDir || e.IsSymlink {
				size = 0
			}
			have[i] = goldenEntry{Path: e.Path, IsDir: e.IsDir, Size: size, MtimeNs: e.Modified.UnixNano(), IsSymlink: e.IsSymlink, Target: e.SymlinkTarget}
		}
		if !reflect.DeepEqual(have, want) || trunc != q.Truncated {
			t.Errorf("%s roots=%v limit=%d truncated golden=%v sidecar=%v\n golden  %v\n sidecar %v", q.Raw, q.Roots, q.Limit, q.Truncated, trunc, paths2(want), paths2(have))
		}
	}
}

func paths2(es []goldenEntry) []string {
	out := make([]string, len(es))
	for i, e := range es {
		out[i] = e.Path
	}
	return out
}

func firstDiff(want, got []fsops.Entry) string {
	if len(want) != len(got) {
		return fmt.Sprintf("count go=%d sidecar=%d\n go      %v\n sidecar %v", len(want), len(got), paths(want), paths(got))
	}
	for i := range want {
		a, b := entryKey(want[i]), entryKey(got[i])
		for k := range a {
			if !reflect.DeepEqual(a[k], b[k]) {
				return fmt.Sprintf("%s field %s: go=%v sidecar=%v", want[i].Path, k, a[k], b[k])
			}
		}
	}
	return "equal"
}

func paths(es []fsops.Entry) []string {
	out := make([]string, len(es))
	for i, e := range es {
		out[i] = e.Path
	}
	return out
}

func TestSidecarRecentsMatchGoWalk(t *testing.T) {
	bin := indexdBinary(t)
	root := diffFixture(t)
	ops := fsops.New([]string{root}, false)
	sup := sidecar.NewSupervisor(sidecar.Config{Name: "rfe-indexd", Path: bin, Logf: t.Logf}, nil)
	sup.Run()
	t.Cleanup(sup.Stop)
	deadline := time.Now().Add(10 * time.Second)
	for {
		if _, err := sup.Client(); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("sidecar never started")
		}
		time.Sleep(20 * time.Millisecond)
	}
	walk := sidecarRecentWalker(sup)
	for _, limit := range []int{1, 5, 8, 100} {
		for _, r := range []string{root, filepath.Join(root, "a"), filepath.Join(root, "docs")} {
			var want, got recentHeap
			heap.Init(&want)
			heap.Init(&got)
			walkForRecentWithOps(context.Background(), ops, r, limit, &want)
			walk(context.Background(), ops, r, limit, &got)
			if !reflect.DeepEqual(keys(got.sortedNewestFirst()), keys(want.sortedNewestFirst())) {
				t.Errorf("limit %d root %s: %s", limit, r, firstDiff(want.sortedNewestFirst(), got.sortedNewestFirst()))
			}
		}
	}
}

// With a built, watched index the walker answers from recents.index; the result must still equal the Go walk's, a
// file created afterwards must show up without a rebuild, and a root outside the index must still be answered.
func TestSidecarRecentsFromLiveIndexMatchGoWalk(t *testing.T) {
	bin := indexdBinary(t)
	root := diffFixture(t)
	ops := fsops.New([]string{root}, false)
	sup := sidecar.NewSupervisor(sidecar.Config{Name: "rfe-indexd", Path: bin, Logf: t.Logf}, nil)
	sup.Run()
	t.Cleanup(sup.Stop)
	var c *sidecar.Client
	deadline := time.Now().Add(10 * time.Second)
	for {
		var err error
		if c, err = sup.Client(); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("sidecar never started")
		}
		time.Sleep(20 * time.Millisecond)
	}
	var built buildResp
	if err := c.Call(context.Background(), "index.build", buildReq{Roots: []string{root}, MaxEntries: 1_000_000, MaxBytes: 1 << 30}, &built); err != nil {
		t.Fatalf("index.build: %v", err)
	}
	if _, ok := watching(c); !ok {
		t.Skip("the file watcher could not be installed here")
	}

	// The index must be what answers, not the walk behind it.
	var direct recentsResp
	if err := c.Call(context.Background(), "recents.index", recentsReq{Roots: []string{root}, Limit: 5}, &direct); err != nil || !direct.Ready {
		t.Fatalf("recents.index ready=%v err=%v, want ready", direct.Ready, err)
	}

	walk := sidecarRecentWalker(sup)
	compare := func(label string) {
		t.Helper()
		for _, limit := range []int{1, 5, 8, 100} {
			for _, r := range []string{root, filepath.Join(root, "a"), filepath.Join(root, "docs")} {
				var want, got recentHeap
				heap.Init(&want)
				heap.Init(&got)
				walkForRecentWithOps(context.Background(), ops, r, limit, &want)
				walk(context.Background(), ops, r, limit, &got)
				if !reflect.DeepEqual(keys(got.sortedNewestFirst()), keys(want.sortedNewestFirst())) {
					t.Errorf("%s limit %d root %s: %s", label, limit, r, firstDiff(want.sortedNewestFirst(), got.sortedNewestFirst()))
				}
			}
		}
	}
	compare("built")

	fresh := filepath.Join(root, "docs", "created-after-build.txt")
	if err := os.WriteFile(fresh, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	until := time.Now().Add(10 * time.Second)
	for {
		var h recentHeap
		heap.Init(&h)
		walk(context.Background(), ops, root, 5, &h)
		found := false
		for _, e := range h {
			if e.Path == fresh {
				found = true
			}
		}
		if found {
			break
		}
		if time.Now().After(until) {
			t.Fatal("a file created after the build never reached recents")
		}
		time.Sleep(100 * time.Millisecond)
	}
	compare("after a change")

	// A root the index does not cover is declined by recents.index and the walker still answers it.
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "o.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	var declined recentsResp
	if err := c.Call(context.Background(), "recents.index", recentsReq{Roots: []string{outside}, Limit: 5}, &declined); err != nil || declined.Ready {
		t.Fatalf("outside root: ready=%v err=%v, want declined", declined.Ready, err)
	}
	var h recentHeap
	heap.Init(&h)
	walk(context.Background(), ops, outside, 5, &h)
	if h.Len() != 1 {
		t.Fatalf("outside root: walker found %d entries, want 1", h.Len())
	}
}

func TestSidecarFallsBackWhenUnavailable(t *testing.T) {
	ops := fsops.New([]string{t.TempDir()}, false)
	si := newSidecarIndex(ops, sidecar.Config{Name: "rfe-indexd", Path: "/nonexistent/rfe-indexd"})
	t.Cleanup(si.sup.Stop)
	if _, _, ok := si.query(diffQuery(t, "q=x"), ops.Roots(), 10); ok {
		t.Fatal("query reported ok with no sidecar")
	}
	// The recents walker must still produce the Go walk's result.
	dir := ops.Roots()[0]
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	var h recentHeap
	heap.Init(&h)
	sidecarRecentWalker(si.sup)(context.Background(), ops, dir, 5, &h)
	if h.Len() != 1 {
		t.Fatalf("fallback walk found %d entries, want 1", h.Len())
	}
}

func TestAcceptSidecarPath(t *testing.T) {
	root := filepath.FromSlash("/srv/data")
	scopes := prepareRootScopes([]string{root})
	if runtime.GOOS == "windows" {
		t.Skip("POSIX paths")
	}
	for p, want := range map[string]bool{
		"/srv/data":                true,
		"/srv/data/a.txt":          true,
		"/srv/data/a/../../etc/pw": false,
		"/srv/data/./a":            false,
		"/srv/data//a":             false,
		"/srv/data-evil/a":         false,
		"/srv/dat":                 false,
		"srv/data/a":               false,
		"":                         false,
		"/etc/passwd":              false,
	} {
		if got := acceptSidecarPath(p, scopes); got != want {
			t.Errorf("%q: got %v want %v", p, got, want)
		}
	}
}

func TestSearchSurvivesSidecarKilledMidFlight(t *testing.T) {
	bin := indexdBinary(t)
	root := diffFixture(t)
	ops := fsops.New([]string{root}, false)
	si := newSidecarIndex(ops, sidecar.Config{Name: "rfe-indexd", Path: bin, Logf: t.Logf})
	t.Cleanup(si.sup.Stop)

	search := func() []fsops.Entry {
		rr := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/v1/search?q=photo", nil)
		searchHandler(ops, si)(rr, req)
		if rr.Code != 200 {
			t.Fatalf("status %d: %s", rr.Code, rr.Body.String())
		}
		var out []fsops.Entry
		if err := json.Unmarshal(rr.Body.Bytes(), &out); err != nil {
			t.Fatal(err)
		}
		return out
	}
	waitFor := func(what string, cond func() bool) {
		t.Helper()
		deadline := time.Now().Add(15 * time.Second)
		for !cond() {
			if time.Now().After(deadline) {
				t.Fatalf("timed out waiting for %s", what)
			}
			time.Sleep(10 * time.Millisecond)
		}
	}
	waitFor("the first snapshot", func() bool {
		_, _, ok := si.query(diffQuery(t, "q=photo"), []string{root}, 10)
		return ok
	})
	want := paths(search())
	if len(want) == 0 {
		t.Fatal("no results from the sidecar index")
	}

	c, err := si.sup.Client()
	if err != nil {
		t.Fatal(err)
	}
	p, err := os.FindProcess(c.Pid())
	if err != nil {
		t.Fatal(err)
	}
	// Searches keep arriving while the process dies: none may fail or come back different.
	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			select {
			case <-stop:
				return
			default:
			}
			if got := paths(search()); !reflect.DeepEqual(got, want) {
				t.Errorf("search during the kill returned %v, want %v", got, want)
				return
			}
		}
	}()
	time.Sleep(30 * time.Millisecond)
	_ = p.Kill()
	waitFor("the sidecar to be noticed dead", func() bool { _, err := si.sup.Client(); return err != nil })
	waitFor("the supervisor to restart it and rebuild", func() bool {
		_, _, ok := si.query(diffQuery(t, "q=photo"), []string{root}, 10)
		return ok
	})
	close(stop)
	<-done
	if got := paths(search()); !reflect.DeepEqual(got, want) {
		t.Fatalf("after the restart got %v, want %v", got, want)
	}
}

func TestSidecarIndexSeesChangesWithoutARebuild(t *testing.T) {
	bin := indexdBinary(t)
	root := diffFixture(t)
	ops := fsops.New([]string{root}, false)
	si := newSidecarIndex(ops, sidecar.Config{Name: "rfe-indexd", Path: bin, Logf: t.Logf})
	t.Cleanup(si.sup.Stop)
	has := func(name string) bool {
		got, _, ok := si.query(diffQuery(t, "q="+name), []string{root}, 10)
		return ok && len(got) > 0
	}
	deadline := time.Now().Add(15 * time.Second)
	for !has("photo") {
		if time.Now().After(deadline) {
			t.Fatal("index never ready")
		}
		time.Sleep(20 * time.Millisecond)
	}
	c := mustClient(t, si)
	if _, ok := watching(c); !ok {
		t.Skip("file system watches are unavailable here")
	}
	if err := os.WriteFile(filepath.Join(root, "a", "zz-live-added.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	for !has("zz-live-added") {
		if time.Now().After(deadline) {
			t.Fatal("a file created after the build never showed up")
		}
		time.Sleep(50 * time.Millisecond)
	}
	if err := os.Remove(filepath.Join(root, "a", "zz-live-added.txt")); err != nil {
		t.Fatal(err)
	}
	for has("zz-live-added") {
		if time.Now().After(deadline) {
			t.Fatal("a deleted file never went away")
		}
		time.Sleep(50 * time.Millisecond)
	}
}
