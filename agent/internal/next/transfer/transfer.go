// Package transfer is the resumable chunked upload engine of the rewritten
// agent. A client opens a session (declared size, chunk size, whole-file
// SHA-256, target path), sends the chunks in any order and in parallel, and
// completes the session; the finished file is then published at the target
// path by an atomic rename through the file service.
//
// Guarantees:
//   - a chunk is acknowledged only after its bytes are written to the session's
//     temp file and fsynced, and its chunk row is recorded after that;
//   - every chunk and the whole file are verified against their SHA-256
//     (hex digests are compared without regard to letter case);
//   - a session can be used only by the device that opened it and by admin
//     devices; anyone else gets api.ErrNotFound, the same answer as for an
//     unknown id;
//   - Drain waits for chunk writes and completions that are running, so a
//     shutdown never loses a chunk that was acknowledged;
//   - temp files live at <Dir>/<id><TempSuffix>, the name the old agent used,
//     so sessions imported from the old database resume.
//
// The store is described by the Store interface below, because package store
// does not yet carry the write methods this engine needs.
package transfer

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/ZaidQamhieh/rfe/agent/internal/next/api"
	"github.com/ZaidQamhieh/rfe/agent/internal/next/authz"
	"github.com/ZaidQamhieh/rfe/agent/internal/next/files"
	"github.com/ZaidQamhieh/rfe/agent/internal/next/store"
)

// Names and shapes of the files and ids the engine creates or accepts.
const (
	// TempSuffix ends the name of a session's temp file (<id><TempSuffix>).
	TempSuffix = ".tmp"
	// MaxIDLen is the longest session id accepted.
	MaxIDLen = 128
	// MaxPathLen is the longest target path accepted.
	MaxPathLen = 4096
	// MaxChunksPerUpload bounds the chunk rows one session can create.
	MaxChunksPerUpload = 1 << 20
	// DirMode is the permission of the temp directory.
	DirMode fs.FileMode = 0o700
	// tempFileMode is the permission of a session's temp file.
	tempFileMode fs.FileMode = 0o600
)

// Default limits, the same ceilings as the old agent (openapi POST /transfers).
const (
	MaxTransferSize      int64 = 64 << 30
	MaxChunkSize               = 32 << 20
	MaxActivePerDevice         = 4
	MaxReservedPerDevice int64 = 64 << 30
	MaxActivePerHost           = 16
	MaxReservedPerHost   int64 = 128 << 30
	// StaleAfter is how long an open session may go without a chunk or status
	// request before the sweep removes it.
	StaleAfter = 7 * 24 * time.Hour
	// TerminalRetention is how long completed and failed rows are kept.
	TerminalRetention = 90 * 24 * time.Hour
	// DefaultSweepInterval is the period of the background sweep.
	DefaultSweepInterval = time.Hour
)

// ErrClosed is returned once Drain or Close has begun: the engine accepts no
// new work and the client should retry (it resends chunks it was not told
// about).
var ErrClosed = fmt.Errorf("%w: the agent is shutting down", api.ErrTransferActive)

// Wrap formats for the failures shared by the transfer files; the text is what
// callers and logs see.
const (
	errOpenTempFmt    = "transfer: open temp file: %w"
	errLoadChunksFmt  = "transfer: load chunks: %w"
	errRefreshSessFmt = "transfer: refresh session: %w"
)

// Limits are the admission ceilings and retention times of a Manager.
type Limits struct {
	MaxTransferSize      int64
	MaxChunkSize         int
	MaxActivePerDevice   int
	MaxReservedPerDevice int64
	MaxActivePerHost     int
	MaxReservedPerHost   int64
	StaleAfter           time.Duration
	TerminalRetention    time.Duration
}

// DefaultLimits returns the limits of the old agent.
func DefaultLimits() Limits {
	return Limits{
		MaxTransferSize: MaxTransferSize, MaxChunkSize: MaxChunkSize,
		MaxActivePerDevice: MaxActivePerDevice, MaxReservedPerDevice: MaxReservedPerDevice,
		MaxActivePerHost: MaxActivePerHost, MaxReservedPerHost: MaxReservedPerHost,
		StaleAfter: StaleAfter, TerminalRetention: TerminalRetention,
	}
}

// Store is the persistence the engine needs: upload rows and their received
// chunk numbers. Times are Unix seconds. Of these methods package store has
// none yet (it only has the read-all Uploads and the import); each is listed
// in the report so they can be added there.
type Store interface {
	// OpenUploads returns every row with status open, without Chunks.
	OpenUploads() ([]store.Upload, error)
	// UploadByID returns the row without Chunks, or nil, nil when absent.
	UploadByID(id string) (*store.Upload, error)
	// InsertUpload adds a new row; a duplicate id is an error.
	InsertUpload(u store.Upload) error
	// ChunkNumbers returns the received chunk numbers, ascending.
	ChunkNumbers(id string) ([]int, error)
	// HasChunk reports whether chunk n has a row.
	HasChunk(id string, n int) (bool, error)
	// AddChunk records chunk n; recording one that exists is not an error.
	AddChunk(id string, n int) error
	// DeleteChunks removes the rows of the given chunk numbers.
	DeleteChunks(id string, nums []int) error
	// TouchUpload sets updated_at of an open row.
	TouchUpload(id string, at int64) error
	// FinishUpload sets the status (completed or failed) and sets both
	// updated_at and terminal_at to at.
	FinishUpload(id, status string, at int64) error
	// DeleteUpload removes the row and its chunk rows.
	DeleteUpload(id string) error
	// DeleteTerminalBefore removes completed and failed rows whose terminal_at
	// is before cutoff and returns how many.
	DeleteTerminalBefore(cutoff int64) (int, error)
}

// Publisher is the file service of the device's view of the disk (its jail and
// read-only state are carried by the value the caller passes). *files.Service
// implements it.
type Publisher interface {
	Meta(path string) (*files.Entry, error)
	Checksum(path, algo string) (string, error)
	PublishReader(path string, src io.Reader, overwrite bool) (*files.Entry, error)
}

// Config configures a Manager. Dir and Store are required.
type Config struct {
	// Dir is the temp directory (<data>/transfers); created if missing.
	Dir   string
	Store Store
	// Limits is used as given unless it is the zero value, then DefaultLimits.
	Limits Limits
	// Now is the clock; default time.Now.
	Now func() time.Time
	// SyncFile flushes a written temp file to stable storage; default
	// (*os.File).Sync. Tests replace it to observe the order of operations.
	SyncFile func(*os.File) error
	// Logf receives non-fatal problems (a temp file that could not be
	// removed); default log.Printf.
	Logf func(format string, args ...any)
}

// lease records what in this process is using one session.
type lease struct {
	writers    int  // chunk writes in progress
	completing bool // a Complete holds the session exclusively
}

// Manager coordinates upload sessions. It is safe for concurrent use.
type Manager struct {
	cfg Config

	mu       sync.Mutex // guards leases, closed, sweeper and serialises admission and deletion
	leases   map[string]*lease
	doomed   map[string]bool // sessions of a dropped device that were in use; removed when their last lease ends
	closed   bool
	inflight sync.WaitGroup // chunk writes and completions that have begun

	sweepStop chan struct{}
	sweepDone chan struct{}
}

// New creates the temp directory, reconciles the open sessions in the store
// with the temp files on disk (see recoverOpen) and returns the Manager.
func New(cfg Config) (*Manager, error) {
	if cfg.Dir == "" || cfg.Store == nil {
		return nil, errors.New("transfer: Dir and Store are required")
	}
	if cfg.Limits == (Limits{}) {
		cfg.Limits = DefaultLimits()
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.SyncFile == nil {
		cfg.SyncFile = func(f *os.File) error { return f.Sync() }
	}
	if cfg.Logf == nil {
		cfg.Logf = log.Printf
	}
	if err := os.MkdirAll(cfg.Dir, DirMode); err != nil {
		return nil, fmt.Errorf("transfer: create temp dir: %w", err)
	}
	m := &Manager{cfg: cfg, leases: map[string]*lease{}, doomed: map[string]bool{}}
	if err := m.recoverOpen(); err != nil {
		return nil, err
	}
	return m, nil
}

// now returns the clock's time as Unix seconds.
func (m *Manager) now() int64 { return m.cfg.Now().Unix() }

// validID reports whether id is a plain token (letters, digits, '-', '_') of
// bounded length, so it can name a temp file without escaping the directory.
func validID(id string) bool {
	if id == "" || len(id) > MaxIDLen {
		return false
	}
	for _, r := range id {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
		default:
			return false
		}
	}
	return true
}

// validTempName reports whether name is one plain path element; names read
// from the database are checked before they are joined to the directory.
func validTempName(name string) bool {
	return name != "" && name != "." && name != ".." && filepath.Base(name) == name &&
		!strings.ContainsAny(name, `/\`)
}

// tempPath returns the full path of a row's temp file.
func (m *Manager) tempPath(u *store.Upload) (string, error) {
	if !validTempName(u.TempName) {
		return "", fmt.Errorf("transfer: session %q has an invalid temp file name", u.ID)
	}
	return filepath.Join(m.cfg.Dir, u.TempName), nil
}

// removeTemp deletes a row's temp file; a failure other than "already gone" is
// logged, since the orphan sweep removes what is left.
func (m *Manager) removeTemp(u *store.Upload) {
	path, err := m.tempPath(u)
	if err != nil {
		m.cfg.Logf("transfer: %v", err)
		return
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		m.cfg.Logf("transfer: remove temp file of %s: %v", u.ID, err)
	}
}

// totalChunksFor returns how many chunks a file of size bytes needs; an empty
// file is one empty chunk.
func totalChunksFor(size int64, chunkSize int) int {
	if size <= 0 {
		return 1
	}
	return int((size-1)/int64(chunkSize) + 1)
}

// chunkOffset returns the byte offset of chunk n in the file.
func chunkOffset(chunkSize, n int) int64 {
	return int64(n) * int64(chunkSize)
}

// chunkLen returns the exact length chunk n must have: chunkSize for all but
// the last, and the remainder for the last.
func chunkLen(size int64, chunkSize, n int) int {
	rem := size - chunkOffset(chunkSize, n)
	switch {
	case rem <= 0:
		return 0
	case rem > int64(chunkSize):
		return chunkSize
	}
	return int(rem)
}

// visibleTo reports whether the caller may see and use the session: the
// opening device or an admin. A row without an owner (older than owner
// tracking) is visible to admins only; an empty DeviceID never matches.
func visibleTo(p authz.Principal, u *store.Upload) bool {
	return p.Admin || (p.DeviceID != "" && u.DeviceID == p.DeviceID)
}

// owned loads the session and applies the owner rule; every failure to see it
// is the same api.ErrNotFound. The caller holds m.mu.
func (m *Manager) owned(p authz.Principal, id string) (*store.Upload, error) {
	if !validID(id) {
		return nil, api.ErrNotFound
	}
	u, err := m.cfg.Store.UploadByID(id)
	if err != nil {
		return nil, fmt.Errorf("transfer: load session: %w", err)
	}
	if u == nil || !visibleTo(p, u) {
		return nil, api.ErrNotFound
	}
	return u, nil
}

// leaseKind says what a caller wants to do with a session.
type leaseKind int

const (
	writeLease    leaseKind = iota // one chunk write; many may run together
	completeLease                  // exclusive: no writers, no other completion
)

// begin checks ownership and that the session is open, takes a lease of the
// given kind, refreshes the session's activity time and registers the work with
// Drain. The returned release must be called exactly once.
func (m *Manager) begin(p authz.Principal, id string, kind leaseKind) (*store.Upload, func(), error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return nil, nil, ErrClosed
	}
	u, err := m.owned(p, id)
	if err != nil {
		return nil, nil, err
	}
	if u.Status != store.UploadOpen {
		return nil, nil, fmt.Errorf("%w: %s", api.ErrTransferNotOpen, u.Status)
	}
	l := m.leases[id]
	if l == nil {
		l = &lease{}
	}
	if l.completing || (kind == completeLease && l.writers > 0) {
		return nil, nil, api.ErrTransferActive
	}
	if err := m.cfg.Store.TouchUpload(id, m.now()); err != nil {
		return nil, nil, fmt.Errorf(errRefreshSessFmt, err)
	}
	if kind == completeLease {
		l.completing = true
	} else {
		l.writers++
	}
	m.leases[id] = l
	m.inflight.Add(1)
	var once sync.Once
	release := func() {
		once.Do(func() {
			m.mu.Lock()
			if kind == completeLease {
				l.completing = false
			} else {
				l.writers--
			}
			if l.writers == 0 && !l.completing {
				delete(m.leases, id)
				m.dropDoomedLocked(id)
			}
			m.mu.Unlock()
			m.inflight.Done()
		})
	}
	return u, release, nil
}

// leased reports whether this process is using the session. The caller holds
// m.mu.
func (m *Manager) leased(id string) bool {
	l := m.leases[id]
	return l != nil && (l.writers > 0 || l.completing)
}
