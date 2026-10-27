package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ZaidQamhieh/rfe/agent/internal/store"
)

func TestDeleteTransferHandler(t *testing.T) {
	db, _ := newTestDeps(t)
	tr := &store.Transfer{ID: "t1", TargetPath: "/tmp/foo", TotalSize: 10, ChunkSize: 10, TotalChunks: 1}
	if err := db.CreateTransfer(tr); err != nil {
		t.Fatalf("create transfer: %v", err)
	}

	req := asAdmin(withURLParam(httptest.NewRequest(http.MethodDelete, "/v1/transfers/t1", nil), map[string]string{"id": "t1"}))
	rr := httptest.NewRecorder()
	deleteTransferHandler(db)(rr, req)
	if rr.Code != http.StatusNoContent {
		t.Fatalf("want 204, got %d: %s", rr.Code, rr.Body.String())
	}

	req = asAdmin(withURLParam(httptest.NewRequest(http.MethodDelete, "/v1/transfers/t1", nil), map[string]string{"id": "t1"}))
	rr = httptest.NewRecorder()
	deleteTransferHandler(db)(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("want 404 on second delete, got %d: %s", rr.Code, rr.Body.String())
	}
}

// asDevice attaches an ordinary (code-paired, non-admin) device.
func asDevice(r *http.Request, id string) *http.Request {
	return r.WithContext(withDevice(r.Context(), &store.Device{ID: id, ViaLogin: false}))
}

// PR-03: a non-admin device must not delete another device's transfer row,
// and must not learn that the row exists.
func TestDeleteTransferHandler_NonOwnerForbidden(t *testing.T) {
	db, _ := newTestDeps(t)
	tr := &store.Transfer{ID: "t1", TargetPath: "/tmp/foo", TotalSize: 10, ChunkSize: 10, TotalChunks: 1, DeviceID: "owner"}
	if err := db.CreateTransfer(tr); err != nil {
		t.Fatalf("create transfer: %v", err)
	}

	req := asDevice(withURLParam(httptest.NewRequest(http.MethodDelete, "/v1/transfers/t1", nil), map[string]string{"id": "t1"}), "intruder")
	rr := httptest.NewRecorder()
	deleteTransferHandler(db)(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("want 404 for non-owner, got %d: %s", rr.Code, rr.Body.String())
	}
	// The row must survive the rejected delete.
	switch got, err := db.GetTransfer("t1"); {
	case err != nil:
		t.Fatalf("get transfer: %v", err)
	case got == nil:
		t.Fatal("transfer was deleted by a non-owner")
	}

	// The owner itself may delete it.
	req = asDevice(withURLParam(httptest.NewRequest(http.MethodDelete, "/v1/transfers/t1", nil), map[string]string{"id": "t1"}), "owner")
	rr = httptest.NewRecorder()
	deleteTransferHandler(db)(rr, req)
	if rr.Code != http.StatusNoContent {
		t.Fatalf("want 204 for owner, got %d: %s", rr.Code, rr.Body.String())
	}
}

// PR-03: /transfers/list must scope rows to the caller and withhold the
// whole-host aggregates and device/user filter lists from a non-admin.
func TestListTransfersHandler_ScopedToCaller(t *testing.T) {
	db, _ := newTestDeps(t)
	for _, tr := range []*store.Transfer{
		{ID: "mine", TargetPath: "/tmp/mine", TotalSize: 10, ChunkSize: 10, TotalChunks: 1, DeviceID: "me"},
		{ID: "theirs", TargetPath: "/srv/secret/theirs", TotalSize: 10, ChunkSize: 10, TotalChunks: 1, DeviceID: "them"},
	} {
		if err := db.CreateTransfer(tr); err != nil {
			t.Fatalf("create transfer %s: %v", tr.ID, err)
		}
	}

	// Non-admin passing ?device=them must NOT be able to widen its scope.
	req := asDevice(httptest.NewRequest(http.MethodGet, "/v1/transfers/list?device=them", nil), "me")
	rr := httptest.NewRecorder()
	listTransfersHandler(db)(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", rr.Code, rr.Body.String())
	}
	var got struct {
		Transfers []map[string]any `json:"transfers"`
		Devices   []map[string]any `json:"devices"`
		Users     []string         `json:"users"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got.Transfers) != 1 || got.Transfers[0]["id"] != "mine" {
		t.Fatalf("non-admin should see only its own transfer, got %v", got.Transfers)
	}
	if len(got.Devices) != 0 || len(got.Users) != 0 {
		t.Fatalf("non-admin must not receive device/user lists, got devices=%v users=%v", got.Devices, got.Users)
	}
	if strings.Contains(rr.Body.String(), "/srv/secret/theirs") {
		t.Fatalf("non-admin response leaked another device's path: %s", rr.Body.String())
	}

	// An admin sees both rows.
	req = asAdmin(httptest.NewRequest(http.MethodGet, "/v1/transfers/list", nil))
	rr = httptest.NewRecorder()
	listTransfersHandler(db)(rr, req)
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode admin: %v", err)
	}
	if len(got.Transfers) != 2 {
		t.Fatalf("admin should see both transfers, got %v", got.Transfers)
	}
}

// PR-03: the admin-only middleware gates the control-plane routes.
func TestAdminOnly(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
	tests := []struct {
		name string
		req  *http.Request
		want int
	}{
		{"admin", asAdmin(httptest.NewRequest(http.MethodGet, "/v1/logs", nil)), http.StatusOK},
		{"paired device", asDevice(httptest.NewRequest(http.MethodGet, "/v1/logs", nil), "d1"), http.StatusForbidden},
		{"no device", httptest.NewRequest(http.MethodGet, "/v1/logs", nil), http.StatusForbidden},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rr := httptest.NewRecorder()
			adminOnly(next).ServeHTTP(rr, tc.req)
			if rr.Code != tc.want {
				t.Fatalf("want %d, got %d", tc.want, rr.Code)
			}
		})
	}
}
