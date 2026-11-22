package api_test

import (
	"net/http"
	"os"
	"testing"

	"github.com/ZaidQamhieh/rfe/agent/internal/next/api"
	"github.com/ZaidQamhieh/rfe/agent/internal/next/audit"
	"github.com/ZaidQamhieh/rfe/agent/internal/next/authz"
)

// Values of the tests that pin agent-next to the old agent's answers.
const (
	headerReferrerName  = "Referrer-Policy"
	headerReferrerWant  = "no-referrer"
	missingDeviceID     = "00000000-0000-4000-8000-00000000dead"
	detailLaunchDenied  = "outcome=permission_denied"
	copyDestinationName = "dest"
)

// TestEveryAnswerCarriesReferrerPolicy checks the header on a public success, an
// authentication failure, a missing route and a wrong method.
func TestEveryAnswerCarriesReferrerPolicy(t *testing.T) {
	r := newRig(t, rigOptions{})
	for name, res := range map[string]result{
		"public success": r.do("GET", "/health", "", nil),
		"unauthorized":   r.do("GET", "/status", "", nil),
		"no such route":  r.do("GET", "/no/such/route", "", nil),
		"wrong method":   r.do("PUT", "/health", "", nil),
	} {
		if got := res.Header().Get(headerReferrerName); got != headerReferrerWant {
			t.Errorf("%s (%d): %s = %q, want %q", name, res.Code, headerReferrerName, got, headerReferrerWant)
		}
	}
}

// TestDeleteOfAMissingDeviceIsNotFoundAndNotAudited checks that revoking or
// purging a device that does not exist answers 404 NOT_FOUND and records no
// audit entry for it: nothing was revoked or removed.
func TestDeleteOfAMissingDeviceIsNotFoundAndNotAudited(t *testing.T) {
	h := newHostRig(t, nil)
	for _, query := range []string{"", "?purge=true"} {
		h.do("DELETE", "/devices/"+missingDeviceID+query, h.admin, nil).wantError(t, http.StatusNotFound, api.CodeNotFound)
	}
	for _, e := range h.auditEntries() {
		if e.Target == missingDeviceID {
			t.Errorf("audit entry for a device that does not exist: %+v", e)
		}
	}
	// A device that may not touch another device is still refused before the lookup.
	h.do("DELETE", "/devices/"+missingDeviceID, h.dev, nil).wantError(t, http.StatusForbidden, authz.CodeForbidden)
}

// TestRefusedAppLaunchIsAudited checks that a launch the device has no grant
// for answers CAPABILITY_DENIED and is recorded as app_launch.
func TestRefusedAppLaunchIsAudited(t *testing.T) {
	h := newHostRig(t, nil)
	h.do("POST", "/apps/"+appID+"/launch", h.dev, nil).wantError(t, http.StatusForbidden, authz.CodeCapabilityDenied)
	for _, e := range h.auditEntries() {
		if e.Action == audit.ActionAppLaunch && e.Target == appID && e.Detail == detailLaunchDenied {
			return
		}
	}
	t.Fatalf("no app_launch entry for the refused launch in %+v", h.auditEntries())
}

// TestReadOnlyRefusesBatchRoutesUpFront checks that delete, copy, move, trash
// restore and trash delete in a read-only agent (global or per device) are
// refused as a whole with one 403 READ_ONLY, and nothing changes on disk.
func TestReadOnlyRefusesBatchRoutesUpFront(t *testing.T) {
	f := newFileRig(t, 0)
	src := f.put("a.txt", "a")
	phoneTok, phoneID := f.pairDevice(newPhone("phone-1", 2))
	if res := f.do("PATCH", "/devices/"+phoneID, f.admin, map[string]any{"modify": true, "delete": true, "readOnly": true}); res.Code != http.StatusOK {
		t.Fatalf("patch = %d %s", res.Code, res.Body.String())
	}
	dest := f.path(copyDestinationName)
	if err := os.Mkdir(dest, 0o755); err != nil {
		t.Fatal(err)
	}
	f.set.setReadOnly(true) // global read-only; the phone is read-only itself as well

	for _, tok := range []string{f.admin, phoneTok} {
		for name, call := range map[string]func() result{
			"delete":           func() result { return f.do("DELETE", "/fs"+q("path", src), tok, nil) },
			"delete permanent": func() result { return f.do("DELETE", "/fs"+q("path", src, "permanent", "true"), tok, nil) },
			"copy": func() result {
				return f.do("POST", "/fs/copy", tok, map[string]any{"sources": []string{src}, "destDir": dest})
			},
			"copy overwrite": func() result {
				return f.do("POST", "/fs/copy", tok, map[string]any{"sources": []string{src}, "destDir": dest, "overwrite": true})
			},
			"move": func() result {
				return f.do("POST", "/fs/move", tok, map[string]any{"sources": []string{src}, "destDir": dest})
			},
			"restore":      func() result { return f.do("POST", "/trash/restore", tok, map[string][]string{"ids": {"missing-id"}}) },
			"trash delete": func() result { return f.do("DELETE", "/trash", tok, map[string][]string{"ids": {"missing-id"}}) },
			"single write": func() result { return f.do("POST", "/fs/folder", tok, map[string]string{"path": f.path("x")}) },
		} {
			t.Run(name, func(t *testing.T) { call().wantError(t, http.StatusForbidden, authz.CodeReadOnly) })
		}
	}
	if _, err := os.Stat(src); err != nil {
		t.Fatalf("a read-only batch changed the source: %v", err)
	}
	if left, _ := os.ReadDir(dest); len(left) != 0 {
		t.Fatalf("a read-only batch wrote into the destination: %v", left)
	}
	// Without the grant the guard still refuses the whole request.
	f.set.setReadOnly(false)
	tok, _ := f.pairDevice(newPhone("phone-2", 3))
	f.do("DELETE", "/fs"+q("path", src), tok, nil).wantError(t, http.StatusForbidden, authz.CodeCapabilityDenied)
}

// TestActiveNowCountsOnlySessionsThatReceivedAChunk checks the old definition:
// a session that was just opened but has no chunk yet is not active now.
func TestActiveNowCountsOnlySessionsThatReceivedAChunk(t *testing.T) {
	x := newXferRig(t, nil)
	data := xferPayload(xferSize)
	s := x.open(x.admin, "idle.bin", data, false)
	var l api.TransferListJSON
	x.do(http.MethodGet, "/transfers/list", x.admin, nil).json(t, &l)
	if l.ActiveNow != 0 || l.Counts["open"] != 1 {
		t.Fatalf("freshly opened: activeNow = %d counts = %v, want 0 and one open", l.ActiveNow, l.Counts)
	}
	x.put(x.admin, s.ID, data, 0)
	x.do(http.MethodGet, "/transfers/list", x.admin, nil).json(t, &l)
	if l.ActiveNow != 1 {
		t.Fatalf("after a chunk: activeNow = %d, want 1", l.ActiveNow)
	}
}
