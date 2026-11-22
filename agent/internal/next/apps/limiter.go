package apps

import (
	"time"

	"github.com/ZaidQamhieh/rfe/agent/internal/next/ratelimit"
)

// unknownDeviceKey is the bucket for a call with an empty device id.
const unknownDeviceKey = "<unknown>"

// limiter keeps a fixed-window budget per device, on the shared ratelimit table.
type limiter struct {
	lim *ratelimit.Limiter
}

// newLimiter allows attempts calls per span for each of at most maxKeys keys.
func newLimiter(attempts int, span time.Duration, maxKeys int) *limiter {
	return &limiter{lim: ratelimit.New(attempts, span, maxKeys)}
}

// allow counts one call by key at time now and reports whether it is within
// the budget.
func (l *limiter) allow(key string, now time.Time) bool {
	if key == "" {
		key = unknownDeviceKey
	}
	return l.lim.Allow(key, now)
}
