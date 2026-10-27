package server

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ZaidQamhieh/rfe/agent/internal/store"
)

// auditTestDB is a bare store for handlers that only need somewhere to write
// their audit entry.
func auditTestDB(t *testing.T) *store.DB {
	t.Helper()
	db, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func TestRestartHandler_Supported(t *testing.T) {
	origSupported, origRestart, origDelay := restartSupportedFn, restartAgentFn, restartDelay
	defer func() {
		restartSupportedFn, restartAgentFn, restartDelay = origSupported, origRestart, origDelay
	}()

	restartDelay = time.Millisecond
	restartSupportedFn = func() bool { return true }
	called := make(chan struct{}, 1)
	restartAgentFn = func() error {
		called <- struct{}{}
		return nil
	}

	req := httptest.NewRequest(http.MethodPost, "/v1/agent/restart", nil)
	rr := httptest.NewRecorder()
	restartHandler(auditTestDB(t))(rr, req)

	if rr.Code != http.StatusAccepted {
		t.Fatalf("want 202, got %d", rr.Code)
	}

	select {
	case <-called:
	case <-time.After(time.Second):
		t.Fatal("restartAgentFn was not invoked")
	}
}

func TestRestartHandler_Unsupported(t *testing.T) {
	origSupported := restartSupportedFn
	defer func() { restartSupportedFn = origSupported }()
	restartSupportedFn = func() bool { return false }

	req := httptest.NewRequest(http.MethodPost, "/v1/agent/restart", nil)
	rr := httptest.NewRecorder()
	restartHandler(auditTestDB(t))(rr, req)

	if rr.Code != http.StatusNotImplemented {
		t.Fatalf("want 501, got %d", rr.Code)
	}
}
