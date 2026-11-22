package api

import (
	"net/http"
	"strconv"

	"github.com/ZaidQamhieh/rfe/agent/internal/next/apps"
	"github.com/ZaidQamhieh/rfe/agent/internal/next/authz"
	"github.com/ZaidQamhieh/rfe/agent/internal/next/pairing"
	"github.com/ZaidQamhieh/rfe/agent/internal/next/store"
)

// msgJailRefused is the only text a client sees when a jail folder is refused:
// the validator's own error may name host paths.
const msgJailRefused = "jailRoot must be an absolute folder inside the folders this computer shares"

// msgLaunchNeedsView is the BAD_REQUEST text for a launch grant without the catalog grant.
const msgLaunchNeedsView = "launching apps also requires viewing them"

// msgPurgeInvalid is the BAD_REQUEST text for a purge value that is not a boolean.
const msgPurgeInvalid = "purge must be true or false"

// queryPurge is the query parameter of DELETE /devices/{id}.
const queryPurge = "purge"

// queryNonce is the query parameter that carries the pairing poll nonce.
const queryNonce = "nonce"

// DeviceJSON is a device as an admin sees it (openapi Device).
type DeviceJSON struct {
	ID          string `json:"id"`
	Label       string `json:"label"`
	Created     int64  `json:"created"`
	LastSeen    int64  `json:"lastSeen"`
	Revoked     bool   `json:"revoked"`
	Current     bool   `json:"current"`
	LastAddress string `json:"lastAddress"`
	LastVersion string `json:"lastVersion"`
	JailRoot    string `json:"jailRoot"`
	ReadOnly    bool   `json:"readOnly"`
	ViewApps    bool   `json:"viewApps"`
	LaunchApps  bool   `json:"launchApps"`
	Browse      bool   `json:"browse"`
	Download    bool   `json:"download"`
	Upload      bool   `json:"upload"`
	Modify      bool   `json:"modify"`
	Delete      bool   `json:"delete"`
	Share       bool   `json:"share"`
	ViaLogin    bool   `json:"viaLogin"`
}

// OwnDeviceJSON is the minimal record an ordinary device gets for itself: no
// other device's label, address, version, jail or revocation state is exposed.
type OwnDeviceJSON struct {
	ID       string `json:"id"`
	Label    string `json:"label"`
	Created  int64  `json:"created"`
	LastSeen int64  `json:"lastSeen"`
	Current  bool   `json:"current"`
}

// deviceJSON converts a stored device; current marks the caller's own row.
func deviceJSON(d store.Device, current bool) DeviceJSON {
	return DeviceJSON{
		ID: d.ID, Label: d.Label, Created: d.Created, LastSeen: d.LastSeen, Revoked: d.Revoked,
		Current: current, LastAddress: d.LastAddress, LastVersion: d.LastVersion, JailRoot: d.JailRoot,
		ReadOnly: d.ReadOnly, ViewApps: d.ViewApps, LaunchApps: d.LaunchApps,
		Browse: d.CanBrowse, Download: d.CanDownload, Upload: d.CanUpload, Modify: d.CanModify,
		Delete: d.CanDelete, Share: d.CanShare, ViaLogin: d.IsAdmin(),
	}
}

// validateJail checks a jail folder with the configured validator. Any refusal
// becomes the fixed BAD_REQUEST text, and with no validator every folder is
// refused (fail closed).
func (s *Server) validateJail(root string) (string, error) {
	if s.cfg.ValidateJail == nil {
		return "", badRequest(msgJailRefused)
	}
	clean, err := s.cfg.ValidateJail(root)
	if err != nil {
		return "", badRequest(msgJailRefused)
	}
	return clean, nil
}

// devicesList answers the device list: every device for an admin, only the
// caller's own minimal record for anyone else.
func (s *Server) devicesList(w http.ResponseWriter, r *http.Request) error {
	c, err := callOf(r)
	if err != nil {
		return err
	}
	all, err := s.cfg.Devices.Devices()
	if err != nil {
		return err
	}
	if c.principal.Admin {
		out := make([]DeviceJSON, 0, len(all))
		for _, d := range all {
			out = append(out, deviceJSON(d, d.ID == c.device.ID))
		}
		writeJSON(w, http.StatusOK, out)
		return nil
	}
	own := make([]OwnDeviceJSON, 0, 1)
	for _, d := range all {
		if d.ID == c.device.ID {
			own = append(own, OwnDeviceJSON{ID: d.ID, Label: d.Label, Created: d.Created, LastSeen: d.LastSeen, Current: true})
		}
	}
	writeJSON(w, http.StatusOK, own)
	return nil
}

// devicePatchBody is the JSON of PATCH /devices/{id}; a missing field is left
// as it is.
type devicePatchBody struct {
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

// setIf overwrites *dst with *src when src is present.
func setIf(dst *bool, src *bool) {
	if src != nil {
		*dst = *src
	}
}

// devicePatch changes one device's jail, read-only flag and grants (admin only).
// Everything is validated, and a revoked device refused with 409, before the
// first write, so a bad request changes nothing, and the writes are one
// transaction, so a storage failure changes nothing either.
func (s *Server) devicePatch(w http.ResponseWriter, r *http.Request) error {
	c, err := callOf(r)
	if err != nil {
		return err
	}
	var b devicePatchBody
	if err := decodeJSON(r, &b); err != nil {
		return err
	}
	id := r.PathValue("id")
	cur, err := s.cfg.Devices.DeviceByID(id)
	if err != nil {
		return err
	}
	if cur == nil {
		return pairing.ErrDeviceNotFound
	}
	// A revoked device is frozen: refuse before anything is validated or written.
	if cur.Revoked {
		return pairing.ErrDeviceRevoked
	}
	by := pairing.ActorOf(c.device)

	jail := ""
	if b.JailRoot != nil && *b.JailRoot != "" {
		if jail, err = s.validateJail(*b.JailRoot); err != nil {
			return err
		}
	}
	g := authz.Grants{
		Browse: cur.CanBrowse, Download: cur.CanDownload, Upload: cur.CanUpload, Modify: cur.CanModify,
		Delete: cur.CanDelete, Share: cur.CanShare, ViewApps: cur.ViewApps, LaunchApps: cur.LaunchApps,
	}
	setIf(&g.Browse, b.Browse)
	setIf(&g.Download, b.Download)
	setIf(&g.Upload, b.Upload)
	setIf(&g.Modify, b.Modify)
	setIf(&g.Delete, b.Delete)
	setIf(&g.Share, b.Share)
	setIf(&g.ViewApps, b.ViewApps)
	setIf(&g.LaunchApps, b.LaunchApps)
	filesChanged := b.Browse != nil || b.Download != nil || b.Upload != nil || b.Modify != nil || b.Delete != nil || b.Share != nil
	appsChanged := b.ViewApps != nil || b.LaunchApps != nil
	if appsChanged && g.LaunchApps && !g.ViewApps {
		return badRequest(msgLaunchNeedsView)
	}

	patch := pairing.DevicePatch{ReadOnly: b.ReadOnly}
	if b.JailRoot != nil {
		patch.Jail = &jail
	}
	if filesChanged {
		patch.Files = &g
	}
	if appsChanged {
		patch.Apps = &g
	}
	if err := s.cfg.Pairing.UpdateDevice(by, id, patch); err != nil {
		return err
	}
	updated, err := s.cfg.Devices.DeviceByID(id)
	if err != nil {
		return err
	}
	if updated == nil {
		return pairing.ErrDeviceNotFound
	}
	writeJSON(w, http.StatusOK, deviceJSON(*updated, updated.ID == c.device.ID))
	return nil
}

// deviceDelete revokes a device, or with purge=true deletes its row. A device may
// act on itself; another device needs an admin (the guard and the service both check).
func (s *Server) deviceDelete(w http.ResponseWriter, r *http.Request) error {
	c, err := callOf(r)
	if err != nil {
		return err
	}
	purge := false
	if raw := r.URL.Query().Get(queryPurge); raw != "" {
		if purge, err = strconv.ParseBool(raw); err != nil {
			return badRequest(msgPurgeInvalid)
		}
	}
	by, id := pairing.ActorOf(c.device), r.PathValue("id")
	if purge {
		err = s.cfg.Pairing.Remove(by, id)
	} else {
		err = s.cfg.Pairing.Revoke(by, id)
	}
	if err != nil { // an unknown device is 404 NOT_FOUND, so a caller can tell it was never there
		return err
	}
	// Its unfinished uploads can no longer be completed; drop them so they do not fill the quota if it is paired again.
	if d, ok := s.cfg.Transfers.(interface{ DropDevice(deviceID string) }); ok {
		d.DropDevice(id)
	}
	writeNoContent(w)
	return nil
}

// statusStarted is the body of an accepted app launch.
const statusStarted = "started"

// appsList answers the host app catalog for the caller.
func (s *Server) appsList(w http.ResponseWriter, r *http.Request) error {
	c, err := callOf(r)
	if err != nil {
		return err
	}
	cat, err := s.cfg.Apps.List(r.Context(), c.principal, c.policy)
	if err != nil {
		return err
	}
	if cat.Apps == nil {
		cat.Apps = []apps.App{} // the contract's "apps" is an array, never null
	}
	writeJSON(w, http.StatusOK, cat)
	return nil
}

// appLaunch asks the host to start a catalog app by its opaque id; 202 means the
// native launcher accepted the handoff.
func (s *Server) appLaunch(w http.ResponseWriter, r *http.Request) error {
	c, err := callOf(r)
	if err != nil {
		return err
	}
	if err := s.cfg.Apps.Launch(r.Context(), c.principal, c.policy, r.PathValue("id")); err != nil {
		return err
	}
	writeJSON(w, http.StatusAccepted, struct {
		Status string `json:"status"`
	}{statusStarted})
	return nil
}
