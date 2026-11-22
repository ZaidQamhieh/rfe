package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/ZaidQamhieh/rfe/agent/internal/store"
)

// idemDevice is a non-admin paired device that may modify files (needed for overwrite=true).
func idemDevice(id string) *store.Device {
	return &store.Device{ID: id, CanModify: true}
}

// postComplete calls the complete handler as device dev (nil = admin) and returns the recorder.
func postComplete(t *testing.T, id string, dev *store.Device, call func(http.ResponseWriter, *http.Request)) *httptest.ResponseRecorder {
	t.Helper()
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/transfers/"+id+"/complete", nil)
	req = withURLParam(req, map[string]string{"id": id})
	if dev == nil {
		req = asAdmin(req)
	} else {
		req = req.WithContext(withDevice(req.Context(), dev))
	}
	call(rr, req)
	return rr
}

// TestCompleteTransferIsIdempotent: a repeated complete for an already published session
// (the first response was lost) returns the same 200 body, never CONFLICT or TRANSFER_NOT_OPEN,
// and does not publish a second time.
func TestCompleteTransferIsIdempotent(t *testing.T) {
	for _, overwrite := range []bool{false, true} {
		tm, ops := newTestTransferManager(t)
		content := []byte("the bytes of a file whose complete ack was lost")
		target := filepath.Join(t.TempDir(), "out.bin")
		id := uuid.New().String()
		if _, err := tm.OpenSession(id, target, int64(len(content)), 1024, sha256hex(content), overwrite, "owner-1"); err != nil {
			t.Fatalf("OpenSession: %v", err)
		}
		if err := tm.WriteChunk(id, 0, content, sha256hex(content)); err != nil {
			t.Fatalf("WriteChunk: %v", err)
		}
		owner := idemDevice("owner-1")
		call := completeTransferHandler(tm, ops)

		first := postComplete(t, id, owner, call)
		if first.Code != http.StatusOK {
			t.Fatalf("overwrite=%v first complete: %d %s", overwrite, first.Code, first.Body.String())
		}
		second := postComplete(t, id, owner, call)
		if second.Code != http.StatusOK {
			t.Fatalf("overwrite=%v repeat complete: want 200, got %d %s", overwrite, second.Code, second.Body.String())
		}
		var a, b map[string]any
		if err := json.Unmarshal(first.Body.Bytes(), &a); err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(second.Body.Bytes(), &b); err != nil {
			t.Fatalf("repeat body: %v (%s)", err, second.Body.String())
		}
		if a["sha256"] != b["sha256"] || a["path"] != b["path"] || a["size"] != b["size"] || b["verified"] != true {
			t.Fatalf("overwrite=%v repeat body differs: first=%v second=%v", overwrite, a, b)
		}
		if got, err := os.ReadFile(target); err != nil || string(got) != string(content) {
			t.Fatalf("target = %q, %v", got, err)
		}
	}
}

// TestCompleteTransferRepeatHidesFromForeignDevice: the idempotent answer is only for the owner.
func TestCompleteTransferRepeatHidesFromForeignDevice(t *testing.T) {
	tm, ops := newTestTransferManager(t)
	content := []byte("owned")
	target := filepath.Join(t.TempDir(), "out.bin")
	id := uuid.New().String()
	if _, err := tm.OpenSession(id, target, int64(len(content)), 64, sha256hex(content), false, "owner-1"); err != nil {
		t.Fatal(err)
	}
	if err := tm.WriteChunk(id, 0, content, sha256hex(content)); err != nil {
		t.Fatal(err)
	}
	call := completeTransferHandler(tm, ops)
	if rr := postComplete(t, id, idemDevice("owner-1"), call); rr.Code != http.StatusOK {
		t.Fatalf("first complete: %d %s", rr.Code, rr.Body.String())
	}
	rr := postComplete(t, id, idemDevice("other-1"), call)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("foreign repeat: want 404, got %d %s", rr.Code, rr.Body.String())
	}
	if strings.Contains(rr.Body.String(), target) {
		t.Fatalf("foreign repeat leaks the path: %s", rr.Body.String())
	}
}

// TestCompleteTransferRepeatOfFailedKeepsError: a failed session stays a conflict on complete.
func TestCompleteTransferRepeatOfFailedKeepsError(t *testing.T) {
	tm, ops := newTestTransferManager(t)
	content := []byte("failing")
	id := uuid.New().String()
	if _, err := tm.OpenSession(id, filepath.Join(t.TempDir(), "f.bin"), int64(len(content)), 64, sha256hex(content), false, "owner-1"); err != nil {
		t.Fatal(err)
	}
	if err := tm.DeleteSession(id); err != nil {
		t.Fatal(err)
	}
	rr := postComplete(t, id, idemDevice("owner-1"), completeTransferHandler(tm, ops))
	if rr.Code == http.StatusOK {
		t.Fatalf("complete of a removed session answered 200: %s", rr.Body.String())
	}
}
