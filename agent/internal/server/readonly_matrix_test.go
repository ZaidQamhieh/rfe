package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/ZaidQamhieh/rfe/agent/internal/fsops"
	"github.com/ZaidQamhieh/rfe/agent/internal/store"
)

// TestRouteMatrix_ReadOnlyBlocksEveryMutation is the PR-81 regression: a
// table-driven pass over every fs/trash/content mutating route (registered
// exactly as server.New wires them), proving the read-only invariant end to
// end through real routing rather than by calling handlers directly.
//
// The invariant has two shapes, both asserted here:
//   - Single-target routes (folder/file/rename/compress/extract/chmod/
//     content/empty-trash) reject the whole request with 403.
//   - Batch routes (copy/move/delete/trash-restore) accept the request but
//     degrade every item to a READ_ONLY BatchResult instead of a top-level
//     error — see fsops.Copy/Move/Delete/MoveToTrash/RestoreFromTrash. A test
//     that only checked for 403 everywhere would wrongly flag these as bugs.
//
// Transfer routes have their own equivalent,
// TestRegisterTransferRoutes_ReadOnlyWiring, in transferhandlers_test.go.
func TestRouteMatrix_ReadOnlyBlocksEveryMutation(t *testing.T) {
	root := t.TempDir()
	cfg := Config{TrashDir: t.TempDir()}
	roOps := fsops.New([]string{root}, true)

	r := chi.NewRouter()
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			next.ServeHTTP(w, req.WithContext(withDevice(req.Context(), &store.Device{ViaLogin: true})))
		})
	})
	r.Use(deviceJailMiddleware(roOps))
	r.Route("/v1", func(r chi.Router) {
		registerFsRoutes(r, cfg, roOps, noIndex{})
		registerTrashRoutes(r, cfg, roOps)
		registerContentRoutes(r, cfg, roOps)
	})

	blocked := []struct{ method, path, body string }{
		{http.MethodPost, "/v1/fs/folder", `{"path":"/new"}`},
		{http.MethodPost, "/v1/fs/file", `{"path":"/new.txt"}`},
		{http.MethodPatch, "/v1/fs/rename", `{"src":"/a","dst":"/b"}`},
		{http.MethodPost, "/v1/fs/compress", `{"sources":["/a"],"dest":"/a.zip"}`},
		{http.MethodPost, "/v1/fs/extract", `{"archive":"/a.zip","destDir":"/out"}`},
		{http.MethodPost, "/v1/fs/chmod", `{"path":"/a","mode":"644"}`},
		{http.MethodPut, "/v1/content?path=/a", `hello`},
		{http.MethodDelete, "/v1/trash", ``},
	}
	for _, tc := range blocked {
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			rr := httptest.NewRecorder()
			r.ServeHTTP(rr, httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body)))
			if rr.Code != http.StatusForbidden {
				t.Fatalf("want 403, got %d: %s", rr.Code, rr.Body.String())
			}
		})
	}

	degraded := []struct{ method, path, body string }{
		{http.MethodDelete, "/v1/fs?path=/a", ``},
		{http.MethodPost, "/v1/fs/copy", `{"sources":["/a"],"destDir":"/out"}`},
		{http.MethodPost, "/v1/fs/move", `{"sources":["/a"],"destDir":"/out"}`},
		{http.MethodPost, "/v1/trash/restore", `{"ids":["abc"]}`},
	}
	for _, tc := range degraded {
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			rr := httptest.NewRecorder()
			r.ServeHTTP(rr, httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body)))
			if rr.Code == http.StatusForbidden {
				t.Fatalf("batch route must not 403 the whole request, got %d", rr.Code)
			}
			var body struct {
				Results []fsops.BatchResult `json:"results"`
			}
			if err := json.NewDecoder(rr.Body).Decode(&body); err != nil {
				t.Fatalf("decode: %v", err)
			}
			if len(body.Results) == 0 {
				t.Fatalf("expected at least one result")
			}
			for _, res := range body.Results {
				if res.OK || res.Error == nil || res.Error.Code != "READ_ONLY" {
					t.Fatalf("expected a READ_ONLY result, got %+v", res)
				}
			}
		})
	}

	// Sanity: a read route on the same router must not be gated.
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/v1/fs?path="+root, nil))
	if rr.Code == http.StatusForbidden {
		t.Fatalf("read route must not be read-only gated, got %d: %s", rr.Code, rr.Body.String())
	}
}
