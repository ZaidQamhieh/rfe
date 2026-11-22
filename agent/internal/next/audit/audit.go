// Package audit is the agent's trail of account, device and share events:
// pairing, registration, login (failures too), device revoke, remove and limit
// changes, share-link mint and revoke, and agent restart. File operations are
// deliberately not recorded: they arrive at transfer volume and would bury the
// security-relevant entries, so Record refuses any action outside the list
// below. The log keeps only the newest Retention entries.
package audit

import (
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/ZaidQamhieh/rfe/agent/internal/next/store"
)

// Action names. They are the values of the OpenAPI AuditEntry.action enum and
// the same strings the old agent wrote, so imported entries keep their meaning.
const (
	ActionPair          = "pair"
	ActionRegister      = "register"
	ActionLogin         = "login"
	ActionLoginFailed   = "login_failed"
	ActionDeviceRevoked = "device_revoked"
	ActionDeviceRemoved = "device_removed"
	ActionDeviceUpdated = "device_updated" // limit, jail or permission change
	ActionShareCreated  = "share_created"
	ActionShareRevoked  = "share_revoked"
	ActionAgentRestart  = "agent_restart"
	ActionAppLaunch     = "app_launch"
	ActionWake          = "wake_on_lan"
)

// Limits. The default and maximum page sizes and the retention are the values
// the OpenAPI /audit description states.
const (
	DefaultLimit     = 100
	MaxLimit         = 500
	DefaultRetention = 5000
	// MaxFieldBytes bounds each of actor, target and detail: they carry text
	// an unauthenticated caller can choose (a login user name, a device label).
	MaxFieldBytes = 1024
)

// ErrUnknownAction is returned by Record for an action outside the recorded set.
var ErrUnknownAction = errors.New("audit: action is not a recorded event")

// recorded is the closed set of actions Record accepts.
var recorded = map[string]struct{}{
	ActionPair: {}, ActionRegister: {}, ActionLogin: {}, ActionLoginFailed: {},
	ActionDeviceRevoked: {}, ActionDeviceRemoved: {}, ActionDeviceUpdated: {},
	ActionShareCreated: {}, ActionShareRevoked: {}, ActionAgentRestart: {},
	ActionAppLaunch: {}, ActionWake: {},
}

// Store is the database the log writes to and reads from; *store.DB is the
// production implementation.
type Store interface {
	AppendAudit(e store.AuditEntry, retain int) (int64, error)
	ListAudit(limit int, beforeID int64) ([]store.AuditEntry, error)
}

// Entry is one recorded event, in the shape GET /audit returns.
type Entry struct {
	ID     int64     `json:"id"`
	At     time.Time `json:"at"`
	Action string    `json:"action"`
	// Actor is the device label or user name responsible, copied in at write
	// time so the entry stays readable after the device or account is gone.
	Actor  string `json:"actor"`
	Target string `json:"target,omitempty"`
	Detail string `json:"detail,omitempty"`
}

// Event is what a caller reports; the log adds the id and the time.
type Event struct {
	Action, Actor, Target, Detail string
}

// Options configures a Log. The zero value of each field selects the default.
type Options struct {
	Now       func() time.Time // default time.Now
	Retention int              // newest entries kept; default DefaultRetention
	// OnError receives the failure of Try (default: dropped); Record returns
	// its error instead.
	OnError func(action string, err error)
}

// Log records and lists audit entries. It is safe for concurrent use because
// the store serialises writes.
type Log struct {
	st   Store
	opts Options
}

// New returns a Log over st, filling unset options with their defaults.
func New(st Store, opts Options) *Log {
	if opts.Now == nil {
		opts.Now = time.Now
	}
	if opts.Retention <= 0 {
		opts.Retention = DefaultRetention
	}
	return &Log{st: st, opts: opts}
}

// Record stores one event stamped with the clock's time and trims the log to
// the retention. It rejects an action outside the recorded set. Actor, target
// and detail are cleaned first (control characters become spaces, invalid
// UTF-8 is replaced, each is cut to MaxFieldBytes) so a hostile login name
// cannot forge lines in a printed log or bloat the table.
func (l *Log) Record(ev Event) (Entry, error) {
	if _, ok := recorded[ev.Action]; !ok {
		return Entry{}, fmt.Errorf("%w: %q", ErrUnknownAction, ev.Action)
	}
	e := store.AuditEntry{
		At:     l.opts.Now().Unix(),
		Action: ev.Action,
		Actor:  clean(ev.Actor),
		Target: clean(ev.Target),
		Detail: clean(ev.Detail),
	}
	id, err := l.st.AppendAudit(e, l.opts.Retention)
	if err != nil {
		return Entry{}, fmt.Errorf("audit: record %s: %w", ev.Action, err)
	}
	e.ID = id
	return toEntry(e), nil
}

// Try records ev for a caller that must not fail because the trail could not
// be written: a failure goes to Options.OnError and is otherwise dropped.
func (l *Log) Try(ev Event) {
	if _, err := l.Record(ev); err != nil && l.opts.OnError != nil {
		l.opts.OnError(ev.Action, err)
	}
}

// Page is one page of entries, newest first. Next is the id to pass as Before
// to read the following (older) page, or 0 when this page reaches the end.
type Page struct {
	Entries []Entry `json:"entries"`
	Next    int64   `json:"-"`
}

// List returns up to limit entries, newest first, older than the entry with id
// before (0 = start from the newest). A limit outside 1..MaxLimit falls back to
// DefaultLimit, as the OpenAPI contract states. Entries is never nil.
func (l *Log) List(limit int, before int64) (Page, error) {
	if limit < 1 || limit > MaxLimit {
		limit = DefaultLimit
	}
	// Ask for one extra row to learn whether an older page exists.
	rows, err := l.st.ListAudit(limit+1, before)
	if err != nil {
		return Page{}, fmt.Errorf("audit: list: %w", err)
	}
	more := len(rows) > limit
	if more {
		rows = rows[:limit]
	}
	p := Page{Entries: make([]Entry, 0, len(rows))}
	for _, r := range rows {
		p.Entries = append(p.Entries, toEntry(r))
	}
	if more {
		p.Next = rows[len(rows)-1].ID
	}
	return p, nil
}

// toEntry converts a stored row to the API shape.
func toEntry(r store.AuditEntry) Entry {
	return Entry{ID: r.ID, At: time.Unix(r.At, 0), Action: r.Action, Actor: r.Actor, Target: r.Target, Detail: r.Detail}
}

// clean makes s safe to store and print: valid UTF-8, no control characters,
// at most MaxFieldBytes bytes (cut on a character boundary).
func clean(s string) string {
	s = strings.ToValidUTF8(s, "�")
	s = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, s)
	if len(s) <= MaxFieldBytes {
		return s
	}
	cut := MaxFieldBytes
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut]
}
