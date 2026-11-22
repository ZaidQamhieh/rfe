package server

import (
	"log"
	"net/http"
	"time"

	"github.com/ZaidQamhieh/rfe/agent/internal/backup"
	"github.com/ZaidQamhieh/rfe/agent/internal/launcher"
)

// launcherInfo is the part of the launcher's state.json that is safe to show an admin: which versions are current,
// previous, waiting to start, or refused. It carries no path.
type launcherInfo struct {
	Current  string    `json:"current"`
	Previous string    `json:"previous"`
	Pending  string    `json:"pending"`
	Bad      []string  `json:"bad"`
	Updated  time.Time `json:"updated,omitzero"`
}

// backupsResponse is the body of GET /v1/system/backups: the backup scheduler's status plus, when the agent runs
// under the launcher, its version pointers.
type backupsResponse struct {
	backup.StatusReport
	Launcher      *launcherInfo `json:"launcher"`
	LauncherError string        `json:"launcher_error,omitempty"`
}

// backupsStatusHandler implements GET /v1/system/backups (admin only). The status file only ever holds paths relative
// to the data dir or bare file names, and error texts are scrubbed of host paths before they are stored, so the reply
// names nothing outside the data dir.
func backupsStatusHandler(cfg Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rep, err := backup.Status(cfg.DataDir)
		if err != nil {
			log.Printf("backups: reading status: %v", err)
		}
		resp := backupsResponse{StatusReport: rep}
		if cfg.BundleRoot != "" {
			st, err := launcher.NewStore(cfg.BundleRoot).Read()
			if err != nil {
				log.Printf("backups: launcher state: %v", err)
				resp.LauncherError = "launcher state is unreadable"
			} else {
				resp.Launcher = &launcherInfo{Current: st.Current, Previous: st.Previous, Pending: st.Pending, Bad: st.Bad, Updated: st.Updated}
			}
		}
		writeJSON(w, http.StatusOK, resp)
	}
}
