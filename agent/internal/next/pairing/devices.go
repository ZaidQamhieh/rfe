package pairing

import (
	"errors"
	"strings"

	"github.com/ZaidQamhieh/rfe/agent/internal/next/audit"
	"github.com/ZaidQamhieh/rfe/agent/internal/next/authz"
	"github.com/ZaidQamhieh/rfe/agent/internal/next/store"
)

// bearerPrefix starts an Authorization header that carries a device token.
const bearerPrefix = "Bearer "

// BearerToken returns the token in an Authorization header value, or "".
func BearerToken(header string) string {
	if len(header) <= len(bearerPrefix) || !strings.EqualFold(header[:len(bearerPrefix)], bearerPrefix) {
		return ""
	}
	return strings.TrimSpace(header[len(bearerPrefix):])
}

// Actor is who asks for an admin action. Admin is true for a login or
// registration device (see ActorOf) and for the PC itself; DeviceID is the
// calling device, "" for the PC.
type Actor struct {
	DeviceID string
	Name     string // shown in the audit trail
	Admin    bool
}

// PC is the actor for requests that come from the computer itself (the admin
// command line or the desktop app), which are admin by being on the machine.
var PC = Actor{Name: "pc", Admin: true}

// ActorOf describes an authenticated device as an actor; nil is nobody.
func ActorOf(d *store.Device) Actor {
	if d == nil || d.Revoked {
		return Actor{}
	}
	return Actor{DeviceID: d.ID, Name: d.Label, Admin: d.IsAdmin()}
}

// PrincipalOf converts a device into the caller the authorization rules judge.
// Admin is a device that proved the account password; it bypasses the six file
// grants but not the app grants, read-only mode, roots or its jail.
func PrincipalOf(d *store.Device) authz.Principal {
	return authz.Principal{
		DeviceID: d.ID,
		Admin:    d.IsAdmin(),
		Revoked:  d.Revoked,
		Grants: authz.Grants{
			Browse: d.CanBrowse, Download: d.CanDownload, Upload: d.CanUpload,
			Modify: d.CanModify, Delete: d.CanDelete, Share: d.CanShare,
			ViewApps: d.ViewApps, LaunchApps: d.LaunchApps,
		},
		JailRoot: d.JailRoot,
		ReadOnly: d.ReadOnly,
	}
}

// Authenticate resolves a bearer token to its device. Only the token's hash is
// looked up; an unknown token, a revoked device and an account device whose
// account is gone are all ErrUnauthorized, with one message so none can be told
// from another. Imported tokens (hashed by the old agent) work the same way.
func (s *Service) Authenticate(token string) (*store.Device, error) {
	if token == "" {
		return nil, ErrUnauthorized
	}
	d, err := s.o.Store.DeviceByToken(token)
	if err != nil {
		return nil, err
	}
	if d == nil || d.Revoked || !equalSecret(d.TokenHash, store.HashToken(token)) {
		return nil, ErrUnauthorized
	}
	return d, nil
}

// Touch records that a device was just used, from this address and app version.
func (s *Service) Touch(deviceID, address, version string) error {
	return s.deviceErr(s.o.Store.TouchDevice(deviceID, address, version, s.unixNow()))
}

// deviceErr maps the store's missing-device error to the service's.
func (s *Service) deviceErr(err error) error {
	if errors.Is(err, store.ErrDeviceNotFound) {
		return ErrDeviceNotFound
	}
	return err
}

// selfOrAdmin allows an admin, or a device acting on itself.
func selfOrAdmin(by Actor, id string) error {
	if by.Admin || (by.DeviceID != "" && by.DeviceID == id) {
		return nil
	}
	return ErrForbidden
}

// label is the device's name for the audit trail ("" if it is gone).
func (s *Service) label(id string) string {
	if d, err := s.o.Store.DeviceByID(id); err == nil && d != nil {
		return d.Label
	}
	return ""
}

// Revoke stops a device's token from working (its row stays, visible in the
// list). An admin may revoke any device, any device may revoke itself.
func (s *Service) Revoke(by Actor, id string) error {
	if err := selfOrAdmin(by, id); err != nil {
		return err
	}
	name := s.label(id)
	if err := s.deviceErr(s.o.Store.RevokeDevice(id)); err != nil {
		return err
	}
	s.audit(audit.ActionDeviceRevoked, by.Name, id, name)
	return nil
}

// Remove deletes a device row and its share links; same rights as Revoke.
func (s *Service) Remove(by Actor, id string) error {
	if err := selfOrAdmin(by, id); err != nil {
		return err
	}
	name := s.label(id)
	if err := s.deviceErr(s.o.Store.DeleteDevice(id)); err != nil {
		return err
	}
	s.audit(audit.ActionDeviceRemoved, by.Name, id, name)
	return nil
}

// SetJail confines a device to a folder ("" clears it). Admin only. The caller
// has validated root against the allowed roots.
func (s *Service) SetJail(by Actor, id, root string) error {
	if !by.Admin {
		return ErrForbidden
	}
	if err := s.deviceErr(s.o.Store.SetDeviceJail(id, root)); err != nil {
		return err
	}
	s.audit(audit.ActionDeviceUpdated, by.Name, id, "jail changed")
	return nil
}

// SetReadOnly sets or clears a device's read-only flag. Admin only.
func (s *Service) SetReadOnly(by Actor, id string, ro bool) error {
	if !by.Admin {
		return ErrForbidden
	}
	if err := s.deviceErr(s.o.Store.SetDeviceReadOnly(id, ro)); err != nil {
		return err
	}
	s.audit(audit.ActionDeviceUpdated, by.Name, id, "read-only changed")
	return nil
}

// SetFileGrants sets a device's six file grants from g (Browse, Download,
// Upload, Modify, Delete, Share); turning Share off drops its share links.
// Admin only.
func (s *Service) SetFileGrants(by Actor, id string, g authz.Grants) error {
	if !by.Admin {
		return ErrForbidden
	}
	err := s.deviceErr(s.o.Store.SetDeviceFileGrants(id, [6]bool{g.Browse, g.Download, g.Upload, g.Modify, g.Delete, g.Share}))
	if err != nil {
		return err
	}
	s.audit(audit.ActionDeviceUpdated, by.Name, id, "file permissions changed")
	return nil
}

// SetAppGrants sets a device's app-catalog and app-launch grants from g.
// Launching needs the catalog: LaunchApps without ViewApps is a BAD_REQUEST.
// Admin only.
func (s *Service) SetAppGrants(by Actor, id string, g authz.Grants) error {
	if !by.Admin {
		return ErrForbidden
	}
	if g.LaunchApps && !g.ViewApps {
		return badRequest("launching apps also requires viewing them")
	}
	if err := s.deviceErr(s.o.Store.SetDeviceAppGrants(id, g.ViewApps, g.LaunchApps)); err != nil {
		return err
	}
	s.audit(audit.ActionDeviceUpdated, by.Name, id, "app permissions changed")
	return nil
}

// DevicePatch is the set of changes UpdateDevice applies; a nil field is left
// alone. Jail is already validated against the allowed roots ("" clears it).
type DevicePatch struct {
	Jail     *string
	ReadOnly *bool
	Files    *authz.Grants // the six file grants are read from it
	Apps     *authz.Grants // ViewApps and LaunchApps are read from it
}

// UpdateDevice applies a whole patch atomically (see store.DB.UpdateDevice) and
// records one audit event per kind of change, as the single setters do. Admin
// only; LaunchApps without ViewApps is a BAD_REQUEST.
func (s *Service) UpdateDevice(by Actor, id string, p DevicePatch) error {
	if !by.Admin {
		return ErrForbidden
	}
	if p.Apps != nil && p.Apps.LaunchApps && !p.Apps.ViewApps {
		return badRequest("launching apps also requires viewing them")
	}
	u := store.DeviceUpdate{Jail: p.Jail, ReadOnly: p.ReadOnly}
	if g := p.Files; g != nil {
		u.File = &[6]bool{g.Browse, g.Download, g.Upload, g.Modify, g.Delete, g.Share}
	}
	if g := p.Apps; g != nil {
		u.App = &[2]bool{g.ViewApps, g.LaunchApps}
	}
	if err := s.deviceErr(s.o.Store.UpdateDevice(id, u)); err != nil {
		return err
	}
	if p.Jail != nil {
		s.audit(audit.ActionDeviceUpdated, by.Name, id, "jail changed")
	}
	if p.ReadOnly != nil {
		s.audit(audit.ActionDeviceUpdated, by.Name, id, "read-only changed")
	}
	if p.Files != nil {
		s.audit(audit.ActionDeviceUpdated, by.Name, id, "file permissions changed")
	}
	if p.Apps != nil {
		s.audit(audit.ActionDeviceUpdated, by.Name, id, "app permissions changed")
	}
	return nil
}
