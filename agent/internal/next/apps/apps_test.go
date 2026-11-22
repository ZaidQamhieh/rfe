package apps

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ZaidQamhieh/rfe/agent/internal/next/audit"
	"github.com/ZaidQamhieh/rfe/agent/internal/next/authz"
)

// hostRef is a stand-in for a host-only reference; tests check it never leaks.
const hostRef = "/opt/secret-host-dir/launch-me.desktop"

// fakeLister returns a scripted catalog and counts calls. refSuffix lets a
// test change the reference between calls to prove a launch lists afresh.
type fakeLister struct {
	mu       sync.Mutex
	platform string
	apps     []Listing
	err      error
	calls    int
	onList   func(call int)
}

func (f *fakeLister) List(context.Context) (string, []Listing, error) {
	f.mu.Lock()
	f.calls++
	call := f.calls
	hook := f.onList
	f.mu.Unlock()
	if hook != nil {
		hook(call)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.platform, append([]Listing(nil), f.apps...), f.err
}

func (f *fakeLister) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

// fakeLauncher records launches; block, when set, holds a launch open.
type fakeLauncher struct {
	mu      sync.Mutex
	got     []Listing
	err     error
	entered chan struct{}
	block   chan struct{}
}

func (f *fakeLauncher) Launch(_ context.Context, app Listing) error {
	f.mu.Lock()
	f.got = append(f.got, app)
	err := f.err
	f.mu.Unlock()
	if f.entered != nil {
		f.entered <- struct{}{}
	}
	if f.block != nil {
		<-f.block
	}
	return err
}

func (f *fakeLauncher) launched() []Listing {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]Listing(nil), f.got...)
}

// fakeAudit records events.
type fakeAudit struct {
	mu     sync.Mutex
	events []audit.Event
}

func (f *fakeAudit) Try(ev audit.Event) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.events = append(f.events, ev)
}

func (f *fakeAudit) all() []audit.Event {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]audit.Event(nil), f.events...)
}

// clock is a settable time source.
type clock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *clock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *clock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

var (
	idLaunchable = makeID(linuxIDDomain, "editor.desktop")
	idListedOnly = makeID(linuxIDDomain, "viewer.desktop")
)

// rig is a Service over fakes.
type rig struct {
	svc      *Service
	lister   *fakeLister
	launcher *fakeLauncher
	audit    *fakeAudit
	clock    *clock
}

func newRig(t *testing.T, mutate func(*Config)) *rig {
	t.Helper()
	r := &rig{
		lister: &fakeLister{platform: PlatformLinux, apps: []Listing{
			{App: App{ID: idLaunchable, Name: "Editor", Launchable: true}, Ref: hostRef},
			{App: App{ID: idListedOnly, Name: "Viewer", Launchable: false}, Ref: hostRef},
		}},
		launcher: &fakeLauncher{},
		audit:    &fakeAudit{},
		clock:    &clock{t: time.Date(2030, 1, 2, 3, 4, 5, 0, time.UTC)},
	}
	cfg := Config{Lister: r.lister, Launcher: r.launcher, Audit: r.audit, Now: r.clock.now}
	if mutate != nil {
		mutate(&cfg)
	}
	r.svc = New(cfg)
	return r
}

// device builds a caller with the two app grants given.
func device(id string, view, launch bool) authz.Principal {
	return authz.Principal{DeviceID: id, Grants: authz.Grants{ViewApps: view, LaunchApps: launch}}
}

var anyPolicy = authz.Policy{}

func asError(t *testing.T, err error) *Error {
	t.Helper()
	var e *Error
	if !errors.As(err, &e) {
		t.Fatalf("error %v is not an *apps.Error", err)
	}
	return e
}

func TestListNeedsViewAppsAndNeverCallsProviderWhenDenied(t *testing.T) {
	cases := []struct {
		name string
		p    authz.Principal
	}{
		{"no grants", device("d", false, false)},
		{"launch only", device("d", false, true)},
		{"admin login does not bypass", authz.Principal{DeviceID: "d", Admin: true}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := newRig(t, nil)
			_, err := r.svc.List(context.Background(), c.p, anyPolicy)
			if !errors.Is(err, authz.ErrCapabilityDenied) {
				t.Fatalf("err = %v, want capability denied", err)
			}
			e := asError(t, err)
			if e.Code != CodeViewDenied || e.Status != http.StatusForbidden {
				t.Fatalf("code/status = %s/%d", e.Code, e.Status)
			}
			if r.lister.callCount() != 0 {
				t.Fatal("provider was called for a denied caller")
			}
		})
	}
}

func TestDenialCodeIsTheContractCodeAndLegacyCodesAreNamed(t *testing.T) {
	if CodeViewDenied != "CAPABILITY_DENIED" || CodeLaunchDenied != "CAPABILITY_DENIED" {
		t.Fatalf("denial codes = %q, %q; openapi says CAPABILITY_DENIED", CodeViewDenied, CodeLaunchDenied)
	}
	if LegacyCodeViewForbidden != "APP_VIEW_FORBIDDEN" || LegacyCodeLaunchForbidden != "APP_LAUNCH_FORBIDDEN" {
		t.Fatal("legacy codes changed; desktop/src-tauri/src/apps.rs keys on them")
	}
}

func TestListRevokedAndUnknownDevicesAreUnauthorized(t *testing.T) {
	r := newRig(t, nil)
	revoked := device("d", true, true)
	revoked.Revoked = true
	for name, p := range map[string]authz.Principal{"revoked": revoked, "no device": {}} {
		if _, err := r.svc.List(context.Background(), p, anyPolicy); !errors.Is(err, authz.ErrUnauthorized) {
			t.Fatalf("%s: err = %v, want unauthorized", name, err)
		}
	}
}

func TestListReturnsCatalogWithoutHostReferences(t *testing.T) {
	r := newRig(t, nil)
	cat, err := r.svc.List(context.Background(), device("d", true, true), anyPolicy)
	if err != nil {
		t.Fatal(err)
	}
	if cat.Platform != PlatformLinux || len(cat.Apps) != 2 || !cat.LaunchAllowed {
		t.Fatalf("catalog = %+v", cat)
	}
	body, _ := json.Marshal(cat)
	if strings.Contains(string(body), "secret-host-dir") || strings.Contains(string(body), ".desktop") {
		t.Fatalf("catalog JSON leaks a host reference: %s", body)
	}
	if !strings.Contains(string(body), `"launchAllowed":true`) || !strings.Contains(string(body), `"launchable":false`) {
		t.Fatalf("catalog JSON lacks contract fields: %s", body)
	}
}

func TestListLaunchAllowedFollowsTheLaunchGrant(t *testing.T) {
	r := newRig(t, nil)
	cat, err := r.svc.List(context.Background(), device("d", true, false), anyPolicy)
	if err != nil {
		t.Fatal(err)
	}
	if cat.LaunchAllowed {
		t.Fatal("view-only device was told it may launch")
	}
}

func TestListEmptyCatalogIsAnEmptyArray(t *testing.T) {
	r := newRig(t, func(c *Config) { c.Lister = &fakeLister{platform: PlatformLinux} })
	cat, err := r.svc.List(context.Background(), device("d", true, false), anyPolicy)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(cat)
	if !strings.Contains(string(body), `"apps":[]`) {
		t.Fatalf("body = %s", body)
	}
}

func TestListCleansWhatAProviderReturns(t *testing.T) {
	good := makeID(linuxIDDomain, "good.desktop")
	r := newRig(t, func(c *Config) {
		c.Lister = &fakeLister{platform: PlatformLinux, apps: []Listing{
			{App: App{ID: good, Name: "Go\nod/App\x00", Icon: "../../etc/passwd", Launchable: true}, Ref: hostRef},
			{App: App{ID: good, Name: "Duplicate", Launchable: true}},
			{App: App{ID: "app_short", Name: "Bad id"}},
			{App: App{ID: makeID(linuxIDDomain, "blank"), Name: " \n "}},
		}}
	})
	cat, err := r.svc.List(context.Background(), device("d", true, false), anyPolicy)
	if err != nil {
		t.Fatal(err)
	}
	if len(cat.Apps) != 1 {
		t.Fatalf("apps = %+v, want only the good one", cat.Apps)
	}
	if a := cat.Apps[0]; a.Name != "Good App" || a.Icon != "" {
		t.Fatalf("app = %+v", a)
	}
}

func TestListProviderErrorsMapToContractErrors(t *testing.T) {
	cases := []struct {
		err    error
		status int
		code   string
	}{
		{ErrProviderUnsupported, http.StatusNotImplemented, CodeCatalogUnsupported},
		{ErrProviderMissing, http.StatusServiceUnavailable, CodeLaunchUnavailable},
	}
	for _, c := range cases {
		r := newRig(t, nil)
		r.lister.err = c.err
		_, err := r.svc.List(context.Background(), device("d", true, false), anyPolicy)
		e := asError(t, err)
		if e.Status != c.status || e.Code != c.code {
			t.Fatalf("%v -> %d %s", c.err, e.Status, e.Code)
		}
	}
	r := newRig(t, nil)
	r.lister.err = errors.New("read " + hostRef + ": permission denied")
	_, err := r.svc.List(context.Background(), device("d", true, false), anyPolicy)
	var e *Error
	if err == nil || errors.As(err, &e) {
		t.Fatalf("unexpected failure must stay an internal error, got %v", err)
	}
}

func TestCatalogRateLimitPerDeviceWithResetAfterWindow(t *testing.T) {
	r := newRig(t, func(c *Config) { c.CatalogAttempts = 3; c.CatalogWindow = time.Minute })
	a, b := device("a", true, false), device("b", true, false)
	for i := 0; i < 3; i++ {
		if _, err := r.svc.List(context.Background(), a, anyPolicy); err != nil {
			t.Fatalf("call %d: %v", i, err)
		}
	}
	if _, err := r.svc.List(context.Background(), a, anyPolicy); asError(t, err).Code != CodeCatalogRateLimited {
		t.Fatalf("4th call err = %v", err)
	}
	if _, err := r.svc.List(context.Background(), b, anyPolicy); err != nil {
		t.Fatalf("another device must have its own budget: %v", err)
	}
	r.clock.advance(time.Minute)
	if _, err := r.svc.List(context.Background(), a, anyPolicy); err != nil {
		t.Fatalf("budget did not reset: %v", err)
	}
}

func TestLaunchGrantMatrix(t *testing.T) {
	roPrincipal := device("d", true, true)
	roPrincipal.ReadOnly = true
	admin := authz.Principal{DeviceID: "d", Admin: true, Grants: authz.Grants{ViewApps: true, LaunchApps: true}}
	adminNoApps := authz.Principal{DeviceID: "d", Admin: true}
	cases := []struct {
		name    string
		p       authz.Principal
		pol     authz.Policy
		allowed bool
	}{
		{"view and launch", device("d", true, true), anyPolicy, true},
		{"view only", device("d", true, false), anyPolicy, false},
		{"launch without view", device("d", false, true), anyPolicy, false},
		{"neither", device("d", false, false), anyPolicy, false},
		{"admin with both grants", admin, anyPolicy, true},
		{"admin without app grants", adminNoApps, anyPolicy, false},
		// Launching is not a write in authz (the old agent also ignored
		// read-only for apps), so read-only alone must not block it.
		{"device read-only with grants", roPrincipal, anyPolicy, true},
		{"global read-only with grants", device("d", true, true), authz.Policy{ReadOnly: true}, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := newRig(t, nil)
			err := r.svc.Launch(context.Background(), c.p, c.pol, idLaunchable)
			if c.allowed {
				if err != nil || len(r.launcher.launched()) != 1 {
					t.Fatalf("err = %v, launches = %d", err, len(r.launcher.launched()))
				}
				return
			}
			if !errors.Is(err, authz.ErrCapabilityDenied) {
				t.Fatalf("err = %v, want capability denied", err)
			}
			if e := asError(t, err); e.Code != CodeLaunchDenied || e.Status != http.StatusForbidden {
				t.Fatalf("code/status = %s/%d", e.Code, e.Status)
			}
			if len(r.launcher.launched()) != 0 || r.lister.callCount() != 0 {
				t.Fatal("a denied caller reached the provider")
			}
		})
	}
}

func TestLaunchRevokedDeviceIsUnauthorized(t *testing.T) {
	r := newRig(t, nil)
	p := device("d", true, true)
	p.Revoked = true
	if err := r.svc.Launch(context.Background(), p, anyPolicy, idLaunchable); !errors.Is(err, authz.ErrUnauthorized) {
		t.Fatalf("err = %v", err)
	}
}

func TestLaunchRejectsAnythingButAnOpaqueID(t *testing.T) {
	bad := []string{
		"", "firefox", "../../bin/sh", "/usr/bin/xterm", hostRef, "app_",
		"app_" + strings.Repeat("g", 64), "app_" + strings.Repeat("a", 63),
		"app_" + strings.Repeat("a", 65), strings.ToUpper(idLaunchable),
		idLaunchable + "\n", idLaunchable + " ;rm -rf /", "-a /bin/sh",
	}
	for _, id := range bad {
		r := newRig(t, nil)
		err := r.svc.Launch(context.Background(), device("d", true, true), anyPolicy, id)
		if e := asError(t, err); e.Code != CodeBadAppID || e.Status != http.StatusBadRequest {
			t.Fatalf("id %q -> %v", id, err)
		}
		if len(r.launcher.launched()) != 0 || r.lister.callCount() != 0 {
			t.Fatalf("id %q reached the provider", id)
		}
		for _, ev := range r.audit.all() {
			if strings.Contains(ev.Target, "/") || strings.Contains(ev.Target, "\n") || ev.Target == id && id != "" {
				t.Fatalf("audit target keeps raw caller text: %q", ev.Target)
			}
		}
	}
}

func TestLaunchResolvesIDAgainstAFreshListing(t *testing.T) {
	r := newRig(t, nil)
	r.lister.onList = func(call int) {
		r.lister.mu.Lock()
		defer r.lister.mu.Unlock()
		r.lister.apps[0].Ref = "/fresh/ref-" + string(rune('0'+call)) + ".desktop"
	}
	if err := r.svc.Launch(context.Background(), device("d", true, true), anyPolicy, idLaunchable); err != nil {
		t.Fatal(err)
	}
	got := r.launcher.launched()
	if len(got) != 1 || got[0].Ref != "/fresh/ref-1.desktop" || got[0].ID != idLaunchable {
		t.Fatalf("launched = %+v", got)
	}
	if r.lister.callCount() != 1 {
		t.Fatalf("listings = %d, want 1 fresh listing per launch", r.lister.callCount())
	}
	if err := r.svc.Launch(context.Background(), device("d", true, true), anyPolicy, idLaunchable); err != nil {
		t.Fatal(err)
	}
	if got := r.launcher.launched(); got[1].Ref != "/fresh/ref-2.desktop" {
		t.Fatalf("second launch used a stale reference: %+v", got[1])
	}
}

func TestLaunchUnknownAndNonLaunchableApps(t *testing.T) {
	r := newRig(t, nil)
	other := makeID(linuxIDDomain, "gone.desktop")
	if err := r.svc.Launch(context.Background(), device("d", true, true), anyPolicy, other); asError(t, err).Code != CodeNotFound || asError(t, err).Status != http.StatusNotFound {
		t.Fatalf("unknown id err = %v", err)
	}
	if err := r.svc.Launch(context.Background(), device("d", true, true), anyPolicy, idListedOnly); asError(t, err).Code != CodeNotLaunchable || asError(t, err).Status != http.StatusConflict {
		t.Fatalf("non-launchable err = %v", err)
	}
	if len(r.launcher.launched()) != 0 {
		t.Fatal("launcher called for an app that cannot be launched")
	}
}

func TestLaunchProviderErrorsMapAndNeverLeakTheReference(t *testing.T) {
	cases := []struct {
		name   string
		listEr error
		launch error
		status int
		code   string
	}{
		{"list unsupported", ErrProviderUnsupported, nil, http.StatusNotImplemented, CodeCatalogUnsupported},
		{"list missing", ErrProviderMissing, nil, http.StatusServiceUnavailable, CodeLaunchUnavailable},
		{"gone", nil, ErrGone, http.StatusNotFound, CodeNotFound},
		{"not launchable", nil, ErrNotLaunchable, http.StatusConflict, CodeNotLaunchable},
		{"no session", nil, ErrNoDesktopSession, http.StatusServiceUnavailable, CodeNoSession},
		{"launcher missing", nil, ErrProviderMissing, http.StatusServiceUnavailable, CodeLaunchUnavailable},
		{"start failed", nil, ErrStartFailed, http.StatusBadGateway, CodeLaunchFailed},
		{"unexpected", nil, errors.New("exec " + hostRef + ": boom"), http.StatusBadGateway, CodeLaunchFailed},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := newRig(t, nil)
			r.lister.err = c.listEr
			r.launcher.err = c.launch
			err := r.svc.Launch(context.Background(), device("d", true, true), anyPolicy, idLaunchable)
			e := asError(t, err)
			if e.Status != c.status || e.Code != c.code {
				t.Fatalf("got %d %s, want %d %s", e.Status, e.Code, c.status, c.code)
			}
			if strings.Contains(err.Error(), "secret-host-dir") {
				t.Fatalf("error text leaks a host path: %q", err.Error())
			}
		})
	}
}

func TestOnlyOneLaunchRunsAtATime(t *testing.T) {
	r := newRig(t, nil)
	r.launcher.entered = make(chan struct{}, 1)
	r.launcher.block = make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- r.svc.Launch(context.Background(), device("a", true, true), anyPolicy, idLaunchable)
	}()
	<-r.launcher.entered
	err := r.svc.Launch(context.Background(), device("b", true, true), anyPolicy, idLaunchable)
	if e := asError(t, err); e.Code != CodeLaunchBusy || e.Status != http.StatusConflict {
		t.Fatalf("second launch err = %v", err)
	}
	close(r.launcher.block)
	if err := <-done; err != nil {
		t.Fatalf("first launch: %v", err)
	}
	r.launcher.block = nil
	if err := r.svc.Launch(context.Background(), device("b", true, true), anyPolicy, idLaunchable); err != nil {
		t.Fatalf("slot was not released: %v", err)
	}
}

func TestLaunchRateLimitPerDeviceCountsFailedAttemptsAndResets(t *testing.T) {
	r := newRig(t, func(c *Config) { c.LaunchAttempts = 2; c.LaunchWindow = time.Minute })
	a := device("a", true, true)
	missing := makeID(linuxIDDomain, "gone.desktop")
	_ = r.svc.Launch(context.Background(), a, anyPolicy, missing)
	_ = r.svc.Launch(context.Background(), a, anyPolicy, idLaunchable)
	err := r.svc.Launch(context.Background(), a, anyPolicy, idLaunchable)
	if e := asError(t, err); e.Code != CodeLaunchRateLimited || e.Status != http.StatusTooManyRequests {
		t.Fatalf("3rd attempt err = %v", err)
	}
	if n := len(r.launcher.launched()); n != 1 {
		t.Fatalf("launches = %d, want 1", n)
	}
	if err := r.svc.Launch(context.Background(), device("b", true, true), anyPolicy, idLaunchable); err != nil {
		t.Fatalf("another device must have its own budget: %v", err)
	}
	r.clock.advance(time.Minute)
	if err := r.svc.Launch(context.Background(), a, anyPolicy, idLaunchable); err != nil {
		t.Fatalf("budget did not reset: %v", err)
	}
}

func TestDeniedAttemptsDoNotSpendTheLaunchBudget(t *testing.T) {
	r := newRig(t, func(c *Config) { c.LaunchAttempts = 1 })
	for i := 0; i < 3; i++ {
		_ = r.svc.Launch(context.Background(), device("a", true, false), anyPolicy, idLaunchable)
	}
	if err := r.svc.Launch(context.Background(), device("a", true, true), anyPolicy, idLaunchable); err != nil {
		t.Fatalf("err = %v", err)
	}
}

func TestLaunchIsAuditedWithOpaqueIDNameAndOutcomeOnly(t *testing.T) {
	r := newRig(t, nil)
	_ = r.svc.Launch(context.Background(), device("d", true, true), anyPolicy, idLaunchable)
	_ = r.svc.Launch(context.Background(), device("d", true, false), anyPolicy, idLaunchable)
	ev := r.audit.all()
	if len(ev) != 2 {
		t.Fatalf("events = %+v", ev)
	}
	if ev[0].Action != audit.ActionAppLaunch || ev[0].Actor != "d" || ev[0].Target != idLaunchable ||
		ev[0].Detail != `name="Editor" outcome=started` {
		t.Fatalf("started event = %+v", ev[0])
	}
	if ev[1].Detail != "outcome=permission_denied" {
		t.Fatalf("denied event = %+v", ev[1])
	}
	for _, e := range ev {
		if strings.Contains(e.Detail+e.Target, "secret-host-dir") {
			t.Fatalf("audit leaks a host reference: %+v", e)
		}
	}
}

func TestServiceWorksWithoutAnAuditor(t *testing.T) {
	r := newRig(t, func(c *Config) { c.Audit = nil })
	if err := r.svc.Launch(context.Background(), device("d", true, true), anyPolicy, idLaunchable); err != nil {
		t.Fatal(err)
	}
}

func TestLimiterTableIsBounded(t *testing.T) {
	l := newLimiter(1, time.Minute, 2)
	now := time.Unix(1000, 0)
	if !l.allow("a", now) || !l.allow("b", now) {
		t.Fatal("first two devices must fit")
	}
	if l.allow("c", now) {
		t.Fatal("a third device must be refused while the table is full")
	}
	if l.allow("a", now) {
		t.Fatal("a known device is still over its budget")
	}
	if !l.allow("c", now.Add(time.Minute)) {
		t.Fatal("expired windows must free slots")
	}
}

func TestDefaultsAreApplied(t *testing.T) {
	s := New(Config{Lister: &fakeLister{}, Launcher: &fakeLauncher{}})
	c := s.cfg
	if c.Now == nil || c.CatalogAttempts != DefaultCatalogAttempts || c.LaunchAttempts != DefaultLaunchAttempts ||
		c.LaunchTimeout != DefaultLaunchTimeout || c.LimiterDevices != DefaultLimiterDevices {
		t.Fatalf("defaults not applied: %+v", c)
	}
}
