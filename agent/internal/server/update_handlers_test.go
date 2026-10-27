package server

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestLatestAppHandler(t *testing.T) {
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "rfe-2.0.0-20.apk"), []byte("apk-bytes"), 0o644)

	rr := httptest.NewRecorder()
	latestAppHandler(dir)(rr, httptest.NewRequest(http.MethodGet, "/v1/app/latest", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("code = %d", rr.Code)
	}
	if body := rr.Body.String(); !contains(body, `"versionCode":20`) || !contains(body, `"versionName":"2.0.0"`) {
		t.Fatalf("unexpected body: %s", body)
	}
}

func TestLatestAppHandler_NoneIs204(t *testing.T) {
	rr := httptest.NewRecorder()
	latestAppHandler(t.TempDir())(rr, httptest.NewRequest(http.MethodGet, "/v1/app/latest", nil))
	if rr.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", rr.Code)
	}
}

func TestDownloadAppHandler_StreamsBytes(t *testing.T) {
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "rfe-2.0.0-20.apk"), []byte("apk-bytes"), 0o644)

	rr := httptest.NewRecorder()
	downloadAppHandler(dir)(rr, httptest.NewRequest(http.MethodGet, "/v1/app/download", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("code = %d", rr.Code)
	}
	if rr.Body.String() != "apk-bytes" {
		t.Fatalf("unexpected body: %q", rr.Body.String())
	}
	if ct := rr.Header().Get("Content-Type"); ct != "application/vnd.android.package-archive" {
		t.Fatalf("unexpected content-type: %s", ct)
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || indexOf(s, sub) >= 0)
}
func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

func TestLatestAppHandler_ReportsTheDigest(t *testing.T) {
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "rfe-2.0.0-20.apk"), []byte("apk-bytes"), 0o644)
	rr := httptest.NewRecorder()
	latestAppHandler(dir)(rr, httptest.NewRequest(http.MethodGet, "/v1/app/latest", nil))
	sum := sha256.Sum256([]byte("apk-bytes"))
	if want := `"sha256":"` + hex.EncodeToString(sum[:]) + `"`; !contains(rr.Body.String(), want) {
		t.Fatalf("missing %s in %s", want, rr.Body.String())
	}
}
