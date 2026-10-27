package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestStatusHandler(t *testing.T) {
	cfg := Config{
		Version:   "1.2.3",
		StartTime: time.Now().Add(-5 * time.Second),
		DataDir:   "/tmp",
	}

	req := httptest.NewRequest(http.MethodGet, "/v1/status", nil)
	rr := httptest.NewRecorder()

	statusHandler(cfg)(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", rr.Code)
	}

	var got statusResponse
	if err := json.NewDecoder(rr.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if got.Version != "1.2.3" {
		t.Errorf("version = %q, want 1.2.3", got.Version)
	}
	if got.UptimeSeconds < 0 {
		t.Errorf("uptimeSeconds = %d, want >= 0", got.UptimeSeconds)
	}
	if got.Platform == "" {
		t.Error("platform empty")
	}
	if got.TotalBytes == 0 {
		t.Error("totalBytes is zero — disk stat likely failed")
	}
}
