package api

import (
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/ZaidQamhieh/rfe/agent/internal/next/audit"
	"github.com/ZaidQamhieh/rfe/agent/internal/next/authz"
)

// Limits, path pieces and fixed texts of the share routes.
const (
	// maxShareTokenBytes bounds the token of the public route. A real token is
	// 64 hex characters; a longer value cannot name a link.
	maxShareTokenBytes = 256
	sharePathSegment   = "/share/"
	shareURLScheme     = "https://"
	msgShareNotFound   = "share link not found or expired"
)

// ShareCaller is the device that mints, lists or revokes links.
type ShareCaller struct {
	Principal authz.Principal
	Label     string // names the device in audit events
}

// ShareMintRequest is the body of POST /share/mint. A zero lifetime means the
// share service's default.
type ShareMintRequest struct {
	Path             string
	ExpiresInSeconds int64
}

// ShareMinted is a new link. Token is the only copy of the secret.
type ShareMinted struct {
	Token     string
	TokenHash string
	ExpiresAt int64 // Unix seconds
	Events    []audit.Event
}

// ShareSummary is one active link as listed to its owner.
type ShareSummary struct {
	TokenHash string
	Path      string
	ExpiresAt int64 // Unix seconds
}

// ShareDownload is a file ready to be sent for a public link. The handler calls
// Close once it is done, also when sending fails.
type ShareDownload struct {
	Content io.ReadSeeker
	Name    string
	ModTime time.Time
	// Header carries the headers that stop a browser from running the file.
	Header http.Header
	// RecordErr is set when the serve could not be written to the history; the
	// file is still delivered and the handler logs it.
	RecordErr error
	Close     func() error
}

// ShareService mints, lists, revokes and serves share links. The implementation
// wraps the share package's Service (which this package cannot import).
type ShareService interface {
	Mint(c ShareCaller, req ShareMintRequest) (ShareMinted, error)
	List(c ShareCaller) ([]ShareSummary, error)
	Revoke(c ShareCaller, tokenHash string) ([]audit.Event, error)
	// Resolve is the unauthenticated fetch: it checks the link, uses it up and
	// opens the file. ip is the caller's address, for the rate limit.
	Resolve(token, ip string) (*ShareDownload, error)
}

// ShareLinkJSON is the answer of POST /share/mint (openapi ShareLink).
type ShareLinkJSON struct {
	Token     string `json:"token"`
	TokenHash string `json:"tokenHash"`
	ExpiresAt int64  `json:"expiresAt"`
	URL       string `json:"url"`
}

// ShareSummaryJSON is one row of GET /share (openapi ShareLinkSummary).
type ShareSummaryJSON struct {
	TokenHash string `json:"tokenHash"`
	Path      string `json:"path"`
	ExpiresAt int64  `json:"expiresAt"`
}

// shareMintBody is the JSON of POST /share/mint.
type shareMintBody struct {
	Path             string `json:"path"`
	ExpiresInSeconds int64  `json:"expiresInSeconds"`
}

// shareCaller describes the authenticated device of r for the share service.
func shareCaller(r *http.Request) (ShareCaller, error) {
	c, err := callOf(r)
	if err != nil {
		return ShareCaller{}, err
	}
	label := ""
	if c.device != nil {
		label = c.device.Label
	}
	return ShareCaller{Principal: c.principal, Label: label}, nil
}

// recordAudit writes the events a share call returned. A failure to write the
// trail does not undo the call, so the audit log's own error handling decides.
func (s *Server) recordAudit(events []audit.Event) {
	if s.cfg.Misc.Audit == nil {
		return
	}
	for _, ev := range events {
		s.cfg.Misc.Audit.Try(ev)
	}
}

// shareURL builds the address a recipient fetches: the configured base, or the
// Host the caller used, then the route and the token.
func (s *Server) shareURL(r *http.Request, token string) string {
	base := ""
	if s.cfg.Misc.ShareBaseURL != nil {
		base = strings.TrimRight(s.cfg.Misc.ShareBaseURL(), "/")
	}
	if base == "" {
		base = shareURLScheme + r.Host
	}
	return base + PathPrefix + sharePathSegment + token
}

// shareMint answers POST /share/mint with a new single-use link. The token is
// in this answer only; the path is not echoed back.
func (s *Server) shareMint(w http.ResponseWriter, r *http.Request) error {
	svc := s.cfg.Misc.Shares
	if svc == nil {
		return notYet
	}
	if !s.cfg.Policy.Get().AllowSharing { // sharing is off by default (old share_handlers.go:73)
		return ErrForbidden
	}
	caller, err := shareCaller(r)
	if err != nil {
		return err
	}
	var body shareMintBody
	if err := decodeJSON(r, &body); err != nil {
		return err
	}
	minted, err := svc.Mint(caller, ShareMintRequest{Path: body.Path, ExpiresInSeconds: body.ExpiresInSeconds})
	if err != nil {
		return err
	}
	s.recordAudit(minted.Events)
	writeJSON(w, http.StatusOK, ShareLinkJSON{
		Token: minted.Token, TokenHash: minted.TokenHash, ExpiresAt: minted.ExpiresAt,
		URL: s.shareURL(r, minted.Token),
	})
	return nil
}

// shareList answers GET /share with the caller's active links (every link for
// an admin device).
func (s *Server) shareList(w http.ResponseWriter, r *http.Request) error {
	svc := s.cfg.Misc.Shares
	if svc == nil {
		return notYet
	}
	caller, err := shareCaller(r)
	if err != nil {
		return err
	}
	links, err := svc.List(caller)
	if err != nil {
		return err
	}
	out := make([]ShareSummaryJSON, 0, len(links))
	for _, l := range links {
		out = append(out, ShareSummaryJSON{TokenHash: l.TokenHash, Path: l.Path, ExpiresAt: l.ExpiresAt})
	}
	writeJSON(w, http.StatusOK, out)
	return nil
}

// shareRevoke answers DELETE /share/{tokenHash}; a link of another device is
// reported exactly like an unknown one.
func (s *Server) shareRevoke(w http.ResponseWriter, r *http.Request) error {
	svc := s.cfg.Misc.Shares
	if svc == nil {
		return notYet
	}
	caller, err := shareCaller(r)
	if err != nil {
		return err
	}
	events, err := svc.Revoke(caller, r.PathValue("tokenHash"))
	if err != nil {
		return err
	}
	s.recordAudit(events)
	writeNoContent(w)
	return nil
}

// shareFetch answers the public GET /share/{token}: it sends the file once, with
// Range support, and never names a host path. The first request that passes the
// share service's checks uses the link up, so a ranged resume needs a new link.
func (s *Server) shareFetch(w http.ResponseWriter, r *http.Request) error {
	svc := s.cfg.Misc.Shares
	if svc == nil {
		return notYet
	}
	token := r.PathValue("token")
	if token == "" || len(token) > maxShareTokenBytes {
		return &apiError{kind: ErrNotFound, msg: msgShareNotFound}
	}
	d, err := svc.Resolve(token, remoteHost(r))
	if err != nil {
		return err
	}
	defer func() {
		if d.Close == nil {
			return
		}
		if closeErr := d.Close(); closeErr != nil {
			s.logf("api: share: close file: %v", closeErr)
		}
	}()
	if d.RecordErr != nil {
		s.logf("api: share: record serve: %v", d.RecordErr)
	}
	h := w.Header()
	for k, v := range d.Header {
		h[k] = v
	}
	http.ServeContent(w, r, d.Name, d.ModTime, d.Content)
	return nil
}
