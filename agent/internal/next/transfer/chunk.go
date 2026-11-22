package transfer

import (
	"crypto/sha256"
	"fmt"
	"os"

	"github.com/ZaidQamhieh/rfe/agent/internal/next/api"
	"github.com/ZaidQamhieh/rfe/agent/internal/next/authz"
	"github.com/ZaidQamhieh/rfe/agent/internal/next/store"
)

// WriteChunk stores chunk n of a session. The chunk must have exactly the
// length the session's geometry requires and match chunkSHA256 (hex, either
// letter case). The order is fixed: write the bytes, fsync the temp file,
// record the chunk row, then return nil, so a nil return means the chunk
// survives a crash. Sending a chunk that is already recorded is a no-op. Any
// number of chunks of one session may be written in parallel.
func (m *Manager) WriteChunk(p authz.Principal, id string, n int, data []byte, chunkSHA256 string) error {
	u, release, err := m.begin(p, id, writeLease)
	if err != nil {
		return err
	}
	defer release()
	if n < 0 || n >= u.TotalChunks {
		return fmt.Errorf("%w: chunk %d is outside 0..%d", api.ErrBadRequest, n, u.TotalChunks-1)
	}
	if want := chunkLen(u.TotalSize, u.ChunkSize, n); len(data) != want {
		return fmt.Errorf("%w: chunk %d is %d bytes, want %d", api.ErrBadRequest, n, len(data), want)
	}
	want, ok := parseSHA256(chunkSHA256)
	if !ok {
		return fmt.Errorf("%w: chunk sha256 %s", api.ErrBadRequest, sha256DigitsMsg)
	}
	if sha256.Sum256(data) != want {
		return fmt.Errorf("%w: chunk %d", api.ErrChunkHashMismatch, n)
	}
	switch has, err := m.cfg.Store.HasChunk(id, n); {
	case err != nil:
		return fmt.Errorf("transfer: look up chunk: %w", err)
	case has:
		return nil
	}
	if err := m.writeDurably(u, n, data); err != nil {
		return err
	}
	if err := m.cfg.Store.AddChunk(id, n); err != nil {
		return fmt.Errorf("transfer: record chunk: %w", err)
	}
	return nil
}

// writeDurably writes data at the offset of chunk n in the session's temp file
// and fsyncs the file before returning.
func (m *Manager) writeDurably(u *store.Upload, n int, data []byte) error {
	path, err := m.tempPath(u)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_WRONLY, tempFileMode)
	if err != nil {
		return fmt.Errorf(errOpenTempFmt, err)
	}
	if _, err := f.WriteAt(data, chunkOffset(u.ChunkSize, n)); err != nil {
		_ = f.Close()
		return fmt.Errorf("transfer: write chunk: %w", err)
	}
	if err := m.cfg.SyncFile(f); err != nil {
		_ = f.Close()
		return fmt.Errorf("transfer: flush chunk: %w", err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("transfer: close temp file: %w", err)
	}
	return nil
}
