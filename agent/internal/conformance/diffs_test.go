package conformance

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"strings"
	"testing"
)

// messageMask replaces error message text when a case is compared under a
// named v3 difference: the rewrite may word its messages differently, only the
// codes and shapes are the contract.
const messageMask = "<message>"

// statusImproved is the Status of a documented intended difference: the new
// agent behaves better than the old one there, and keeps doing so.
const statusImproved = "improved"

// statusUnchanged is the status of an AlreadyOnOld entry: the old agent
// already behaves as the new one does.
const statusUnchanged = "unchanged"

// v3Diff names one intended difference between the current agent (the
// goldens, or the live oracle in a differential run) and a rewritten agent
// reporting apiVersion 3. The old response is first rewritten by Expect into
// what the new agent must give (messages are then masked); Mask then reduces
// both sides to the part the contract fixes (for example a status and a
// content type, or only the JSON shape). AlreadyOnOld marks a case where
// today's agent already behaves as v3 will; it is kept so a v3 run is still
// held to it, and has neither Expect nor Mask. Status is statusImproved (the
// default) for a documented improvement, statusUnchanged for AlreadyOnOld.
type v3Diff struct {
	ID           string
	Case         string
	Why          string
	Status       string
	AlreadyOnOld bool
	Expect       func(r *result)
	Mask         func(r *result)
}

// status returns the entry's Status, defaulting as the v3Diff comment says.
func (d v3Diff) status() string {
	switch {
	case d.Status != "":
		return d.Status
	case d.AlreadyOnOld:
		return statusUnchanged
	}
	return statusImproved
}

// forCases repeats one difference for several cases, keeping its ID, reason
// and functions.
func forCases(d v3Diff, cases ...string) []v3Diff {
	out := make([]v3Diff, 0, len(cases))
	for _, c := range cases {
		d.Case = c
		out = append(out, d)
	}
	return out
}

// joinDiffs concatenates difference lists in order.
func joinDiffs(lists ...[]v3Diff) []v3Diff {
	var out []v3Diff
	for _, l := range lists {
		out = append(out, l...)
	}
	return out
}

// expectedV3Diffs is the table of documented intended differences, each with a
// reason and a status (see v3Diff). The new agent is not bent to match the
// old one: where it behaves better it stays. The old agent is the oracle only
// for the invariant groups (pairing, tofu, auth, jail, transfer; see
// groups_test.go), and only a difference in one of those that is not named here
// fails the run. A difference in any other group is an improvement: it is
// reported, and should get an entry here with its reason, but never fails.
//
//   - C1: DELETE /fs of a path that does not exist must report ok=false with
//     PATH_NOT_FOUND for that item. Today's agent does so for the default
//     move-to-trash delete but answers ok=true for permanent=true.
//
//   - C2: GET /transfers/{id}, chunk and complete are limited to the opening
//     device or an admin; any other device gets 404. Today's agent already does
//     this (see the *-other-uploader cases); the *-other-device cases stop at the
//     missing upload grant with 403 before the owner check, and stay that way.
//
//   - C8: apiVersion reported by an authenticated /v1/health becomes 3; the
//     host's listen, LAN and Tailscale addresses and MAC are not compared.
//
//   - G1 (applies to every case, differential runs only): the wording of an
//     error "message", including results[].error.message of a batch reply, is
//     not part of the contract, so a reply that differs from the oracle's only
//     in message text (same status, same code, same other fields) is reported
//     as explained. It is a rule, not a table entry.
//
//   - A1: a device without the apps grants, or with app access off, gets 403
//     CAPABILITY_DENIED instead of APP_VIEW_FORBIDDEN / APP_LAUNCH_FORBIDDEN
//     (protocol/compat.md).
//
//   - W1: POST /wol with a bad or missing address is 400 BAD_REQUEST instead
//     of INVALID_MAC / INVALID_BODY. (A valid address whose send fails is
//     500 INTERNAL instead of WOL_FAILED; no case sends one, because it would
//     broadcast on the LAN.)
//
//   - W2: POST /wol does not check the per-device `wake` grant: the rewrite's
//     store has no per-device grants at all (every paired device is treated
//     alike), whereas the current agent answers 403 CAPABILITY_DENIED to a
//     device without the grant (rfe-8ddd.15). It is a rule, not a table entry:
//     no case pairs a device without the grant, and the rewrite must add the
//     grant to its device model before it can enforce it (rfe-8ddd.23).
//
//   - L1/L2: log text is implementation specific, so only the JSON shape of
//     GET /logs is compared; the rewrite also honours the limit parameter.
//
//   - M1: /metrics numbers are live host readings; only the shape is compared.
//
//   - T1: thumbnails are rendered at bucketed sizes, so the bytes and length
//     differ; the status and content type are still compared.
//
//   - S1: a share link minted without expiresInSeconds lives 15 minutes; the
//     contract says so too, so the current agent already conforms.
//
//   - Q1: the rewrite also searches file contents, so a content query finds
//     files the current agent does not; the rest of the reply is compared.
//
//   - C3: DELETE /trash of an id that is not in the trash is 404 PATH_NOT_FOUND
//     instead of an idempotent 200 {"ok":true}.
//
//   - S2: POST /share/mint of a missing file is 404 PATH_NOT_FOUND instead of
//     NOT_FOUND; of a directory it is 422 NOT_A_FILE, as GET /content answers
//     (the current agent already answers so).
//
//   - F1: copy and move of a missing source fail the item with PATH_NOT_FOUND
//     instead of COPY_FAILED / CONFLICT; compress of a missing source is 404
//     PATH_NOT_FOUND instead of 500 INTERNAL.
//
//   - F2: moving a folder into itself fails the item with CONFLICT instead of
//     MOVE_FAILED.
//
//   - B1: a negative bandwidth limit is 400 BAD_REQUEST instead of being
//     accepted and stored as 0.
//
//   - E1: the per-item FORBIDDEN for a destination outside the root says
//     "forbidden" without the path; only the wording differs.
//
//   - H1: GET /v1/status adds apiVersion, the listen and LAN addresses, MAC,
//     Tailscale address, fingerprint, name, counts and sidecar state; the host
//     values are not compared and the added keys are dropped.
//
//   - P1: the approved pair-request poll omits the optional tailscaleAddress
//     (the current agent sends "").
//
//   - R1: a request that cannot succeed because the agent (or the device) is
//     read-only is refused up front with one 403 READ_ONLY, and nothing is
//     touched; the old agent answered 200 with a READ_ONLY result per path on
//     DELETE /fs, copy, move and trash restore. (Improved.)
//
//   - D1: DELETE /devices/{id} of a device that does not exist is 404
//     NOT_FOUND, so a caller can tell; the old agent answered 204 and wrote an
//     audit entry for nothing. (Improved.)
//
// New real differences found by a run belong in /tmp differences.md first and
// are added here only when the owner accepts them as the new contract.
var expectedV3Diffs = joinDiffs(
	[]v3Diff{
		{
			ID: "C1", Case: "fs-delete-missing-permanent",
			Why:    "permanent delete of a missing path: item ok=false PATH_NOT_FOUND instead of ok=true",
			Expect: func(r *result) { markBatchItemNotFound(r, 0) },
		},
		{
			ID: "C1", Case: "fs-delete-batch-body",
			Why:    "in a batch, the missing path (item 1) is ok=false PATH_NOT_FOUND; the existing one stays ok=true",
			Expect: func(r *result) { markBatchItemNotFound(r, 1) },
		},
		{
			ID: "C2", Case: "upload-status-other-uploader", AlreadyOnOld: true,
			Why: "another device with upload rights reading a session it did not open gets 404",
		},
		{
			ID: "C2", Case: "upload-chunk-other-uploader", AlreadyOnOld: true,
			Why: "another device with upload rights sending a chunk to a session it did not open gets 404",
		},
		{
			ID: "C2", Case: "upload-complete-other-uploader", AlreadyOnOld: true,
			Why: "another device with upload rights completing a session it did not open gets 404",
		},
		{
			ID: "C3", Case: "trash-delete-one-missing",
			Why:    "deleting a trash id that is not in the trash is 404 PATH_NOT_FOUND instead of an idempotent ok:true",
			Expect: func(r *result) { becomesError(r, http.StatusNotFound, "PATH_NOT_FOUND") },
		},
		{
			ID: "S2", Case: "share-mint-missing",
			Why:    "minting a link to a missing file is PATH_NOT_FOUND instead of NOT_FOUND",
			Expect: func(r *result) { rewriteErrorCode(r, "PATH_NOT_FOUND", "NOT_FOUND") },
		},
		{
			ID: "S2", Case: "share-mint-directory", AlreadyOnOld: true,
			Why: "minting a link to a directory is 422 NOT_A_FILE, as GET /content answers",
		},
		{
			ID: "F1", Case: "fs-copy-missing-source",
			Why:    "a copy whose source is missing fails the item with PATH_NOT_FOUND instead of COPY_FAILED",
			Expect: func(r *result) { rewriteItemErrorCode(r, "PATH_NOT_FOUND", "COPY_FAILED") },
		},
		{
			ID: "F1", Case: "fs-move-missing-source",
			Why:    "a move whose source is missing fails the item with PATH_NOT_FOUND instead of CONFLICT",
			Expect: func(r *result) { rewriteItemErrorCode(r, "PATH_NOT_FOUND", "CONFLICT") },
		},
		{
			ID: "F1", Case: "fs-compress-missing-source",
			Why:    "compressing a missing source is 404 PATH_NOT_FOUND instead of 500 INTERNAL",
			Expect: func(r *result) { becomesError(r, http.StatusNotFound, "PATH_NOT_FOUND") },
		},
		{
			ID: "F2", Case: "fs-move-into-itself",
			Why:    "moving a folder into itself fails the item with CONFLICT instead of MOVE_FAILED",
			Expect: func(r *result) { rewriteItemErrorCode(r, "CONFLICT", "MOVE_FAILED") },
		},
		{
			ID: "B1", Case: "bandwidth-put-negative",
			Why:    "a negative bandwidth limit is 400 BAD_REQUEST instead of being accepted and stored as 0",
			Expect: func(r *result) { becomesError(r, http.StatusBadRequest, "BAD_REQUEST") },
		},
		{
			ID: "E1", Case: "escape-copy-out",
			Why:    "the per-item FORBIDDEN for a destination outside the root says \"forbidden\" without the path; the code is kept",
			Expect: func(r *result) { maskItemErrorMessages(r.JSON) },
		},
		{
			ID: "P1", Case: "pair-request-poll-approved",
			Why:    "after approval the poll reply omits the optional tailscaleAddress (the current agent sends \"\")",
			Expect: dropJSONKeys("tailscaleAddress"),
		},
		{
			ID: "H1", Case: "status-owner",
			Why:  "status adds apiVersion, the listen and LAN addresses, MAC, Tailscale address, fingerprint, name, counts and sidecar state; host values are not compared and the added keys are dropped",
			Mask: dropJSONKeys("address", "apiVersion", "counts", "fingerprint", "macAddress", "name", "sidecars", "tailscaleAddress"),
		},
		{
			ID: "L2", Case: "logs-limit",
			Why:  "the rewrite honours ?limit on /logs; log text itself is implementation specific, so only the shape is compared",
			Mask: shapeOnly,
		},
		{
			ID: "S1", Case: "share-mint-default-expiry", AlreadyOnOld: true,
			Why: "a share link minted without expiresInSeconds lives 15 minutes, as the contract says",
		},
		{
			ID: "R1", Case: "read-only-delete", Status: statusImproved,
			Why:    "a delete in a read-only agent is one 403 READ_ONLY and touches nothing, instead of 200 with a READ_ONLY result for the path",
			Expect: func(r *result) { becomesError(r, http.StatusForbidden, "READ_ONLY") },
		},
		{
			ID: "D1", Case: "device-delete-missing", Status: statusImproved,
			Why:    "deleting a device that does not exist is 404 NOT_FOUND instead of 204, so the caller learns the device was never there",
			Expect: func(r *result) { becomesError(r, http.StatusNotFound, "NOT_FOUND") },
		},
		{
			ID: "Q1", Case: "search-content",
			Why:  "the rewrite also searches file contents, so the list of hits differs; status and reply keys are still compared",
			Mask: dropJSONKeys("items", "results", "hits", "total", "count", "truncated"),
		},
	},
	forCases(v3Diff{
		ID:     "C8",
		Why:    "apiVersion reported by an authenticated /v1/health becomes 3; the listen and LAN addresses, MAC and Tailscale address are host values, so they are not compared",
		Expect: func(r *result) { setJSONField(r, "apiVersion", json.Number("3")) },
		Mask:   dropJSONKeys("address", "tailscaleAddress", "macAddress"),
	}, "health-owner", "read-only-health"),
	forCases(v3Diff{
		ID:  "A1",
		Why: "app denial is 403 CAPABILITY_DENIED instead of APP_VIEW_FORBIDDEN / APP_LAUNCH_FORBIDDEN",
		Expect: func(r *result) {
			rewriteErrorCode(r, "CAPABILITY_DENIED", "APP_VIEW_FORBIDDEN", "APP_LAUNCH_FORBIDDEN")
		},
	}, "apps-guest", "apps-launch-guest", "apps-owner", "apps-launch-unknown"),
	forCases(v3Diff{
		ID:  "W1",
		Why: "a bad or missing wake-on-LAN address is 400 BAD_REQUEST instead of INVALID_MAC / INVALID_BODY",
		Expect: func(r *result) {
			rewriteErrorCode(r, "BAD_REQUEST", "INVALID_MAC", "INVALID_BODY")
		},
	}, "wol-bad-mac", "wol-empty-body"),
	forCases(v3Diff{
		ID:   "L1",
		Why:  "log text is implementation specific; only the JSON shape of GET /logs is compared",
		Mask: shapeOnly,
	}, "logs-owner"),
	forCases(v3Diff{
		ID:   "M1",
		Why:  "metrics are live host readings; only the JSON shape is compared",
		Mask: shapeOnly,
	}, "metrics-owner"),
	forCases(v3Diff{
		ID:   "T1",
		Why:  "thumbnails are rendered at bucketed sizes, so the image bytes and length differ; status and content type are compared",
		Mask: dropBody,
	}, "thumb-png", "thumb-png-large", "thumb-png-odd-size"),
)

// v3DiffFor returns the named difference for a case, or nil.
func v3DiffFor(caseName string) *v3Diff {
	for i := range expectedV3Diffs {
		if expectedV3Diffs[i].Case == caseName {
			return &expectedV3Diffs[i]
		}
	}
	return nil
}

// applyDiff rewrites the oracle's result into what the new agent must give
// and masks both sides as the difference says. A nil difference changes nothing.
func applyDiff(d *v3Diff, oracle, got *result) {
	if d == nil {
		return
	}
	if d.Expect != nil {
		d.Expect(oracle)
		maskMessages(oracle.JSON)
		maskMessages(got.JSON)
	}
	if d.Mask != nil {
		d.Mask(oracle)
		d.Mask(got)
	}
}

// setJSONField sets a top-level key of a JSON object body.
func setJSONField(r *result, key string, value any) {
	if m, ok := r.JSON.(map[string]any); ok {
		m[key] = value
	}
}

// rewriteErrorCode changes the top-level "code" of an error body from any of
// the listed codes to code; other codes are left alone.
func rewriteErrorCode(r *result, code string, from ...string) {
	m, _ := r.JSON.(map[string]any)
	cur, _ := m["code"].(string)
	for _, f := range from {
		if cur == f {
			m["code"] = code
			return
		}
	}
}

// becomesError makes r an error reply with the given status and code and a
// masked message, for a case where the oracle answered with another status.
func becomesError(r *result, status int, code string) {
	r.Status = status
	if r.Headers == nil {
		r.Headers = map[string]string{}
	}
	r.Headers["Content-Type"] = "application/json"
	r.JSON = map[string]any{"code": code, "message": messageMask}
}

// itemErrorObjects returns the "error" object of each item of a batch reply ({"results":[...]}).
func itemErrorObjects(v any) []map[string]any {
	m, _ := v.(map[string]any)
	list, _ := m["results"].([]any)
	var out []map[string]any
	for _, item := range list {
		im, _ := item.(map[string]any)
		if eo, ok := im["error"].(map[string]any); ok {
			out = append(out, eo)
		}
	}
	return out
}

// hasItemErrors reports whether a batch reply has at least one per-item error.
func hasItemErrors(v any) bool {
	return len(itemErrorObjects(v)) > 0
}

// rewriteItemErrorCode changes the code of each per-item error from any of the
// listed codes to code; other codes are left alone.
func rewriteItemErrorCode(r *result, code string, from ...string) {
	for _, eo := range itemErrorObjects(r.JSON) {
		cur, _ := eo["code"].(string)
		for _, f := range from {
			if cur == f {
				eo["code"] = code
			}
		}
	}
}

// maskItemErrorMessages replaces the message of each per-item error with messageMask.
func maskItemErrorMessages(v any) {
	for _, eo := range itemErrorObjects(v) {
		if _, isStr := eo["message"].(string); isStr {
			eo["message"] = messageMask
		}
	}
}

// markBatchItemNotFound rewrites item i of a batch reply ({"results":[...]})
// to the failure form {"ok":false,"error":{"code":"PATH_NOT_FOUND",...}}.
func markBatchItemNotFound(r *result, i int) {
	m, _ := r.JSON.(map[string]any)
	list, _ := m["results"].([]any)
	if i >= len(list) {
		return
	}
	if item, ok := list[i].(map[string]any); ok {
		item["ok"] = false
		item["error"] = map[string]any{"code": "PATH_NOT_FOUND", "message": messageMask}
	}
}

// maskMessages replaces every "message" string in a decoded JSON value with messageMask.
func maskMessages(v any) {
	switch x := v.(type) {
	case map[string]any:
		for k, child := range x {
			if _, isStr := child.(string); isStr && k == "message" {
				x[k] = messageMask
				continue
			}
			maskMessages(child)
		}
	case []any:
		for _, child := range x {
			maskMessages(child)
		}
	}
}

// skeleton reduces a decoded JSON value to its shape: scalars become their
// type name, objects keep their keys, arrays keep one element's shape (or
// "empty").
func skeleton(v any) any {
	switch x := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, child := range x {
			out[k] = skeleton(child)
		}
		return out
	case []any:
		if len(x) == 0 {
			return "empty"
		}
		return []any{skeleton(x[0])}
	case string:
		return "string"
	case json.Number:
		return "number"
	case bool:
		return "bool"
	case nil:
		return "null"
	}
	return "other"
}

// shapeOnly keeps the status and headers and reduces the body to its shape.
func shapeOnly(r *result) {
	if r.JSON != nil {
		r.JSON = skeleton(r.JSON)
		return
	}
	dropBody(r)
}

// dropBody removes the body (and its length and hash) and the Accept-Ranges
// header that goes with a sized body, keeping the status and the other headers.
func dropBody(r *result) {
	r.JSON, r.Text, r.BodyLen, r.BodySHA256 = nil, "", 0, ""
	delete(r.Headers, "Accept-Ranges")
}

// dropJSONKeys returns a mask that deletes the listed top-level keys of a JSON object body.
func dropJSONKeys(keys ...string) func(*result) {
	return func(r *result) {
		if m, ok := r.JSON.(map[string]any); ok {
			for _, k := range keys {
				delete(m, k)
			}
		}
	}
}

// TestExpectedV3DiffsAreWellFormed needs no agent. Every named difference must
// name a case in the table, appear once per case, carry a reason, and either
// change that case's golden, mask it, or say the old agent already conforms.
func TestExpectedV3DiffsAreWellFormed(t *testing.T) {
	known := map[string]bool{}
	for _, n := range caseNames() {
		known[n] = true
	}
	seen := map[string]bool{}
	for _, d := range expectedV3Diffs {
		if d.ID == "" || d.Why == "" || d.Case == "" {
			t.Errorf("diff %+v needs ID, Case and Why", d)
			continue
		}
		if !known[d.Case] {
			t.Errorf("%s names case %q, which is not in the request table", d.ID, d.Case)
		}
		if seen[d.Case] {
			t.Errorf("case %q has more than one named difference", d.Case)
		}
		seen[d.Case] = true
		if d.status() != statusImproved && d.status() != statusUnchanged || d.AlreadyOnOld != (d.status() == statusUnchanged) {
			t.Errorf("%s/%s: status %q must be %q, or %q for an AlreadyOnOld entry", d.ID, d.Case, d.status(), statusImproved, statusUnchanged)
		}
		if d.AlreadyOnOld == (d.Expect != nil || d.Mask != nil) {
			t.Errorf("%s/%s: set AlreadyOnOld, or Expect and/or Mask, not both and not neither", d.ID, d.Case)
			continue
		}
		if d.Expect == nil || *update {
			continue
		}
		before, err := loadGolden(d.Case)
		if errors.Is(err, os.ErrNotExist) {
			t.Errorf("%s/%s: no golden file yet; run -update", d.ID, d.Case)
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		after, _ := loadGolden(d.Case)
		d.Expect(&after)
		if string(marshalResult(before)) == string(marshalResult(after)) {
			t.Errorf("%s/%s: Expect leaves the golden unchanged, so it names no difference", d.ID, d.Case)
		}
	}
}

// TestDiffIDsAreDocumented fails when a difference ID is not explained in the
// expectedV3Diffs comment, so a new entry cannot go in without its reason there.
func TestDiffIDsAreDocumented(t *testing.T) {
	b, err := os.ReadFile("diffs_test.go")
	if err != nil {
		t.Fatal(err)
	}
	var listed strings.Builder
	for _, line := range strings.Split(string(b), "\n") {
		if strings.HasPrefix(line, "//   - ") {
			listed.WriteString(line[:strings.Index(line, ":")+1] + "\n")
		}
	}
	for _, d := range expectedV3Diffs {
		if !strings.Contains(listed.String(), d.ID) {
			t.Errorf("difference %s has no entry in the expectedV3Diffs comment", d.ID)
		}
	}
}
