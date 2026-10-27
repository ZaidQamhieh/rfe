// Package server — tests for the GET /v1/content handler, focused on S3
// gzip-on-download and its interaction with HTTP Range (resumable download).
package server

import (
	"bytes"
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ZaidQamhieh/rfe/agent/internal/fsops"
)

// getContent builds and executes a GET /v1/content request against the
// handler directly (no router), returning the recorder.
func getContent(ops *fsops.Ops, path string, headers map[string]string) *httptest.ResponseRecorder {
	q := url.Values{}
	q.Set("path", path)
	req := httptest.NewRequest(http.MethodGet, "/v1/content?"+q.Encode(), nil)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rr := httptest.NewRecorder()
	downloadHandler(ops)(rr, req)
	return rr
}

func TestDownloadHandler_GzipCompression(t *testing.T) {
	root := t.TempDir()
	ops := fsops.New([]string{root}, false)

	// Above compressMinBytes (1024) so it's eligible.
	original := []byte(strings.Repeat("the quick brown fox jumps over the lazy dog\n", 50))
	if len(original) < compressMinBytes {
		t.Fatalf("test fixture too small: %d bytes", len(original))
	}

	tests := []struct {
		name        string
		fileName    string
		acceptGzip  bool
		rangeHeader string
		wantGzip    bool
	}{
		{
			name:       "non-range request with Accept-Encoding gzip on compressible extension gets gzip",
			fileName:   "notes.txt",
			acceptGzip: true,
			wantGzip:   true,
		},
		{
			name:       "non-range request without Accept-Encoding gets plain bytes",
			fileName:   "notes2.txt",
			acceptGzip: false,
			wantGzip:   false,
		},
		{
			name:       "non-compressible extension never gets gzip even with Accept-Encoding",
			fileName:   "image.jpg",
			acceptGzip: true,
			wantGzip:   false,
		},
		{
			name:        "range request never gets gzip even with Accept-Encoding",
			fileName:    "notes3.txt",
			acceptGzip:  true,
			rangeHeader: "bytes=0-9",
			wantGzip:    false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			target := filepath.Join(root, tc.fileName)
			if err := os.WriteFile(target, original, 0o644); err != nil {
				t.Fatalf("WriteFile: %v", err)
			}

			headers := map[string]string{}
			if tc.acceptGzip {
				headers["Accept-Encoding"] = "gzip"
			}
			if tc.rangeHeader != "" {
				headers["Range"] = tc.rangeHeader
			}

			rr := getContent(ops, target, headers)

			gotGzip := rr.Header().Get("Content-Encoding") == "gzip"
			if gotGzip != tc.wantGzip {
				t.Fatalf("Content-Encoding gzip = %v, want %v (headers: %+v)", gotGzip, tc.wantGzip, rr.Header())
			}

			if tc.rangeHeader != "" {
				// Range path must be entirely unaffected: verify it still
				// behaves like a normal ranged request (206, partial body).
				if rr.Code != http.StatusPartialContent {
					t.Fatalf("expected 206 for ranged request, got %d", rr.Code)
				}
				if rr.Body.Len() != 10 {
					t.Fatalf("expected 10-byte ranged body, got %d", rr.Body.Len())
				}
				return
			}

			if gotGzip {
				gr, err := gzip.NewReader(bytes.NewReader(rr.Body.Bytes()))
				if err != nil {
					t.Fatalf("gzip.NewReader: %v", err)
				}
				decompressed, err := io.ReadAll(gr)
				if err != nil {
					t.Fatalf("decompress: %v", err)
				}
				if !bytes.Equal(decompressed, original) {
					t.Fatalf("decompressed content mismatch: got %d bytes, want %d", len(decompressed), len(original))
				}
			} else {
				if !bytes.Equal(rr.Body.Bytes(), original) {
					t.Fatalf("plain content mismatch: got %d bytes, want %d", rr.Body.Len(), len(original))
				}
			}
		})
	}
}

// TestDownloadHandler_GzipSkippedBelowSizeFloor verifies a tiny compressible
// file is never gzip'd even with Accept-Encoding: gzip, since gzip overhead
// isn't worth it below compressMinBytes.
func TestDownloadHandler_GzipSkippedBelowSizeFloor(t *testing.T) {
	root := t.TempDir()
	ops := fsops.New([]string{root}, false)

	target := filepath.Join(root, "tiny.txt")
	tiny := []byte("hello")
	if err := os.WriteFile(target, tiny, 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	rr := getContent(ops, target, map[string]string{"Accept-Encoding": "gzip"})
	if rr.Header().Get("Content-Encoding") == "gzip" {
		t.Fatalf("expected no gzip for a file below the size floor")
	}
	if !bytes.Equal(rr.Body.Bytes(), tiny) {
		t.Fatalf("content mismatch: got %q, want %q", rr.Body.Bytes(), tiny)
	}
}

// The phone resumes a download with Range plus If-Range set to the Last-Modified it saw first: an unchanged file
// answers 206 with the tail, a changed one answers 200 with the whole new file (never a tail of the new bytes).
func TestDownloadHandler_IfRangeResume(t *testing.T) {
	root := t.TempDir()
	ops := fsops.New([]string{root}, false)
	p := filepath.Join(root, "f.bin")
	old := bytes.Repeat([]byte("a"), 4000)
	if err := os.WriteFile(p, old, 0o644); err != nil {
		t.Fatal(err)
	}
	first := getContent(ops, p, nil)
	lm := first.Header().Get("Last-Modified")
	if lm == "" {
		t.Fatal("the agent must send Last-Modified; the app's If-Range depends on it")
	}

	rr := getContent(ops, p, map[string]string{"Range": "bytes=1000-", "If-Range": lm})
	if rr.Code != http.StatusPartialContent || rr.Body.Len() != 3000 {
		t.Fatalf("unchanged file: status %d, %d bytes; want 206 and 3000", rr.Code, rr.Body.Len())
	}

	changed := bytes.Repeat([]byte("b"), 4100)
	if err := os.WriteFile(p, changed, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(p, time.Now().Add(time.Hour), time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	rr = getContent(ops, p, map[string]string{"Range": "bytes=1000-", "If-Range": lm})
	if rr.Code != http.StatusOK || !bytes.Equal(rr.Body.Bytes(), changed) {
		t.Fatalf("changed file: status %d, %d bytes; want 200 and the whole new file", rr.Code, rr.Body.Len())
	}
}
