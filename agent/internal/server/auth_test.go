package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ZaidQamhieh/rfe/agent/internal/fsops"
	"github.com/ZaidQamhieh/rfe/agent/internal/settings"
	"github.com/ZaidQamhieh/rfe/agent/internal/store"
)

func newAuthTestDeps(t *testing.T) (*store.DB, *settings.Store, string) {
	t.Helper()
	db, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	st, err := settings.Load(db, false, nil, "test-pc")
	if err != nil {
		t.Fatalf("settings: %v", err)
	}
	token := "test-token-abc123"
	if err := db.CreateDevice("dev1", "phone1", token); err != nil {
		t.Fatalf("create device: %v", err)
	}
	return db, st, token
}

func okHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
}

func TestAuthMiddleware_MissingHeader(t *testing.T) {
	db, _, _ := newAuthTestDeps(t)
	handler := authMiddleware(db)(okHandler())

	req := httptest.NewRequest(http.MethodGet, "/v1/fs", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rr.Code)
	}
}

func TestAuthMiddleware_InvalidFormat(t *testing.T) {
	db, _, _ := newAuthTestDeps(t)
	handler := authMiddleware(db)(okHandler())

	req := httptest.NewRequest(http.MethodGet, "/v1/fs", nil)
	req.Header.Set("Authorization", "Basic abc123")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rr.Code)
	}
}

func TestAuthMiddleware_InvalidToken(t *testing.T) {
	db, _, _ := newAuthTestDeps(t)
	handler := authMiddleware(db)(okHandler())

	req := httptest.NewRequest(http.MethodGet, "/v1/fs", nil)
	req.Header.Set("Authorization", "Bearer wrong-token")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rr.Code)
	}
}

func TestAuthMiddleware_ValidToken(t *testing.T) {
	db, _, token := newAuthTestDeps(t)
	handler := authMiddleware(db)(okHandler())

	req := httptest.NewRequest(http.MethodGet, "/v1/fs", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestAuthMiddleware_RevokedToken(t *testing.T) {
	db, _, token := newAuthTestDeps(t)
	if err := db.RevokeDevice("dev1"); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	handler := authMiddleware(db)(okHandler())

	req := httptest.NewRequest(http.MethodGet, "/v1/fs", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rr.Code)
	}
}

func TestAuthMiddleware_SetsDeviceContext(t *testing.T) {
	db, _, token := newAuthTestDeps(t)
	var gotDevice *store.Device
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotDevice = r.Context().Value(deviceCtxKey).(*store.Device)
		w.WriteHeader(http.StatusOK)
	})
	handler := authMiddleware(db)(inner)

	req := httptest.NewRequest(http.MethodGet, "/v1/fs", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	if gotDevice == nil || gotDevice.ID != "dev1" {
		t.Fatalf("expected device dev1 in context, got %+v", gotDevice)
	}
}

func TestAuthMiddleware_WebSessionCookieRequiresSameOriginMarker(t *testing.T) {
	db, _, token := newAuthTestDeps(t)
	handler := authMiddleware(db)(okHandler())

	request := httptest.NewRequest(http.MethodGet, "https://agent.example:8765/v1/status", nil)
	request.AddCookie(&http.Cookie{Name: webSessionCookie, Value: token})
	request.Header.Set(webSessionHeader, "1")
	request.Header.Set("Sec-Fetch-Site", "same-origin")
	request.Header.Set("Origin", "https://agent.example:8765")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("same-origin web session returned %d: %s", response.Code, response.Body.String())
	}

	request = httptest.NewRequest(http.MethodGet, "https://agent.example:8765/v1/status", nil)
	request.AddCookie(&http.Cookie{Name: webSessionCookie, Value: token})
	request.Header.Set("Sec-Fetch-Site", "same-origin")
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("cookie without marker returned %d, want 403", response.Code)
	}

	request = httptest.NewRequest(http.MethodGet, "https://agent.example:8765/v1/status", nil)
	request.AddCookie(&http.Cookie{Name: webSessionCookie, Value: token})
	request.Header.Set(webSessionHeader, "1")
	request.Header.Set("Sec-Fetch-Site", "same-site")
	request.Header.Set("Origin", "https://other.example")
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("cross-origin web session returned %d, want 403", response.Code)
	}
}

func TestWritePairResponseKeepsBrowserTokenHttpOnly(t *testing.T) {
	request := httptest.NewRequest(http.MethodPost, "https://agent.example:8765/v1/login", nil)
	request.Header.Set(webSessionHeader, "1")
	response := httptest.NewRecorder()
	writePairResponse(response, request, pairResponse{DeviceToken: "secret-token", DeviceID: "device-1"})

	if strings.Contains(response.Body.String(), "secret-token") || strings.Contains(response.Body.String(), `"deviceToken"`) {
		t.Fatalf("browser response exposed the bearer token: %s", response.Body.String())
	}
	cookie := response.Result().Cookies()
	if len(cookie) != 1 {
		t.Fatalf("got %d cookies, want one", len(cookie))
	}
	if c := cookie[0]; c.Name != webSessionCookie || c.Value != "secret-token" || !c.HttpOnly || !c.Secure || c.SameSite != http.SameSiteStrictMode || c.Path != "/v1" {
		t.Fatalf("unexpected browser session cookie: %+v", c)
	}
	if response.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("auth response Cache-Control = %q, want no-store", response.Header().Get("Cache-Control"))
	}

	request = httptest.NewRequest(http.MethodPost, "https://agent.example:8765/v1/login", nil)
	response = httptest.NewRecorder()
	writePairResponse(response, request, pairResponse{DeviceToken: "native-token"})
	if !strings.Contains(response.Body.String(), `"deviceToken":"native-token"`) {
		t.Fatalf("native response did not preserve bearer-token contract: %s", response.Body.String())
	}
}

func TestLogoutHandlerClearsOnlyBrowserCookie(t *testing.T) {
	response := httptest.NewRecorder()
	db, _ := newTestDeps(t)
	logoutHandler(db)(response, httptest.NewRequest(http.MethodPost, "/v1/auth/logout", nil))
	if response.Code != http.StatusNoContent {
		t.Fatalf("logout status = %d, want 204", response.Code)
	}
	cookies := response.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("logout emitted %d cookies, want one", len(cookies))
	}
	if c := cookies[0]; c.Name != webSessionCookie || c.Value != "" || c.MaxAge >= 0 || !c.HttpOnly || !c.Secure || c.SameSite != http.SameSiteStrictMode || c.Path != "/v1" {
		t.Fatalf("unexpected logout cookie: %+v", c)
	}
}

func TestDeviceJailMiddleware_NoJail(t *testing.T) {
	root := t.TempDir()
	ops := fsops.New([]string{root}, false)
	device := &store.Device{ID: "d1"}

	var gotOps *fsops.Ops
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotOps = opsFromContext(r.Context(), ops)
		w.WriteHeader(http.StatusOK)
	})
	handler := deviceJailMiddleware(ops)(inner)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	ctx := context.WithValue(req.Context(), deviceCtxKey, device)
	req = req.WithContext(ctx)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if gotOps == nil {
		t.Fatal("expected ops in context")
	}
}

func TestDeviceJailMiddleware_ReadOnly(t *testing.T) {
	root := t.TempDir()
	ops := fsops.New([]string{root}, false)
	device := &store.Device{ID: "d1", ReadOnly: true}

	var gotOps *fsops.Ops
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotOps = opsFromContext(r.Context(), ops)
		w.WriteHeader(http.StatusOK)
	})
	handler := deviceJailMiddleware(ops)(inner)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	ctx := context.WithValue(req.Context(), deviceCtxKey, device)
	req = req.WithContext(ctx)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if gotOps == nil {
		t.Fatal("expected ops in context")
	}
	// Verify read-only by attempting a write operation.
	_, err := gotOps.CreateFolder(root + "/test-dir")
	if err == nil {
		t.Fatal("expected error from read-only ops")
	}
}

func TestLogout_EndsALoginSessionAndRevokesAPairedBrowser(t *testing.T) {
	db, _ := newTestDeps(t)
	if err := db.CreateUser("owner", "pw-hash"); err != nil {
		t.Fatal(err)
	}
	loginID, err := db.LoginDevice("browser-login", "Browser", "login-token", "", "owner", "pw-hash")
	if err != nil {
		t.Fatal(err)
	}
	pairedID, err := db.UpsertDevice("browser-paired", "Browser", "paired-token", "", false)
	if err != nil {
		t.Fatal(err)
	}
	for _, token := range []string{"login-token", "paired-token"} {
		req := httptest.NewRequest(http.MethodPost, "https://example.test/v1/auth/logout", nil)
		req.Header.Set(webSessionHeader, "1")
		req.AddCookie(&http.Cookie{Name: webSessionCookie, Value: token})
		logoutHandler(db)(httptest.NewRecorder(), req)
	}
	if d, _ := db.GetDeviceByID(loginID); d != nil {
		t.Fatal("a password-login session must be deleted on logout")
	}
	if d, _ := db.GetDeviceByID(pairedID); d == nil || !d.Revoked {
		t.Fatal("a paired browser must be revoked on logout, so a copied cookie value is dead")
	}
}
