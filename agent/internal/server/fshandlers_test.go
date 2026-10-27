package server

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ZaidQamhieh/rfe/agent/internal/fsops"
)

// newFsFixture creates a temp dir with a few files and returns an Ops rooted there.
func newFsFixture(t *testing.T) (*fsops.Ops, string) {
	t.Helper()
	root := t.TempDir()
	for _, name := range []string{"a.txt", "b.txt"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte("hello"), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	if err := os.Mkdir(filepath.Join(root, "sub"), 0o755); err != nil {
		t.Fatalf("mkdir sub: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "sub", "c.txt"), []byte("nested"), 0o644); err != nil {
		t.Fatalf("write sub/c.txt: %v", err)
	}
	return fsops.New([]string{root}, false), root
}

// ---- listDirHandler ----

func TestListDirHandler_OK(t *testing.T) {
	ops, root := newFsFixture(t)
	req := httptest.NewRequest(http.MethodGet, "/v1/fs?path="+root, nil)
	rr := httptest.NewRecorder()
	listDirHandler(ops)(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	var listing fsops.Listing
	if err := json.Unmarshal(rr.Body.Bytes(), &listing); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(listing.Entries) == 0 {
		t.Fatal("expected entries")
	}
}

func TestListDirHandler_MissingPath(t *testing.T) {
	ops, _ := newFsFixture(t)
	req := httptest.NewRequest(http.MethodGet, "/v1/fs", nil)
	rr := httptest.NewRecorder()
	listDirHandler(ops)(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestListDirHandler_NotFound(t *testing.T) {
	ops, root := newFsFixture(t)
	req := httptest.NewRequest(http.MethodGet, "/v1/fs?path="+root+"/nonexistent", nil)
	rr := httptest.NewRecorder()
	listDirHandler(ops)(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestListDirHandler_WithLimit(t *testing.T) {
	ops, root := newFsFixture(t)
	req := httptest.NewRequest(http.MethodGet, "/v1/fs?path="+root+"&limit=1", nil)
	rr := httptest.NewRecorder()
	listDirHandler(ops)(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	var listing fsops.Listing
	if err := json.Unmarshal(rr.Body.Bytes(), &listing); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(listing.Entries) > 1 {
		t.Fatalf("expected at most 1 entry, got %d", len(listing.Entries))
	}
}

// ---- createFolderHandler ----

func TestCreateFolderHandler_OK(t *testing.T) {
	ops, root := newFsFixture(t)
	body := `{"path":"` + root + `/newfolder"}`
	req := httptest.NewRequest(http.MethodPost, "/v1/fs/folder", strings.NewReader(body))
	rr := httptest.NewRecorder()
	createFolderHandler(ops)(rr, req)

	if rr.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rr.Code, rr.Body.String())
	}
	if _, err := os.Stat(filepath.Join(root, "newfolder")); err != nil {
		t.Fatalf("folder not created: %v", err)
	}
}

func TestCreateFolderHandler_MissingPath(t *testing.T) {
	ops, _ := newFsFixture(t)
	req := httptest.NewRequest(http.MethodPost, "/v1/fs/folder", strings.NewReader(`{}`))
	rr := httptest.NewRecorder()
	createFolderHandler(ops)(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

// ---- createFileHandler ----

func TestCreateFileHandler_OK(t *testing.T) {
	ops, root := newFsFixture(t)
	body := `{"path":"` + root + `/newfile.txt"}`
	req := httptest.NewRequest(http.MethodPost, "/v1/fs/file", strings.NewReader(body))
	rr := httptest.NewRecorder()
	createFileHandler(ops)(rr, req)

	if rr.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rr.Code, rr.Body.String())
	}
	if _, err := os.Stat(filepath.Join(root, "newfile.txt")); err != nil {
		t.Fatalf("file not created: %v", err)
	}
}

func TestCreateFileHandler_MissingPath(t *testing.T) {
	ops, _ := newFsFixture(t)
	req := httptest.NewRequest(http.MethodPost, "/v1/fs/file", strings.NewReader(`{}`))
	rr := httptest.NewRecorder()
	createFileHandler(ops)(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

// ---- renameHandler ----

func TestRenameHandler_OK(t *testing.T) {
	ops, root := newFsFixture(t)
	src := filepath.Join(root, "a.txt")
	dst := filepath.Join(root, "renamed.txt")
	body := `{"src":"` + src + `","dst":"` + dst + `"}`
	req := httptest.NewRequest(http.MethodPatch, "/v1/fs/rename", strings.NewReader(body))
	rr := httptest.NewRecorder()
	renameHandler(ops)(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	if _, err := os.Stat(dst); err != nil {
		t.Fatalf("renamed file missing: %v", err)
	}
	if _, err := os.Stat(src); err == nil {
		t.Fatal("original file still exists")
	}
}

func TestRenameHandler_MissingFields(t *testing.T) {
	ops, _ := newFsFixture(t)
	req := httptest.NewRequest(http.MethodPatch, "/v1/fs/rename", strings.NewReader(`{"src":"x"}`))
	rr := httptest.NewRecorder()
	renameHandler(ops)(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

// ---- deleteHandler ----

func TestDeleteHandler_PermanentQueryParam(t *testing.T) {
	ops, root := newFsFixture(t)
	target := filepath.Join(root, "a.txt")
	trashDir := t.TempDir()

	req := httptest.NewRequest(http.MethodDelete, "/v1/fs?path="+target+"&permanent=true", nil)
	rr := httptest.NewRecorder()
	deleteHandler(ops, trashDir)(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	if _, err := os.Stat(target); err == nil {
		t.Fatal("file should have been deleted")
	}
}

func TestDeleteHandler_MissingPaths(t *testing.T) {
	ops, _ := newFsFixture(t)
	req := httptest.NewRequest(http.MethodDelete, "/v1/fs", nil)
	rr := httptest.NewRecorder()
	deleteHandler(ops, t.TempDir())(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestDeleteHandler_JSONBody(t *testing.T) {
	ops, root := newFsFixture(t)
	trashDir := t.TempDir()
	body := `{"paths":["` + filepath.Join(root, "a.txt") + `","` + filepath.Join(root, "b.txt") + `"]}`
	req := httptest.NewRequest(http.MethodDelete, "/v1/fs?permanent=true", strings.NewReader(body))
	req.Header.Set("Content-Length", "999")
	rr := httptest.NewRecorder()
	deleteHandler(ops, trashDir)(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestDeleteHandler_TrashMode(t *testing.T) {
	ops, root := newFsFixture(t)
	target := filepath.Join(root, "b.txt")
	trashDir := t.TempDir()

	req := httptest.NewRequest(http.MethodDelete, "/v1/fs?path="+target, nil)
	rr := httptest.NewRecorder()
	deleteHandler(ops, trashDir)(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
}

// ---- copyHandler ----

func TestCopyHandler_OK(t *testing.T) {
	ops, root := newFsFixture(t)
	dest := filepath.Join(root, "sub")
	body := `{"sources":["` + filepath.Join(root, "a.txt") + `"],"destDir":"` + dest + `"}`
	req := httptest.NewRequest(http.MethodPost, "/v1/fs/copy", strings.NewReader(body))
	rr := httptest.NewRecorder()
	copyHandler(ops)(rr, req)

	if rr.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d: %s", rr.Code, rr.Body.String())
	}
	if _, err := os.Stat(filepath.Join(dest, "a.txt")); err != nil {
		t.Fatalf("copy target missing: %v", err)
	}
	// Original should still exist.
	if _, err := os.Stat(filepath.Join(root, "a.txt")); err != nil {
		t.Fatalf("original missing after copy: %v", err)
	}
}

func TestCopyHandler_MissingFields(t *testing.T) {
	ops, _ := newFsFixture(t)
	req := httptest.NewRequest(http.MethodPost, "/v1/fs/copy", strings.NewReader(`{"sources":[]}`))
	rr := httptest.NewRecorder()
	copyHandler(ops)(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

// ---- moveHandler ----

func TestMoveHandler_OK(t *testing.T) {
	ops, root := newFsFixture(t)
	src := filepath.Join(root, "a.txt")
	dest := filepath.Join(root, "sub")
	body := `{"sources":["` + src + `"],"destDir":"` + dest + `"}`
	req := httptest.NewRequest(http.MethodPost, "/v1/fs/move", strings.NewReader(body))
	rr := httptest.NewRecorder()
	moveHandler(ops)(rr, req)

	if rr.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d: %s", rr.Code, rr.Body.String())
	}
	if _, err := os.Stat(filepath.Join(dest, "a.txt")); err != nil {
		t.Fatalf("moved file missing: %v", err)
	}
	if _, err := os.Stat(src); err == nil {
		t.Fatal("source should have been removed")
	}
}

func TestMoveHandler_MissingFields(t *testing.T) {
	ops, _ := newFsFixture(t)
	req := httptest.NewRequest(http.MethodPost, "/v1/fs/move", strings.NewReader(`{}`))
	rr := httptest.NewRecorder()
	moveHandler(ops)(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

// ---- metaHandler ----

func TestMetaHandler_OK(t *testing.T) {
	ops, root := newFsFixture(t)
	target := filepath.Join(root, "a.txt")
	req := httptest.NewRequest(http.MethodGet, "/v1/fs/meta?path="+target, nil)
	rr := httptest.NewRecorder()
	metaHandler(ops)(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	var entry fsops.Entry
	if err := json.Unmarshal(rr.Body.Bytes(), &entry); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if entry.Name != "a.txt" {
		t.Fatalf("unexpected name: %s", entry.Name)
	}
}

func TestMetaHandler_MissingPath(t *testing.T) {
	ops, _ := newFsFixture(t)
	req := httptest.NewRequest(http.MethodGet, "/v1/fs/meta", nil)
	rr := httptest.NewRecorder()
	metaHandler(ops)(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestMetaHandler_NotFound(t *testing.T) {
	ops, root := newFsFixture(t)
	req := httptest.NewRequest(http.MethodGet, "/v1/fs/meta?path="+root+"/nope.txt", nil)
	rr := httptest.NewRecorder()
	metaHandler(ops)(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rr.Code)
	}
}

// ---- healthHandler ----

func TestHealthHandler_UnauthenticatedGetsMinimalFingerprint(t *testing.T) {
	db, st := newTestDeps(t)
	cfg := Config{
		Name:     "test-pc",
		Version:  "1.0.0",
		Address:  "192.168.1.1:8765",
		Settings: st,
	}
	req := httptest.NewRequest(http.MethodGet, "/v1/health", nil)
	rr := httptest.NewRecorder()
	healthHandler(cfg, db)(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	var resp map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["status"] != "ok" {
		t.Fatalf("expected status ok, got %v", resp["status"])
	}
	// No topology/identity fields for a caller without a valid device token
	// (PR-61) — an unpaired caller can't fingerprint or map the host.
	for _, field := range []string{"name", "version", "os", "readOnly", "address", "tailscaleAddress", "macAddress", "apiVersion", "features"} {
		if _, ok := resp[field]; ok {
			t.Fatalf("expected %q to be absent for an unauthenticated caller, got %v", field, resp[field])
		}
	}
}

func TestHealthHandler_AuthenticatedGetsFullDetail(t *testing.T) {
	db, st, token := newAuthTestDeps(t)
	cfg := Config{
		Name:       "test-pc",
		Version:    "1.0.0",
		Address:    "192.168.1.1:8765",
		MACAddress: "aa:bb:cc:dd:ee:ff",
		Settings:   st,
	}
	req := httptest.NewRequest(http.MethodGet, "/v1/health", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rr := httptest.NewRecorder()
	healthHandler(cfg, db)(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	var resp map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["name"] != "test-pc" {
		t.Fatalf("expected name test-pc, got %v", resp["name"])
	}
	if resp["macAddress"] != "aa:bb:cc:dd:ee:ff" {
		t.Fatalf("expected macAddress, got %v", resp["macAddress"])
	}
}

func TestHealthHandler_WebSessionCookieGetsFullDetail(t *testing.T) {
	db, st, token := newAuthTestDeps(t)
	cfg := Config{Name: "test-pc", Version: "1.0.0", Settings: st}
	req := httptest.NewRequest(http.MethodGet, "https://agent.example:8765/v1/health", nil)
	req.AddCookie(&http.Cookie{Name: webSessionCookie, Value: token})
	req.Header.Set(webSessionHeader, "1")
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	req.Header.Set("Origin", "https://agent.example:8765")
	rr := httptest.NewRecorder()
	healthHandler(cfg, db)(rr, req)

	var resp map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["name"] != "test-pc" {
		t.Fatalf("expected browser session to receive authenticated detail, got %v", resp)
	}
}

func TestHealthHandler_RevokedTokenGetsMinimalFingerprint(t *testing.T) {
	db, st, token := newAuthTestDeps(t)
	if err := db.RevokeDevice("dev1"); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	cfg := Config{Name: "test-pc", Version: "1.0.0", Settings: st}
	req := httptest.NewRequest(http.MethodGet, "/v1/health", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rr := httptest.NewRecorder()
	healthHandler(cfg, db)(rr, req)

	var resp map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if _, ok := resp["name"]; ok {
		t.Fatalf("expected no name for a revoked device token, got %v", resp["name"])
	}
}

// ---- handleFsError ----

func TestHandleFsError_AllCases(t *testing.T) {
	cases := []struct {
		err    error
		status int
		code   string
	}{
		{fsops.ErrForbidden, http.StatusForbidden, "FORBIDDEN"},
		{fsops.ErrNotFound, http.StatusNotFound, "PATH_NOT_FOUND"},
		{fsops.ErrReadOnly, http.StatusForbidden, "READ_ONLY"},
		{fsops.ErrUnsupported, http.StatusBadRequest, "UNSUPPORTED_FORMAT"},
		{fsops.ErrConflict, http.StatusConflict, "CONFLICT"},
		{fsops.ErrStale, http.StatusConflict, "STALE_WRITE"},
	}
	for _, tc := range cases {
		t.Run(tc.code, func(t *testing.T) {
			rr := httptest.NewRecorder()
			handleFsError(rr, tc.err)
			if rr.Code != tc.status {
				t.Fatalf("expected %d, got %d", tc.status, rr.Code)
			}
			var got apiError
			if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
				t.Fatalf("decode: %v", err)
			}
			if got.Code != tc.code {
				t.Fatalf("expected code %s, got %s", tc.code, got.Code)
			}
		})
	}
}

// ---- trash handlers ----

func TestListTrashHandler_EmptyTrash(t *testing.T) {
	trashDir := t.TempDir()
	req := httptest.NewRequest(http.MethodGet, "/v1/trash", nil)
	rr := httptest.NewRecorder()
	listTrashHandler(fsops.New(nil, false), trashDir)(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestEmptyTrashHandler_OK(t *testing.T) {
	trashDir := t.TempDir()
	req := httptest.NewRequest(http.MethodDelete, "/v1/trash", nil)
	rr := httptest.NewRecorder()
	emptyTrashHandler(fsops.New(nil, false), trashDir)(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestRestoreTrashHandler_MissingIDs(t *testing.T) {
	ops, _ := newFsFixture(t)
	trashDir := t.TempDir()
	req := httptest.NewRequest(http.MethodPost, "/v1/trash/restore", strings.NewReader(`{}`))
	rr := httptest.NewRecorder()
	restoreTrashHandler(ops, trashDir)(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

// ---- compressHandler ----

func TestCompressHandler_MissingFields(t *testing.T) {
	ops, _ := newFsFixture(t)
	req := httptest.NewRequest(http.MethodPost, "/v1/fs/compress", strings.NewReader(`{}`))
	rr := httptest.NewRecorder()
	compressHandler(ops)(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestCompressHandler_OK(t *testing.T) {
	ops, root := newFsFixture(t)
	dest := filepath.Join(root, "archive.zip")
	body := `{"sources":["` + filepath.Join(root, "a.txt") + `"],"dest":"` + dest + `"}`
	req := httptest.NewRequest(http.MethodPost, "/v1/fs/compress", strings.NewReader(body))
	rr := httptest.NewRecorder()
	compressHandler(ops)(rr, req)

	if rr.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rr.Code, rr.Body.String())
	}
}

// ---- extractHandler ----

func TestExtractHandler_MissingFields(t *testing.T) {
	ops, _ := newFsFixture(t)
	req := httptest.NewRequest(http.MethodPost, "/v1/fs/extract", strings.NewReader(`{}`))
	rr := httptest.NewRecorder()
	extractHandler(ops)(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestExtractHandler_OK(t *testing.T) {
	ops, root := newFsFixture(t)
	// First create a zip to extract.
	archive := filepath.Join(root, "test.zip")
	cBody := `{"sources":["` + filepath.Join(root, "a.txt") + `"],"dest":"` + archive + `"}`
	cReq := httptest.NewRequest(http.MethodPost, "/v1/fs/compress", strings.NewReader(cBody))
	cRR := httptest.NewRecorder()
	compressHandler(ops)(cRR, cReq)
	if cRR.Code != http.StatusCreated {
		t.Fatalf("compress: expected 201, got %d: %s", cRR.Code, cRR.Body.String())
	}

	outDir := filepath.Join(root, "extracted")
	if err := os.Mkdir(outDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	body := `{"archive":"` + archive + `","destDir":"` + outDir + `"}`
	req := httptest.NewRequest(http.MethodPost, "/v1/fs/extract", strings.NewReader(body))
	rr := httptest.NewRecorder()
	extractHandler(ops)(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestHandleFsError_DefaultCase(t *testing.T) {
	rr := httptest.NewRecorder()
	handleFsError(rr, errors.New("some random error"))
	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", rr.Code)
	}
	var got apiError
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Code != "INTERNAL" {
		t.Fatalf("expected INTERNAL, got %s", got.Code)
	}
}

func TestEmptyTrashHandler_WithIDs(t *testing.T) {
	trashDir := t.TempDir()
	body := `{"ids":["nonexistent-id"]}`
	req := httptest.NewRequest(http.MethodDelete, "/v1/trash", strings.NewReader(body))
	req.Header.Set("Content-Length", "999")
	rr := httptest.NewRecorder()
	emptyTrashHandler(fsops.New(nil, false), trashDir)(rr, req)

	// Should succeed even with nonexistent IDs (idempotent).
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
}

// A body that fails to decode must be rejected, never read as "empty all".
func TestEmptyTrashHandler_BadBodyKeepsTrash(t *testing.T) {
	cases := map[string]func() *http.Request{
		"wrong type": func() *http.Request {
			return httptest.NewRequest(http.MethodDelete, "/v1/trash", strings.NewReader(`{"ids":"x"}`))
		},
		"malformed json": func() *http.Request {
			return httptest.NewRequest(http.MethodDelete, "/v1/trash", strings.NewReader(`{"ids":[`))
		},
		"empty object": func() *http.Request {
			return httptest.NewRequest(http.MethodDelete, "/v1/trash", strings.NewReader(`{}`))
		},
		"empty ids": func() *http.Request {
			return httptest.NewRequest(http.MethodDelete, "/v1/trash", strings.NewReader(`{"ids":[]}`))
		},
		"null": func() *http.Request {
			return httptest.NewRequest(http.MethodDelete, "/v1/trash", strings.NewReader(`null`))
		},
		"unsized body": func() *http.Request {
			req := httptest.NewRequest(http.MethodDelete, "/v1/trash", io.NopCloser(strings.NewReader(`{"ids":"x"}`)))
			req.ContentLength = -1
			return req
		},
	}
	for name, mk := range cases {
		t.Run(name, func(t *testing.T) {
			ops, root := newFsFixture(t)
			trashDir := filepath.Join(root, ".trash")
			if err := os.Mkdir(trashDir, 0o755); err != nil {
				t.Fatalf("mkdir trash: %v", err)
			}
			ops.MoveToTrash([]string{filepath.Join(root, "a.txt")}, trashDir)
			before, err := os.ReadDir(trashDir)
			if err != nil || len(before) == 0 {
				t.Fatalf("expected trash items before the call, got %d (err %v)", len(before), err)
			}

			rr := httptest.NewRecorder()
			emptyTrashHandler(ops, trashDir)(rr, mk())
			if rr.Code != http.StatusBadRequest {
				t.Fatalf("expected 400, got %d: %s", rr.Code, rr.Body.String())
			}
			after, _ := os.ReadDir(trashDir)
			if len(after) != len(before) {
				t.Fatalf("trash changed on a rejected request: %d entries before, %d after", len(before), len(after))
			}
		})
	}
}

// An unsized body that does decode still purges only the listed ids.
func TestEmptyTrashHandler_UnsizedValidBody(t *testing.T) {
	trashDir := t.TempDir()
	req := httptest.NewRequest(http.MethodDelete, "/v1/trash", io.NopCloser(strings.NewReader(`{"ids":["nonexistent-id"]}`)))
	req.ContentLength = -1
	rr := httptest.NewRecorder()
	emptyTrashHandler(fsops.New(nil, false), trashDir)(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestListTrashHandler_WithItems(t *testing.T) {
	ops, root := newFsFixture(t)
	trashDir := filepath.Join(root, ".trash")
	if err := os.Mkdir(trashDir, 0o755); err != nil {
		t.Fatalf("mkdir trash: %v", err)
	}
	ops.MoveToTrash([]string{filepath.Join(root, "a.txt")}, trashDir)

	rr := httptest.NewRecorder()
	listTrashHandler(ops, trashDir)(rr, httptest.NewRequest(http.MethodGet, "/v1/trash", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	var got map[string]any
	_ = json.Unmarshal(rr.Body.Bytes(), &got)
	items, ok := got["items"].([]any)
	if !ok || len(items) == 0 {
		t.Fatalf("expected non-empty items, got %v", got)
	}
}

func TestRestoreTrashHandler_ValidRestore(t *testing.T) {
	ops, root := newFsFixture(t)
	trashDir := filepath.Join(root, ".trash")
	if err := os.Mkdir(trashDir, 0o755); err != nil {
		t.Fatalf("mkdir trash: %v", err)
	}
	ops.MoveToTrash([]string{filepath.Join(root, "a.txt")}, trashDir)

	items, err := fsops.ListTrash(trashDir)
	if err != nil || len(items) == 0 {
		t.Fatalf("expected trashed items: %v", err)
	}
	id := items[0].ID

	body := `{"ids":["` + id + `"]}`
	rr := httptest.NewRecorder()
	restoreTrashHandler(ops, trashDir)(rr, httptest.NewRequest(http.MethodPost, "/v1/trash/restore", strings.NewReader(body)))
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestCreateFolderHandler_ForbiddenOutsideRoot(t *testing.T) {
	ops, _ := newFsFixture(t)
	body := `{"path":"/tmp/outside-root-` + t.Name() + `/test"}`
	req := httptest.NewRequest(http.MethodPost, "/v1/fs/folder", strings.NewReader(body))
	rr := httptest.NewRecorder()
	createFolderHandler(ops)(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestCreateFileHandler_ForbiddenOutsideRoot(t *testing.T) {
	ops, _ := newFsFixture(t)
	body := `{"path":"/tmp/outside-root-` + t.Name() + `/test.txt"}`
	req := httptest.NewRequest(http.MethodPost, "/v1/fs/file", strings.NewReader(body))
	rr := httptest.NewRecorder()
	createFileHandler(ops)(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestRenameHandler_NotFound(t *testing.T) {
	ops, root := newFsFixture(t)
	body := `{"src":"` + filepath.Join(root, "nope.txt") + `","dst":"` + filepath.Join(root, "x.txt") + `"}`
	req := httptest.NewRequest(http.MethodPatch, "/v1/fs/rename", strings.NewReader(body))
	rr := httptest.NewRecorder()
	renameHandler(ops)(rr, req)

	if rr.Code == http.StatusOK {
		t.Fatal("expected error for nonexistent source")
	}
}

// A chunked request has ContentLength -1; its paths must still be read (a client that streams the body used
// to get "no paths" or, worse, a delete of only the query path).
func TestDeleteHandler_ChunkedBody(t *testing.T) {
	ops, root := newFsFixture(t)
	target := filepath.Join(root, "a.txt")
	body, _ := json.Marshal(map[string]any{"paths": []string{target}})
	req := httptest.NewRequest(http.MethodDelete, "/v1/fs?permanent=true", io.NopCloser(bytes.NewReader(body)))
	req.ContentLength = -1
	rr := httptest.NewRecorder()
	deleteHandler(ops, t.TempDir())(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	if _, err := os.Stat(target); err == nil {
		t.Fatal("file should have been deleted")
	}
}

func TestDeleteHandler_MalformedBodyIsRejected(t *testing.T) {
	ops, root := newFsFixture(t)
	target := filepath.Join(root, "a.txt")
	req := httptest.NewRequest(http.MethodDelete, "/v1/fs?path="+target+"&permanent=true", strings.NewReader("{not json"))
	rr := httptest.NewRecorder()
	deleteHandler(ops, t.TempDir())(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
	if _, err := os.Stat(target); err != nil {
		t.Fatal("nothing may be deleted on a malformed body")
	}
}

func TestHealthHandler_AdvertisesAPIVersionAndSortedFeatures(t *testing.T) {
	db, st, token := newAuthTestDeps(t)
	req := httptest.NewRequest(http.MethodGet, "/v1/health", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rr := httptest.NewRecorder()
	healthHandler(Config{Settings: st}, db)(rr, req)
	var resp struct {
		APIVersion int      `json:"apiVersion"`
		Features   []string `json:"features"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.APIVersion != APIVersion || len(resp.Features) == 0 {
		t.Fatalf("got %+v", resp)
	}
	for i := 1; i < len(resp.Features); i++ {
		if resp.Features[i-1] >= resp.Features[i] {
			t.Fatalf("features not sorted/unique: %v", resp.Features)
		}
	}
}
