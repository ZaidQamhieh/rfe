// Package server — audit trail (backlog #37).
//
// Records account/device/share events so a host owner can answer "what
// happened to this agent, and who did it" after the fact. Deliberately not a
// per-file log: file operations arrive at transfer volume and would bury the
// security-relevant entries. Reading it is admin-only (same rule as
// /v1/metrics and /v1/logs — it describes every device, not just the caller).
package server

import (
	"log"
	"net/http"
	"strconv"

	"github.com/ZaidQamhieh/rfe/agent/internal/store"
)

// audit records one event, attributing it to the request's authenticated
// device. A failure is logged and swallowed — the audit trail must never turn
// a working operation into an error.
func audit(db *store.DB, r *http.Request, action, target, detail string) {
	auditAs(db, actorFor(r), action, target, detail)
}

// auditAs records an event whose actor isn't the request's device — an
// unauthenticated pair/login/register attempt, or the daemon itself.
func auditAs(db *store.DB, actor, action, target, detail string) {
	if err := db.AppendAudit(action, actor, target, detail); err != nil {
		log.Printf("audit: record %s: %v", action, err)
	}
}

// actorFor names the caller for display: its device label, falling back to
// the raw id. (store.Device carries no username field — the login account is
// recorded on the login/register entries themselves.)
func actorFor(r *http.Request) string {
	d := deviceFromContext(r)
	if d == nil {
		return ""
	}
	if d.Label != "" {
		return d.Label
	}
	return d.ID
}

type auditResponse struct {
	Entries []store.AuditEntry `json:"entries"`
}

// listAuditHandler implements GET /v1/audit?limit=&before=. Newest first;
// `before` takes the last id of a previous page to walk backwards.
func listAuditHandler(db *store.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
		before, _ := strconv.ParseInt(r.URL.Query().Get("before"), 10, 64)
		entries, err := db.AuditEntries(limit, before)
		if err != nil {
			writeInternal(w, "list audit", err)
			return
		}
		writeJSON(w, http.StatusOK, auditResponse{Entries: entries})
	}
}
