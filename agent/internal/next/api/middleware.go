package api

import (
	"context"
	"errors"
	"net"
	"net/http"
	"runtime/debug"
	"strings"

	"github.com/ZaidQamhieh/rfe/agent/internal/next/authz"
	"github.com/ZaidQamhieh/rfe/agent/internal/next/pairing"
	"github.com/ZaidQamhieh/rfe/agent/internal/next/store"
)

// Header names and values the server sets or reads.
const (
	headerAuthorization = "Authorization"
	headerClientVersion = "X-RFE-Client-Version"
	headerCacheControl  = "Cache-Control"
	headerContentType   = "Content-Type"
	headerRetryAfter    = "Retry-After"
	headerNoSniff       = "X-Content-Type-Options"
	headerAllow         = "Allow"
	headerReferrer      = "Referrer-Policy"

	referrerValue = "no-referrer"

	cacheNoStore  = "no-store"
	contentJSON   = "application/json"
	noSniffValue  = "nosniff"
	msgNoRoute    = "no such route"
	msgBadMethod  = "method not allowed"
	msgPanicInner = "panic"
)

// Methods the router probes to tell a wrong method (405) from a wrong path (404).
var probeMethods = []string{
	http.MethodGet, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete,
}

// call is what the guard learned about an authenticated request. It travels in
// the request context.
type call struct {
	principal authz.Principal
	device    *store.Device
	policy    authz.Policy
}

type callKey struct{}

// PrincipalFrom returns the authenticated caller the middleware put in ctx. It
// reports false on a route that needs no credential.
func PrincipalFrom(ctx context.Context) (authz.Principal, bool) {
	c, ok := ctx.Value(callKey{}).(*call)
	if !ok {
		return authz.Principal{}, false
	}
	return c.principal, true
}

// callOf returns the call of a request that passed the guard of a device or
// admin route. A handler of such a route always has one; a missing call is an
// internal error (the table and the handler disagree), never a free pass.
func callOf(r *http.Request) (*call, error) {
	c, ok := r.Context().Value(callKey{}).(*call)
	if !ok {
		return nil, errors.New("api: handler reached without an authenticated caller")
	}
	return c, nil
}

// statusWriter records whether and how a response was started, so recovery
// knows if it may still send an error and the log can show the status.
type statusWriter struct {
	http.ResponseWriter
	status int
}

// WriteHeader records the status and passes it on.
func (w *statusWriter) WriteHeader(code int) {
	if w.status == 0 {
		w.status = code
	}
	w.ResponseWriter.WriteHeader(code)
}

// Write marks the response as started (status 200 when none was set).
func (w *statusWriter) Write(b []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	return w.ResponseWriter.Write(b)
}

// Unwrap lets http.ResponseController reach the real writer.
func (w *statusWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

// ServeHTTP is the outermost layer: it recovers panics, sets the headers every
// answer carries, bounds the body, sends unknown routes to the contract error
// body and hands everything else to the router.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	sw := &statusWriter{ResponseWriter: w}
	start := s.clock.Now()
	defer func() {
		if v := recover(); v != nil {
			if v == http.ErrAbortHandler {
				panic(v)
			}
			s.logf("api: %s %s: %s: %v\n%s", r.Method, r.Pattern, msgPanicInner, v, debug.Stack())
			if sw.status == 0 {
				s.writeError(sw, r, errors.New(msgPanicInner))
			}
		}
		s.logf("api: %s %s -> %d in %s", r.Method, r.Pattern, sw.status, s.clock.Now().Sub(start))
	}()
	h := sw.Header()
	h.Set(headerCacheControl, cacheNoStore)
	h.Set(headerNoSniff, noSniffValue)
	h.Set(headerReferrer, referrerValue)
	_, pattern := s.mux.Handler(r)
	if r.Body != nil && !ownsBodyLimit(pattern) { // a file-data route bounds its own body
		r.Body = http.MaxBytesReader(sw, r.Body, s.maxBody)
	}
	if pattern == "" {
		s.unmatched(sw, r)
		return
	}
	s.mux.ServeHTTP(sw, r)
}

// unmatched answers a request no route took: 405 with an Allow header when the
// path exists under another method, otherwise 404, both in the contract body.
func (s *Server) unmatched(w http.ResponseWriter, r *http.Request) {
	var allowed []string
	for _, m := range probeMethods {
		probe := r.Clone(r.Context())
		probe.Method = m
		if _, p := s.mux.Handler(probe); p != "" {
			allowed = append(allowed, m)
		}
	}
	if len(allowed) == 0 {
		writeJSON(w, http.StatusNotFound, ErrorBody{Code: CodeNotFound, Message: msgNoRoute})
		return
	}
	w.Header().Set(headerAllow, strings.Join(allowed, ", "))
	writeJSON(w, http.StatusMethodNotAllowed, ErrorBody{Code: CodeBadRequest, Message: msgBadMethod})
}

// route wraps a handler for one table row: the guard runs first (token, tier,
// grants, read-only), then the handler, and any error goes through Map.
func (s *Server) route(rt Route, h handlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx, err := s.guard(rt, r)
		if err == nil {
			err = h(w, r.WithContext(ctx))
			if err == nil {
				s.publishAfter(rt, w)
			}
		}
		if err != nil {
			s.writeError(w, r, err)
		}
	})
}

// guard authenticates and authorizes a request for its route. A public route
// passes untouched. Otherwise the bearer token must name a live device, the
// admin tier needs an admin device, and authz.Decide must allow the route's
// action. On success the returned context carries the caller.
func (s *Server) guard(rt Route, r *http.Request) (context.Context, error) {
	if rt.Tier == TierPublic {
		return r.Context(), nil
	}
	dev, err := s.cfg.Pairing.Authenticate(pairing.BearerToken(r.Header.Get(headerAuthorization)))
	if err != nil {
		return nil, err
	}
	set := s.cfg.Policy.Get()
	c := &call{
		principal: pairing.PrincipalOf(dev),
		device:    dev,
		policy:    authz.Policy{ReadOnly: set.ReadOnly, Roots: set.Roots},
	}
	target := authz.Target{DeviceID: r.PathValue("id")}
	if rt.Tier == TierAdmin {
		if d := authz.Decide(c.principal, authz.ActionAdmin, target, c.policy); !d.Allowed {
			return nil, d.Err
		}
	}
	if !rt.HandlerDecides() {
		if d := authz.Decide(c.principal, rt.Action, target, c.policy); !d.Allowed {
			return nil, d.Err
		}
	}
	host := remoteHost(r)
	if err := s.cfg.Pairing.Touch(dev.ID, host, r.Header.Get(headerClientVersion)); err != nil {
		s.logf("api: record device use: %v", err) // a bookkeeping failure must not refuse the request
	}
	return context.WithValue(r.Context(), callKey{}, c), nil
}

// remoteHost is the caller's address without the port, or the raw value when it
// has none. Forwarding headers are not trusted: the agent is reached directly.
func remoteHost(r *http.Request) string {
	if h, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return h
	}
	return r.RemoteAddr
}
