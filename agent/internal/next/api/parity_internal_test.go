package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ZaidQamhieh/rfe/agent/internal/next/authz"
	"github.com/ZaidQamhieh/rfe/agent/internal/next/settings"
	"github.com/ZaidQamhieh/rfe/agent/internal/next/store"
)

// noop is a handler that does nothing.
func noop(http.ResponseWriter, *http.Request) error { return nil }

func TestCheckParityNamesEveryMismatch(t *testing.T) {
	routes := []Route{{get, "/a", public, none}, {post, "/b", device, none}, {get, "/c", device, none}}
	h := map[string]handlerFunc{"GET /a": noop, "POST /b": noop}
	if err := checkParity(routes, h, keySet("GET /c")); err != nil {
		t.Fatalf("a consistent set failed: %v", err)
	}
	cases := []struct {
		name string
		h    map[string]handlerFunc
		nyi  map[string]struct{}
		want string
	}{
		{"route without handler", h, keySet(), "route has no handler: GET /c"},
		{"handler without route", map[string]handlerFunc{"GET /a": noop, "POST /b": noop, "GET /zzz": noop}, keySet("GET /c"), "handler has no route: GET /zzz"},
		{"stale not-implemented entry", h, keySet("GET /c", "POST /b"), "has a handler but is listed as not implemented: POST /b"},
		{"unknown not-implemented entry", h, keySet("GET /c", "GET /ghost"), "not-implemented entry has no route: GET /ghost"},
	}
	for _, c := range cases {
		err := checkParity(routes, c.h, c.nyi)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: err = %v, want it to contain %q", c.name, err, c.want)
		}
	}
}

func TestProductionTableAndRegistryAgree(t *testing.T) {
	s := &Server{}
	if err := checkParity(Routes, s.registry(), NotYetImplemented); err != nil {
		t.Fatal(err)
	}
}

// fakePairing authenticates one fixed device; every other method is the nil
// embedded interface, so a call to it panics and fails the test.
type fakePairing struct {
	Pairing
	dev *store.Device
}

// Authenticate returns the fixed device for the token "good".
func (f fakePairing) Authenticate(token string) (*store.Device, error) {
	if token != "good" {
		return nil, errNoDevice
	}
	return f.dev, nil
}

// Touch accepts the bookkeeping call.
func (f fakePairing) Touch(string, string, string) error { return nil }

var errNoDevice = &apiError{kind: authz.ErrUnauthorized, msg: "no device"}

type fixedPolicy struct{ s settings.Settings }

// Get returns the fixed settings.
func (f fixedPolicy) Get() settings.Settings { return f.s }

func TestGuardPutsThePrincipalAndPolicyInTheContext(t *testing.T) {
	dev := &store.Device{ID: "d1", Origin: store.OriginLogin, CanBrowse: true, JailRoot: "/j", ReadOnly: true}
	s := &Server{
		cfg:    Config{Pairing: fakePairing{dev: dev}, Policy: fixedPolicy{settings.Settings{ReadOnly: true, Roots: []string{"/r"}}}},
		logger: discardLogger{},
	}
	req := httptest.NewRequest(http.MethodGet, "/v1/x", nil)
	req.Header.Set("Authorization", "Bearer good")
	ctx, err := s.guard(Route{get, "/x", admin, authz.ActionAdmin}, req)
	if err != nil {
		t.Fatal(err)
	}
	p, ok := PrincipalFrom(ctx)
	if !ok || p.DeviceID != "d1" || !p.Admin || p.JailRoot != "/j" || !p.ReadOnly || !p.Grants.Browse {
		t.Fatalf("principal = %+v ok=%v", p, ok)
	}
	if c := ctx.Value(callKey{}).(*call); !c.policy.ReadOnly || len(c.policy.Roots) != 1 {
		t.Fatalf("policy = %+v", c.policy)
	}
	if _, ok := PrincipalFrom(context.Background()); ok {
		t.Fatal("a bare context has a principal")
	}
	// A public route needs no credential and carries no principal.
	pub, err := s.guard(Route{get, "/x", public, none}, httptest.NewRequest(http.MethodGet, "/v1/x", nil))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := PrincipalFrom(pub); ok {
		t.Fatal("a public request has a principal")
	}
	// Refused without a valid token.
	if _, err := s.guard(Route{get, "/x", device, none}, httptest.NewRequest(http.MethodGet, "/v1/x", nil)); Map(err).Code != authz.CodeUnauthorized {
		t.Fatalf("no token: %v", err)
	}
}
