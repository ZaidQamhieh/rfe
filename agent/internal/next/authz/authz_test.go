package authz_test

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/ZaidQamhieh/rfe/agent/internal/next/authz"
)

// fileCaps lists the six per-device file grants in the order the old agent's
// route tables name them (agent/internal/server/auth.go:149-156).
var fileCaps = []authz.Capability{
	authz.CapBrowse, authz.CapDownload, authz.CapUpload,
	authz.CapModify, authz.CapDelete, authz.CapShare,
}

const testDeviceID = "dev-1"

// grantsFromMask builds a Grants value from a 6-bit mask, bit i = fileCaps[i].
func grantsFromMask(mask int) authz.Grants {
	return authz.Grants{
		Browse:   mask&(1<<0) != 0,
		Download: mask&(1<<1) != 0,
		Upload:   mask&(1<<2) != 0,
		Modify:   mask&(1<<3) != 0,
		Delete:   mask&(1<<4) != 0,
		Share:    mask&(1<<5) != 0,
	}
}

// oldDecision is a hand port of the old agent's requireDeviceFileCapabilities
// (agent/internal/server/auth.go:175-191) over deviceHasCapability
// (auth.go:193-213): an admin (ViaLogin) device passes, otherwise the first
// required capability the device lacks denies. It does not share code with the
// package under test.
func oldDecision(admin bool, g authz.Grants, required []authz.Capability) (allowed bool, missing authz.Capability) {
	if admin {
		return true, ""
	}
	has := map[authz.Capability]bool{
		authz.CapBrowse: g.Browse, authz.CapDownload: g.Download, authz.CapUpload: g.Upload,
		authz.CapModify: g.Modify, authz.CapDelete: g.Delete, authz.CapShare: g.Share,
	}
	for _, c := range required {
		if !has[c] {
			return false, c
		}
	}
	return true, ""
}

// oldRequirements are the capability lists the old agent demands per action,
// each with the registration it comes from.
var oldRequirements = map[authz.Action][]authz.Capability{
	authz.ActionBrowse:          {authz.CapBrowse},                                    // server.go:201
	authz.ActionDownload:        {authz.CapDownload},                                  // server.go:229
	authz.ActionUpload:          {authz.CapUpload},                                    // server.go:241
	authz.ActionModify:          {authz.CapModify},                                    // server.go:203
	authz.ActionDelete:          {authz.CapDelete},                                    // server.go:202
	authz.ActionMove:            {authz.CapModify, authz.CapDelete},                   // server.go:207
	authz.ActionMintShare:       {authz.CapShare, authz.CapBrowse, authz.CapDownload}, // server.go:188
	authz.ActionUploadOverwrite: {authz.CapUpload, authz.CapModify},                   // server.go:241 + transferhandlers.go:231
	authz.ActionCopyOverwrite:   {authz.CapModify, authz.CapDelete},                   // server.go:206 + fshandlers.go:248
	authz.ActionServeShare:      {authz.CapShare, authz.CapDownload},                  // share_handlers.go:205
}

func TestDecideAgreesWithOldCapabilityRulesForEveryGrantCombination(t *testing.T) {
	for action, required := range oldRequirements {
		for mask := 0; mask < 1<<len(fileCaps); mask++ {
			for _, admin := range []bool{false, true} {
				g := grantsFromMask(mask)
				p := authz.Principal{DeviceID: testDeviceID, Admin: admin, Grants: g}
				got := authz.Decide(p, action, authz.Target{}, authz.Policy{})
				wantAllowed, wantMissing := oldDecision(admin, g, required)
				if got.Allowed != wantAllowed {
					t.Fatalf("%v admin=%v mask=%06b: allowed=%v, old agent says %v", action, admin, mask, got.Allowed, wantAllowed)
				}
				if got.Missing != wantMissing {
					t.Fatalf("%v admin=%v mask=%06b: missing=%q, old agent says %q", action, admin, mask, got.Missing, wantMissing)
				}
				if !wantAllowed {
					if got.Code != authz.CodeCapabilityDenied || !errors.Is(got.Err, authz.ErrCapabilityDenied) {
						t.Fatalf("%v mask=%06b: code=%q err=%v, want CAPABILITY_DENIED", action, mask, got.Code, got.Err)
					}
				}
			}
		}
	}
}

func TestShareActionNeedsOnlyTheShareGrant(t *testing.T) {
	p := authz.Principal{DeviceID: testDeviceID, Grants: authz.Grants{Share: true}}
	if d := authz.Decide(p, authz.ActionShare, authz.Target{}, authz.Policy{}); !d.Allowed {
		t.Fatalf("share grant alone should allow ActionShare: %+v", d)
	}
	p.Grants = authz.Grants{Browse: true, Download: true}
	if d := authz.Decide(p, authz.ActionShare, authz.Target{}, authz.Policy{}); d.Allowed {
		t.Fatal("browse+download without share must not allow ActionShare")
	}
}

// App grants are explicit: an admin login grants no implicit access
// (agent/internal/server/server.go:87-89, apps_handlers.go:55 and :90).
func TestAppActionsNeverBypassedByAdmin(t *testing.T) {
	cases := []struct {
		name   string
		action authz.Action
		g      authz.Grants
		want   bool
	}{
		{"view none", authz.ActionViewApps, authz.Grants{}, false},
		{"view granted", authz.ActionViewApps, authz.Grants{ViewApps: true}, true},
		{"view needs viewApps not launch", authz.ActionViewApps, authz.Grants{LaunchApps: true}, false},
		{"launch needs both: only launch", authz.ActionLaunchApps, authz.Grants{LaunchApps: true}, false},
		{"launch needs both: only view", authz.ActionLaunchApps, authz.Grants{ViewApps: true}, false},
		{"launch both", authz.ActionLaunchApps, authz.Grants{ViewApps: true, LaunchApps: true}, true},
		{"file grants do not help", authz.ActionViewApps, authz.Grants{Browse: true, Download: true, Upload: true, Modify: true, Delete: true, Share: true}, false},
	}
	for _, c := range cases {
		for _, admin := range []bool{false, true} {
			d := authz.Decide(authz.Principal{DeviceID: testDeviceID, Admin: admin, Grants: c.g}, c.action, authz.Target{}, authz.Policy{})
			if d.Allowed != c.want {
				t.Errorf("%s admin=%v: allowed=%v want %v", c.name, admin, d.Allowed, c.want)
			}
			if !c.want && d.Code != authz.CodeCapabilityDenied {
				t.Errorf("%s admin=%v: code=%q want CAPABILITY_DENIED (compat.md C5)", c.name, admin, d.Code)
			}
		}
	}
}

func TestRevokedOrAnonymousPrincipalIsUnauthorized(t *testing.T) {
	all := authz.Grants{Browse: true, Download: true, Upload: true, Modify: true, Delete: true, Share: true, ViewApps: true, LaunchApps: true}
	for name, p := range map[string]authz.Principal{
		"revoked":   {DeviceID: testDeviceID, Revoked: true, Admin: true, Grants: all}, // auth.go:100
		"no device": {Admin: true, Grants: all},                                        // auth.go:176-179
	} {
		for _, a := range []authz.Action{authz.ActionNone, authz.ActionBrowse, authz.ActionAdmin} {
			d := authz.Decide(p, a, authz.Target{}, authz.Policy{})
			if d.Allowed || d.Code != authz.CodeUnauthorized || !errors.Is(d.Err, authz.ErrUnauthorized) {
				t.Errorf("%s %v: %+v, want UNAUTHORIZED", name, a, d)
			}
		}
	}
}

func TestAuthenticatedOnlyActionNeedsNoGrant(t *testing.T) {
	d := authz.Decide(authz.Principal{DeviceID: testDeviceID}, authz.ActionNone, authz.Target{}, authz.Policy{})
	if !d.Allowed || d.Err != nil || d.Code != "" {
		t.Fatalf("ActionNone with no grants: %+v", d)
	}
}

// Read-only applies to write actions for admin devices too: "global policy,
// roots, jail and read-only still apply" (compat.md C5 text of the task).
func TestReadOnlyBlocksWritesFromEveryCaller(t *testing.T) {
	all := authz.Grants{Browse: true, Download: true, Upload: true, Modify: true, Delete: true, Share: true}
	writes := []authz.Action{authz.ActionUpload, authz.ActionUploadOverwrite, authz.ActionModify, authz.ActionDelete, authz.ActionMove, authz.ActionCopyOverwrite}
	reads := []authz.Action{authz.ActionBrowse, authz.ActionDownload, authz.ActionMintShare, authz.ActionShare}
	for _, admin := range []bool{false, true} {
		for _, src := range []string{"global", "device", "both"} {
			p := authz.Principal{DeviceID: testDeviceID, Admin: admin, Grants: all, ReadOnly: src != "global"}
			pol := authz.Policy{ReadOnly: src != "device"}
			for _, a := range writes {
				d := authz.Decide(p, a, authz.Target{}, pol)
				if d.Allowed || d.Code != authz.CodeReadOnly || !errors.Is(d.Err, authz.ErrReadOnly) {
					t.Errorf("admin=%v %s %v: %+v, want READ_ONLY (auth.go:137, server.go:257)", admin, src, a, d)
				}
			}
			for _, a := range reads {
				if d := authz.Decide(p, a, authz.Target{}, pol); !d.Allowed {
					t.Errorf("admin=%v %s %v: reads and share minting stay allowed, got %+v", admin, src, a, d)
				}
			}
		}
	}
}

// requireFileCapabilities runs before requireWritable (server.go:241-242), so a
// missing grant is reported before read-only.
func TestMissingGrantIsReportedBeforeReadOnly(t *testing.T) {
	p := authz.Principal{DeviceID: testDeviceID, ReadOnly: true}
	d := authz.Decide(p, authz.ActionUpload, authz.Target{}, authz.Policy{ReadOnly: true})
	if d.Code != authz.CodeCapabilityDenied {
		t.Fatalf("code=%q want CAPABILITY_DENIED", d.Code)
	}
}

func TestAdminOnlyActions(t *testing.T) {
	for _, admin := range []bool{false, true} {
		d := authz.Decide(authz.Principal{DeviceID: testDeviceID, Admin: admin}, authz.ActionAdmin, authz.Target{}, authz.Policy{})
		if d.Allowed != admin {
			t.Errorf("admin=%v: allowed=%v", admin, d.Allowed)
		}
		if !admin && (d.Code != authz.CodeForbidden || !errors.Is(d.Err, authz.ErrForbidden)) {
			t.Errorf("non-admin: %+v, want FORBIDDEN (settings_handlers.go:38)", d)
		}
	}
}

func TestSelfOrAdminActions(t *testing.T) {
	cases := []struct {
		name    string
		admin   bool
		target  string
		allowed bool
	}{
		{"self", false, testDeviceID, true},
		{"other device", false, "dev-2", false},
		{"admin other", true, "dev-2", true},
		{"empty target", false, "", false},
	}
	for _, c := range cases {
		d := authz.Decide(authz.Principal{DeviceID: testDeviceID, Admin: c.admin}, authz.ActionSelfOrAdmin, authz.Target{DeviceID: c.target}, authz.Policy{})
		if d.Allowed != c.allowed {
			t.Errorf("%s: allowed=%v want %v", c.name, d.Allowed, c.allowed)
		}
		if !c.allowed && d.Code != authz.CodeForbidden {
			t.Errorf("%s: code=%q want FORBIDDEN (settings_handlers.go:48)", c.name, d.Code)
		}
	}
}

// wolRelayHandler refuses a device flagged read-only (wol_handler.go:30) and
// ignores the global switch and file grants.
func TestWakeOnLAN(t *testing.T) {
	if d := authz.Decide(authz.Principal{DeviceID: testDeviceID}, authz.ActionWakeOnLAN, authz.Target{}, authz.Policy{ReadOnly: true}); !d.Allowed {
		t.Errorf("a device without grants may wake the PC even under global read-only: %+v", d)
	}
	d := authz.Decide(authz.Principal{DeviceID: testDeviceID, ReadOnly: true, Admin: true}, authz.ActionWakeOnLAN, authz.Target{}, authz.Policy{})
	if d.Allowed || d.Code != authz.CodeForbidden {
		t.Errorf("read-only device: %+v, want FORBIDDEN", d)
	}
}

func TestPathsAgainstRootsAndJail(t *testing.T) {
	root := filepath.Join(string(filepath.Separator), "srv", "data")
	inRoot := filepath.Join(root, "a", "b.txt")
	sibling := filepath.Join(string(filepath.Separator), "srv", "data2", "x")
	jail := filepath.Join(root, "alice")
	inJail := filepath.Join(jail, "f")
	outsideJail := filepath.Join(root, "bob", "f")
	other := filepath.Join(string(filepath.Separator), "etc", "passwd")
	dotdot := filepath.Join(jail, "..", "bob", "f") // cleans to outsideJail

	cases := []struct {
		name  string
		roots []string
		jail  string
		paths []string
		want  bool
	}{
		{"no roots no jail allows anything", nil, "", []string{other}, true},
		{"inside root", []string{root}, "", []string{inRoot}, true},
		{"root itself", []string{root}, "", []string{root}, true},
		{"sibling with shared prefix", []string{root}, "", []string{sibling}, false},
		{"outside roots", []string{root}, "", []string{other}, false},
		{"second root", []string{root, filepath.Dir(other)}, "", []string{other}, true},
		{"jail only, inside", nil, jail, []string{inJail}, true},
		{"jail only, outside", nil, jail, []string{outsideJail}, false},
		{"jail and root, inside jail", []string{root}, jail, []string{inJail}, true},
		{"jail and root, in root but outside jail", []string{root}, jail, []string{outsideJail}, false},
		{"jail outside every root denies all", []string{root}, other, []string{other}, false},
		{"jail outside every root denies root too", []string{root}, other, []string{inRoot}, false},
		{"dotdot is cleaned before the check", nil, jail, []string{dotdot}, false},
		{"all paths must pass", []string{root}, "", []string{inRoot, other}, false},
		{"no paths means no path check", []string{root}, jail, nil, true},
	}
	for _, c := range cases {
		for _, admin := range []bool{false, true} {
			p := authz.Principal{DeviceID: testDeviceID, Admin: admin, JailRoot: c.jail, Grants: authz.Grants{Browse: true}}
			d := authz.Decide(p, authz.ActionBrowse, authz.Target{Paths: c.paths}, authz.Policy{Roots: c.roots})
			if d.Allowed != c.want {
				t.Errorf("%s admin=%v: allowed=%v want %v", c.name, admin, d.Allowed, c.want)
			}
			if !c.want && (d.Code != authz.CodeForbidden || !errors.Is(d.Err, authz.ErrForbidden)) {
				t.Errorf("%s: %+v, want FORBIDDEN (jail.go:122)", c.name, d)
			}
		}
	}
}

// Decide must not mutate its inputs: it is called with shared policy slices.
func TestDecideDoesNotMutateInputs(t *testing.T) {
	roots := []string{filepath.Join(string(filepath.Separator), "srv", "..", "srv", "data")}
	before := roots[0]
	p := authz.Principal{DeviceID: testDeviceID, Grants: authz.Grants{Browse: true}}
	authz.Decide(p, authz.ActionBrowse, authz.Target{Paths: []string{before}}, authz.Policy{Roots: roots})
	if roots[0] != before {
		t.Fatalf("Policy.Roots was modified: %q", roots[0])
	}
}

func TestUnknownActionFailsClosed(t *testing.T) {
	d := authz.Decide(authz.Principal{DeviceID: testDeviceID, Admin: true}, authz.Action(-1), authz.Target{}, authz.Policy{})
	if d.Allowed || d.Code != authz.CodeForbidden {
		t.Fatalf("unknown action: %+v, want FORBIDDEN", d)
	}
}

func TestDenialMessagesNameTheMissingGrant(t *testing.T) {
	d := authz.Decide(authz.Principal{DeviceID: testDeviceID}, authz.ActionMove, authz.Target{}, authz.Policy{})
	// auth.go:186: "device lacks <capability> permission"; move checks modify first.
	if got, want := d.Err.Error(), "device lacks modify permission"; got != want {
		t.Fatalf("message %q, want %q", got, want)
	}
}

func TestActionNamesAreDistinct(t *testing.T) {
	seen := map[string]authz.Action{}
	for _, a := range authz.AllActions() {
		n := a.String()
		if n == "" {
			t.Errorf("action %d has no name", int(a))
		}
		if prev, dup := seen[n]; dup {
			t.Errorf("actions %d and %d share the name %q", int(prev), int(a), n)
		}
		seen[n] = a
	}
}
