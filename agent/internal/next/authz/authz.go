// Package authz decides whether a device may perform an action. It is a pure
// function over values: no database, no filesystem, no clock. It reproduces the
// rules of the old agent (agent/internal/server) so the new agent answers every
// request the same way; each rule below cites the old code it comes from.
package authz

import (
	"errors"
	"fmt"
	"path/filepath"
)

// Capability names one grant. The string is the name the old agent prints in
// its denial message and uses as the key in the device record.
type Capability string

// The per-device grants (agent/internal/server/auth.go:149-156 for the six file
// grants, store.Device ViewApps/LaunchApps for the two app grants).
const (
	CapBrowse     Capability = "browse"
	CapDownload   Capability = "download"
	CapUpload     Capability = "upload"
	CapModify     Capability = "modify"
	CapDelete     Capability = "delete"
	CapShare      Capability = "share"
	CapViewApps   Capability = "viewApps"
	CapLaunchApps Capability = "launchApps"
)

// Error codes of the openapi Error enum that a denial can carry.
const (
	CodeUnauthorized     = "UNAUTHORIZED"      // auth.go:81,92,101
	CodeForbidden        = "FORBIDDEN"         // settings_handlers.go:38
	CodeCapabilityDenied = "CAPABILITY_DENIED" // auth.go:186
	CodeReadOnly         = "READ_ONLY"         // server.go:257
)

// Denial kinds. A Decision's Err wraps exactly one of these, so callers test
// with errors.Is.
var (
	ErrUnauthorized     = errors.New("unauthorized")
	ErrForbidden        = errors.New("forbidden")
	ErrCapabilityDenied = errors.New("capability denied")
	ErrReadOnly         = errors.New("read-only")
)

// Denial messages. Where the old agent had a message it is kept word for word.
const (
	msgDeviceRequired = "device required"                                                    // auth.go:178
	msgRevoked        = "invalid or revoked token"                                           // auth.go:101
	msgLacksFormat    = "device lacks %s permission"                                         // auth.go:186
	msgReadOnly       = "agent is in read-only mode"                                         // fsops.go:37, used at server.go:257
	msgAdminRequired  = "admin device required"                                              // settings_handlers.go:38
	msgSelfOrAdmin    = "managing other devices requires an admin (login) session or the PC" // settings_handlers.go:48
	msgWakeOnLAN      = "this device is not permitted to send Wake-on-LAN"                   // wol_handler.go:30
	msgOutsideRoots   = "path is outside allowed root"                                       // fsops.go:27, returned by jail.go:122
	msgUnknownAction  = "unknown action"
)

// Grants are the capabilities recorded on one device.
type Grants struct {
	Browse, Download, Upload, Modify, Delete, Share bool
	ViewApps, LaunchApps                            bool
}

// Has mirrors deviceHasCapability (auth.go:193-213): an unknown capability is
// never granted.
func (g Grants) Has(c Capability) bool {
	switch c {
	case CapBrowse:
		return g.Browse
	case CapDownload:
		return g.Download
	case CapUpload:
		return g.Upload
	case CapModify:
		return g.Modify
	case CapDelete:
		return g.Delete
	case CapShare:
		return g.Share
	case CapViewApps:
		return g.ViewApps
	case CapLaunchApps:
		return g.LaunchApps
	default:
		return false
	}
}

// Principal is the authenticated caller. An empty DeviceID means no device was
// found for the request (auth.go:176-179).
type Principal struct {
	DeviceID string
	// Admin is a device that signed in with the account password (the old
	// Device.ViaLogin, settings_handlers.go:28). It bypasses the six file grants
	// but not the app grants, read-only, roots or its jail.
	Admin    bool
	Revoked  bool
	Grants   Grants
	JailRoot string // "" means no per-device jail
	ReadOnly bool   // per-device read-only flag (auth.go:137)
}

// Policy is the agent-wide state that applies to every caller.
type Policy struct {
	ReadOnly bool     // global read-only mode
	Roots    []string // allowed folders; empty means unrestricted
}

// Target is what the action is aimed at. Paths are checked lexically against
// the roots and the jail; resolving symlinks is the filesystem layer's job.
type Target struct {
	DeviceID string   // the device being managed, for ActionSelfOrAdmin
	Paths    []string // every path the request names (source and destination)
}

// Action is one thing a caller may be allowed to do.
type Action int

// The actions. Each row of rules says what it needs.
const (
	// ActionNone needs only a valid, non-revoked device.
	ActionNone Action = iota
	ActionBrowse
	ActionDownload
	ActionUpload
	// ActionUploadOverwrite is an upload that replaces an existing file; it also
	// needs modify (transferhandlers.go:231,360,490).
	ActionUploadOverwrite
	ActionModify
	ActionDelete
	// ActionShare is the share grant alone.
	ActionShare
	// ActionMintShare creates a share link: share, browse and download
	// (server.go:188; compat.md C5).
	ActionMintShare
	// ActionServeShare is evaluated for the link's owner when the link is fetched:
	// share and download (share_handlers.go:205).
	ActionServeShare
	// ActionMove needs modify and delete (server.go:207; compat.md C5).
	ActionMove
	// ActionCopyOverwrite is a copy that replaces the destination: modify, plus
	// delete (fshandlers.go:248).
	ActionCopyOverwrite
	ActionViewApps
	ActionLaunchApps
	// ActionAdmin needs an admin device (settings_handlers.go:34-40).
	ActionAdmin
	// ActionSelfOrAdmin lets a device act on its own record, an admin on any
	// (settings_handlers.go:44-50).
	ActionSelfOrAdmin
	// ActionWakeOnLAN is refused to a read-only device (wol_handler.go:30).
	ActionWakeOnLAN

	actionCount // keep last
)

// rule is the requirement of one action.
type rule struct {
	name        string
	caps        []Capability // all required, checked in order
	writes      bool         // refused under global or device read-only
	admin       bool
	selfOrAdmin bool
	wol         bool
}

// rules is the single table behind Decide.
var rules = map[Action]rule{
	ActionNone:            {name: "none"},
	ActionBrowse:          {name: "browse", caps: []Capability{CapBrowse}},
	ActionDownload:        {name: "download", caps: []Capability{CapDownload}},
	ActionUpload:          {name: "upload", caps: []Capability{CapUpload}, writes: true},
	ActionUploadOverwrite: {name: "uploadOverwrite", caps: []Capability{CapUpload, CapModify}, writes: true},
	ActionModify:          {name: "modify", caps: []Capability{CapModify}, writes: true},
	ActionDelete:          {name: "delete", caps: []Capability{CapDelete}, writes: true},
	ActionShare:           {name: "share", caps: []Capability{CapShare}},
	ActionMintShare:       {name: "mintShare", caps: []Capability{CapShare, CapBrowse, CapDownload}},
	ActionServeShare:      {name: "serveShare", caps: []Capability{CapShare, CapDownload}},
	ActionMove:            {name: "move", caps: []Capability{CapModify, CapDelete}, writes: true},
	ActionCopyOverwrite:   {name: "copyOverwrite", caps: []Capability{CapModify, CapDelete}, writes: true},
	ActionViewApps:        {name: "viewApps", caps: []Capability{CapViewApps}},
	ActionLaunchApps:      {name: "launchApps", caps: []Capability{CapViewApps, CapLaunchApps}},
	ActionAdmin:           {name: "admin", admin: true},
	ActionSelfOrAdmin:     {name: "selfOrAdmin", selfOrAdmin: true},
	ActionWakeOnLAN:       {name: "wakeOnLAN", wol: true},
}

// String returns the action's short name, or "action(N)" for an unknown value.
func (a Action) String() string {
	if r, ok := rules[a]; ok {
		return r.name
	}
	return fmt.Sprintf("action(%d)", int(a))
}

// Writes reports whether the action changes files and is therefore refused
// under read-only mode.
func (a Action) Writes() bool { return rules[a].writes }

// AllActions lists every defined action in declaration order.
func AllActions() []Action {
	out := make([]Action, 0, actionCount)
	for a := Action(0); a < actionCount; a++ {
		out = append(out, a)
	}
	return out
}

// fileGrant reports whether c is one of the six file grants an admin device
// bypasses. App grants are explicit for everyone (server.go:87-89).
func fileGrant(c Capability) bool {
	switch c {
	case CapBrowse, CapDownload, CapUpload, CapModify, CapDelete, CapShare:
		return true
	}
	return false
}

// Denial is the error inside a refused Decision. Its text is safe to send to
// the client; Unwrap gives the kind (ErrForbidden and so on).
type Denial struct {
	Kind error
	Msg  string
}

func (d *Denial) Error() string { return d.Msg }
func (d *Denial) Unwrap() error { return d.Kind }

// CodeOf returns the openapi error code for a denial kind, or "" if err is not
// one of the four kinds.
func CodeOf(err error) string {
	switch {
	case errors.Is(err, ErrUnauthorized):
		return CodeUnauthorized
	case errors.Is(err, ErrForbidden):
		return CodeForbidden
	case errors.Is(err, ErrCapabilityDenied):
		return CodeCapabilityDenied
	case errors.Is(err, ErrReadOnly):
		return CodeReadOnly
	}
	return ""
}

// Decision is the outcome of Decide. When Allowed is false, Err is a *Denial,
// Code is its openapi code and Missing names the grant that was lacking (only
// for CAPABILITY_DENIED).
type Decision struct {
	Allowed bool
	Err     error
	Code    string
	Missing Capability
}

func allow() Decision { return Decision{Allowed: true} }

func deny(kind error, msg string) Decision {
	return Decision{Err: &Denial{Kind: kind, Msg: msg}, Code: CodeOf(kind)}
}

// Decide says whether p may do action a on t under policy pol. It runs the
// checks in the order the old agent's middleware chain did: valid device, the
// action's own requirement (admin, grants), read-only, then paths. It does no
// I/O and does not modify its arguments.
func Decide(p Principal, a Action, t Target, pol Policy) Decision {
	if p.DeviceID == "" {
		return deny(ErrUnauthorized, msgDeviceRequired) // auth.go:176-179
	}
	if p.Revoked {
		return deny(ErrUnauthorized, msgRevoked) // auth.go:100-101
	}
	r, ok := rules[a]
	if !ok {
		return deny(ErrForbidden, msgUnknownAction) // fail closed
	}
	switch {
	case r.admin && !p.Admin:
		return deny(ErrForbidden, msgAdminRequired) // settings_handlers.go:34-40
	case r.selfOrAdmin && !(p.Admin || (t.DeviceID != "" && t.DeviceID == p.DeviceID)):
		return deny(ErrForbidden, msgSelfOrAdmin) // settings_handlers.go:44-50
	case r.wol && p.ReadOnly:
		return deny(ErrForbidden, msgWakeOnLAN) // wol_handler.go:29-31
	}
	for _, c := range r.caps {
		if p.Admin && fileGrant(c) {
			continue // auth.go:181-183: login devices keep full file access
		}
		if !p.Grants.Has(c) {
			d := deny(ErrCapabilityDenied, fmt.Sprintf(msgLacksFormat, c)) // auth.go:184-188
			d.Missing = c
			return d
		}
	}
	if r.writes && (pol.ReadOnly || p.ReadOnly) {
		return deny(ErrReadOnly, msgReadOnly) // server.go:256-258; auth.go:137
	}
	if len(t.Paths) > 0 && !pathsAllowed(t.Paths, pol.Roots, p.JailRoot) {
		return deny(ErrForbidden, msgOutsideRoots) // fsops jail.go:122
	}
	return allow()
}

// pathsAllowed applies the effective roots of Ops.Jailed (fsops/jail.go:94-113):
// no roots and no jail allows everything; a jail inside a root narrows to the
// jail; a jail with no global roots is the only root; a jail outside every root
// allows nothing.
func pathsAllowed(paths, roots []string, jail string) bool {
	effective := roots
	if jail != "" {
		jail = filepath.Clean(jail)
		switch {
		case len(roots) == 0:
			effective = []string{jail}
		case underAny(jail, roots):
			effective = []string{jail}
		default:
			return false
		}
	}
	if len(effective) == 0 {
		return true
	}
	for _, p := range paths {
		if !underAny(p, effective) {
			return false
		}
	}
	return true
}

func underAny(p string, roots []string) bool {
	for _, r := range roots {
		if isUnder(p, r) {
			return true
		}
	}
	return false
}

// isUnder reports whether p equals root or is below it, after cleaning both. A
// filesystem root such as "/" already ends in a separator, so no second one is
// added (fsops/jail.go:278-290).
func isUnder(p, root string) bool {
	root = filepath.Clean(root)
	p = filepath.Clean(p)
	if p == root {
		return true
	}
	prefix := root
	if len(prefix) == 0 || prefix[len(prefix)-1] != filepath.Separator {
		prefix += string(filepath.Separator)
	}
	return len(p) >= len(prefix) && p[:len(prefix)] == prefix
}
