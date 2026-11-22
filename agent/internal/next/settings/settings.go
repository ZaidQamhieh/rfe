// Package settings holds the agent's owner-controlled configuration: the
// folders the agent may expose (roots), read-only mode, the display name,
// share-link switch, photo-backup destination and the upload/download
// bandwidth limits. Values live in the v3 database's settings table under the
// same keys the old agent used (and the legacy import copies), so an imported
// database keeps its policy. Update validates every field before it writes
// anything, writes all changed fields in one transaction, and then tells the
// subscribers what changed.
package settings

import (
	"errors"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"

	"github.com/ZaidQamhieh/rfe/agent/internal/next/store"
)

// Keys of the settings table. They are the old agent's config keys, written
// by the legacy import, so they must not change.
const (
	KeyReadOnly        = "readOnly"
	KeyRoots           = "roots"
	KeyAgentName       = "agentName"
	KeyAllowSharing    = "allowSharing"
	KeyPhotoBackupRoot = "photoBackupRoot"
	KeyMaxUploadBPS    = "maxUploadBytesPerSec"
	KeyMaxDownloadBPS  = "maxDownloadBytesPerSec"
)

// Defaults and limits.
const (
	// DefaultFilesFolderName is the folder under the user's home that a new
	// agent exposes unless the owner picks other roots or unrestricted access.
	DefaultFilesFolderName = "RFE Files"
	// DefaultAgentName is used when the caller supplies no display name.
	DefaultAgentName = "RFE agent"
	// DefaultReadOnly and DefaultAllowSharing are the seeds for a new database.
	DefaultReadOnly     = false
	DefaultAllowSharing = false

	MaxAgentNameRunes = 64
	MaxRoots          = 256
	MaxPathBytes      = 4096

	// rootSeparator joins the roots into the one settings value. A path cannot
	// contain a newline (Update rejects control characters), so it is safe.
	rootSeparator = "\n"
	trueValue     = "true"
	falseValue    = "false"
)

// ErrInvalid is matched (errors.Is) by every validation failure of Update.
var ErrInvalid = errors.New("invalid setting")

// ErrNoDefaultRoot is returned by Open for a new database when the owner chose
// no roots explicitly and no home directory is known: the agent must not fall
// back to unrestricted access on its own.
var ErrNoDefaultRoot = errors.New("settings: no home directory for the default folder and no roots chosen")

// InvalidError names the field and the reason a value was refused.
type InvalidError struct{ Field, Reason string }

func (e *InvalidError) Error() string {
	return fmt.Sprintf("invalid setting %s: %s", e.Field, e.Reason)
}

// Is makes errors.Is(err, ErrInvalid) true for an InvalidError.
func (e *InvalidError) Is(target error) bool { return target == ErrInvalid }

// invalid builds an InvalidError.
func invalid(field, format string, args ...any) error {
	return &InvalidError{Field: field, Reason: fmt.Sprintf(format, args...)}
}

// Store is the database the settings live in; *store.DB is the production
// implementation.
type Store interface {
	Settings() (map[string]string, error)
	SeedSettings(kv map[string]string) error
	SetSettings(kv map[string]string) error
}

// Settings is the full typed configuration.
type Settings struct {
	ReadOnly bool
	// Roots are the absolute folders the agent may expose. Empty means
	// unrestricted access, which only an explicit choice produces.
	Roots                  []string
	AgentName              string
	AllowSharing           bool
	PhotoBackupRoot        string // empty = not configured
	MaxUploadBytesPerSec   int64  // 0 = unlimited
	MaxDownloadBytesPerSec int64  // 0 = unlimited
}

// Unrestricted reports whether no folder limit is configured.
func (s Settings) Unrestricted() bool { return len(s.Roots) == 0 }

// Update is a partial change: a nil field is left as it is. A non-nil Roots
// pointing at an empty list selects unrestricted access.
type Update struct {
	ReadOnly               *bool
	Roots                  *[]string
	AgentName              *string
	AllowSharing           *bool
	PhotoBackupRoot        *string
	MaxUploadBytesPerSec   *int64
	MaxDownloadBytesPerSec *int64
}

// Change is what a subscriber receives after a successful Update.
type Change struct{ Old, New Settings }

// Options seeds the settings of a database that has none yet. Rows already
// stored always win over these.
type Options struct {
	// HomeDir is the signed-in user's home directory; the default root is
	// HomeDir/DefaultFilesFolderName.
	HomeDir string
	// RootsExplicit says the owner chose SeedRoots: used as given, and an empty
	// list means unrestricted. When false SeedRoots is ignored and the default
	// folder is used.
	RootsExplicit bool
	SeedRoots     []string
	ReadOnly      bool
	AgentName     string
}

// Service is the live settings, safe for concurrent use.
type Service struct {
	st Store

	updMu sync.Mutex // serialises Update including its notifications
	mu    sync.RWMutex
	cur   Settings

	subMu  sync.Mutex
	nextID int
	subs   map[int]func(Change)
}

// Initialised reports whether the database already holds the agent's settings
// (the roots row exists). The admin CLI checks it before Open so a database the
// daemon never started is not seeded behind its back.
func Initialised(st Store) (bool, error) {
	kv, err := st.Settings()
	if err != nil {
		return false, err
	}
	_, ok := kv[KeyRoots]
	return ok, nil
}

// Open loads the stored settings, first writing a seed for every key that has
// no row. An existing roots row, even an empty one (unrestricted), is kept as
// it is. A stored value that cannot be read as its type is an error naming the
// key, never silently replaced by a default.
func Open(st Store, opts Options) (*Service, error) {
	kv, err := st.Settings()
	if err != nil {
		return nil, fmt.Errorf("settings: read: %w", err)
	}
	seed := map[string]string{}
	if _, ok := kv[KeyReadOnly]; !ok {
		seed[KeyReadOnly] = formatBool(opts.ReadOnly)
	}
	if _, ok := kv[KeyAllowSharing]; !ok {
		seed[KeyAllowSharing] = formatBool(DefaultAllowSharing)
	}
	if _, ok := kv[KeyAgentName]; !ok {
		seed[KeyAgentName] = seedName(opts)
	}
	if _, ok := kv[KeyRoots]; !ok {
		roots, err := seedRoots(opts)
		if err != nil {
			return nil, err
		}
		seed[KeyRoots] = strings.Join(roots, rootSeparator)
	}
	if len(seed) > 0 {
		if err := st.SeedSettings(seed); err != nil {
			return nil, fmt.Errorf("settings: seed: %w", err)
		}
		// Another process may have seeded first; read back what is stored.
		if kv, err = st.Settings(); err != nil {
			return nil, fmt.Errorf("settings: read: %w", err)
		}
	}
	cur, err := parse(kv, opts)
	if err != nil {
		return nil, err
	}
	return &Service{st: st, cur: cur, subs: map[int]func(Change){}}, nil
}

// seedName returns the display name for a new database.
func seedName(opts Options) string {
	if n := strings.TrimSpace(opts.AgentName); n != "" {
		return n
	}
	return DefaultAgentName
}

// seedRoots returns the roots for a new database: the owner's explicit choice,
// else the default folder under the home directory.
func seedRoots(opts Options) ([]string, error) {
	if opts.RootsExplicit {
		return cleanRootsLoose(opts.SeedRoots), nil
	}
	if opts.HomeDir == "" {
		return nil, ErrNoDefaultRoot
	}
	home, err := filepath.Abs(opts.HomeDir)
	if err != nil {
		return nil, fmt.Errorf("settings: home directory: %w", err)
	}
	return []string{filepath.Join(home, DefaultFilesFolderName)}, nil
}

// parse turns the stored rows into Settings. An empty value for the switches
// and the name counts as unset (the old agent re-seeded those); an empty roots
// row is a deliberate unrestricted setting.
func parse(kv map[string]string, opts Options) (Settings, error) {
	var s Settings
	var err error
	if s.ReadOnly, err = parseBool(kv, KeyReadOnly, opts.ReadOnly); err != nil {
		return s, err
	}
	if s.AllowSharing, err = parseBool(kv, KeyAllowSharing, DefaultAllowSharing); err != nil {
		return s, err
	}
	if s.AgentName = kv[KeyAgentName]; s.AgentName == "" {
		s.AgentName = seedName(opts)
	}
	s.Roots = cleanRootsLoose(strings.Split(kv[KeyRoots], rootSeparator))
	s.PhotoBackupRoot = kv[KeyPhotoBackupRoot]
	if s.MaxUploadBytesPerSec, err = parseBandwidth(kv, KeyMaxUploadBPS); err != nil {
		return s, err
	}
	if s.MaxDownloadBytesPerSec, err = parseBandwidth(kv, KeyMaxDownloadBPS); err != nil {
		return s, err
	}
	return s, nil
}

// parseBool reads a stored "true"/"false"; a missing or empty row gives def.
func parseBool(kv map[string]string, key string, def bool) (bool, error) {
	switch v := kv[key]; v {
	case "":
		return def, nil
	case trueValue:
		return true, nil
	case falseValue:
		return false, nil
	default:
		return false, fmt.Errorf("settings: %s has the stored value %q, expected %q or %q", key, v, trueValue, falseValue)
	}
}

// parseBandwidth reads a stored limit; a value that is not a non-negative
// whole number is an error rather than a silent 0 (which means unlimited).
func parseBandwidth(kv map[string]string, key string) (int64, error) {
	v := kv[key]
	if v == "" {
		return 0, nil
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil || n < 0 {
		return 0, fmt.Errorf("settings: %s has the stored value %q, expected a whole number of bytes per second (0 = unlimited)", key, v)
	}
	return n, nil
}

// cleanRootsLoose normalises roots already chosen or stored: blanks are
// dropped, paths cleaned, relative ones resolved against the working
// directory (what the old agent did, so a stored policy keeps its meaning),
// duplicates removed. Update uses the strict check instead.
func cleanRootsLoose(in []string) []string {
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, r := range in {
		r = strings.TrimSpace(r)
		if r == "" {
			continue
		}
		c := filepath.Clean(r)
		if abs, err := filepath.Abs(c); err == nil {
			c = abs
		}
		if _, dup := seen[c]; dup {
			continue
		}
		seen[c] = struct{}{}
		out = append(out, c)
	}
	return out
}

// Get returns a copy of the current settings.
func (s *Service) Get() Settings {
	s.mu.RLock()
	defer s.mu.RUnlock()
	c := s.cur
	c.Roots = append([]string{}, s.cur.Roots...)
	return c
}

// Subscribe registers fn to be called, in order, after each Update that
// changed something. fn runs on the updating goroutine with no settings lock
// held, so it may call Get but must not call Update. The returned function
// removes the subscription.
func (s *Service) Subscribe(fn func(Change)) (cancel func()) {
	s.subMu.Lock()
	defer s.subMu.Unlock()
	id := s.nextID
	s.nextID++
	s.subs[id] = fn
	return func() {
		s.subMu.Lock()
		defer s.subMu.Unlock()
		delete(s.subs, id)
	}
}

// Update validates every given field, then writes the changed ones in a
// single transaction, applies them and notifies subscribers; it returns the
// new settings. If any field is invalid, or the write fails, nothing is
// changed. A request that changes nothing writes and notifies nothing.
func (s *Service) Update(u Update) (Settings, error) {
	s.updMu.Lock()
	defer s.updMu.Unlock()
	old := s.Get()
	next := old
	rows := map[string]string{}

	if u.ReadOnly != nil && *u.ReadOnly != old.ReadOnly {
		next.ReadOnly = *u.ReadOnly
		rows[KeyReadOnly] = formatBool(next.ReadOnly)
	}
	if u.AllowSharing != nil && *u.AllowSharing != old.AllowSharing {
		next.AllowSharing = *u.AllowSharing
		rows[KeyAllowSharing] = formatBool(next.AllowSharing)
	}
	if u.AgentName != nil {
		name, err := validateName(*u.AgentName)
		if err != nil {
			return old, err
		}
		if name != old.AgentName {
			next.AgentName = name
			rows[KeyAgentName] = name
		}
	}
	if u.Roots != nil {
		roots, err := validateRoots(*u.Roots)
		if err != nil {
			return old, err
		}
		if !sameList(roots, old.Roots) {
			next.Roots = roots
			rows[KeyRoots] = strings.Join(roots, rootSeparator)
		}
	}
	if u.PhotoBackupRoot != nil {
		p, err := validatePhotoRoot(*u.PhotoBackupRoot)
		if err != nil {
			return old, err
		}
		if p != old.PhotoBackupRoot {
			next.PhotoBackupRoot = p
			rows[KeyPhotoBackupRoot] = p
		}
	}
	for _, b := range []struct {
		key string
		in  *int64
		to  *int64
		was int64
	}{
		{KeyMaxUploadBPS, u.MaxUploadBytesPerSec, &next.MaxUploadBytesPerSec, old.MaxUploadBytesPerSec},
		{KeyMaxDownloadBPS, u.MaxDownloadBytesPerSec, &next.MaxDownloadBytesPerSec, old.MaxDownloadBytesPerSec},
	} {
		if b.in == nil {
			continue
		}
		if *b.in < 0 {
			return old, invalid(b.key, "%d is negative; use 0 for unlimited", *b.in)
		}
		if *b.in != b.was {
			*b.to = *b.in
			rows[b.key] = strconv.FormatInt(*b.in, 10)
		}
	}

	if len(rows) == 0 {
		return old, nil
	}
	if err := s.st.SetSettings(rows); err != nil {
		return old, fmt.Errorf("settings: save: %w", err)
	}
	s.mu.Lock()
	s.cur = next
	s.mu.Unlock()
	s.notify(Change{Old: old, New: s.Get()})
	return s.Get(), nil
}

// notify calls every subscriber with c.
func (s *Service) notify(c Change) {
	s.subMu.Lock()
	fns := make([]func(Change), 0, len(s.subs))
	for id := 0; id < s.nextID; id++ {
		if fn, ok := s.subs[id]; ok {
			fns = append(fns, fn)
		}
	}
	s.subMu.Unlock()
	for _, fn := range fns {
		fn(c)
	}
}

// formatBool is the stored text of a switch.
func formatBool(b bool) string {
	if b {
		return trueValue
	}
	return falseValue
}

// sameList reports whether a and b hold the same strings in the same order.
func sameList(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// hasControl reports whether s has a control character (a newline included).
func hasControl(s string) bool {
	return strings.IndexFunc(s, unicode.IsControl) >= 0
}

// validateName checks a display name and returns it trimmed.
func validateName(in string) (string, error) {
	name := strings.TrimSpace(in)
	switch {
	case name == "":
		return "", invalid(KeyAgentName, "must not be empty")
	case !utf8.ValidString(name) || hasControl(name):
		return "", invalid(KeyAgentName, "must be plain text without control characters")
	case utf8.RuneCountInString(name) > MaxAgentNameRunes:
		return "", invalid(KeyAgentName, "is longer than %d characters", MaxAgentNameRunes)
	}
	return name, nil
}

// validateAbsPath checks one folder path for field and returns it cleaned. It
// must be absolute: a relative path could never match a request path.
func validateAbsPath(field, in string) (string, error) {
	p := strings.TrimSpace(in)
	switch {
	case p == "":
		return "", invalid(field, "a path must not be blank")
	case !utf8.ValidString(p) || hasControl(p):
		return "", invalid(field, "path %q has invalid characters", p)
	case len(p) > MaxPathBytes:
		return "", invalid(field, "a path is longer than %d bytes", MaxPathBytes)
	case !filepath.IsAbs(p):
		return "", invalid(field, "path %q is not absolute", p)
	}
	return filepath.Clean(p), nil
}

// validateRoots checks the roots list and returns it cleaned and without
// duplicates. An empty list is accepted: it is the explicit unrestricted choice.
func validateRoots(in []string) ([]string, error) {
	if len(in) > MaxRoots {
		return nil, invalid(KeyRoots, "more than %d folders", MaxRoots)
	}
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, r := range in {
		c, err := validateAbsPath(KeyRoots, r)
		if err != nil {
			return nil, err
		}
		if _, dup := seen[c]; dup {
			continue
		}
		seen[c] = struct{}{}
		out = append(out, c)
	}
	return out, nil
}

// validatePhotoRoot checks the photo-backup folder; empty clears it.
func validatePhotoRoot(in string) (string, error) {
	if strings.TrimSpace(in) == "" {
		return "", nil
	}
	return validateAbsPath(KeyPhotoBackupRoot, in)
}

// The v3 database is the settings store in production.
var _ Store = (*store.DB)(nil)
