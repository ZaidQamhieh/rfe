package pairing

import (
	"net"
	"net/netip"
	"sync"
	"time"
)

// unknownSource is the limiter key for a caller whose address is unusable.
const unknownSource = "unknown"

// ipv6PrefixBits is how much of an IPv6 address names one source: a /64 is
// what one subscriber or link gets, so guessing from many addresses inside it
// still draws on one allowance.
const ipv6PrefixBits = 64

// limiter is a fixed-window counter per key with an injected clock. Keys are
// bounded: when the table is full of live windows a new key is refused, so an
// attacker inventing keys cannot grow memory.
type limiter struct {
	mu      sync.Mutex
	now     func() time.Time
	limit   int
	window  time.Duration
	maxKeys int
	m       map[string]*window
}

// window is one key's count since start.
type window struct {
	start time.Time
	n     int
}

// newLimiter allows limit hits per key per window.
func newLimiter(limit int, w time.Duration, maxKeys int, now func() time.Time) *limiter {
	return &limiter{now: now, limit: limit, window: w, maxKeys: maxKeys, m: make(map[string]*window)}
}

// Allow counts one hit for key and reports whether it is within the limit.
func (l *limiter) Allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	w := l.m[key]
	if w == nil || now.Sub(w.start) >= l.window {
		if w == nil && len(l.m) >= l.maxKeys {
			l.sweep(now)
			if len(l.m) >= l.maxKeys {
				return false
			}
		}
		w = &window{start: now}
		l.m[key] = w
	}
	w.n++
	return w.n <= l.limit
}

// Exhausted reports whether key has used up its allowance, without counting.
func (l *limiter) Exhausted(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	w := l.m[key]
	return w != nil && l.now().Sub(w.start) < l.window && w.n >= l.limit
}

// sweep drops windows that have ended; the caller holds l.mu.
func (l *limiter) sweep(now time.Time) {
	for k, w := range l.m {
		if now.Sub(w.start) >= l.window {
			delete(l.m, k)
		}
	}
}

// sourceKey names the source of a request for rate limiting: the IPv4 address,
// or the /64 of an IPv6 one. A port is ignored.
func sourceKey(remote string) string {
	host := remote
	if h, _, err := net.SplitHostPort(remote); err == nil {
		host = h
	}
	a, err := netip.ParseAddr(host)
	if err != nil {
		return unknownSource
	}
	a = a.Unmap().WithZone("")
	if a.Is6() {
		p, err := a.Prefix(ipv6PrefixBits)
		if err != nil {
			return unknownSource
		}
		return p.String()
	}
	return a.String()
}

// dualLimiter applies a per-source limit and a shared limit over all sources.
type dualLimiter struct {
	perSource *limiter
	global    *limiter
}

// newDualLimiter allows attempts per source per window and attempts*factor in
// total, so many sources together still cannot out-guess one.
func newDualLimiter(attempts int, w time.Duration, factor, maxKeys int, now func() time.Time) *dualLimiter {
	return &dualLimiter{
		perSource: newLimiter(attempts, w, maxKeys, now),
		global:    newLimiter(attempts*factor, w, 1, now),
	}
}

// Allow counts one attempt from remote against both limits. The source is
// checked first so a noisy one spends its own allowance before everyone's.
func (d *dualLimiter) Allow(remote string) bool {
	return d.perSource.Allow(sourceKey(remote)) && d.global.Allow("")
}
