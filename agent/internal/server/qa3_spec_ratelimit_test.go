package server

// Target: agent/internal/server, the 429 responses that the contract documents, driven through New(...).
// Each limit below is taken from protocol/openapi.yaml, with its threshold and scope where the contract
// states them. Where the contract gives no figure, the test is marked `proposed policy`.
//
// Requirements covered (the brief's bullets were not numbered, so they are numbered here):
//   B5a  RESOURCE_LIMIT per device: at most 4 open sessions per device. The 5th is 429, with no
//        Retry-After. Another device is unaffected. Deleting a session frees a slot.
//   B5b  RESOURCE_LIMIT host-wide: at most 16 open sessions across the host.
//   B5c  RESOURCE_LIMIT bytes: 64 GiB per device and 128 GiB across the host. Exactly at the limit is
//        admitted, one more byte is refused.
//   B5d  CHUNK_BUSY: when the agent holds as many chunk bodies in memory as it allows, a chunk gets 429
//        with an integer Retry-After. The limit is global. It clears when the capacity is freed.
//        proposed policy: the contract gives no figure for the in-memory capacity, so the test takes
//        the whole configured chunk budget at run time and does not assume a number.
//   B5e  /v1/auth/challenge: 30 per minute per source (contract 342-343), 31st is 429 RATE_LIMITED.
//        Another source is unaffected, and it recovers after the window.
//   B5f  /v1/pair: 10 per minute (contract 393-396), 11th attempt from one source is 429 RATE_LIMITED.
//
// Sources read (line numbers in protocol/openapi.yaml):
//   342-343   /auth/challenge 429: "Too many challenge requests (rate-limited at 30/min). code=RATE_LIMITED."
//   393-396   /pair 429: "Too many pairing attempts (rate-limited at 10/min per agent, since the pairing code
//             is short and brute-forceable). code=RATE_LIMITED."
//   1262-1269 POST /transfers: "at most 4 open sessions and 64 GiB per device, and 16 open sessions and 128 GiB
//             across the host. Reaching either session or byte quota returns 429 RESOURCE_LIMIT."
//   1331-1336 POST /transfers 429: "No Retry-After is supplied because capacity can become available as soon as
//             another upload finishes. code=RESOURCE_LIMIT."
//   1417-1421 PUT chunk 429: "The agent is already holding as many chunk bodies in memory as it allows. Retry
//             after the `Retry-After` seconds. code=CHUNK_BUSY." (Retry-After is an integer header.)
//   2570-2573 RESOURCE_LIMIT: "no fixed Retry-After is provided."
//   2577      CHUNK_BUSY: "too many chunk bodies are in flight; retry after `Retry-After`."
//   docs/security-route-matrix.md 10: "Challenge/pair/login have per-source rate limits with a 4,096-key cap and
//             shared overflow window."
//   docs/host-setup.md 200-205: 4 sessions and 64 GiB per device, 16 sessions and 128 GiB per host.
//
// Run (from agent/): go test ./internal/server -run 'TestQA3_RATE' -count=1 -v
// Runtime: the challenge test waits 61 seconds for the 1-minute window to pass.

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ZaidQamhieh/rfe/agent/internal/pairing"
	"github.com/ZaidQamhieh/rfe/agent/internal/store"
	"github.com/ZaidQamhieh/rfe/agent/internal/transfer"
)

// Upload limits from openapi.yaml 1266-1269 and docs/host-setup.md 200-205.
const (
	qa3LChunkSize      = 16
	qa3LDeviceSessions = 4  // "at most 4 open sessions ... per device"
	qa3LHostSessions   = 16 // "16 open sessions ... across the host"
)

const (
	qa3LDeviceBytes int64 = 64 << 30  // "64 GiB per device"
	qa3LHostBytes   int64 = 128 << 30 // "128 GiB across the host"
)

// qa3LSHA is the SHA-256 hex of data. Session admission does not check it, so any value would do here.
func qa3LSHA(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

type qa3LStack struct {
	t    *testing.T
	h    http.Handler
	db   *store.DB
	root string
}

// qa3LNewStack builds the real router over a fresh store. Uploads must stay under root.
func qa3LNewStack(t *testing.T) *qa3LStack {
	t.Helper()
	root := t.TempDir()
	db, st := newTestDepsWithRoots(t, []string{root})
	tm, err := transfer.New(db, t.TempDir())
	if err != nil {
		t.Fatalf("transfer.New: %v", err)
	}
	h, err := New(Config{
		Name:          "qa3-ratelimit",
		Settings:      st,
		TrashDir:      t.TempDir(),
		ThumbCacheDir: t.TempDir(),
		UpdatesDir:    t.TempDir(),
		StartTime:     time.Now(),
		DataDir:       t.TempDir(),
	}, db, pairing.New(db, "127.0.0.1", "", "fp"), tm)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return &qa3LStack{t: t, h: h, db: db, root: root}
}

// device mints a paired, non-owner device with exactly the given file grants and returns its id and token.
func (s *qa3LStack) device(name string, browse, download, upload, modify, del, share bool) (string, string) {
	s.t.Helper()
	token := "tok-" + name
	id, err := s.db.UpsertDevice(name, name, token, "", false)
	if err != nil {
		s.t.Fatalf("UpsertDevice(%s): %v", name, err)
	}
	if err := s.db.SetDeviceFilePermissions(id, browse, download, upload, modify, del, share); err != nil {
		s.t.Fatalf("SetDeviceFilePermissions(%s): %v", name, err)
	}
	return id, token
}

func (s *qa3LStack) do(method, path, token string, body []byte) *httptest.ResponseRecorder {
	s.t.Helper()
	req := httptest.NewRequest(method, path, bytes.NewReader(body))
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rr := httptest.NewRecorder()
	s.h.ServeHTTP(rr, req)
	return rr
}

// postFrom sends a POST with the given RemoteAddr, which is the source the limiters key on.
func (s *qa3LStack) postFrom(path, remote, body string) *httptest.ResponseRecorder {
	s.t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.RemoteAddr = remote
	rr := httptest.NewRecorder()
	s.h.ServeHTTP(rr, req)
	return rr
}

// openSession posts POST /v1/transfers through the router.
func (s *qa3LStack) openSession(token, target string, size int64, overwrite bool) *httptest.ResponseRecorder {
	s.t.Helper()
	body, err := json.Marshal(map[string]any{
		"path":      target,
		"size":      size,
		"sha256":    qa3LSHA(nil),
		"chunkSize": qa3LChunkSize,
		"overwrite": overwrite,
	})
	if err != nil {
		s.t.Fatalf("marshal open body: %v", err)
	}
	return s.do(http.MethodPost, "/v1/transfers", token, body)
}

// qa3LWant fails unless rr has the wanted status and, when errCode is set, the wanted error code.
func qa3LWant(t *testing.T, what string, rr *httptest.ResponseRecorder, code int, errCode string) {
	t.Helper()
	if rr.Code != code {
		t.Fatalf("%s: status = %d, want %d: %s", what, rr.Code, code, rr.Body.String())
	}
	if errCode == "" {
		return
	}
	var e apiError
	if err := json.Unmarshal(rr.Body.Bytes(), &e); err != nil {
		t.Fatalf("%s: decode error body %q: %v", what, rr.Body.String(), err)
	}
	if e.Code != errCode {
		t.Fatalf("%s: error code = %q, want %q", what, e.Code, errCode)
	}
}

func qa3LSessionID(t *testing.T, rr *httptest.ResponseRecorder) string {
	t.Helper()
	var got struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil || got.ID == "" {
		t.Fatalf("no session id in %d response %q (decode err %v)", rr.Code, rr.Body.String(), err)
	}
	return got.ID
}

// TestQA3_RATE_ResourceLimit_PerDeviceSessionCap covers B5a.
// Requirement B5a: at most 4 open sessions per device. The 5th open is 429 RESOURCE_LIMIT with no Retry-After.
// Source: openapi.yaml 1266-1269 ("at most 4 open sessions and 64 GiB per device"), 1331-1336 (429 RESOURCE_LIMIT,
//
//	"No Retry-After is supplied"), 2570-2573.
//
// Expected: opens 1..4 for device A -> 201; open 5 -> 429 RESOURCE_LIMIT, Retry-After header empty; device B's
//
//	open -> 201; delete of A's first session -> 204; A's next open -> 201.
//
// catches: a fifth session admitted; a refusal with the wrong code or a Retry-After added; the quota shared
//
//	across devices; a deleted session not freeing its slot.
func TestQA3_RATE_ResourceLimit_PerDeviceSessionCap(t *testing.T) {
	s := qa3LNewStack(t)
	_, tokA := s.device("qa3-cap-a", true, false, true, false, false, false)
	_, tokB := s.device("qa3-cap-b", true, false, true, false, false, false)

	var first string
	for i := 1; i <= qa3LDeviceSessions; i++ {
		rr := s.openSession(tokA, filepath.Join(s.root, fmt.Sprintf("a-%d.bin", i)), 16, false)
		qa3LWant(t, fmt.Sprintf("device A open %d of %d", i, qa3LDeviceSessions), rr, http.StatusCreated, "")
		if i == 1 {
			first = qa3LSessionID(t, rr)
		}
	}

	fifth := s.openSession(tokA, filepath.Join(s.root, "a-5.bin"), 16, false)
	qa3LWant(t, "device A open beyond its session cap", fifth, http.StatusTooManyRequests, "RESOURCE_LIMIT")
	if ra := fifth.Header().Get("Retry-After"); ra != "" {
		t.Fatalf("RESOURCE_LIMIT carries Retry-After %q; the contract says none is supplied", ra)
	}

	qa3LWant(t, "device B open while device A is at its cap",
		s.openSession(tokB, filepath.Join(s.root, "b-1.bin"), 16, false), http.StatusCreated, "")

	qa3LWant(t, "delete one of device A's sessions",
		s.do(http.MethodDelete, "/v1/transfers/"+first, tokA, nil), http.StatusNoContent, "")
	qa3LWant(t, "device A open after deleting a session",
		s.openSession(tokA, filepath.Join(s.root, "a-6.bin"), 16, false), http.StatusCreated, "")
}

// TestQA3_RATE_ResourceLimit_HostSessionCap covers B5b.
// Requirement B5b: 16 open sessions across the host. The 17th open, from a device with no sessions of its own,
//
//	is 429 RESOURCE_LIMIT.
//
// Source: openapi.yaml 1266-1269 ("16 open sessions and 128 GiB across the host"), 1331-1336, 2570-2573.
// Expected: four devices each open 4 sessions (qa3LHostSessions in total, each open -> 201). A fifth device's
//
//	first open -> 429 RESOURCE_LIMIT. The refusal is host-wide, not per device, because that device has none.
//
// catches: the host admits a 17th session; the host cap is counted per device instead of across the host.
func TestQA3_RATE_ResourceLimit_HostSessionCap(t *testing.T) {
	s := qa3LNewStack(t)
	var toks []string
	for d := 0; d < qa3LHostSessions/qa3LDeviceSessions+1; d++ {
		_, tok := s.device(fmt.Sprintf("qa3-host-%d", d), true, false, true, false, false, false)
		toks = append(toks, tok)
	}

	// Devices 0..3 each open qa3LDeviceSessions sessions: qa3LHostSessions in total.
	for d := 0; d < qa3LHostSessions/qa3LDeviceSessions; d++ {
		for i := 1; i <= qa3LDeviceSessions; i++ {
			rr := s.openSession(toks[d], filepath.Join(s.root, fmt.Sprintf("h-%d-%d.bin", d, i)), 16, false)
			qa3LWant(t, fmt.Sprintf("device %d open %d (host total below %d)", d, i, qa3LHostSessions), rr, http.StatusCreated, "")
		}
	}

	last := len(toks) - 1
	qa3LWant(t, "open beyond the host session cap from a device with no sessions",
		s.openSession(toks[last], filepath.Join(s.root, "h-over.bin"), 16, false),
		http.StatusTooManyRequests, "RESOURCE_LIMIT")
}

// TestQA3_RATE_ResourceLimit_PerDeviceByteCap covers B5c (per device).
// Requirement B5c: 64 GiB reserved per device. A session of exactly 64 GiB is admitted. One more byte is 429.
// Source: openapi.yaml 1266-1269 ("64 GiB per device"), 1287 ("Maximum 64 GiB per upload"), 1331-1336.
// Expected: device A opens a session of 64 GiB -> 201; A opens a 1-byte session -> 429 RESOURCE_LIMIT;
//
//	device B opens a 1-byte session -> 201 (host total is still below 128 GiB).
//
// catches: a device admits more than 64 GiB reserved; or A's full quota blocks device B.
func TestQA3_RATE_ResourceLimit_PerDeviceByteCap(t *testing.T) {
	s := qa3LNewStack(t)
	_, tokA := s.device("qa3-bytes-a", true, false, true, false, false, false)
	_, tokB := s.device("qa3-bytes-b", true, false, true, false, false, false)

	qa3LWant(t, "device A session of exactly 64 GiB",
		s.openSession(tokA, filepath.Join(s.root, "bytes-a-full.bin"), qa3LDeviceBytes, false), http.StatusCreated, "")
	qa3LWant(t, "device A session of 1 byte beyond the 64 GiB device reserve",
		s.openSession(tokA, filepath.Join(s.root, "bytes-a-over.bin"), 1, false),
		http.StatusTooManyRequests, "RESOURCE_LIMIT")
	qa3LWant(t, "device B 1-byte session while device A holds 64 GiB",
		s.openSession(tokB, filepath.Join(s.root, "bytes-b.bin"), 1, false), http.StatusCreated, "")
}

// TestQA3_RATE_ResourceLimit_HostByteCap covers B5c (host).
// Requirement B5c: 128 GiB reserved across the host. Two devices at 64 GiB each reach it exactly. The next byte is 429.
// Source: openapi.yaml 1266-1269 ("128 GiB across the host"), 1331-1336, 2570-2571.
// Expected: device B opens 64 GiB -> 201; device C opens 64 GiB -> 201 (host total at 2 * 64 GiB, which equals
//
//	128 GiB); device D, which holds nothing, opens 1 byte -> 429 RESOURCE_LIMIT.
//
// catches: the host admits reservations beyond 128 GiB; or the host sum is checked per device, not across devices.
func TestQA3_RATE_ResourceLimit_HostByteCap(t *testing.T) {
	s := qa3LNewStack(t)
	_, tokB := s.device("qa3-hbytes-b", true, false, true, false, false, false)
	_, tokC := s.device("qa3-hbytes-c", true, false, true, false, false, false)
	_, tokD := s.device("qa3-hbytes-d", true, false, true, false, false, false)

	qa3LWant(t, "device B 64 GiB", s.openSession(tokB, filepath.Join(s.root, "hb-b.bin"), qa3LDeviceBytes, false),
		http.StatusCreated, "")
	qa3LWant(t, "device C 64 GiB, host total reaches 2 * 64 GiB",
		s.openSession(tokC, filepath.Join(s.root, "hb-c.bin"), qa3LDeviceBytes, false), http.StatusCreated, "")
	qa3LWant(t, "device D 1 byte with host at the 128 GiB reserve",
		s.openSession(tokD, filepath.Join(s.root, "hb-d.bin"), 1, false), http.StatusTooManyRequests, "RESOURCE_LIMIT")
	if qa3LHostBytes != 2*qa3LDeviceBytes {
		t.Fatalf("test arithmetic: host reserve %d is not two device reserves %d", qa3LHostBytes, qa3LDeviceBytes)
	}
}

// TestQA3_RATE_ChunkBusy_IsGlobalAndClears covers B5d.
// Requirement B5d: while the agent holds as many chunk bodies in memory as it allows, a chunk is 429 CHUNK_BUSY
//
//	with an integer Retry-After. The scope is the agent, not the device. The refusal clears once capacity frees.
//
// Source: openapi.yaml 1417-1421 ("The agent is already holding as many chunk bodies in memory as it allows.
//
//	Retry after the `Retry-After` seconds. code=CHUNK_BUSY.", header Retry-After integer); 2577.
//
// proposed policy: the contract names no figure for the in-memory capacity. The test takes the whole
//
//	configured chunk budget (chunkBudget) at run time, so the response shape and scope are checked, not a number.
//
// Expected: device A's chunk -> 429 CHUNK_BUSY, Retry-After is an integer; device B's chunk -> 429 CHUNK_BUSY
//
//	(agent-wide scope); after the budget is released, device A's chunk -> 204.
//
// catches: CHUNK_BUSY missing or given a different code; Retry-After missing or not an integer; the limit
//
//	applied per device, so B is not refused; a budget that never clears.
func TestQA3_RATE_ChunkBusy_IsGlobalAndClears(t *testing.T) {
	s := qa3LNewStack(t)
	_, tokA := s.device("qa3-busy-a", true, false, true, false, false, false)
	_, tokB := s.device("qa3-busy-b", true, false, true, false, false, false)
	content := []byte("0123456789abcdef")
	sha := qa3LSHA(content)
	idA := s.openSessionWithSHA(tokA, filepath.Join(s.root, "busy-a.bin"), int64(len(content)), sha)
	idB := s.openSessionWithSHA(tokB, filepath.Join(s.root, "busy-b.bin"), int64(len(content)), sha)

	// Take every byte of the configured chunk budget, read at run time.
	chunkBudget.mu.Lock()
	free := chunkBudget.free
	chunkBudget.mu.Unlock()
	if !chunkBudget.acquire(free, 0, nil) {
		t.Fatalf("could not take the whole chunk budget (%d bytes) before the test", free)
	}
	var once sync.Once
	release := func() { once.Do(func() { chunkBudget.release(free) }) }
	t.Cleanup(release)

	for _, c := range []struct{ name, tok, id string }{
		{"device A", tokA, idA},
		{"device B", tokB, idB},
	} {
		rr := s.putChunk(c.tok, c.id, content)
		qa3LWant(t, c.name+" chunk while the agent holds its chunk-body budget", rr, http.StatusTooManyRequests, "CHUNK_BUSY")
		ra := rr.Header().Get("Retry-After")
		if _, err := strconv.Atoi(ra); err != nil {
			t.Fatalf("%s: Retry-After = %q, want integer seconds", c.name, ra)
		}
	}

	release()
	qa3LWant(t, "device A chunk after the budget is released",
		s.putChunk(tokA, idA, content), http.StatusNoContent, "")
}

// openSessionWithSHA opens a 16-byte session whose whole-file hash is sha, and returns its id.
func (s *qa3LStack) openSessionWithSHA(token, target string, size int64, sha string) string {
	s.t.Helper()
	body, err := json.Marshal(map[string]any{
		"path":      target,
		"size":      size,
		"sha256":    sha,
		"chunkSize": qa3LChunkSize,
		"overwrite": false,
	})
	if err != nil {
		s.t.Fatalf("marshal open body: %v", err)
	}
	rr := s.do(http.MethodPost, "/v1/transfers", token, body)
	qa3LWant(s.t, "open session "+target, rr, http.StatusCreated, "")
	return qa3LSessionID(s.t, rr)
}

// putChunk sends PUT /v1/transfers/{id}/chunks/0 with a matching X-Chunk-Sha256.
func (s *qa3LStack) putChunk(token, id string, data []byte) *httptest.ResponseRecorder {
	s.t.Helper()
	req := httptest.NewRequest(http.MethodPut, fmt.Sprintf("/v1/transfers/%s/chunks/0", id), bytes.NewReader(data))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-Chunk-Sha256", qa3LSHA(data))
	rr := httptest.NewRecorder()
	s.h.ServeHTTP(rr, req)
	return rr
}

// TestQA3_RATE_AuthChallenge_ThirtyPerMinutePerSource covers B5e.
// Requirement B5e: 30 challenge requests per minute per source. The 31st is 429 RATE_LIMITED. Another source is
//
//	unaffected. After the one-minute window passes, the same source is admitted again.
//
// Source: openapi.yaml 342-343 ("Too many challenge requests (rate-limited at 30/min). code=RATE_LIMITED.");
//
//	docs/security-route-matrix.md 10 ("Challenge/pair/login have per-source rate limits ...").
//	The contract gives no Retry-After for this route, so none is checked.
//
// Expected: source 192.0.2.10 requests 1..30 -> 200; request 31 -> 429 RATE_LIMITED; source 192.0.2.11 -> 200;
//
//	after 61 seconds, source 192.0.2.10 -> 200.
//
// catches: a 31st challenge admitted; a limit shared across sources; a source locked out past the window.
func TestQA3_RATE_AuthChallenge_ThirtyPerMinutePerSource(t *testing.T) {
	s := qa3LNewStack(t)
	const src = "192.0.2.10:40000"
	const other = "192.0.2.11:40000"
	const window = 60 * time.Second

	for i := 1; i <= 30; i++ {
		rr := s.postFrom("/v1/auth/challenge", src, "")
		qa3LWant(t, fmt.Sprintf("challenge %d of 30 from %s", i, src), rr, http.StatusOK, "")
	}
	qa3LWant(t, "31st challenge from the same source in the window",
		s.postFrom("/v1/auth/challenge", src, ""), http.StatusTooManyRequests, "RATE_LIMITED")
	qa3LWant(t, "challenge from a different source",
		s.postFrom("/v1/auth/challenge", other, ""), http.StatusOK, "")

	time.Sleep(window + time.Second)
	qa3LWant(t, "challenge from the first source after the window",
		s.postFrom("/v1/auth/challenge", src, ""), http.StatusOK, "")
}

// TestQA3_RATE_Pair_TenPerMinuteFromOneSource covers B5f.
// Requirement B5f: 10 pairing attempts per minute. The 11th from the same source is 429 RATE_LIMITED.
// Source: openapi.yaml 393-396 ("Too many pairing attempts (rate-limited at 10/min per agent, since the pairing
//
//	code is short and brute-forceable). code=RATE_LIMITED.").
//
// Scope: UNTESTABLE beyond one source. The contract says "per agent" here, and security-route-matrix.md 10 says
//
//	"per-source". This test only checks one source, where both readings give the same result.
//
// Expected: attempts 1..10 from one source are not 429 (the body is malformed, so they fail later with a 4xx);
//
//	attempt 11 -> 429 RATE_LIMITED.
//
// catches: an 11th pairing attempt admitted; the limit counted only after a successful pairing.
func TestQA3_RATE_Pair_TenPerMinuteFromOneSource(t *testing.T) {
	s := qa3LNewStack(t)
	const src = "198.51.100.7:40000"

	for i := 1; i <= 10; i++ {
		rr := s.postFrom("/v1/pair", src, "{}")
		if rr.Code == http.StatusTooManyRequests {
			t.Fatalf("pairing attempt %d of 10 from one source was rate-limited: %s", i, rr.Body.String())
		}
	}
	qa3LWant(t, "11th pairing attempt from the same source in the minute",
		s.postFrom("/v1/pair", src, "{}"), http.StatusTooManyRequests, "RATE_LIMITED")
}
