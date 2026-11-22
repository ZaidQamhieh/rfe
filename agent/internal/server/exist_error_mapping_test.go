package server

import (
	"io/fs"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// A raw "exists" error (an exclusive create that lost a race after the early
// check) is 409 CONFLICT with fixed text, not a 500.
func TestHandleFsError_ErrExistIsConflict(t *testing.T) {
	rec := httptest.NewRecorder()
	handleFsError(rec, &fs.PathError{Op: "open", Path: "/secret/path/x", Err: fs.ErrExist})
	if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "CONFLICT") {
		t.Fatalf("got %d %s, want 409 CONFLICT", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "/secret/path") {
		t.Fatalf("OS error text leaked: %s", rec.Body.String())
	}
}
