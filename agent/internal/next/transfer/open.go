package transfer

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/ZaidQamhieh/rfe/agent/internal/next/api"
	"github.com/ZaidQamhieh/rfe/agent/internal/next/authz"
	"github.com/ZaidQamhieh/rfe/agent/internal/next/store"
)

// OpenRequest describes an upload to start. ID is chosen by the caller (the
// server generates a UUID); opening the same ID again with identical fields
// returns the existing session, so a lost response can be retried.
type OpenRequest struct {
	ID        string
	Path      string
	Size      int64
	ChunkSize int
	SHA256    string // hex digest of the whole file, either letter case
	Overwrite bool
}

// sha256DigitsMsg is the rejection text for a malformed digest, built from the
// digest's hex length: "must be 64 hex digits".
var sha256DigitsMsg = fmt.Sprintf("must be %d hex digits", hex.EncodedLen(sha256.Size))

// parseSHA256 decodes a 64-digit hex digest in either letter case.
func parseSHA256(s string) ([sha256.Size]byte, bool) {
	var out [sha256.Size]byte
	if len(s) != hex.EncodedLen(sha256.Size) {
		return out, false
	}
	if _, err := hex.Decode(out[:], []byte(s)); err != nil {
		return out, false
	}
	return out, true
}

// validate checks a request against the limits and returns its chunk count.
func (m *Manager) validate(req OpenRequest) (int, error) {
	lim := m.cfg.Limits
	switch {
	case !validID(req.ID):
		return 0, fmt.Errorf("%w: invalid transfer id", api.ErrBadRequest)
	case req.Path == "" || len(req.Path) > MaxPathLen:
		return 0, fmt.Errorf("%w: invalid target path", api.ErrBadRequest)
	case req.Size < 0:
		return 0, fmt.Errorf("%w: negative size", api.ErrBadRequest)
	case req.Size > lim.MaxTransferSize:
		return 0, fmt.Errorf("%w: declared size is over the limit", api.ErrPayloadTooLarge)
	case req.ChunkSize <= 0 || req.ChunkSize > lim.MaxChunkSize:
		return 0, fmt.Errorf("%w: chunk size must be between 1 and %d", api.ErrBadRequest, lim.MaxChunkSize)
	}
	if _, ok := parseSHA256(req.SHA256); !ok {
		return 0, fmt.Errorf("%w: sha256 %s", api.ErrBadRequest, sha256DigitsMsg)
	}
	n := totalChunksFor(req.Size, req.ChunkSize)
	if n > MaxChunksPerUpload {
		return 0, fmt.Errorf("%w: too many chunks, use a larger chunk size", api.ErrBadRequest)
	}
	return n, nil
}

// Open starts an upload session for the caller, or returns the caller's
// existing open session with the same ID and fields. pub is the file service
// of the caller's view of the disk; it is used to refuse early when the target
// exists and overwrite is false (Complete decides again, atomically, at
// publish time). It enforces the per-device and per-host session and byte
// ceilings with api.ErrResourceLimit.
func (m *Manager) Open(p authz.Principal, pub Publisher, req OpenRequest) (*store.Upload, error) {
	if p.DeviceID == "" {
		return nil, fmt.Errorf("%w: no device", api.ErrForbidden)
	}
	total, err := m.validate(req)
	if err != nil {
		return nil, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return nil, ErrClosed
	}
	existing, err := m.cfg.Store.UploadByID(req.ID)
	if err != nil {
		return nil, fmt.Errorf("transfer: load session: %w", err)
	}
	if existing != nil {
		return m.reopenLocked(p, existing, req)
	}
	if !req.Overwrite {
		if err := checkFree(pub, req.Path); err != nil {
			return nil, err
		}
	}
	if _, err := m.sweepLocked(m.cfg.Now()); err != nil {
		m.cfg.Logf("transfer: sweep before admission: %v", err)
	}
	if err := m.admitLocked(p.DeviceID, req.Size); err != nil {
		return nil, err
	}
	u := store.Upload{
		ID: req.ID, DeviceID: p.DeviceID, TargetPath: req.Path,
		TotalSize: req.Size, ChunkSize: req.ChunkSize, TotalChunks: total,
		SHA256: strings.ToLower(req.SHA256), Overwrite: req.Overwrite,
		Status: store.UploadOpen, TempName: req.ID + TempSuffix, UpdatedAt: m.now(),
	}
	if err := m.cfg.Store.InsertUpload(u); err != nil {
		return nil, fmt.Errorf("transfer: record session: %w", err)
	}
	// The row is written first so the reservation counts at once; if the temp
	// file cannot be made the row is released again.
	if err := m.createTemp(&u); err != nil {
		if delErr := m.cfg.Store.DeleteUpload(u.ID); delErr != nil {
			m.cfg.Logf("transfer: release reservation of %s: %v", u.ID, delErr)
		}
		return nil, err
	}
	return &u, nil
}

// checkFree returns api.ErrConflict if something already exists at path.
func checkFree(pub Publisher, path string) error {
	if pub == nil {
		return fmt.Errorf("%w: no file service", api.ErrBadRequest)
	}
	_, err := pub.Meta(path)
	switch {
	case err == nil:
		return fmt.Errorf("%w: the target already exists", api.ErrConflict)
	case errors.Is(err, api.ErrPathNotFound):
		return nil
	}
	return err
}

// reopenLocked answers Open for an id that already has a row: the owner's open
// session with identical fields is returned (with its received chunks);
// anything else is a conflict, and never says whose session it is.
func (m *Manager) reopenLocked(p authz.Principal, u *store.Upload, req OpenRequest) (*store.Upload, error) {
	want, _ := parseSHA256(req.SHA256)
	have, haveOK := parseSHA256(u.SHA256)
	same := u.Status == store.UploadOpen && visibleTo(p, u) && u.TargetPath == req.Path &&
		u.TotalSize == req.Size && u.ChunkSize == req.ChunkSize && u.Overwrite == req.Overwrite &&
		haveOK && have == want
	if !same {
		return nil, fmt.Errorf("%w: transfer id is already in use", api.ErrConflict)
	}
	return m.statusLocked(u)
}

// admitLocked applies the session-count and reserved-byte ceilings to a new
// session of size bytes for device.
func (m *Manager) admitLocked(device string, size int64) error {
	open, err := m.cfg.Store.OpenUploads()
	if err != nil {
		return fmt.Errorf("transfer: list open sessions: %w", err)
	}
	lim := m.cfg.Limits
	var hostN, devN int
	var hostB, devB int64
	for _, o := range open {
		hostN++
		hostB += o.TotalSize
		if o.DeviceID == device {
			devN++
			devB += o.TotalSize
		}
	}
	if hostN >= lim.MaxActivePerHost || hostB+size > lim.MaxReservedPerHost ||
		devN >= lim.MaxActivePerDevice || devB+size > lim.MaxReservedPerDevice {
		return api.ErrResourceLimit
	}
	return nil
}

// createTemp makes the session's temp file at its full declared size (sparse
// where the file system allows) and flushes it and its directory entry, so a
// chunk acknowledged later is never lost because the file itself was not
// durable. A failure removes the partial file.
func (m *Manager) createTemp(u *store.Upload) (err error) {
	path, err := m.tempPath(u)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, tempFileMode)
	if err != nil {
		return fmt.Errorf("transfer: create temp file: %w", err)
	}
	defer func() {
		if err != nil {
			if rmErr := os.Remove(path); rmErr != nil && !errors.Is(rmErr, fs.ErrNotExist) {
				m.cfg.Logf("transfer: remove incomplete temp file of %s: %v", u.ID, rmErr)
			}
		}
	}()
	if err = sizeAndSync(m, f, u.TotalSize); err != nil {
		return err
	}
	if err = syncDir(filepath.Dir(path)); err != nil {
		return fmt.Errorf("transfer: flush temp directory: %w", err)
	}
	return nil
}

// sizeAndSync sets f's length to size, flushes it and closes it.
func sizeAndSync(m *Manager, f *os.File, size int64) error {
	if err := f.Truncate(size); err != nil {
		_ = f.Close()
		return fmt.Errorf("transfer: size temp file: %w", err)
	}
	if err := m.cfg.SyncFile(f); err != nil {
		_ = f.Close()
		return fmt.Errorf("transfer: flush temp file: %w", err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("transfer: close temp file: %w", err)
	}
	return nil
}

// Status returns the session with the numbers of the chunks received (the
// client sends the rest), and refreshes its activity time when it is open. A
// session being completed answers api.ErrTransferActive.
func (m *Manager) Status(p authz.Principal, id string) (*store.Upload, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	u, err := m.owned(p, id)
	if err != nil {
		return nil, err
	}
	if l := m.leases[id]; l != nil && l.completing {
		return nil, api.ErrTransferActive
	}
	return m.statusLocked(u)
}

// statusLocked loads the chunk numbers into u and, for an open session, marks
// it active.
func (m *Manager) statusLocked(u *store.Upload) (*store.Upload, error) {
	if u.Status == store.UploadOpen {
		if err := m.cfg.Store.TouchUpload(u.ID, m.now()); err != nil {
			return nil, fmt.Errorf(errRefreshSessFmt, err)
		}
	}
	chunks, err := m.cfg.Store.ChunkNumbers(u.ID)
	if err != nil {
		return nil, fmt.Errorf(errLoadChunksFmt, err)
	}
	out := *u
	out.Chunks = chunks
	return &out, nil
}

// Lookup returns the session row (no chunks) without refreshing its activity
// time, for a caller that must check what a session is before using it.
func (m *Manager) Lookup(p authz.Principal, id string) (*store.Upload, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.owned(p, id)
}

// Delete removes a session and its temp file. A session with a chunk write or
// a completion in progress answers api.ErrTransferActive. The row is removed
// first; a temp file that cannot be removed is logged and left to the orphan
// sweep.
func (m *Manager) Delete(p authz.Principal, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	u, err := m.owned(p, id)
	if err != nil {
		return err
	}
	if m.leased(id) {
		return api.ErrTransferActive
	}
	if err := m.cfg.Store.DeleteUpload(id); err != nil {
		return fmt.Errorf("transfer: delete session: %w", err)
	}
	m.removeTemp(u)
	return nil
}

// DropDevice deletes the device's open sessions that no request is using, with their temporary files. A revoked or
// removed device cannot finish them, and left alone they would keep its upload quota full if the same device id is
// paired again. It is best effort: a session that cannot be removed is left for the sweeper.
func (m *Manager) DropDevice(deviceID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	open, err := m.cfg.Store.OpenUploads()
	if err != nil {
		return
	}
	for i := range open {
		u := &open[i]
		if u.DeviceID != deviceID {
			continue
		}
		if m.leased(u.ID) {
			m.doomed[u.ID] = true // in use now; removed when the request ends
			continue
		}
		if m.cfg.Store.DeleteUpload(u.ID) == nil {
			m.removeTemp(u)
		}
	}
}

// dropDoomedLocked removes a session marked by DropDevice once nothing uses it,
// unless it has since finished. The caller holds m.mu.
func (m *Manager) dropDoomedLocked(id string) {
	if !m.doomed[id] {
		return
	}
	delete(m.doomed, id)
	u, err := m.cfg.Store.UploadByID(id)
	if err != nil || u == nil || u.Status != store.UploadOpen {
		return
	}
	if m.cfg.Store.DeleteUpload(id) == nil {
		m.removeTemp(u)
	}
}
