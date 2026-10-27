// Package server — settings and device-management route handlers.
package server

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"path/filepath"

	"github.com/go-chi/chi/v5"

	"github.com/ZaidQamhieh/rfe/agent/internal/fsops"
	"github.com/ZaidQamhieh/rfe/agent/internal/settings"
	"github.com/ZaidQamhieh/rfe/agent/internal/store"
)

// withDevice injects a device into a context (test seam mirroring authMiddleware).
func withDevice(ctx context.Context, d *store.Device) context.Context {
	return context.WithValue(ctx, deviceCtxKey, d)
}

func deviceFromContext(r *http.Request) *store.Device {
	d, _ := r.Context().Value(deviceCtxKey).(*store.Device)
	return d
}

// isAdminDevice reports whether d authenticated via /login or /register
// (the single account's password) rather than /pair (a one-time code for an
// ordinary device like the phone app) — see the via_login column comment in
// store.migrate. Only admin devices may manage OTHER devices: mint pairing
// codes, and toggle another device's jail/read-only/revoked state.
func isAdminDevice(d *store.Device) bool {
	return d != nil && d.ViaLogin
}

// adminOnly rejects callers that are not admin devices (see isAdminDevice).
// Must run after authMiddleware, which puts the device in the context.
func adminOnly(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !isAdminDevice(deviceFromContext(r)) {
			writeError(w, http.StatusForbidden, "FORBIDDEN", "admin device required")
			return
		}
		next.ServeHTTP(w, r)
	})
}

type settingsBody struct {
	ReadOnly        *bool     `json:"readOnly,omitempty"`
	Roots           *[]string `json:"roots,omitempty"`
	AgentName       *string   `json:"agentName,omitempty"`
	AllowSharing    *bool     `json:"allowSharing,omitempty"`
	PhotoBackupRoot *string   `json:"photoBackupRoot,omitempty"`
}

func getSettingsHandler(st *settings.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, settingsJSON(st, r))
	}
}

// settingsJSON returns global configuration only to password-authenticated
// admin devices. For ordinary paired devices it reports the effective policy
// applied to this request, so unrelated host-wide paths and unavailable photo
// backup destinations are not disclosed.
// fileCapabilitiesJSON is what the calling device may do with files, so a client can hide actions the agent would
// refuse. Login (owner) devices retain full file access, as in requireDeviceFileCapabilities.
func fileCapabilitiesJSON(device *store.Device) map[string]bool {
	out := map[string]bool{}
	for _, c := range []fileCapability{capBrowse, capDownload, capUpload, capModify, capDelete, capShare} {
		out[string(c)] = device != nil && (device.ViaLogin || deviceHasCapability(device, c))
	}
	return out
}

func settingsJSON(st *settings.Store, r *http.Request) map[string]any {
	device := deviceFromContext(r)
	admin := isAdminDevice(device)
	// Authenticated routes install request-scoped Ops via deviceJailMiddleware.
	// Construct the same effective view as a fallback for direct handler use,
	// such as the server's unit-level handler seams.
	effectiveOps := fsops.NewWithSettings(st)
	if device != nil {
		if device.JailRoot != "" {
			effectiveOps = effectiveOps.Jailed(device.JailRoot)
		}
		if device.ReadOnly {
			effectiveOps = effectiveOps.ReadOnly()
		}
	}
	effectiveOps = opsFromContext(r.Context(), effectiveOps)
	backupRoot := st.PhotoBackupRoot()
	// This is a policy check only: it confirms that the destination is inside
	// the caller's effective roots and that writes are not blocked by policy.
	// The eventual filesystem operation can still fail due to OS permissions.
	backupAvailable := backupRoot != "" && !effectiveOps.IsReadOnly()
	if backupAvailable {
		_, err := effectiveOps.Resolve(backupRoot)
		backupAvailable = err == nil
	}
	if admin {
		return map[string]any{
			"isAdmin":               true,
			"effectiveScope":        "global",
			"accessDenied":          false,
			"readOnly":              st.IsReadOnly(),
			"roots":                 st.Roots(),
			"agentName":             st.AgentName(),
			"allowSharing":          st.IsAllowSharing(),
			"photoBackupConfigured": backupRoot != "",
			"photoBackupAvailable":  backupAvailable,
			"photoBackupRoot":       backupRoot,
			"fileCapabilities":      fileCapabilitiesJSON(device),
		}
	}

	effectiveRoots := effectiveOps.Roots()
	// Preserve the JSON array contract for denyAll; encoding a nil Go slice
	// would send `null`, which is ambiguous for generated API clients.
	if effectiveRoots == nil {
		effectiveRoots = []string{}
	}
	globalRoots := st.Roots()
	// An empty effective root list normally means unrestricted access. It
	// means no access only when this device has an explicit jail that falls
	// outside every configured host root (Ops.Jailed's denyAll case).
	accessDenied := device != nil && device.JailRoot != "" && len(globalRoots) > 0 && len(effectiveRoots) == 0
	response := map[string]any{
		"isAdmin":               false,
		"effectiveScope":        "device",
		"readOnly":              effectiveOps.IsReadOnly(),
		"roots":                 effectiveRoots,
		"accessDenied":          accessDenied,
		"agentName":             st.AgentName(),
		"allowSharing":          st.IsAllowSharing(),
		"photoBackupConfigured": backupRoot != "",
		"photoBackupAvailable":  backupAvailable,
		"fileCapabilities":      fileCapabilitiesJSON(device),
	}
	if backupAvailable {
		response["photoBackupRoot"] = backupRoot
	}
	return response
}

func patchSettingsHandler(st *settings.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Changing global security policy (read-only, roots, sharing, backup
		// destination) is an admin operation — an ordinary paired device must
		// not be able to weaken the server for every other client.
		if !isAdminDevice(deviceFromContext(r)) {
			writeError(w, http.StatusForbidden, "FORBIDDEN", "admin device required")
			return
		}
		var b settingsBody
		if !decodeJSONBody(w, r, &b) {
			return
		}
		if b.ReadOnly != nil {
			if err := st.SetReadOnly(*b.ReadOnly); err != nil {
				writeInternal(w, "patch settings", err)
				return
			}
		}
		if b.Roots != nil {
			if err := st.SetRoots(*b.Roots); err != nil {
				writeInternal(w, "patch settings", err)
				return
			}
		}
		if b.AgentName != nil {
			if err := st.SetAgentName(*b.AgentName); err != nil {
				writeInternal(w, "patch settings", err)
				return
			}
		}
		if b.AllowSharing != nil {
			if err := st.SetAllowSharing(*b.AllowSharing); err != nil {
				writeInternal(w, "patch settings", err)
				return
			}
		}
		if b.PhotoBackupRoot != nil {
			if err := st.SetPhotoBackupRoot(*b.PhotoBackupRoot); err != nil {
				writeInternal(w, "patch settings", err)
				return
			}
		}
		writeJSON(w, http.StatusOK, settingsJSON(st, r))
	}
}

// --------- GET /settings/bandwidth ---------

func getBandwidthHandler(st *settings.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !isAdminDevice(deviceFromContext(r)) {
			writeError(w, http.StatusForbidden, "FORBIDDEN", "admin device required")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"maxUploadBytesPerSec":   st.MaxUploadBytesPerSec(),
			"maxDownloadBytesPerSec": st.MaxDownloadBytesPerSec(),
		})
	}
}

// --------- PUT /settings/bandwidth ---------

func putBandwidthHandler(st *settings.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !isAdminDevice(deviceFromContext(r)) {
			writeError(w, http.StatusForbidden, "FORBIDDEN", "admin device required")
			return
		}
		var b struct {
			MaxUploadBytesPerSec   *int64 `json:"maxUploadBytesPerSec"`
			MaxDownloadBytesPerSec *int64 `json:"maxDownloadBytesPerSec"`
		}
		if !decodeJSONBody(w, r, &b) {
			return
		}
		if b.MaxUploadBytesPerSec != nil {
			if err := st.SetMaxUploadBytesPerSec(*b.MaxUploadBytesPerSec); err != nil {
				writeInternal(w, "put bandwidth", err)
				return
			}
		}
		if b.MaxDownloadBytesPerSec != nil {
			if err := st.SetMaxDownloadBytesPerSec(*b.MaxDownloadBytesPerSec); err != nil {
				writeInternal(w, "put bandwidth", err)
				return
			}
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"maxUploadBytesPerSec":   st.MaxUploadBytesPerSec(),
			"maxDownloadBytesPerSec": st.MaxDownloadBytesPerSec(),
		})
	}
}

// deviceJSON builds the Device JSON shape shared by GET /devices (list) and
// PATCH /devices/{id} (single updated device).
func deviceJSON(d store.Device, cur *store.Device) map[string]any {
	return map[string]any{
		"id":          d.ID,
		"label":       d.Label,
		"created":     d.Created.Unix(),
		"lastSeen":    d.LastSeen.Unix(),
		"revoked":     d.Revoked,
		"current":     cur != nil && cur.ID == d.ID,
		"lastAddress": d.LastAddress,
		"lastVersion": d.LastVersion,
		"jailRoot":    d.JailRoot,
		"readOnly":    d.ReadOnly,
		"viewApps":    d.ViewApps,
		"launchApps":  d.LaunchApps,
		"browse":      d.CanBrowse,
		"download":    d.CanDownload,
		"upload":      d.CanUpload,
		"modify":      d.CanModify,
		"delete":      d.CanDelete,
		"share":       d.CanShare,
		"viaLogin":    d.ViaLogin,
	}
}

func listDevicesHandler(db *store.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cur := deviceFromContext(r)
		if !isAdminDevice(cur) {
			if cur == nil {
				writeJSON(w, http.StatusOK, []map[string]any{})
				return
			}
			self, err := db.GetDeviceByID(cur.ID)
			if err != nil {
				writeInternal(w, "list caller device", err)
				return
			}
			if self == nil {
				writeJSON(w, http.StatusOK, []map[string]any{})
				return
			}
			writeJSON(w, http.StatusOK, []map[string]any{{
				"id":       self.ID,
				"label":    self.Label,
				"created":  self.Created.Unix(),
				"lastSeen": self.LastSeen.Unix(),
				"current":  true,
			}})
			return
		}
		devices, err := db.ListDevices()
		if err != nil {
			writeInternal(w, "list devices", err)
			return
		}
		out := make([]map[string]any, 0, len(devices))
		for _, d := range devices {
			out = append(out, deviceJSON(d, cur))
		}
		writeJSON(w, http.StatusOK, out)
	}
}

// revokeDeviceHandler revokes device `id`. The third arg is the URL path param
// (the route wrapper passes chi.URLParam so this stays unit-testable).
//
// A device may always revoke ITSELF. Revoking another device additionally
// requires cur to be an admin device (authenticated via /login or /register —
// see isAdminDevice) — an ordinary paired device (e.g. the phone app, paired
// via /pair) still gets 403 FORBIDDEN. The `rfe revoke`/`remove` admin
// CLI remains available regardless, for headless/PC-side use.
func revokeDeviceHandler(db *store.DB) func(http.ResponseWriter, *http.Request, string) {
	return func(w http.ResponseWriter, r *http.Request, id string) {
		cur := deviceFromContext(r)
		if cur == nil || (cur.ID != id && !isAdminDevice(cur)) {
			writeError(w, http.StatusForbidden, "FORBIDDEN", "managing other devices requires an admin (login) session or the PC")
			return
		}
		if err := db.RevokeDevice(id); err != nil {
			writeInternal(w, "revoke device", err)
			return
		}
		audit(db, r, store.AuditDeviceRevoked, id, "")
		w.WriteHeader(http.StatusNoContent)
	}
}

// deleteDeviceHandler permanently removes device `id` (used to clear revoked
// devices from the list; reached via DELETE /v1/devices/{id}?purge=true).
//
// Same admin-or-self rule as revokeDeviceHandler.
func deleteDeviceHandler(db *store.DB) func(http.ResponseWriter, *http.Request, string) {
	return func(w http.ResponseWriter, r *http.Request, id string) {
		cur := deviceFromContext(r)
		if cur == nil || (cur.ID != id && !isAdminDevice(cur)) {
			writeError(w, http.StatusForbidden, "FORBIDDEN", "managing other devices requires an admin (login) session or the PC")
			return
		}
		if err := db.DeleteDevice(id); err != nil {
			writeInternal(w, "delete device", err)
			return
		}
		audit(db, r, store.AuditDeviceRemoved, id, "")
		w.WriteHeader(http.StatusNoContent)
	}
}

// deviceUpdateBody is the PATCH /v1/devices/{id} request body. Pointer fields
// distinguish an omitted permission from an explicit false value.
type deviceUpdateBody struct {
	JailRoot   *string `json:"jailRoot"`
	ReadOnly   *bool   `json:"readOnly"`
	ViewApps   *bool   `json:"viewApps"`
	LaunchApps *bool   `json:"launchApps"`
	Browse     *bool   `json:"browse"`
	Download   *bool   `json:"download"`
	Upload     *bool   `json:"upload"`
	Modify     *bool   `json:"modify"`
	Delete     *bool   `json:"delete"`
	Share      *bool   `json:"share"`
}

// setDeviceJailHandler implements PATCH /v1/devices/{id}: sets a target
// device's per-device path jail and/or read-only flag. This is an admin-only
// operation (see isAdminDevice) — an ordinary paired device gets 403
// FORBIDDEN, same as before this endpoint had a real implementation. The
// `rfe jail`/`readonly` admin CLI remains available for PC-side use.
func setDeviceJailHandler(db *store.DB, st *settings.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cur := deviceFromContext(r)
		if !isAdminDevice(cur) {
			writeError(w, http.StatusForbidden, "FORBIDDEN", "device access limits require an admin (login) session or the PC")
			return
		}
		id := chi.URLParam(r, "id")
		target, err := db.GetDeviceByID(id)
		if err != nil {
			writeInternal(w, "set device jail", err)
			return
		}
		if target == nil {
			writeError(w, http.StatusNotFound, "NOT_FOUND", "no such device")
			return
		}
		var b deviceUpdateBody
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxJSONBody)).Decode(&b); err != nil {
			writeError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid JSON body")
			return
		}
		viewApps := target.ViewApps
		launchApps := target.LaunchApps
		if b.ViewApps != nil {
			viewApps = *b.ViewApps
		}
		if b.LaunchApps != nil {
			launchApps = *b.LaunchApps
		}
		if launchApps && !viewApps {
			writeError(w, http.StatusBadRequest, "BAD_REQUEST", "launchApps requires viewApps")
			return
		}
		// The settings are written one by one, so a later failure would leave
		// the earlier ones applied while the caller is told the request
		// failed. rollback puts the values read above back in that case.
		rollback := func() {
			if _, err := SetDeviceJail(db, id, target.JailRoot, st.Roots(), st.IsReadOnly()); err != nil {
				log.Printf("device %s: could not restore jail after a failed update: %v", id, err)
			}
			if err := db.SetDeviceReadOnly(id, target.ReadOnly); err != nil {
				log.Printf("device %s: could not restore read-only after a failed update: %v", id, err)
			}
			if err := db.SetDeviceAppPermissions(id, target.ViewApps, target.LaunchApps); err != nil {
				log.Printf("device %s: could not restore app permissions after a failed update: %v", id, err)
			}
			if err := db.SetDeviceFilePermissions(id, target.CanBrowse, target.CanDownload, target.CanUpload, target.CanModify, target.CanDelete, target.CanShare); err != nil {
				log.Printf("device %s: could not restore file permissions after a failed update: %v", id, err)
			}
		}
		if b.JailRoot != nil {
			if _, err := SetDeviceJail(db, id, *b.JailRoot, st.Roots(), st.IsReadOnly()); err != nil {
				writeError(w, http.StatusBadRequest, "BAD_REQUEST", err.Error())
				return
			}
		}
		if b.ReadOnly != nil {
			if err := db.SetDeviceReadOnly(id, *b.ReadOnly); err != nil {
				rollback()
				writeInternal(w, "set device jail", err)
				return
			}
		}
		if b.ViewApps != nil || b.LaunchApps != nil {
			if err := db.SetDeviceAppPermissions(id, viewApps, launchApps); err != nil {
				rollback()
				writeInternal(w, "set device app permissions", err)
				return
			}
		}
		if b.Browse != nil || b.Download != nil || b.Upload != nil || b.Modify != nil || b.Delete != nil || b.Share != nil {
			browse, download := target.CanBrowse, target.CanDownload
			upload, modify, del, share := target.CanUpload, target.CanModify, target.CanDelete, target.CanShare
			if b.Browse != nil {
				browse = *b.Browse
			}
			if b.Download != nil {
				download = *b.Download
			}
			if b.Upload != nil {
				upload = *b.Upload
			}
			if b.Modify != nil {
				modify = *b.Modify
			}
			if b.Delete != nil {
				del = *b.Delete
			}
			if b.Share != nil {
				share = *b.Share
			}
			if err := db.SetDeviceFilePermissions(id, browse, download, upload, modify, del, share); err != nil {
				rollback()
				writeInternal(w, "set device file permissions", err)
				return
			}
		}
		updated, err := db.GetDeviceByID(id)
		if err != nil || updated == nil {
			writeError(w, http.StatusInternalServerError, "INTERNAL", "device vanished mid-update")
			return
		}
		audit(db, r, store.AuditDeviceUpdated, id,
			fmt.Sprintf("jailRoot=%q readOnly=%t viewApps=%t launchApps=%t browse=%t download=%t upload=%t modify=%t delete=%t share=%t",
				updated.JailRoot, updated.ReadOnly, updated.ViewApps, updated.LaunchApps,
				updated.CanBrowse, updated.CanDownload, updated.CanUpload, updated.CanModify, updated.CanDelete, updated.CanShare))
		writeJSON(w, http.StatusOK, deviceJSON(*updated, cur))
	}
}

// ValidateJailRoot validates a candidate per-device jailRoot against the
// agent's configured global roots, returning the cleaned path to persist (or
// "" to clear the per-device restriction).
//
// jailRoot must be either "" (clear the per-device restriction) or an
// absolute, cleaned path that resolves within the agent's configured global
// roots (roots) — if any roots are configured. A jailRoot outside the global
// roots would widen access if it were ever honored on its own, so it is
// rejected outright rather than accepted and silently clamped: better to fail
// loudly than to let an admin believe a jail is in effect when
// fsops.Ops.Jailed would in fact deny everything.
//
// Shared by setDeviceJailHandler's predecessor and the `rfe jail` admin
// CLI command, so both enforce the same containment rule.
func ValidateJailRoot(jailRoot string, roots []string, readOnly bool) (string, error) {
	if jailRoot == "" {
		return "", nil
	}
	clean := filepath.Clean(jailRoot)
	if !filepath.IsAbs(clean) {
		return "", fmt.Errorf("jailRoot must be an absolute path")
	}
	if len(roots) > 0 {
		// Reuse the same jail-resolution logic used for ordinary path access
		// (cleans + resolves symlinks + checks containment) so a jailRoot
		// that only "looks" contained via a symlink trick is rejected the
		// same way an escaping request would be.
		baseOps := fsops.New(roots, readOnly)
		if _, err := baseOps.Resolve(clean); err != nil {
			return "", fmt.Errorf("jailRoot must resolve within the agent's configured roots")
		}
	}
	return clean, nil
}

// SetDeviceJail validates jailRoot against the agent's configured global
// roots (via ValidateJailRoot) and, if valid, persists it as device id's
// per-device path jail (db.SetDeviceJail). Returns the cleaned jailRoot that
// was stored.
//
// Shared by setDeviceJailHandler (PATCH /v1/devices/{id}, admin-only) and the
// `rfe jail <device-id> <path>` CLI command, so both enforce the same
// validation and persistence logic.
func SetDeviceJail(db *store.DB, id, jailRoot string, roots []string, readOnly bool) (string, error) {
	clean, err := ValidateJailRoot(jailRoot, roots, readOnly)
	if err != nil {
		return "", err
	}
	if err := db.SetDeviceJail(id, clean); err != nil {
		return "", err
	}
	return clean, nil
}
