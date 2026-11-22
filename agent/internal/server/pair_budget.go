// Package server — global failed-guess budget for /v1/pair.
package server

import (
	"strconv"
	"sync"
	"time"
)

// The pairing code is only ~40 bits, and the per-address limiter alone lets a caller with many source addresses
// (IPv6 on a LAN) guess tens of thousands of codes a minute. The budget below counts FAILED code lookups across all
// addresses, in constant memory, so honest pairing (correct codes) never spends it.
const (
	// pairFailBudget is how many failed lookups are answered per pairFailBudgetWindow before /v1/pair refuses everything
	// until the window ends.
	pairFailBudget       = 60
	pairFailBudgetWindow = time.Minute
	// More than pairLockFailures failed lookups inside pairLockWindow lock pairing for pairLockCooldown.
	pairLockFailures = 200
	pairLockWindow   = 10 * time.Minute
	pairLockCooldown = 10 * time.Minute
	// auditPairLocked is the audit action written once per lockout.
	auditPairLocked = "pair.locked"
	// pairLockedMsg is the 429 message while pairing is locked or the failure budget is spent.
	pairLockedMsg = "pairing is paused after too many failed attempts, try again later"
)

// pairBudget is a pair of fixed-window counters (one per minute, one per 10 minutes) plus a lockout deadline: a
// fixed amount of state however many addresses call. now is injectable so tests can move time.
type pairBudget struct {
	mu          sync.Mutex
	now         func() time.Time
	minStart    time.Time // zero when no failure is counted in the current minute window
	minFails    int
	longStart   time.Time // zero when no failure is counted in the current 10-minute window
	longFails   int
	lockedUntil time.Time
}

func newPairBudget(now func() time.Time) *pairBudget {
	return &pairBudget{now: now}
}

// rollLocked starts a new window for each counter whose window has ended. Callers hold b.mu.
func (b *pairBudget) rollLocked(now time.Time) {
	if !b.minStart.IsZero() && !now.Before(b.minStart.Add(pairFailBudgetWindow)) {
		b.minStart, b.minFails = time.Time{}, 0
	}
	if !b.longStart.IsZero() && !now.Before(b.longStart.Add(pairLockWindow)) {
		b.longStart, b.longFails = time.Time{}, 0
	}
}

// refusedFor returns how long /v1/pair must refuse every request (lockout, or a spent per-minute budget), or 0
// when it may proceed. It records nothing.
func (b *pairBudget) refusedFor() time.Duration {
	b.mu.Lock()
	defer b.mu.Unlock()
	now := b.now()
	if now.Before(b.lockedUntil) {
		return b.lockedUntil.Sub(now)
	}
	b.rollLocked(now)
	if b.minFails >= pairFailBudget {
		return b.minStart.Add(pairFailBudgetWindow).Sub(now)
	}
	return 0
}

// recordFailure counts one failed code lookup. When it pushes the 10-minute total over pairLockFailures it starts
// the cooldown, restarts both counters and reports locked=true with that total, exactly once per lockout.
func (b *pairBudget) recordFailure() (failures int, locked bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	now := b.now()
	if now.Before(b.lockedUntil) {
		return 0, false // a request that was already in flight when the lock began
	}
	b.rollLocked(now)
	if b.minStart.IsZero() {
		b.minStart = now
	}
	if b.longStart.IsZero() {
		b.longStart = now
	}
	b.minFails++
	b.longFails++
	if b.longFails <= pairLockFailures {
		return 0, false
	}
	failures = b.longFails
	b.lockedUntil = now.Add(pairLockCooldown)
	b.minStart, b.minFails = time.Time{}, 0
	b.longStart, b.longFails = time.Time{}, 0
	return failures, true
}

// retryAfterSeconds renders d as a Retry-After value: whole seconds, rounded up, at least 1.
func retryAfterSeconds(d time.Duration) string {
	secs := int((d + time.Second - 1) / time.Second)
	if secs < 1 {
		secs = 1
	}
	return strconv.Itoa(secs)
}
