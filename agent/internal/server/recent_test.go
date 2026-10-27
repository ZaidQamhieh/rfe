package server

import (
	"container/heap"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"sync/atomic"
	"time"

	"testing"

	"github.com/ZaidQamhieh/rfe/agent/internal/fsops"
)

// doRecent invokes recentHandler directly with the given raw query string
// (without the leading '?') and decodes the response.
func doRecent(t *testing.T, ops *fsops.Ops, rawQuery string) (*httptest.ResponseRecorder, []fsops.Entry) {
	t.Helper()
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v1/fs/recent?"+rawQuery, nil)
	recentHandler(ops)(rr, req)

	var entries []fsops.Entry
	if rr.Code == http.StatusOK {
		if err := json.Unmarshal(rr.Body.Bytes(), &entries); err != nil {
			t.Fatalf("decode body as bare array: %v\nbody: %s", err, rr.Body.String())
		}
	}
	return rr, entries
}

// TestRecentHandler_OrdersNewestFirst uses the shared search fixture (see
// newSearchFixture in search_test.go): 7 files at the fixture root with
// mtimes 2020..2025, plus a nested.jpg (2023) one level deeper.
func TestRecentHandler_OrdersNewestFirst(t *testing.T) {
	ops, _ := newSearchFixture(t)
	rr, entries := doRecent(t, ops, "limit=3")
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	if len(entries) != 3 {
		t.Fatalf("expected 3 entries, got %d: %v", len(entries), names(entries))
	}
	got := names(entries)
	want := []string{"weird.xyz", "archive.zip", "notes.txt"} // 2025, 2024-12, 2024-01
	for i, w := range want {
		if got[i] != w {
			t.Fatalf("position %d: want %q, got %q (full: %v)", i, w, got[i], got)
		}
	}
}

// TestRecentHandler_ExcludesDirectories verifies Subfolder itself never
// appears in results — recent is a files-only feature.
func TestRecentHandler_ExcludesDirectories(t *testing.T) {
	ops, _ := newSearchFixture(t)
	_, entries := doRecent(t, ops, "limit=100")
	if containsName(names(entries), "Subfolder") {
		t.Fatalf("directory should never appear in recent results: %v", names(entries))
	}
	// All 8 files (7 at root + nested.jpg) should be present when the limit
	// comfortably exceeds the fixture's file count.
	if len(entries) != 8 {
		t.Fatalf("expected 8 files, got %d: %v", len(entries), names(entries))
	}
}

// TestRecentHandler_LimitCapsResultCount verifies the top-K heap actually
// keeps only the most recent `limit` entries, not just any `limit` entries.
func TestRecentHandler_LimitCapsResultCount(t *testing.T) {
	ops, _ := newSearchFixture(t)
	_, entries := doRecent(t, ops, "limit=1")
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	if entries[0].Name != "weird.xyz" {
		t.Fatalf("expected the single most recent file (weird.xyz, 2025-05-05), got %q", entries[0].Name)
	}
}

// TestRecentHandler_RootParamScoped verifies the `root` param restricts the
// walk to that subtree (here, Subfolder — containing only nested.jpg).
func TestRecentHandler_RootParamScoped(t *testing.T) {
	ops, dir := newSearchFixture(t)
	_, entries := doRecent(t, ops, "root="+dir+"/Subfolder&limit=100")
	if len(entries) != 1 || entries[0].Name != "nested.jpg" {
		t.Fatalf("expected only nested.jpg scoped to Subfolder, got %v", names(entries))
	}
}

func TestRecentHandler_DefaultLimit(t *testing.T) {
	ops, _ := newSearchFixture(t)
	rr, entries := doRecent(t, ops, "")
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	if len(entries) != 8 {
		t.Fatalf("expected all 8 files under the default limit, got %d", len(entries))
	}
}

func TestRecentHandler_CachesCompleteResultsByJailAndLimit(t *testing.T) {
	rootA := t.TempDir()
	rootB := t.TempDir()
	ops := fsops.New([]string{rootA, rootB}, false)
	var walks atomic.Int32
	handler := recentHandlerWithWalker(ops, func(_ context.Context, _ *fsops.Ops, root string, _ int, h *recentHeap) {
		walks.Add(1)
		heap.Push(h, fsops.Entry{
			Name:     filepath.Base(root),
			Path:     filepath.Join(root, "example.txt"),
			Modified: time.Unix(100, 0),
		})
	})
	request := func(rawQuery string) []fsops.Entry {
		t.Helper()
		rr := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/v1/fs/recent?"+rawQuery, nil)
		handler(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
		}
		var entries []fsops.Entry
		if err := json.Unmarshal(rr.Body.Bytes(), &entries); err != nil {
			t.Fatalf("decode response: %v; body: %s", err, rr.Body.String())
		}
		return entries
	}

	queryA := "root=" + url.QueryEscape(rootA)
	first := request(queryA + "&limit=2")
	second := request(queryA + "&limit=2")
	if walks.Load() != 1 {
		t.Fatalf("same jail and limit should reuse the complete scan, got %d walks", walks.Load())
	}
	if len(first) != 1 || len(second) != 1 || first[0].Name != second[0].Name {
		t.Fatalf("cached result mismatch: first=%v second=%v", names(first), names(second))
	}

	// A different limit must not reuse a result truncated for another query.
	request(queryA + "&limit=3")
	if walks.Load() != 2 {
		t.Fatalf("different limit should trigger its own scan, got %d walks", walks.Load())
	}

	// A different requested root must not reuse entries from the first walk.
	request("root=" + url.QueryEscape(rootB) + "&limit=2")
	if walks.Load() != 3 {
		t.Fatalf("different requested root should trigger its own scan, got %d walks", walks.Load())
	}
}

func TestRecentResultCacheExpiresAndPreservesEmptyArrays(t *testing.T) {
	cache := &recentResultCache{}
	now := time.Unix(100, 0)
	entry := fsops.Entry{Name: "original.txt", Path: "/share/original.txt"}
	cache.store("jail", []fsops.Entry{entry}, now)
	entry.Name = "mutated.txt"

	entries, ok := cache.load("jail", now.Add(recentCacheTTL-time.Nanosecond))
	if !ok || len(entries) != 1 || entries[0].Name != "original.txt" {
		t.Fatalf("cache should return an isolated copy before expiry: entries=%v ok=%v", names(entries), ok)
	}
	if _, ok := cache.load("jail", now.Add(recentCacheTTL)); ok {
		t.Fatal("cache entry should expire at its TTL")
	}

	cache.store("empty", []fsops.Entry{}, now)
	empty, ok := cache.load("empty", now)
	if !ok || empty == nil || len(empty) != 0 {
		t.Fatalf("empty cache result must remain a JSON array: entries=%v ok=%v", empty, ok)
	}
}

func TestRecentHandler_DoesNotCachePartialResults(t *testing.T) {
	root := t.TempDir()
	ops := fsops.New([]string{root}, false)
	var walks atomic.Int32
	handler := recentHandlerWithWalker(ops, func(_ context.Context, _ *fsops.Ops, scanRoot string, _ int, h *recentHeap) {
		walks.Add(1)
		heap.Push(h, fsops.Entry{
			Name:     "example.txt",
			Path:     filepath.Join(scanRoot, "example.txt"),
			Modified: time.Unix(100, 0),
		})
	})
	pathQuery := "root=" + url.QueryEscape(root)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	partial := httptest.NewRecorder()
	partialReq := httptest.NewRequest(http.MethodGet, "/v1/fs/recent?"+pathQuery, nil).WithContext(ctx)
	handler(partial, partialReq)
	if partial.Code != http.StatusOK || partial.Header().Get(headerSearchTimeBudget) != "1" {
		t.Fatalf("expected flagged partial 200, got %d with headers %v", partial.Code, partial.Header())
	}

	complete := httptest.NewRecorder()
	completeReq := httptest.NewRequest(http.MethodGet, "/v1/fs/recent?"+pathQuery, nil)
	handler(complete, completeReq)
	if complete.Code != http.StatusOK || complete.Header().Get(headerSearchTimeBudget) != "" {
		t.Fatalf("expected complete 200, got %d with headers %v", complete.Code, complete.Header())
	}
	if walks.Load() != 2 {
		t.Fatalf("partial result must not be cached, got %d walks", walks.Load())
	}
}

// TestRecentHandler_SetsTimeBudgetHeaderWhenBudgetExhausted verifies that an
// exhausted budget is FLAGGED rather than failed: the handler still returns
// 200 with a JSON array body and sets headerSearchTimeBudget="1".
//
// Scope, deliberately: recentTimeBudget is a compile-time constant with no
// injection seam, so this pre-cancels the request context to reach the
// ctx.Err() branch without a 15s sleep. That aborts the walk on its very
// first callback, so the result set here is empty, not partial -- this test
// pins the flag-don't-fail contract, NOT the "results collected before the
// cutoff survive" one. Proving that needs a seam in recent.go.
func TestRecentHandler_SetsTimeBudgetHeaderWhenBudgetExhausted(t *testing.T) {
	ops, _ := newSearchFixture(t)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v1/fs/recent?limit=100", nil)
	req = req.WithContext(ctx)
	recentHandler(ops)(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	if rr.Header().Get(headerSearchTimeBudget) != "1" {
		t.Fatalf("expected %s=1 when context is cancelled, got %q", headerSearchTimeBudget, rr.Header().Get(headerSearchTimeBudget))
	}

	var entries []fsops.Entry
	if err := json.Unmarshal(rr.Body.Bytes(), &entries); err != nil {
		t.Fatalf("response body should decode as JSON array, got error: %v\nbody: %s", err, rr.Body.String())
	}
}

// TestRecentHandler_NoTimeBudgetHeaderOnNormalCompletion verifies that when
// the walk completes normally (without context timeout), the
// headerSearchTimeBudget header is not set.
func TestRecentHandler_NoTimeBudgetHeaderOnNormalCompletion(t *testing.T) {
	ops, _ := newSearchFixture(t)
	rr, _ := doRecent(t, ops, "limit=100")

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	if rr.Header().Get(headerSearchTimeBudget) != "" {
		t.Fatalf("did not expect %s to be set on normal completion, got %q", headerSearchTimeBudget, rr.Header().Get(headerSearchTimeBudget))
	}
}

func TestRecentHandler_LimitsConcurrentScans(t *testing.T) {
	ops, _ := newSearchFixture(t)
	entered := make(chan struct{}, recentMaxConcurrentScans)
	release := make(chan struct{})
	handler := recentHandlerWithWalker(ops, func(context.Context, *fsops.Ops, string, int, *recentHeap) {
		entered <- struct{}{}
		<-release
	})
	responses := make(chan *httptest.ResponseRecorder, recentMaxConcurrentScans)
	for range recentMaxConcurrentScans {
		go func() {
			rr := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/v1/fs/recent", nil)
			handler(rr, req)
			responses <- rr
		}()
	}

	for range recentMaxConcurrentScans {
		select {
		case <-entered:
		case <-time.After(2 * time.Second):
			close(release)
			t.Fatal("expected concurrent scans to enter the walker")
		}
	}

	busy := httptest.NewRecorder()
	busyRequest := httptest.NewRequest(http.MethodGet, "/v1/fs/recent", nil)
	handler(busy, busyRequest)
	if busy.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429 when scan capacity is full, got %d: %s", busy.Code, busy.Body.String())
	}
	if got := busy.Header().Get("Retry-After"); got != recentBusyRetryAfter {
		t.Fatalf("expected Retry-After=%q, got %q", recentBusyRetryAfter, got)
	}
	var apiErr apiError
	if err := json.Unmarshal(busy.Body.Bytes(), &apiErr); err != nil {
		t.Fatalf("decode busy response: %v", err)
	}
	if apiErr.Code != "RECENT_BUSY" {
		t.Fatalf("expected RECENT_BUSY error code, got %q", apiErr.Code)
	}

	close(release)
	for range recentMaxConcurrentScans {
		select {
		case rr := <-responses:
			if rr.Code != http.StatusOK {
				t.Fatalf("expected admitted scan to complete with 200, got %d: %s", rr.Code, rr.Body.String())
			}
		case <-time.After(2 * time.Second):
			t.Fatal("admitted scan did not finish after release")
		}
	}
}

// TestWalkForRecent_SkipsUnreadableDirectory verifies that walkForRecent
// silently skips directories with permission-denied errors and continues
// the walk on readable parts of the tree.
func TestWalkForRecent_SkipsUnreadableDirectory(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("test requires non-root; root bypasses permission checks")
	}

	dir := t.TempDir()

	// Two readable files, one sorting BEFORE the unreadable dir and one
	// AFTER it. The second is the load-bearing one: WalkDir visits entries in
	// lexical order, so a walk that ABORTS on the permission error instead of
	// skipping past it still collects "a-readable.txt" and would look correct.
	// Only "z-readable.txt" distinguishes skipping from aborting.
	for _, name := range []string{"a-readable.txt", "z-readable.txt"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(name), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}

	// Create an unreadable subdirectory with a file inside it.
	unreadableDir := filepath.Join(dir, "unreadable")
	if err := os.Mkdir(unreadableDir, 0o755); err != nil {
		t.Fatalf("mkdir unreadable: %v", err)
	}
	unreadableFile := filepath.Join(unreadableDir, "hidden.txt")
	if err := os.WriteFile(unreadableFile, []byte("hidden"), 0o644); err != nil {
		t.Fatalf("write hidden file: %v", err)
	}
	// Remove read permissions from the directory.
	if err := os.Chmod(unreadableDir, 0o000); err != nil {
		t.Fatalf("chmod unreadable dir: %v", err)
	}
	t.Cleanup(func() {
		// Restore permissions so TempDir cleanup can remove it.
		os.Chmod(unreadableDir, 0o755)
	})

	ctx := context.Background()
	h := &recentHeap{}
	heap.Init(h)
	walkForRecent(ctx, dir, 100, h)

	// Both readable files must be present -- the one after the unreadable
	// directory proves the walk continued past it -- and hidden.txt must not.
	got := map[string]bool{}
	for _, e := range *h {
		got[e.Name] = true
	}
	if len(got) != 2 || !got["a-readable.txt"] || !got["z-readable.txt"] {
		t.Fatalf("expected both readable files, got %v", got)
	}
	if got["hidden.txt"] {
		t.Fatal("hidden.txt leaked out of the unreadable directory")
	}
}
