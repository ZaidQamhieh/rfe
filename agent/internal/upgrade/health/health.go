// Package health is the tiny heartbeat protocol between the launcher and the agent it starts. The launcher listens on
// 127.0.0.1 (a random port) and hands the child two environment variables: RFE_HEALTH_URL and a per-run random
// RFE_HEALTH_TOKEN. The agent POSTs {"state":"starting|migrating|ready","seq":N} with the token as a bearer; the
// launcher uses the timing of those beats to decide whether a new version is alive. Plain HTTP is fine because it
// never leaves the loopback interface and the token is single-run. It uses the standard library only (no database,
// no cgo), so both the launcher and the agent import it.
package health

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// Environment variables the launcher sets for the agent it starts.
const (
	EnvURL   = "RFE_HEALTH_URL"
	EnvToken = "RFE_HEALTH_TOKEN"
)

// HeartbeatInterval is how often a started Reporter sends its current state.
const HeartbeatInterval = 5 * time.Second

const (
	healthPath   = "/v1/health"
	maxBeatBytes = 1024
	sendTimeout  = 2 * time.Second
)

// State is what the agent says about itself.
type State string

// The three states an agent reports, in order.
const (
	Starting  State = "starting"
	Migrating State = "migrating"
	Ready     State = "ready"
)

func (s State) valid() bool { return s == Starting || s == Migrating || s == Ready }

// Beat is the JSON body of one heartbeat. Addr is the agent's listen address, when it knows it.
type Beat struct {
	State State  `json:"state"`
	Seq   int64  `json:"seq"`
	Addr  string `json:"addr,omitempty"`
}

// Snapshot is what the Server has seen so far. Times come from the Server's clock.
type Snapshot struct {
	State      State
	Seq        int64
	Beats      int
	LastBeat   time.Time // the latest beat of any state
	LastReady  time.Time // the latest beat whose state was ready
	FirstReady time.Time // the first beat whose state was ready
	Addr       string    // the listen address the agent reported
}

// Server is the launcher's side: a loopback HTTP listener that records heartbeats.
type Server struct {
	ln    net.Listener
	srv   *http.Server
	token string
	now   func() time.Time

	mu   sync.Mutex
	snap Snapshot
}

// Listen starts a Server on 127.0.0.1 with a random port and a fresh random token. now stamps each beat (nil means
// time.Now), so the launcher's clock can be replaced in tests.
func Listen(now func() time.Time) (*Server, error) {
	if now == nil {
		now = time.Now
	}
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		return nil, err
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	s := &Server{ln: ln, token: hex.EncodeToString(raw), now: now}
	mux := http.NewServeMux()
	mux.HandleFunc(healthPath, s.handle)
	s.srv = &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 5 * time.Second}
	go s.srv.Serve(ln) //nolint:errcheck // ErrServerClosed on Close is the normal end
	return s, nil
}

// Env returns the KEY=VALUE entries to add to the child's environment.
func (s *Server) Env() []string {
	return []string{EnvURL + "=http://" + s.ln.Addr().String() + healthPath, EnvToken + "=" + s.token}
}

// Snapshot returns a copy of what has been recorded.
func (s *Server) Snapshot() Snapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.snap
}

// Close stops the listener.
func (s *Server) Close() error { return s.srv.Close() }

// handle checks method, token, size and content of one beat and records it. A stale or repeated seq is refused
// (409), so a replayed request cannot refresh the clock.
func (s *Server) handle(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return
	}
	got := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	if subtle.ConstantTimeCompare([]byte(got), []byte(s.token)) != 1 {
		http.Error(w, "bad token", http.StatusUnauthorized)
		return
	}
	var b Beat
	dec := json.NewDecoder(io.LimitReader(r.Body, maxBeatBytes))
	if err := dec.Decode(&b); err != nil || !b.State.valid() || b.Seq <= 0 || len(b.Addr) > 256 {
		http.Error(w, "bad beat", http.StatusBadRequest)
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if b.Seq <= s.snap.Seq {
		http.Error(w, "stale seq", http.StatusConflict)
		return
	}
	now := s.now()
	s.snap.State, s.snap.Seq, s.snap.LastBeat = b.State, b.Seq, now
	s.snap.Beats++
	if b.Addr != "" {
		s.snap.Addr = b.Addr
	}
	if b.State == Ready {
		s.snap.LastReady = now
		if s.snap.FirstReady.IsZero() {
			s.snap.FirstReady = now
		}
	}
	w.WriteHeader(http.StatusNoContent)
}

// Reporter is the agent's side. A nil *Reporter is valid and does nothing, so callers need no checks when the
// agent was not started by a launcher.
type Reporter struct {
	url, token string
	client     *http.Client

	mu    sync.Mutex
	state State
	addr  string
	seq   int64

	kick chan struct{}
	stop chan struct{}
	done chan struct{}
	once sync.Once
	logf func(format string, args ...any)
}

// FromEnv builds a Reporter from RFE_HEALTH_URL and RFE_HEALTH_TOKEN. With no URL it returns nil, nil (the agent
// runs unchanged under a plain supervisor). A URL that is not plain http to a loopback IP, or a missing token, is an
// error: the agent must not send its token anywhere else. logf receives send failures (nil discards them).
func FromEnv(getenv func(string) string, logf func(string, ...any)) (*Reporter, error) {
	raw := getenv(EnvURL)
	if raw == "" {
		return nil, nil
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "http" {
		return nil, fmt.Errorf("%s must be an http URL on the loopback interface", EnvURL)
	}
	if ip := net.ParseIP(u.Hostname()); ip == nil || !ip.IsLoopback() {
		return nil, fmt.Errorf("%s must name a loopback IP, not %q", EnvURL, u.Hostname())
	}
	tok := getenv(EnvToken)
	if tok == "" {
		return nil, fmt.Errorf("%s is set but %s is empty", EnvURL, EnvToken)
	}
	return &Reporter{url: raw, token: tok, client: &http.Client{Timeout: sendTimeout}, state: Starting,
		kick: make(chan struct{}, 1), stop: make(chan struct{}), done: make(chan struct{}), logf: logf}, nil
}

// SetState changes the state to report and sends it at once (when Start was called).
func (r *Reporter) SetState(s State) {
	if r == nil {
		return
	}
	r.mu.Lock()
	r.state = s
	r.mu.Unlock()
	select {
	case r.kick <- struct{}{}:
	default:
	}
}

// SetAddr records the listen address included in later beats.
func (r *Reporter) SetAddr(addr string) {
	if r == nil {
		return
	}
	r.mu.Lock()
	r.addr = addr
	r.mu.Unlock()
}

// Start sends the current state now and then every interval (HeartbeatInterval when zero) until Stop.
func (r *Reporter) Start(interval time.Duration) {
	if r == nil {
		return
	}
	if interval <= 0 {
		interval = HeartbeatInterval
	}
	go func() {
		defer close(r.done)
		t := time.NewTicker(interval)
		defer t.Stop()
		warned := false
		for {
			if err := r.send(context.Background()); err != nil && !warned && r.logf != nil {
				warned = true
				r.logf("health heartbeat failed: %v", err)
			}
			select {
			case <-r.stop:
				return
			case <-t.C:
			case <-r.kick:
			}
		}
	}()
}

// Stop ends the heartbeat loop and waits for it.
func (r *Reporter) Stop() {
	if r == nil {
		return
	}
	r.once.Do(func() { close(r.stop) })
	select {
	case <-r.done:
	case <-time.After(2 * sendTimeout):
	}
}

// send posts one beat with the next sequence number.
func (r *Reporter) send(ctx context.Context) error {
	r.mu.Lock()
	r.seq++
	b := Beat{State: r.state, Seq: r.seq, Addr: r.addr}
	r.mu.Unlock()
	body, err := json.Marshal(b)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, r.url, strings.NewReader(string(body)))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+r.token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := r.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, io.LimitReader(resp.Body, maxBeatBytes))
	if resp.StatusCode != http.StatusNoContent {
		return errors.New("launcher answered " + resp.Status)
	}
	return nil
}
