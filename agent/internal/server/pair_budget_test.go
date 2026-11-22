package server

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ZaidQamhieh/rfe/agent/internal/pairing"
	"github.com/ZaidQamhieh/rfe/agent/internal/store"
)

// budgetClock is a hand-advanced clock for the global pairing failure budget.
type budgetClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *budgetClock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *budgetClock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

// pairBudgetEnv is an in-process /v1/pair handler with a fake clock.
type pairBudgetEnv struct {
	t      *testing.T
	db     *store.DB
	pm     *pairing.Manager
	nonces *nonceStore
	h      http.HandlerFunc
	clock  *budgetClock
	next   int // next distinct fake source address
}

func newPairBudgetEnv(t *testing.T) *pairBudgetEnv {
	t.Helper()
	db, _ := newTestDeps(t)
	pm := pairing.New(db, "127.0.0.1:8765", "", "fingerprint")
	nonces := newNonceStore()
	clock := &budgetClock{t: time.Date(2030, 1, 1, 12, 0, 0, 0, time.UTC)}
	cfg := Config{Name: "test-pc", CertFingerprint: testProofFp}
	return &pairBudgetEnv{t: t, db: db, pm: pm, nonces: nonces, clock: clock,
		h: pairHandlerClock(cfg, db, pm, nonces, clock.now)}
}

// freshAddr returns a RemoteAddr no earlier request used.
func (e *pairBudgetEnv) freshAddr() string {
	e.next++
	return fmt.Sprintf("10.%d.%d.%d:4000", (e.next>>16)&255, (e.next>>8)&255, e.next&255)
}

// post sends a /v1/pair body from a fresh source address.
func (e *pairBudgetEnv) post(body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/v1/pair", strings.NewReader(body))
	req.RemoteAddr = e.freshAddr()
	rr := httptest.NewRecorder()
	e.h(rr, req)
	return rr
}

// pairBody builds a request carrying a valid device proof and the given code.
func (e *pairBudgetEnv) pairBody(code string) string {
	pub, nonce, sig := signedDeviceProof(e.t, e.nonces)
	return `{"pairingCode":"` + code + `","deviceLabel":"phone","devicePublicKey":"` + pub +
		`","nonce":"` + nonce + `","signature":"` + sig + `"}`
}

// wrongGuess sends a well-formed request with a wrong code from a fresh address.
func (e *pairBudgetEnv) wrongGuess() *httptest.ResponseRecorder {
	return e.post(e.pairBody("WRONGCODE"))
}

// wrongGuesses sends n wrong guesses and fails the test unless each is a plain 401 INVALID_CODE.
func (e *pairBudgetEnv) wrongGuesses(n int) {
	e.t.Helper()
	for i := 0; i < n; i++ {
		if rr := e.wrongGuess(); rr.Code != http.StatusUnauthorized {
			e.t.Fatalf("wrong guess %d: want 401, got %d: %s", i+1, rr.Code, rr.Body.String())
		}
	}
}

// lockedAuditRows counts the pair.locked rows in the audit log.
func (e *pairBudgetEnv) lockedAuditRows() []store.AuditEntry {
	e.t.Helper()
	entries, err := e.db.AuditEntries(200, 0)
	if err != nil {
		e.t.Fatalf("audit entries: %v", err)
	}
	var rows []store.AuditEntry
	for _, en := range entries {
		if en.Action == "pair.locked" {
			rows = append(rows, en)
		}
	}
	return rows
}

func retryAfterHeader(t *testing.T, rr *httptest.ResponseRecorder) int {
	t.Helper()
	n, err := strconv.Atoi(rr.Header().Get("Retry-After"))
	if err != nil || n <= 0 {
		t.Fatalf("429 needs a positive Retry-After in seconds, got %q", rr.Header().Get("Retry-After"))
	}
	return n
}

// Wrong codes from thousands of distinct addresses (more than the per-address map holds) hit the global
// per-minute failure budget; a correct code is refused while it is spent and works again after the window.
func TestPairBudget_ManyAddressesExhaustTheMinuteBudget(t *testing.T) {
	e := newPairBudgetEnv(t)
	code, _, err := e.pm.Mint(time.Hour)
	if err != nil {
		t.Fatalf("mint: %v", err)
	}

	e.wrongGuesses(60)
	for i := 0; i < 5000; i++ {
		rr := e.post("{}")
		if rr.Code != http.StatusTooManyRequests {
			t.Fatalf("request %d after the budget: want 429 from a fresh address, got %d: %s", i+1, rr.Code, rr.Body.String())
		}
	}
	rr := e.post(e.pairBody(code))
	if rr.Code != http.StatusTooManyRequests {
		t.Fatalf("a correct code while the failure budget is spent: want 429, got %d: %s", rr.Code, rr.Body.String())
	}
	if got := retryAfterHeader(t, rr); got > 60 {
		t.Fatalf("minute budget Retry-After should be at most 60s, got %d", got)
	}
	if !e.pm.Peek(code).Valid {
		t.Fatal("a refused request must not consume the pairing code")
	}
	if rows := e.lockedAuditRows(); len(rows) != 0 {
		t.Fatalf("a spent minute budget is not a lockout, got audit rows %+v", rows)
	}

	e.clock.advance(61 * time.Second)
	if rr := e.post(e.pairBody(code)); rr.Code != http.StatusOK {
		t.Fatalf("correct code after the window: want 200, got %d: %s", rr.Code, rr.Body.String())
	}
}

// Honest pairing is never starved: a correct code pairs while the failure budget is healthy, successes are not
// counted, and requests that fail before the code lookup (bad proof) are not counted either.
func TestPairBudget_OnlyFailedCodeLookupsCount(t *testing.T) {
	e := newPairBudgetEnv(t)
	e.wrongGuesses(59)
	for i := 0; i < 100; i++ {
		if rr := e.post(`{"pairingCode":"WRONGCODE","deviceLabel":"phone"}`); rr.Code != http.StatusBadRequest {
			t.Fatalf("missing proof %d: want 400, got %d: %s", i+1, rr.Code, rr.Body.String())
		}
	}
	for i := 0; i < 100; i++ {
		code, _, err := e.pm.Mint(time.Hour)
		if err != nil {
			t.Fatalf("mint: %v", err)
		}
		if rr := e.post(e.pairBody(code)); rr.Code != http.StatusOK {
			t.Fatalf("honest pairing %d under budget: want 200, got %d: %s", i+1, rr.Code, rr.Body.String())
		}
	}
	e.wrongGuesses(1) // the 60th failure is still answered normally
	if rr := e.wrongGuess(); rr.Code != http.StatusTooManyRequests {
		t.Fatalf("61st failure in the minute: want 429, got %d: %s", rr.Code, rr.Body.String())
	}
}

// More than 200 failures in 10 minutes lock pairing for 10 minutes with one audit row, whatever the code.
func TestPairBudget_LockoutAfterTooManyFailuresIn10Minutes(t *testing.T) {
	e := newPairBudgetEnv(t)
	code, _, err := e.pm.Mint(time.Hour)
	if err != nil {
		t.Fatalf("mint: %v", err)
	}
	for minute := 0; minute < 3; minute++ {
		e.wrongGuesses(60)
		e.clock.advance(61 * time.Second)
	}
	e.wrongGuesses(20) // 200 so far: not yet over
	if rr := e.post(e.pairBody(code)); rr.Code == http.StatusTooManyRequests {
		t.Fatalf("200 failures is the limit, not over it: got 429 %s", rr.Body.String())
	}
	if !e.pm.Peek(code).Valid {
		// that request paired successfully and used the code; mint another for the lockout checks
		if code, _, err = e.pm.Mint(time.Hour); err != nil {
			t.Fatalf("mint: %v", err)
		}
	}
	e.wrongGuesses(1) // the 201st failure is answered 401 and trips the lock
	if rows := e.lockedAuditRows(); len(rows) != 1 {
		t.Fatalf("want exactly one pair.locked audit row after the 201st failure, got %+v", rows)
	}

	for i := 0; i < 50; i++ {
		rr := e.post(e.pairBody(code))
		if rr.Code != http.StatusTooManyRequests {
			t.Fatalf("correct code during lockout: want 429, got %d: %s", rr.Code, rr.Body.String())
		}
		if got := retryAfterHeader(t, rr); got > 600 || got < 590 {
			t.Fatalf("lockout Retry-After should be about 600s, got %d", got)
		}
	}
	if !e.pm.Peek(code).Valid {
		t.Fatal("a code refused during lockout must stay valid")
	}
	rows := e.lockedAuditRows()
	if len(rows) != 1 {
		t.Fatalf("refused requests must not add audit rows, got %d", len(rows))
	}
	if strings.Contains(rows[0].Detail+rows[0].Target+rows[0].Actor, "10.") {
		t.Fatalf("pair.locked row must not list source addresses: %+v", rows[0])
	}
	if !strings.Contains(rows[0].Detail, "201") {
		t.Fatalf("pair.locked detail should carry the failure count: %q", rows[0].Detail)
	}

	e.clock.advance(9 * time.Minute)
	if rr := e.post(e.pairBody(code)); rr.Code != http.StatusTooManyRequests {
		t.Fatalf("9 minutes into the 10-minute cooldown: want 429, got %d", rr.Code)
	}
	e.clock.advance(2 * time.Minute)
	if rr := e.post(e.pairBody(code)); rr.Code != http.StatusOK {
		t.Fatalf("after the cooldown: want 200, got %d: %s", rr.Code, rr.Body.String())
	}
	e.wrongGuesses(1) // counters restarted with the cooldown
	if rows := e.lockedAuditRows(); len(rows) != 1 {
		t.Fatalf("expiry must not write another row, got %d", len(rows))
	}
}

// Failures spread so that no 10-minute window holds more than 200 never lock pairing.
func TestPairBudget_FailuresOutsideTheWindowDoNotAccumulate(t *testing.T) {
	e := newPairBudgetEnv(t)
	for round := 0; round < 3; round++ {
		for minute := 0; minute < 3; minute++ {
			e.wrongGuesses(60)
			e.clock.advance(61 * time.Second)
		}
		e.clock.advance(11 * time.Minute)
	}
	if rows := e.lockedAuditRows(); len(rows) != 0 {
		t.Fatalf("no lockout expected, got %+v", rows)
	}
}
