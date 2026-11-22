package search

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/ZaidQamhieh/rfe/agent/internal/next/rootfs"
)

// fakeIndexer is an Indexer whose answer a test supplies; it records queries.
type fakeIndexer struct {
	mu    sync.Mutex
	calls []IndexQuery
	fn    func(q IndexQuery) (IndexResult, error)
}

func (f *fakeIndexer) Query(_ context.Context, q IndexQuery) (IndexResult, error) {
	f.mu.Lock()
	f.calls = append(f.calls, q)
	f.mu.Unlock()
	return f.fn(q)
}

func (f *fakeIndexer) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

// ordered answers with the given paths, cut at the query's limit the way the
// sidecar does.
func ordered(paths ...string) func(IndexQuery) (IndexResult, error) {
	return func(q IndexQuery) (IndexResult, error) {
		res := IndexResult{Ready: true}
		for i, p := range paths {
			if i >= q.Limit {
				res.Truncated = true
				break
			}
			res.Entries = append(res.Entries, IndexEntry{Path: p})
		}
		return res, nil
	}
}

// xTree makes root/x1..x5 and returns their paths.
func xTree(e *env) []string {
	var out []string
	for _, n := range []string{"x1", "x2", "x3", "x4", "x5"} {
		out = append(out, e.write(n, "x"))
	}
	return out
}

func withIndexer(idx Indexer, notes *[]string) func(*Config) {
	return func(c *Config) {
		c.Indexer = idx
		if notes != nil {
			c.Note = func(ev string) { *notes = append(*notes, ev) }
		}
	}
}

func TestIndexerServesFirstPageAndWalkContinues(t *testing.T) {
	var notes []string
	idx := &fakeIndexer{}
	e := newEnv(t, withIndexer(idx, &notes))
	paths := xTree(e)
	idx.fn = ordered(paths...)

	res := e.search(Request{Query: "X", Limit: 2})
	eq(t, e.rels(res), []string{"x1", "x2"})
	if !res.Truncated || res.NextCursor == nil {
		t.Fatalf("first page: truncated=%v cursor=%v", res.Truncated, res.NextCursor)
	}
	if idx.count() != 1 {
		t.Fatalf("index calls = %d, want 1", idx.count())
	}
	q := idx.calls[0]
	if q.Limit != 3 || !reflect.DeepEqual(q.Roots, []string{e.root}) || q.Filters.Needle != "x" || q.Filters.Glob != "" {
		t.Fatalf("index query = %+v", q)
	}
	// The cursor of an index page continues in the walk and joins up exactly.
	rest := e.search(Request{Query: "X", Limit: 10, Cursor: *res.NextCursor})
	eq(t, e.rels(rest), []string{"x3", "x4", "x5"})
	if idx.count() != 1 {
		t.Fatalf("a cursor request must not use the index (calls = %d)", idx.count())
	}
	if len(notes) != 0 {
		t.Fatalf("unexpected events %v", notes)
	}
}

func TestIndexerAnswerIsUsedNotTheWalk(t *testing.T) {
	idx := &fakeIndexer{}
	e := newEnv(t, withIndexer(idx, nil))
	paths := xTree(e)
	idx.fn = ordered(paths[1])
	eq(t, e.names(Request{Query: "x"}), []string{"x2"})
}

func TestIndexerNotUsedForContentOrWhenFilterChanges(t *testing.T) {
	idx := &fakeIndexer{fn: ordered()}
	e := newEnv(t, withIndexer(idx, nil))
	xTree(e)
	eq(t, e.names(Request{Query: "x", Content: true}), []string{"x1", "x2", "x3", "x4", "x5"})
	if idx.count() != 0 {
		t.Fatalf("content search asked the index")
	}
}

func TestIndexerFiltersOnTheWire(t *testing.T) {
	idx := &fakeIndexer{fn: ordered()}
	e := newEnv(t, withIndexer(idx, nil))
	after := time.Date(2020, 1, 2, 3, 4, 5, 6, time.UTC)
	var min int64 = 3
	e.search(Request{Query: "X*Y", Types: []string{"video", "Image"}, Exts: []string{".MP4"}, MinSize: &min, ModifiedAfter: &after})
	if idx.count() != 1 {
		t.Fatalf("calls = %d", idx.count())
	}
	f := idx.calls[0].Filters
	if f.Glob != "x*y" || f.Needle != "" || !reflect.DeepEqual(f.Types, []string{"image", "video"}) || !reflect.DeepEqual(f.Exts, []string{"mp4"}) ||
		f.MinSize == nil || *f.MinSize != 3 || f.MaxSize != nil || f.ModAfterNs == nil || *f.ModAfterNs != after.UnixNano() || f.ModBeforeNs != nil {
		t.Fatalf("filters = %+v", f)
	}
}

func TestIndexResultsAreRevalidated(t *testing.T) {
	idx := &fakeIndexer{}
	e := newEnv(t, withIndexer(idx, nil))
	good1 := e.write("x1", "x")
	good2 := e.write("sub/x9", "x")
	inProt := e.write(filepath.Join(protectedName, "x-in-prot"), "x")
	inHidden := e.write(".git/x-git", "x")
	notMatching := e.write("plain.dat", "x")
	outsidePath := filepath.Join(e.outside, "x-outside")
	if err := os.WriteFile(outsidePath, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	e.link("link", e.outside)
	viaLink := filepath.Join(e.root, "link", "x-outside")

	idx.fn = ordered(
		good1,
		outsidePath, // outside the root
		viaLink,     // through a symlink that leaves the root
		inProt,      // protected folder
		inHidden,    // folder the walk prunes
		notMatching, // does not match the query
		filepath.Join(e.root, "missing", "x-gone"), // does not exist
		"x1", // relative
		e.root+string(filepath.Separator)+".."+string(filepath.Separator)+filepath.Base(e.root)+string(filepath.Separator)+"x1", // unclean
		e.root, // the root itself
		good2,
	)
	eq(t, e.names(Request{Query: "x"}), []string{"x1", "sub/x9"})
}

func TestIndexerFallbacks(t *testing.T) {
	cases := map[string]struct {
		fn    func(IndexQuery) (IndexResult, error)
		event string
	}{
		"error":     {func(IndexQuery) (IndexResult, error) { return IndexResult{}, errors.New("sidecar died") }, EventIndexFallback},
		"not ready": {func(IndexQuery) (IndexResult, error) { return IndexResult{}, ErrIndexNotReady }, EventIndexNotReady},
		"ready false": {func(IndexQuery) (IndexResult, error) {
			return IndexResult{Ready: false}, nil
		}, EventIndexNotReady},
		"cut by its own budget": {func(IndexQuery) (IndexResult, error) {
			return IndexResult{Ready: true, Truncated: true, Entries: []IndexEntry{}}, nil
		}, EventIndexFallback},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			var notes []string
			idx := &fakeIndexer{fn: c.fn}
			e := newEnv(t, withIndexer(idx, &notes))
			xTree(e)
			eq(t, e.names(Request{Query: "x"}), []string{"x1", "x2", "x3", "x4", "x5"})
			if idx.count() != 1 {
				t.Fatalf("index calls = %d", idx.count())
			}
			eq(t, notes, []string{c.event})
		})
	}
}

func TestIndexDroppedEntriesFallBackToWalk(t *testing.T) {
	// The index says "limit reached" but half of its entries are invalid, so
	// matches after them could be missing: the walk answers.
	var notes []string
	idx := &fakeIndexer{}
	e := newEnv(t, withIndexer(idx, &notes))
	paths := xTree(e)
	idx.fn = func(q IndexQuery) (IndexResult, error) {
		res := IndexResult{Ready: true, Truncated: true}
		for range q.Limit {
			res.Entries = append(res.Entries, IndexEntry{Path: filepath.Join(e.root, "nope")})
		}
		res.Entries[0] = IndexEntry{Path: paths[0]}
		return res, nil
	}
	eq(t, e.names(Request{Query: "x", Limit: 2}), []string{"x1", "x2"})
	eq(t, notes, []string{EventIndexFallback})
}

func TestIndexerCancelIsNotAFallback(t *testing.T) {
	var notes []string
	ctx, cancel := context.WithCancel(context.Background())
	idx := &fakeIndexer{fn: func(IndexQuery) (IndexResult, error) {
		cancel()
		return IndexResult{}, context.Canceled
	}}
	e := newEnv(t, withIndexer(idx, &notes))
	xTree(e)
	if _, err := e.svc.Search(ctx, Request{Query: "x"}); !errors.Is(err, context.Canceled) {
		t.Fatalf("got %v, want context.Canceled", err)
	}
	if len(notes) != 0 {
		t.Fatalf("events %v", notes)
	}
}

func TestIndexerAcrossRoots(t *testing.T) {
	idx := &fakeIndexer{}
	rootA, rootB := t.TempDir(), t.TempDir()
	set, err := rootfs.New([]string{rootA, rootB}, rootfs.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer set.Close()
	both := New(set, Config{Roots: []string{rootA, rootB}, Indexer: idx})
	a, b := filepath.Join(rootA, "x1"), filepath.Join(rootB, "x2")
	for _, p := range []string{a, b} {
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	byRoot := map[string]string{rootA: a, rootB: b}
	idx.fn = func(q IndexQuery) (IndexResult, error) {
		return ordered(byRoot[q.Roots[0]])(q)
	}
	res, err := both.Search(context.Background(), Request{Query: "x", Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Entries) != 1 || res.Entries[0].Path != a || !res.Truncated || res.NextCursor == nil {
		t.Fatalf("page 1: %+v", res)
	}
	// The second root is asked only for what is still needed (limit minus the
	// first root's hits, plus one to learn whether more exist).
	res, err = both.Search(context.Background(), Request{Query: "x", Limit: 5})
	if err != nil || len(res.Entries) != 2 || res.Entries[1].Path != b {
		t.Fatalf("both roots: %+v err=%v", res, err)
	}
	if idx.count() != 4 || idx.calls[2].Limit != 6 || idx.calls[3].Limit != 5 {
		t.Fatalf("calls = %+v", idx.calls)
	}
}

type fakeCaller struct {
	op     string
	req    []byte
	answer string
	err    error
}

func (c *fakeCaller) Call(_ context.Context, op string, req, resp any) error {
	c.op = op
	c.req, _ = json.Marshal(req)
	if c.err != nil {
		return c.err
	}
	return json.Unmarshal([]byte(c.answer), resp)
}

func TestSidecarIndexerWire(t *testing.T) {
	c := &fakeCaller{answer: `{"ready":true,"truncated":true,"entries":[{"path":"/r/a","name":"a","size":1},{"path":"/r/b"}]}`}
	min := int64(2)
	res, err := SidecarIndexer{Caller: c}.Query(context.Background(), IndexQuery{
		Filters: IndexFilters{Needle: "a", MinSize: &min}, Roots: []string{"/r"}, Limit: 7,
	})
	if err != nil {
		t.Fatal(err)
	}
	if c.op != "index.query" {
		t.Fatalf("op = %q", c.op)
	}
	if !res.Truncated || len(res.Entries) != 2 || res.Entries[1].Path != "/r/b" {
		t.Fatalf("result = %+v", res)
	}
	var wire map[string]json.RawMessage
	if err := json.Unmarshal(c.req, &wire); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"filters", "roots", "limit"} {
		if _, ok := wire[k]; !ok {
			t.Errorf("request lacks %q: %s", k, c.req)
		}
	}
	var filt map[string]json.RawMessage
	if err := json.Unmarshal(wire["filters"], &filt); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"glob", "needle", "types", "exts", "minSize", "maxSize", "modAfterNs", "modBeforeNs"} {
		if _, ok := filt[k]; !ok {
			t.Errorf("filters lack %q: %s", k, wire["filters"])
		}
	}
	if string(filt["minSize"]) != "2" || string(filt["maxSize"]) != "null" {
		t.Errorf("sizes = %s %s", filt["minSize"], filt["maxSize"])
	}

	c = &fakeCaller{answer: `{"ready":false,"entries":[]}`}
	if _, err := (SidecarIndexer{Caller: c}).Query(context.Background(), IndexQuery{}); !errors.Is(err, ErrIndexNotReady) {
		t.Fatalf("not ready: %v", err)
	}
	boom := errors.New("boom")
	c = &fakeCaller{err: boom}
	if _, err := (SidecarIndexer{Caller: c}).Query(context.Background(), IndexQuery{}); !errors.Is(err, boom) {
		t.Fatalf("call error: %v", err)
	}
}
