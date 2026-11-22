package search

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
	"unicode/utf16"

	"github.com/ZaidQamhieh/rfe/agent/internal/next/api"
	"github.com/ZaidQamhieh/rfe/agent/internal/next/files"
	"github.com/ZaidQamhieh/rfe/agent/internal/next/rootfs"
)

// protectedName is the folder inside the test root that is the agent's
// protected data folder. It has no leading dot so that only rootfs, not the
// hidden-folder rule, can keep it out of results.
const protectedName = "agentdata"

// env is a Service over a temp root that holds a protected folder.
type env struct {
	t       *testing.T
	root    string
	outside string
	prot    string
	set     *rootfs.Set
	svc     *Service
}

// newEnv builds the Set (one root, one protected folder inside it) and a
// Service; mod may change the Config.
func newEnv(t *testing.T, mod ...func(*Config)) *env {
	t.Helper()
	root := t.TempDir()
	e := &env{t: t, root: root, outside: t.TempDir(), prot: filepath.Join(root, protectedName)}
	e.mkdir(protectedName)
	e.write(filepath.Join(protectedName, "report-secret.txt"), "needle secret")
	set, err := rootfs.New([]string{root}, rootfs.Options{Protected: []string{e.prot}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { set.Close() })
	e.set = set
	cfg := Config{Roots: []string{root}}
	for _, m := range mod {
		m(&cfg)
	}
	e.svc = New(set, cfg)
	return e
}

func (e *env) mkdir(rel string) {
	e.t.Helper()
	if err := os.MkdirAll(filepath.Join(e.root, rel), 0o755); err != nil {
		e.t.Fatal(err)
	}
}

func (e *env) write(rel, content string) string {
	e.t.Helper()
	p := filepath.Join(e.root, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		e.t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		e.t.Fatal(err)
	}
	return p
}

// link makes root/rel a symlink to target; the test is skipped where symlinks
// cannot be made.
func (e *env) link(rel, target string) {
	e.t.Helper()
	if err := os.Symlink(target, filepath.Join(e.root, rel)); err != nil {
		e.t.Skipf("symlinks unavailable: %v", err)
	}
}

// search runs req and fails the test on error.
func (e *env) search(req Request) *Result {
	e.t.Helper()
	res, err := e.svc.Search(context.Background(), req)
	if err != nil {
		e.t.Fatalf("Search(%+v): %v", req, err)
	}
	return res
}

// rels returns the result paths relative to the root, with forward slashes.
func (e *env) rels(res *Result) []string {
	out := []string{}
	for _, en := range res.Entries {
		r, err := filepath.Rel(e.root, en.Path)
		if err != nil {
			e.t.Fatal(err)
		}
		out = append(out, filepath.ToSlash(r))
	}
	return out
}

func (e *env) names(req Request) []string {
	e.t.Helper()
	return e.rels(e.search(req))
}

func eq(t *testing.T, got, want []string) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got  %q\nwant %q", got, want)
	}
}

// reportTree creates the shared tree used by the name tests.
func reportTree(e *env) {
	e.write("a/b/report-final.pdf", "0123456789")
	e.write("docs/Report.TXT", "hello")
	e.write("docs/notes.md", "# notes")
	e.mkdir("reports")
	e.write(".hidden-report.txt", "x")
	e.write(".git/report-in-git.txt", "x")
	e.write("node_modules/report-pkg.js", "x")
}

func TestNameSearchSubstringCaseInsensitive(t *testing.T) {
	e := newEnv(t)
	reportTree(e)
	// Depth-first, names sorted per folder; hidden files match, hidden and
	// dependency folders are pruned, the protected folder is absent.
	want := []string{".hidden-report.txt", "a/b/report-final.pdf", "docs/Report.TXT", "reports"}
	eq(t, e.names(Request{Query: "report"}), want)
	eq(t, e.names(Request{Query: "  REPORT "}), want)
}

func TestNameSearchGlob(t *testing.T) {
	e := newEnv(t)
	reportTree(e)
	eq(t, e.names(Request{Query: "*.md"}), []string{"docs/notes.md"})
	eq(t, e.names(Request{Query: "REPORT*"}), []string{"a/b/report-final.pdf", "docs/Report.TXT", "reports"})
	eq(t, e.names(Request{Query: "notes.m?"}), []string{"docs/notes.md"})
	// A glob is matched against the whole name, not a part of it.
	eq(t, e.names(Request{Query: "port*"}), []string{})
	_, err := e.svc.Search(context.Background(), Request{Query: "a[*"})
	if !errors.Is(err, api.ErrBadRequest) {
		t.Fatalf("invalid glob: got %v, want ErrBadRequest", err)
	}
}

func TestNameSearchUnicodeFold(t *testing.T) {
	e := newEnv(t)
	e.write("\u00c4PFEL.txt", "x")
	eq(t, e.names(Request{Query: "\u00e4pfel"}), []string{"\u00c4PFEL.txt"})
}

func TestEntryFields(t *testing.T) {
	e := newEnv(t)
	e.write("docs/a.txt", "hello")
	res := e.search(Request{Query: "a.txt"})
	if len(res.Entries) != 1 {
		t.Fatalf("got %d entries", len(res.Entries))
	}
	en := res.Entries[0]
	if en.Name != "a.txt" || en.IsDir || en.Size != 5 || en.Path != filepath.Join(e.root, "docs", "a.txt") {
		t.Fatalf("entry = %+v", en)
	}
}

func TestExplicitHiddenRootIsSearched(t *testing.T) {
	e := newEnv(t)
	e.write(".git/report-in-git.txt", "x")
	res := e.search(Request{Query: "report", Root: filepath.Join(e.root, ".git")})
	if len(res.Entries) != 1 {
		t.Fatalf("hidden folder named as the root must be searched, got %d", len(res.Entries))
	}
}

func TestFilters(t *testing.T) {
	e := newEnv(t)
	files := map[string]struct {
		size int
		mod  time.Time
	}{
		"p.jpg":     {5, time.Date(2001, 1, 1, 0, 0, 0, 0, time.UTC)},
		"v.mp4":     {50, time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)},
		"d.pdf":     {500, time.Date(2021, 1, 1, 0, 0, 0, 0, time.UTC)},
		"notes.txt": {7, time.Date(2010, 1, 1, 0, 0, 0, 0, time.UTC)},
	}
	for name, f := range files {
		p := e.write(name, strings.Repeat("x", f.size))
		if err := os.Chtimes(p, f.mod, f.mod); err != nil {
			t.Fatal(err)
		}
	}
	e.mkdir("dir.d")
	old := time.Date(2010, 1, 1, 0, 0, 0, 0, time.UTC)
	if err := os.Chtimes(filepath.Join(e.root, "dir.d"), old, old); err != nil {
		t.Fatal(err)
	}
	ms := func(n int64) *int64 { return &n }
	at := func(y int) *time.Time { v := time.Date(y, 1, 1, 0, 0, 0, 0, time.UTC); return &v }
	cases := []struct {
		name string
		req  Request
		want []string
	}{
		{"all", Request{Query: "."}, []string{"d.pdf", "dir.d", "notes.txt", "p.jpg", "v.mp4"}},
		{"image", Request{Query: ".", Types: []string{"image"}}, []string{"p.jpg"}},
		{"folder", Request{Query: ".", Types: []string{"folder"}}, []string{"dir.d"}},
		{"two types", Request{Query: ".", Types: []string{"Image", " video "}}, []string{"p.jpg", "v.mp4"}},
		{"ext dot and case", Request{Query: ".", Exts: []string{".PDF"}}, []string{"d.pdf"}},
		{"min size drops folders", Request{Query: ".", MinSize: ms(40)}, []string{"d.pdf", "v.mp4"}},
		{"max size drops folders", Request{Query: ".", MaxSize: ms(10)}, []string{"notes.txt", "p.jpg"}},
		{"size range", Request{Query: ".", MinSize: ms(6), MaxSize: ms(100)}, []string{"notes.txt", "v.mp4"}},
		{"modified after", Request{Query: ".", ModifiedAfter: at(2015)}, []string{"d.pdf", "v.mp4"}},
		{"modified before", Request{Query: ".", ModifiedBefore: at(2005), Types: []string{"image"}}, []string{"p.jpg"}},
		{"types and ext are ANDed", Request{Query: ".", Types: []string{"document"}, Exts: []string{"txt"}}, []string{"notes.txt"}},
		{"glob and filter", Request{Query: "*.p*", Types: []string{"document"}}, []string{"d.pdf"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) { eq(t, e.names(c.req), c.want) })
	}
	_, err := e.svc.Search(context.Background(), Request{Query: ".", Types: []string{"bogus"}})
	if !errors.Is(err, api.ErrBadRequest) {
		t.Fatalf("unknown type: got %v", err)
	}
}

func TestQueryValidation(t *testing.T) {
	e := newEnv(t)
	for name, req := range map[string]Request{
		"empty":     {Query: ""},
		"blank":     {Query: "   "},
		"too long":  {Query: strings.Repeat("a", MaxQueryBytes+1)},
		"bad glob":  {Query: "[a?"},
		"bad type":  {Query: "x", Types: []string{"nope"}},
		"bad root":  {Query: "x", Root: "relative/path"},
		"no folder": {Query: "x", Root: filepath.Join(e.root, "missing")},
	} {
		_, err := e.svc.Search(context.Background(), req)
		want := api.ErrBadRequest
		if name == "no folder" {
			want = api.ErrPathNotFound
		}
		if !errors.Is(err, want) {
			t.Errorf("%s: got %v, want %v", name, err, want)
		}
	}
	e.write("f.txt", "x")
	_, err := e.svc.Search(context.Background(), Request{Query: "x", Root: filepath.Join(e.root, "f.txt")})
	if !errors.Is(err, api.ErrBadRequest) {
		t.Errorf("root that is a file: got %v", err)
	}
}

func TestContentSearch(t *testing.T) {
	e := newEnv(t)
	e.write("c/one.txt", "Hello NEEDLE world")
	e.write("c/two.txt", "nothing here")
	e.write("c/bin.dat", "\x00needle")
	e.write("c/big.txt", strings.Repeat("a", MaxContentFileBytes)+"needle")
	e.write("c/edge.txt", strings.Repeat("a", ContentChunkBytes-3)+"needle")
	e.write("c/star.txt", "a*b")
	e.write(".hid/hidden.txt", "needle")
	e.mkdir("c/sub")

	eq(t, e.names(Request{Query: "needle", Content: true}), []string{"c/edge.txt", "c/one.txt"})
	eq(t, e.names(Request{Query: "NeEdLe", Content: true, Exts: []string{"txt"}}), []string{"c/edge.txt", "c/one.txt"})
	eq(t, e.names(Request{Query: "needle", Content: true, Exts: []string{"dat"}}), []string{})
	eq(t, e.names(Request{Query: "needle", Content: true, MaxSize: func() *int64 { n := int64(100); return &n }()}), []string{"c/one.txt"})
	// In a content search a * is plain text, not a glob.
	eq(t, e.names(Request{Query: "a*b", Content: true}), []string{"c/star.txt"})
	// The protected folder's file holds the text but is never read.
	for _, p := range e.names(Request{Query: "secret", Content: true}) {
		t.Errorf("protected content returned: %s", p)
	}
}

func TestScopeAndJail(t *testing.T) {
	e := newEnv(t)
	e.write("jail/inside-note.txt", "needle")
	e.write("other/outside-note.txt", "needle")
	jail, err := e.set.Narrow(filepath.Join(e.root, "jail"))
	if err != nil {
		t.Fatal(err)
	}
	defer jail.Close()

	// Configured roots that the jail does not allow are left out.
	svc := New(jail, Config{Roots: []string{e.root}})
	res, err := svc.Search(context.Background(), Request{Query: "note"})
	if err != nil || len(res.Entries) != 0 {
		t.Fatalf("outside configured root: entries=%v err=%v", res, err)
	}
	// A configured root inside the jail is searched, and only it.
	svc = New(jail, Config{Roots: []string{filepath.Join(e.root, "jail")}})
	res, err = svc.Search(context.Background(), Request{Query: "note"})
	if err != nil || len(res.Entries) != 1 || res.Entries[0].Name != "inside-note.txt" {
		t.Fatalf("jail root: %v err=%v", res, err)
	}
	// A named root outside the jail is refused, by name or by content.
	for _, req := range []Request{
		{Query: "note", Root: filepath.Join(e.root, "other")},
		{Query: "needle", Root: e.root, Content: true},
	} {
		if _, err := svc.Search(context.Background(), req); !errors.Is(err, api.ErrForbidden) {
			t.Errorf("%+v: got %v, want ErrForbidden", req, err)
		}
	}
	// A path to a file outside as q finds nothing (it is a name query).
	res, err = svc.Search(context.Background(), Request{Query: filepath.Join(e.root, "other", "outside-note.txt")})
	if err != nil || len(res.Entries) != 0 {
		t.Fatalf("path as query: %v err=%v", res, err)
	}
}

func TestPathQueryDoesNotLeakOutsideRoot(t *testing.T) {
	e := newEnv(t)
	e.write("pics/cat.jpg", "x")
	e.mkdir("docs")
	for _, q := range []string{
		filepath.Join(e.root, "pics", "cat.jpg"),
		filepath.Join("..", "pics", "cat.jpg"),
	} {
		res := e.search(Request{Query: q, Root: filepath.Join(e.root, "docs")})
		if len(res.Entries) != 0 {
			t.Errorf("q=%q returned %v", q, e.rels(res))
		}
	}
}

func TestDenyAllSetFindsNothing(t *testing.T) {
	set, err := rootfs.New(nil, rootfs.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer set.Close()
	home := t.TempDir()
	if err := os.WriteFile(filepath.Join(home, "x.txt"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	svc := New(set, Config{HomeDir: func() (string, error) { return home, nil }})
	res, err := svc.Search(context.Background(), Request{Query: "x"})
	if err != nil || len(res.Entries) != 0 {
		t.Fatalf("deny-all: %v err=%v", res, err)
	}
	if _, err := svc.Search(context.Background(), Request{Query: "x", Root: home}); !errors.Is(err, api.ErrForbidden) {
		t.Fatalf("deny-all named root: %v", err)
	}
}

func TestHomeFolderIsTheDefaultRoot(t *testing.T) {
	e := newEnv(t)
	home := filepath.Join(e.root, "home")
	e.write("home/mine.txt", "x")
	e.write("elsewhere/mine.txt", "x")
	e.svc = New(e.set, Config{HomeDir: func() (string, error) { return home, nil }})
	res := e.search(Request{Query: "mine"})
	if len(res.Entries) != 1 || res.Entries[0].Path != filepath.Join(home, "mine.txt") {
		t.Fatalf("home default: %v", e.rels(res))
	}
}

func TestSymlinkEscapeNotFollowed(t *testing.T) {
	e := newEnv(t)
	if err := os.WriteFile(filepath.Join(e.outside, "secret-needle.txt"), []byte("needle"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(e.outside, "deep"), 0o755); err != nil {
		t.Fatal(err)
	}
	e.link("link-out", e.outside)
	e.write("docs/Report.TXT", "x")
	e.link("link-in", "docs")
	e.link("loop", ".")

	// Nothing below a link is searched: not by name, not by content.
	eq(t, e.names(Request{Query: "secret"}), []string{})
	eq(t, e.names(Request{Query: "needle", Content: true}), []string{})
	eq(t, e.names(Request{Query: "report"}), []string{"docs/Report.TXT"})
	eq(t, e.names(Request{Query: "deep"}), []string{})

	// The link itself is an entry, but it reports no type of an outside
	// target: it is not a folder (finding: the old walk followed the link).
	res := e.search(Request{Query: "link-out"})
	if len(res.Entries) != 1 || !res.Entries[0].IsSymlink || res.Entries[0].IsDir {
		t.Fatalf("link-out entry: %+v", res.Entries)
	}
	eq(t, e.names(Request{Query: "link-out", Types: []string{"folder"}}), []string{})
	eq(t, e.names(Request{Query: "link-out", Types: []string{"other"}}), []string{"link-out"})
	// A link to a folder inside the root is a navigable folder entry, still
	// not entered.
	eq(t, e.names(Request{Query: "link-in", Types: []string{"folder"}}), []string{"link-in"})
	eq(t, e.names(Request{Query: "loop"}), []string{"loop"})

	// A named root that is a link to the outside is refused.
	_, err := e.svc.Search(context.Background(), Request{Query: "secret", Root: filepath.Join(e.root, "link-out")})
	if !errors.Is(err, api.ErrForbidden) {
		t.Fatalf("root through escaping link: %v", err)
	}
}

func TestProtectedFolderNeverSearchedOrReturned(t *testing.T) {
	e := newEnv(t)
	e.write("note-report.txt", "needle")
	e.link("to-data", e.prot)
	e.link("to-secret", filepath.Join(e.prot, "report-secret.txt"))

	for _, req := range []Request{
		{Query: "report"},
		{Query: "secret"},
		{Query: "agentdata"},
		{Query: "*"},
		{Query: "needle", Content: true},
		{Query: "report", Root: e.root},
	} {
		for _, p := range e.names(req) {
			if strings.Contains(p, protectedName) || strings.Contains(p, "report-secret") || strings.HasPrefix(p, "to-") {
				t.Errorf("%+v returned %q", req, p)
			}
		}
	}
	eq(t, e.names(Request{Query: "report"}), []string{"note-report.txt"})
	// Starting inside the protected folder is refused.
	if _, err := e.svc.Search(context.Background(), Request{Query: "x", Root: e.prot}); !errors.Is(err, api.ErrForbidden) {
		t.Fatalf("root = protected folder: %v", err)
	}
	if _, err := e.svc.Search(context.Background(), Request{Query: "x", Root: filepath.Join(e.prot, "sub")}); err == nil {
		t.Fatal("root below the protected folder must fail")
	}
}

func TestResultCapAndPaging(t *testing.T) {
	e := newEnv(t)
	const total = MaxLimit + 10
	for i := 0; i < total; i++ {
		e.write(fmt.Sprintf("many/f%03d.dat", i), "")
	}
	res := e.search(Request{Query: "f"})
	if len(res.Entries) != DefaultLimit || !res.Truncated || res.NextCursor == nil {
		t.Fatalf("default limit: n=%d truncated=%v cursor=%v", len(res.Entries), res.Truncated, res.NextCursor)
	}
	res = e.search(Request{Query: "f", Limit: 100 * MaxLimit})
	if len(res.Entries) != MaxLimit || !res.Truncated || res.NextCursor == nil {
		t.Fatalf("max limit: n=%d truncated=%v", len(res.Entries), res.Truncated)
	}
	rest := e.search(Request{Query: "f", Limit: MaxLimit, Cursor: *res.NextCursor})
	if len(rest.Entries) != total-MaxLimit || rest.Truncated || rest.NextCursor != nil {
		t.Fatalf("last page: n=%d truncated=%v cursor=%v", len(rest.Entries), rest.Truncated, rest.NextCursor)
	}
	// Exactly a full page and nothing more is not truncated.
	exact := e.search(Request{Query: "f00", Limit: 10})
	if len(exact.Entries) != 10 || exact.Truncated || exact.NextCursor != nil {
		t.Fatalf("exact page: n=%d truncated=%v", len(exact.Entries), exact.Truncated)
	}
}

// pageAll follows cursors with the page size until the search ends.
func pageAll(t *testing.T, svc *Service, req Request, size int) []string {
	t.Helper()
	var out []string
	req.Limit = size
	for i := 0; i < 1000; i++ {
		res, err := svc.Search(context.Background(), req)
		if err != nil {
			t.Fatal(err)
		}
		for _, en := range res.Entries {
			out = append(out, en.Path)
		}
		if res.NextCursor == nil {
			if res.Truncated {
				t.Fatal("truncated result without a cursor")
			}
			return out
		}
		req.Cursor = *res.NextCursor
	}
	t.Fatal("paging did not end")
	return nil
}

func TestCursorPagingEqualsFullListing(t *testing.T) {
	e := newEnv(t)
	// Matching folders and files at several depths, so a page can end on a
	// folder, inside it, or after it.
	for _, p := range []string{
		"m1/m1a/m-x", "m1/m1a/other", "m1/m2", "m1/zz/m-deep/m-file", "m2/m3/m4", "m2/skip", "m5", "m6/m7/m8/m9",
	} {
		e.write(p, "x")
	}
	e.mkdir("m1/m1a/m-empty")
	full := pageAll(t, e.svc, Request{Query: "m"}, MaxLimit)
	if len(full) < 12 {
		t.Fatalf("fixture too small: %d results", len(full))
	}
	for size := 1; size <= len(full)+1; size++ {
		got := pageAll(t, e.svc, Request{Query: "m"}, size)
		if !reflect.DeepEqual(got, full) {
			t.Fatalf("page size %d:\ngot  %q\nwant %q", size, got, full)
		}
	}
}

func TestCursorPagingAcrossRoots(t *testing.T) {
	rootA, rootB := t.TempDir(), t.TempDir()
	set, err := rootfs.New([]string{rootA, rootB}, rootfs.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer set.Close()
	for _, p := range []string{
		filepath.Join(rootA, "x1"), filepath.Join(rootA, "d", "x2"), filepath.Join(rootB, "x3"), filepath.Join(rootB, "e", "x4"),
	} {
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	svc := New(set, Config{Roots: []string{rootA, rootB}})
	full := pageAll(t, svc, Request{Query: "x"}, MaxLimit)
	want := []string{
		filepath.Join(rootA, "d", "x2"), filepath.Join(rootA, "x1"), filepath.Join(rootB, "e", "x4"), filepath.Join(rootB, "x3"),
	}
	eq(t, full, want)
	for size := 1; size <= 4; size++ {
		eq(t, pageAll(t, svc, Request{Query: "x"}, size), want)
	}
	// A cursor made for one set of roots is refused for another.
	res, err := svc.Search(context.Background(), Request{Query: "x", Limit: 1})
	if err != nil || res.NextCursor == nil {
		t.Fatal(err)
	}
	_, err = svc.Search(context.Background(), Request{Query: "x", Root: rootB, Cursor: *res.NextCursor})
	if !errors.Is(err, api.ErrBadRequest) {
		t.Fatalf("foreign cursor: %v", err)
	}
}

func TestBadCursors(t *testing.T) {
	e := newEnv(t)
	e.write("a.txt", "x")
	for name, c := range map[string]string{
		"not base64": "!!!",
		"too long":   strings.Repeat("A", MaxCursorBytes+1),
		"no path":    "YQ", // base64 of "a": no separator, no path
	} {
		_, err := e.svc.Search(context.Background(), Request{Query: "a", Cursor: c})
		if !errors.Is(err, api.ErrBadRequest) {
			t.Errorf("%s: got %v", name, err)
		}
	}
}

func TestCancelAndBudget(t *testing.T) {
	e := newEnv(t)
	e.write("a.txt", "x")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := e.svc.Search(ctx, Request{Query: "a"}); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled context: %v", err)
	}

	// The time budget is not an error: the result is partial and flagged.
	e.svc = New(e.set, Config{Roots: []string{e.root}, Budget: time.Nanosecond})
	res := e.search(Request{Query: "a"})
	if !res.TimeBudget || res.Truncated {
		t.Fatalf("budget: TimeBudget=%v Truncated=%v", res.TimeBudget, res.Truncated)
	}
}

func TestBudgetResultContinuesFromCursor(t *testing.T) {
	e := newEnv(t)
	for i := 0; i < 5; i++ {
		e.write(fmt.Sprintf("d%d/f%d.txt", i, i), "x")
	}
	full := pageAll(t, e.svc, Request{Query: "f"}, MaxLimit)
	// Stop the walk after a few entries through a context that ends on the
	// third check, then continue from the cursor it gives.
	w := &walker{s: e.svc, ctx: &countdownCtx{Context: context.Background(), left: 5}, f: mustCompile(t, Request{Query: "f"}), limit: MaxLimit}
	starts, err := e.svc.startFolders("")
	if err != nil {
		t.Fatal(err)
	}
	part := w.run(starts, nil)
	if !part.TimeBudget || part.NextCursor == nil || len(part.Entries) == 0 || len(part.Entries) >= len(full) {
		t.Fatalf("partial result: %+v", part)
	}
	rest := e.search(Request{Query: "f", Cursor: *part.NextCursor})
	var got []string
	for _, en := range append(part.Entries, rest.Entries...) {
		got = append(got, en.Path)
	}
	eq(t, got, full)
}

// countdownCtx reports an error after Err has been called left times.
type countdownCtx struct {
	context.Context
	left int
}

func (c *countdownCtx) Err() error {
	if c.left <= 0 {
		return context.DeadlineExceeded
	}
	c.left--
	return nil
}

func mustCompile(t *testing.T, r Request) *filters {
	t.Helper()
	f, err := compile(r)
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func TestParseQuery(t *testing.T) {
	v := map[string][]string{
		"q": {" Photo* "}, "root": {"/r"}, "limit": {"7"}, "cursor": {"c"}, "content": {"true"},
		"types": {"image, video"}, "ext": {"jpg,.png"}, "minSize": {"1"}, "maxSize": {"9"},
		"modifiedAfter": {"2020-01-02T03:04:05Z"}, "modifiedBefore": {"2021-01-02T03:04:05Z"},
	}
	req, err := ParseQuery(v)
	if err != nil {
		t.Fatal(err)
	}
	if req.Query != "Photo*" || req.Root != "/r" || req.Limit != 7 || req.Cursor != "c" || !req.Content ||
		!reflect.DeepEqual(req.Types, []string{"image", " video"}) || !reflect.DeepEqual(req.Exts, []string{"jpg", ".png"}) ||
		*req.MinSize != 1 || *req.MaxSize != 9 || req.ModifiedAfter.Year() != 2020 || req.ModifiedBefore.Year() != 2021 {
		t.Fatalf("parsed %+v", req)
	}
	// A limit that is not a positive integer means the default.
	for _, l := range []string{"0", "-3", "abc", ""} {
		r, err := ParseQuery(map[string][]string{"q": {"x"}, "limit": {l}})
		if err != nil || r.Limit != 0 {
			t.Errorf("limit %q: %+v %v", l, r, err)
		}
	}
	for _, bad := range []map[string][]string{
		{"q": {"x"}, "minSize": {"1.5"}},
		{"q": {"x"}, "maxSize": {"big"}},
		{"q": {"x"}, "modifiedAfter": {"yesterday"}},
		{"q": {"x"}, "modifiedBefore": {"2020-01-02"}},
		{"q": {"x"}, "content": {"maybe"}},
	} {
		if _, err := ParseQuery(bad); !errors.Is(err, api.ErrBadRequest) {
			t.Errorf("%v: got %v", bad, err)
		}
	}
}

func TestCategoryForName(t *testing.T) {
	for name, want := range map[string]string{"a.JPG": "image", "b.mkv": "video", "c.flac": "audio", "d.PDF": "document", "e.7z": "archive", "f": "other", "g.xyz": "other"} {
		if got := CategoryForName(name, false); got != want {
			t.Errorf("%s: %s, want %s", name, got, want)
		}
	}
	if CategoryForName("x.jpg", true) != "folder" {
		t.Error("a folder is always folder")
	}
}

func TestContentSearchSnippet(t *testing.T) {
	e := newEnv(t)
	e.write("s/one.txt", "first line\r\nHéllo 😀 NEEDLE world\nlast line")
	e.write("s/long.txt", strings.Repeat("x", 400)+"needle"+strings.Repeat("y", 400))
	res := e.search(Request{Query: "needle", Content: true})
	got := map[string]*files.Snippet{}
	for _, en := range res.Entries {
		got[en.Name] = en.Snippet
	}
	one := got["one.txt"]
	if one == nil || one.Text != "Héllo 😀 NEEDLE world" {
		t.Fatalf("one.txt snippet = %+v", one)
	}
	units := utf16.Encode([]rune(one.Text))
	if m := string(utf16.Decode(units[one.Start : one.Start+one.Length])); m != "NEEDLE" {
		t.Errorf("match span = %q", m)
	}
	long := got["long.txt"]
	if long == nil || len(long.Text) > SnippetMaxBytes {
		t.Fatalf("long.txt snippet = %+v", long)
	}
	if long.Text[long.Start:long.Start+long.Length] != "needle" {
		t.Errorf("long match span = %q", long.Text[long.Start:long.Start+long.Length])
	}
	// A name search never carries a snippet.
	for _, en := range e.search(Request{Query: "one"}).Entries {
		if en.Snippet != nil {
			t.Errorf("name hit has a snippet: %+v", en)
		}
	}
}
