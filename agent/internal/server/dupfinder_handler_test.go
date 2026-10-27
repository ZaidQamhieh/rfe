package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ZaidQamhieh/rfe/agent/internal/fsops"
)

func TestBatchChecksumHandler(t *testing.T) {
	dir := t.TempDir()
	files := []string{
		filepath.Join(dir, "a.txt"),
		filepath.Join(dir, "b.txt"),
		filepath.Join(dir, "c.txt"),
	}
	for i, f := range files {
		if err := os.WriteFile(f, []byte(strings.Repeat("x", i+1)), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	ops := fsops.New([]string{dir}, false)
	handler := batchChecksumHandler(ops)

	pathsJSON, _ := json.Marshal(files)
	body := `{"paths":` + string(pathsJSON) + `}`
	req := httptest.NewRequest("POST", "/v1/fs/checksums", strings.NewReader(body))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}

	var resp struct {
		Checksums []checksumResult `json:"checksums"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if len(resp.Checksums) != 3 {
		t.Fatalf("got %d results, want 3", len(resp.Checksums))
	}
	for i, r := range resp.Checksums {
		if r.Hash == "" {
			t.Errorf("result[%d] hash is empty", i)
		}
		if r.Error != "" {
			t.Errorf("result[%d] unexpected error: %s", i, r.Error)
		}
		if r.Path != files[i] {
			t.Errorf("result[%d] path = %q, want %q", i, r.Path, files[i])
		}
	}
	// Verify unique hashes (different content).
	hashes := map[string]bool{}
	for _, r := range resp.Checksums {
		hashes[r.Hash] = true
	}
	if len(hashes) != 3 {
		t.Errorf("expected 3 unique hashes, got %d", len(hashes))
	}
}

func TestBatchChecksumHandler_WithErrors(t *testing.T) {
	dir := t.TempDir()
	good := filepath.Join(dir, "good.txt")
	os.WriteFile(good, []byte("data"), 0o644)
	bad := filepath.Join(dir, "nonexistent.txt")

	ops := fsops.New([]string{dir}, false)
	handler := batchChecksumHandler(ops)

	pathsJSON, _ := json.Marshal([]string{good, bad})
	body := `{"paths":` + string(pathsJSON) + `}`
	req := httptest.NewRequest("POST", "/v1/fs/checksums", strings.NewReader(body))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	var resp struct {
		Checksums []checksumResult `json:"checksums"`
	}
	json.NewDecoder(rec.Body).Decode(&resp)
	if len(resp.Checksums) != 2 {
		t.Fatalf("got %d results, want 2", len(resp.Checksums))
	}
	if resp.Checksums[0].Hash == "" {
		t.Error("expected hash for good file")
	}
	if resp.Checksums[1].Error == "" {
		t.Error("expected error for nonexistent file")
	}
}

func TestBatchChecksumHandler_EmptyPaths(t *testing.T) {
	dir := t.TempDir()
	ops := fsops.New([]string{dir}, false)
	handler := batchChecksumHandler(ops)

	body := `{"paths":[]}`
	req := httptest.NewRequest("POST", "/v1/fs/checksums", strings.NewReader(body))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}
