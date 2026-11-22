package daemon

import (
	"log"
	"time"

	"github.com/ZaidQamhieh/rfe/agent/internal/backup"
	"github.com/ZaidQamhieh/rfe/agent/internal/launcher"
)

// backupStatus serves GET /system/backups: the backup scheduler's status file plus, when the agent runs under the
// launcher (bundleRoot set), its version pointers. Same body as the current agent's handler (internal/server).
type backupStatus struct {
	dataDir, bundleRoot string
	logf                func(string, ...any)
}

type launcherInfo struct {
	Current  string    `json:"current"`
	Previous string    `json:"previous"`
	Pending  string    `json:"pending"`
	Bad      []string  `json:"bad"`
	Updated  time.Time `json:"updated,omitzero"`
}

type backupsResponse struct {
	backup.StatusReport
	Launcher      *launcherInfo `json:"launcher"`
	LauncherError string        `json:"launcher_error,omitempty"`
}

// Report reads the status file (a missing or damaged one yields an empty report) and the launcher state.
func (b backupStatus) Report() (any, error) {
	logf := b.logf
	if logf == nil {
		logf = log.Printf
	}
	rep, err := backup.Status(b.dataDir)
	if err != nil {
		logf("backups: reading status: %v", err)
	}
	resp := backupsResponse{StatusReport: rep}
	if b.bundleRoot != "" {
		st, err := launcher.NewStore(b.bundleRoot).Read()
		if err != nil {
			logf("backups: launcher state: %v", err)
			resp.LauncherError = "launcher state is unreadable"
		} else {
			resp.Launcher = &launcherInfo{Current: st.Current, Previous: st.Previous, Pending: st.Pending, Bad: st.Bad, Updated: st.Updated}
		}
	}
	return resp, nil
}
