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

func TestChmodHandler(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "test.txt")
	if err := os.WriteFile(f, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}

	ops := fsops.New([]string{dir}, false)
	handler := chmodHandler(ops)

	body := `{"path":"` + f + `","mode":"0755"}`
	req := httptest.NewRequest("POST", "/v1/fs/chmod", strings.NewReader(body))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}

	info, err := os.Stat(f)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o755 {
		t.Errorf("mode = %o, want 0755", info.Mode().Perm())
	}

	var entry fsops.Entry
	if err := json.NewDecoder(rec.Body).Decode(&entry); err != nil {
		t.Fatal(err)
	}
	if entry.Name != "test.txt" {
		t.Errorf("entry.Name = %q, want test.txt", entry.Name)
	}
}

func TestChmodHandler_InvalidMode(t *testing.T) {
	dir := t.TempDir()
	ops := fsops.New([]string{dir}, false)
	handler := chmodHandler(ops)

	body := `{"path":"/whatever","mode":"zzzz"}`
	req := httptest.NewRequest("POST", "/v1/fs/chmod", strings.NewReader(body))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

func TestChmodHandler_MissingFields(t *testing.T) {
	dir := t.TempDir()
	ops := fsops.New([]string{dir}, false)
	handler := chmodHandler(ops)

	body := `{"path":""}`
	req := httptest.NewRequest("POST", "/v1/fs/chmod", strings.NewReader(body))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}
