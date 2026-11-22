// Package api holds the HTTP-facing tables of the new agent: the one route
// table that drives authentication and authorization, and the map from domain
// errors to HTTP status and error code. Handlers live in server.go and handlers_*.go.
package api

import (
	"net/http"

	"github.com/ZaidQamhieh/rfe/agent/internal/next/authz"
)

// PathPrefix is the URL prefix every route sits under; protocol/openapi.yaml
// lists paths relative to it (its server URL ends in /v1).
const PathPrefix = "/v1"

// Tier says what a route demands before its handler runs.
type Tier int

const (
	// TierPublic needs no credential.
	TierPublic Tier = iota
	// TierDevice needs a valid, non-revoked device token and the route's Action.
	TierDevice
	// TierAdmin needs an admin device (one that signed in with the account password).
	TierAdmin
)

// String returns the tier's name.
func (t Tier) String() string {
	switch t {
	case TierPublic:
		return "public"
	case TierDevice:
		return "device"
	case TierAdmin:
		return "admin"
	}
	return "tier(?)"
}

// Route is one entry of the route table.
type Route struct {
	Method  string // upper-case HTTP method
	Pattern string // path relative to PathPrefix, {name} for parameters
	Tier    Tier
	// Action is what authz.Decide must allow before the handler runs. Public
	// routes use ActionNone; admin routes use ActionAdmin.
	Action authz.Action
}

// Writes reports whether the route changes files, so read-only mode refuses it.
func (r Route) Writes() bool { return r.Action.Writes() }

// handlerDecidesRoutes are the routes whose handler makes the authorization
// decision itself, so it can record the refusal: the guard only authenticates.
var handlerDecidesRoutes = map[string]bool{
	routeKey(http.MethodPost, "/apps/{id}/launch"): true, // a refused launch is audited
}

// HandlerDecides reports whether the guard leaves the route's Action to the handler.
func (r Route) HandlerDecides() bool { return handlerDecidesRoutes[routeKey(r.Method, r.Pattern)] }

// FullPattern is the pattern with PathPrefix, as the router registers it.
func (r Route) FullPattern() string { return PathPrefix + r.Pattern }

// shorthand for the table below.
const (
	get    = http.MethodGet
	post   = http.MethodPost
	put    = http.MethodPut
	patch  = http.MethodPatch
	del    = http.MethodDelete
	none   = authz.ActionNone
	public = TierPublic
	device = TierDevice
	admin  = TierAdmin
)

// Routes lists every route of the old agent (agent/internal/server/server.go),
// in registration order. The file and line of each group is in its comment.
// A test keeps this list equal to the operations in protocol/openapi.yaml.
var Routes = []Route{
	// Reachable without a token: server.go:144-155 (registerUnauthRoutes).
	{get, "/health", public, none},
	{post, "/auth/challenge", public, none},
	{post, "/auth/logout", public, none},
	{post, "/pair", public, none},
	{post, "/pair/request", public, none},
	{post, "/pair/request/{id}/reveal", public, none},
	{get, "/pair/request/{id}", public, none},
	{post, "/register", public, none},
	{post, "/login", public, none},
	{get, "/share/{token}", public, none},
	// Optional account server (rfe-7np). Served by the old agent; the rewrite answers 501 until it ports them.
	{get, "/accounts/key", public, none},
	{post, "/accounts/assertion", admin, authz.ActionAdmin},
	{get, "/settings/account-server", admin, authz.ActionAdmin},
	{put, "/settings/account-server", admin, authz.ActionAdmin},
	{del, "/settings/account-server", admin, authz.ActionAdmin},
	// Push channel (rfe-ahy). Served by the old agent; the rewrite answers 501 until it ports it.
	{get, "/events", device, authz.ActionBrowse},

	// Authenticated, no grants: server.go:78-85.
	{get, "/status", device, none},
	{get, "/transfers/list", device, none},
	{del, "/transfers/{id}", device, none}, // owner or admin, decided on the record

	// App catalog and launch: server.go:90-94; grants in apps_handlers.go:55,90.
	{get, "/apps", device, authz.ActionViewApps},
	{post, "/apps/{id}/launch", device, authz.ActionLaunchApps},

	// Admin-only control plane: server.go:100-109 (adminOnly middleware).
	{get, "/metrics", admin, authz.ActionAdmin},
	{get, "/users", admin, authz.ActionAdmin},
	{del, "/users/{username}", admin, authz.ActionAdmin},
	{get, "/logs", admin, authz.ActionAdmin},
	{get, "/audit", admin, authz.ActionAdmin},
	{get, "/system/backups", admin, authz.ActionAdmin},
	{post, "/agent/restart", admin, authz.ActionAdmin},

	// Settings and device management: server.go:160-181. The old handlers check
	// admin themselves: settings_handlers.go:166,211,225,382, pair.go:180,
	// pair_request.go:307. GET /settings and GET /devices answer a reduced view to
	// a non-admin, so they stay on the device tier.
	{get, "/settings", device, none},
	{patch, "/settings", admin, authz.ActionAdmin},
	{get, "/settings/bandwidth", admin, authz.ActionAdmin},
	{put, "/settings/bandwidth", admin, authz.ActionAdmin},
	{get, "/devices", device, none},
	{patch, "/devices/{id}", admin, authz.ActionAdmin},
	{del, "/devices/{id}", device, authz.ActionSelfOrAdmin}, // settings_handlers.go:329,347
	{post, "/wol", device, authz.ActionWakeOnLAN},
	{post, "/pairing/generate", admin, authz.ActionAdmin},
	{get, "/pair/requests", admin, authz.ActionAdmin},
	{post, "/pair/requests/{id}/approve", admin, authz.ActionAdmin},
	{post, "/pair/requests/{id}/reject", admin, authz.ActionAdmin},

	// Share links: server.go:187-191. Listing and revoking are scoped to the
	// caller inside the handler (share_handlers.go:367-380, :348).
	{post, "/share/mint", device, authz.ActionMintShare},
	{del, "/share/{tokenHash}", device, none},
	{get, "/share", device, none},

	// Browse-class reads outside /fs: server.go:121-123.
	{get, "/system/drives", device, authz.ActionBrowse},
	{get, "/search", device, authz.ActionBrowse},
	{get, "/thumb", device, authz.ActionDownload},

	// In-app updater: server.go:194-197 (no grant).
	{get, "/app/latest", device, none},
	{get, "/app/download", device, none},

	// Filesystem: server.go:200-217.
	{get, "/fs", device, authz.ActionBrowse},
	{del, "/fs", device, authz.ActionDelete},
	{post, "/fs/folder", device, authz.ActionModify},
	{post, "/fs/file", device, authz.ActionModify},
	{patch, "/fs/rename", device, authz.ActionModify},
	{post, "/fs/copy", device, authz.ActionModify}, // overwrite also needs ActionCopyOverwrite
	{post, "/fs/move", device, authz.ActionMove},
	{post, "/fs/compress", device, authz.ActionModify},
	{post, "/fs/extract", device, authz.ActionModify},
	{get, "/fs/meta", device, authz.ActionBrowse},
	{get, "/fs/media-info", device, authz.ActionBrowse},
	{get, "/fs/checksum", device, authz.ActionDownload},
	{post, "/fs/chmod", device, authz.ActionModify},
	{get, "/fs/archive", device, authz.ActionBrowse},
	{post, "/fs/checksums", device, authz.ActionDownload},
	{get, "/fs/recent", device, authz.ActionBrowse},
	{get, "/fs/usage", device, authz.ActionBrowse},

	// Trash: server.go:220-224.
	{get, "/trash", device, authz.ActionBrowse},
	{post, "/trash/restore", device, authz.ActionModify},
	{del, "/trash", device, authz.ActionDelete},

	// Whole-file content: server.go:228-232 (a write replaces a file: modify).
	{get, "/content", device, authz.ActionDownload},
	{put, "/content", device, authz.ActionModify},

	// Transcoded HLS video (ffmpeg): server.go registerContentRoutes.
	{get, "/stream", device, authz.ActionDownload},

	// Resumable uploads: server.go:238-247. Status is open to the caller and
	// narrowed to the session's owner in the handler; the rest need upload and
	// are refused under read-only. An overwrite session also needs
	// ActionUploadOverwrite (transferhandlers.go:231,360,490).
	{get, "/transfers/{id}", device, none},
	{post, "/transfers", device, authz.ActionUpload},
	{put, "/transfers/{id}/chunks/{n}", device, authz.ActionUpload},
	{post, "/transfers/{id}/complete", device, authz.ActionUpload},
}
