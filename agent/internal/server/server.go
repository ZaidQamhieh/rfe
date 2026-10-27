// Package server wires the agent's HTTP routes and middleware.
package server

import (
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"runtime"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/ZaidQamhieh/rfe/agent/internal/fsops"
	"github.com/ZaidQamhieh/rfe/agent/internal/pairing"
	"github.com/ZaidQamhieh/rfe/agent/internal/settings"
	"github.com/ZaidQamhieh/rfe/agent/internal/store"
	"github.com/ZaidQamhieh/rfe/agent/internal/thumbs"
	"github.com/ZaidQamhieh/rfe/agent/internal/transfer"
	"github.com/ZaidQamhieh/rfe/agent/internal/webui"
)

// Config holds the runtime settings the server needs.
type Config struct {
	Name             string
	Version          string
	CertFingerprint  string
	Address          string // LAN address used in QR payload / health response
	TailscaleAddress string // Tailscale address, if detected
	MACAddress       string // MAC address of the LAN interface (for Wake-on-LAN)
	ThumbCacheDir    string // directory for on-disk thumbnail cache
	Settings         *settings.Store
	UpdatesDir       string    // directory of downloadable APKs for in-app update
	TrashDir         string    // trash store root (XDG Trash on Linux; managed dir elsewhere)
	StartTime        time.Time // agent process start time (for uptime reporting)
	DataDir          string    // data directory path (for disk-space reporting)
	// OnPairRequest, when set, is told about each new approve-on-PC pairing request (desktop notification).
	OnPairRequest func(PairPrompt)
}

// New builds the v1 router and wires all routes.
func New(cfg Config, db *store.DB, pm *pairing.Manager, tm *transfer.Manager) (http.Handler, error) {
	ops := fsops.NewWithSettings(cfg.Settings)
	searchIndex := newIndexBackend(ops)

	thumbRenderer, err := thumbs.New(cfg.ThumbCacheDir)
	if err != nil {
		return nil, err
	}
	useThumbSidecar(thumbRenderer)

	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	// Keep RemoteAddr as the socket peer. There is no configured trusted proxy,
	// so RealIP would let callers forge the source address with forwarded headers.
	r.Use(middleware.Recoverer)

	nonces := newNonceStore()

	r.Route("/v1", func(r chi.Router) {
		// API responses often contain private file or host metadata. Do not let
		// browsers or intermediate caches retain them; the thumbnail handler
		// explicitly opts into its existing private cache policy.
		r.Use(func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				w.Header().Set("Cache-Control", "no-store")
				w.Header().Set("X-Content-Type-Options", "nosniff")
				w.Header().Set("Referrer-Policy", "no-referrer")
				next.ServeHTTP(w, req)
			})
		})
		registerUnauthRoutes(r, cfg, db, pm, ops, nonces)

		// Authenticated, no path jail (non-filesystem endpoints).
		r.Group(func(r chi.Router) {
			r.Use(authMiddleware(db))
			r.Get("/status", statusHandler(cfg))
			// Transfer history is scoped to the caller inside the handlers:
			// a non-admin sees only its own sessions (PR-03).
			r.Get("/transfers/list", listTransfersHandler(db))
			r.Delete("/transfers/{id}", deleteTransferHandler(db, tm))
		})

		// Host app catalog and launch are authenticated separately from the
		// filesystem jail. Both handlers enforce the caller's explicit app
		// capabilities; ViaLogin/admin provenance grants no implicit access.
		r.Group(func(r chi.Router) {
			r.Use(authMiddleware(db))
			r.Get("/apps", listAppsHandler())
			r.Post("/apps/{id}/launch", launchAppHandler(db))
		})

		// Admin-only control plane: whole-host telemetry, login-account
		// management, the agent's journal, and process restart. These expose
		// or mutate state belonging to every device, so an ordinary paired
		// device (phone app, guest) must not reach them — PR-03.
		r.Group(func(r chi.Router) {
			r.Use(authMiddleware(db))
			r.Use(adminOnly)
			r.Get("/metrics", metricsHandler())
			r.Get("/users", listUsersHandler(db))
			r.Delete("/users/{username}", deleteUserHandler(db))
			r.Get("/logs", listLogsHandler())
			r.Get("/audit", listAuditHandler(db))
			r.Post("/agent/restart", restartHandler(db))
		})

		// Authenticated sub-router.
		r.Group(func(r chi.Router) {
			r.Use(authMiddleware(db))
			// Narrows the shared ops to the calling device's jailRoot (H2),
			// if it has one. Must run after authMiddleware (needs the device
			// in context) and before any handler that resolves paths.
			r.Use(deviceJailMiddleware(ops))

			registerSettingsAndDeviceRoutes(r, cfg, db, pm)
			registerShareRoutes(r, cfg, db, ops)
			r.With(requireFileCapabilities(capBrowse)).Get("/system/drives", drivesHandler(ops))
			r.With(requireFileCapabilities(capBrowse)).Get("/search", searchHandler(ops, searchIndex))
			r.With(requireFileCapabilities(capDownload)).Get("/thumb", thumbHandler(ops, thumbRenderer))
			registerUpdateRoutes(r, cfg)
			registerFsRoutes(r, cfg, ops, searchIndex)
			registerTrashRoutes(r, cfg, ops)
			registerContentRoutes(r, cfg, ops)
			registerTransferRoutes(r, tm, cfg, ops)
		})
	})

	// Web companion: browser-based agent control/status/settings UI, served
	// at the root (distinct from the /v1 API tree — chi matches the more
	// specific /v1 prefix first regardless of registration order).
	r.Mount("/", webui.Handler())

	return r, nil
}

// registerUnauthRoutes wires the routes reachable without a bearer token:
// health, pairing, registration, login, the device-identity challenge,
// browser-session logout, and the single-use share-link fetch (rate-limited
// and expiring — see docs/r1-share-link-threat-model.md).
func registerUnauthRoutes(r chi.Router, cfg Config, db *store.DB, pm *pairing.Manager, ops *fsops.Ops, nonces *nonceStore) {
	r.Get("/health", healthHandler(cfg, db))
	r.Post("/auth/challenge", challengeHandler(nonces, cfg.CertFingerprint))
	r.Post("/auth/logout", logoutHandler(db))
	r.Post("/pair", pairHandler(cfg, db, pm, nonces))
	r.Post("/pair/request", createPairRequestHandler(cfg, db, nonces))
	r.Post("/pair/request/{id}/reveal", revealPairRequestHandler(cfg, db))
	r.Get("/pair/request/{id}", pollPairRequestHandler(cfg, db))
	r.Post("/register", registerHandler(cfg, db, pm, nonces))
	r.Post("/login", loginHandler(cfg, db, nonces))
	r.Get("/share/{token}", serveShareHandler(db, ops, cfg.Settings.IsAllowSharing))
}

// registerSettingsAndDeviceRoutes wires agent settings, bandwidth limits, and
// paired-device management (list/jail/readonly/revoke/purge/pairing-code
// generation).
func registerSettingsAndDeviceRoutes(r chi.Router, cfg Config, db *store.DB, pm *pairing.Manager) {
	r.Get("/settings", getSettingsHandler(cfg.Settings))
	r.Patch("/settings", patchSettingsHandler(cfg.Settings))
	r.Get("/settings/bandwidth", getBandwidthHandler(cfg.Settings))
	r.Put("/settings/bandwidth", putBandwidthHandler(cfg.Settings))
	r.Get("/devices", listDevicesHandler(db))
	r.Patch("/devices/{id}", setDeviceJailHandler(db, cfg.Settings))
	r.Delete("/devices/{id}", func(w http.ResponseWriter, req *http.Request) {
		id := chi.URLParam(req, "id")
		// ?purge=true permanently removes the row (used to clear revoked
		// devices); otherwise the device is revoked.
		if req.URL.Query().Get("purge") == "true" {
			deleteDeviceHandler(db)(w, req, id)
			return
		}
		revokeDeviceHandler(db)(w, req, id)
	})
	r.Post("/wol", wolRelayHandler(db))
	r.Post("/pairing/generate", generatePairingHandler(pm, cfg.Settings))
	r.Get("/pair/requests", listPairRequestsHandler(cfg, db))
	r.Post("/pair/requests/{id}/approve", decidePairRequestHandler(db, true))
	r.Post("/pair/requests/{id}/reject", decidePairRequestHandler(db, false))
}

// registerShareRoutes wires the authenticated R1 share-link management
// endpoints (mint/revoke/list — serving the file itself is unauthenticated,
// see registerUnauthRoutes).
func registerShareRoutes(r chi.Router, cfg Config, db *store.DB, ops *fsops.Ops) {
	r.With(requireFileCapabilities(capShare, capBrowse, capDownload)).Post("/share/mint", mintShareHandler(cfg, db, ops))
	r.Delete("/share/{tokenHash}", revokeShareHandler(db))
	r.Get("/share", listSharesHandler(db))
}

// registerUpdateRoutes wires the in-app updater's APK metadata/download.
func registerUpdateRoutes(r chi.Router, cfg Config) {
	r.Get("/app/latest", latestAppHandler(cfg.UpdatesDir))
	r.Get("/app/download", downloadAppHandler(cfg.UpdatesDir))
}

// registerFsRoutes wires the filesystem CRUD/browse endpoints.
func registerFsRoutes(r chi.Router, cfg Config, ops *fsops.Ops, recents indexBackend) {
	r.With(requireFileCapabilities(capBrowse)).Get("/fs", listDirHandler(ops))
	r.With(requireFileCapabilities(capDelete)).Delete("/fs", deleteHandler(ops, cfg.TrashDir))
	r.With(requireFileCapabilities(capModify)).Post("/fs/folder", createFolderHandler(ops))
	r.With(requireFileCapabilities(capModify)).Post("/fs/file", createFileHandler(ops))
	r.With(requireFileCapabilities(capModify)).Patch("/fs/rename", renameHandler(ops))
	r.With(requireFileCapabilities(capModify)).Post("/fs/copy", copyHandler(ops))
	r.With(requireFileCapabilities(capModify, capDelete)).Post("/fs/move", moveHandler(ops))
	r.With(requireFileCapabilities(capModify)).Post("/fs/compress", compressHandler(ops))
	r.With(requireFileCapabilities(capModify)).Post("/fs/extract", extractHandler(ops))
	r.With(requireFileCapabilities(capBrowse)).Get("/fs/meta", metaHandler(ops))
	r.With(requireFileCapabilities(capDownload)).Get("/fs/checksum", checksumHandler(ops))
	r.With(requireFileCapabilities(capModify)).Post("/fs/chmod", chmodHandler(ops))
	r.With(requireFileCapabilities(capBrowse)).Get("/fs/archive", archivePeekHandler(ops))
	r.With(requireFileCapabilities(capDownload)).Post("/fs/checksums", batchChecksumHandler(ops))
	r.With(requireFileCapabilities(capBrowse)).Get("/fs/recent", recentHandlerWithWalker(ops, recentWalker(recents)))
}

// registerTrashRoutes wires the trash list/restore/empty endpoints.
func registerTrashRoutes(r chi.Router, cfg Config, ops *fsops.Ops) {
	r.With(requireFileCapabilities(capBrowse)).Get("/trash", listTrashHandler(ops, cfg.TrashDir))
	r.With(requireFileCapabilities(capModify)).Post("/trash/restore", restoreTrashHandler(ops, cfg.TrashDir))
	r.With(requireFileCapabilities(capDelete)).Delete("/trash", emptyTrashHandler(ops, cfg.TrashDir))
}

// registerContentRoutes wires whole-file download/write (as opposed to the
// chunked transfer endpoints in registerTransferRoutes).
func registerContentRoutes(r chi.Router, cfg Config, ops *fsops.Ops) {
	r.With(requireFileCapabilities(capDownload)).Get("/content", downloadHandler(ops, cfg.Settings))
	// Replacing an existing text file is a modification, not an upload.
	r.With(requireFileCapabilities(capModify)).Put("/content", writeContentHandler(ops))
}

// registerTransferRoutes wires the resumable chunked upload session
// endpoints. Every route that writes to the filesystem sits behind
// requireWritable — the resumable upload path used to be the one way around
// the read-only policy fsops.Ops enforces everywhere else (PR-04).
func registerTransferRoutes(r chi.Router, tm *transfer.Manager, cfg Config, ops *fsops.Ops) {
	r.Get("/transfers/{id}", transferStatusHandler(tm))
	r.Group(func(r chi.Router) {
		r.Use(requireFileCapabilities(capUpload))
		r.Use(requireWritable(ops))
		r.Post("/transfers", openTransferHandler(tm, ops))
		r.Put("/transfers/{id}/chunks/{n}", uploadChunkHandler(tm, cfg.Settings))
		r.Post("/transfers/{id}/complete", completeTransferHandler(tm, ops))
	})
}

// requireWritable rejects mutating requests when the agent is globally
// read-only or the calling device is marked read-only. It reads the
// device-narrowed ops the jail middleware installed, so the per-device flag
// counts as much as the global one. Must run after deviceJailMiddleware.
func requireWritable(base *fsops.Ops) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if opsFromContext(r.Context(), base).IsReadOnly() {
				writeError(w, http.StatusForbidden, "READ_ONLY", fsops.ErrReadOnly.Error())
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// --------- helpers ---------

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

type apiError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, apiError{Code: code, Message: message})
}

// writeInternal logs the real error server-side and returns a generic 500 to
// the client, so absolute paths, DB details, and OS messages don't leak into
// responses (and, via the web companion, into an HTML sink) — PR-53.
func writeInternal(w http.ResponseWriter, context string, err error) {
	log.Printf("%s: %v", context, err)
	writeError(w, http.StatusInternalServerError, "INTERNAL", "internal error")
}

// maxJSONBody bounds JSON request bodies (PR-51). Streaming routes (chunk
// upload, content write) set their own, larger limits.
const maxJSONBody = 1 << 20 // 1 MiB

// decodeJSONBody reads exactly one JSON document from r into dst under a size
// cap. Returns false (after writing 400/413) on malformed, oversized, or
// multi-document bodies. It does not reject unknown fields, to stay compatible
// with older clients; tightening that is a follow-up.
func decodeJSONBody(w http.ResponseWriter, r *http.Request, dst any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, maxJSONBody)
	dec := json.NewDecoder(r.Body)
	if err := dec.Decode(dst); err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			writeError(w, http.StatusRequestEntityTooLarge, "PAYLOAD_TOO_LARGE", "request body too large")
			return false
		}
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid JSON body")
		return false
	}
	// More is meaningful only while iterating an array or object; after a
	// top-level value it does not detect a second JSON document. Decode once
	// more and require EOF so ambiguous bodies cannot be interpreted differently
	// by intermediaries or future handler code.
	if err := dec.Decode(&struct{}{}); err != io.EOF {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			writeError(w, http.StatusRequestEntityTooLarge, "PAYLOAD_TOO_LARGE", "request body too large")
			return false
		}
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "unexpected trailing data")
		return false
	}
	return true
}

// --------- health ---------

// APIVersion is the protocol revision this agent speaks. A v1.42.x agent sends
// none, which clients read as 1. Bump only for a change a client must know about.
const APIVersion = 2

// Features lists the optional capabilities this agent has, sorted. Clients gate
// newer UI on these ids, never on version strings. Ids are only ever added.
var Features = []string{
	"pairRequest", // /pair/request* (phone-initiated pairing approved on the PC)
}

// healthHandler is deliberately reachable without a device credential (a phone
// probing reachability/latency shouldn't need one), but the topology detail
// below is not: name/OS/version/read-only/addresses/MAC let an unauthenticated
// caller fingerprint and map the host (PR-61). Every real caller (host-card
// ping, connection diagnostics, the weekly digest) already has a paired
// device token and sends it on every request regardless of route, so gating
// the detail on a valid token is invisible to them; an unpaired caller now
// only gets a bare liveness signal.
func healthHandler(cfg Config, db *store.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]any{"status": "ok"}
		if authorizedDevice(r, db) {
			// Surfacing the agent's known addresses lets an already-paired app
			// learn the Tailscale (or LAN) address it didn't capture at pairing
			// time, simply by reaching the agent successfully via either one.
			// macAddress is included so the app can cache it for Wake-on-LAN
			// when the host is asleep (and thus unreachable).
			resp["name"] = cfg.Settings.AgentName()
			resp["version"] = cfg.Version
			resp["apiVersion"] = APIVersion
			resp["features"] = Features
			resp["os"] = runtime.GOOS
			resp["readOnly"] = cfg.Settings.IsReadOnly()
			resp["address"] = cfg.Address
			resp["tailscaleAddress"] = cfg.TailscaleAddress
			if cfg.MACAddress != "" {
				resp["macAddress"] = cfg.MACAddress
			}
		}
		writeJSON(w, http.StatusOK, resp)
	}
}

// authorizedDevice reports whether r carries a valid bearer token or
// same-origin browser session for a known, non-revoked device, without the
// side effects of authMiddleware.
func authorizedDevice(r *http.Request, db *store.DB) bool {
	hdr := r.Header.Get("Authorization")
	var token string
	if hdr != "" {
		parts := strings.SplitN(hdr, " ", 2)
		if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
			return false
		}
		token = strings.TrimSpace(parts[1])
	} else if cookie, err := r.Cookie(webSessionCookie); err == nil && validWebSessionRequest(r) {
		token = cookie.Value
	} else {
		return false
	}
	device, err := db.DeviceByToken(token)
	return err == nil && device != nil && !device.Revoked
}
