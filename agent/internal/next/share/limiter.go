package share

import (
	"time"

	"github.com/ZaidQamhieh/rfe/agent/internal/next/ratelimit"
)

// unknownIPKey is the bucket used for a request whose source address is empty.
const unknownIPKey = "<unknown>"

// ipLimiter keeps a fixed-window request budget per source address for the
// public fetch route, on the shared ratelimit table.
type ipLimiter struct {
	lim *ratelimit.Limiter
}

// newIPLimiter returns a limiter that allows attempts requests per window from
// each address and tracks at most maxIPs addresses (at least one).
func newIPLimiter(attempts int, window time.Duration, maxIPs int) *ipLimiter {
	return &ipLimiter{lim: ratelimit.New(attempts, window, maxIPs)}
}

// allow counts one request from ip at time now and reports whether it is
// within the budget.
func (l *ipLimiter) allow(ip string, now time.Time) bool {
	if ip == "" {
		ip = unknownIPKey
	}
	return l.lim.Allow(ip, now)
}
