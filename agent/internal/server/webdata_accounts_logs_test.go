package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
)

func requestWithUsername(username string) *http.Request {
	req := httptest.NewRequest(http.MethodDelete, "/v1/users/"+username, nil)
	route := chi.NewRouteContext()
	route.URLParams.Add("username", username)
	return req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, route))
}

func TestUserWebHandlersOmitHashesAndProtectLastAccount(t *testing.T) {
	db, _ := newTestDeps(t)
	if err := db.CreateUser("owner", "secret-hash"); err != nil {
		t.Fatal(err)
	}
	if err := db.CreateUser("second", "another-hash"); err != nil {
		t.Fatal(err)
	}
	deviceID, err := db.LoginDevice("owner-client", "browser", "owner-token", "", "owner", "secret-hash")
	if err != nil {
		t.Fatal(err)
	}
	listRR := httptest.NewRecorder()
	listUsersHandler(db)(listRR, httptest.NewRequest(http.MethodGet, "/v1/users", nil))
	if listRR.Code != http.StatusOK {
		t.Fatalf("list users status = %d: %s", listRR.Code, listRR.Body.String())
	}
	var users []map[string]any
	if err := json.Unmarshal(listRR.Body.Bytes(), &users); err != nil {
		t.Fatal(err)
	}
	if len(users) != 2 {
		t.Fatalf("listed %d users, want 2", len(users))
	}
	for _, user := range users {
		if user["passwordHash"] != nil || user["password_hash"] != nil {
			t.Fatalf("user endpoint exposed password data: %+v", user)
		}
		if user["username"] == "" || user["created"] == nil {
			t.Fatalf("user response missing safe fields: %+v", user)
		}
	}

	unknownRR := httptest.NewRecorder()
	deleteUserHandler(db)(unknownRR, requestWithUsername("missing"))
	if unknownRR.Code != http.StatusNotFound {
		t.Fatalf("delete missing user = %d, want 404", unknownRR.Code)
	}
	deleteRR := httptest.NewRecorder()
	deleteUserHandler(db)(deleteRR, requestWithUsername("owner"))
	if deleteRR.Code != http.StatusNoContent {
		t.Fatalf("delete existing user = %d: %s", deleteRR.Code, deleteRR.Body.String())
	}
	device, err := db.GetDeviceByID(deviceID)
	if err != nil || device == nil || !device.Revoked {
		t.Fatalf("deleted user's login device = (%+v,%v), want revoked", device, err)
	}
	lastRR := httptest.NewRecorder()
	deleteUserHandler(db)(lastRR, requestWithUsername("second"))
	if lastRR.Code != http.StatusBadRequest || !strings.Contains(lastRR.Body.String(), "LAST_USER") {
		t.Fatalf("delete last user = %d: %s", lastRR.Code, lastRR.Body.String())
	}
}

func TestJournalLogsParsingAndUnavailableFallback(t *testing.T) {
	bin := filepath.Join(t.TempDir(), "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	journal := filepath.Join(bin, "journalctl")
	lines := make([]string, 205)
	for i := range lines {
		lines[i] = "2026-09-28T10:00:00+00:00 host rfe[123]: 2026/09/28 10:00:00 line"
	}
	script := "#!/bin/sh\nprintf '%s\\n' " + "'" + strings.Join(lines, "' '") + "'\n"
	if err := os.WriteFile(journal, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)
	rr := httptest.NewRecorder()
	listLogsHandler()(rr, httptest.NewRequest(http.MethodGet, "/v1/logs", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("logs status = %d: %s", rr.Code, rr.Body.String())
	}
	var logs []map[string]string
	if err := json.Unmarshal(rr.Body.Bytes(), &logs); err != nil {
		t.Fatal(err)
	}
	if len(logs) != maxLogLines || logs[0]["ts"] != "2026-09-28T10:00:00+00:00" || logs[0]["message"] != "line" {
		t.Fatalf("trimmed journal response = %d rows, first=%+v", len(logs), logs[0])
	}
	for _, tc := range []struct {
		line, wantTS, wantMsg string
	}{
		{"bare", "", "bare"},
		{"timestamp message", "timestamp", "message"},
		{"2026-09-28T10:00:00Z host rfe[9]: 2026/09/28 10:00:01 ready", "2026-09-28T10:00:00Z", "ready"},
	} {
		gotTS, gotMsg := splitLogLine(tc.line)
		if gotTS != tc.wantTS || gotMsg != tc.wantMsg {
			t.Errorf("splitLogLine(%q) = (%q,%q), want (%q,%q)", tc.line, gotTS, gotMsg, tc.wantTS, tc.wantMsg)
		}
	}
	if err := os.WriteFile(journal, []byte("#!/bin/sh\nexit 1\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	rr = httptest.NewRecorder()
	listLogsHandler()(rr, httptest.NewRequest(http.MethodGet, "/v1/logs", nil))
	if rr.Code != http.StatusOK || strings.TrimSpace(rr.Body.String()) != "[]" {
		t.Fatalf("unavailable journal response = %d: %s", rr.Code, rr.Body.String())
	}
}
