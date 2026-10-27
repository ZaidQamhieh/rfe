package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/ZaidQamhieh/rfe/agent/internal/fsops"
	"github.com/ZaidQamhieh/rfe/agent/internal/store"
	"github.com/ZaidQamhieh/rfe/agent/internal/transfer"
)

func TestFileCapabilityRouteGates(t *testing.T) {
	db, st := newTestDeps(t)
	if err := st.SetAllowSharing(true); err != nil {
		t.Fatalf("enable sharing: %v", err)
	}
	ops, root := newFsFixture(t)
	tm, err := transfer.New(db, t.TempDir())
	if err != nil {
		t.Fatalf("transfer.New: %v", err)
	}

	device := &store.Device{ID: "guest", CanBrowse: true}
	r := chi.NewRouter()
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			next.ServeHTTP(w, req.WithContext(withDevice(req.Context(), device)))
		})
	})
	r.Use(deviceJailMiddleware(ops))
	r.Route("/v1", func(r chi.Router) {
		registerShareRoutes(r, Config{Address: "127.0.0.1:8765", Settings: st}, db, ops)
		r.With(requireFileCapabilities(capBrowse)).Get("/system/drives", drivesHandler(ops))
		r.With(requireFileCapabilities(capBrowse)).Get("/search", searchHandler(ops, noIndex{}))
		r.With(requireFileCapabilities(capDownload)).Get("/thumb", thumbHandler(ops, nil))
		registerFsRoutes(r, Config{TrashDir: t.TempDir()}, ops, noIndex{})
		registerTrashRoutes(r, Config{TrashDir: t.TempDir()}, ops)
		registerContentRoutes(r, Config{}, ops)
		registerTransferRoutes(r, tm, Config{}, ops)
	})

	cases := []struct {
		name, method, path, body string
		grant                    func(*store.Device)
	}{
		{"drives", http.MethodGet, "/v1/system/drives", "", func(d *store.Device) { d.CanBrowse = false }},
		{"search", http.MethodGet, "/v1/search?q=a", "", func(d *store.Device) { d.CanBrowse = false }},
		{"browse", http.MethodGet, "/v1/fs?path=" + root, "", func(d *store.Device) { d.CanBrowse = false }},
		{"metadata", http.MethodGet, "/v1/fs/meta?path=" + root + "/a.txt", "", func(d *store.Device) { d.CanBrowse = false }},
		{"archive listing", http.MethodGet, "/v1/fs/archive?path=" + root + "/a.txt", "", func(d *store.Device) { d.CanBrowse = false }},
		{"recent", http.MethodGet, "/v1/fs/recent", "", func(d *store.Device) { d.CanBrowse = false }},
		{"trash listing", http.MethodGet, "/v1/trash", "", func(d *store.Device) { d.CanBrowse = false }},
		{"download", http.MethodGet, "/v1/content?path=" + root + "/a.txt", "", func(d *store.Device) { d.CanDownload = false }},
		{"thumbnail", http.MethodGet, "/v1/thumb?path=" + root + "/a.txt", "", func(d *store.Device) { d.CanDownload = false }},
		{"checksum", http.MethodGet, "/v1/fs/checksum?path=" + root + "/a.txt", "", func(d *store.Device) { d.CanDownload = false }},
		{"batch checksums", http.MethodPost, "/v1/fs/checksums", `{"paths":["/a.txt"]}`, func(d *store.Device) { d.CanDownload = false }},
		{"edit content requires modify", http.MethodPut, "/v1/content?path=" + root + "/a.txt", "updated", func(d *store.Device) { d.CanModify = false }},
		{"upload transfer", http.MethodPost, "/v1/transfers", `{}`, func(d *store.Device) { d.CanUpload = false }},
		{"overwrite upload requires modify", http.MethodPost, "/v1/transfers", `{"path":"` + root + `/replace.bin","size":0,"sha256":"` + sha256hex(nil) + `","chunkSize":1024,"overwrite":true}`, func(d *store.Device) { d.CanModify = false }},
		{"upload chunk", http.MethodPut, "/v1/transfers/id/chunks/0", "", func(d *store.Device) { d.CanUpload = false }},
		{"upload complete", http.MethodPost, "/v1/transfers/id/complete", "", func(d *store.Device) { d.CanUpload = false }},
		{"modify", http.MethodPost, "/v1/fs/folder", `{"path":"/new"}`, func(d *store.Device) { d.CanModify = false }},
		{"create file", http.MethodPost, "/v1/fs/file", `{"path":"/new"}`, func(d *store.Device) { d.CanModify = false }},
		{"rename", http.MethodPatch, "/v1/fs/rename", `{"src":"/a","dst":"/b"}`, func(d *store.Device) { d.CanModify = false }},
		{"copy", http.MethodPost, "/v1/fs/copy", `{"sources":["/a"],"destDir":"/"}`, func(d *store.Device) { d.CanModify = false }},
		{"compress", http.MethodPost, "/v1/fs/compress", `{"sources":["/a"],"dest":"/a.zip"}`, func(d *store.Device) { d.CanModify = false }},
		{"extract", http.MethodPost, "/v1/fs/extract", `{"archive":"/a.zip","destDir":"/"}`, func(d *store.Device) { d.CanModify = false }},
		{"chmod", http.MethodPost, "/v1/fs/chmod", `{"path":"/a","mode":"644"}`, func(d *store.Device) { d.CanModify = false }},
		{"restore", http.MethodPost, "/v1/trash/restore", `{"ids":["id"]}`, func(d *store.Device) { d.CanModify = false }},
		{"move needs delete", http.MethodPost, "/v1/fs/move", `{"sources":["/a"],"destDir":"/"}`, func(d *store.Device) { d.CanModify, d.CanDelete = true, false }},
		{"delete", http.MethodDelete, "/v1/fs?path=/a", "", func(d *store.Device) { d.CanDelete = false }},
		{"empty trash", http.MethodDelete, "/v1/trash", "", func(d *store.Device) { d.CanDelete = false }},
		{"share needs grant", http.MethodPost, "/v1/share/mint", `{"path":"/a.txt"}`, func(d *store.Device) { d.CanShare = false }},
		{"share needs browse", http.MethodPost, "/v1/share/mint", `{"path":"/a.txt"}`, func(d *store.Device) { d.CanShare, d.CanBrowse = true, false }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			device = &store.Device{ID: "guest", CanBrowse: true, CanDownload: true, CanUpload: true, CanModify: true, CanDelete: true, CanShare: true}
			tc.grant(device)
			rr := httptest.NewRecorder()
			r.ServeHTTP(rr, httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body)))
			if rr.Code != http.StatusForbidden {
				t.Fatalf("want 403, got %d: %s", rr.Code, rr.Body.String())
			}
			var apiErr apiError
			if err := json.Unmarshal(rr.Body.Bytes(), &apiErr); err != nil || apiErr.Code != "CAPABILITY_DENIED" {
				t.Fatalf("want CAPABILITY_DENIED, got %+v (%v)", apiErr, err)
			}
		})
	}

	// Owner provenance bypasses device grants, but it is not an admin grant
	// manufactured by the capability flags and does not bypass global RO.
	device = &store.Device{ID: "owner", ViaLogin: true}
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/v1/fs?path="+root, nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("owner should browse despite empty device grants, got %d: %s", rr.Code, rr.Body.String())
	}
	if isAdminDevice(&store.Device{CanBrowse: true, CanDownload: true, CanUpload: true, CanModify: true, CanDelete: true, CanShare: true}) {
		t.Fatal("file capabilities must not imply owner/admin provenance")
	}
}

func TestDeviceFilePermissionPatchRevokesShares(t *testing.T) {
	db, st := newTestDeps(t)
	if err := db.CreateDevice("guest", "phone", "guest-token"); err != nil {
		t.Fatalf("create guest: %v", err)
	}
	if err := db.SetDeviceFilePermissions("guest", true, true, true, true, true, true); err != nil {
		t.Fatalf("enable grants: %v", err)
	}
	if err := db.CreateShareToken("active", "/tmp/shared.txt", "guest", time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("create share: %v", err)
	}

	r := chi.NewRouter()
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			next.ServeHTTP(w, req.WithContext(withDevice(req.Context(), &store.Device{ID: "owner", ViaLogin: true})))
		})
	})
	r.Route("/v1", func(r chi.Router) {
		registerSettingsAndDeviceRoutes(r, Config{Settings: st}, db, nil)
	})
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPatch, "/v1/devices/guest", strings.NewReader(`{"browse":false,"download":true,"upload":false,"modify":true,"delete":false,"share":false}`))
	r.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("PATCH returned %d: %s", rr.Code, rr.Body.String())
	}
	var updated map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &updated); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if updated["browse"] != false || updated["download"] != true || updated["upload"] != false || updated["modify"] != true || updated["delete"] != false || updated["share"] != false {
		t.Fatalf("PATCH response omitted permissions: %v", updated)
	}
	if token, err := db.GetShareToken("active"); err != nil || token != nil {
		t.Fatalf("disabling share must revoke current token: (%+v, %v)", token, err)
	}
}

func TestFileCapabilityDoesNotBypassGlobalReadOnly(t *testing.T) {
	root := t.TempDir()
	path := root + "/existing.txt"
	if err := os.WriteFile(path, []byte("old"), 0600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	ops := fsops.New([]string{root}, true)
	r := chi.NewRouter()
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			d := &store.Device{ID: "owner", ViaLogin: true}
			next.ServeHTTP(w, req.WithContext(withDevice(req.Context(), d)))
		})
	})
	r.Use(deviceJailMiddleware(ops))
	r.Route("/v1", func(r chi.Router) { registerContentRoutes(r, Config{}, ops) })
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, httptest.NewRequest(http.MethodPut, "/v1/content?path="+path, strings.NewReader("new")))
	if rr.Code != http.StatusForbidden {
		t.Fatalf("global read-only should remain enforced, got %d: %s", rr.Code, rr.Body.String())
	}
	var apiErr apiError
	if err := json.Unmarshal(rr.Body.Bytes(), &apiErr); err != nil || apiErr.Code != "READ_ONLY" {
		t.Fatalf("want READ_ONLY, got %+v (%v)", apiErr, err)
	}
}
