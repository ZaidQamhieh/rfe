package sidecar

import (
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

// Stats counts what one supervised sidecar has been through since the agent started.
type Stats struct {
	Name          string `json:"name"`
	Running       bool   `json:"running"`
	Version       string `json:"version,omitempty"`
	Starts        uint64 `json:"starts"`
	StartFailures uint64 `json:"startFailures"`
	Crashes       uint64 `json:"crashes"` // a running sidecar died or failed a ping
	BreakerOpens  uint64 `json:"breakerOpens"`
	BreakerOpen   bool   `json:"breakerOpen"`
}

type supStats struct {
	starts, startFailures, crashes, breakerOpens atomic.Uint64
	breakerOpen                                  atomic.Bool
}

// Stats returns the supervisor's counters.
func (s *Supervisor) Stats() Stats {
	st := Stats{
		Name:          s.cfg.Name,
		Starts:        s.stats.starts.Load(),
		StartFailures: s.stats.startFailures.Load(),
		Crashes:       s.stats.crashes.Load(),
		BreakerOpens:  s.stats.breakerOpens.Load(),
		BreakerOpen:   s.stats.breakerOpen.Load(),
	}
	if c, err := s.Client(); err == nil {
		st.Running = true
		st.Version = c.Version()
	}
	return st
}

var (
	regMu       sync.Mutex
	supervisors = map[*Supervisor]struct{}{}
	events      = map[string]uint64{}
)

func register(s *Supervisor) {
	regMu.Lock()
	supervisors[s] = struct{}{}
	regMu.Unlock()
}

func unregister(s *Supervisor) {
	regMu.Lock()
	delete(supervisors, s)
	regMu.Unlock()
}

// Note counts a named event (a fallback to the built-in implementation, a refused sidecar, a poisoned file...).
// Events are what `rfe status` shows, so "no fallbacks in the field" can be read off instead of grepped.
func Note(event string) {
	regMu.Lock()
	events[event]++
	regMu.Unlock()
}

// Report is a snapshot of every supervisor and event counter in this process.
type Report struct {
	At       time.Time         `json:"at"`
	Sidecars []Stats           `json:"sidecars"`
	Events   map[string]uint64 `json:"events"`
}

func Snapshot() Report {
	regMu.Lock()
	sups := make([]*Supervisor, 0, len(supervisors))
	for s := range supervisors {
		sups = append(sups, s)
	}
	ev := make(map[string]uint64, len(events))
	for k, v := range events {
		ev[k] = v
	}
	regMu.Unlock()
	r := Report{At: time.Now().UTC(), Events: ev}
	for _, s := range sups {
		r.Sidecars = append(r.Sidecars, s.Stats())
	}
	sort.Slice(r.Sidecars, func(i, j int) bool { return r.Sidecars[i].Name < r.Sidecars[j].Name })
	return r
}

// ResetEventsForTest clears the process-wide event counters.
func ResetEventsForTest() {
	regMu.Lock()
	events = map[string]uint64{}
	regMu.Unlock()
}
