package server

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/ZaidQamhieh/rfe/agent/internal/events"
	"github.com/ZaidQamhieh/rfe/agent/internal/store"
)

// heartbeatEvery is how often an idle stream gets a comment line, so proxies and
// phones do not drop a quiet connection. A variable so tests can shorten it.
var heartbeatEvery = 20 * time.Second

// sseWriteTimeout bounds one write to a stream; a client that stops reading
// longer than this is disconnected instead of holding the connection forever.
const sseWriteTimeout = 30 * time.Second

// eventsHandler serves GET /v1/events as a Server-Sent Events stream. It sends a
// "hello" event at once, then one event per changed kind (never a path or file
// content) and a comment heartbeat while idle. It ends when the client leaves,
// a write fails or stalls, or the hub is closed. Admin-only kinds reach only
// admin devices. Must run behind authMiddleware.
func eventsHandler(hub *events.Hub, db *store.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		dev := deviceFromContext(r)
		admin := isAdminDevice(dev)
		owner := ""
		if dev != nil {
			owner = dev.ID
		}
		sub := hub.SubscribeAs(admin, owner)
		if sub == nil {
			w.Header().Set("Retry-After", "30")
			writeError(w, http.StatusServiceUnavailable, "TOO_MANY_STREAMS", "too many event streams are open")
			return
		}
		defer hub.Unsubscribe(sub)

		rc := http.NewResponseController(w)
		// The server's read and write timeouts are sized for file transfers and
		// would cut a long-lived stream; clear the read one, and re-arm the write
		// one around each write below. A writer without deadline support (a test
		// recorder) is fine.
		if err := rc.SetReadDeadline(time.Time{}); err != nil && !errors.Is(err, http.ErrNotSupported) {
			writeInternal(w, "events read deadline", err)
			return
		}
		h := w.Header()
		h.Set("Content-Type", "text/event-stream")
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
			return
		}
		tick := time.NewTicker(heartbeatEvery)
		defer tick.Stop()
		for {
			select {
			case <-r.Context().Done():
				return
			case <-hub.Done():
				return
			case <-tick.C:
				// The stream was authorised once at connect: end it when the
				// device was revoked or its admin status changed since.
				if db != nil {
					if tok := requestToken(r); tok != "" {
						if d, err := db.DeviceByToken(tok); err == nil && (d == nil || d.Revoked || isAdminDevice(d) != admin) {
							return
						}
					}
				}
				if !send(": ping\n\n") {
					return
				}
			case <-sub.Wake():
				for _, kind := range sub.Take() {
					if !send(frame(kind)) {
						return
					}
				}
			}
		}
	}
}

// requestToken is the bearer token or web-session cookie the request was
// authenticated with ("" when neither is present).
func requestToken(r *http.Request) string {
	if hdr := r.Header.Get("Authorization"); hdr != "" {
		if parts := strings.SplitN(hdr, " ", 2); len(parts) == 2 && strings.EqualFold(parts[0], "Bearer") {
			return strings.TrimSpace(parts[1])
		}
		return ""
	}
	if c, err := r.Cookie(webSessionCookie); err == nil {
		return c.Value
	}
	return ""
}

// eventPublishers maps a mutating route ("METHOD /v1/pattern", as chi reports
// it) to the event kinds a successful call announces. Reads are never listed.
var eventPublishers = map[string][]string{
	"POST /v1/pair":                       {events.KindDevices},
	"POST /v1/pair/request":               {events.KindDevices},
	"POST /v1/register":                   {events.KindDevices},
	"POST /v1/login":                      {events.KindDevices},
	"POST /v1/auth/logout":                {events.KindDevices},
	"PATCH /v1/devices/{id}":              {events.KindDevices},
	"DELETE /v1/devices/{id}":             {events.KindDevices},
	"POST /v1/pair/requests/{id}/approve": {events.KindDevices},
	"POST /v1/pair/requests/{id}/reject":  {events.KindDevices},
	"DELETE /v1/users/{username}":         {events.KindDevices},
	"PATCH /v1/settings":                  {events.KindSettings},
	"PUT /v1/settings/bandwidth":          {events.KindSettings},
	"PUT /v1/settings/account-server":     {events.KindSettings},
	"DELETE /v1/settings/account-server":  {events.KindSettings},
	"POST /v1/transfers":                  {events.KindTransfers},
	"PUT /v1/transfers/{id}/chunks/{n}":   {events.KindTransfers},
	"POST /v1/transfers/{id}/complete":    {events.KindTransfers, events.KindFiles},
	"DELETE /v1/transfers/{id}":           {events.KindTransfers},
	"DELETE /v1/fs":                       {events.KindFiles},
	"POST /v1/fs/folder":                  {events.KindFiles},
	"POST /v1/fs/file":                    {events.KindFiles},
	"PATCH /v1/fs/rename":                 {events.KindFiles},
	"POST /v1/fs/copy":                    {events.KindFiles},
	"POST /v1/fs/move":                    {events.KindFiles},
	"POST /v1/fs/compress":                {events.KindFiles},
	"POST /v1/fs/extract":                 {events.KindFiles},
	"POST /v1/fs/chmod":                   {events.KindFiles},
	"POST /v1/trash/restore":              {events.KindFiles},
	"DELETE /v1/trash":                    {events.KindFiles},
	"PUT /v1/content":                     {events.KindFiles},
}

// publishOnMutation announces, after a request succeeded (status below 400),
// the event kinds eventPublishers lists for the route that served it. Safe
// methods pass through unwrapped so the event stream itself keeps its flusher.
func publishOnMutation(hub *events.Hub) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodGet || r.Method == http.MethodHead || r.Method == http.MethodOptions {
				next.ServeHTTP(w, r)
				return
			}
			ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
			next.ServeHTTP(ww, r)
			if status := ww.Status(); status >= http.StatusBadRequest {
				return
			}
			rctx := chi.RouteContext(r.Context())
			if rctx == nil {
				return
			}
			pattern := strings.TrimSuffix(rctx.RoutePattern(), "/")
			for _, kind := range eventPublishers[r.Method+" "+pattern] {
				hub.Publish(kind)
			}
		})
	}
}
