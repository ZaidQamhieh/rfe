//go:build !windows

package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/ZaidQamhieh/rfe/agent/internal/fsops"
)

// Naming the wrong kind of filesystem object is a client error with a
// documented code, never a 500 (found by live end-to-end testing).

func errCode(t *testing.T, rr *httptest.ResponseRecorder) string {
	t.Helper()
	var body struct{ Code string }
	_ = json.Unmarshal(rr.Body.Bytes(), &body)
	return body.Code
}

func TestDownload_FIFOAndDirectoryAreNotAFile(t *testing.T) {
	root := t.TempDir()
	fifo := filepath.Join(root, "pipe")
	if err := syscall.Mkfifo(fifo, 0o644); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(root, "dir")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	ops := fsops.New([]string{root}, false)
	for _, p := range []string{fifo, dir} {
		rr := getContent(ops, p, nil)
		if rr.Code != http.StatusUnprocessableEntity || errCode(t, rr) != "NOT_A_FILE" {
			t.Fatalf("GET /content %s = %d %s, want 422 NOT_A_FILE", p, rr.Code, rr.Body.String())
		}
	}
}

func TestListDir_RegularFileIsBadRequest(t *testing.T) {
	root := t.TempDir()
	f := filepath.Join(root, "plain.txt")
	if err := os.WriteFile(f, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	ops := fsops.New([]string{root}, false)
	q := url.Values{"path": {f}}
	rr := httptest.NewRecorder()
	listDirHandler(ops)(rr, httptest.NewRequest(http.MethodGet, "/v1/fs?"+q.Encode(), nil))
	if rr.Code != http.StatusBadRequest || errCode(t, rr) != "BAD_REQUEST" {
		t.Fatalf("GET /fs on a file = %d %s, want 400 BAD_REQUEST", rr.Code, rr.Body.String())
	}
}

func TestWriteContent_FolderIsNotAFile(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "dir")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	ops := fsops.New([]string{root}, false)
	q := url.Values{"path": {dir}}
	rr := httptest.NewRecorder()
	writeContentHandler(ops)(rr, httptest.NewRequest(http.MethodPut, "/v1/content?"+q.Encode(), bytes.NewReader([]byte("x"))))
	if rr.Code != http.StatusUnprocessableEntity || errCode(t, rr) != "NOT_A_FILE" {
		t.Fatalf("PUT /content on a folder = %d %s, want 422 NOT_A_FILE", rr.Code, rr.Body.String())
	}
	if st, err := os.Stat(dir); err != nil || !st.IsDir() {
		t.Fatalf("folder changed: %v", err)
	}
}
