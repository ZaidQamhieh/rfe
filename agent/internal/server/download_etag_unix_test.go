//go:build !windows

package server

import (
	"bytes"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ZaidQamhieh/rfe/agent/internal/fsops"
)

// A file replaced by another with the same size and the same mtime (a different inode) must not resume either.
func TestDownloadETag_ReplacedFileSameSizeSameMtimeRestarts(t *testing.T) {
	root := t.TempDir()
	ops := fsops.New([]string{root}, false)
	p := filepath.Join(root, "f.bin")
	mt := time.Unix(1_800_000_000, 777)
	writeAt(t, p, bytes.Repeat([]byte("a"), 4000), mt)
	etag := getContent(ops, p, nil).Header().Get("ETag")
	if etag == "" {
		t.Fatal("no ETag on the first response")
	}

	// Keep the old inode alive so the new file cannot reuse its number.
	old, err := os.Open(p)
	if err != nil {
		t.Fatal(err)
	}
	defer old.Close()
	tmp := filepath.Join(root, "new.tmp")
	changed := bytes.Repeat([]byte("b"), 4000)
	writeAt(t, tmp, changed, mt)
	if err := os.Rename(tmp, p); err != nil {
		t.Fatal(err)
	}
	rr := getContent(ops, p, map[string]string{"Range": "bytes=1000-", "If-Range": etag})
	if rr.Code != http.StatusOK || !bytes.Equal(rr.Body.Bytes(), changed) {
		t.Fatalf("replaced file: status %d, %d bytes; want 200 and the whole new file", rr.Code, rr.Body.Len())
	}
}
