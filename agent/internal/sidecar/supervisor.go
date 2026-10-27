package sidecar

import (
	"context"
	"fmt"
	"sync"
	"time"
)

const (
	minBackoff = time.Second
	maxBackoff = time.Minute
	// A sidecar that stays up this long is considered healthy again and resets the backoff.
	healthyAfter = 30 * time.Second
	// This many consecutive failed starts open the breaker: the agent stays on its Go fallback until the next
	// retry window.
	breakerFailures = 5
	pingInterval    = 15 * time.Second
	pingTimeout     = 5 * time.Second
)

// Supervisor keeps one sidecar running: it restarts it with exponential backoff after a crash or a failed
// health ping and reports unavailability instead of blocking callers.
type Supervisor struct {
	cfg     Config
	onReady func(*Client)

	mu     sync.Mutex
	client *Client

	// timings are fields so tests can shrink them.
	minBackoff, maxBackoff, healthyAfter, pingEvery time.Duration

	stats supStats
	stop  chan struct{}
	done  chan struct{}
	run   sync.Once // set by Run; Stop closes done itself when Run never started the loop
}

// NewSupervisor creates a supervisor; Run starts it. onReady, when set, runs (in the supervisor goroutine's
// shadow, not blocking restarts) each time a sidecar completes its handshake.
func NewSupervisor(cfg Config, onReady func(*Client)) *Supervisor {
	s := &Supervisor{
		cfg: cfg, onReady: onReady,
		minBackoff: minBackoff, maxBackoff: maxBackoff, healthyAfter: healthyAfter, pingEvery: pingInterval,
		stop: make(chan struct{}), done: make(chan struct{}),
	}
	register(s)
	return s
}

// Run supervises until Stop. It returns immediately; work happens in a goroutine.
func (s *Supervisor) Run() {
	s.run.Do(func() { go s.loop() })
}

// Client returns the live sidecar, or ErrUnavailable.
func (s *Supervisor) Client() (*Client, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.client == nil {
		return nil, s.unavailable()
	}
	select {
	case <-s.client.Done():
		return nil, s.unavailable()
	default:
	}
	return s.client, nil
}

func (s *Supervisor) unavailable() error {
	if s.stats.breakerOpen.Load() {
		return fmt.Errorf("%w: %w", ErrUnavailable, ErrBreakerOpen)
	}
	return ErrUnavailable
}

// Call runs op on the live sidecar.
func (s *Supervisor) Call(ctx context.Context, op string, req, resp any) error {
	c, err := s.Client()
	if err != nil {
		return err
	}
	return c.Call(ctx, op, req, resp)
}

// CallBody runs op with a binary body on the live sidecar and returns the response body.
func (s *Supervisor) CallBody(ctx context.Context, op string, req any, body []byte, resp any) ([]byte, error) {
	c, err := s.Client()
	if err != nil {
		return nil, err
	}
	return c.CallBody(ctx, op, req, body, resp)
}

func (s *Supervisor) setClient(c *Client) {
	s.mu.Lock()
	s.client = c
	s.mu.Unlock()
}

func (s *Supervisor) sleep(d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return true
	case <-s.stop:
		return false
	}
}

func (s *Supervisor) loop() {
	defer close(s.done)
	backoff := s.minBackoff
	failures := 0
	for {
		select {
		case <-s.stop:
			return
		default:
		}
		started := time.Now()
		c, err := Start(s.cfg)
		if err != nil {
			failures++
			s.stats.startFailures.Add(1)
			if failures == breakerFailures {
				s.stats.breakerOpens.Add(1)
				s.stats.breakerOpen.Store(true)
				Note(s.cfg.Name + ".breaker-open")
			}
			s.cfg.logf("sidecar %s: start failed (%d): %v", s.cfg.Name, failures, err)
			wait := backoff
			if failures >= breakerFailures {
				wait = s.maxBackoff // breaker open: stay on the fallback until the next long retry
			}
			if !s.sleep(wait) {
				return
			}
			backoff = min(backoff*2, s.maxBackoff)
			continue
		}
		s.stats.breakerOpen.Store(false)
		s.stats.starts.Add(1)
		s.setClient(c)
		s.cfg.logf("sidecar %s: ready (version %s)", s.cfg.Name, c.Version())
		if s.onReady != nil {
			go s.onReady(c)
		}
		s.watch(c)
		s.setClient(nil)
		_ = c.Close()
		select {
		case <-s.stop:
			return
		default:
		}
		s.stats.crashes.Add(1)
		Note(s.cfg.Name + ".crash")
		if time.Since(started) >= s.healthyAfter {
			backoff = s.minBackoff
			failures = 0 // only a run that stayed up clears the count; a start that dies at once does not
		}
		s.cfg.logf("sidecar %s: stopped, restarting in %v", s.cfg.Name, backoff)
		if !s.sleep(backoff) {
			return
		}
		backoff = min(backoff*2, s.maxBackoff)
	}
}

// watch blocks until the client dies, a ping fails, or Stop is called.
func (s *Supervisor) watch(c *Client) {
	t := time.NewTicker(s.pingEvery)
	defer t.Stop()
	for {
		select {
		case <-c.Done():
			return
		case <-s.stop:
			return
		case <-t.C:
			ctx, cancel := context.WithTimeout(context.Background(), pingTimeout)
			err := c.Call(ctx, "ping", struct{}{}, nil)
			cancel()
			if err != nil {
				s.cfg.logf("sidecar %s: ping failed: %v", s.cfg.Name, err)
				return
			}
		}
	}
}

// Stop ends supervision and the running sidecar.
func (s *Supervisor) Stop() {
	select {
	case <-s.stop:
	default:
		close(s.stop)
	}
	s.run.Do(func() { close(s.done) }) // Run was never called: there is no loop to wait for
	<-s.done
	unregister(s)
	s.mu.Lock()
	c := s.client
	s.client = nil
	s.mu.Unlock()
	if c != nil {
		_ = c.Close()
	}
}
