package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// getMetrics calls the metrics handler as an admin and returns the raw body and its decoded form.
func getMetrics(t *testing.T, h http.HandlerFunc) (string, map[string]any) {
	t.Helper()
	rr := httptest.NewRecorder()
	h(rr, asAdmin(httptest.NewRequest(http.MethodGet, "/v1/metrics", nil)))
	if rr.Code != http.StatusOK {
		t.Fatalf("metrics: %d %s", rr.Code, rr.Body.String())
	}
	var out map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &out); err != nil {
		t.Fatalf("metrics body: %v (%s)", err, rr.Body.String())
	}
	return rr.Body.String(), out
}

// walkLeaves calls fn for every scalar in a decoded JSON value, with its dotted key path.
func walkLeaves(prefix string, v any, fn func(path string, v any)) {
	if m, ok := v.(map[string]any); ok {
		for k, child := range m {
			walkLeaves(prefix+"."+k, child, fn)
		}
		return
	}
	fn(prefix, v)
}

// TestMetricsReportsTransferCountersFromHandlers drives the real complete handler into each
// transfer failure mode it can reach over HTTP and reads the counters back from /v1/metrics:
// the idempotent replay and a complete on a failed row, and a HASH_MISMATCH with every chunk received.
// The admin-only tier of the route is covered by TestRouteTierMatrix (route_tier_matrix_test.go).
func TestMetricsReportsTransferCountersFromHandlers(t *testing.T) {
	tm, ops := newTestTransferManager(t)
	h := metricsHandler(tm)
	call := completeTransferHandler(tm, ops)
	owner := idemDevice("owner-secret-device")

	_, m := getMetrics(t, h)
	tr, ok := m["transfers"].(map[string]any)
	if !ok {
		t.Fatalf("no transfers object in %v", m)
	}
	for _, k := range []string{"completeOnTerminalSession", "hashMismatchAllChunksReceived", "sessionsResetByReconcile",
		"publishTempFoundInDest", "enospcWriteChunk", "enospcPublish", "openSessions", "oldestOpenSessionAgeSeconds"} {
		if v, ok := tr[k].(float64); !ok || v != 0 {
			t.Fatalf("fresh %s = %v, want 0", k, tr[k])
		}
	}

	content := []byte("the bytes of a file whose complete ack was lost")
	target := filepath.Join(t.TempDir(), "secret-folder", "out.bin")
	id := uuid.New().String()
	if _, err := tm.OpenSession(id, target, int64(len(content)), 1024, sha256hex(content), false, "owner-secret-device"); err != nil {
		t.Fatal(err)
	}
	if err := tm.WriteChunk(id, 0, content, sha256hex(content)); err != nil {
		t.Fatal(err)
	}
	if rr := postComplete(t, id, owner, call); rr.Code != http.StatusOK {
		t.Fatalf("complete: %d %s", rr.Code, rr.Body.String())
	}
	if rr := postComplete(t, id, owner, call); rr.Code != http.StatusOK {
		t.Fatalf("replay: %d %s", rr.Code, rr.Body.String())
	}

	bad := uuid.New().String()
	wrong := []byte("not what the session promised, same length!!!!!")
	if len(wrong) != len(content) {
		t.Fatalf("fixture lengths differ: %d vs %d", len(wrong), len(content))
	}
	if _, err := tm.OpenSession(bad, filepath.Join(t.TempDir(), "bad.bin"), int64(len(content)), 1024, sha256hex(content), false, "owner-secret-device"); err != nil {
		t.Fatal(err)
	}
	if err := tm.WriteChunk(bad, 0, wrong, sha256hex(wrong)); err != nil {
		t.Fatal(err)
	}
	if rr := postComplete(t, bad, owner, call); rr.Code != http.StatusUnprocessableEntity {
		t.Fatalf("mismatching complete: %d %s", rr.Code, rr.Body.String())
	}
	if rr := postComplete(t, bad, owner, call); rr.Code != http.StatusConflict {
		t.Fatalf("complete on failed row: %d %s", rr.Code, rr.Body.String())
	}

	// One open session left to show in the gauge.
	if _, err := tm.OpenSession(uuid.New().String(), filepath.Join(t.TempDir(), "open.bin"), 4, 4, sha256hex([]byte("abcd")), false, "owner-secret-device"); err != nil {
		t.Fatal(err)
	}

	body, m := getMetrics(t, h)
	tr = m["transfers"].(map[string]any)
	want := map[string]float64{
		"completeOnTerminalSession":     2, // the replay of a completed row and the complete on a failed row
		"hashMismatchAllChunksReceived": 1,
		"openSessions":                  1,
	}
	for k, v := range want {
		if tr[k] != v {
			t.Errorf("%s = %v, want %v (body %s)", k, tr[k], v, body)
		}
	}

	// Metrics carry numbers only: no path, device label, session id or temp name can appear in them.
	walkLeaves("", m, func(path string, v any) {
		if _, isNum := v.(float64); !isNum {
			t.Errorf("metric %s is %T (%v), want a number", path, v, v)
		}
	})
	for _, secret := range []string{target, "secret-folder", "owner-secret-device", id, bad, ".rfe-upload-", ".tmp", string(filepath.Separator)} {
		if strings.Contains(body, secret) {
			t.Errorf("metrics body contains %q: %s", secret, body)
		}
	}
}
