// Package server — list endpoints (plus user removal) backing the web
// companion's Transfers, Users, and Logs pages. All three surface data RFE
// genuinely has (upload sessions, login accounts, the agent's own journald
// output) — none is synthesized.
package server

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"os/exec"
	"path"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/ZaidQamhieh/rfe/agent/internal/store"
	"github.com/ZaidQamhieh/rfe/agent/internal/transfer"
)

// goLogPrefix matches the "2006/01/02 15:04:05 " stamp Go's log.Printf
// prepends by default — redundant with journald's own timestamp, so it's
// stripped from the message for the Logs page.
var goLogPrefix = regexp.MustCompile(`^\d{4}/\d{2}/\d{2} \d{2}:\d{2}:\d{2} `)

// --------- GET /transfers/list ---------

// maxTransferRows caps how many recent transfer rows the list returns. The
// table accumulates every upload session ever opened (thousands of stale
// "open" rows the client never finalized), so the page shows recent activity
// only; the summary counts come from the full-table aggregate below.
const maxTransferRows = 200

func listTransfersHandler(db *store.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// A non-admin device sees only the sessions it opened, and none of the
		// whole-host aggregates or the device/user filter lists — those name
		// other people's devices, accounts, and upload paths (PR-03). Its
		// filter params are ignored rather than honoured, so they can't widen
		// the scope back out.
		caller := deviceFromContext(r)
		admin := isAdminDevice(caller)
		deviceFilter := r.URL.Query().Get("device")
		userFilter := r.URL.Query().Get("user")
		if !admin {
			// An empty device filter means "every device" — never let a
			// non-admin reach that, including the ID-less case.
			if caller == nil || caller.ID == "" {
				writeError(w, http.StatusForbidden, "FORBIDDEN", "device required")
				return
			}
			deviceFilter, userFilter = caller.ID, ""
		}
		transfers, err := db.ListTransfers(maxTransferRows, deviceFilter, userFilter)
		if err != nil {
			writeInternal(w, "list transfers", err)
			return
		}

		var (
			counts    map[string]int
			activeNow int
			devices   = make([]map[string]any, 0)
			users     = make([]string, 0)
		)
		if admin {
			counts, err = db.CountTransfersByStatus()
			if err != nil {
				writeInternal(w, "count transfers by status", err)
				return
			}
			activeNow, err = db.CountActiveTransfers()
			if err != nil {
				writeInternal(w, "count active transfers", err)
				return
			}
			transferDevices, err := db.ListTransferDevices()
			if err != nil {
				writeInternal(w, "list transfer devices", err)
				return
			}
			for _, d := range transferDevices {
				devices = append(devices, map[string]any{"id": d.ID, "label": d.Label, "username": d.Username})
			}
			accounts, err := db.ListUsers()
			if err != nil {
				writeInternal(w, "list users", err)
				return
			}
			for _, u := range accounts {
				users = append(users, u.Username)
			}
			sort.Strings(users)
		} else {
			// Scoped counts, derived from the caller's own rows. Capped at
			// maxTransferRows like the list itself, unlike the admin aggregate.
			counts = make(map[string]int, len(transfers))
			cutoff := time.Now().Add(-store.ActiveTransferWindow).Unix()
			for _, t := range transfers {
				counts[t.Status]++
				if t.Status == "open" && t.UpdatedAt >= cutoff {
					activeNow++
				}
			}
		}
		total := 0
		for _, n := range counts {
			total += n
		}
		rows := make([]map[string]any, 0, len(transfers))
		for _, t := range transfers {
			received := t.ReceivedCount
			var progress float64
			if t.TotalChunks > 0 {
				progress = float64(received) / float64(t.TotalChunks) * 100
			}
			// receivedBytes is an estimate: full chunks received × chunkSize,
			// capped at the real total (the final chunk is usually smaller).
			receivedBytes := int64(received) * int64(t.ChunkSize)
			if receivedBytes > t.TotalSize {
				receivedBytes = t.TotalSize
			}
			rows = append(rows, map[string]any{
				"id":            t.ID,
				"name":          path.Base(t.TargetPath),
				"path":          t.TargetPath,
				"totalSize":     t.TotalSize,
				"receivedBytes": receivedBytes,
				"progress":      progress,
				"status":        t.Status,    // open | completed | failed
				"deviceId":      t.DeviceID,  // "" on rows created before the device_id migration
				"updatedAt":     t.UpdatedAt, // unix seconds, stamped per received chunk; 0 = never
			})
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"total":     total,
			"counts":    counts,    // status -> count, whole table
			"transfers": rows,      // most recent maxTransferRows only
			"activeNow": activeNow, // open transfers that received a chunk recently
			"devices":   devices,   // distinct devices with at least one transfer, for the filter-chip row
			"users":     users,     // distinct login accounts with at least one transfer, for the user filter-chip row
		})
	}
}

// --------- DELETE /transfers/{id} ---------

// deleteTransferHandler removes an inactive transfer session and its temp
// file. Production supplies the manager so deletion shares its activity lease
// with uploads/completion and the stale sweeper. The variadic form preserves
// the handler's DB-only test seam for records without temp files.
func deleteTransferHandler(db *store.DB, managers ...*transfer.Manager) http.HandlerFunc {
	var manager *transfer.Manager
	if len(managers) > 0 {
		manager = managers[0]
	}
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		// Only the device that opened the session (or an admin) may drop its
		// history row. Non-owners get the same 404 a missing ID gets, so a
		// foreign session ID is neither confirmed nor deletable (PR-03,
		// matching callerOwnsTransfer's rule in transferhandlers.go).
		t, err := db.GetTransfer(id)
		if err != nil {
			writeInternal(w, "get transfer", err)
			return
		}
		// GetTransfer reports a missing row as (nil, nil), not sql.ErrNoRows.
		if t == nil || !callerOwnsTransfer(r, t) {
			writeError(w, http.StatusNotFound, "NOT_FOUND", "no such transfer")
			return
		}
		if manager != nil {
			err = manager.DeleteSession(id)
		} else {
			err = db.DeleteTransfer(id)
		}
		switch {
		case err == nil:
			w.WriteHeader(http.StatusNoContent)
		case errors.Is(err, sql.ErrNoRows):
			writeError(w, http.StatusNotFound, "NOT_FOUND", "no such transfer")
		case errors.Is(err, transfer.ErrNotFound):
			writeError(w, http.StatusNotFound, "NOT_FOUND", "no such transfer")
		case errors.Is(err, transfer.ErrSessionActive):
			writeError(w, http.StatusConflict, "TRANSFER_ACTIVE", "transfer is currently in use; retry after the request finishes")
		default:
			writeInternal(w, "delete transfer", err)
		}
	}
}

// --------- GET /users ---------

func listUsersHandler(db *store.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		users, err := db.ListUsers()
		if err != nil {
			writeInternal(w, "list users", err)
			return
		}
		out := make([]map[string]any, 0, len(users))
		for _, u := range users {
			out = append(out, map[string]any{
				"username": u.Username,
				"created":  u.Created.Unix(),
			})
		}
		writeJSON(w, http.StatusOK, out)
	}
}

// --------- DELETE /users/{username} ---------

// deleteUserHandler removes a login account. Admin-gated at the router (see
// the admin group in server.New) — every account is a full admin today (see
// store.User), so only another admin may remove one. There's no self-vs-other
// distinction like device management has: any admin may remove any account,
// except the last one (see store.ErrLastUser), which would brick password
// login entirely.
func deleteUserHandler(db *store.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		username := chi.URLParam(r, "username")
		err := db.DeleteUser(username)
		switch {
		case err == nil:
			w.WriteHeader(http.StatusNoContent)
		case errors.Is(err, store.ErrLastUser):
			writeError(w, http.StatusBadRequest, "LAST_USER", "cannot delete the only remaining login account")
		case errors.Is(err, sql.ErrNoRows):
			writeError(w, http.StatusNotFound, "NOT_FOUND", "no such user")
		default:
			writeInternal(w, "delete user", err)
		}
	}
}

// --------- GET /logs ---------

// maxLogLines caps how much of the journal the logs endpoint returns.
const maxLogLines = 200

// listLogsHandler tails the agent's own journald output. Admin-gated at the
// router (see the admin group in server.New): the journal spans every device,
// so it is not a jailed or read-only caller's to read. Linux/systemd only —
// returns an empty list elsewhere rather than erroring.
func listLogsHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// A wedged journald must not pin a request goroutine and a process
		// forever, so the command gets a deadline tied to the request.
		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, "journalctl", "--user", "-u", "rfe.service",
			"-n", "200", "--no-pager", "-o", "short-iso")
		raw, err := cmd.Output()
		if err != nil {
			// No journald (non-systemd, or not run as a unit): honestly empty.
			writeJSON(w, http.StatusOK, []map[string]any{})
			return
		}
		lines := strings.Split(strings.TrimRight(string(raw), "\n"), "\n")
		if len(lines) > maxLogLines {
			lines = lines[len(lines)-maxLogLines:]
		}
		out := make([]map[string]any, 0, len(lines))
		for _, line := range lines {
			ts, msg := splitLogLine(line)
			out = append(out, map[string]any{"ts": ts, "message": msg})
		}
		writeJSON(w, http.StatusOK, out)
	}
}

// splitLogLine parses a `short-iso` journald line
// ("2006-01-02T15:04:05+ZZ:ZZ host rfe[pid]: message") into the
// timestamp and the message. RFE's logs are plain log.Printf output with no
// severity level, so no level is inferred. Falls back to the whole line as
// the message if the shape doesn't match.
func splitLogLine(line string) (ts, msg string) {
	fields := strings.SplitN(line, " ", 2)
	if len(fields) < 2 {
		return "", line
	}
	ts = fields[0]
	rest := fields[1]
	// Drop the "host rfe[pid]: " prefix if present.
	if i := strings.Index(rest, ": "); i >= 0 {
		rest = rest[i+2:]
	}
	return ts, goLogPrefix.ReplaceAllString(rest, "")
}
