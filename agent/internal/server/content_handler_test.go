// Package server — tests for the PUT /v1/content handler.
package server

import (
	"bytes"
	"encoding/json"
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

// putContent builds and executes a PUT /v1/content request against the
// handler directly (no router), returning the recorder.
func putContent(ops *fsops.Ops, path string, baseModified *time.Time, body []byte) *httptest.ResponseRecorder {
	q := url.Values{}
	q.Set("path", path)
	if baseModified != nil {
		q.Set("baseModified", baseModified.Format(time.RFC3339Nano))
	}
	req := httptest.NewRequest(http.MethodPut, "/v1/content?"+q.Encode(), bytes.NewReader(body))
	rr := httptest.NewRecorder()
	writeContentHandler(ops)(rr, req)
	return rr
}

// TestWriteContentHandler_Create verifies a fresh write returns 200 with the
// updated Entry and the bytes land on disk.
func TestWriteContentHandler_Create(t *testing.T) {
	root := t.TempDir()
	ops := fsops.New([]string{root}, false)

	target := filepath.Join(root, "note.txt")
	rr := putContent(ops, target, nil, []byte("hello world"))
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	var entry fsops.Entry
	if err := json.Unmarshal(rr.Body.Bytes(), &entry); err != nil {
		t.Fatalf("decode entry: %v", err)
	}
	if entry.Size != int64(len("hello world")) {
		t.Fatalf("unexpected entry size: %d", entry.Size)
	}

	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(got) != "hello world" {
		t.Fatalf("unexpected content: %q", got)
	}
}

// TestWriteContentHandler_MissingPath verifies 400 BAD_REQUEST when path is omitted.
func TestWriteContentHandler_MissingPath(t *testing.T) {
	root := t.TempDir()
	ops := fsops.New([]string{root}, false)

	req := httptest.NewRequest(http.MethodPut, "/v1/content", bytes.NewReader([]byte("x")))
	rr := httptest.NewRecorder()
	writeContentHandler(ops)(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rr.Code, rr.Body.String())
	}
	var got apiError
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode error body: %v", err)
	}
	if got.Code != "BAD_REQUEST" {
		t.Fatalf("unexpected error code: %+v", got)
	}
}

// TestWriteContentHandler_ReadOnly verifies 403 READ_ONLY in read-only mode.
func TestWriteContentHandler_ReadOnly(t *testing.T) {
	root := t.TempDir()
	ops := fsops.New([]string{root}, true)

	target := filepath.Join(root, "note.txt")
	rr := putContent(ops, target, nil, []byte("hello"))
	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", rr.Code, rr.Body.String())
	}
	var got apiError
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode error body: %v", err)
	}
	if got.Code != "READ_ONLY" {
		t.Fatalf("unexpected error code: %+v", got)
	}
}

// TestWriteContentHandler_OutsideJail verifies 403 FORBIDDEN for a path
// outside the configured jail root.
func TestWriteContentHandler_OutsideJail(t *testing.T) {
	root := t.TempDir()
	ops := fsops.New([]string{root}, false)
	outside := t.TempDir()

	target := filepath.Join(outside, "note.txt")
	rr := putContent(ops, target, nil, []byte("hello"))
	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", rr.Code, rr.Body.String())
	}
	var got apiError
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode error body: %v", err)
	}
	if got.Code != "FORBIDDEN" {
		t.Fatalf("unexpected error code: %+v", got)
	}
}

// TestWriteContentHandler_StaleBaseModified verifies 409 STALE_WRITE when
// baseModified doesn't match the file's current mtime, and that the file is
// left unchanged.
func TestWriteContentHandler_StaleBaseModified(t *testing.T) {
	root := t.TempDir()
	ops := fsops.New([]string{root}, false)

	target := filepath.Join(root, "note.txt")
	if err := os.WriteFile(target, []byte("original"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	stale := time.Now().Add(-1 * time.Hour)
	rr := putContent(ops, target, &stale, []byte("clobber"))
	if rr.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d: %s", rr.Code, rr.Body.String())
	}
	var got apiError
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode error body: %v", err)
	}
	if got.Code != "STALE_WRITE" {
		t.Fatalf("unexpected error code: %+v", got)
	}

	gotBody, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(gotBody) != "original" {
		t.Fatalf("file should be unchanged, got %q", gotBody)
	}
}

// TestWriteContentHandler_MatchingBaseModified verifies a write with a
// baseModified matching the file's current mtime succeeds.
func TestWriteContentHandler_MatchingBaseModified(t *testing.T) {
	root := t.TempDir()
	ops := fsops.New([]string{root}, false)

	target := filepath.Join(root, "note.txt")
	if err := os.WriteFile(target, []byte("original"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	info, err := os.Stat(target)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	base := info.ModTime()

	rr := putContent(ops, target, &base, []byte("updated"))
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(got) != "updated" {
		t.Fatalf("unexpected content: %q", got)
	}
}

// TestWriteContentHandler_OversizeBodyIs413 verifies a body over the 5MiB cap
// is rejected with 413 PAYLOAD_TOO_LARGE.
func TestWriteContentHandler_OversizeBodyIs413(t *testing.T) {
	root := t.TempDir()
	ops := fsops.New([]string{root}, false)

	target := filepath.Join(root, "big.bin")
	body := strings.Repeat("x", int(MaxContentBytes)+1)

	rr := putContent(ops, target, nil, []byte(body))
	if rr.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("expected 413, got %d: %s", rr.Code, rr.Body.String())
	}
	var got apiError
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode error body: %v", err)
	}
	if got.Code != "PAYLOAD_TOO_LARGE" {
		t.Fatalf("unexpected error code: %+v", got)
	}

	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatalf("expected no file to be created, stat err: %v", err)
	}
}
