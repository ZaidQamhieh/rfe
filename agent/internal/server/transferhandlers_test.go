package server

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/ZaidQamhieh/rfe/agent/internal/fsops"
	"github.com/ZaidQamhieh/rfe/agent/internal/store"
	"github.com/ZaidQamhieh/rfe/agent/internal/transfer"
)

// TestTransferStatus_ForeignDeviceGets404 is the PR-11 regression: a paired
// device that did not open a transfer cannot observe it — the response is
// indistinguishable from "no such transfer".
func TestTransferStatus_ForeignDeviceGets404(t *testing.T) {
	tm, _ := newTestTransferManager(t)
	target := filepath.Join(t.TempDir(), "out.bin")
	id := uuid.New().String()
	if _, err := tm.OpenSession(id, target, 10, 16, "deadbeef", false, "owner-1"); err != nil {
		t.Fatalf("OpenSession: %v", err)
	}

	// Foreign non-admin device → 404.
	rr := httptest.NewRecorder()
	req := withURLParam(httptest.NewRequest(http.MethodGet, "/v1/transfers/"+id, nil), map[string]string{"id": id})
	req = req.WithContext(withDevice(req.Context(), &store.Device{ID: "other-1", ViaLogin: false}))
	transferStatusHandler(tm)(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("foreign device: expected 404, got %d", rr.Code)
	}

	// The owning device → 200.
	rr2 := httptest.NewRecorder()
	req2 := withURLParam(httptest.NewRequest(http.MethodGet, "/v1/transfers/"+id, nil), map[string]string{"id": id})
	req2 = req2.WithContext(withDevice(req2.Context(), &store.Device{ID: "owner-1"}))
	transferStatusHandler(tm)(rr2, req2)
	if rr2.Code != http.StatusOK {
		t.Fatalf("owner: expected 200, got %d: %s", rr2.Code, rr2.Body.String())
	}
}

// newTestTransferManager builds a transfer.Manager backed by a fresh DB and
// temp dir, plus an Ops with no path jail.
func newTestTransferManager(t *testing.T) (*transfer.Manager, *fsops.Ops) {
	t.Helper()
	db, st := newTestDeps(t)
	tm, err := transfer.New(db, t.TempDir())
	if err != nil {
		t.Fatalf("transfer.New: %v", err)
	}
	ops := fsops.NewWithSettings(st)
	return tm, ops
}

// withURLParam returns req with chi URL params set, so chi.URLParam works
// when invoking handlers directly (without the router).
func withURLParam(req *http.Request, params map[string]string) *http.Request {
	rctx := chi.NewRouteContext()
	for k, v := range params {
		rctx.URLParams.Add(k, v)
	}
	return req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
}

func sha256hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// TestOpenTransferHandler_ChunkSizeOverCapIs400 verifies the open-session
// handler rejects a chunkSize above the 32MiB cap with a 400.
func TestOpenTransferHandler_ChunkSizeOverCapIs400(t *testing.T) {
	tm, ops := newTestTransferManager(t)

	body := `{"path":"file.bin","size":100,"sha256":"deadbeef","chunkSize":33554433}`
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/transfers", strings.NewReader(body))
	openTransferHandler(tm, ops)(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rr.Code, rr.Body.String())
	}
	var got apiError
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode error body: %v", err)
	}
	if got.Code != "BAD_REQUEST" {
		t.Fatalf("unexpected error code: %+v", got)
	}
}

// TestOpenTransferHandler_NegativeSizeIs400 verifies the open-session handler
// rejects a negative size with a 400 BAD_REQUEST. A negative size would break
// the totalChunks calculation and os.Truncate downstream.
func TestOpenTransferHandler_NegativeSizeIs400(t *testing.T) {
	tm, ops := newTestTransferManager(t)

	body := `{"path":"file.bin","size":-1,"sha256":"deadbeef","chunkSize":1024}`
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/transfers", strings.NewReader(body))
	openTransferHandler(tm, ops)(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rr.Code, rr.Body.String())
	}
	var got apiError
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode error body: %v", err)
	}
	if got.Code != "BAD_REQUEST" {
		t.Fatalf("unexpected error code: %+v", got)
	}
}

// TestOpenTransferHandler_ZeroSizeIsAllowed sanity-checks that a zero-byte
// file (a legitimate upload) is accepted.
func TestOpenTransferHandler_ZeroSizeIsAllowed(t *testing.T) {
	tm, ops := newTestTransferManager(t)

	target := filepath.Join(t.TempDir(), "empty.bin")
	body := `{"path":"` + target + `","size":0,"sha256":"` + sha256hex(nil) + `","chunkSize":1024}`
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/transfers", strings.NewReader(body))
	openTransferHandler(tm, ops)(rr, req)

	if rr.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rr.Code, rr.Body.String())
	}
}

// TestOpenTransferHandler_DestinationExistsIs409 verifies that opening an
// upload session for a target path that already exists, without overwrite,
// returns 409 CONFLICT rather than 500 INTERNAL.
func TestOpenTransferHandler_DestinationExistsIs409(t *testing.T) {
	tm, ops := newTestTransferManager(t)

	target := filepath.Join(t.TempDir(), "existing.bin")
	if err := os.WriteFile(target, []byte("already here"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	body := `{"path":"` + target + `","size":11,"sha256":"` + sha256hex([]byte("hello world")) + `","chunkSize":1024}`
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/transfers", strings.NewReader(body))
	openTransferHandler(tm, ops)(rr, req)

	if rr.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d: %s", rr.Code, rr.Body.String())
	}
	var got apiError
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode error body: %v", err)
	}
	if got.Code != "CONFLICT" {
		t.Fatalf("unexpected error code: %+v", got)
	}
}

// TestOpenTransferHandler_OverwriteBypassesConflict verifies that
// overwrite=true allows opening a session for a target path that already
// exists.
func TestOpenTransferHandler_OverwriteBypassesConflict(t *testing.T) {
	tm, ops := newTestTransferManager(t)

	target := filepath.Join(t.TempDir(), "existing.bin")
	if err := os.WriteFile(target, []byte("already here"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	body := `{"path":"` + target + `","size":11,"sha256":"` + sha256hex([]byte("hello world")) + `","chunkSize":1024,"overwrite":true}`
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/transfers", strings.NewReader(body))
	req = asAdmin(req)
	openTransferHandler(tm, ops)(rr, req)

	if rr.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestCompleteTransferOverwriteRechecksModifyGrant(t *testing.T) {
	tm, ops := newTestTransferManager(t)
	target := filepath.Join(t.TempDir(), "replace.bin")
	if err := os.WriteFile(target, []byte("old"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	id := uuid.New().String()
	if _, err := tm.OpenSession(id, target, 0, 1024, sha256hex(nil), true, "guest"); err != nil {
		t.Fatalf("OpenSession: %v", err)
	}

	req := withURLParam(httptest.NewRequest(http.MethodPost, "/v1/transfers/"+id+"/complete", nil), map[string]string{"id": id})
	req = req.WithContext(withDevice(req.Context(), &store.Device{ID: "guest", CanUpload: true, CanModify: false}))
	rr := httptest.NewRecorder()
	completeTransferHandler(tm, ops)(rr, req)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("overwrite completion without modify should return 403, got %d: %s", rr.Code, rr.Body.String())
	}
	var apiErr apiError
	if err := json.Unmarshal(rr.Body.Bytes(), &apiErr); err != nil || apiErr.Code != "CAPABILITY_DENIED" {
		t.Fatalf("want CAPABILITY_DENIED, got %+v (%v)", apiErr, err)
	}
}

func TestUploadChunkOverwriteRechecksModifyGrant(t *testing.T) {
	tm, _ := newTestTransferManager(t)
	target := filepath.Join(t.TempDir(), "replace.bin")
	id := uuid.New().String()
	if _, err := tm.OpenSession(id, target, 0, 1024, sha256hex(nil), true, "guest"); err != nil {
		t.Fatalf("OpenSession: %v", err)
	}

	req := withURLParam(httptest.NewRequest(http.MethodPut, "/v1/transfers/"+id+"/chunks/0", nil), map[string]string{"id": id, "n": "0"})
	req.Header.Set("X-Chunk-Sha256", sha256hex(nil))
	req = req.WithContext(withDevice(req.Context(), &store.Device{ID: "guest", CanUpload: true, CanModify: false}))
	rr := httptest.NewRecorder()
	uploadChunkHandler(tm)(rr, req)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("overwrite chunk without modify should return 403, got %d: %s", rr.Code, rr.Body.String())
	}
	var apiErr apiError
	if err := json.Unmarshal(rr.Body.Bytes(), &apiErr); err != nil || apiErr.Code != "CAPABILITY_DENIED" {
		t.Fatalf("want CAPABILITY_DENIED, got %+v (%v)", apiErr, err)
	}
}

// TestOpenTransferHandler_ChunkSizeAtCapIsAllowed sanity-checks the boundary:
// exactly 32MiB is accepted.
func TestOpenTransferHandler_ChunkSizeAtCapIsAllowed(t *testing.T) {
	tm, ops := newTestTransferManager(t)

	target := filepath.Join(t.TempDir(), "file.bin")
	body := `{"path":"` + target + `","size":100,"sha256":"deadbeef","chunkSize":33554432}`
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/transfers", strings.NewReader(body))
	openTransferHandler(tm, ops)(rr, req)

	if rr.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rr.Code, rr.Body.String())
	}
}

// TestUploadChunkHandler_OversizedBodyIs413 verifies a chunk body larger than
// the session's chunkSize is rejected with 413, using the error envelope.
func TestUploadChunkHandler_OversizedBodyIs413(t *testing.T) {
	tm, ops := newTestTransferManager(t)

	target := filepath.Join(t.TempDir(), "out.bin")
	const chunkSize = 16
	content := make([]byte, chunkSize*2)
	id := uuid.New().String()
	sess, err := tm.OpenSession(id, target, int64(len(content)), chunkSize, sha256hex(content), false, "")
	if err != nil {
		t.Fatalf("OpenSession: %v", err)
	}
	_ = ops // ops unused beyond constructing tm in this subtest

	// Body larger than chunkSize.
	oversized := make([]byte, chunkSize+1)
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/v1/transfers/"+id+"/chunks/0", strings.NewReader(string(oversized)))
	req.Header.Set("X-Chunk-Sha256", sha256hex(oversized))
	req = asAdmin(withURLParam(req, map[string]string{"id": id, "n": "0"}))

	uploadChunkHandler(tm)(rr, req)

	if rr.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("expected 413, got %d: %s", rr.Code, rr.Body.String())
	}
	var got apiError
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode error body: %v", err)
	}
	if got.Code != "PAYLOAD_TOO_LARGE" {
		t.Fatalf("unexpected error code: %+v", got)
	}

	// Sanity: the session is unaffected (still open, no chunk recorded).
	got2, err := tm.Status(sess.ID)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if len(got2.ReceivedChunks) != 0 {
		t.Fatalf("expected no chunks recorded, got %v", got2.ReceivedChunks)
	}
}

// TestCompleteTransferHandler_SuccessIncludesVerifiedSHA256 verifies that a
// successful POST /transfers/{id}/complete response includes verified=true
// and the expected whole-file sha256 (H3 — post-transfer integrity check).
func TestCompleteTransferHandler_SuccessIncludesVerifiedSHA256(t *testing.T) {
	tm, ops := newTestTransferManager(t)

	content := []byte("hello world, this is the uploaded file content")
	wantSHA256 := sha256hex(content)
	target := filepath.Join(t.TempDir(), "out.bin")

	id := uuid.New().String()
	if _, err := tm.OpenSession(id, target, int64(len(content)), 1024, wantSHA256, false, ""); err != nil {
		t.Fatalf("OpenSession: %v", err)
	}

	// Write the single chunk.
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/v1/transfers/"+id+"/chunks/0", strings.NewReader(string(content)))
	req.Header.Set("X-Chunk-Sha256", sha256hex(content))
	req = asAdmin(withURLParam(req, map[string]string{"id": id, "n": "0"}))
	uploadChunkHandler(tm)(rr, req)
	if rr.Code != http.StatusNoContent {
		t.Fatalf("upload chunk: expected 204, got %d: %s", rr.Code, rr.Body.String())
	}

	// Complete the transfer.
	rr = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/v1/transfers/"+id+"/complete", nil)
	req = asAdmin(withURLParam(req, map[string]string{"id": id}))
	completeTransferHandler(tm, ops)(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	var got map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	verified, ok := got["verified"].(bool)
	if !ok || !verified {
		t.Fatalf("expected verified=true, got %+v", got["verified"])
	}
	if got["sha256"] != wantSHA256 {
		t.Fatalf("expected sha256=%s, got %v", wantSHA256, got["sha256"])
	}
	// Sanity: the usual Entry fields are still present.
	if got["path"] == nil || got["size"] == nil {
		t.Fatalf("expected Entry fields in response, got %+v", got)
	}
}

// TestUploadChunkHandler_ExactSizeIsAccepted sanity-checks that a body
// exactly matching chunkSize still succeeds.
func TestUploadChunkHandler_ExactSizeIsAccepted(t *testing.T) {
	tm, _ := newTestTransferManager(t)

	target := filepath.Join(t.TempDir(), "out.bin")
	const chunkSize = 16
	content := make([]byte, chunkSize)
	for i := range content {
		content[i] = byte(i)
	}
	id := uuid.New().String()
	if _, err := tm.OpenSession(id, target, int64(len(content)), chunkSize, sha256hex(content), false, ""); err != nil {
		t.Fatalf("OpenSession: %v", err)
	}

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/v1/transfers/"+id+"/chunks/0", strings.NewReader(string(content)))
	req.Header.Set("X-Chunk-Sha256", sha256hex(content))
	req = asAdmin(withURLParam(req, map[string]string{"id": id, "n": "0"}))

	uploadChunkHandler(tm)(rr, req)

	if rr.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", rr.Code, rr.Body.String())
	}
}

// TestRequireWritable is the PR-04 regression: the resumable-upload routes
// used to be the one way around read-only. The gate must honour both the
// global setting and the per-device ops the jail middleware installs.
func TestRequireWritable(t *testing.T) {
	root := t.TempDir()
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })

	tests := []struct {
		name string
		ops  *fsops.Ops // installed in the request context, as deviceJailMiddleware does
		want int
	}{
		{"writable", fsops.New([]string{root}, false), http.StatusOK},
		{"read-only", fsops.New([]string{root}, true), http.StatusForbidden},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/v1/transfers", nil)
			req = req.WithContext(context.WithValue(req.Context(), opsCtxKey, tc.ops))
			rr := httptest.NewRecorder()
			// base ops is writable: only the context ops must decide.
			requireWritable(fsops.New([]string{root}, false))(next).ServeHTTP(rr, req)
			if rr.Code != tc.want {
				t.Fatalf("want %d, got %d: %s", tc.want, rr.Code, rr.Body.String())
			}
		})
	}

	// With no ops in the context, the base ops decides.
	req := httptest.NewRequest(http.MethodPost, "/v1/transfers", nil)
	rr := httptest.NewRecorder()
	requireWritable(fsops.New([]string{root}, true))(next).ServeHTTP(rr, req)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("read-only base ops: want 403, got %d", rr.Code)
	}
}

// TestRegisterTransferRoutes_ReadOnlyWiring proves the PR-04 gate is actually
// wired onto the mutating routes (the middleware being correct is useless if
// the routes bypass it), and that read status stays reachable.
func TestRegisterTransferRoutes_ReadOnlyWiring(t *testing.T) {
	db, _ := newTestDeps(t)
	tm, err := transfer.New(db, t.TempDir())
	if err != nil {
		t.Fatalf("transfer.New: %v", err)
	}
	roOps := fsops.New([]string{t.TempDir()}, true)

	r := chi.NewRouter()
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			next.ServeHTTP(w, req.WithContext(withDevice(req.Context(), &store.Device{ViaLogin: true})))
		})
	})
	r.Route("/v1", func(r chi.Router) {
		registerTransferRoutes(r, tm, Config{}, roOps)
	})

	for _, tc := range []struct{ method, path string }{
		{http.MethodPost, "/v1/transfers"},
		{http.MethodPut, "/v1/transfers/abc/chunks/0"},
		{http.MethodPost, "/v1/transfers/abc/complete"},
	} {
		rr := httptest.NewRecorder()
		r.ServeHTTP(rr, httptest.NewRequest(tc.method, tc.path, strings.NewReader("{}")))
		if rr.Code != http.StatusForbidden {
			t.Fatalf("%s %s: want 403 under read-only, got %d: %s", tc.method, tc.path, rr.Code, rr.Body.String())
		}
	}

	// GET status is not a mutation — it must not be gated (404: no such row).
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/v1/transfers/abc", nil))
	if rr.Code == http.StatusForbidden {
		t.Fatalf("GET status must not be read-only gated, got 403")
	}
}
