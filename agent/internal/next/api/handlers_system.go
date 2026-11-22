package api

import (
	"net/http"

	"github.com/ZaidQamhieh/rfe/agent/internal/next/pairing"
)

// health answers the liveness probe. Anyone gets {"status":"ok"}; the identity,
// version, addresses and read-only flag are added only when the request carries
// a valid, non-revoked device token, so an unpaired caller learns nothing else.
func (s *Server) health(w http.ResponseWriter, r *http.Request) error {
	authenticated := false
	if token := pairing.BearerToken(r.Header.Get(headerAuthorization)); token != "" {
		_, err := s.cfg.Pairing.Authenticate(token)
		authenticated = err == nil // a bad token is treated as no token, not as an error
	}
	writeJSON(w, http.StatusOK, s.cfg.System.Health(authenticated))
	return nil
}

// status answers the authenticated self-report (version, uptime, disk, addresses).
func (s *Server) status(w http.ResponseWriter, _ *http.Request) error {
	st, err := s.cfg.System.Status()
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, st)
	return nil
}

// metrics answers the admin-only live host readings.
func (s *Server) metrics(w http.ResponseWriter, _ *http.Request) error {
	writeJSON(w, http.StatusOK, s.cfg.System.Metrics())
	return nil
}
