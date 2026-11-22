package health

import (
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

type fakeNow struct {
	mu sync.Mutex
	t  time.Time
}

func (f *fakeNow) Now() time.Time { f.mu.Lock(); defer f.mu.Unlock(); return f.t }

func envMap(s *Server) func(string) string {
	m := map[string]string{}
	for _, kv := range s.Env() {
		k, v, _ := strings.Cut(kv, "=")
		m[k] = v
	}
	return func(k string) string { return m[k] }
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	for i := 0; i < 400; i++ {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("condition not reached")
}

func TestReporterBeatsReachServerWithServerClock(t *testing.T) {
	clk := &fakeNow{t: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	s, err := Listen(clk.Now)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	r, err := FromEnv(envMap(s), nil)
	if err != nil || r == nil {
		t.Fatalf("FromEnv: %v %v", r, err)
	}
	r.SetAddr(":8765")
	r.Start(time.Hour)
	defer r.Stop()
	waitFor(t, func() bool { return s.Snapshot().State == Starting })
	r.SetState(Migrating)
	waitFor(t, func() bool { return s.Snapshot().State == Migrating })
	if !s.Snapshot().FirstReady.IsZero() {
		t.Fatal("ready recorded before a ready beat")
	}
	r.SetState(Ready)
	waitFor(t, func() bool { return s.Snapshot().State == Ready })
	sn := s.Snapshot()
	if sn.Addr != ":8765" || sn.FirstReady != clk.t || sn.LastReady != clk.t || sn.Seq < 3 {
		t.Fatalf("snapshot %+v", sn)
	}
}

func TestServerRefusesBadRequests(t *testing.T) {
	s, err := Listen(nil)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	get := envMap(s)
	post := func(auth, body string) int {
		req, _ := http.NewRequest(http.MethodPost, get(EnvURL), strings.NewReader(body))
		if auth != "" {
			req.Header.Set("Authorization", auth)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp.StatusCode
	}
	good := "Bearer " + get(EnvToken)
	cases := []struct {
		name, auth, body string
		want             int
	}{
		{"no token", "", `{"state":"ready","seq":1}`, 401},
		{"wrong token", "Bearer nope", `{"state":"ready","seq":1}`, 401},
		{"unknown state", good, `{"state":"great","seq":1}`, 400},
		{"zero seq", good, `{"state":"ready","seq":0}`, 400},
		{"not json", good, `ready`, 400},
		{"ok", good, `{"state":"ready","seq":5}`, 204},
		{"replayed seq", good, `{"state":"ready","seq":5}`, 409},
		{"older seq", good, `{"state":"ready","seq":2}`, 409},
	}
	for _, c := range cases {
		if got := post(c.auth, c.body); got != c.want {
			t.Errorf("%s: status %d, want %d", c.name, got, c.want)
		}
	}
	resp, err := http.Get(get(EnvURL))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("GET status %d", resp.StatusCode)
	}
	if sn := s.Snapshot(); sn.Beats != 1 || sn.Seq != 5 {
		t.Errorf("only the one good beat may be recorded: %+v", sn)
	}
}

func TestFromEnvAbsentIsNilAndNilReporterIsSafe(t *testing.T) {
	r, err := FromEnv(func(string) string { return "" }, nil)
	if r != nil || err != nil {
		t.Fatalf("got %v %v", r, err)
	}
	r.SetState(Ready)
	r.SetAddr("x")
	r.Start(0)
	r.Stop()
}

func TestFromEnvRefusesNonLoopbackAndMissingToken(t *testing.T) {
	for name, env := range map[string]map[string]string{
		"public ip":   {EnvURL: "http://192.0.2.1:9/v1/health", EnvToken: "t"},
		"host name":   {EnvURL: "http://example.com/v1/health", EnvToken: "t"},
		"https":       {EnvURL: "https://127.0.0.1:9/v1/health", EnvToken: "t"},
		"no token":    {EnvURL: "http://127.0.0.1:9/v1/health"},
		"garbage url": {EnvURL: "::::"},
	} {
		if r, err := FromEnv(func(k string) string { return env[k] }, nil); err == nil || r != nil {
			t.Errorf("%s: accepted (%v, %v)", name, r, err)
		}
	}
}
