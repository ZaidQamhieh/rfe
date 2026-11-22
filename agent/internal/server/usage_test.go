package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"

	"github.com/ZaidQamhieh/rfe/agent/internal/fsops"
)

func TestScanUsageSumsPerChildLargestFirst(t *testing.T) {
	root := t.TempDir()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(os.MkdirAll(filepath.Join(root, "big", "deep"), 0o755))
	must(os.WriteFile(filepath.Join(root, "big", "a.bin"), make([]byte, 300), 0o644))
	must(os.WriteFile(filepath.Join(root, "big", "deep", "b.bin"), make([]byte, 200), 0o644))
	must(os.WriteFile(filepath.Join(root, "top.txt"), make([]byte, 100), 0o644))
	must(os.MkdirAll(filepath.Join(root, "empty"), 0o755))
	ops := fsops.New([]string{root}, false)
	got := scanUsage(context.Background(), ops, root)
	if got.TotalBytes != 600 || got.Partial {
		t.Fatalf("total=%d partial=%v", got.TotalBytes, got.Partial)
	}
	want := []usageEntry{{"big", true, 500}, {"top.txt", false, 100}, {"empty", true, 0}}
	if len(got.Entries) != len(want) {
		t.Fatalf("entries=%v", got.Entries)
	}
	for i := range want {
		if got.Entries[i] != want[i] {
			t.Fatalf("entry %d = %v, want %v", i, got.Entries[i], want[i])
		}
	}
}

func TestScanUsageDoesNotFollowSymlinkOutOfRoot(t *testing.T) {
	root, outside := t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "secret.bin"), make([]byte, 5000), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "link")); err != nil {
		t.Skip("symlinks unavailable")
	}
	got := scanUsage(context.Background(), fsops.New([]string{root}, false), root)
	if got.TotalBytes >= 5000 {
		t.Fatalf("followed the link: total=%d", got.TotalBytes)
	}
}

func TestScanUsageCancelledIsPartial(t *testing.T) {
	root := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if got := scanUsage(ctx, fsops.New([]string{root}, false), root); !got.Partial {
		t.Fatal("a cancelled scan must say partial")
	}
}

func TestUsageHandlerRefusesAFile(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "f.txt"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	h := usageHandler(fsops.New([]string{root}, false))
	req := httptest.NewRequest(http.MethodGet, "/v1/fs/usage?path="+url.QueryEscape(filepath.Join(root, "f.txt")), nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status %d, want 400: %s", rr.Code, rr.Body)
	}
}
