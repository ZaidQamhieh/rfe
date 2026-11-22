package api

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/ZaidQamhieh/rfe/agent/internal/events"
	"github.com/ZaidQamhieh/rfe/agent/internal/next/pairing"
)

// heartbeatEvery is how often an idle stream gets a comment line, so proxies and
// phones do not drop a quiet connection.
const heartbeatEvery = 20 * time.Second

// sseWriteTimeout bounds one write to a stream; a client that stops reading
// longer than this is disconnected instead of holding the connection forever.
const sseWriteTimeout = 30 * time.Second

var errTooManyStreams = &apiError{kind: ErrNotAvailable, msg: "too many event streams are open"}

// eventsStream serves GET /v1/events as a Server-Sent Events stream: a "hello"
// event at once, then one event per changed kind (never a path or content) and
// a comment heartbeat while idle. It ends when the client leaves, a write fails
// or stalls, the hub closes, or the device is revoked. Admin-only kinds reach
// only admin devices. Without a hub the route answers 501 like any unported one.
func (s *Server) eventsStream(w http.ResponseWriter, r *http.Request) error {
	hub := s.cfg.Events
	if hub == nil {
		return notYet
	}
	c, err := callOf(r)
	if err != nil {
		return err
	}
	admin := c.principal.Admin
	sub := hub.SubscribeAs(admin, c.principal.DeviceID)
	if sub == nil {
		w.Header().Set(headerRetryAfter, "30")
		return errTooManyStreams
	}
	defer hub.Unsubscribe(sub)

	rc := http.NewResponseController(w)
	// The server's timeouts are sized for file transfers and would cut a long
	// stream: clear the read one and re-arm the write one around each write.
	if err := rc.SetReadDeadline(time.Time{}); err != nil && !errors.Is(err, http.ErrNotSupported) {
		return err
	}
	h := w.Header()
	h.Set(headerContentType, "text/event-stream")
	h.Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)

	send := func(text string) bool {
		if err := rc.SetWriteDeadline(time.Now().Add(sseWriteTimeout)); err != nil && !errors.Is(err, http.ErrNotSupported) {
			return false
		}
		if _, err := io.WriteString(w, text); err != nil {
			return false
		}
		return rc.Flush() == nil
	}
	frame := func(kind string) string {
		seq := hub.Seq()
		return fmt.Sprintf("id: %d\nevent: %s\ndata: {\"kind\":\"%s\",\"seq\":%d}\n\n", seq, kind, kind, seq)
	}
	if !send(frame(events.KindHello)) {
		return nil
	}
	token := pairing.BearerToken(r.Header.Get(headerAuthorization))
	tick := time.NewTicker(heartbeatEvery)
	defer tick.Stop()
	for {
		select {
		case <-r.Context().Done():
			return nil
		case <-hub.Done():
			return nil
		case <-tick.C:
			// Authorised once at connect: end the stream when the device was
			// revoked or its admin status changed since.
			if d, err := s.cfg.Pairing.Authenticate(token); err != nil || d == nil || pairing.PrincipalOf(d).Admin != admin {
				return nil
			}
			if !send(": ping\n\n") {
				return nil
			}
		case <-sub.Wake():
			for _, kind := range sub.Take() {
				if !send(frame(kind)) {
					return nil
				}
			}
		}
	}
}

// eventPublishers maps a mutating route to the event kinds a successful call
// announces. Reads are never listed.
var eventPublishers = map[string][]string{
	routeKey(post, "/pair"):                       {events.KindDevices},
	routeKey(post, "/pair/request"):               {events.KindDevices},
	routeKey(post, "/register"):                   {events.KindDevices},
	routeKey(post, "/login"):                      {events.KindDevices},
	routeKey(post, "/auth/logout"):                {events.KindDevices},
	routeKey(patch, "/devices/{id}"):              {events.KindDevices},
	routeKey(del, "/devices/{id}"):                {events.KindDevices},
	routeKey(post, "/pair/requests/{id}/approve"): {events.KindDevices},
	routeKey(post, "/pair/requests/{id}/reject"):  {events.KindDevices},
	routeKey(del, "/users/{username}"):            {events.KindDevices},
	routeKey(patch, "/settings"):                  {events.KindSettings},
	routeKey(put, "/settings/bandwidth"):          {events.KindSettings},
	routeKey(post, "/transfers"):                  {events.KindTransfers},
	routeKey(put, "/transfers/{id}/chunks/{n}"):   {events.KindTransfers},
	routeKey(post, "/transfers/{id}/complete"):    {events.KindTransfers, events.KindFiles},
	routeKey(del, "/transfers/{id}"):              {events.KindTransfers},
	routeKey(del, "/fs"):                          {events.KindFiles},
	routeKey(post, "/fs/folder"):                  {events.KindFiles},
	routeKey(post, "/fs/file"):                    {events.KindFiles},
	routeKey(patch, "/fs/rename"):                 {events.KindFiles},
	routeKey(post, "/fs/copy"):                    {events.KindFiles},
	routeKey(post, "/fs/move"):                    {events.KindFiles},
	routeKey(post, "/fs/compress"):                {events.KindFiles},
	routeKey(post, "/fs/extract"):                 {events.KindFiles},
	routeKey(post, "/fs/chmod"):                   {events.KindFiles},
	routeKey(post, "/trash/restore"):              {events.KindFiles},
	routeKey(del, "/trash"):                       {events.KindFiles},
	routeKey(put, "/content"):                     {events.KindFiles},
}

// publishAfter announces the kinds of a mutating route once its handler
// succeeded: it returned no error and did not itself answer with a 4xx or 5xx.
func (s *Server) publishAfter(rt Route, w http.ResponseWriter) {
	if s.cfg.Events == nil {
		return
	}
	kinds := eventPublishers[routeKey(rt.Method, rt.Pattern)]
	if len(kinds) == 0 {
		return
	}
	if sw, ok := w.(*statusWriter); ok && sw.status >= http.StatusBadRequest {
		return
	}
	for _, k := range kinds {
		s.cfg.Events.Publish(k)
	}
}
