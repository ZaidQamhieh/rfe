package api

import (
	"errors"
	"net/http"
	"path/filepath"
	"strconv"

	"github.com/ZaidQamhieh/rfe/agent/internal/next/audit"
	"github.com/ZaidQamhieh/rfe/agent/internal/next/authz"
	"github.com/ZaidQamhieh/rfe/agent/internal/next/settings"
)

// Scopes of GET /settings: the host policy for an admin, the caller's own
// effective policy for any other device.
const (
	scopeGlobal = "global"
	scopeDevice = "device"
)

// Query parameters of GET /audit.
const (
	queryAuditBefore = "before"
)

// MiscServices are the optional services behind search, thumbnails, share links,
// settings and the audit trail. A nil service leaves its routes answering
// 501 NOT_IMPLEMENTED, so a build without it still starts.
type MiscServices struct {
	Search    SearchService
	Thumbs    ThumbService
	ThumbSets ThumbSetFor
	Shares    ShareService
	// ShareBaseURL is the scheme and address a share link starts with, for
	// example "https://192.0.2.10:8765". Empty means the Host of the request.
	ShareBaseURL func() string
	Settings     SettingsService
	Audit        AuditLog
}

// SettingsService reads and changes the host settings. *settings.Service
// satisfies it.
type SettingsService interface {
	Get() settings.Settings
	Update(u settings.Update) (settings.Settings, error)
}

// AuditLog records and lists audit events. *audit.Log satisfies it.
type AuditLog interface {
	Try(ev audit.Event)
	List(limit int, before int64) (audit.Page, error)
}

// registerMiscHandlers adds the handlers of search, thumbnails, share links,
// settings and the audit trail to the registry of s.
func registerMiscHandlers(s *Server, m map[string]handlerFunc) {
	m[routeKey(get, "/search")] = s.search
	m[routeKey(get, "/thumb")] = s.thumb
	m[routeKey(post, "/share/mint")] = s.shareMint
	m[routeKey(get, "/share")] = s.shareList
	m[routeKey(del, "/share/{tokenHash}")] = s.shareRevoke
	m[routeKey(get, "/share/{token}")] = s.shareFetch
	m[routeKey(get, "/settings")] = s.settingsGet
	m[routeKey(patch, "/settings")] = s.settingsPatch
	m[routeKey(get, "/settings/bandwidth")] = s.bandwidthGet
	m[routeKey(put, "/settings/bandwidth")] = s.bandwidthPut
	m[routeKey(get, "/audit")] = s.auditList
}

// FileCapsJSON is what the calling device may do with files (openapi
// AgentSettings.fileCapabilities).
type FileCapsJSON struct {
	Browse   bool `json:"browse"`
	Download bool `json:"download"`
	Upload   bool `json:"upload"`
	Modify   bool `json:"modify"`
	Delete   bool `json:"delete"`
	Share    bool `json:"share"`
}

// AgentSettingsJSON is the settings answer (openapi AgentSettings).
type AgentSettingsJSON struct {
	IsAdmin               bool         `json:"isAdmin"`
	EffectiveScope        string       `json:"effectiveScope"`
	AccessDenied          bool         `json:"accessDenied"`
	ReadOnly              bool         `json:"readOnly"`
	Roots                 []string     `json:"roots"`
	AgentName             string       `json:"agentName"`
	AllowSharing          bool         `json:"allowSharing"`
	PhotoBackupConfigured bool         `json:"photoBackupConfigured"`
	PhotoBackupAvailable  bool         `json:"photoBackupAvailable"`
	PhotoBackupRoot       *string      `json:"photoBackupRoot,omitempty"`
	FileCapabilities      FileCapsJSON `json:"fileCapabilities"`
}

// settingsPatchBody is the JSON of PATCH /settings; a missing or null field is
// left as it is.
type settingsPatchBody struct {
	ReadOnly        *bool     `json:"readOnly"`
	Roots           *[]string `json:"roots"`
	AgentName       *string   `json:"agentName"`
	AllowSharing    *bool     `json:"allowSharing"`
	PhotoBackupRoot *string   `json:"photoBackupRoot"`
}

// BandwidthJSON is the body of the bandwidth routes (openapi BandwidthSettings).
type BandwidthJSON struct {
	MaxUploadBytesPerSec   int64 `json:"maxUploadBytesPerSec"`
	MaxDownloadBytesPerSec int64 `json:"maxDownloadBytesPerSec"`
}

// bandwidthPutBody is the JSON of PUT /settings/bandwidth; a missing field keeps
// its current limit.
type bandwidthPutBody struct {
	MaxUploadBytesPerSec   *int64 `json:"maxUploadBytesPerSec"`
	MaxDownloadBytesPerSec *int64 `json:"maxDownloadBytesPerSec"`
}

// auditBody is the body of GET /audit.
type auditBody struct {
	Entries []audit.Entry `json:"entries"`
}

// underAnyRoot reports whether p is one of roots or below one of them.
func underAnyRoot(p string, roots []string) bool {
	p = filepath.Clean(p)
	for _, root := range roots {
		rel, err := filepath.Rel(filepath.Clean(root), p)
		if err == nil && rel != parentDir && !hasParentPrefix(rel) {
			return true
		}
	}
	return false
}

// parentDir is the path element that leaves a base directory.
const parentDir = ".."

// hasParentPrefix reports whether a relative path starts by leaving its base.
func hasParentPrefix(rel string) bool {
	return len(rel) > len(parentDir) && rel[:len(parentDir)] == parentDir && rel[len(parentDir)] == filepath.Separator
}

// effectiveView is the policy one device meets: read-only when the host or the
// device is, and its folders narrowed by its jail. denied is true when the jail
// lies outside every host folder, which means no access at all (an empty roots
// list would otherwise mean unrestricted).
func effectiveView(set settings.Settings, p authz.Principal) (roots []string, readOnly, denied bool) {
	readOnly = set.ReadOnly || p.ReadOnly
	roots = append([]string{}, set.Roots...)
	if p.JailRoot == "" {
		return roots, readOnly, false
	}
	jail := filepath.Clean(p.JailRoot)
	if len(roots) == 0 || underAnyRoot(jail, roots) {
		return []string{jail}, readOnly, false
	}
	return []string{}, readOnly, true
}

// fileCapsOf lists what the caller may do with files; an admin device may do all.
func fileCapsOf(p authz.Principal) FileCapsJSON {
	if p.Admin {
		return FileCapsJSON{true, true, true, true, true, true}
	}
	g := p.Grants
	return FileCapsJSON{g.Browse, g.Download, g.Upload, g.Modify, g.Delete, g.Share}
}

// withSharingPolicy clears the share capability while sharing is switched off on
// the host, so an app never offers a link the agent would refuse. The grant is
// kept in the store and applies again when sharing is switched back on.
func withSharingPolicy(c FileCapsJSON, allowSharing bool) FileCapsJSON {
	if !allowSharing {
		c.Share = false
	}
	return c
}

// settingsView builds the answer for a caller. An admin sees the host settings;
// any other device sees its own effective policy, and the photo-backup folder
// only when that folder is reachable and writable for it.
func settingsView(set settings.Settings, p authz.Principal) AgentSettingsJSON {
	roots, readOnly, denied := effectiveView(set, p)
	available := set.PhotoBackupRoot != "" && !readOnly && !denied &&
		(len(roots) == 0 || underAnyRoot(set.PhotoBackupRoot, roots))
	out := AgentSettingsJSON{
		IsAdmin:               p.Admin,
		EffectiveScope:        scopeDevice,
		AccessDenied:          denied,
		ReadOnly:              readOnly,
		Roots:                 roots,
		AgentName:             set.AgentName,
		AllowSharing:          set.AllowSharing,
		PhotoBackupConfigured: set.PhotoBackupRoot != "",
		PhotoBackupAvailable:  available,
		FileCapabilities:      withSharingPolicy(fileCapsOf(p), set.AllowSharing),
	}
	if p.Admin {
		out.EffectiveScope = scopeGlobal
		out.AccessDenied = false
		out.ReadOnly = set.ReadOnly
		out.Roots = append([]string{}, set.Roots...)
		root := set.PhotoBackupRoot
		out.PhotoBackupRoot = &root
	} else if available {
		root := set.PhotoBackupRoot
		out.PhotoBackupRoot = &root
	}
	return out
}

// settingsError turns a refused setting into a BAD_REQUEST that names only the
// field: the validator's reason may contain a host path.
func settingsError(err error) error {
	var invalid *settings.InvalidError
	if errors.As(err, &invalid) {
		return badRequest("invalid setting: " + invalid.Field)
	}
	return err
}

// currentSettings returns the live settings from the settings service, or from
// the policy source when no settings service is wired.
func (s *Server) currentSettings() settings.Settings {
	if s.cfg.Misc.Settings != nil {
		return s.cfg.Misc.Settings.Get()
	}
	return s.cfg.Policy.Get()
}

// settingsGet answers GET /settings with the view that fits the caller.
func (s *Server) settingsGet(w http.ResponseWriter, r *http.Request) error {
	c, err := callOf(r)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, settingsView(s.currentSettings(), c.principal))
	return nil
}

// settingsPatch applies the given host settings (admin only, by the route table)
// and answers with the new settings. A refused value changes nothing.
func (s *Server) settingsPatch(w http.ResponseWriter, r *http.Request) error {
	svc := s.cfg.Misc.Settings
	if svc == nil {
		return notYet
	}
	c, err := callOf(r)
	if err != nil {
		return err
	}
	var body settingsPatchBody
	if err := decodeJSON(r, &body); err != nil {
		return err
	}
	updated, err := svc.Update(settings.Update{
		ReadOnly: body.ReadOnly, Roots: body.Roots, AgentName: body.AgentName,
		AllowSharing: body.AllowSharing, PhotoBackupRoot: body.PhotoBackupRoot,
	})
	if err != nil {
		return settingsError(err)
	}
	writeJSON(w, http.StatusOK, settingsView(updated, c.principal))
	return nil
}

// bandwidthGet answers GET /settings/bandwidth.
func (s *Server) bandwidthGet(w http.ResponseWriter, _ *http.Request) error {
	if s.cfg.Misc.Settings == nil {
		return notYet
	}
	cur := s.cfg.Misc.Settings.Get()
	writeJSON(w, http.StatusOK, BandwidthJSON{cur.MaxUploadBytesPerSec, cur.MaxDownloadBytesPerSec})
	return nil
}

// bandwidthPut sets the transfer speed limits that appear in the body and
// answers with both limits as they are afterwards.
func (s *Server) bandwidthPut(w http.ResponseWriter, r *http.Request) error {
	svc := s.cfg.Misc.Settings
	if svc == nil {
		return notYet
	}
	var body bandwidthPutBody
	if err := decodeJSON(r, &body); err != nil {
		return err
	}
	updated, err := svc.Update(settings.Update{
		MaxUploadBytesPerSec: body.MaxUploadBytesPerSec, MaxDownloadBytesPerSec: body.MaxDownloadBytesPerSec,
	})
	if err != nil {
		return settingsError(err)
	}
	writeJSON(w, http.StatusOK, BandwidthJSON{updated.MaxUploadBytesPerSec, updated.MaxDownloadBytesPerSec})
	return nil
}

// auditList answers GET /audit: newest events first. A limit that is missing,
// not a number or outside 1..500 gives the default page of 100; before is the
// id of the last entry of the previous page.
func (s *Server) auditList(w http.ResponseWriter, r *http.Request) error {
	if s.cfg.Misc.Audit == nil {
		return notYet
	}
	q := r.URL.Query()
	limit, _ := strconv.Atoi(q.Get(queryLimit)) // a bad number is 0: the log applies its default
	before, err := strconv.ParseInt(q.Get(queryAuditBefore), 10, 64)
	if err != nil || before < 0 {
		before = 0 // as the old agent did: an unusable cursor means the newest page
	}
	page, err := s.cfg.Misc.Audit.List(limit, before)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, auditBody{Entries: page.Entries})
	return nil
}
