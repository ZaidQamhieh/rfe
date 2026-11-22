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
	"strings"

	"github.com/ZaidQamhieh/rfe/agent/internal/store"
)

// resetBrokenSessions first completes every open session whose bytes are already published (see
// alreadyPublished), then marks as failed every open upload session whose temp file is gone or shorter than
// the chunks the database says were received. Such a session can never complete (the bytes are not on
// disk), and resuming it would make the phone skip chunks it believes were delivered. It returns how
// many sessions were reset. A restored backup or a lost temp dir is what usually causes this.
func (m *Manager) resetBrokenSessions() (int, error) {
	open, err := m.db.ListOpenTransfers()
	if err != nil {
		return 0, fmt.Errorf("list open transfers: %w", err)
	}
	reset := 0
	for i := range open {
		t := &open[i]
		published, err := m.alreadyPublished(t)
		if err != nil {
			return reset, err
		}
		if published {
			if err := m.db.SetTransferStatus(t.ID, store.TransferCompleted); err != nil {
				return reset, fmt.Errorf("complete published transfer %s: %w", t.ID, err)
			}
			if path, ok := m.sessionTempPath(t); ok {
				if rmErr := removeTempIgnoreMissing(path); rmErr != nil {
					log.Printf("transfer: could not remove temp file of published session %s: %v", t.ID, rmErr)
				}
			}
			log.Printf("transfer: upload session %s was published before a restart; marked completed", t.ID)
			continue
		}
		broken, err := m.sessionBroken(t)
		if err != nil {
			return reset, err
		}
		if !broken {
			continue
		}
		if err := m.db.SetTransferStatus(t.ID, store.TransferFailed); err != nil {
			return reset, fmt.Errorf("fail broken transfer %s: %w", t.ID, err)
		}
		// The failed row can never resume, so its leftover short file only wastes disk.
		if path, ok := m.sessionTempPath(t); ok {
			if rmErr := os.Remove(path); rmErr != nil && !errors.Is(rmErr, fs.ErrNotExist) {
				log.Printf("transfer: could not remove temp file of reset session %s: %v", t.ID, rmErr)
			}
		}
		log.Printf("transfer: reset upload session %s: temp file missing or shorter than its received chunks", t.ID)
		reset++
		m.stats.sessionsResetByReconcile.Add(1)
	}
	return reset, nil
}

// alreadyPublished reports whether an open session whose chunks have all arrived was already published:
// the destination is a regular file of the declared size whose SHA-256 equals the session's. A crash
// between making the file visible and recording it leaves exactly this state; the bytes are the ones
// the client asked for, so the session is done (never failed, never needing a second publish).
func (m *Manager) alreadyPublished(t *store.Transfer) (bool, error) {
	if t.ReceivedCount < t.TotalChunks {
		return false, nil
	}
	// Lstat, not Stat: a symlink at the destination is never read as proof of publication.
	info, err := os.Lstat(t.TargetPath)
	if err != nil || !info.Mode().IsRegular() || info.Size() != t.TotalSize {
		return false, nil // absent, unreadable or different: not published by this session
	}
	f, err := os.Open(t.TargetPath)
	if err != nil {
		log.Printf("transfer: cannot open destination of session %s to check publication: %v", t.ID, err)
		return false, nil
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return false, fmt.Errorf("hash destination of transfer %s: %w", t.ID, err)
	}
	return strings.EqualFold(hex.EncodeToString(h.Sum(nil)), t.SHA256), nil
}

// sessionBroken reports whether the session's temp file is missing, or shorter than the end of the
// furthest chunk recorded as received.
func (m *Manager) sessionBroken(t *store.Transfer) (bool, error) {
	path, ok := m.sessionTempPath(t)
	if !ok {
		return true, nil // not a path this manager owns, so it can never be resumed here
	}
	info, err := os.Stat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return true, nil
	}
	if err != nil {
		return false, fmt.Errorf("stat temp file of transfer %s: %w", t.ID, err)
	}
	if t.ReceivedCount == 0 {
		return false, nil
	}
	chunks, err := m.db.ChunkNumbers(t.ID)
	if err != nil {
		return false, fmt.Errorf("list chunks of transfer %s: %w", t.ID, err)
	}
	var need int64
	for _, n := range chunks {
		l := t.ExpectedChunkLen(n)
		if l < 0 {
			return true, nil // a received chunk outside the session's own range: the row is inconsistent
		}
		if end := int64(n)*int64(t.ChunkSize) + int64(l); end > need {
			need = end
		}
	}
	return info.Size() < need, nil
}
