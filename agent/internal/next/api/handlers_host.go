package api

import (
	"context"
	"errors"
	"net/http"
	"strconv"

	"github.com/ZaidQamhieh/rfe/agent/internal/next/audit"
	"github.com/ZaidQamhieh/rfe/agent/internal/next/hostops"
	"github.com/ZaidQamhieh/rfe/agent/internal/next/pairing"
	"github.com/ZaidQamhieh/rfe/agent/internal/next/store"
	"github.com/ZaidQamhieh/rfe/agent/internal/updates"
)

// Names, codes and fixed client texts of the host routes. The texts are the
// old agent's, word for word, except where a comment says otherwise.
const (
	pathUsername = "username"

	// codeLastUser is the code of a refused delete of the only account
	// (openapi Error enum).
	codeLastUser = "LAST_USER"

	msgNoSuchUser     = "no such user"
	msgLastUser       = "cannot delete the only remaining login account"
	msgRestartUnsupp  = "remote restart is not supported on this platform"
	msgWolRateLimited = "too many Wake-on-LAN requests, try again in a minute"
	msgInvalidMAC     = "MAC must be 6-byte colon-separated"
	msgNoUpdate       = "no update available"

	wolStatusSent = "sent"
)

// HostServices are the optional services behind /users, /logs, /agent/restart,
// /wol and /app/latest, /app/download. A nil service leaves its routes
// answering 501 NOT_IMPLEMENTED, so a build without it still starts.
type HostServices struct {
	Users   UserDirectory
	Logs    LogSource
	Restart Restarter
	Wake    WakeService
	Updates UpdateSource
	Backups BackupStatus
}

// UserDirectory lists and removes login accounts. *store.DB satisfies it.
type UserDirectory interface {
	Users() ([]store.User, error)
	DeleteUser(username string) error
}

// LogSource reads the newest lines of the agent's log. hostops.Journal satisfies it.
type LogSource interface {
	Tail(ctx context.Context, limit int) ([]hostops.LogLine, error)
}

// Restarter restarts the agent's service. Supported says whether this host can;
// Restart is called after the answer was sent and may wait before it acts.
// *hostops.Restarter satisfies it.
type Restarter interface {
	Supported() bool
	Restart() error
}

// WakeService sends a Wake-on-LAN packet for a device and returns the MAC in
// canonical form. It reports hostops.ErrInvalidMAC and hostops.ErrWakeRateLimited.
// *hostops.Waker satisfies it.
type WakeService interface {
	Wake(deviceID, mac string) (string, error)
}

// BackupStatus reports the backup scheduler's status and the launcher's version pointers for
// GET /system/backups; the value is encoded as the reply body (openapi BackupsStatus).
type BackupStatus interface{ Report() (any, error) }

// UpdateSource finds the newest Android APK. Latest and Open return nil, nil
// when there is none. *hostops.Updates satisfies it.
type UpdateSource interface {
	Latest() (*updates.Release, error)
	Open() (*hostops.Download, error)
}

// registerHostHandlers adds the handlers of /users, /logs, /agent/restart, /wol and /app/latest, /app/download.
func registerHostHandlers(s *Server, m map[string]handlerFunc) {
	m[routeKey(get, "/users")] = s.usersList
	m[routeKey(del, "/users/{username}")] = s.userDelete
	m[routeKey(get, "/logs")] = s.logsList
	m[routeKey(get, "/system/backups")] = s.backupsStatus
	m[routeKey(post, "/agent/restart")] = s.agentRestart
	m[routeKey(post, "/wol")] = s.wakeOnLAN
	m[routeKey(get, "/app/latest")] = s.appLatest
	m[routeKey(get, "/app/download")] = s.appDownload
}

// UserJSON is one account in GET /users (openapi UserSummary): never the hash.
type UserJSON struct {
	Username string `json:"username"`
	Created  int64  `json:"created"`
}

// actorName names the calling device for the audit trail: its label, or its id
// when it has none.
func actorName(c *call) string {
	if c.device == nil {
		return ""
	}
	if c.device.Label != "" {
		return c.device.Label
	}
	return c.device.ID
}

// usersList answers GET /users with every login account's name and creation time.
func (s *Server) usersList(w http.ResponseWriter, _ *http.Request) error {
	svc := s.cfg.Host.Users
	if svc == nil {
		return notYet
	}
	users, err := svc.Users()
	if err != nil {
		return err
	}
	out := make([]UserJSON, 0, len(users))
	for _, u := range users {
		out = append(out, UserJSON{Username: u.Username, Created: u.Created})
	}
	writeJSON(w, http.StatusOK, out)
	return nil
}

// userDelete answers DELETE /users/{username}: 204, 404 for an unknown account,
// 400 LAST_USER for the only remaining one.
func (s *Server) userDelete(w http.ResponseWriter, r *http.Request) error {
	svc := s.cfg.Host.Users
	if svc == nil {
		return notYet
	}
	err := svc.DeleteUser(r.PathValue(pathUsername))
	switch {
	case err == nil:
		writeNoContent(w)
		return nil
	case errors.Is(err, store.ErrUserNotFound):
		return &apiError{kind: ErrNotFound, msg: msgNoSuchUser}
	case errors.Is(err, store.ErrLastUser):
		// A typed error carries its own code, which is how Map sends it.
		return &pairing.Error{Code: codeLastUser, Status: http.StatusBadRequest, Message: msgLastUser}
	}
	return err
}

// backupsStatus answers GET /system/backups (admin only) with the backup and launcher status.
func (s *Server) backupsStatus(w http.ResponseWriter, _ *http.Request) error {
	svc := s.cfg.Host.Backups
	if svc == nil {
		return notYet
	}
	rep, err := svc.Report()
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, rep)
	return nil
}

// logsList answers GET /logs with the newest lines of the agent's log. The
// optional limit query picks how many (an unusable value means the default). A
// host without a readable log answers an empty list.
func (s *Server) logsList(w http.ResponseWriter, r *http.Request) error {
	svc := s.cfg.Host.Logs
	if svc == nil {
		return notYet
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get(queryLimit)) // a bad number is 0: the default applies
	lines, err := svc.Tail(r.Context(), limit)
	if errors.Is(err, hostops.ErrLogsUnavailable) {
		s.logf("api: logs: %v", err)
		lines, err = []hostops.LogLine{}, nil
	}
	if err != nil {
		return err
	}
	if lines == nil {
		lines = []hostops.LogLine{}
	}
	writeJSON(w, http.StatusOK, lines)
	return nil
}

// agentRestart answers POST /agent/restart with 202 and then restarts the
// service in the background; it answers 501 where that is not supported.
func (s *Server) agentRestart(w http.ResponseWriter, r *http.Request) error {
	svc := s.cfg.Host.Restart
	if svc == nil {
		return notYet
	}
	if !svc.Supported() {
		return &apiError{kind: ErrNotImplemented, msg: msgRestartUnsupp}
	}
	c, err := callOf(r)
	if err != nil {
		return err
	}
	s.recordAudit([]audit.Event{{Action: audit.ActionAgentRestart, Actor: actorName(c)}})
	writeJSON(w, http.StatusAccepted, map[string]bool{"ok": true})
	go func() {
		if err := svc.Restart(); err != nil {
			s.logf("api: agent restart: %v", err)
		}
	}()
	return nil
}

// wolBody is the JSON of POST /wol.
type wolBody struct {
	MAC string `json:"mac"`
}

// wakeOnLAN answers POST /wol: it sends a magic packet for the MAC and records
// the event. A malformed MAC is BAD_REQUEST and a device over its budget is
// RATE_LIMITED. (Read-only devices are refused before this by the route's action.)
func (s *Server) wakeOnLAN(w http.ResponseWriter, r *http.Request) error {
	svc := s.cfg.Host.Wake
	if svc == nil {
		return notYet
	}
	c, err := callOf(r)
	if err != nil {
		return err
	}
	var body wolBody
	if err := decodeJSON(r, &body); err != nil {
		return err
	}
	deviceID := ""
	if c.device != nil {
		deviceID = c.device.ID
	}
	mac, err := svc.Wake(deviceID, body.MAC)
	switch {
	case errors.Is(err, hostops.ErrWakeRateLimited):
		return &apiError{kind: ErrRateLimited, msg: msgWolRateLimited}
	case errors.Is(err, hostops.ErrInvalidMAC):
		return badRequest(msgInvalidMAC)
	case err != nil:
		return err // the real cause is logged; the client sees the fixed internal text
	}
	s.recordAudit([]audit.Event{{Action: audit.ActionWake, Actor: actorName(c), Target: mac}})
	writeJSON(w, http.StatusOK, map[string]string{"status": wolStatusSent})
	return nil
}

// appLatest answers GET /app/latest with the newest APK's version, size and
// SHA-256, or 204 when there is none.
func (s *Server) appLatest(w http.ResponseWriter, _ *http.Request) error {
	svc := s.cfg.Host.Updates
	if svc == nil {
		return notYet
	}
	rel, err := svc.Latest()
	if err != nil {
		return err
	}
	if rel == nil {
		writeNoContent(w)
		return nil
	}
	writeJSON(w, http.StatusOK, rel)
	return nil
}

// appDownload answers GET /app/download with the newest APK. http.ServeContent
// adds Content-Length and Range support (206, 416).
func (s *Server) appDownload(w http.ResponseWriter, r *http.Request) error {
	svc := s.cfg.Host.Updates
	if svc == nil {
		return notYet
	}
	dl, err := svc.Open()
	if err != nil {
		return err
	}
	if dl == nil {
		return &apiError{kind: ErrNotFound, msg: msgNoUpdate}
	}
	defer func() {
		if err := dl.Close(); err != nil {
			s.logf("api: close update file: %v", err)
		}
	}()
	w.Header().Set(headerContentType, hostops.APKContentType)
	http.ServeContent(w, r, dl.Name, dl.ModTime, dl.Content)
	return nil
}
