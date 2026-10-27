package server

import (
	"encoding/json"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ZaidQamhieh/rfe/agent/internal/fsops"
	"github.com/ZaidQamhieh/rfe/agent/internal/sidecar"
)

// Through the real handler and a ready sidecar index: a search rooted under a hidden directory (which the index
// skips) must walk live and find the file, not answer [] from the index.
func TestSearchHandlerWalksLiveForARootTheIndexSkips(t *testing.T) {
	bin := indexdBinary(t)
	root := diffFixture(t)
	hidden := filepath.Join(root, ".private")
	if err := os.MkdirAll(hidden, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(hidden, "zebra-notes.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	ops := fsops.New([]string{root}, false)
	si := newSidecarIndex(ops, sidecar.Config{Name: "rfe-indexd", Path: bin, Logf: t.Logf})
	t.Cleanup(si.sup.Stop)

	deadline := time.Now().Add(15 * time.Second)
	for {
		if _, _, ok := si.query(diffQuery(t, "q=photo"), []string{root}, 10); ok {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("index never became ready")
		}
		time.Sleep(10 * time.Millisecond)
	}

	rr := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/v1/search?q=zebra&root="+url.QueryEscape(hidden), nil)
	searchHandler(ops, si)(rr, req)
	if rr.Code != 200 {
		t.Fatalf("status %d: %s", rr.Code, rr.Body.String())
	}
	var out []fsops.Entry
	if err := json.Unmarshal(rr.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 || out[0].Name != "zebra-notes.txt" {
		t.Fatalf("got %+v, want the file under the hidden root", out)
	}
}
