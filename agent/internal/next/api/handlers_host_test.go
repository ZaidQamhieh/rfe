package api_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ZaidQamhieh/rfe/agent/internal/next/api"
	"github.com/ZaidQamhieh/rfe/agent/internal/next/apps"
	"github.com/ZaidQamhieh/rfe/agent/internal/next/audit"
	"github.com/ZaidQamhieh/rfe/agent/internal/next/hostops"
	"github.com/ZaidQamhieh/rfe/agent/internal/next/store"
	"github.com/ZaidQamhieh/rfe/agent/internal/updates"
)

const (
	hostMAC       = "aa:bb:cc:dd:ee:ff"
	hostAPKName   = "rfe-3.4.5-67.apk"
	hostAPKBytes  = "0123456789abcdefghij"
	restartWait   = 2 * time.Second
	hostAdminName = "my phone" // the label registerAdmin gives the admin device
)

// hostRig is the stack with host services behind the real handler.
type hostRig struct {
	*rig
	log        *audit.Log
	admin, dev string
	devID      string
}

// newHostRig builds the stack; mutate may fill or swap a host service before the
// handler is built. The audit log is real.
func newHostRig(t *testing.T, mutate func(*api.HostServices)) *hostRig {
	t.Helper()
	r := newRig(t, rigOptions{})
	log := audit.New(r.db, audit.Options{})
	var host api.HostServices
	if mutate != nil {
		mutate(&host)
	}
	h, err := api.New(api.Config{
		Pairing: panicPairing{r.svc}, Devices: r.db, System: stubSystem{}, Policy: r.set, Logger: r.logs,
		Apps: apps.New(apps.Config{Lister: fakeLister{}, Launcher: r.launcher, Audit: log}),
		Misc: api.MiscServices{Audit: log}, Host: host,
	})
	if err != nil {
		t.Fatal(err)
	}
	r.h = h
	hr := &hostRig{rig: r, log: log}
	hr.admin, _ = r.registerAdmin(newPhone("admin-phone", 1))
	hr.dev, hr.devID = r.pairDevice(newPhone("dev-phone", 2))
	return hr
}

// auditEntries returns the recorded audit entries, newest first.
func (h *hostRig) auditEntries() []audit.Entry {
	h.t.Helper()
	page, err := h.log.List(0, 0)
	if err != nil {
		h.t.Fatal(err)
	}
	return page.Entries
}

// fakeRestarter counts restarts and reports each one on a channel.
type fakeRestarter struct {
	supported bool
	called    chan struct{}
}

func (f *fakeRestarter) Supported() bool { return f.supported }
func (f *fakeRestarter) Restart() error  { f.called <- struct{}{}; return nil }

// fakeSender records the packets it is asked to send.
type fakeSender struct {
	mu      sync.Mutex
	packets [][]byte
	err     error
}

func (f *fakeSender) Send(p []byte) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.packets = append(f.packets, p)
	return f.err
}

func (f *fakeSender) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.packets)
}

// stubUsers is a user directory with fixed accounts that records deletions.
type stubUsers struct {
	users   []store.User
	deleted []string
	err     error
}

func (s *stubUsers) Users() ([]store.User, error) { return s.users, nil }
func (s *stubUsers) DeleteUser(name string) error {
	if s.err != nil {
		return s.err
	}
	s.deleted = append(s.deleted, name)
	return nil
}

// stubUpdates is an update source with a fixed answer.
type stubUpdates struct {
	rel *updates.Release
	err error
}

func (s stubUpdates) Latest() (*updates.Release, error) { return s.rel, s.err }
func (s stubUpdates) Open() (*hostops.Download, error)  { return nil, s.err }

func TestHostRoutesRefuseNonAdminAndAnonymous(t *testing.T) {
	restart := &fakeRestarter{supported: true, called: make(chan struct{}, 1)}
	users := &stubUsers{}
	h := newHostRig(t, func(s *api.HostServices) {
		s.Users, s.Restart = users, restart
		s.Logs = hostops.Journal{Run: func(context.Context, string, ...string) ([]byte, error) { return nil, nil }}
	})
	routes := []struct{ method, path string }{
		{http.MethodGet, "/users"},
		{http.MethodDelete, "/users/owner"},
		{http.MethodGet, "/logs"},
		{http.MethodPost, "/agent/restart"},
	}
	for _, rt := range routes {
		h.do(rt.method, rt.path, h.dev, nil).wantError(t, http.StatusForbidden, "FORBIDDEN")
		h.do(rt.method, rt.path, "", nil).wantError(t, http.StatusUnauthorized, "UNAUTHORIZED")
	}
	if len(users.deleted) != 0 || len(restart.called) != 0 {
		t.Fatalf("a refused call reached a service: deleted %v, restarts %d", users.deleted, len(restart.called))
	}
}

func TestUsersListShowsNamesAndTimesOnly(t *testing.T) {
	h := newHostRig(t, func(s *api.HostServices) { s.Users = &stubUsers{} })
	res := h.do(http.MethodGet, "/users", h.admin, nil)
	if res.Code != http.StatusOK || strings.TrimSpace(res.Body.String()) != "[]" {
		t.Fatalf("no accounts = %d %q, want an empty array", res.Code, res.Body.String())
	}

	real := newHostRig(t, nil)
	real.rebuildWithUsers(t)
	res = real.do(http.MethodGet, "/users", real.admin, nil)
	var got []map[string]any
	res.json(t, &got)
	if res.Code != http.StatusOK || len(got) != 1 || got[0]["username"] != "owner" {
		t.Fatalf("users = %d %s", res.Code, res.Body.String())
	}
	if _, ok := got[0]["created"]; !ok || len(got[0]) != 2 || strings.Contains(res.Body.String(), "hash") {
		t.Fatalf("user row has the wrong shape: %s", res.Body.String())
	}
}

// rebuildWithUsers swaps in a handler whose user directory is the real store.
func (h *hostRig) rebuildWithUsers(t *testing.T) {
	t.Helper()
	handler, err := api.New(api.Config{
		Pairing: panicPairing{h.svc}, Devices: h.db, System: stubSystem{}, Policy: h.set, Logger: h.logs,
		Apps: apps.New(apps.Config{Lister: fakeLister{}, Launcher: h.launcher}),
		Misc: api.MiscServices{Audit: h.log}, Host: api.HostServices{Users: h.db},
	})
	if err != nil {
		t.Fatal(err)
	}
	h.h = handler
}

func TestUserDeleteAnswers(t *testing.T) {
	users := &stubUsers{}
	h := newHostRig(t, func(s *api.HostServices) { s.Users = users })
	if res := h.do(http.MethodDelete, "/users/bob", h.admin, nil); res.Code != http.StatusNoContent || res.Body.Len() != 0 {
		t.Fatalf("delete = %d %q, want 204 with no body", res.Code, res.Body.String())
	}
	if len(users.deleted) != 1 || users.deleted[0] != "bob" {
		t.Fatalf("deleted = %v", users.deleted)
	}
	users.err = store.ErrUserNotFound
	h.do(http.MethodDelete, "/users/ghost", h.admin, nil).wantError(t, http.StatusNotFound, "NOT_FOUND")
	users.err = store.ErrLastUser
	h.do(http.MethodDelete, "/users/bob", h.admin, nil).wantError(t, http.StatusBadRequest, "LAST_USER")
	users.err = errors.New("disk exploded at " + hostPath)
	res := h.do(http.MethodDelete, "/users/bob", h.admin, nil)
	res.wantError(t, http.StatusInternalServerError, "INTERNAL")
	if strings.Contains(res.Body.String(), hostPath) {
		t.Fatalf("internal detail leaked: %s", res.Body.String())
	}
}

func TestUserDeleteOverTheRealStore(t *testing.T) {
	h := newHostRig(t, nil)
	h.rebuildWithUsers(t)
	h.do(http.MethodDelete, "/users/nobody", h.admin, nil).wantError(t, http.StatusNotFound, "NOT_FOUND")
	// The owner is the only account: it cannot be removed.
	h.do(http.MethodDelete, "/users/owner", h.admin, nil).wantError(t, http.StatusBadRequest, "LAST_USER")
}

func TestLogsLimitParameter(t *testing.T) {
	var args []string
	h := newHostRig(t, func(s *api.HostServices) {
		s.Logs = hostops.Journal{Run: func(_ context.Context, _ string, a ...string) ([]byte, error) {
			args = a
			return []byte("2030-01-02T03:04:05+00:00 host rfe[1]: one\n2030-01-02T03:04:06+00:00 host rfe[1]: two\n"), nil
		}}
	})
	res := h.do(http.MethodGet, "/logs?limit=5", h.admin, nil)
	var lines []hostops.LogLine
	res.json(t, &lines)
	if res.Code != http.StatusOK || len(lines) != 2 || lines[1].Message != "two" || lines[0].TS != "2030-01-02T03:04:05+00:00" {
		t.Fatalf("logs = %d %s", res.Code, res.Body.String())
	}
	if got := strings.Join(args, " "); !strings.Contains(got, "-n 5 ") {
		t.Fatalf("journal args = %q, want the limit 5", got)
	}
	for _, q := range []string{"", "?limit=abc", "?limit=0", "?limit=999999"} {
		h.do(http.MethodGet, "/logs"+q, h.admin, nil)
		if got := strings.Join(args, " "); !strings.Contains(got, "-n 200 ") {
			t.Fatalf("limit %q: journal args = %q, want the default 200", q, got)
		}
	}
}

func TestLogsWithoutJournalIsEmptyList(t *testing.T) {
	h := newHostRig(t, func(s *api.HostServices) {
		s.Logs = hostops.Journal{Run: func(context.Context, string, ...string) ([]byte, error) { return nil, errors.New("no journald") }}
	})
	res := h.do(http.MethodGet, "/logs", h.admin, nil)
	if res.Code != http.StatusOK || strings.TrimSpace(res.Body.String()) != "[]" {
		t.Fatalf("logs = %d %q, want 200 []", res.Code, res.Body.String())
	}
}

func TestRestartCallsTheInjectedRestarterAndAudits(t *testing.T) {
	restart := &fakeRestarter{supported: true, called: make(chan struct{}, 1)}
	h := newHostRig(t, func(s *api.HostServices) { s.Restart = restart })
	res := h.do(http.MethodPost, "/agent/restart", h.admin, nil)
	if res.Code != http.StatusAccepted || strings.TrimSpace(res.Body.String()) != `{"ok":true}` {
		t.Fatalf("restart = %d %q", res.Code, res.Body.String())
	}
	select {
	case <-restart.called:
	case <-time.After(restartWait):
		t.Fatal("the restarter was not called")
	}
	es := h.auditEntries()
	if len(es) == 0 || es[0].Action != audit.ActionAgentRestart || es[0].Actor != hostAdminName {
		t.Fatalf("audit = %+v, want agent_restart by %q", es, hostAdminName)
	}
}

func TestRestartUnsupportedIs501AndNothingRuns(t *testing.T) {
	restart := &fakeRestarter{supported: false, called: make(chan struct{}, 1)}
	h := newHostRig(t, func(s *api.HostServices) { s.Restart = restart })
	h.do(http.MethodPost, "/agent/restart", h.admin, nil).wantError(t, http.StatusNotImplemented, "NOT_IMPLEMENTED")
	select {
	case <-restart.called:
		t.Fatal("an unsupported restart ran")
	case <-time.After(50 * time.Millisecond):
	}
	for _, e := range h.auditEntries() {
		if e.Action == audit.ActionAgentRestart {
			t.Fatalf("an unsupported restart was audited: %+v", e)
		}
	}
}

func TestWakeSendsPacketAndAudits(t *testing.T) {
	sender := &fakeSender{}
	h := newHostRig(t, func(s *api.HostServices) {
		s.Wake = hostops.NewWaker(sender, hostops.WakerOptions{Budget: 2})
	})
	res := h.do(http.MethodPost, "/wol", h.dev, map[string]string{"mac": strings.ToUpper(hostMAC)})
	if res.Code != http.StatusOK || strings.TrimSpace(res.Body.String()) != `{"status":"sent"}` {
		t.Fatalf("wol = %d %q", res.Code, res.Body.String())
	}
	if sender.count() != 1 || len(sender.packets[0]) != 102 {
		t.Fatalf("packets = %d", sender.count())
	}
	es := h.auditEntries()
	if len(es) == 0 || es[0].Action != audit.ActionWake || es[0].Target != hostMAC {
		t.Fatalf("audit = %+v, want wake_on_lan for %s", es, hostMAC)
	}
}

func TestWakeRefusals(t *testing.T) {
	sender := &fakeSender{}
	h := newHostRig(t, func(s *api.HostServices) {
		s.Wake = hostops.NewWaker(sender, hostops.WakerOptions{Budget: 2})
	})
	h.do(http.MethodPost, "/wol", h.dev, map[string]string{"mac": "not-a-mac"}).wantError(t, http.StatusBadRequest, "BAD_REQUEST")
	h.do(http.MethodPost, "/wol", h.dev, "{").wantError(t, http.StatusBadRequest, "BAD_REQUEST")
	h.do(http.MethodPost, "/wol", "", map[string]string{"mac": hostMAC}).wantError(t, http.StatusUnauthorized, "UNAUTHORIZED")
	if sender.count() != 0 {
		t.Fatal("a packet was sent for a refused request")
	}
	// Two requests reached the service (the bad body and the anonymous one never did) and spent the budget: the next is limited.
	h.do(http.MethodPost, "/wol", h.dev, map[string]string{"mac": "bad"})
	h.do(http.MethodPost, "/wol", h.dev, map[string]string{"mac": hostMAC}).wantError(t, http.StatusTooManyRequests, "RATE_LIMITED")

	if err := h.db.SetDeviceReadOnly(h.devID, true); err != nil {
		t.Fatal(err)
	}
	h.do(http.MethodPost, "/wol", h.dev, map[string]string{"mac": hostMAC}).wantError(t, http.StatusForbidden, "FORBIDDEN")
}

func TestWakeSenderFailureHidesDetail(t *testing.T) {
	sender := &fakeSender{err: errors.New("dial " + hostPath)}
	h := newHostRig(t, func(s *api.HostServices) { s.Wake = hostops.NewWaker(sender, hostops.WakerOptions{}) })
	res := h.do(http.MethodPost, "/wol", h.dev, map[string]string{"mac": hostMAC})
	res.wantError(t, http.StatusInternalServerError, "INTERNAL")
	if strings.Contains(res.Body.String(), hostPath) || !strings.Contains(h.logs.String(), hostPath) {
		t.Fatalf("body %q, log %q: the cause belongs in the log only", res.Body.String(), h.logs.String())
	}
}

func TestAppLatestWithStubSource(t *testing.T) {
	want := &updates.Release{VersionName: "3.4.5", VersionCode: 67, Size: 20, SHA256: "abc123"}
	h := newHostRig(t, func(s *api.HostServices) { s.Updates = stubUpdates{rel: want} })
	res := h.do(http.MethodGet, "/app/latest", h.dev, nil)
	var got map[string]any
	res.json(t, &got)
	if res.Code != http.StatusOK || got["versionName"] != "3.4.5" || got["versionCode"] != float64(67) ||
		got["size"] != float64(20) || got["sha256"] != "abc123" || len(got) != 4 {
		t.Fatalf("latest = %d %s", res.Code, res.Body.String())
	}
	h.do(http.MethodGet, "/app/latest", "", nil).wantError(t, http.StatusUnauthorized, "UNAUTHORIZED")
}

func TestAppLatestNoneIs204AndFailureIs500(t *testing.T) {
	h := newHostRig(t, func(s *api.HostServices) { s.Updates = stubUpdates{} })
	if res := h.do(http.MethodGet, "/app/latest", h.dev, nil); res.Code != http.StatusNoContent || res.Body.Len() != 0 {
		t.Fatalf("none = %d %q, want 204 with no body", res.Code, res.Body.String())
	}
	h = newHostRig(t, func(s *api.HostServices) { s.Updates = stubUpdates{err: errors.New("read " + hostPath)} })
	res := h.do(http.MethodGet, "/app/latest", h.dev, nil)
	res.wantError(t, http.StatusInternalServerError, "INTERNAL")
	if strings.Contains(res.Body.String(), hostPath) {
		t.Fatalf("internal detail leaked: %s", res.Body.String())
	}
}

// updatesDir writes the newest APK into a temp folder and returns the folder
// and the digest of the APK.
func updatesDir(t *testing.T) (string, string) {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "rfe-1.0.0-1.apk"), []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, hostAPKName), []byte(hostAPKBytes), 0o600); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte(hostAPKBytes))
	return dir, hex.EncodeToString(sum[:])
}

func TestAppLatestAndDownloadOverTheRealFolder(t *testing.T) {
	dir, digest := updatesDir(t)
	h := newHostRig(t, func(s *api.HostServices) { s.Updates = hostops.NewUpdates(dir) })

	res := h.do(http.MethodGet, "/app/latest", h.dev, nil)
	var rel updates.Release
	res.json(t, &rel)
	if res.Code != http.StatusOK || rel.VersionCode != 67 || rel.SHA256 != digest || rel.Size != int64(len(hostAPKBytes)) {
		t.Fatalf("latest = %d %s, want code 67, size %d, sha %s", res.Code, res.Body.String(), len(hostAPKBytes), digest)
	}

	dl := h.do(http.MethodGet, "/app/download", h.dev, nil)
	sum := sha256.Sum256(dl.Body.Bytes())
	if dl.Code != http.StatusOK || dl.Header().Get("Content-Type") != hostops.APKContentType ||
		dl.Header().Get("Content-Length") != "20" || hex.EncodeToString(sum[:]) != rel.SHA256 {
		t.Fatalf("download = %d type %q length %q body %q", dl.Code, dl.Header().Get("Content-Type"),
			dl.Header().Get("Content-Length"), dl.Body.String())
	}

	part := h.do(http.MethodGet, "/app/download", h.dev, nil, withHeader("Range", "bytes=5-9"))
	if part.Code != http.StatusPartialContent || part.Body.String() != hostAPKBytes[5:10] {
		t.Fatalf("range = %d %q, want 206 %q", part.Code, part.Body.String(), hostAPKBytes[5:10])
	}
	h.do(http.MethodGet, "/app/download", "", nil).wantError(t, http.StatusUnauthorized, "UNAUTHORIZED")
}

func TestAppDownloadWithoutUpdateIs404(t *testing.T) {
	h := newHostRig(t, func(s *api.HostServices) { s.Updates = hostops.NewUpdates(t.TempDir()) })
	h.do(http.MethodGet, "/app/download", h.dev, nil).wantError(t, http.StatusNotFound, "NOT_FOUND")
	if res := h.do(http.MethodGet, "/app/latest", h.dev, nil); res.Code != http.StatusNoContent {
		t.Fatalf("latest with no APK = %d", res.Code)
	}
}

func TestHostRoutesAnswer501WhenServicesAreNotWired(t *testing.T) {
	h := newHostRig(t, nil)
	for _, rt := range []struct{ method, path, token string }{
		{http.MethodGet, "/users", h.admin},
		{http.MethodDelete, "/users/x", h.admin},
		{http.MethodGet, "/logs", h.admin},
		{http.MethodPost, "/agent/restart", h.admin},
		{http.MethodPost, "/wol", h.dev},
		{http.MethodGet, "/app/latest", h.dev},
		{http.MethodGet, "/app/download", h.dev},
	} {
		body := any(nil)
		if rt.path == "/wol" {
			body = map[string]string{"mac": hostMAC}
		}
		h.do(rt.method, rt.path, rt.token, body).wantError(t, http.StatusNotImplemented, "NOT_IMPLEMENTED")
	}
}
