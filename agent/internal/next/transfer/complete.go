package transfer

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/ZaidQamhieh/rfe/agent/internal/next/api"
	"github.com/ZaidQamhieh/rfe/agent/internal/next/authz"
	"github.com/ZaidQamhieh/rfe/agent/internal/next/files"
	"github.com/ZaidQamhieh/rfe/agent/internal/next/store"
)

// Complete finishes a session: every chunk must be present
// (api.ErrUploadIncomplete otherwise, and the session stays open), the whole
// file must match the declared SHA-256 (api.ErrHashMismatch otherwise: the
// session is marked failed and its temp file removed), and the file is
// published at the target path by an atomic rename through pub, which replaces
// an existing file only when the session says overwrite (api.ErrConflict
// otherwise, session still open).
//
// If publishing worked but recording the completion failed, the entry is
// returned together with the error; the session stays open, and a repeated
// Complete finds the identical file already at the target and finishes
// without publishing again.
func (m *Manager) Complete(p authz.Principal, pub Publisher, id string) (*files.Entry, error) {
	if pub == nil {
		return nil, fmt.Errorf("%w: no file service", api.ErrBadRequest)
	}
	u, release, err := m.begin(p, id, completeLease)
	if err != nil {
		return nil, err
	}
	defer release()
	chunks, err := m.cfg.Store.ChunkNumbers(id)
	if err != nil {
		return nil, fmt.Errorf(errLoadChunksFmt, err)
	}
	if len(chunks) < u.TotalChunks {
		return nil, fmt.Errorf("%w: %d of %d chunks received", api.ErrUploadIncomplete, len(chunks), u.TotalChunks)
	}
	path, err := m.tempPath(u)
	if err != nil {
		return nil, err
	}
	got, err := hashFile(path)
	if err != nil {
		return nil, err
	}
	if want, ok := parseSHA256(u.SHA256); !ok || got != want {
		m.fail(u)
		return nil, api.ErrHashMismatch
	}
	entry, err := publishFile(pub, u, path)
	if errors.Is(err, api.ErrConflict) && !u.Overwrite {
		// A first attempt that published but failed to record leaves the same
		// bytes at the target; that is success, not a conflict.
		if existing := m.publishedAlready(pub, u); existing != nil {
			entry, err = existing, nil
		}
	}
	if err != nil {
		return nil, err
	}
	if err := m.cfg.Store.FinishUpload(id, store.UploadCompleted, m.now()); err != nil {
		return entry, fmt.Errorf("transfer: file published but recording it failed: %w", err)
	}
	m.removeTemp(u)
	return entry, nil
}

// publishedAlready returns the target's entry if it is a file of the declared
// size whose SHA-256 equals the session's, or nil.
func (m *Manager) publishedAlready(pub Publisher, u *store.Upload) *files.Entry {
	entry, err := pub.Meta(u.TargetPath)
	if err != nil || entry.IsDir || entry.Size != u.TotalSize {
		return nil
	}
	got, err := pub.Checksum(u.TargetPath, files.AlgoSHA256)
	if err != nil || !strings.EqualFold(got, u.SHA256) {
		return nil
	}
	return entry
}

// hashFile returns the SHA-256 of the file at path; the file is closed before
// it returns, so it can be removed afterwards on every platform.
func hashFile(path string) ([sha256.Size]byte, error) {
	var out [sha256.Size]byte
	f, err := os.Open(path)
	if err != nil {
		return out, fmt.Errorf(errOpenTempFmt, err)
	}
	defer f.Close()
	sum := sha256.New()
	if _, err := io.Copy(sum, f); err != nil {
		return out, fmt.Errorf("transfer: hash temp file: %w", err)
	}
	copy(out[:], sum.Sum(nil))
	return out, nil
}

// publishFile streams the temp file at path to the session's target through
// pub and closes the file before returning.
func publishFile(pub Publisher, u *store.Upload, path string) (*files.Entry, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf(errOpenTempFmt, err)
	}
	defer f.Close()
	return pub.PublishReader(u.TargetPath, f, u.Overwrite)
}

// fail marks a session whose whole-file hash did not match as failed and
// removes its temp file: it cannot be resumed, because it is unknown which
// chunk is wrong. If the status cannot be recorded the row is deleted so its
// reservation is released.
func (m *Manager) fail(u *store.Upload) {
	if err := m.cfg.Store.FinishUpload(u.ID, store.UploadFailed, m.now()); err != nil {
		m.cfg.Logf("transfer: record failure of %s: %v", u.ID, err)
		if delErr := m.cfg.Store.DeleteUpload(u.ID); delErr != nil {
			m.cfg.Logf("transfer: release reservation of %s: %v", u.ID, delErr)
		}
	}
	m.removeTemp(u)
}
