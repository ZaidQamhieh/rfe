package server

import (
	"net/http"
	"time"

	"github.com/ZaidQamhieh/rfe/agent/internal/store"
)

// restartDelay lets the HTTP response reach the caller before the restart
// command tears down this process. Var (not const) so tests can shrink it.
var restartDelay = 300 * time.Millisecond

// restartSupportedFn/restartAgentFn are package vars (not direct calls to
// restartSupported/restartAgent) so tests can stub out the real systemctl
// invocation instead of actually restarting a service in CI.
var (
	restartSupportedFn = restartSupported
	restartAgentFn     = restartAgent
)

// restartHandler triggers a restart of the agent's own service. Restart-only
// by design — no remote stop endpoint — so there is nothing to get
// permanently stuck in even if the only device that could reach the agent is
// the one issuing the request.
func restartHandler(db *store.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !restartSupportedFn() {
			writeError(w, http.StatusNotImplemented, "NOT_IMPLEMENTED", "remote restart is not supported on this platform")
			return
		}
		audit(db, r, store.AuditAgentRestart, "", "")
		writeJSON(w, http.StatusAccepted, map[string]bool{"ok": true})
		go func() {
			time.Sleep(restartDelay)
			_ = restartAgentFn()
		}()
	}
}
