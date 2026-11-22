package transfer

import (
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/ZaidQamhieh/rfe/agent/internal/fsops"
)

// publishIntentMinAge is how old a recorded publish temp must be before the sweep may remove it, so a slow
// copy that is still running (or just ended) is never raced.
const publishIntentMinAge = time.Hour

// PublishOptions returns the fsops hooks that make session id's publish leave a trace: its temp gets the
// deterministic name, an intent row (folder + name) is stored before the copy, and the row is deleted
// once the temp name is gone. A killed publish keeps its row, which the sweep later follows. MoveFrom names
// the session's own temp so a publish on the same filesystem moves that file into place instead of copying it.
func (m *Manager) PublishOptions(id string) fsops.PublishOptions {
	name := fsops.PublishTempName(id)
	return fsops.PublishOptions{
		TempName: name,
		MoveFrom: m.tempPathFor(id),
		Begin: func(destDir, tempName string) error {
			return m.db.PutPublishIntent(id, destDir, tempName, time.Now().Unix())
		},
		End: func() {
			if err := m.db.DeletePublishIntent(id); err != nil {
				log.Printf("transfer: could not drop the publish intent of session %s: %v", id, err)
			}
		},
	}
}

// sweepPublishIntentsLocked removes the publish temps that sessions recorded more than publishIntentMinAge
// before now and then deletes their rows. It acts only on recorded rows (never a glob), skips a session
// that is publishing now (its completion lease is held; the caller holds m.mu or runs before any
// completion), and deletes through fsops.RemovePublishTemp, which refuses symlinks. A row that does not
// name the session's own temp, or that fsops refuses, is dropped without touching any file; a row whose
// removal failed for another reason stays for the next sweep. It returns how many temps it removed.
func (m *Manager) sweepPublishIntentsLocked(now time.Time) (int, error) {
	rows, err := m.db.ListPublishIntentsBefore(now.Add(-publishIntentMinAge).Unix())
	if err != nil {
		return 0, fmt.Errorf("list publish intents: %w", err)
	}
	removed := 0
	var sweepErr error
	for _, row := range rows {
		if m.completing[row.SessionID] {
			continue
		}
		if row.TempName == fsops.PublishTempName(row.SessionID) {
			err := fsops.RemovePublishTemp(row.DestDir, row.TempName)
			if err != nil && !errors.Is(err, fsops.ErrPublishTempRefused) {
				log.Printf("transfer: could not remove the publish temp of session %s: %v", row.SessionID, err)
				if sweepErr == nil {
					sweepErr = fmt.Errorf("remove publish temp of session %s: %w", row.SessionID, err)
				}
				continue
			}
			if err == nil {
				removed++
				m.stats.publishTempFoundInDest.Add(1)
			} else {
				log.Printf("transfer: left the publish temp of session %s in place: %v", row.SessionID, err)
			}
		} else {
			log.Printf("transfer: ignored the publish intent of session %s: it does not name the session's own temp", row.SessionID)
		}
		if err := m.db.DeletePublishIntent(row.SessionID); err != nil {
			if sweepErr == nil {
				sweepErr = fmt.Errorf("delete publish intent of session %s: %w", row.SessionID, err)
			}
		}
	}
	return removed, sweepErr
}
