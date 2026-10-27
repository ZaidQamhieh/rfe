// Package server — auth middleware.
package server

import (
	"context"
	"net"
	"net/http"
	"net/url"
	"strings"

	"github.com/ZaidQamhieh/rfe/agent/internal/fsops"
	"github.com/ZaidQamhieh/rfe/agent/internal/store"
)

type contextKey string

const deviceCtxKey contextKey = "device"

// opsCtxKey holds the per-request *fsops.Ops (see deviceJailMiddleware),
// already narrowed to the calling device's jailRoot when it has one.
const opsCtxKey contextKey = "ops"

// webSessionCookie is used only by the embedded browser companion. Native
// clients continue to authenticate with Authorization: Bearer tokens.
const webSessionCookie = "rfe_session"

// webSessionHeader marks same-origin requests made by the embedded browser
// companion. A cross-origin HTML form cannot add this header, and the server
// does not enable CORS for cross-origin JavaScript requests.
const webSessionHeader = "X-RFE-Web-Session"

func isWebSessionRequest(r *http.Request) bool {
	return r.Header.Get(webSessionHeader) == "1"
}

func setWebSessionCookie(w http.ResponseWriter, token string) {
	http.SetCookie(w, &http.Cookie{
		Name: webSessionCookie, Value: token, Path: "/v1",
		HttpOnly: true, Secure: true, SameSite: http.SameSiteStrictMode,
	})
	w.Header().Set("Cache-Control", "no-store")
}

func clearWebSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name: webSessionCookie, Value: "", Path: "/v1",
		HttpOnly: true, Secure: true, SameSite: http.SameSiteStrictMode, MaxAge: -1,
	})
	w.Header().Set("Cache-Control", "no-store")
}

// validWebSessionRequest adds a same-origin check for cookie-authenticated
// browser requests. Sec-Fetch-Site covers requests without an Origin header;
// when Origin is present it must match the HTTPS host serving the agent.
func validWebSessionRequest(r *http.Request) bool {
	if !isWebSessionRequest(r) {
		return false
	}
	if site := r.Header.Get("Sec-Fetch-Site"); site != "" && site != "same-origin" {
		return false
	}
	if origin := r.Header.Get("Origin"); origin != "" {
		u, err := url.Parse(origin)
		if err != nil || u.Scheme != "https" || !strings.EqualFold(u.Host, r.Host) || u.User != nil || (u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.Fragment != "" {
			return false
		}
	}
	return true
}

// authMiddleware validates a Bearer token or same-origin web-session cookie
// against the device store. Returns 401 if missing/invalid/revoked.
func authMiddleware(db *store.DB) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			hdr := r.Header.Get("Authorization")
			var token string
			if hdr != "" {
				parts := strings.SplitN(hdr, " ", 2)
				if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
					writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "invalid Authorization header format")
					return
				}
				token = strings.TrimSpace(parts[1])
			} else if cookie, err := r.Cookie(webSessionCookie); err == nil {
				if !validWebSessionRequest(r) {
					writeError(w, http.StatusForbidden, "WEB_SESSION_ORIGIN", "browser session requires a same-origin request")
					return
				}
				token = cookie.Value
			} else {
				writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "missing Authorization header")
				return
			}
			device, err := db.DeviceByToken(token)
			if err != nil {
				writeInternal(w, "auth middleware", err)
				return
			}
			if device == nil || device.Revoked {
				writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "invalid or revoked token")
				return
			}
			addr := r.RemoteAddr
			if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
				addr = host
			}
			ver := r.Header.Get("X-RFE-Client-Version")
			_ = db.TouchDevice(device.ID, addr, ver)

			ctx := context.WithValue(r.Context(), deviceCtxKey, device)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// deviceJailMiddleware reads the *store.Device placed in context by
// authMiddleware and, when it has a non-empty JailRoot (H2 per-device
// jail), narrows baseOps to that subtree via Ops.Jailed and injects the
// result into the request context under opsCtxKey. Handlers retrieve it via
// opsFromContext, which falls back to baseOps for devices with no jailRoot
// (and for any request that — for whatever reason — has no device in
// context), preserving today's behavior unchanged.
//
// This must run AFTER authMiddleware in the middleware chain (it depends on
// deviceCtxKey being populated), and BEFORE any handler that resolves paths.
func deviceJailMiddleware(baseOps *fsops.Ops) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ops := baseOps
			if device, ok := r.Context().Value(deviceCtxKey).(*store.Device); ok && device != nil {
				if device.JailRoot != "" {
					ops = ops.Jailed(device.JailRoot)
				}
				// Per-device read-only (#8): force the request's ops read-only
				// so every fs write returns ErrReadOnly (→ 403 READ_ONLY),
				// while reads/downloads still work. Composes after Jailed.
				if device.ReadOnly {
					ops = ops.ReadOnly()
				}
			}
			ctx := context.WithValue(r.Context(), opsCtxKey, ops)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

type fileCapability string

const (
	capBrowse   fileCapability = "browse"
	capDownload fileCapability = "download"
	capUpload   fileCapability = "upload"
	capModify   fileCapability = "modify"
	capDelete   fileCapability = "delete"
	capShare    fileCapability = "share"
)

// requireFileCapabilities enforces per-device file-action grants after auth.
// Login/register devices are the owner and retain full file access; the
// global read-only setting and filesystem roots are still enforced by Ops.
func requireFileCapabilities(required ...fileCapability) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !requireDeviceFileCapabilities(w, r, required...) {
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// requireDeviceFileCapabilities applies route capability checks from a
// handler as well as middleware. Upload handlers use this to require modify
// permission for replacement writes and overwrite sessions.
func requireDeviceFileCapabilities(w http.ResponseWriter, r *http.Request, required ...fileCapability) bool {
	device := deviceFromContext(r)
	if device == nil {
		writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "device required")
		return false
	}
	if device.ViaLogin {
		return true
	}
	for _, capability := range required {
		if !deviceHasCapability(device, capability) {
			writeError(w, http.StatusForbidden, "CAPABILITY_DENIED", "device lacks "+string(capability)+" permission")
			return false
		}
	}
	return true
}

func deviceHasCapability(device *store.Device, capability fileCapability) bool {
	if device == nil {
		return false
	}
	switch capability {
	case capBrowse:
		return device.CanBrowse
	case capDownload:
		return device.CanDownload
	case capUpload:
		return device.CanUpload
	case capModify:
		return device.CanModify
	case capDelete:
		return device.CanDelete
	case capShare:
		return device.CanShare
	default:
		return false
	}
}

// opsFromContext returns the per-request *fsops.Ops injected by
// deviceJailMiddleware, or baseOps if the context has none (e.g. in unit
// tests that call handlers directly without the middleware chain).
func opsFromContext(ctx context.Context, baseOps *fsops.Ops) *fsops.Ops {
	if ops, ok := ctx.Value(opsCtxKey).(*fsops.Ops); ok && ops != nil {
		return ops
	}
	return baseOps
}
