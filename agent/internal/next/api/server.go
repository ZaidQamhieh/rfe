package api

import (
	"context"
	"errors"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/ZaidQamhieh/rfe/agent/internal/events"
	"github.com/ZaidQamhieh/rfe/agent/internal/next/apps"
	"github.com/ZaidQamhieh/rfe/agent/internal/next/authz"
	"github.com/ZaidQamhieh/rfe/agent/internal/next/pairing"
	"github.com/ZaidQamhieh/rfe/agent/internal/next/settings"
	"github.com/ZaidQamhieh/rfe/agent/internal/next/store"
	"github.com/ZaidQamhieh/rfe/agent/internal/next/system"
)

// DefaultMaxBodyBytes caps a request body (1 MiB). Every JSON body of this
// slice is small; file content travels on routes that set their own limit.
const DefaultMaxBodyBytes int64 = 1 << 20

// Pairing is the part of the pairing service the HTTP layer uses.
// *pairing.Service satisfies it.
type Pairing interface {
	Authenticate(token string) (*store.Device, error)
	Touch(deviceID, address, version string) error
	Challenge(remote string) (pairing.Challenge, error)
	PairWithCode(in pairing.PairInput) (pairing.PairResult, error)
	Register(in pairing.RegisterInput) (pairing.PairResult, error)
	Login(in pairing.LoginInput) (pairing.PairResult, error)
	CreateRequest(in pairing.RequestInput) (pairing.RequestCreated, error)
	Reveal(id, clientNonce, remote string) error
	Poll(id, nonce, remote string) (pairing.PollResult, error)
	ListPending(by pairing.Actor) ([]pairing.PendingRequest, error)
	Decide(by pairing.Actor, id string, approve bool) error
	Generate(by pairing.Actor, req pairing.GenerateRequest, validateJail func(root string) (string, error)) (pairing.MintedCode, error)
	Revoke(by pairing.Actor, id string) error
	Remove(by pairing.Actor, id string) error
	SetJail(by pairing.Actor, id, root string) error
	SetReadOnly(by pairing.Actor, id string, ro bool) error
	SetFileGrants(by pairing.Actor, id string, g authz.Grants) error
	SetAppGrants(by pairing.Actor, id string, g authz.Grants) error
	UpdateDevice(by pairing.Actor, id string, p pairing.DevicePatch) error
}

// DeviceDirectory reads device rows for the device list. *store.DB satisfies it.
type DeviceDirectory interface {
	Devices() ([]store.Device, error)
	DeviceByID(id string) (*store.Device, error)
}

// System answers health, status and metrics. *system.Service satisfies it.
type System interface {
	Health(authenticated bool) system.Health
	Status() (system.Status, error)
	Metrics() system.Metrics
}

// Apps lists and launches host apps. *apps.Service satisfies it.
type Apps interface {
	List(ctx context.Context, p authz.Principal, pol authz.Policy) (apps.Catalog, error)
	Launch(ctx context.Context, p authz.Principal, pol authz.Policy, rawID string) error
}

// PolicySource supplies the live global policy (read-only mode, allowed roots).
// *settings.Service satisfies it.
type PolicySource interface{ Get() settings.Settings }

// Clock supplies the current time, for request timing in the log.
type Clock interface{ Now() time.Time }

// Logger receives server-side diagnostics (internal errors, panics, one line
// per request). *log.Logger satisfies it. Client responses never carry its text.
type Logger interface {
	Printf(format string, args ...any)
}

// Config wires a Server. Pairing, Devices, System, Apps and Policy are required.
type Config struct {
	Pairing Pairing
	Devices DeviceDirectory
	System  System
	Apps    Apps
	Policy  PolicySource

	// Files gives each caller's file service. Nil leaves every file route
	// answering 501 NOT_IMPLEMENTED.
	Files FilesProvider

	// Transfers and Uploads serve the resumable upload routes; either nil leaves
	// those routes answering 501 NOT_IMPLEMENTED. ChunkBufferBytes and ChunkWait
	// tune the chunk memory budget (defaults DefaultChunkBufferBytes, DefaultChunkWait).
	Transfers        Transfers
	Uploads          UploadLister
	ChunkBufferBytes int64
	ChunkWait        time.Duration

	// Misc holds the services of search, thumbnails, share links, settings and
	// audit; a nil one leaves its routes answering 501 NOT_IMPLEMENTED.
	Misc MiscServices

	// Host holds the services of the user list, log tail, restart, Wake-on-LAN
	// and app update routes; a nil one leaves its routes answering 501.
	Host HostServices

	// Events is the change-hint hub behind GET /v1/events; successful mutations
	// publish to it. Nil leaves that route answering 501 NOT_IMPLEMENTED.
	Events *events.Hub

	// ValidateJail checks a per-device or guest-code jail folder against the
	// allowed roots and returns its normalised form. Nil refuses every non-empty
	// jail. Its error text is never sent to the client.
	ValidateJail func(root string) (string, error)

	Clock        Clock  // default: the system clock
	Logger       Logger // default: discard
	MaxBodyBytes int64  // default DefaultMaxBodyBytes
}

// Server is the HTTP handler of the new agent: a ServeMux built from the Routes
// table, wrapped in recovery, a body limit, authentication and authorization.
type Server struct {
	upPacer, downPacer sharedPacer // one timeline per direction, so parallel transfers share the limit
	cfg                Config
	mux                *http.ServeMux
	maxBody            int64
	logger             Logger
	clock              Clock
	handlers           map[string]handlerFunc
}

// handlerFunc serves one route and returns the error to report, or nil after it
// wrote a response itself.
type handlerFunc func(w http.ResponseWriter, r *http.Request) error

// routeKey names a route in the handler registry and in NotYetImplemented.
func routeKey(method, pattern string) string { return method + " " + pattern }

// notYet is the answer of a route in NotYetImplemented.
var notYet = &apiError{kind: ErrNotImplemented, msg: "this operation is not available yet"}

// NotYetImplemented lists the routes of the table that have no handler yet; each
// answers 501 NOT_IMPLEMENTED after the usual authentication and authorization.
// New fails when a route is in neither this set nor the handler registry, or in
// both, so the list shrinks as handlers arrive and cannot go stale.
var NotYetImplemented = keySet(
	routeKey(get, "/accounts/key"),
	routeKey(post, "/accounts/assertion"),
	routeKey(get, "/settings/account-server"),
	routeKey(put, "/settings/account-server"),
	routeKey(del, "/settings/account-server"),
	routeKey(get, "/stream"),
)

// keySet turns a list into a set.
func keySet(keys ...string) map[string]struct{} {
	m := make(map[string]struct{}, len(keys))
	for _, k := range keys {
		m[k] = struct{}{}
	}
	return m
}

// checkParity compares the route table with the handler registry and the
// not-yet-implemented set. It names every route without a handler, every handler
// without a route, every set entry that is unknown or already has a handler.
func checkParity(routes []Route, handlers map[string]handlerFunc, nyi map[string]struct{}) error {
	inTable := make(map[string]struct{}, len(routes))
	var problems []string
	for _, rt := range routes {
		k := routeKey(rt.Method, rt.Pattern)
		inTable[k] = struct{}{}
		_, has := handlers[k]
		_, notYet := nyi[k]
		switch {
		case !has && !notYet:
			problems = append(problems, "route has no handler: "+k)
		case has && notYet:
			problems = append(problems, "route has a handler but is listed as not implemented: "+k)
		}
	}
	for k := range handlers {
		if _, ok := inTable[k]; !ok {
			problems = append(problems, "handler has no route: "+k)
		}
	}
	for k := range nyi {
		if _, ok := inTable[k]; !ok {
			problems = append(problems, "not-implemented entry has no route: "+k)
		}
	}
	if len(problems) == 0 {
		return nil
	}
	sort.Strings(problems)
	return errors.New("api: route table mismatch: " + strings.Join(problems, "; "))
}

// New validates cfg and builds the handler. It fails when a required service is
// missing or the route table, handlers and NotYetImplemented disagree, so a
// mistake shows at startup and not as a missing route in production.
func New(cfg Config) (http.Handler, error) {
	switch {
	case cfg.Pairing == nil:
		return nil, errors.New("api: a pairing service is required")
	case cfg.Devices == nil:
		return nil, errors.New("api: a device directory is required")
	case cfg.System == nil:
		return nil, errors.New("api: a system service is required")
	case cfg.Apps == nil:
		return nil, errors.New("api: an apps service is required")
	case cfg.Policy == nil:
		return nil, errors.New("api: a policy source is required")
	}
	s := &Server{cfg: cfg, maxBody: cfg.MaxBodyBytes, logger: cfg.Logger, clock: cfg.Clock}
	if s.maxBody <= 0 {
		s.maxBody = DefaultMaxBodyBytes
	}
	if s.logger == nil {
		s.logger = discardLogger{}
	}
	if s.clock == nil {
		s.clock = systemClock{}
	}
	s.handlers = s.registry()

	if err := checkParity(Routes, s.handlers, NotYetImplemented); err != nil {
		return nil, err
	}
	s.mux = http.NewServeMux()
	for _, rt := range Routes {
		h, ok := s.handlers[routeKey(rt.Method, rt.Pattern)]
		if !ok {
			h = notYetHandler
		}
		s.mux.Handle(rt.Method+" "+rt.FullPattern(), s.route(rt, h))
	}
	return s, nil
}

// notYetHandler is the handler of every route in NotYetImplemented.
func notYetHandler(http.ResponseWriter, *http.Request) error { return notYet }

// registry maps each implemented route to its handler.
func (s *Server) registry() map[string]handlerFunc {
	m := map[string]handlerFunc{
		routeKey(get, "/health"):                      s.health,
		routeKey(get, "/events"):                      s.eventsStream,
		routeKey(get, "/status"):                      s.status,
		routeKey(get, "/metrics"):                     s.metrics,
		routeKey(post, "/auth/challenge"):             s.challenge,
		routeKey(post, "/auth/logout"):                s.logout,
		routeKey(post, "/pair"):                       s.pair,
		routeKey(post, "/pair/request"):               s.pairRequest,
		routeKey(post, "/pair/request/{id}/reveal"):   s.pairReveal,
		routeKey(get, "/pair/request/{id}"):           s.pairPoll,
		routeKey(get, "/pair/requests"):               s.pairPending,
		routeKey(post, "/pair/requests/{id}/approve"): s.pairApprove,
		routeKey(post, "/pair/requests/{id}/reject"):  s.pairReject,
		routeKey(post, "/register"):                   s.register,
		routeKey(post, "/login"):                      s.login,
		routeKey(post, "/pairing/generate"):           s.pairingGenerate,
		routeKey(get, "/devices"):                     s.devicesList,
		routeKey(patch, "/devices/{id}"):              s.devicePatch,
		routeKey(del, "/devices/{id}"):                s.deviceDelete,
		routeKey(get, "/apps"):                        s.appsList,
		routeKey(post, "/apps/{id}/launch"):           s.appLaunch,
	}
	registerTransferHandlers(s, m)
	registerFilesHandlers(s, m)
	registerMiscHandlers(s, m)
	registerFsExtraHandlers(s, m)
	registerContentHandlers(s, m)
	registerHostHandlers(s, m)
	return m
}

// systemClock is the real clock.
type systemClock struct{}

// Now returns the current time.
func (systemClock) Now() time.Time { return time.Now() }

// discardLogger drops every line.
type discardLogger struct{}

// Printf discards its arguments.
func (discardLogger) Printf(string, ...any) {}

// logf writes one diagnostic line.
func (s *Server) logf(format string, args ...any) { s.logger.Printf(format, args...) }
