package server

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/ZaidQamhieh/rfe/agent/internal/fsops"
	"github.com/ZaidQamhieh/rfe/agent/internal/sidecar"
)

func assertUniquePaths(t *testing.T, entries []fsops.Entry) {
	t.Helper()
	seen := map[string]bool{}
	for _, e := range entries {
		if seen[e.Path] {
			t.Errorf("duplicate hit %s", e.Path)
		}
		seen[e.Path] = true
	}
}

// Nested roots both cover Subfolder; the live walk must list each path once, and a duplicate must not use up the limit.
func TestSearchLiveWalkDedupesOverlappingRoots(t *testing.T) {
	single, dir := newSearchFixture(t)
	_, base := doSearch(t, single, "q=*&limit=500&root="+url.QueryEscape(dir))
	if len(base) == 0 {
		t.Fatal("fixture search found nothing")
	}

	nested := fsops.New([]string{dir, filepath.Join(dir, "Subfolder")}, false)
	_, got := doSearch(t, nested, "q=*&limit=500")
	assertUniquePaths(t, got)
	if len(got) != len(base) {
		t.Fatalf("overlapping roots: %d hits, want %d", len(got), len(base))
	}

	// Limit exactly the number of distinct entries: duplicates must not use up the limit and push a real entry out.
	_, got = doSearch(t, nested, "q=*&limit="+strconv.Itoa(len(base)))
	assertUniquePaths(t, got)
	want := map[string]bool{}
	for _, e := range base {
		want[e.Path] = true
	}
	for _, e := range got {
		delete(want, e.Path)
	}
	if len(got) != len(base) || len(want) != 0 {
		t.Fatalf("limit=%d: %d hits, missing %v; duplicates used up the limit", len(base), len(got), want)
	}
}

// waitIndexReady polls until the sidecar index answers for root.
func waitIndexReady(t *testing.T, si *sidecarIndex, root string) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for {
		if _, _, ok := si.query(&searchFilters{}, []string{root}, 1); ok {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("index never became ready")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// Same overlap through the sidecar index: each path once.
func TestSearchSidecarDedupesOverlappingRoots(t *testing.T) {
	bin := indexdBinary(t)
	root := diffFixture(t)
	ops := fsops.New([]string{root, filepath.Join(root, "a")}, false)
	si := newSidecarIndex(ops, sidecar.Config{Name: "rfe-indexd", Path: bin, Logf: t.Logf})
	t.Cleanup(si.sup.Stop)
	waitIndexReady(t, si, root)

	got, _, ok := si.query(diffQuery(t, "q=alpha"), []string{root, filepath.Join(root, "a")}, 50)
	if !ok {
		t.Fatal("index declined")
	}
	if len(got) == 0 {
		t.Fatal("no hits for alpha")
	}
	assertUniquePaths(t, got)
}

// Overlapping and repeated roots through the sidecar index: a limit equal to the number of distinct matches must
// still return every one of them (the sidecar applies the limit before the agent's own dedupe).
func TestSearchSidecarLimitCountsDistinctHitsWithOverlappingRoots(t *testing.T) {
	bin := indexdBinary(t)
	root := diffFixture(t)
	sub := filepath.Join(root, "a")
	ops := fsops.New([]string{root, sub}, false)
	si := newSidecarIndex(ops, sidecar.Config{Name: "rfe-indexd", Path: bin, Logf: t.Logf})
	t.Cleanup(si.sup.Stop)
	waitIndexReady(t, si, root)

	sep := string(filepath.Separator)
	filters := diffQuery(t, "q=*")
	base, _, ok := si.query(filters, []string{root}, 100000)
	if !ok || len(base) < 10 {
		t.Fatalf("single root: ok=%v, %d hits", ok, len(base))
	}
	assertUniquePaths(t, base)
	for _, roots := range [][]string{
		{root, sub},
		{sub, root, root},
		{root + sep, sub + sep, root},
	} {
		for _, limit := range []int{3, len(base) - 1, len(base)} {
			got, _, ok := si.query(filters, roots, limit)
			if !ok {
				t.Fatalf("roots %v: index declined", roots)
			}
			assertUniquePaths(t, got)
			if len(got) != limit {
				t.Fatalf("roots %v limit %d: %d hits", roots, limit, len(got))
			}
		}
	}
}

// A client that disconnects (request context cancelled) must stop the sidecar call: the handler must not answer
// from the index for a request that is already gone.
func TestSearchSidecarCallFollowsRequestContext(t *testing.T) {
	bin := indexdBinary(t)
	root := diffFixture(t)
	ops := fsops.New([]string{root}, false)
	si := newSidecarIndex(ops, sidecar.Config{Name: "rfe-indexd", Path: bin, Logf: t.Logf})
	t.Cleanup(si.sup.Stop)
	waitIndexReady(t, si, root)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	rr := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/v1/search?q=photo&root="+url.QueryEscape(root), nil).WithContext(ctx)
	searchHandler(ops, si)(rr, req)
	var out []fsops.Entry
	if err := json.Unmarshal(rr.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if len(out) != 0 {
		t.Fatalf("cancelled request was answered from the index: %d hits", len(out))
	}

	// Control: a live request is still served.
	rr = httptest.NewRecorder()
	searchHandler(ops, si)(rr, httptest.NewRequest("GET", "/v1/search?q=photo&root="+url.QueryEscape(root), nil))
	if err := json.Unmarshal(rr.Body.Bytes(), &out); err != nil || len(out) == 0 {
		t.Fatalf("live request: err=%v hits=%d", err, len(out))
	}
}
