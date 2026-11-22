package conformance

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

// jsonResult builds a 200 result holding the decoded JSON text.
func jsonResult(t *testing.T, status int, body string) result {
	t.Helper()
	v, err := decodeJSON([]byte(body))
	if err != nil {
		t.Fatal(err)
	}
	return result{Status: status, JSON: v}
}

// jsonErrorResult is jsonResult with the JSON content type header an agent's error reply carries.
func jsonErrorResult(t *testing.T, status int, body string) result {
	t.Helper()
	r := jsonResult(t, status, body)
	r.Headers = map[string]string{"Content-Type": "application/json"}
	return r
}

// TestCompareResultsVerdicts needs no agent: it checks that the differential
// comparison tells identical, named and unexplained differences apart.
func TestCompareResultsVerdicts(t *testing.T) {
	health := func(v int) result {
		return jsonResult(t, 200, fmt.Sprintf(`{"apiVersion":%d,"status":"ok"}`, v))
	}
	cases := []struct {
		name     string
		caseName string
		oracle   result
		got      result
		want     string
	}{
		{"same without a named difference", "fs-list-root", jsonResult(t, 200, `{"a":1}`), jsonResult(t, 200, `{"a":1}`), verdictIdentical},
		{"unnamed difference in an invariant group fails", "bad-token-fs-list", jsonResult(t, 200, `{"a":1}`), jsonResult(t, 200, `{"a":2}`), verdictBeyond},
		{"unnamed difference elsewhere is an improvement", "fs-list-root", jsonResult(t, 200, `{"a":1}`), jsonResult(t, 200, `{"a":2}`), verdictImproved},
		{"named difference observed", "health-owner", health(2), health(3), verdictExplained},
		{"named difference the new agent does not implement is not a failure outside the invariants", "health-owner", health(2), health(2), verdictImproved},
		{"named difference observed in an invariant group", "pair-request-poll-approved",
			jsonResult(t, 200, `{"status":"approved","tailscaleAddress":""}`), jsonResult(t, 200, `{"status":"approved"}`), verdictExplained},
		{"named difference plus another change in an invariant group fails", "pair-request-poll-approved",
			jsonResult(t, 200, `{"status":"approved","tailscaleAddress":""}`), jsonResult(t, 200, `{"status":"rejected"}`), verdictBeyond},
		{"read-only batch refused up front (R1)", "read-only-delete",
			jsonResult(t, 200, `{"results":[{"ok":false,"path":"/x","error":{"code":"READ_ONLY","message":"m"}}]}`),
			jsonErrorResult(t, 403, `{"code":"READ_ONLY","message":"agent is read-only"}`), verdictExplained},
		{"missing device is 404 (D1)", "device-delete-missing",
			result{Status: 204}, jsonErrorResult(t, 404, `{"code":"NOT_FOUND","message":"no such device"}`), verdictExplained},
		{"mask-only difference not seen", "metrics-owner", jsonResult(t, 200, `{"cpu":1}`), jsonResult(t, 200, `{"cpu":1}`), verdictNotSeen},
		{"named difference plus another change", "pair-request-poll-approved",
			jsonResult(t, 200, `{"apiVersion":2,"status":"ok"}`), jsonResult(t, 200, `{"apiVersion":3,"status":"down"}`), verdictBeyond},
		{"status change is never masked", "pair-request-poll-approved", health(2), result{Status: 500, JSON: health(3).JSON}, verdictBeyond},
		{"error wording only (G1)", "fs-list-root",
			jsonResult(t, 404, `{"code":"PATH_NOT_FOUND","message":"file not found"}`),
			jsonResult(t, 404, `{"code":"PATH_NOT_FOUND","message":"path not found"}`), verdictExplained},
		{"error wording rule needs the same code (G1)", "unauth-fs-list",
			jsonResult(t, 404, `{"code":"PATH_NOT_FOUND","message":"x"}`),
			jsonResult(t, 404, `{"code":"NOT_FOUND","message":"x"}`), verdictBeyond},
		{"success replies are not covered by G1", "unauth-fs-list",
			jsonResult(t, 200, `{"message":"a"}`), jsonResult(t, 200, `{"message":"b"}`), verdictBeyond},
		{"error code rewrite", "apps-guest",
			jsonResult(t, 403, `{"code":"APP_VIEW_FORBIDDEN","message":"old words"}`),
			jsonResult(t, 403, `{"code":"CAPABILITY_DENIED","message":"new words"}`), verdictExplained},
		{"error code rewrite is exact", "apps-guest",
			jsonResult(t, 403, `{"code":"APP_VIEW_FORBIDDEN","message":"m"}`),
			jsonResult(t, 403, `{"code":"FORBIDDEN","message":"m"}`), verdictImproved},
		{"shape-only mask ignores values, not keys", "metrics-owner",
			jsonResult(t, 200, `{"cpu":1,"mem":2}`), jsonResult(t, 200, `{"cpu":9,"mem":8}`), verdictExplained},
		{"shape-only mask sees a missing key", "metrics-owner",
			jsonResult(t, 200, `{"cpu":1,"mem":2}`), jsonResult(t, 200, `{"cpu":9}`), verdictImproved},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			before := string(marshalResult(c.oracle))
			got := compareResults(c.caseName, c.oracle, c.got)
			if got.verdict != c.want {
				t.Errorf("verdict = %s, want %s\n%s", got.verdict, c.want, got.diff)
			}
			if after := string(marshalResult(c.oracle)); after != before {
				t.Errorf("compareResults changed its input:\n%s\n%s", before, after)
			}
		})
	}
}

// TestLineDiff checks the diff shows exactly the changed lines with context.
func TestLineDiff(t *testing.T) {
	old := "a\nb\nc\nd\ne\nf\ng\nh\n"
	got := lineDiff(old, "a\nb\nc\nd\nE\nf\ng\nh\n")
	for _, want := range []string{"- e\n", "+ E\n", "  d\n", "  f\n"} {
		if !strings.Contains(got, want) {
			t.Errorf("diff lacks %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "  a\n") {
		t.Errorf("diff shows unchanged lines far from the change:\n%s", got)
	}
	if lineDiff(old, old) != "" {
		t.Errorf("identical texts give a diff: %q", lineDiff(old, old))
	}
}

// TestExpiryNormalization checks a share expiry is reported as the minutes left.
func TestExpiryNormalization(t *testing.T) {
	n := &normalizer{relExpiry: true, now: func() time.Time { return time.Unix(1_000_000, 0) }}
	v, _ := decodeJSON([]byte(`{"expiresAt":1000900}`))
	got := n.walk("", v).(map[string]any)["expiresAt"]
	if got != "<expires in 900s>" {
		t.Errorf("expiresAt = %v", got)
	}
}
