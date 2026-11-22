package transfer

import (
	"errors"
	"log"
	"sync/atomic"
	"syscall"
	"time"
)

// counters are process-lifetime tallies of upload failure modes, kept next to the Manager that observes
// them so a test (or a second manager over the same database) never shares state with another. They are
// side effects only: nothing in the transfer engine reads them. They carry no path, device or session id.
type counters struct {
	completeOnTerminalSession     atomic.Int64
	hashMismatchAllChunksReceived atomic.Int64
	sessionsResetByReconcile      atomic.Int64
	publishTempFoundInDest        atomic.Int64
	enospcWriteChunk              atomic.Int64
	enospcPublish                 atomic.Int64
}

// Metrics is a point-in-time reading of the upload observability counters and gauges, as served (admin
// only) under "transfers" in GET /v1/metrics. Counters reset when the agent restarts. Every field is a
// number: there are deliberately no labels, so no path, device label or session id can leak into it.
type Metrics struct {
	// CompleteOnTerminalSession counts complete requests for a session already completed or failed
	// (the idempotent replay of a lost answer included).
	CompleteOnTerminalSession int64 `json:"completeOnTerminalSession"`
	// HashMismatchAllChunksReceived counts whole-file hash failures although every chunk was marked received.
	HashMismatchAllChunksReceived int64 `json:"hashMismatchAllChunksReceived"`
	// SessionsResetByReconcile counts open sessions failed at startup because their temp file was missing or short.
	SessionsResetByReconcile int64 `json:"sessionsResetByReconcile"`
	// PublishTempFoundInDest counts stale .rfe-upload- temps the sweep removed from destination folders.
	PublishTempFoundInDest int64 `json:"publishTempFoundInDest"`
	// EnospcWriteChunk and EnospcPublish count disk-full errors while writing a chunk and while publishing.
	EnospcWriteChunk int64 `json:"enospcWriteChunk"`
	EnospcPublish    int64 `json:"enospcPublish"`
	// OpenSessions is the number of open upload sessions when the metrics were read.
	OpenSessions int `json:"openSessions"`
	// OldestOpenSessionAgeSeconds is how long the least recently active open session has been idle (0 when
	// none). A session that never received a chunk has no activity time and is not measured.
	OldestOpenSessionAgeSeconds int64 `json:"oldestOpenSessionAgeSeconds"`
}

// Metrics reads the counters and computes the open-session gauges as of now. When the database read for the
// gauges fails, the counters are still returned together with the error.
func (m *Manager) Metrics(now time.Time) (Metrics, error) {
	out := Metrics{
		CompleteOnTerminalSession:     m.stats.completeOnTerminalSession.Load(),
		HashMismatchAllChunksReceived: m.stats.hashMismatchAllChunksReceived.Load(),
		SessionsResetByReconcile:      m.stats.sessionsResetByReconcile.Load(),
		PublishTempFoundInDest:        m.stats.publishTempFoundInDest.Load(),
		EnospcWriteChunk:              m.stats.enospcWriteChunk.Load(),
		EnospcPublish:                 m.stats.enospcPublish.Load(),
	}
	open, err := m.db.ListOpenTransfers()
	if err != nil {
		return out, err
	}
	out.OpenSessions = len(open)
	for i := range open {
		if open[i].UpdatedAt <= 0 {
			continue
		}
		if age := now.Unix() - open[i].UpdatedAt; age > out.OldestOpenSessionAgeSeconds {
			out.OldestOpenSessionAgeSeconds = age
		}
	}
	return out, nil
}

// NoteCompleteOnTerminalSession records a complete request answered for an already completed or failed
// session. The HTTP handler calls it for the idempotent replay it answers without entering Complete.
func (m *Manager) NoteCompleteOnTerminalSession(id string) {
	m.stats.completeOnTerminalSession.Add(1)
	log.Printf("transfer: complete on terminal session %s", id)
}

// countENOSPC adds one to c when err is, or wraps, a disk-full error.
func countENOSPC(c *atomic.Int64, id string, err error) {
	if errors.Is(err, syscall.ENOSPC) {
		c.Add(1)
		log.Printf("transfer: disk full for session %s: %v", id, err)
	}
}
