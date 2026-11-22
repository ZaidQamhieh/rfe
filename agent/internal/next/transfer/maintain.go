package transfer

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ZaidQamhieh/rfe/agent/internal/next/store"
)

// recoverOpen reconciles the open sessions in the store with the temp files on
// disk. It runs once from New, so it covers both a restart after a crash and
// sessions imported from the old database:
//   - a row whose id, temp name or geometry is invalid is marked failed;
//   - a row with updated_at 0 (old data) gets the current time, so the first
//     sweep does not expire it;
//   - chunk rows that are out of range or whose bytes lie beyond the temp
//     file's real size are discarded (the client resends them), and a missing
//     temp file is recreated at full size with no chunk rows;
//   - the temp file is brought back to the declared size.
func (m *Manager) recoverOpen() error {
	open, err := m.cfg.Store.OpenUploads()
	if err != nil {
		return fmt.Errorf("transfer: list open sessions: %w", err)
	}
	for i := range open {
		if err := m.recoverOne(&open[i]); err != nil {
			return err
		}
	}
	return nil
}

// recoverOne applies the rules of recoverOpen to one session.
func (m *Manager) recoverOne(u *store.Upload) error {
	if !validID(u.ID) || !validTempName(u.TempName) || u.ChunkSize <= 0 || u.TotalSize < 0 ||
		u.TotalChunks != totalChunksFor(u.TotalSize, u.ChunkSize) {
		m.cfg.Logf("transfer: session %q has invalid fields; marking it failed", u.ID)
		if err := m.cfg.Store.FinishUpload(u.ID, store.UploadFailed, m.now()); err != nil {
			return fmt.Errorf("transfer: fail invalid session: %w", err)
		}
		if validTempName(u.TempName) {
			m.removeTemp(u)
		}
		return nil
	}
	if u.UpdatedAt <= 0 {
		if err := m.cfg.Store.TouchUpload(u.ID, m.now()); err != nil {
			return fmt.Errorf("transfer: refresh imported session: %w", err)
		}
	}
	path, err := m.tempPath(u)
	if err != nil {
		return err
	}
	chunks, err := m.cfg.Store.ChunkNumbers(u.ID)
	if err != nil {
		return fmt.Errorf(errLoadChunksFmt, err)
	}
	info, err := os.Lstat(path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		if err := m.cfg.Store.DeleteChunks(u.ID, chunks); err != nil {
			return fmt.Errorf("transfer: discard chunks: %w", err)
		}
		return m.createTemp(u)
	case err != nil:
		return fmt.Errorf("transfer: inspect temp file: %w", err)
	case !info.Mode().IsRegular():
		return fmt.Errorf("transfer: temp file of %s is not a regular file", u.ID)
	}
	var beyond []int
	for _, n := range chunks {
		if n < 0 || n >= u.TotalChunks || chunkOffset(u.ChunkSize, n)+int64(chunkLen(u.TotalSize, u.ChunkSize, n)) > info.Size() {
			beyond = append(beyond, n)
		}
	}
	if len(beyond) > 0 {
		if err := m.cfg.Store.DeleteChunks(u.ID, beyond); err != nil {
			return fmt.Errorf("transfer: discard chunks: %w", err)
		}
	}
	if info.Size() != u.TotalSize {
		f, err := os.OpenFile(path, os.O_WRONLY, tempFileMode)
		if err != nil {
			return fmt.Errorf(errOpenTempFmt, err)
		}
		return sizeAndSync(m, f, u.TotalSize)
	}
	return nil
}

// Sweep removes open sessions with no activity for Limits.StaleAfter (and
// their temp files), completed and failed rows older than
// Limits.TerminalRetention, and orphan temp files (regular files ending in
// TempSuffix that no open row names and that are older than StaleAfter). A
// session with a chunk write or completion in progress is never removed. It
// returns how many rows and files it removed and the first problems met.
func (m *Manager) Sweep(now time.Time) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.sweepLocked(now)
}

// sweepLocked is Sweep with m.mu held.
func (m *Manager) sweepLocked(now time.Time) (int, error) {
	lim := m.cfg.Limits
	cutoff := now.Add(-lim.StaleAfter)
	open, err := m.cfg.Store.OpenUploads()
	if err != nil {
		return 0, fmt.Errorf("transfer: list open sessions: %w", err)
	}
	var errs []error
	removed := 0
	referenced := map[string]bool{}
	for i := range open {
		u := &open[i]
		referenced[u.TempName] = true
		if m.leased(u.ID) {
			continue
		}
		if u.UpdatedAt <= 0 {
			// Old data without a time: start its clock now instead of expiring it.
			if err := m.cfg.Store.TouchUpload(u.ID, now.Unix()); err != nil {
				errs = append(errs, err)
			}
			continue
		}
		if !time.Unix(u.UpdatedAt, 0).Before(cutoff) {
			continue
		}
		if err := m.cfg.Store.DeleteUpload(u.ID); err != nil {
			errs = append(errs, err)
			continue
		}
		removed++
		m.removeTemp(u)
	}
	pruned, err := m.cfg.Store.DeleteTerminalBefore(now.Add(-lim.TerminalRetention).Unix())
	if err != nil {
		errs = append(errs, fmt.Errorf("transfer: prune history: %w", err))
	}
	removed += pruned
	orphans, err := m.sweepOrphans(cutoff, referenced)
	removed += orphans
	if err != nil {
		errs = append(errs, err)
	}
	return removed, errors.Join(errs...)
}

// sweepOrphans removes old regular files named *TempSuffix in the temp
// directory that no open row (referenced) and no lease owns. Symlinks and
// other file types are left alone.
func (m *Manager) sweepOrphans(cutoff time.Time, referenced map[string]bool) (int, error) {
	entries, err := os.ReadDir(m.cfg.Dir)
	if err != nil {
		return 0, fmt.Errorf("transfer: read temp directory: %w", err)
	}
	var errs []error
	removed := 0
	for _, e := range entries {
		name := e.Name()
		if !strings.HasSuffix(name, TempSuffix) || referenced[name] || m.leased(strings.TrimSuffix(name, TempSuffix)) {
			continue
		}
		info, err := e.Info()
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			errs = append(errs, err)
			continue
		}
		if !info.Mode().IsRegular() || !info.ModTime().Before(cutoff) {
			continue
		}
		if err := os.Remove(filepath.Join(m.cfg.Dir, name)); err != nil && !errors.Is(err, fs.ErrNotExist) {
			errs = append(errs, err)
			continue
		}
		removed++
	}
	return removed, errors.Join(errs...)
}

// StartSweeper runs Sweep once now and then every interval (DefaultSweepInterval
// if interval <= 0) until the returned stop function is called or the Manager
// is drained; stop waits for the sweeper to exit and may be called more than
// once. Calling StartSweeper twice replaces nothing: the second call returns
// a stop function for the sweeper already running.
func (m *Manager) StartSweeper(interval time.Duration) (stop func()) {
	if interval <= 0 {
		interval = DefaultSweepInterval
	}
	m.mu.Lock()
	if m.sweepStop == nil && !m.closed {
		m.sweepStop = make(chan struct{})
		m.sweepDone = make(chan struct{})
		go m.sweepLoop(interval, m.sweepStop, m.sweepDone)
	}
	m.mu.Unlock()
	return m.stopSweeper
}

// sweepLoop is the sweeper goroutine.
func (m *Manager) sweepLoop(interval time.Duration, stop <-chan struct{}, done chan<- struct{}) {
	defer close(done)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		if _, err := m.Sweep(m.cfg.Now()); err != nil {
			m.cfg.Logf("transfer: sweep: %v", err)
		}
		select {
		case <-ticker.C:
		case <-stop:
			return
		}
	}
}

// stopSweeper stops the background sweeper, if one runs, and waits for it.
func (m *Manager) stopSweeper() {
	m.mu.Lock()
	stop, done := m.sweepStop, m.sweepDone
	m.sweepStop = nil
	m.mu.Unlock()
	if stop == nil {
		return
	}
	close(stop)
	<-done
}

// Drain stops the engine from accepting new work (new calls get ErrClosed),
// stops the sweeper, and waits until every chunk write and completion that had
// begun has finished, so no acknowledged chunk is lost at shutdown. It returns
// ctx.Err() if ctx ends first; the work in progress is then still running.
func (m *Manager) Drain(ctx context.Context) error {
	m.mu.Lock()
	m.closed = true
	m.mu.Unlock()
	m.stopSweeper()
	done := make(chan struct{})
	go func() {
		m.inflight.Wait()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Close drains without a time limit.
func (m *Manager) Close() error { return m.Drain(context.Background()) }
