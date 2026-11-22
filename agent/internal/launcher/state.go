// Package launcher holds what the rfe-launcher binary needs: the on-disk install layout, the crash-safe
// state.json that says which agent version runs next, the pure state machine over that file, the single-instance
// lock and the child-process supervisor loop. It uses the standard library and internal/bundle only: no cgo, no
// database, no TLS.
package launcher

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"syscall"
	"time"
)

// Names inside an install root: <root>/versions/<v>/ holds one bundle, <root>/staging/ is where a new bundle is
// assembled before it is renamed into versions/.
const (
	StateFile   = "state.json"
	BackupFile  = "state.json.bak"
	VersionsDir = "versions"
	StagingDir  = "staging"
	LockFile    = "launcher.lock"

	// MaxAttempts is how many failed runs of a pending version are tolerated before it is marked bad.
	MaxAttempts = 2
	// ExitRestartIntoPending is the reserved exit code with which an agent asks the launcher to restart it
	// into the pending version.
	ExitRestartIntoPending = 75

	maxStateBytes = 1 << 20
	maxVersionLen = 64
)

// State is the content of state.json. Current is the version that runs normally, Previous the one before it
// (the rollback target), Pending a staged version waiting to be tried, Attempts counts failed runs per version,
// Bad lists versions that must never start again.
type State struct {
	Current                 string         `json:"current"`
	Previous                string         `json:"previous"`
	Pending                 string         `json:"pending"`
	Attempts                map[string]int `json:"attempts"`
	Bad                     []string       `json:"bad"`
	ExpectedCertFingerprint string         `json:"expected_cert_fingerprint"`
	Updated                 time.Time      `json:"updated"`
}

// Errors the state functions return; test with errors.Is.
var (
	ErrCorruptState = errors.New("state file is corrupt")
	ErrTransition   = errors.New("invalid state transition")
	ErrNoFallback   = errors.New("no good version to fall back to")
)

// ValidVersion reports whether v is usable as a directory name under versions/: 1 to 64 characters, starting with a
// letter or digit, then letters, digits and . _ + - only. This keeps a version taken from a file from escaping the root.
func ValidVersion(v string) bool {
	if v == "" || len(v) > maxVersionLen {
		return false
	}
	for i, r := range v {
		alnum := r >= '0' && r <= '9' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z'
		if i == 0 && !alnum {
			return false
		}
		if !alnum && r != '.' && r != '_' && r != '+' && r != '-' {
			return false
		}
	}
	return true
}

// IsBad reports whether v is listed as bad.
func (s State) IsBad(v string) bool {
	for _, b := range s.Bad {
		if b == v {
			return true
		}
	}
	return false
}

// clone returns a deep copy with non-nil collections, so Next never changes its input and JSON shows {} and [].
func (s State) clone() State {
	c := s
	c.Attempts = make(map[string]int, len(s.Attempts))
	for k, v := range s.Attempts {
		c.Attempts[k] = v
	}
	c.Bad = append([]string{}, s.Bad...)
	return c
}

// validate checks every version name in the state.
func (s State) validate() error {
	if s.ExpectedCertFingerprint != "" && !validFingerprint(s.ExpectedCertFingerprint) {
		return fmt.Errorf("%w: invalid expected_cert_fingerprint", ErrCorruptState)
	}
	for _, v := range append([]string{s.Current, s.Previous, s.Pending}, s.Bad...) {
		if v != "" && !ValidVersion(v) {
			return fmt.Errorf("%w: invalid version name %q", ErrCorruptState, v)
		}
	}
	for v := range s.Attempts {
		if !ValidVersion(v) {
			return fmt.Errorf("%w: invalid version name %q in attempts", ErrCorruptState, v)
		}
	}
	return nil
}

// EventKind names a state transition.
type EventKind string

// The transitions of the state machine.
const (
	// EvSetCurrent records the version that runs normally; only on a fresh state (or a repeat of the current one).
	EvSetCurrent EventKind = "set_current"
	// EvSetPending stages a version to be tried at the next restart.
	EvSetPending EventKind = "set_pending"
	// EvPromote makes the pending version current (the old current becomes previous). Phase 5 calls it after probation.
	EvPromote EventKind = "promote"
	// EvFailPending records one failed run of the pending version; at MaxAttempts it is marked bad and dropped.
	EvFailPending EventKind = "fail_pending"
	// EvSetFingerprint records the agent's TLS certificate fingerprint the launcher expects to see (only while none
	// is recorded). Event.Version carries it (64 lowercase hex characters).
	EvSetFingerprint EventKind = "set_fingerprint"
	// EvDowngrade makes an already installed, non-bad version current at once (the old current becomes previous) and
	// drops any pending version. The offline "rfe downgrade" uses it while no launcher runs.
	EvDowngrade EventKind = "downgrade"
	// EvMarkBad marks a version bad at once (for example its binary failed verification) and moves off it.
	EvMarkBad EventKind = "mark_bad"
)

// Event is an input of Next. At becomes State.Updated.
type Event struct {
	Kind    EventKind
	Version string
	At      time.Time
}

// Next applies one event to a state and returns the new state; the input is not changed and nothing is read or
// written, so every transition can be tested as a table. A transition that makes no sense returns ErrTransition.
func Next(s State, ev Event) (State, error) {
	n := s.clone()
	n.Updated = ev.At.UTC()
	v := ev.Version
	switch ev.Kind {
	case EvSetCurrent:
		if !ValidVersion(v) || n.IsBad(v) {
			return s, fmt.Errorf("%w: cannot set current %q", ErrTransition, v)
		}
		if n.Current != "" && n.Current != v {
			return s, fmt.Errorf("%w: current is already %q", ErrTransition, n.Current)
		}
		n.Current = v
		if n.Pending == v {
			n.Pending = ""
		}
	case EvSetPending:
		if !ValidVersion(v) || n.IsBad(v) || n.Current == "" || v == n.Current {
			return s, fmt.Errorf("%w: cannot stage %q (current %q)", ErrTransition, v, n.Current)
		}
		n.Pending = v
		delete(n.Attempts, v)
	case EvPromote:
		if n.Pending == "" {
			return s, fmt.Errorf("%w: nothing pending to promote", ErrTransition)
		}
		n.Previous, n.Current, n.Pending = n.Current, n.Pending, ""
		delete(n.Attempts, n.Current)
	case EvFailPending:
		if n.Pending == "" || (v != "" && v != n.Pending) {
			return s, fmt.Errorf("%w: %q is not the pending version", ErrTransition, v)
		}
		p := n.Pending
		n.Attempts[p]++
		if n.Attempts[p] >= MaxAttempts {
			n.Bad = appendUnique(n.Bad, p)
			n.Pending = ""
		}
	case EvSetFingerprint:
		if !validFingerprint(v) {
			return s, fmt.Errorf("%w: %q is not a SHA-256 fingerprint", ErrTransition, v)
		}
		if n.ExpectedCertFingerprint != "" && n.ExpectedCertFingerprint != v {
			return s, fmt.Errorf("%w: a different fingerprint is already recorded", ErrTransition)
		}
		n.ExpectedCertFingerprint = v
	case EvDowngrade:
		if !ValidVersion(v) || n.IsBad(v) || v == n.Current || n.Current == "" {
			return s, fmt.Errorf("%w: cannot switch to %q (current %q)", ErrTransition, v, n.Current)
		}
		n.Previous, n.Current, n.Pending = n.Current, v, ""
		delete(n.Attempts, v)
	case EvMarkBad:
		if !ValidVersion(v) {
			return s, fmt.Errorf("%w: invalid version %q", ErrTransition, v)
		}
		if v == n.Current && (n.Previous == "" || n.IsBad(n.Previous) || n.Previous == v) {
			return s, fmt.Errorf("%w: %q is current and has no good previous", ErrNoFallback, v)
		}
		n.Bad = appendUnique(n.Bad, v)
		if n.Pending == v {
			n.Pending = ""
		}
		if n.Current == v {
			n.Current, n.Previous = n.Previous, ""
		}
		if n.Previous == v {
			n.Previous = ""
		}
	default:
		return s, fmt.Errorf("%w: unknown event %q", ErrTransition, ev.Kind)
	}
	return n, nil
}

// validFingerprint reports whether f is a lowercase hex SHA-256 (64 characters).
func validFingerprint(f string) bool {
	if len(f) != 64 {
		return false
	}
	for _, r := range f {
		if !(r >= '0' && r <= '9' || r >= 'a' && r <= 'f') {
			return false
		}
	}
	return true
}

func appendUnique(list []string, v string) []string {
	for _, x := range list {
		if x == v {
			return list
		}
	}
	return append(list, v)
}

// Pick says which version to start: the pending one while it is not bad and has failed fewer than MaxAttempts
// times, otherwise the current one. The bool is true when the pending version was chosen.
func Pick(s State) (string, bool) {
	if s.Pending != "" && !s.IsBad(s.Pending) && s.Attempts[s.Pending] < MaxAttempts {
		return s.Pending, true
	}
	return s.Current, false
}

// Store reads and writes the state of one install root. hook is a test seam: it is called before each durable
// step of a write and may panic or fail to simulate a kill at that point.
type Store struct {
	Root string
	hook func(step string) error
	now  func() time.Time
}

// NewStore returns a Store for the install root.
func NewStore(root string) *Store { return &Store{Root: root, now: time.Now} }

func (s *Store) path(name string) string { return filepath.Join(s.Root, name) }

// VersionDir is <root>/versions/<v>, refusing a version name that is not a plain directory name.
func (s *Store) VersionDir(v string) (string, error) {
	if !ValidVersion(v) {
		return "", fmt.Errorf("invalid version name %q", v)
	}
	return filepath.Join(s.Root, VersionsDir, v), nil
}

// StagingPath is <root>/staging.
func (s *Store) StagingPath() string { return s.path(StagingDir) }

// LockPath is the single-instance lock file of this root.
func (s *Store) LockPath() string { return s.path(LockFile) }

// Read loads state.json. A missing file (a fresh install) gives an empty state and no error. A file that is not
// valid JSON, is too large, holds an unsafe version name, or is not a plain file is refused with an error that
// wraps ErrCorruptState and names state.json.bak, the last good copy; Read never falls back to it by itself.
func (s *Store) Read() (State, error) {
	p := s.path(StateFile)
	st, err := os.Lstat(p)
	if errors.Is(err, fs.ErrNotExist) {
		return State{}.clone(), nil
	}
	if err != nil {
		return State{}, err
	}
	if !st.Mode().IsRegular() {
		return State{}, fmt.Errorf("%w: %s is not a plain file", ErrCorruptState, p)
	}
	f, err := os.Open(p)
	if err != nil {
		return State{}, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxStateBytes+1))
	if err != nil {
		return State{}, err
	}
	hint := fmt.Sprintf("; the last good copy is %s", s.path(BackupFile))
	if len(data) > maxStateBytes {
		return State{}, fmt.Errorf("%w: %s is larger than %d bytes%s", ErrCorruptState, p, maxStateBytes, hint)
	}
	var out State
	if err := json.Unmarshal(data, &out); err != nil {
		return State{}, fmt.Errorf("%w: %s is not valid JSON (%v)%s", ErrCorruptState, p, err, hint)
	}
	if err := out.validate(); err != nil {
		return State{}, fmt.Errorf("%w%s", err, hint)
	}
	return out.clone(), nil
}

// Write replaces state.json atomically (temp file, fsync, rename, directory fsync) and then does the same for
// state.json.bak, so a kill at any point leaves state.json as the complete old or complete new state, and the
// backup as a complete state too. Both files are plain files; a symlink at either name is refused.
func (s *Store) Write(st State) error {
	st = st.clone()
	if err := st.validate(); err != nil {
		return err
	}
	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if err := os.MkdirAll(s.Root, 0o700); err != nil {
		return err
	}
	for _, name := range []string{StateFile, BackupFile} {
		if fi, err := os.Lstat(s.path(name)); err == nil && !fi.Mode().IsRegular() {
			return fmt.Errorf("%s is not a plain file", s.path(name))
		}
	}
	if err := s.replace(StateFile, data, "state"); err != nil {
		return err
	}
	return s.replace(BackupFile, data, "bak")
}

// step runs the test hook, if any, before a durable step.
func (s *Store) step(name string) error {
	if s.hook != nil {
		return s.hook(name)
	}
	return nil
}

// replace writes data to a new temp file in the root and renames it over name, syncing the file and then the
// directory. The temp file is removed on an error; a panic (a simulated kill) leaves it, as a real kill would.
func (s *Store) replace(name string, data []byte, tag string) error {
	if err := s.step(tag + ":write-tmp"); err != nil {
		return err
	}
	f, err := os.CreateTemp(s.Root, name+".*.tmp")
	if err != nil {
		return err
	}
	tmp := f.Name()
	fail := func(err error) error {
		f.Close()
		os.Remove(tmp)
		return err
	}
	if _, err := f.Write(data); err != nil {
		return fail(err)
	}
	if err := s.step(tag + ":sync-tmp"); err != nil {
		return fail(err)
	}
	if err := f.Sync(); err != nil {
		return fail(err)
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	if err := s.step(tag + ":rename"); err != nil {
		os.Remove(tmp)
		return err
	}
	if err := renameReplace(tmp, s.path(name)); err != nil {
		os.Remove(tmp)
		return err
	}
	if err := s.step(tag + ":sync-dir"); err != nil {
		return err
	}
	return syncDir(s.Root)
}

// syncDir flushes a directory's entries to disk so a rename survives power loss. Windows cannot sync a directory
// handle, and a few filesystems answer EINVAL; both are skipped.
func syncDir(dir string) error {
	if runtime.GOOS == "windows" {
		return nil
	}
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer d.Close()
	if err := d.Sync(); err != nil && !errors.Is(err, syscall.EINVAL) {
		return err
	}
	return nil
}

// Apply reads the state, applies one event with Next and writes the result: one atomic state write per transition.
func (s *Store) Apply(kind EventKind, version string) (State, error) {
	return s.ApplyEvent(Event{Kind: kind, Version: version})
}

// ApplyEvent is Apply for an event with more fields than a version (its At is set from the store's clock).
func (s *Store) ApplyEvent(ev Event) (State, error) {
	cur, err := s.Read()
	if err != nil {
		return State{}, err
	}
	ev.At = s.now()
	next, err := Next(cur, ev)
	if err != nil {
		return cur, err
	}
	if err := s.Write(next); err != nil {
		return cur, err
	}
	return next, nil
}

// AgentFileName is the agent executable's name inside a bundle on this platform.
func AgentFileName() string {
	if runtime.GOOS == "windows" {
		return "rfe.exe"
	}
	return "rfe"
}
