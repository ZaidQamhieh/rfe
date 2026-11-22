package launcher

import (
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/ZaidQamhieh/rfe/agent/internal/upgrade/health"
)

// Defaults for the health watcher; every one can be set in Config.
const (
	DefaultStartTimeout      = 120 * time.Second // no heartbeat before ready for this long: the new version failed
	DefaultProbation         = 10 * time.Minute  // a ready version must stay healthy this long to be promoted
	DefaultHealthGap         = 10 * time.Second  // during probation a ready beat must arrive at least this often
	DefaultPollInterval      = time.Second       // how often the watcher looks at the clock
	DefaultProbeInterval     = 10 * time.Second  // how often the launcher dials the agent's TLS port
	probeDialTimeout         = 5 * time.Second
	maxConsecutiveProbeFails = 3
)

// verdict is a decision of the watcher, applied by Run's goroutine (which owns every state write).
type verdict struct {
	kind        verdictKind
	reason      string
	fingerprint string
}

type verdictKind int

const (
	verdictFail        verdictKind = iota + 1 // kill the child and fail the pending version
	verdictPromote                            // probation passed
	verdictFingerprint                        // record the agent's certificate fingerprint
)

// monitor watches one child. For a pending version it enforces the startup timeout, the probation (health gaps and
// certificate pinning) and decides promotion; for a current version it only learns the certificate fingerprint when
// none is recorded yet.
type monitor struct {
	cfg      *Config
	srv      *health.Server
	pending  bool
	expected string // fingerprint expected from the agent; "" until learned
	events   chan verdict
	stop     chan struct{}
	wg       sync.WaitGroup
}

// newMonitor starts the watcher goroutine; call close to end it.
func newMonitor(cfg *Config, srv *health.Server, pending bool, expected string) *monitor {
	m := &monitor{cfg: cfg, srv: srv, pending: pending, expected: expected, events: make(chan verdict, 8), stop: make(chan struct{})}
	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		m.loop()
	}()
	return m
}

func (m *monitor) close() {
	close(m.stop)
	m.wg.Wait()
}

// emit sends a verdict unless the monitor is being closed.
func (m *monitor) emit(v verdict) bool {
	select {
	case m.events <- v:
		return true
	case <-m.stop:
		return false
	}
}

// loop ticks on the configured clock until a final verdict or close.
func (m *monitor) loop() {
	clk := m.cfg.Clock
	start := clk.Now()
	var lastProbe time.Time
	probeFails := 0
	learned := m.expected != ""
	for {
		select {
		case <-m.stop:
			return
		case <-clk.After(m.cfg.PollInterval):
		}
		now := clk.Now()
		sn := m.srv.Snapshot()
		if sn.FirstReady.IsZero() { // still starting
			if m.pending {
				ref := start
				if sn.LastBeat.After(ref) {
					ref = sn.LastBeat
				}
				if now.Sub(ref) > m.cfg.StartTimeout {
					m.emit(verdict{kind: verdictFail, reason: fmt.Sprintf("no heartbeat for %s before the agent reported ready", m.cfg.StartTimeout)})
					return
				}
			}
			continue
		}
		if m.pending && now.Sub(sn.LastReady) > m.cfg.HealthGap {
			m.emit(verdict{kind: verdictFail, reason: fmt.Sprintf("no ready heartbeat for %s during probation", m.cfg.HealthGap)})
			return
		}
		addr := probeTarget(m.cfg.ProbeAddr, sn.Addr)
		if addr != "" && (lastProbe.IsZero() || now.Sub(lastProbe) >= m.cfg.ProbeInterval) && (m.pending || !learned) {
			lastProbe = now
			fp, err := m.cfg.ProbeFingerprint(addr)
			switch {
			case err != nil:
				probeFails++
				m.cfg.logf("certificate probe of %s failed (%d): %v", addr, probeFails, err)
				if m.pending && probeFails >= maxConsecutiveProbeFails {
					m.emit(verdict{kind: verdictFail, reason: fmt.Sprintf("agent not reachable on %s: %v", addr, err)})
					return
				}
			case !learned:
				probeFails = 0
				learned, m.expected = true, fp
				if !m.emit(verdict{kind: verdictFingerprint, fingerprint: fp}) {
					return
				}
			case fp != m.expected:
				if m.pending {
					m.emit(verdict{kind: verdictFail, reason: fmt.Sprintf("certificate fingerprint changed: expected %s, agent presents %s", m.expected, fp)})
					return
				}
				m.cfg.logf("current version presents a different certificate than recorded (expected %s, got %s)", m.expected, fp)
			default:
				probeFails = 0
			}
		}
		if !m.pending {
			if learned {
				return
			}
			continue
		}
		if now.Sub(sn.FirstReady) >= m.cfg.Probation && (learned || addr == "") {
			if addr == "" {
				m.cfg.logf("no agent address known: promoting without a certificate check")
			}
			m.emit(verdict{kind: verdictPromote})
			return
		}
	}
}

// probeTarget returns the address to dial: the configured one, else the one the agent reported. A wildcard or empty
// host becomes 127.0.0.1, because the launcher runs on the agent's own machine.
func probeTarget(configured, reported string) string {
	addr := configured
	if addr == "" {
		addr = reported
	}
	if addr == "" {
		return ""
	}
	host, port, err := net.SplitHostPort(addr)
	if err != nil || port == "" {
		return ""
	}
	if ip := net.ParseIP(host); host == "" || (ip != nil && ip.IsUnspecified()) {
		host = "127.0.0.1"
	}
	return net.JoinHostPort(host, port)
}

// ErrNoCertificate means the TLS handshake finished without the agent presenting a certificate.
var ErrNoCertificate = errors.New("agent presented no certificate")

// TLSFingerprint dials addr over TLS and returns the lowercase hex SHA-256 of the leaf certificate it presents.
// Chain verification is switched off on purpose: the agent's certificate is self-signed and trusted only by this
// pinned fingerprint, which the caller compares. Nothing is sent over the connection.
func TLSFingerprint(addr string) (string, error) {
	d := &net.Dialer{Timeout: probeDialTimeout}
	conn, err := tls.DialWithDialer(d, "tcp", addr, &tls.Config{InsecureSkipVerify: true, MinVersion: tls.VersionTLS12}) //nolint:gosec // pinned by fingerprint
	if err != nil {
		return "", err
	}
	defer conn.Close()
	certs := conn.ConnectionState().PeerCertificates
	if len(certs) == 0 {
		return "", ErrNoCertificate
	}
	sum := sha256.Sum256(certs[0].Raw)
	return strings.ToLower(hex.EncodeToString(sum[:])), nil
}
