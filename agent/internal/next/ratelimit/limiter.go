// Package ratelimit holds the fixed-window request budget shared by the next
// agent's public routes.
package ratelimit

import (
	"sync"
	"time"
)

// bucket is one key's budget: when its window started and how many requests it
// has made since.
type bucket struct {
	start time.Time
	hits  int
}

// Limiter keeps a fixed-window request budget per key. The table of keys is
// bounded: when it is full, expired windows are dropped, and if it is still full
// a new key is refused until a slot ages out, so a flood of spoofed keys cannot
// grow memory without limit and cannot starve keys that already have a window.
type Limiter struct {
	mu       sync.Mutex
	attempts int
	window   time.Duration
	maxKeys  int
	entries  map[string]bucket
}

// New returns a Limiter that allows attempts requests per window for each key
// and tracks at most maxKeys keys (at least one).
func New(attempts int, window time.Duration, maxKeys int) *Limiter {
	if maxKeys < 1 {
		maxKeys = 1
	}
	return &Limiter{attempts: attempts, window: window, maxKeys: maxKeys, entries: make(map[string]bucket)}
}

// Allow counts one request by key at time now and reports whether it is within
// the budget.
func (l *Limiter) Allow(key string, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	if e, ok := l.entries[key]; ok && now.Sub(e.start) < l.window {
		if e.hits >= l.attempts {
			return false
		}
		e.hits++
		l.entries[key] = e
		return true
	}
	if len(l.entries) >= l.maxKeys {
		cutoff := now.Add(-l.window)
		for k, e := range l.entries {
			if !e.start.After(cutoff) {
				delete(l.entries, k)
			}
		}
		if _, ok := l.entries[key]; !ok && len(l.entries) >= l.maxKeys {
			return false
		}
	}
	l.entries[key] = bucket{start: now, hits: 1}
	return true
}
