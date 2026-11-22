package transfer

import (
	"errors"
	"fmt"
	"os"
	"sync"
	"syscall"
	"time"
)

// ErrInsufficientStorage means the disk holding the transfer temp files is full. It is retryable: the session
// stays open and every chunk already recorded stays valid.
var ErrInsufficientStorage = errors.New("not enough free disk space")

// syncBatchChunks and syncBatchMaxDelay bound a group of chunk writes that share one data sync: the group is
// synced when it holds this many chunks, when no other chunk of the session is mid-write, or after the delay,
// whichever comes first. The delay is a variable only so tests can shorten it.
const syncBatchChunks = 8

var syncBatchMaxDelay = time.Second

// Test seams for the disk operations an upload depends on; production code never replaces them.
var (
	// reserveSpace allocates size bytes for f and sets its length to size (see sysio_*.go).
	reserveSpace = reserveSpaceFile
	// writeChunkAt writes one chunk's bytes at its offset in the temp file.
	writeChunkAt = func(f *os.File, p []byte, off int64) (int, error) { return f.WriteAt(p, off) }
	// syncData forces f's written bytes to stable storage (see sysio_*.go).
	syncData = syncDataFile
)

// SetSparseReservations switches OpenSession between reserving the declared size on disk (false, the default) and
// only setting the temp file's length (true). It exists for tests in other packages that declare sizes far
// beyond the test disk to exercise quota accounting; it is not meant to be called by the agent itself.
func SetSparseReservations(on bool) {
	if on {
		reserveSpace = func(f *os.File, size int64) error { return f.Truncate(size) }
		return
	}
	reserveSpace = reserveSpaceFile
}

// isDiskFull reports whether err, or an error it wraps, means the disk or the user's quota is full.
func isDiskFull(err error) bool {
	return errors.Is(err, syscall.ENOSPC) || errors.Is(err, syscall.EDQUOT)
}

// storageErr wraps err with op; a full disk additionally matches ErrInsufficientStorage.
func storageErr(op string, err error) error {
	if isDiskFull(err) {
		return fmt.Errorf("%s: %w (%w)", op, ErrInsufficientStorage, err)
	}
	return fmt.Errorf("%s: %w", op, err)
}

// sessionSync groups the chunk writes of one session so a single data sync covers several chunks.
// All fields except flushMu are guarded by Manager.syncMu.
type sessionSync struct {
	id, path string
	writers  int        // WriteChunk calls that are writing bytes and have not yet joined a batch
	cur      *syncBatch // chunks written and waiting for their sync, nil when none
	flushMu  sync.Mutex // one data sync at a time per session
}

// syncBatch is one group of written chunks. Its waiters block on done; err is final once done is closed.
type syncBatch struct {
	chunks []int
	timer  *time.Timer
	done   chan struct{}
	err    error
}

// beginChunk registers a chunk write that is about to start on the session's temp file.
func (m *Manager) beginChunk(id, path string) *sessionSync {
	m.syncMu.Lock()
	defer m.syncMu.Unlock()
	if m.syncs == nil {
		m.syncs = make(map[string]*sessionSync)
	}
	ss := m.syncs[id]
	if ss == nil {
		ss = &sessionSync{id: id, path: path}
		m.syncs[id] = ss
	}
	ss.writers++
	return ss
}

// releaseLocked forgets ss once nothing is writing or waiting on it. Caller holds syncMu.
func (m *Manager) releaseLocked(ss *sessionSync) {
	if ss.writers == 0 && ss.cur == nil && m.syncs[ss.id] == ss {
		delete(m.syncs, ss.id)
	}
}

// abandonChunk is beginChunk's undo for a write that failed before reaching commitChunk. If it was the last
// writer, the chunks already waiting for a sync must not wait for it any longer, so it flushes them.
func (m *Manager) abandonChunk(ss *sessionSync) {
	m.syncMu.Lock()
	ss.writers--
	var b *syncBatch
	if ss.writers == 0 && ss.cur != nil {
		b, ss.cur = ss.cur, nil
		if b.timer != nil {
			b.timer.Stop()
		}
	}
	m.releaseLocked(ss)
	m.syncMu.Unlock()
	if b != nil {
		m.flushBatch(ss, b)
	}
}

// commitChunk adds the just-written chunk n to the session's open batch and returns only after that batch is
// synced and every chunk in it is recorded as received (or the sync failed, which is the returned error and
// leaves the chunks unrecorded). The goroutine that completes a batch performs the sync for all its members.
func (m *Manager) commitChunk(ss *sessionSync, n int) error {
	m.syncMu.Lock()
	ss.writers--
	b := ss.cur
	if b == nil {
		b = &syncBatch{done: make(chan struct{})}
		ss.cur = b
	}
	b.chunks = append(b.chunks, n)
	flushNow := len(b.chunks) >= syncBatchChunks || ss.writers == 0
	if flushNow {
		ss.cur = nil
		if b.timer != nil {
			b.timer.Stop()
		}
	} else if b.timer == nil {
		b.timer = time.AfterFunc(syncBatchMaxDelay, func() { m.flushDue(ss, b) })
	}
	m.releaseLocked(ss)
	m.syncMu.Unlock()
	if flushNow {
		m.flushBatch(ss, b)
	}
	<-b.done
	return b.err
}

// flushDue is the timer path: it flushes b unless a writer already took it.
func (m *Manager) flushDue(ss *sessionSync, b *syncBatch) {
	m.syncMu.Lock()
	if ss.cur != b {
		m.syncMu.Unlock()
		return
	}
	ss.cur = nil
	m.releaseLocked(ss)
	m.syncMu.Unlock()
	m.flushBatch(ss, b)
}

// flushBatch syncs the session's temp file once, records every chunk of b as received, and wakes b's waiters.
// A chunk is recorded only after its bytes are on stable storage, so a power cut can never leave the bitmap
// claiming bytes the disk lost.
func (m *Manager) flushBatch(ss *sessionSync, b *syncBatch) {
	ss.flushMu.Lock()
	b.err = m.syncAndMark(ss, b.chunks)
	ss.flushMu.Unlock()
	close(b.done)
}

// syncAndMark runs the data sync, then records chunks in the database.
func (m *Manager) syncAndMark(ss *sessionSync, chunks []int) error {
	f, err := os.OpenFile(ss.path, os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("open temp file for sync: %w", err)
	}
	err = syncData(f)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		countENOSPC(&m.stats.enospcWriteChunk, ss.id, err)
		return storageErr("sync temp file", err)
	}
	for _, n := range chunks {
		if err := m.db.MarkChunkReceived(ss.id, n); err != nil {
			return err
		}
	}
	return nil
}
