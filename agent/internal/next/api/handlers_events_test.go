package api_test

import (
	"bufio"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	evhub "github.com/ZaidQamhieh/rfe/agent/internal/events"
)

// sseStream is an open GET /v1/events connection.
type sseStream struct {
	resp  *http.Response
	lines chan string
}

func openStream(t *testing.T, r *rig, token string) *sseStream {
	t.Helper()
	srv := httptest.NewServer(r.h)
	t.Cleanup(srv.Close)
	req, err := http.NewRequest(http.MethodGet, srv.URL+"/v1/events", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	s := &sseStream{resp: resp, lines: make(chan string, 64)}
	go func() {
		sc := bufio.NewScanner(resp.Body)
		for sc.Scan() {
			s.lines <- sc.Text()
		}
		close(s.lines)
	}()
	return s
}

// next returns the next "event:" kind, skipping comments and other fields.
func (s *sseStream) next(t *testing.T) string {
	t.Helper()
	deadline := time.After(3 * time.Second)
	for {
		select {
		case l, ok := <-s.lines:
			if !ok {
				t.Fatal("stream ended")
			}
			if k, found := strings.CutPrefix(l, "event: "); found {
				return k
			}
		case <-deadline:
			t.Fatal("no event within 3s")
		}
	}
}

func TestEventsStreamSaysHelloThenPublishedKinds(t *testing.T) {
	hub := evhub.NewHub()
	t.Cleanup(hub.Close)
	r := newRig(t, rigOptions{events: hub})
	token, _ := r.pairDevice(newPhone("p1", 1))
	s := openStream(t, r, token)
	if ct := s.resp.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("content type = %q", ct)
	}
	if k := s.next(t); k != evhub.KindHello {
		t.Fatalf("first event = %q, want hello", k)
	}
	hub.Publish(evhub.KindFiles)
	if k := s.next(t); k != evhub.KindFiles {
		t.Fatalf("event = %q, want files", k)
	}
}

func TestEventsNeedAToken(t *testing.T) {
	r := newRig(t, rigOptions{events: evhub.NewHub()})
	r.do(http.MethodGet, "/events", "", nil).wantError(t, http.StatusUnauthorized, "UNAUTHORIZED")
}

func TestEventsKeepAnswering501WithoutAHub(t *testing.T) {
	r := newRig(t, rigOptions{})
	token, _ := r.pairDevice(newPhone("p1", 1))
	r.do(http.MethodGet, "/events", token, nil).wantError(t, http.StatusNotImplemented, "NOT_IMPLEMENTED")
}

func TestEventsAdminOnlyKindsSkipPlainDevices(t *testing.T) {
	hub := evhub.NewHub()
	t.Cleanup(hub.Close)
	r := newRig(t, rigOptions{events: hub})
	plain, _ := r.pairDevice(newPhone("p1", 1))
	admin, _ := r.registerAdmin(newPhone("a1", 2))
	ps, as := openStream(t, r, plain), openStream(t, r, admin)
	ps.next(t)
	as.next(t)
	hub.Publish(evhub.KindDevices)
	hub.Publish(evhub.KindFiles)
	if k := as.next(t); k != evhub.KindDevices {
		t.Fatalf("admin got %q first, want devices", k)
	}
	if k := ps.next(t); k != evhub.KindFiles {
		t.Fatalf("plain device got %q, want files (devices is admin-only)", k)
	}
}

func TestSuccessfulMutationPublishesItsKind(t *testing.T) {
	hub := evhub.NewHub()
	t.Cleanup(hub.Close)
	r := newRig(t, rigOptions{events: hub})
	admin, _ := r.registerAdmin(newPhone("a1", 2))
	_, victim := r.pairDevice(newPhone("p1", 1))
	s := openStream(t, r, admin)
	s.next(t)
	if res := r.do(http.MethodDelete, "/devices/"+victim, admin, nil); res.Code >= 400 {
		t.Fatalf("delete device = %d %s", res.Code, res.Body.String())
	}
	if k := s.next(t); k != evhub.KindDevices {
		t.Fatalf("event = %q, want devices", k)
	}
}

func TestFailedMutationPublishesNothing(t *testing.T) {
	hub := evhub.NewHub()
	t.Cleanup(hub.Close)
	r := newRig(t, rigOptions{events: hub})
	before := hub.Seq()
	r.do(http.MethodDelete, "/devices/nope", "", nil) // 401: no token
	if hub.Seq() != before {
		t.Fatalf("a refused request published an event")
	}
}
