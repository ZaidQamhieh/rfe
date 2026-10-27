package server

import (
	"archive/zip"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/ZaidQamhieh/rfe/agent/internal/fsops"
)

func TestArchivePeekHandler_Zip(t *testing.T) {
	dir := t.TempDir()
	zipPath := filepath.Join(dir, "test.zip")

	// Create a zip with 3 files.
	zf, err := os.Create(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(zf)
	for _, name := range []string{"a.txt", "b.txt", "sub/"} {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if name != "sub/" {
			_, _ = w.Write([]byte("content"))
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	zf.Close()

	ops := fsops.New([]string{dir}, false)
	handler := archivePeekHandler(ops)

	req := httptest.NewRequest("GET", "/v1/fs/archive?path="+zipPath, nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}

	var resp struct {
		Entries []ArchiveEntry `json:"entries"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if len(resp.Entries) != 3 {
		t.Fatalf("got %d entries, want 3", len(resp.Entries))
	}
}

func TestArchivePeekHandler_Limit(t *testing.T) {
	dir := t.TempDir()
	zipPath := filepath.Join(dir, "big.zip")

	zf, err := os.Create(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(zf)
	for i := 0; i < 10; i++ {
		w, err := zw.Create("file" + string(rune('a'+i)) + ".txt")
		if err != nil {
			t.Fatal(err)
		}
		_, _ = w.Write([]byte("data"))
	}
	zw.Close()
	zf.Close()

	ops := fsops.New([]string{dir}, false)
	handler := archivePeekHandler(ops)

	req := httptest.NewRequest("GET", "/v1/fs/archive?path="+zipPath+"&limit=3", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	var resp struct {
		Entries []ArchiveEntry `json:"entries"`
	}
	json.NewDecoder(rec.Body).Decode(&resp)
	if len(resp.Entries) != 3 {
		t.Fatalf("got %d entries, want 3", len(resp.Entries))
	}
}

func TestArchivePeekHandler_UnsupportedFormat(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "test.rar")
	os.WriteFile(f, []byte("not a rar"), 0o644)

	ops := fsops.New([]string{dir}, false)
	handler := archivePeekHandler(ops)

	req := httptest.NewRequest("GET", "/v1/fs/archive?path="+f, nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

func TestArchivePeekHandler_MissingPath(t *testing.T) {
	dir := t.TempDir()
	ops := fsops.New([]string{dir}, false)
	handler := archivePeekHandler(ops)

	req := httptest.NewRequest("GET", "/v1/fs/archive", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

// A limit above archivePeekMaxLimit is clamped, as search clamps its limit,
// so one request cannot ask for an unbounded listing.
func TestArchivePeekHandler_LimitIsClamped(t *testing.T) {
	dir := t.TempDir()
	zipPath := filepath.Join(dir, "many.zip")
	zf, err := os.Create(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(zf)
	for i := 0; i < archivePeekMaxLimit+10; i++ {
		if _, err := zw.Create(fmt.Sprintf("f%05d", i)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	zf.Close()

	handler := archivePeekHandler(fsops.New([]string{dir}, false))
	req := httptest.NewRequest("GET", "/v1/fs/archive?path="+zipPath+"&limit=1000000000", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var resp struct {
		Entries []ArchiveEntry `json:"entries"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if len(resp.Entries) != archivePeekMaxLimit {
		t.Fatalf("got %d entries, want %d", len(resp.Entries), archivePeekMaxLimit)
	}
}

// A limit too large for an int is clamped too, not replaced by the default.
func TestArchivePeekHandler_OverflowLimitIsClamped(t *testing.T) {
	dir := t.TempDir()
	zipPath := filepath.Join(dir, "many.zip")
	zf, err := os.Create(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(zf)
	for i := 0; i < 600; i++ {
		if _, err := zw.Create(fmt.Sprintf("f%05d", i)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	zf.Close()
	rec := httptest.NewRecorder()
	archivePeekHandler(fsops.New([]string{dir}, false)).ServeHTTP(rec,
		httptest.NewRequest("GET", "/v1/fs/archive?path="+zipPath+"&limit=99999999999999999999", nil))
	var resp struct {
		Entries []ArchiveEntry `json:"entries"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusOK || len(resp.Entries) != 600 {
		t.Fatalf("status %d, %d entries; want 200 and all 600", rec.Code, len(resp.Entries))
	}
}
