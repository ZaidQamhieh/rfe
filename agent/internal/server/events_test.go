package server

import (
	"bufio"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/ZaidQamhieh/rfe/agent/internal/events"
	"github.com/ZaidQamhieh/rfe/agent/internal/pairing"
	"github.com/ZaidQamhieh/rfe/agent/internal/store"
	"github.com/ZaidQamhieh/rfe/agent/internal/transfer"
)

const streamWait = 5 * time.Second

// eventsRig is a live test server with an event hub and three callers: an
// admin (login) device, an ordinary paired device with browse, and one without.
type eventsRig struct {
	srv *httptest.Server
	hub *events.Hub
	db  *store.DB
	// plainID is the ordinary device's id.
	plainID string
}

func newEventsRig(t *testing.T) *eventsRig {
	t.Helper()
	db, st := newTestDeps(t)
	tm, err := transfer.New(db, t.TempDir())
	if err != nil {
		t.Fatalf("transfer.New: %v", err)
	}
	hub := events.NewHub()
	h, err := New(Config{
		Name:          "events",
		Settings:      st,
		TrashDir:      t.TempDir(),
		ThumbCacheDir: t.TempDir(),
		UpdatesDir:    t.TempDir(),
		StartTime:     time.Now(),
		DataDir:       t.TempDir(),
		Events:        hub,
	}, db, pairing.New(db, "127.0.0.1", "", "fp"), tm)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := db.RegisterAccount("admin-dev", "admin-dev", "tok-admin", "", "owner", "hash"); err != nil {
		t.Fatalf("RegisterAccount: %v", err)
	}
	plainID, err := db.UpsertDevice("plain-dev", "plain-dev", "tok-plain", "", false)
	if err != nil {
		t.Fatalf("UpsertDevice: %v", err)
	}
	if err := db.SetDeviceFilePermissions(plainID, true, false, false, false, false, false); err != nil {
		t.Fatalf("SetDeviceFilePermissions: %v", err)
	}
	blindID, err := db.UpsertDevice("blind-dev", "blind-dev", "tok-blind", "", false)
	if err != nil {
		t.Fatalf("UpsertDevice: %v", err)
	}
	if err := db.SetDeviceFilePermissions(blindID, false, true, true, true, true, true); err != nil {
		t.Fatalf("SetDeviceFilePermissions: %v", err)
	}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return &eventsRig{srv: srv, hub: hub, db: db, plainID: plainID}
}

// stream is an open /v1/events connection; lines carries its text lines and is
// closed when the server ends the stream.
type stream struct {
	resp  *http.Response
	lines chan string
}

func (rg *eventsRig) open(t *testing.T, token string) *stream {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, rg.srv.URL+"/v1/events", nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET /v1/events: %v", err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	s := &stream{resp: resp, lines: make(chan string, 256)}
	if resp.StatusCode == http.StatusOK {
		go func() {
			defer close(s.lines)
			sc := bufio.NewScanner(resp.Body)
			for sc.Scan() {
				s.lines <- sc.Text()
			}
		}()
	}
	return s
}

// line returns the next line, failing the test when none arrives in time or the
// stream has ended.
func (s *stream) line(t *testing.T) string {
	t.Helper()
	select {
	case l, ok := <-s.lines:
		if !ok {
			t.Fatal("stream ended")
		}
		return l
	case <-time.After(streamWait):
		t.Fatal("timed out waiting for a stream line")
	}
	return ""
}

// nextEvent returns the name of the next "event:" line, skipping the rest.
func (s *stream) nextEvent(t *testing.T) string {
	t.Helper()
	for {
		if l := s.line(t); strings.HasPrefix(l, "event: ") {
			return strings.TrimPrefix(l, "event: ")
		}
	}
}

func (rg *eventsRig) call(t *testing.T, method, path, token, body string) *http.Response {
	t.Helper()
	req, _ := http.NewRequest(method, rg.srv.URL+path, strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	resp.Body.Close()
	return resp
}

func TestEventsRequiresAuthAndBrowse(t *testing.T) {
	rg := newEventsRig(t)
	if got := rg.open(t, "").resp.StatusCode; got != http.StatusUnauthorized {
		t.Fatalf("no token = %d, want 401", got)
	}
	if got := rg.open(t, "tok-blind").resp.StatusCode; got != http.StatusForbidden {
		t.Fatalf("device without browse = %d, want 403", got)
	}
	s := rg.open(t, "tok-plain")
	if s.resp.StatusCode != http.StatusOK || !strings.HasPrefix(s.resp.Header.Get("Content-Type"), "text/event-stream") {
		t.Fatalf("plain = %d %q", s.resp.StatusCode, s.resp.Header.Get("Content-Type"))
	}
}

func TestEventsHelloThenAdminOnlyKindsAreScoped(t *testing.T) {
	rg := newEventsRig(t)
	admin, plain := rg.open(t, "tok-admin"), rg.open(t, "tok-plain")
	for name, s := range map[string]*stream{"admin": admin, "plain": plain} {
		if got := s.nextEvent(t); got != "hello" {
			t.Fatalf("%s first event = %q, want hello", name, got)
		}
	}
	// The audit entry is recorded first; only the admin may hear about it.
	if err := rg.db.AppendAudit("test.action", "tester", "", ""); err != nil {
		t.Fatalf("AppendAudit: %v", err)
	}
	if got := admin.nextEvent(t); got != "audit" {
		t.Fatalf("admin event = %q, want audit", got)
	}
	// A folder created over HTTP is a files hint for everyone allowed to browse.
	resp := rg.call(t, http.MethodPost, "/v1/fs/folder", "tok-admin", `{"path":"`+t.TempDir()+`/made"}`)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create folder = %d", resp.StatusCode)
	}
	if got := admin.nextEvent(t); got != "files" {
		t.Fatalf("admin event = %q, want files", got)
	}
	if got := plain.nextEvent(t); got != "files" {
		t.Fatalf("plain device's first event after hello = %q, want files (no audit)", got)
	}
}

func TestEventsPublishedFromMutations(t *testing.T) {
	rg := newEventsRig(t)
	admin := rg.open(t, "tok-admin")
	admin.nextEvent(t) // hello
	resp := rg.call(t, http.MethodPatch, "/v1/settings", "tok-admin", `{"agentName":"renamed"}`)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("PATCH settings = %d", resp.StatusCode)
	}
	if got := admin.nextEvent(t); got != "settings" {
		t.Fatalf("event = %q, want settings", got)
	}
	resp = rg.call(t, http.MethodDelete, "/v1/devices/"+rg.plainID, "tok-admin", "")
	if resp.StatusCode >= 300 {
		t.Fatalf("revoke = %d", resp.StatusCode)
	}
	// The revoke also writes an audit entry, which is announced first.
	got := admin.nextEvent(t)
	if got == "audit" {
		got = admin.nextEvent(t)
	}
	if got != "devices" {
		t.Fatalf("event = %q, want devices", got)
	}
}

func TestEventsFailedMutationPublishesNothing(t *testing.T) {
	rg := newEventsRig(t)
	admin := rg.open(t, "tok-admin")
	admin.nextEvent(t)
	resp := rg.call(t, http.MethodPost, "/v1/fs/folder", "tok-admin", `{}`)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("bad folder request = %d, want 400", resp.StatusCode)
	}
	rg.hub.Publish(events.KindSettings) // marker: arrives only if no files event was queued before it
	if got := admin.nextEvent(t); got != "settings" {
		t.Fatalf("event = %q, want only the marker", got)
	}
}

func TestEventsHeartbeatComment(t *testing.T) {
	old := heartbeatEvery
	heartbeatEvery = 20 * time.Millisecond
	t.Cleanup(func() { heartbeatEvery = old })
	rg := newEventsRig(t)
	s := rg.open(t, "tok-admin")
	for {
		if l := s.line(t); strings.HasPrefix(l, ":") {
			return
		}
	}
}

func TestEventsSubscriberCapAnswers503(t *testing.T) {
	rg := newEventsRig(t)
	for i := 0; i < events.MaxSubscribers; i++ {
		if rg.hub.Subscribe(false) == nil {
			t.Fatalf("subscriber %d refused", i)
		}
	}
	s := rg.open(t, "tok-admin")
	if s.resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("over the cap = %d, want 503", s.resp.StatusCode)
	}
}

func TestEventsHubCloseEndsStreams(t *testing.T) {
	rg := newEventsRig(t)
	s := rg.open(t, "tok-admin")
	s.nextEvent(t)
	rg.hub.Close()
	deadline := time.After(streamWait)
	for {
		select {
		case _, ok := <-s.lines:
			if !ok {
				return
			}
		case <-deadline:
			t.Fatal("stream still open after Close")
		}
	}
}

// Every route the publisher table names must exist, or a rename would silently
// stop its events.
func TestEventPublishTableNamesRealRoutes(t *testing.T) {
	h, _ := newTierRouter(t)
	routes := map[string]bool{}
	_ = chi.Walk(h.(chi.Routes), func(method, pattern string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		routes[method+" "+pattern] = true
		return nil
	})
	if len(eventPublishers) == 0 {
		t.Fatal("empty publisher table")
	}
	for key := range eventPublishers {
		if !routes[key] {
			t.Errorf("publisher table names %q, which is not a registered route", key)
		}
	}
}

func TestEventsStreamEndsWhenDeviceIsRevoked(t *testing.T) {
	old := heartbeatEvery
	heartbeatEvery = 20 * time.Millisecond
	t.Cleanup(func() { heartbeatEvery = old })
	rg := newEventsRig(t)
	s := rg.open(t, "tok-plain")
	if s.resp.StatusCode != http.StatusOK {
		t.Fatalf("open = %d", s.resp.StatusCode)
	}
	if err := rg.db.RevokeDevice(rg.plainID); err != nil {
		t.Fatalf("RevokeDevice: %v", err)
	}
	deadline := time.After(streamWait)
	for {
		select {
		case _, ok := <-s.lines:
			if !ok {
				return // the server ended the stream
			}
		case <-deadline:
			t.Fatal("a revoked device kept its event stream")
		}
	}
}

func TestEventsOneDeviceCannotTakeEverySlot(t *testing.T) {
	rg := newEventsRig(t)
	for i := 0; i < events.MaxPerOwner; i++ {
		if rg.hub.SubscribeAs(false, "dev-a") == nil {
			t.Fatalf("stream %d of one device refused", i)
		}
	}
	if rg.hub.SubscribeAs(false, "dev-a") != nil {
		t.Fatal("one device got more than MaxPerOwner streams")
	}
	if rg.hub.SubscribeAs(false, "dev-b") == nil {
		t.Fatal("another device was refused because dev-a is full")
	}
}
