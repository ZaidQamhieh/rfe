package server

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/ZaidQamhieh/rfe/agent/internal/store"
	"github.com/go-chi/chi/v5"
)

var (
	appIDPattern        = regexp.MustCompile(`^app_[0-9a-f]{64}$`)
	appCatalogLimiter   = newKeyedLimiter(60, time.Minute)
	appLaunchLimiter    = newKeyedLimiter(5, time.Minute)
	appLaunchSlot       = make(chan struct{}, 1)
	errAppUnsupported   = errors.New("app catalog is unsupported on this platform")
	errNoDesktopSession = errors.New("no interactive desktop session")
	errLauncherMissing  = errors.New("native app launcher unavailable")
	errAppStartFailed   = errors.New("native app launch failed")
	errAppNotFound      = errors.New("app is not in the current catalog")
	errAppNotLaunchable = errors.New("app has no supported launch action")
)

// appRecord contains only safe display data for the API. launchRef is an
// internal registration reference and is never serialized or written to audit.
type appRecord struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Icon        string `json:"icon,omitempty"`
	Category    string `json:"category,omitempty"`
	Launchable  bool   `json:"launchable"`
	launchRef   string
}

type appCatalogResponse struct {
	Platform      string      `json:"platform"`
	LaunchAllowed bool        `json:"launchAllowed"`
	Apps          []appRecord `json:"apps"`
}

func listAppsHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		device := deviceFromContext(r)
		if device == nil || !device.ViewApps {
			writeError(w, http.StatusForbidden, "APP_VIEW_FORBIDDEN", "app catalog access is not enabled for this device")
			return
		}
		if !appCatalogLimiter.Allow(device.ID) {
			writeError(w, http.StatusTooManyRequests, "APP_CATALOG_RATE_LIMITED", "too many app catalog requests; try again shortly")
			return
		}

		platform, apps, err := listHostApps()
		if err != nil {
			if err == errAppUnsupported {
				writeError(w, http.StatusNotImplemented, "APP_CATALOG_UNSUPPORTED", "app catalog is not supported on this host platform")
				return
			}
			if err == errLauncherMissing {
				writeError(w, http.StatusServiceUnavailable, "APP_LAUNCH_UNAVAILABLE", "the host app catalog provider is unavailable")
				return
			}
			writeInternal(w, "list host apps", err)
			return
		}
		writeJSON(w, http.StatusOK, appCatalogResponse{
			Platform:      platform,
			LaunchAllowed: device.LaunchApps,
			Apps:          apps,
		})
	}
}

func launchAppHandler(db *store.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rawID := chi.URLParam(r, "id")
		id, validID := canonicalAppID(rawID)
		device := deviceFromContext(r)
		if device == nil || !device.ViewApps || !device.LaunchApps {
			auditAppLaunch(db, r, auditAppID(rawID, validID), "", "permission_denied")
			writeError(w, http.StatusForbidden, "APP_LAUNCH_FORBIDDEN", "app viewing and launching are not enabled for this device")
			return
		}
		if !validID {
			auditAppLaunch(db, r, auditAppID(rawID, false), "", "invalid_id")
			writeError(w, http.StatusBadRequest, "BAD_APP_ID", "app id is invalid")
			return
		}
		if !appLaunchLimiter.Allow(device.ID) {
			auditAppLaunch(db, r, id, "", "rate_limited")
			writeError(w, http.StatusTooManyRequests, "APP_LAUNCH_RATE_LIMITED", "too many app launch attempts; try again shortly")
			return
		}

		_, apps, err := listHostApps()
		if err != nil {
			if err == errAppUnsupported {
				auditAppLaunch(db, r, id, "", "unsupported_platform")
				writeError(w, http.StatusNotImplemented, "APP_CATALOG_UNSUPPORTED", "app launching is not supported on this host platform")
				return
			}
			if err == errLauncherMissing {
				auditAppLaunch(db, r, id, "", "launcher_unavailable")
				writeError(w, http.StatusServiceUnavailable, "APP_LAUNCH_UNAVAILABLE", "the host app catalog provider is unavailable")
				return
			}
			auditAppLaunch(db, r, id, "", "catalog_error")
			writeInternal(w, "resolve app launch", err)
			return
		}
		var selected *appRecord
		for i := range apps {
			if apps[i].ID == id {
				selected = &apps[i]
				break
			}
		}
		if selected == nil {
			auditAppLaunch(db, r, id, "", "not_found")
			writeError(w, http.StatusNotFound, "APP_NOT_FOUND", "app is no longer available in the host catalog")
			return
		}
		if !selected.Launchable {
			auditAppLaunch(db, r, id, selected.Name, "not_launchable")
			writeError(w, http.StatusConflict, "APP_NOT_LAUNCHABLE", "this app has no supported launch action")
			return
		}

		select {
		case appLaunchSlot <- struct{}{}:
			defer func() { <-appLaunchSlot }()
		default:
			auditAppLaunch(db, r, id, selected.Name, "busy")
			writeError(w, http.StatusConflict, "APP_LAUNCH_BUSY", "another app launch is in progress")
			return
		}

		launchErr := launchHostApp(r.Context(), *selected)
		if launchErr != nil {
			switch launchErr {
			case errAppNotFound:
				auditAppLaunch(db, r, id, selected.Name, "not_found")
				writeError(w, http.StatusNotFound, "APP_NOT_FOUND", "app is no longer available in the host catalog")
			case errNoDesktopSession:
				auditAppLaunch(db, r, id, selected.Name, "no_interactive_session")
				writeError(w, http.StatusServiceUnavailable, "NO_INTERACTIVE_SESSION", "the host has no active graphical desktop session")
			case errLauncherMissing:
				auditAppLaunch(db, r, id, selected.Name, "launcher_unavailable")
				writeError(w, http.StatusServiceUnavailable, "APP_LAUNCH_UNAVAILABLE", "the host app launcher is unavailable")
			case errAppStartFailed:
				auditAppLaunch(db, r, id, selected.Name, "launch_failed")
				writeError(w, http.StatusBadGateway, "APP_LAUNCH_FAILED", "the host could not start this app")
			default:
				auditAppLaunch(db, r, id, selected.Name, "launch_failed")
				writeError(w, http.StatusBadGateway, "APP_LAUNCH_FAILED", "the host could not start this app")
			}
			return
		}

		auditAppLaunch(db, r, id, selected.Name, "started")
		writeJSON(w, http.StatusAccepted, map[string]string{"status": "started"})
	}
}

func canonicalAppID(raw string) (string, bool) {
	if !appIDPattern.MatchString(raw) {
		return "", false
	}
	return raw, true
}

// auditAppID never stores caller-controlled malformed route text. A digest
// provides a safe correlation key for rejected path values without retaining
// an arbitrary path, command, or log-injection payload.
func auditAppID(raw string, valid bool) string {
	if valid {
		return raw
	}
	sum := sha256.Sum256([]byte(raw))
	return "invalid_" + hex.EncodeToString(sum[:8])
}

func auditAppLaunch(db *store.DB, r *http.Request, id, name, outcome string) {
	// Each row contains only actor, opaque app ID, sanitized display name, and
	// one of this handler's fixed outcome labels.
	detail := "outcome=" + outcome
	if name != "" {
		detail = fmt.Sprintf("name=%q %s", safeAppDisplay(name), detail)
	}
	audit(db, r, store.AuditAppLaunch, id, detail)
}

func safeAppDisplay(value string) string {
	var b strings.Builder
	for _, r := range value {
		if r < 0x20 || r == 0x7f || r == '/' || r == '\\' {
			if r == '/' || r == '\\' {
				b.WriteByte(' ')
			}
			continue
		}
		if b.Len() >= 240 {
			break
		}
		b.WriteRune(r)
	}
	return strings.TrimSpace(b.String())
}
