package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/ZaidQamhieh/rfe/agent/internal/pairing"
	"github.com/ZaidQamhieh/rfe/agent/internal/store"
	"github.com/ZaidQamhieh/rfe/agent/internal/transfer"
)

type routeTier int

const (
	// tierPublic needs no token.
	tierPublic routeTier = iota
	// tierDevice needs a valid device token; capability and read-only rules are
	// covered by TestRouteMatrix_ReadOnlyBlocksEveryMutation and the handler tests.
	tierDevice
	// tierWriter needs a valid token and refuses read-only (guest) devices.
	tierWriter
	// tierAdmin needs an admin (via_login) device: an ordinary paired device and a
	// read-only device must both get 403.
	tierAdmin
	// tierSelfOrAdmin lets a device act on itself and only an admin act on others.
	tierSelfOrAdmin
	// tierWeb is the bundled web companion's static catch-all.
	tierWeb
)

// routeTiers declares who may call every route the server registers. A route
// added without an entry here fails TestRouteTierMatrix, so each new route has
// to state its tier on purpose. Keys are "METHOD /v1/path" as chi reports them.
var routeTiers = map[string]routeTier{
	"GET /v1/health":                      tierPublic,
	"POST /v1/auth/challenge":             tierPublic,
	"POST /v1/auth/logout":                tierPublic,
	"POST /v1/pair":                       tierPublic,
	"POST /v1/pair/request":               tierPublic,
	"GET /v1/pair/request/{id}":           tierPublic,
	"POST /v1/pair/request/{id}/reveal":   tierPublic,
	"POST /v1/register":                   tierPublic,
	"POST /v1/login":                      tierPublic,
	"GET /v1/share/{token}":               tierPublic,
	"GET /v1/status":                      tierDevice,
	"GET /v1/transfers/list":              tierDevice,
	"DELETE /v1/transfers/{id}":           tierDevice,
	"GET /v1/transfers/{id}":              tierDevice,
	"POST /v1/transfers":                  tierDevice,
	"POST /v1/transfers/{id}/complete":    tierDevice,
	"PUT /v1/transfers/{id}/chunks/{n}":   tierDevice,
	"GET /v1/apps":                        tierDevice,
	"POST /v1/apps/{id}/launch":           tierDevice,
	"GET /v1/settings":                    tierDevice, // reduced view for non-admin devices
	"GET /v1/system/drives":               tierDevice,
	"GET /v1/search":                      tierDevice,
	"GET /v1/thumb":                       tierDevice,
	"GET /v1/app/latest":                  tierDevice,
	"GET /v1/app/download":                tierDevice,
	"GET /v1/fs":                          tierDevice,
	"GET /v1/fs/meta":                     tierDevice,
	"GET /v1/fs/archive":                  tierDevice,
	"GET /v1/fs/checksum":                 tierDevice,
	"POST /v1/fs/checksums":               tierDevice,
	"GET /v1/fs/recent":                   tierDevice,
	"GET /v1/content":                     tierDevice,
	"GET /v1/trash":                       tierDevice,
	"POST /v1/share/mint":                 tierDevice,
	"GET /v1/share":                       tierDevice, // scoped to the caller inside the handler
	"DELETE /v1/share/{tokenHash}":        tierDevice, // owner check inside the handler
	"DELETE /v1/fs":                       tierDevice,
	"POST /v1/fs/folder":                  tierDevice,
	"POST /v1/fs/file":                    tierDevice,
	"PATCH /v1/fs/rename":                 tierDevice,
	"POST /v1/fs/copy":                    tierDevice,
	"POST /v1/fs/move":                    tierDevice,
	"POST /v1/fs/compress":                tierDevice,
	"POST /v1/fs/extract":                 tierDevice,
	"POST /v1/fs/chmod":                   tierDevice,
	"PUT /v1/content":                     tierDevice,
	"DELETE /v1/trash":                    tierDevice,
	"POST /v1/trash/restore":              tierDevice,
	"POST /v1/wol":                        tierWriter,
	"PATCH /v1/settings":                  tierAdmin,
	"GET /v1/settings/bandwidth":          tierAdmin,
	"PUT /v1/settings/bandwidth":          tierAdmin,
	"GET /v1/devices":                     tierDevice, // an ordinary device sees only its own row (see the scoped test below)
	"PATCH /v1/devices/{id}":              tierAdmin,
	"DELETE /v1/devices/{id}":             tierSelfOrAdmin,
	"POST /v1/pairing/generate":           tierAdmin,
	"GET /v1/pair/requests":               tierAdmin,
	"POST /v1/pair/requests/{id}/approve": tierAdmin,
	"POST /v1/pair/requests/{id}/reject":  tierAdmin,
	"GET /v1/metrics":                     tierAdmin,
	"GET /v1/users":                       tierAdmin,
	"DELETE /v1/users/{username}":         tierAdmin,
	"GET /v1/logs":                        tierAdmin,
	"GET /v1/audit":                       tierAdmin,
	"POST /v1/agent/restart":              tierAdmin,
}

// adminSafeToCall lists admin routes that are harmless to call with a real admin
// token in a test. The others (restart, user and device deletion) are only
// ever called with a token that must be refused.
var adminSafeToCall = map[string]bool{
	"GET /v1/settings/bandwidth": true,
	"GET /v1/pair/requests":      true,
	"GET /v1/metrics":            true,
	"GET /v1/users":              true,
	"GET /v1/logs":               true,
	"GET /v1/audit":              true,
}

func fillRoutePath(route string) string {
	var out strings.Builder
	for _, seg := range strings.Split(route, "/") {
		if strings.HasPrefix(seg, "{") && strings.HasSuffix(seg, "}") {
			seg = "x"
		}
		out.WriteString(seg + "/")
	}
	return strings.TrimSuffix(out.String(), "/")
}

// newTierRouter builds the real router with the same wiring as production.
func newTierRouter(t *testing.T) (http.Handler, *store.DB) {
	t.Helper()
	db, st := newTestDeps(t)
	tm, err := transfer.New(db, t.TempDir())
	if err != nil {
		t.Fatalf("transfer.New: %v", err)
	}
	h, err := New(Config{
		Name:          "tier-matrix",
		Settings:      st,
		TrashDir:      t.TempDir(),
		ThumbCacheDir: t.TempDir(),
		UpdatesDir:    t.TempDir(),
		StartTime:     time.Now(),
		DataDir:       t.TempDir(),
	}, db, pairing.New(db, "127.0.0.1", "", "fp"), tm)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return h, db
}

// TestRouteTierMatrix walks the real router built by New and checks the access
// tier of every route: unauthenticated callers get 401 where a token is
// required, and global-policy routes refuse ordinary and read-only devices.
func TestRouteTierMatrix(t *testing.T) {
	h, db := newTierRouter(t)

	mint := func(clientID, token string, viaLogin bool) string {
		t.Helper()
		id, err := db.UpsertDevice(clientID, clientID, token, "", viaLogin)
		if err != nil {
			t.Fatalf("UpsertDevice(%s): %v", clientID, err)
		}
		return id
	}
	// An admin token only authenticates when bound to an existing account.
	if _, err := db.RegisterAccount("admin-dev", "admin-dev", "tok-admin", "", "owner", "hash"); err != nil {
		t.Fatalf("RegisterAccount: %v", err)
	}
	mint("plain-dev", "tok-plain", false)
	roID := mint("ro-dev", "tok-ro", false)
	if err := db.SetDeviceReadOnly(roID, true); err != nil {
		t.Fatalf("SetDeviceReadOnly: %v", err)
	}

	type route struct{ method, pattern string }
	var routes []route
	err := chi.Walk(h.(chi.Routes), func(method, pattern string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		if pattern == "/*" {
			return nil // web companion catch-all, every method
		}
		routes = append(routes, route{method, pattern})
		return nil
	})
	if err != nil {
		t.Fatalf("chi.Walk: %v", err)
	}

	seen := map[string]bool{}
	var untiered []string
	for _, rt := range routes {
		key := rt.method + " " + rt.pattern
		seen[key] = true
		if _, ok := routeTiers[key]; !ok {
			untiered = append(untiered, key)
		}
	}
	var stale []string
	for key := range routeTiers {
		if !seen[key] {
			stale = append(stale, key)
		}
	}
	sort.Strings(untiered)
	sort.Strings(stale)
	if len(untiered) > 0 {
		t.Fatalf("routes with no tier in routeTiers (declare who may call them): %v", untiered)
	}
	if len(stale) > 0 {
		t.Fatalf("routeTiers entries for routes that no longer exist: %v", stale)
	}

	call := func(method, pattern, token string) int {
		req := httptest.NewRequest(method, fillRoutePath(pattern), strings.NewReader(`{}`))
		req.Header.Set("Content-Type", "application/json")
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, req)
		return rr.Code
	}

	for _, rt := range routes {
		key := rt.method + " " + rt.pattern
		tier := routeTiers[key]
		t.Run(key, func(t *testing.T) {
			if tier != tierPublic {
				if code := call(rt.method, rt.pattern, ""); code != http.StatusUnauthorized {
					t.Errorf("no token: want 401, got %d", code)
				}
				if code := call(rt.method, rt.pattern, "not-a-token"); code != http.StatusUnauthorized {
					t.Errorf("bad token: want 401, got %d", code)
				}
			}
			switch tier {
			case tierWriter:
				if code := call(rt.method, rt.pattern, "tok-ro"); code != http.StatusForbidden {
					t.Errorf("read-only device: want 403, got %d", code)
				}
			case tierAdmin:
				for name, tok := range map[string]string{"plain device": "tok-plain", "read-only device": "tok-ro"} {
					if code := call(rt.method, rt.pattern, tok); code != http.StatusForbidden {
						t.Errorf("%s: want 403, got %d", name, code)
					}
				}
				if adminSafeToCall[key] {
					if code := call(rt.method, rt.pattern, "tok-admin"); code == http.StatusUnauthorized || code == http.StatusForbidden {
						t.Errorf("admin device must be allowed, got %d", code)
					}
				}
			case tierSelfOrAdmin:
				// The placeholder id is not the caller's own id.
				for name, tok := range map[string]string{"plain device": "tok-plain", "read-only device": "tok-ro"} {
					if code := call(rt.method, rt.pattern, tok); code != http.StatusForbidden {
						t.Errorf("%s acting on another device: want 403, got %d", name, code)
					}
				}
			}
		})
	}
}

// TestRouteTierMatrix_DeviceListIsScoped pins the one non-admin route that
// reads device state: an ordinary device may list only itself.
func TestRouteTierMatrix_DeviceListIsScoped(t *testing.T) {
	db, st := newTestDeps(t)
	tm, err := transfer.New(db, t.TempDir())
	if err != nil {
		t.Fatalf("transfer.New: %v", err)
	}
	h, err := New(Config{Name: "scope", Settings: st, TrashDir: t.TempDir(), ThumbCacheDir: t.TempDir(),
		UpdatesDir: t.TempDir(), StartTime: time.Now(), DataDir: t.TempDir()}, db, pairing.New(db, "127.0.0.1", "", "fp"), tm)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := db.UpsertDevice("one", "one", "tok-one", "", false); err != nil {
		t.Fatal(err)
	}
	if _, err := db.UpsertDevice("two", "two", "tok-two", "", false); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/v1/devices", nil)
	req.Header.Set("Authorization", "Bearer tok-one")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", rr.Code, rr.Body.String())
	}
	var rows []map[string]any
	if err := json.NewDecoder(rr.Body).Decode(&rows); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("an ordinary device must see only itself, got %d rows: %v", len(rows), rows)
	}
}
