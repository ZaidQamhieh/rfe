package server

import (
	"bytes"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ZaidQamhieh/rfe/agent/internal/fsops"
)

// writeAt writes data to p and pins its modification time to t.
func writeAt(t *testing.T, p string, data []byte, mt time.Time) {
	t.Helper()
	if err := os.WriteFile(p, data, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(p, mt, mt); err != nil {
		t.Fatal(err)
	}
}

// A download carries a strong ETag on full and partial answers, and an unchanged file resumes (206) against it.
func TestDownloadETag_StrongAndResumesWhenUnchanged(t *testing.T) {
	root := t.TempDir()
	ops := fsops.New([]string{root}, false)
	p := filepath.Join(root, "f.bin")
	writeAt(t, p, bytes.Repeat([]byte("a"), 4000), time.Unix(1_800_000_000, 111))

	first := getContent(ops, p, nil)
	etag := first.Header().Get("ETag")
	if etag == "" || strings.HasPrefix(etag, "W/") || !strings.HasPrefix(etag, "\"") || !strings.HasSuffix(etag, "\"") {
		t.Fatalf("ETag %q: want a strong quoted entity tag", etag)
	}
	rr := getContent(ops, p, map[string]string{"Range": "bytes=1000-", "If-Range": etag})
	if rr.Code != http.StatusPartialContent || rr.Body.Len() != 3000 {
		t.Fatalf("unchanged file: status %d, %d bytes; want 206 and 3000", rr.Code, rr.Body.Len())
	}
	if got := rr.Header().Get("ETag"); got != etag {
		t.Fatalf("206 ETag %q differs from the full response's %q", got, etag)
	}
}

// A rewrite with the same size inside the same second (only mtime_ns differs) must not resume onto the old prefix.
func TestDownloadETag_SameSizeSameSecondRewriteRestarts(t *testing.T) {
	root := t.TempDir()
	ops := fsops.New([]string{root}, false)
	p := filepath.Join(root, "f.bin")
	writeAt(t, p, bytes.Repeat([]byte("a"), 4000), time.Unix(1_800_000_000, 100))
	etag := getContent(ops, p, nil).Header().Get("ETag")
	if etag == "" {
		t.Fatal("no ETag on the first response")
	}

	changed := bytes.Repeat([]byte("b"), 4000)
	writeAt(t, p, changed, time.Unix(1_800_000_000, 900_000))
	rr := getContent(ops, p, map[string]string{"Range": "bytes=1000-", "If-Range": etag})
	if rr.Code != http.StatusOK || !bytes.Equal(rr.Body.Bytes(), changed) {
		t.Fatalf("rewritten file: status %d, %d bytes; want 200 and the whole new file", rr.Code, rr.Body.Len())
	}
	if rr.Header().Get("ETag") == etag {
		t.Fatal("the rewritten file kept the old ETag")
	}
}

// If-Match with the old ETag is refused with 412 once the file changed, and honoured while it has not.
func TestDownloadETag_IfMatch(t *testing.T) {
	root := t.TempDir()
	ops := fsops.New([]string{root}, false)
	p := filepath.Join(root, "f.bin")
	writeAt(t, p, bytes.Repeat([]byte("a"), 4000), time.Unix(1_800_000_000, 100))
	etag := getContent(ops, p, nil).Header().Get("ETag")
	if etag == "" {
		t.Fatal("no ETag on the first response")
	}
	if rr := getContent(ops, p, map[string]string{"Range": "bytes=10-", "If-Match": etag}); rr.Code != http.StatusPartialContent {
		t.Fatalf("unchanged file with If-Match: status %d, want 206", rr.Code)
	}
	writeAt(t, p, bytes.Repeat([]byte("b"), 4000), time.Unix(1_800_000_000, 900_000))
	if rr := getContent(ops, p, map[string]string{"Range": "bytes=10-", "If-Match": etag}); rr.Code != http.StatusPreconditionFailed {
		t.Fatalf("changed file with If-Match: status %d, want 412", rr.Code)
	}
}

// The gzip answer names the same file version, so a client that downloaded it can resume with Range against it.
func TestDownloadETag_GzipAnswerCarriesTheSameETag(t *testing.T) {
	root := t.TempDir()
	ops := fsops.New([]string{root}, false)
	p := filepath.Join(root, "log.txt")
	writeAt(t, p, bytes.Repeat([]byte("line of text\n"), 300), time.Unix(1_800_000_000, 5))
	plain := getContent(ops, p, map[string]string{"Range": "bytes=0-9"}).Header().Get("ETag")
	gz := getContent(ops, p, map[string]string{"Accept-Encoding": "gzip"})
	if gz.Header().Get("Content-Encoding") != "gzip" {
		t.Fatalf("expected a gzip answer, got %q", gz.Header().Get("Content-Encoding"))
	}
	if plain == "" || gz.Header().Get("ETag") != plain {
		t.Fatalf("gzip ETag %q, ranged ETag %q: want equal and non-empty", gz.Header().Get("ETag"), plain)
	}
}
