package server

import (
	"context"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// An in-flight publish temp (and any other name with the publish-temp prefix) is invisible to search, to a
// direct-path search and to the recent-files walk, for every device; ordinary files stay visible.
func TestPublishTempsAreHiddenFromSearchAndRecent(t *testing.T) {
	ops, root := newSearchFixture(t)
	temp := filepath.Join(root, ".rfe-upload-1234abcd")
	if err := os.WriteFile(temp, []byte("partial upload bytes"), 0o600); err != nil {
		t.Fatal(err)
	}
	sub := filepath.Join(root, "Subfolder", ".rfe-upload-nested")
	if err := os.WriteFile(sub, []byte("partial"), 0o600); err != nil {
		t.Fatal(err)
	}

	for _, q := range []string{"upload", ".rfe-upload-1234abcd", url.QueryEscape(temp), "*.rfe-upload-*"} {
		rr, entries := doSearch(t, ops, "q="+q+"&root="+url.QueryEscape(root))
		if rr.Code != 200 {
			t.Fatalf("search %q: %d %s", q, rr.Code, rr.Body.String())
		}
		for _, e := range entries {
			if strings.HasPrefix(e.Name, ".rfe-upload-") {
				t.Errorf("search %q returned the publish temp %s", q, e.Path)
			}
		}
	}
	if _, entries := doSearch(t, ops, "q=photo&root="+url.QueryEscape(root)); len(entries) == 0 {
		t.Fatal("ordinary files must still be found")
	}

	var h recentHeap
	walkForRecentWithOps(context.Background(), ops, root, 100, &h)
	if h.Len() == 0 {
		t.Fatal("recent walk found nothing")
	}
	for _, e := range h {
		if strings.HasPrefix(e.Name, ".rfe-upload-") {
			t.Errorf("recent files lists the publish temp %s", e.Path)
		}
	}
}
