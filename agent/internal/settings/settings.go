// Package settings holds the agent's live-mutable configuration: read-only mode,
// the folder jail (roots), the display name, upload and download bandwidth
// limits, one-time share links, and the phone photo-backup folder. Values are
// persisted in the config table so changes made from the phone or the web
// companion take effect immediately without a restart.
package settings

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"github.com/ZaidQamhieh/rfe/agent/internal/store"
)

const (
	keyReadOnly        = "readOnly"
	keyRoots           = "roots"
	keyAgentName       = "agentName"
	keyMaxUploadBPS    = "maxUploadBytesPerSec"
	keyMaxDownloadBPS  = "maxDownloadBytesPerSec"
	keyAllowSharing    = "allowSharing"
	keyPhotoBackupRoot = "photoBackupRoot"
)

// Store is a concurrency-safe view over the agent's settings.
type Store struct {
	db                 *store.DB
	mu                 sync.RWMutex
	readOnly           bool
	roots              []string
	agentName          string
	maxUploadBytesPS   int64 // 0 = unlimited
	maxDownloadBytesPS int64 // 0 = unlimited
	allowSharing       bool  // R1 one-time share links; always seeds false

	// photoBackupRoot is the PC-side destination folder for phone photo
	// backup, set from the web companion Settings page only (never from a
	// phone). Empty = not configured, in which case phones must refuse to
	// back up rather than fall back to a client-picked path.
	photoBackupRoot string
}

// Initialised reports whether the agent has already written its settings. The admin CLI checks it before
// Load, because Load on a database that was never started would seed an empty root list (unrestricted
// access) and read-only=false behind the daemon's back (audit F-122).
func Initialised(db *store.DB) (bool, error) {
	return db.HasConfig(keyRoots)
}

// Load builds a Store. Any config key that is unset is seeded from the
// provided default (typically a CLI flag) and written back; once a key exists
// in the DB, the persisted value wins on every subsequent load.
func Load(db *store.DB, seedReadOnly bool, seedRoots []string, seedName string) (*Store, error) {
	s := &Store{db: db}

	ro, err := seedConfig(db, keyReadOnly, boolToStr(seedReadOnly))
	if err != nil {
		return nil, err
	}
	s.readOnly = parseBool(ro)

	rootsConfigured, err := db.HasConfig(keyRoots)
	if err != nil {
		return nil, err
	}
	rts, err := db.GetConfig(keyRoots)
	if err != nil {
		return nil, err
	}
	if !rootsConfigured {
		s.roots = normalizeRoots(seedRoots)
		if err := db.SetConfig(keyRoots, joinRoots(s.roots)); err != nil {
			return nil, err
		}
	} else {
		s.roots = splitRoots(rts)
	}

	nm, err := seedConfigKeepEmpty(db, keyAgentName, seedName)
	if err != nil {
		return nil, err
	}
	s.agentName = nm

	// R1: always seeds false, not CLI-configurable
	as, err := seedConfig(db, keyAllowSharing, boolToStr(false))
	if err != nil {
		return nil, err
	}
	s.allowSharing = parseBool(as)

	// No seed: empty means "not configured" (the owner sets it explicitly
	// from the web companion), unlike the other keys above.
	if v, err := db.GetConfig(keyPhotoBackupRoot); err != nil {
		return nil, err
	} else {
		s.photoBackupRoot = v
	}

	// Bandwidth limits default to 0 (unlimited) and are only written on
	// first explicit set — no seed needed.
	if s.maxUploadBytesPS, err = loadBandwidth(db, keyMaxUploadBPS); err != nil {
		return nil, err
	}
	if s.maxDownloadBytesPS, err = loadBandwidth(db, keyMaxDownloadBPS); err != nil {
		return nil, err
	}

	return s, nil
}

// IsReadOnly reports whether writes are currently rejected.
func (s *Store) IsReadOnly() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.readOnly
}

// Roots returns a copy of the current jail roots (empty = allow all).
func (s *Store) Roots() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]string, len(s.roots))
	copy(out, s.roots)
	return out
}

// AgentName returns the current display name.
func (s *Store) AgentName() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.agentName
}

// IsAllowSharing reports whether R1 one-time share links are enabled.
func (s *Store) IsAllowSharing() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.allowSharing
}

// SetAllowSharing persists and applies the share-links-enabled flag.
func (s *Store) SetAllowSharing(v bool) error {
	return s.persist(keyAllowSharing, boolToStr(v), func() { s.allowSharing = v })
}

// SetReadOnly persists and applies the read-only flag.
func (s *Store) SetReadOnly(v bool) error {
	return s.persist(keyReadOnly, boolToStr(v), func() { s.readOnly = v })
}

// SetRoots normalizes, persists, and applies the jail roots.
func (s *Store) SetRoots(roots []string) error {
	norm := normalizeRoots(roots)
	return s.persist(keyRoots, joinRoots(norm), func() { s.roots = norm })
}

// SetAgentName persists and applies the display name.
func (s *Store) SetAgentName(name string) error {
	return s.persist(keyAgentName, name, func() { s.agentName = name })
}

// PhotoBackupRoot returns the configured phone-photo-backup destination
// folder, or "" if the owner hasn't set one yet.
func (s *Store) PhotoBackupRoot() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.photoBackupRoot
}

// SetPhotoBackupRoot persists and applies the photo-backup destination
// folder. Web-companion-only by convention (no phone-facing route calls this).
func (s *Store) SetPhotoBackupRoot(v string) error {
	v = strings.TrimSpace(v)
	return s.persist(keyPhotoBackupRoot, v, func() { s.photoBackupRoot = v })
}

// MaxUploadBytesPerSec returns the upload throttle (0 = unlimited).
func (s *Store) MaxUploadBytesPerSec() int64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.maxUploadBytesPS
}

// MaxDownloadBytesPerSec returns the download throttle (0 = unlimited).
func (s *Store) MaxDownloadBytesPerSec() int64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.maxDownloadBytesPS
}

// SetMaxUploadBytesPerSec persists and applies the upload throttle.
func (s *Store) SetMaxUploadBytesPerSec(v int64) error {
	v = clampBandwidth(v)
	return s.persist(keyMaxUploadBPS, strconv.FormatInt(v, 10), func() { s.maxUploadBytesPS = v })
}

// SetMaxDownloadBytesPerSec persists and applies the download throttle.
func (s *Store) SetMaxDownloadBytesPerSec(v int64) error {
	v = clampBandwidth(v)
	return s.persist(keyMaxDownloadBPS, strconv.FormatInt(v, 10), func() { s.maxDownloadBytesPS = v })
}

// persist writes key to the config table, then runs apply, all under the write
// lock. The DB write comes first, so a failed write leaves the in-memory value
// unchanged.
func (s *Store) persist(key, value string, apply func()) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.db.SetConfig(key, value); err != nil {
		return err
	}
	apply()
	return nil
}

func boolToStr(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

// parseBool is the inverse of boolToStr: only the exact stored "true" is true,
// so any other value (including an empty or corrupt one) reads as false.
func parseBool(s string) bool {
	return s == "true"
}

// roots are stored newline-joined (paths may contain commas but not newlines).
func joinRoots(roots []string) string { return strings.Join(roots, "\n") }

func splitRoots(v string) []string {
	return normalizeRoots(strings.Split(v, "\n"))
}

func normalizeRoots(in []string) []string {
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, r := range in {
		r = strings.TrimSpace(r)
		if r == "" {
			continue
		}
		c := filepath.Clean(r)
		// A relative root can never match a request path (they are absolute), so every browse would fail with
		// "path must be absolute"; resolve it against the working directory once, here.
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

// parseBandwidth reads a stored bandwidth limit. A value that is not a
// non-negative integer is an error rather than silently becoming 0, which
// would mean "unlimited".
func parseBandwidth(key, v string) (int64, error) {
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil || n < 0 {
		return 0, fmt.Errorf("setting %s has an invalid value %q: expected a whole number of bytes per second (0 = unlimited); fix or delete this row in the config table of the agent database (sqlite3 <data dir>/agent.db)", key, v)
	}
	return n, nil
}

// seedConfig returns the stored value for key. If the key is unset it writes
// seed back first, so the seed becomes the persisted value.
func seedConfig(db *store.DB, key, seed string) (string, error) {
	v, err := db.GetConfig(key)
	if err != nil {
		return "", err
	}
	if v != "" {
		return v, nil
	}
	if err := db.SetConfig(key, seed); err != nil {
		return "", err
	}
	return seed, nil
}

// seedConfigKeepEmpty is seedConfig for a key whose empty value is valid (an owner who cleared the agent
// name): only a key that is absent is seeded, a stored empty string is kept.
func seedConfigKeepEmpty(db *store.DB, key, seed string) (string, error) {
	exists, err := db.HasConfig(key)
	if err != nil {
		return "", err
	}
	if exists {
		return db.GetConfig(key)
	}
	if err := db.SetConfig(key, seed); err != nil {
		return "", err
	}
	return seed, nil
}

// loadBandwidth returns the stored bandwidth limit for key, or 0 (unlimited)
// when it has never been set.
func loadBandwidth(db *store.DB, key string) (int64, error) {
	v, err := db.GetConfig(key)
	if err != nil || v == "" {
		return 0, err
	}
	return parseBandwidth(key, v)
}

// clampBandwidth is the write-side rule for bandwidth limits: a negative value
// is stored as 0 (unlimited). Load does not clamp; parseBandwidth rejects a
// negative stored value instead.
func clampBandwidth(v int64) int64 {
	if v < 0 {
		return 0
	}
	return v
}
