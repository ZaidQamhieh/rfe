// Package apps is the installed-apps list and launch service of agent-next.
// It has no HTTP in it: the API layer passes the authenticated caller and a
// route value, and maps the returned error with its Code and Status.
//
// A client names an app only by an opaque id ("app_" and 64 hex digits) that
// the host's own app registry produced. The id never carries a path or a
// command, and a launch resolves it against a freshly listed set, so the only
// things ever executed are the fixed launcher programs of the host platform
// (gio, explorer.exe, open) with arguments the service built itself. The
// response never contains a host path, and neither does any error text.
package apps

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"time"

	"github.com/ZaidQamhieh/rfe/agent/internal/next/audit"
	"github.com/ZaidQamhieh/rfe/agent/internal/next/authz"
)

// Platform names the catalog reports (HostAppCatalog.platform).
const (
	PlatformLinux   = "linux"
	PlatformWindows = "windows"
	PlatformDarwin  = "darwin"
)

// Id shape (openapi HostApp.id): a prefix and a SHA-256 in lower-case hex.
const (
	idPrefix    = "app_"
	idHexDigits = 64
)

// Limits. The two budgets are the old agent's (apps_handlers.go:20-21).
const (
	DefaultCatalogAttempts = 60 // catalog reads per device per window
	DefaultCatalogWindow   = time.Minute
	DefaultLaunchAttempts  = 5 // launch attempts per device per window
	DefaultLaunchWindow    = time.Minute
	// DefaultLimiterDevices bounds how many devices each limiter tracks.
	DefaultLimiterDevices = 4096
	// DefaultLaunchTimeout bounds the native launcher call.
	DefaultLaunchTimeout = 10 * time.Second
	// MaxDisplayBytes cuts a display name (the old safeAppDisplay limit).
	MaxDisplayBytes = 240
	// maxIconNameLen bounds a theme icon name.
	maxIconNameLen = 80
)

// Denial codes. The openapi contract says a missing ViewApps or LaunchApps is
// 403 CAPABILITY_DENIED (/apps/{id}/launch 403). The old agent sent
// APP_VIEW_FORBIDDEN and APP_LAUNCH_FORBIDDEN, and desktop/src-tauri/src/apps.rs
// (APP_MESSAGES) still keys its wording on those two. OPEN DECISION: either
// keep these two constants on the contract code and teach the desktop the new
// code, or point them at the Legacy values below; nothing else changes.
const (
	CodeViewDenied   = authz.CodeCapabilityDenied
	CodeLaunchDenied = authz.CodeCapabilityDenied

	LegacyCodeViewForbidden   = "APP_VIEW_FORBIDDEN"
	LegacyCodeLaunchForbidden = "APP_LAUNCH_FORBIDDEN"
)

// Other error codes. They are the old agent's; none is in the openapi Error
// enum yet (the desktop keys on them), which the schema owner should decide.
const (
	CodeCatalogRateLimited = "APP_CATALOG_RATE_LIMITED"
	CodeLaunchRateLimited  = "APP_LAUNCH_RATE_LIMITED"
	CodeCatalogUnsupported = "APP_CATALOG_UNSUPPORTED"
	CodeLaunchUnavailable  = "APP_LAUNCH_UNAVAILABLE"
	CodeBadAppID           = "BAD_APP_ID"
	CodeNotFound           = "APP_NOT_FOUND"
	CodeNotLaunchable      = "APP_NOT_LAUNCHABLE"
	CodeLaunchBusy         = "APP_LAUNCH_BUSY"
	CodeNoSession          = "NO_INTERACTIVE_SESSION"
	CodeLaunchFailed       = "APP_LAUNCH_FAILED"
)

// Audit outcome labels written into the detail of an app_launch entry.
const (
	outcomeStarted       = "started"
	outcomeDenied        = "permission_denied"
	outcomeInvalidID     = "invalid_id"
	outcomeRateLimited   = "rate_limited"
	outcomeUnsupported   = "unsupported_platform"
	outcomeUnavailable   = "launcher_unavailable"
	outcomeCatalogError  = "catalog_error"
	outcomeNotFound      = "not_found"
	outcomeNotLaunchable = "not_launchable"
	outcomeBusy          = "busy"
	outcomeNoSession     = "no_interactive_session"
	outcomeFailed        = "launch_failed"

	// invalidIDPrefix and invalidIDDigest build a safe audit target for a
	// malformed id: a digest, never the caller's text.
	invalidIDPrefix = "invalid_"
	invalidIDDigest = 8
)

// Error is a failure the HTTP layer reports as-is: Code is the error code,
// Status the HTTP status. Message is fixed text with no host data. Cause, when
// set, is reachable with errors.Is and errors.As but never shown to a client.
type Error struct {
	Code    string
	Status  int
	Message string
	Cause   error
}

// Error returns the client-safe message.
func (e *Error) Error() string { return e.Message }

// Unwrap returns the underlying cause, or nil.
func (e *Error) Unwrap() error { return e.Cause }

// Errors the service returns (each has a fixed message).
var (
	ErrCatalogRateLimited  = &Error{Code: CodeCatalogRateLimited, Status: http.StatusTooManyRequests, Message: "too many app catalog requests; try again shortly"}
	ErrLaunchRateLimited   = &Error{Code: CodeLaunchRateLimited, Status: http.StatusTooManyRequests, Message: "too many app launch attempts; try again shortly"}
	ErrCatalogUnsupported  = &Error{Code: CodeCatalogUnsupported, Status: http.StatusNotImplemented, Message: "app catalog is not supported on this host platform"}
	ErrCatalogUnavailable  = &Error{Code: CodeLaunchUnavailable, Status: http.StatusServiceUnavailable, Message: "the host app catalog provider is unavailable"}
	ErrBadAppID            = &Error{Code: CodeBadAppID, Status: http.StatusBadRequest, Message: "app id is invalid"}
	ErrAppNotFound         = &Error{Code: CodeNotFound, Status: http.StatusNotFound, Message: "app is no longer available in the host catalog"}
	ErrAppNotLaunchable    = &Error{Code: CodeNotLaunchable, Status: http.StatusConflict, Message: "this app has no supported launch action"}
	ErrLaunchBusy          = &Error{Code: CodeLaunchBusy, Status: http.StatusConflict, Message: "another app launch is in progress"}
	ErrNoInteractiveUser   = &Error{Code: CodeNoSession, Status: http.StatusServiceUnavailable, Message: "the host has no active graphical desktop session"}
	ErrLauncherUnavailable = &Error{Code: CodeLaunchUnavailable, Status: http.StatusServiceUnavailable, Message: "the host app launcher is unavailable"}
	ErrLaunchFailed        = &Error{Code: CodeLaunchFailed, Status: http.StatusBadGateway, Message: "the host could not start this app"}
)

// Errors a Lister or Launcher returns. Anything else they return is treated as
// an internal failure.
var (
	ErrProviderUnsupported = errors.New("apps: platform has no app catalog")
	ErrProviderMissing     = errors.New("apps: native app tool is unavailable")
	ErrNoDesktopSession    = errors.New("apps: no interactive desktop session")
	ErrGone                = errors.New("apps: app is no longer registered")
	ErrNotLaunchable       = errors.New("apps: app has no supported launch action")
	ErrStartFailed         = errors.New("apps: native launch failed")
)

// App is one catalog entry as the client sees it (openapi HostApp).
type App struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Icon        string `json:"icon,omitempty"`
	Category    string `json:"category,omitempty"`
	Launchable  bool   `json:"launchable"`
}

// Listing is a provider's entry: the public App plus Ref, the platform's own
// registration reference (a .desktop path, an AUMID, a bundle path). Ref is
// handed only to the Launcher and is never serialized or logged.
type Listing struct {
	App
	Ref string `json:"-"`
}

// Catalog is the answer of List (openapi HostAppCatalog).
type Catalog struct {
	Platform      string `json:"platform"`
	LaunchAllowed bool   `json:"launchAllowed"`
	Apps          []App  `json:"apps"`
}

// Lister reads the host's app registry. It returns the platform name and the
// entries; it must return ErrProviderUnsupported or ErrProviderMissing for the
// two expected failures.
type Lister interface {
	List(ctx context.Context) (platform string, apps []Listing, err error)
}

// Launcher asks the host to start one app. It is given only an entry the
// service just listed, runs a fixed native program without a shell, and returns
// ErrNoDesktopSession, ErrProviderMissing, ErrGone, ErrNotLaunchable or
// ErrStartFailed for the expected failures.
type Launcher interface {
	Launch(ctx context.Context, app Listing) error
}

// Auditor records an event without failing the caller; *audit.Log satisfies it.
type Auditor interface {
	Try(ev audit.Event)
}

// Config configures a Service. A zero field takes its default; a nil Lister or
// Launcher takes the host platform's own.
type Config struct {
	Lister   Lister
	Launcher Launcher
	Audit    Auditor // nil: launches are not recorded
	Now      func() time.Time

	CatalogAttempts int
	CatalogWindow   time.Duration
	LaunchAttempts  int
	LaunchWindow    time.Duration
	LimiterDevices  int
	LaunchTimeout   time.Duration
}

// Service lists and launches host apps for authorized callers.
type Service struct {
	cfg       Config
	catalog   *limiter
	launches  *limiter
	launching chan struct{} // one slot: only one native launch at a time
}

// New returns a Service, filling unset Config fields.
func New(cfg Config) *Service {
	if cfg.Lister == nil || cfg.Launcher == nil {
		l, launcher := platformProviders()
		if cfg.Lister == nil {
			cfg.Lister = l
		}
		if cfg.Launcher == nil {
			cfg.Launcher = launcher
		}
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.CatalogAttempts <= 0 {
		cfg.CatalogAttempts = DefaultCatalogAttempts
	}
	if cfg.CatalogWindow <= 0 {
		cfg.CatalogWindow = DefaultCatalogWindow
	}
	if cfg.LaunchAttempts <= 0 {
		cfg.LaunchAttempts = DefaultLaunchAttempts
	}
	if cfg.LaunchWindow <= 0 {
		cfg.LaunchWindow = DefaultLaunchWindow
	}
	if cfg.LimiterDevices <= 0 {
		cfg.LimiterDevices = DefaultLimiterDevices
	}
	if cfg.LaunchTimeout <= 0 {
		cfg.LaunchTimeout = DefaultLaunchTimeout
	}
	return &Service{
		cfg:       cfg,
		catalog:   newLimiter(cfg.CatalogAttempts, cfg.CatalogWindow, cfg.LimiterDevices),
		launches:  newLimiter(cfg.LaunchAttempts, cfg.LaunchWindow, cfg.LimiterDevices),
		launching: make(chan struct{}, 1),
	}
}

// List returns the host's app catalog for caller p. It needs ViewApps (an admin
// login does not bypass it), counts against the caller's catalog budget, and
// reports LaunchAllowed so a view-only device can hide launch controls.
func (s *Service) List(ctx context.Context, p authz.Principal, pol authz.Policy) (Catalog, error) {
	if d := authz.Decide(p, authz.ActionViewApps, authz.Target{}, pol); !d.Allowed {
		return Catalog{}, denied(d, CodeViewDenied)
	}
	if !s.catalog.allow(p.DeviceID, s.cfg.Now()) {
		return Catalog{}, ErrCatalogRateLimited
	}
	platform, listings, err := s.cfg.Lister.List(ctx)
	if err != nil {
		return Catalog{}, listError(err)
	}
	apps := make([]App, 0, len(listings))
	for _, l := range normalize(listings) {
		apps = append(apps, l.App)
	}
	launch := authz.Decide(p, authz.ActionLaunchApps, authz.Target{}, pol)
	return Catalog{Platform: platform, LaunchAllowed: launch.Allowed, Apps: apps}, nil
}

// Launch starts the app with the opaque id rawID for caller p. The order is:
// both app grants, id shape, the caller's launch budget, a fresh listing in
// which the id must still exist and be launchable, then one native launch at a
// time. Every attempt after the grant check is written to the audit trail with
// the opaque id and a fixed outcome label, never the host reference.
func (s *Service) Launch(ctx context.Context, p authz.Principal, pol authz.Policy, rawID string) error {
	id, valid := canonicalID(rawID)
	if d := authz.Decide(p, authz.ActionLaunchApps, authz.Target{}, pol); !d.Allowed {
		s.record(p, auditTarget(rawID, valid), "", outcomeDenied)
		return denied(d, CodeLaunchDenied)
	}
	if !valid {
		s.record(p, auditTarget(rawID, false), "", outcomeInvalidID)
		return ErrBadAppID
	}
	if !s.launches.allow(p.DeviceID, s.cfg.Now()) {
		s.record(p, id, "", outcomeRateLimited)
		return ErrLaunchRateLimited
	}

	_, listings, err := s.cfg.Lister.List(ctx)
	if err != nil {
		outcome := outcomeCatalogError
		switch {
		case errors.Is(err, ErrProviderUnsupported):
			outcome = outcomeUnsupported
		case errors.Is(err, ErrProviderMissing):
			outcome = outcomeUnavailable
		}
		s.record(p, id, "", outcome)
		return listError(err)
	}
	var selected *Listing
	found := normalize(listings)
	for i := range found {
		if found[i].ID == id {
			selected = &found[i]
			break
		}
	}
	if selected == nil {
		s.record(p, id, "", outcomeNotFound)
		return ErrAppNotFound
	}
	if !selected.Launchable {
		s.record(p, id, selected.Name, outcomeNotLaunchable)
		return ErrAppNotLaunchable
	}

	select {
	case s.launching <- struct{}{}:
		defer func() { <-s.launching }()
	default:
		s.record(p, id, selected.Name, outcomeBusy)
		return ErrLaunchBusy
	}

	launchCtx, cancel := context.WithTimeout(ctx, s.cfg.LaunchTimeout)
	defer cancel()
	if err := s.cfg.Launcher.Launch(launchCtx, *selected); err != nil {
		var outcome string
		var out error
		switch {
		case errors.Is(err, ErrGone):
			outcome, out = outcomeNotFound, ErrAppNotFound
		case errors.Is(err, ErrNotLaunchable):
			outcome, out = outcomeNotLaunchable, ErrAppNotLaunchable
		case errors.Is(err, ErrNoDesktopSession):
			outcome, out = outcomeNoSession, ErrNoInteractiveUser
		case errors.Is(err, ErrProviderMissing):
			outcome, out = outcomeUnavailable, ErrLauncherUnavailable
		default:
			outcome, out = outcomeFailed, ErrLaunchFailed
		}
		s.record(p, id, selected.Name, outcome)
		return out
	}
	s.record(p, id, selected.Name, outcomeStarted)
	return nil
}

// denied turns a refused authz decision into the error to return. A missing
// grant becomes an *Error with the given code (it still matches
// authz.ErrCapabilityDenied through Unwrap); any other refusal (no device,
// revoked, read-only) is returned as authz produced it.
func denied(d authz.Decision, code string) error {
	if errors.Is(d.Err, authz.ErrCapabilityDenied) {
		return &Error{Code: code, Status: http.StatusForbidden, Message: d.Err.Error(), Cause: d.Err}
	}
	return d.Err
}

// listError maps a provider failure to the client error. An unexpected failure
// is returned wrapped so the API layer reports it as internal and logs it.
func listError(err error) error {
	switch {
	case errors.Is(err, ErrProviderUnsupported):
		return ErrCatalogUnsupported
	case errors.Is(err, ErrProviderMissing):
		return ErrCatalogUnavailable
	}
	return fmt.Errorf("apps: list host apps: %w", err)
}

// appIDPattern is the openapi shape of an app id.
var appIDPattern = regexp.MustCompile("^" + idPrefix + "[0-9a-f]{" + strconv.Itoa(idHexDigits) + "}$")

// canonicalID returns raw and true when it has the shape of an app id.
func canonicalID(raw string) (string, bool) {
	if !appIDPattern.MatchString(raw) {
		return "", false
	}
	return raw, true
}

// auditTarget is what an audit entry stores for a route id: the id itself when
// it is well formed, otherwise a short digest, so caller text (a path, a
// command, a forged log line) is never retained.
func auditTarget(raw string, valid bool) string {
	if valid {
		return raw
	}
	sum := sha256.Sum256([]byte(raw))
	return invalidIDPrefix + hex.EncodeToString(sum[:invalidIDDigest])
}

// record writes one app_launch audit entry; it never fails the launch. The
// detail holds only the sanitized display name and a fixed outcome label.
func (s *Service) record(p authz.Principal, target, name, outcome string) {
	if s.cfg.Audit == nil {
		return
	}
	detail := "outcome=" + outcome
	if name != "" {
		detail = fmt.Sprintf("name=%q %s", name, detail)
	}
	s.cfg.Audit.Try(audit.Event{Action: audit.ActionAppLaunch, Actor: p.DeviceID, Target: target, Detail: detail})
}
