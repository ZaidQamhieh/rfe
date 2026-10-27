// Package transfer handles resumable file uploads.
//
// Flow:
//  1. POST /v1/transfers           → OpenSession → returns UploadSession
//  2. PUT  /v1/transfers/{id}/chunks/{n} → WriteChunk (idempotent, hash-verified)
//  3. GET  /v1/transfers/{id}       → Status (for resume)
//  4. POST /v1/transfers/{id}/complete → Complete (verify whole-file SHA-256, atomic rename)
package transfer

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/ZaidQamhieh/rfe/agent/internal/store"
)

// ErrNotFound is returned when the session ID is unknown.
var ErrNotFound = errors.New("transfer not found")

// ErrChunkMismatch is returned when the chunk SHA-256 doesn't match.
var ErrChunkMismatch = errors.New("chunk sha256 mismatch")

// ErrFileMismatch is returned when the whole-file SHA-256 doesn't match.
var ErrFileMismatch = errors.New("whole-file sha256 mismatch")

// ErrDestinationExists is returned by OpenSession when overwrite=false and
// the target path already exists.
var ErrDestinationExists = errors.New("destination already exists")

// ErrChunkOutOfRange is returned when a chunk index falls outside the
// session's declared chunk count.
var ErrChunkOutOfRange = errors.New("chunk index out of range")

// ErrChunkWrongSize is returned when a chunk's length is not exactly the
// length the session's geometry requires.
var ErrChunkWrongSize = errors.New("chunk has the wrong length")

// ErrQuotaExceeded means the host or requesting device has reached its
// active-session or reserved-byte limit.
var ErrQuotaExceeded = errors.New("transfer capacity exceeded")

// ErrSessionActive means an explicit delete was attempted while an operation
// is using that session.
var ErrSessionActive = errors.New("transfer is active")

// ErrIncomplete is returned by Complete while chunks are still missing. The
// session stays open so the client can send the rest and complete again.
var ErrIncomplete = errors.New("upload is missing chunks")

// ErrNotOpen means the session exists but no longer accepts chunks or a
// completion request.
var ErrNotOpen = errors.New("transfer is not open")

// ErrTooLarge is returned when a session declares more bytes than
// MaxTransferSize.
var ErrTooLarge = errors.New("declared size exceeds the maximum")

// Reservations count every open session's declared bytes, even though sparse
// temp files may not allocate all of those bytes immediately. The declared
// file-size and aggregate limits are explicit product ceilings that bound
// concurrent disk reservations.
const (
	MaxTransferSize             int64 = 64 << 30 // 64 GiB per upload
	MaxActiveTransfersPerDevice       = 4
	MaxReservedBytesPerDevice   int64 = 64 << 30 // 64 GiB
	MaxActiveTransfersPerHost         = 16
	MaxReservedBytesPerHost     int64 = 128 << 30 // 128 GiB

	// Open sessions expire after a week without a chunk or status/resume request.
	TransferStaleAfter = 7 * 24 * time.Hour
	staleSweepInterval = time.Hour
)

// Kept package-private for the existing transfer-package boundary tests.
const maxTransferSize = MaxTransferSize

// Manager coordinates in-progress transfer sessions.
type Manager struct {
	db      *store.DB
	tempDir string // directory for temp files

	mu         sync.Mutex
	active     map[string]int  // in-process operations that own a session lease
	completing map[string]bool // exclusive completion lease per session
}

// New creates a Manager that stores temp files under tempDir.
func New(db *store.DB, tempDir string) (*Manager, error) {
	if err := os.MkdirAll(tempDir, 0o700); err != nil {
		return nil, fmt.Errorf("create temp dir: %w", err)
	}
	return &Manager{
		db:         db,
		tempDir:    tempDir,
		active:     make(map[string]int),
		completing: make(map[string]bool),
	}, nil
}

// OpenSession creates a new upload session. deviceID is the requesting
// device's ID (empty if unknown, e.g. no device context) — recorded on the
// transfer so the web companion's Transfers page can filter by device.
//
// The overwrite=false check here is a courtesy: it fails the client early
// instead of after a long upload. It is not the guarantee — Complete re-checks
// atomically at publish time, because anything created during the upload would
// slip past this Stat (PR-50).
func (m *Manager) OpenSession(id, targetPath string, size int64, chunkSize int, sha256hex string, overwrite bool, deviceID string) (*store.Transfer, error) {
	return m.openSession(id, targetPath, size, chunkSize, sha256hex, overwrite, deviceID, nil)
}

// OpenSessionWithStat creates a session while checking an overwrite=false
// destination through the caller's effective filesystem view. Server callers
// should pass Ops.Stat so this early conflict check cannot escape a device
// jail; Complete still makes the authoritative atomic no-replace decision.
func (m *Manager) OpenSessionWithStat(id, targetPath string, size int64, chunkSize int, sha256hex string, overwrite bool, deviceID string, stat func(string) (os.FileInfo, error)) (*store.Transfer, error) {
	return m.openSession(id, targetPath, size, chunkSize, sha256hex, overwrite, deviceID, stat)
}

func (m *Manager) openSession(id, targetPath string, size int64, chunkSize int, sha256hex string, overwrite bool, deviceID string, stat func(string) (os.FileInfo, error)) (*store.Transfer, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if id == "" || filepath.Base(id) != id || id == "." || id == ".." {
		return nil, errors.New("invalid transfer ID")
	}
	if size < 0 {
		return nil, fmt.Errorf("%w: negative size", ErrTooLarge)
	}
	if size > MaxTransferSize {
		return nil, fmt.Errorf("%w: %d bytes declared, limit %d", ErrTooLarge, size, MaxTransferSize)
	}
	if chunkSize <= 0 {
		return nil, errors.New("chunk size must be positive")
	}
	if !overwrite {
		statPath := stat
		if statPath == nil {
			statPath = func(path string) (os.FileInfo, error) { return os.Stat(path) }
		}
		if _, err := statPath(targetPath); err == nil {
			return nil, ErrDestinationExists
		} else if stat != nil && !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
	}
	if _, err := m.cleanupStaleLocked(time.Now()); err != nil {
		return nil, fmt.Errorf("clean stale transfers before admission: %w", err)
	}

	// Ceiling division without size+chunkSize, which overflows for a huge
	// chunkSize; a zero-byte upload is one empty chunk.
	totalChunks := 1
	if size > 0 {
		totalChunks = int((size-1)/int64(chunkSize) + 1)
	}
	tempPath := filepath.Join(m.tempDir, id+".tmp")

	t := &store.Transfer{
		ID:          id,
		TargetPath:  targetPath,
		TotalSize:   size,
		ChunkSize:   chunkSize,
		SHA256:      sha256hex,
		TotalChunks: totalChunks,
		TempPath:    tempPath,
		Status:      "open",
		DeviceID:    deviceID,
		Overwrite:   overwrite,
	}
	created, err := m.db.CreateTransferWithinLimits(t, store.TransferLimits{
		MaxHostSessions:   MaxActiveTransfersPerHost,
		MaxHostBytes:      MaxReservedBytesPerHost,
		MaxDeviceSessions: MaxActiveTransfersPerDevice,
		MaxDeviceBytes:    MaxReservedBytesPerDevice,
	})
	if err != nil {
		return nil, err
	}
	if !created {
		return nil, ErrQuotaExceeded
	}

	// Persist the quota reservation first. If file allocation fails, remove the
	// row so its open-session reservation is released immediately.
	f, err := os.OpenFile(tempPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o666)
	if err == nil && size > 0 {
		err = f.Truncate(size)
	}
	if err == nil {
		err = f.Close()
	} else if f != nil {
		_ = f.Close()
	}
	if err != nil {
		if removeErr := os.Remove(tempPath); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
			log.Printf("failed to remove incomplete transfer temp file %s: %v", id, removeErr)
		}
		if deleteErr := m.db.DeleteTransfer(id); deleteErr != nil {
			log.Printf("failed to release transfer reservation %s: %v", id, deleteErr)
		}
		return nil, fmt.Errorf("create transfer temp file: %w", err)
	}
	return t, nil
}

// BeginActivity obtains an in-process lease for an open transfer. HTTP chunk
// handlers hold this from before reading the request body through persistence,
// so the stale sweeper cannot remove a temp file while a slow upload is active.
// Call the returned release function exactly once (it is safe to call more).
func (m *Manager) BeginActivity(id string) (func(), error) {
	return m.beginActivity(id, true)
}

func (m *Manager) beginActivity(id string, requireOpen bool) (func(), error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	t, err := m.db.GetTransfer(id)
	if err != nil {
		return nil, err
	}
	if t == nil {
		return nil, ErrNotFound
	}
	if requireOpen && t.Status != "open" {
		return nil, fmt.Errorf("%w: %s", ErrNotOpen, t.Status)
	}
	if t.Status != "open" {
		return func() {}, nil
	}
	if m.completing[id] {
		return nil, ErrSessionActive
	}
	if err := m.db.TouchOpenTransfer(id, time.Now().Unix()); err != nil {
		return nil, err
	}
	m.active[id]++
	var once sync.Once
	return func() {
		once.Do(func() {
			m.mu.Lock()
			defer m.mu.Unlock()
			if m.active[id] <= 1 {
				delete(m.active, id)
			} else {
				m.active[id]--
			}
		})
	}, nil
}

// beginCompletion obtains an exclusive lease. Chunk writes may run in
// parallel with one another, but completion must wait until they finish and
// prevents new chunk/status activity until publish is done.
func (m *Manager) beginCompletion(id string) (func(), error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	t, err := m.db.GetTransfer(id)
	if err != nil {
		return nil, err
	}
	if t == nil {
		return nil, ErrNotFound
	}
	if t.Status != "open" {
		return nil, fmt.Errorf("%w: %s", ErrNotOpen, t.Status)
	}
	if m.active[id] > 0 || m.completing[id] {
		return nil, ErrSessionActive
	}
	if err := m.db.TouchOpenTransfer(id, time.Now().Unix()); err != nil {
		return nil, err
	}
	m.active[id] = 1
	m.completing[id] = true
	var once sync.Once
	return func() {
		once.Do(func() {
			m.mu.Lock()
			defer m.mu.Unlock()
			delete(m.active, id)
			delete(m.completing, id)
		})
	}, nil
}

// Lookup returns session metadata without refreshing its activity timestamp.
// HTTP handlers use it to check ownership before Status or BeginActivity can
// extend the session's retention window.
func (m *Manager) Lookup(id string) (*store.Transfer, error) {
	t, err := m.db.GetTransfer(id)
	if err != nil {
		return nil, err
	}
	if t == nil {
		return nil, ErrNotFound
	}
	return t, nil
}

// DeleteSession safely removes an upload session and its temporary file. An
// active upload or completion owns a lease and cannot be removed; once this
// method holds the manager lock, no new lease can begin before deletion ends.
func (m *Manager) DeleteSession(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.active[id] > 0 {
		return ErrSessionActive
	}
	t, err := m.db.GetTransfer(id)
	if err != nil {
		return err
	}
	if t == nil {
		return ErrNotFound
	}
	if err := m.db.DeleteTransfer(id); err != nil {
		return err
	}
	if tempPath, ok := m.sessionTempPath(t); ok {
		if err := os.Remove(tempPath); err != nil && !errors.Is(err, os.ErrNotExist) {
			// The row is gone and no operation can still own this file. Leave
			// failed removals for the age-gated orphan sweep below.
			log.Printf("transfer %s deleted but temp cleanup failed: %v", id, err)
		}
	}
	return nil
}

// CleanupStaleSessions removes expired open-session rows and terminal history
// older than the retention window, then sweeps orphan temp files. The manager
// lock coordinates with OpenSession, activity leases, and explicit deletion.
func (m *Manager) CleanupStaleSessions(now time.Time) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.cleanupStaleLocked(now)
}

func (m *Manager) cleanupStaleLocked(now time.Time) (int, error) {
	cutoff := now.Add(-TransferStaleAfter)
	transfers, err := m.db.ListOpenTransfers()
	if err != nil {
		return 0, err
	}
	removed := 0
	var cleanupErr error
	for i := range transfers {
		t := &transfers[i]
		if m.active[t.ID] > 0 || !m.isStale(t, cutoff) {
			continue
		}
		deleted, err := m.db.DeleteStaleOpenTransfer(t)
		if err != nil {
			if cleanupErr == nil {
				cleanupErr = err
			}
			continue
		}
		if !deleted {
			continue
		}
		removed++
		if tempPath, ok := m.sessionTempPath(t); ok {
			if err := os.Remove(tempPath); err != nil && !errors.Is(err, os.ErrNotExist) {
				if cleanupErr == nil {
					cleanupErr = fmt.Errorf("remove stale transfer temp file: %w", err)
				}
			}
		}
	}
	pruned, err := m.db.PruneTerminalTransfersBefore(now.Add(-store.TransferHistoryRetention).Unix())
	if err != nil && cleanupErr == nil {
		cleanupErr = fmt.Errorf("prune terminal transfer history: %w", err)
	}
	removed += pruned
	orphansRemoved, err := m.cleanupOrphanTempsLocked(cutoff)
	removed += orphansRemoved
	if cleanupErr == nil {
		cleanupErr = err
	}
	return removed, cleanupErr
}

// sessionTempPath accepts only the manager-owned direct-child name generated
// for a session. Persisted paths are never trusted as arbitrary deletion paths.
func (m *Manager) sessionTempPath(t *store.Transfer) (string, bool) {
	if t == nil || t.ID == "" || filepath.Base(t.ID) != t.ID || t.ID == "." || t.ID == ".." {
		return "", false
	}
	expected := filepath.Join(m.tempDir, t.ID+".tmp")
	if filepath.Clean(t.TempPath) != filepath.Clean(expected) {
		return "", false
	}
	return expected, true
}

// cleanupOrphanTempsLocked removes only old regular .tmp files directly in
// this manager's dedicated directory that no transfer row references and no
// active lease owns. Holding m.mu prevents new sessions/leases during the
// check-and-remove sequence; symlinks and other file types are left alone.
func (m *Manager) cleanupOrphanTempsLocked(cutoff time.Time) (int, error) {
	referenced, err := m.db.ListTransferTempPaths()
	if err != nil {
		return 0, err
	}
	references := make(map[string]struct{}, len(referenced))
	for _, p := range referenced {
		references[filepath.Clean(p)] = struct{}{}
	}
	entries, err := os.ReadDir(m.tempDir)
	if err != nil {
		return 0, fmt.Errorf("read transfer temp directory: %w", err)
	}
	removed := 0
	var cleanupErr error
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasSuffix(name, ".tmp") {
			continue
		}
		id := strings.TrimSuffix(name, ".tmp")
		if m.active[id] > 0 {
			continue
		}
		path := filepath.Join(m.tempDir, name)
		if _, ok := references[filepath.Clean(path)]; ok {
			continue
		}
		info, err := os.Lstat(path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			if cleanupErr == nil {
				cleanupErr = fmt.Errorf("inspect orphan transfer temp file: %w", err)
			}
			continue
		}
		if !info.Mode().IsRegular() || !info.ModTime().Before(cutoff) {
			continue
		}
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			if cleanupErr == nil {
				cleanupErr = fmt.Errorf("remove orphan transfer temp file: %w", err)
			}
			continue
		}
		removed++
	}
	return removed, cleanupErr
}

func (m *Manager) isStale(t *store.Transfer, cutoff time.Time) bool {
	lastActivity := time.Unix(t.UpdatedAt, 0)
	if t.UpdatedAt <= 0 {
		tempPath, ok := m.sessionTempPath(t)
		if !ok {
			return false
		}
		info, err := os.Stat(tempPath)
		if errors.Is(err, os.ErrNotExist) {
			return true
		}
		if err != nil {
			// If the file cannot be inspected, leave the session in place.
			return false
		}
		lastActivity = info.ModTime()
	}
	return lastActivity.Before(cutoff)
}

// StartStaleCleanup performs an initial cleanup and then sweeps at interval.
// The returned stop function shuts down the process-lifetime worker and waits
// for it to exit.
func (m *Manager) StartStaleCleanup(interval time.Duration) func() {
	if interval <= 0 {
		interval = staleSweepInterval
	}
	if _, err := m.CleanupStaleSessions(time.Now()); err != nil {
		log.Printf("transfer stale-session cleanup failed: %v", err)
	}
	stop := make(chan struct{})
	done := make(chan struct{})
	var once sync.Once
	go func() {
		defer close(done)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				if _, err := m.CleanupStaleSessions(time.Now()); err != nil {
					log.Printf("transfer stale-session cleanup failed: %v", err)
				}
			case <-stop:
				return
			}
		}
	}()
	return func() {
		once.Do(func() { close(stop) })
		<-done
	}
}

// Status returns the current state of a transfer (for resume). This is the
// one path that genuinely needs every received chunk number — the client diffs
// the set to decide what to re-send — so it loads them explicitly; GetTransfer
// no longer carries them (PR-42).
func (m *Manager) Status(id string) (*store.Transfer, error) {
	release, err := m.beginActivity(id, false)
	if err != nil {
		return nil, err
	}
	defer release()

	t, err := m.db.GetTransfer(id)
	if err != nil {
		return nil, err
	}
	if t == nil {
		return nil, ErrNotFound
	}
	chunks, err := m.db.ChunkNumbers(id)
	if err != nil {
		return nil, err
	}
	t.ReceivedChunks = chunks
	return t, nil
}

// WriteChunk writes chunk n to the temp file after verifying its SHA-256.
// The operation is idempotent: writing the same chunk twice with matching
// hash is a no-op.
func (m *Manager) WriteChunk(id string, n int, chunkData []byte, chunkSHA256 string) error {
	release, err := m.BeginActivity(id)
	if err != nil {
		return err
	}
	defer release()

	t, err := m.db.GetTransfer(id)
	if err != nil {
		return err
	}
	if t == nil {
		return ErrNotFound
	}
	if t.Status != "open" {
		return fmt.Errorf("%w: %s", ErrNotOpen, t.Status)
	}

	// The chunk index drives a WriteAt offset (n * chunkSize). Unchecked, a
	// large n seeks far past the declared size and leaves a sparse file of the
	// client's choosing; a negative one is a negative offset (PR-12).
	if n < 0 || n >= t.TotalChunks {
		return fmt.Errorf("%w: chunk %d of %d", ErrChunkOutOfRange, n, t.TotalChunks)
	}
	// Every chunk but the last must be exactly chunkSize, and the last exactly
	// the remainder. The body cap alone only bounds the maximum, so a short
	// chunk would silently leave a hole of zeros inside the file.
	if want := t.ExpectedChunkLen(n); len(chunkData) != want {
		return fmt.Errorf("%w: chunk %d is %d bytes, want %d", ErrChunkWrongSize, n, len(chunkData), want)
	}

	// Verify chunk hash.
	sum := sha256.Sum256(chunkData)
	got := hex.EncodeToString(sum[:])
	if got != chunkSHA256 {
		return fmt.Errorf("%w: got %s want %s", ErrChunkMismatch, got, chunkSHA256)
	}

	// Check idempotency: if already received skip writing but return success.
	// An indexed lookup, not a scan of every chunk received so far — the
	// latter made a large transfer quadratic all on its own (PR-42).
	switch has, err := m.db.HasChunk(id, n); {
	case err != nil:
		return err
	case has:
		return nil
	}

	offset := int64(n) * int64(t.ChunkSize)
	f, err := os.OpenFile(t.TempPath, os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("open temp file: %w", err)
	}
	defer f.Close()
	if _, err := f.WriteAt(chunkData, offset); err != nil {
		return fmt.Errorf("write chunk: %w", err)
	}

	return m.db.MarkChunkReceived(id, n)
}

// Complete verifies the whole-file SHA-256 and atomically renames the temp
// file to the final destination.
func (m *Manager) Complete(id string) (os.FileInfo, string, error) {
	return m.complete(id, nil)
}

// CompleteWithPublisher verifies the central transfer file and then invokes
// publish with that same open, rewound file handle. Server callers should
// publish through the current request's Ops so jail authorization and the
// final atomic placement use one rooted filesystem boundary.
func (m *Manager) CompleteWithPublisher(id string, publish func(string, io.Reader, bool) (os.FileInfo, error)) (os.FileInfo, string, error) {
	return m.complete(id, publish)
}

func (m *Manager) complete(id string, publisher func(string, io.Reader, bool) (os.FileInfo, error)) (os.FileInfo, string, error) {
	release, err := m.beginCompletion(id)
	if err != nil {
		return nil, "", err
	}
	defer release()

	t, err := m.db.GetTransfer(id)
	if err != nil {
		return nil, "", err
	}
	if t == nil {
		return nil, "", ErrNotFound
	}
	if t.Status != "open" {
		return nil, "", fmt.Errorf("%w: %s", ErrNotOpen, t.Status)
	}

	// Every chunk must be on disk before the whole-file hash means anything:
	// a premature complete is a recoverable client mistake, not a corrupt file,
	// so it must not end the session.
	received, err := m.db.ChunkNumbers(id)
	if err != nil {
		return nil, "", err
	}
	if len(received) < t.TotalChunks {
		return nil, "", fmt.Errorf("%w: %d of %d chunks received", ErrIncomplete, len(received), t.TotalChunks)
	}

	// Verify whole-file hash.
	f, err := os.Open(t.TempPath)
	if err != nil {
		return nil, "", fmt.Errorf("open temp: %w", err)
	}
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		_ = f.Close()
		return nil, "", fmt.Errorf("hash file: %w", err)
	}

	got := hex.EncodeToString(h.Sum(nil))
	if got != t.SHA256 {
		_ = f.Close()
		if err := m.db.SetTransferStatus(id, "failed"); err != nil {
			log.Printf("transfer %s failed hash verification but status update failed: %v", id, err)
			if deleteErr := m.db.DeleteTransfer(id); deleteErr != nil {
				log.Printf("failed to release transfer reservation %s after hash failure: %v", id, deleteErr)
			}
		}
		// The temp file is now orphaned (the transfer can't be resumed once
		// failed) — remove it so it doesn't leak on disk.
		if err := os.Remove(t.TempPath); err != nil && !errors.Is(err, os.ErrNotExist) {
			log.Printf("failed to remove mismatched transfer temp file %s: %v", id, err)
		}
		return nil, "", fmt.Errorf("%w: got %s want %s", ErrFileMismatch, got, t.SHA256)
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		_ = f.Close()
		return nil, "", fmt.Errorf("rewind temp: %w", err)
	}

	var info os.FileInfo
	if publisher != nil {
		info, err = publisher(t.TargetPath, f, t.Overwrite)
		_ = f.Close()
		if err != nil {
			if errors.Is(err, ErrDestinationExists) {
				return nil, "", err
			}
			return nil, "", fmt.Errorf("publish: %w", err)
		}
		if err := os.Remove(t.TempPath); err != nil && !errors.Is(err, os.ErrNotExist) {
			log.Printf("transfer %s published but central temp cleanup failed: %v", id, err)
		}
	} else {
		_ = f.Close()
		// Ensure parent directory exists.
		if err := os.MkdirAll(filepath.Dir(t.TargetPath), 0o755); err != nil {
			return nil, "", err
		}

		// Preserve the path-based API for internal non-server callers. Server
		// requests use CompleteWithPublisher and a rooted Ops publisher.
		if err := publish(t.TempPath, t.TargetPath, t.Overwrite); err != nil {
			if errors.Is(err, ErrDestinationExists) {
				return nil, "", err
			}
			return nil, "", fmt.Errorf("rename: %w", err)
		}
	}

	// The bytes are on disk under the final name — the transfer succeeded even
	// if recording that fails, so surface the persistence error instead of
	// dropping it and leaving the row stuck "open" (PR-50).
	if err := m.db.SetTransferStatus(id, "completed"); err != nil {
		if deleteErr := m.db.DeleteTransfer(id); deleteErr != nil {
			log.Printf("failed to release transfer reservation %s after publish: %v", id, deleteErr)
		}
		return nil, t.TargetPath, fmt.Errorf("transfer published to %s but recording it failed: %w", t.TargetPath, err)
	}

	if publisher == nil {
		info, err = os.Stat(t.TargetPath)
		if err != nil {
			return nil, t.TargetPath, nil
		}
	}
	return info, t.TargetPath, nil
}

// publish moves the finished temp file onto its final path. With overwrite it
// replaces whatever is there (moveFile); without it, the create must fail
// rather than replace a file that appeared during the upload — OpenSession's
// Stat is far too early to rely on (PR-50).
//
// The no-replace path uses os.Link, which fails with EEXIST if dst exists and
// so decides atomically, unlike a Stat-then-rename. Link needs both paths on
// one filesystem and a backing FS that supports it; any other failure falls
// back to an O_EXCL copy, which is equally atomic about not clobbering.
func publish(src, dst string, overwrite bool) error {
	if overwrite {
		return moveFile(src, dst)
	}
	switch err := os.Link(src, dst); {
	case err == nil:
		return os.Remove(src) // dst is now a second name for the same inode
	case errors.Is(err, fs.ErrExist):
		return ErrDestinationExists
	}
	return copyAcrossNoReplace(src, dst)
}

// copyAcrossNoReplace copies src onto dst, creating dst with O_EXCL so an
// existing (or concurrently created) file is never replaced. Unlike
// copyAcross it writes dst directly: a temp+rename would reintroduce the
// replace that O_EXCL exists to prevent.
func copyAcrossNoReplace(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.OpenFile(dst, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		if errors.Is(err, fs.ErrExist) {
			return ErrDestinationExists
		}
		return err
	}
	cleanup := func() { out.Close(); os.Remove(dst) }

	if _, err := io.Copy(out, in); err != nil {
		cleanup()
		return err
	}
	if err := out.Sync(); err != nil {
		cleanup()
		return err
	}
	if err := out.Close(); err != nil {
		os.Remove(dst)
		return err
	}
	return os.Remove(src)
}

// moveFile moves src to dst atomically when possible. It tries os.Rename first
// (atomic, same-filesystem); on a cross-device error (EXDEV — dst is on a
// different mount than src) it copies src into dst's directory and atomically
// renames within that directory, then removes src.
func moveFile(src, dst string) error {
	if err := os.Rename(src, dst); err == nil {
		return nil
	} else if !errors.Is(err, syscall.EXDEV) {
		return err
	}
	return copyAcross(src, dst)
}

// copyAcross copies src to a temp file in dst's own directory, fsyncs it, then
// atomically renames it onto dst (both now on the same filesystem) and removes
// src. A failure at any step leaves dst untouched and cleans up the temp.
func copyAcross(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	tmp, err := os.CreateTemp(filepath.Dir(dst), ".rfe-move-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	cleanup := func() { tmp.Close(); os.Remove(tmpName) }

	if _, err := io.Copy(tmp, in); err != nil {
		cleanup()
		return err
	}
	if err := tmp.Sync(); err != nil {
		cleanup()
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return err
	}
	if err := os.Rename(tmpName, dst); err != nil {
		os.Remove(tmpName)
		return err
	}
	return os.Remove(src)
}
