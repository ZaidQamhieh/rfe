// Package system is the health, status and metrics service of agent-next.
//
// It is a plain Service with no HTTP: the API layer decides who may call which
// method (Health is public, Status needs a device, Metrics is admin only) and
// serialises the returned structs. Every data source (clock, settings, network
// addresses, disk, row counts, sidecars, CPU and memory) is injected behind a
// small interface so the service is testable without a real host.
//
// Nothing here returns a filesystem path, a token or a key. The TLS
// fingerprint in Status is the public value a phone pins at pairing.
package system

import (
	"errors"
	"fmt"
	"net"
	"runtime"
	"sort"
	"sync"
	"time"

	"github.com/ZaidQamhieh/rfe/agent/internal/next/settings"
	"github.com/ZaidQamhieh/rfe/agent/internal/sidecar"
)

// HealthOK is the value of Health.Status while the agent is serving.
const HealthOK = "ok"

// percentScale converts a 0..1 ratio to a 0..100 percentage.
const percentScale = 100

// ErrUnsupported is returned by a HostSampler on a platform where CPU or memory
// figures cannot be read; Metrics then reports 0 for that figure without logging.
var ErrUnsupported = errors.New("system: not supported on this platform")

// Clock supplies the current time.
type Clock interface{ Now() time.Time }

// SettingsSource supplies the live owner settings (display name, read-only).
// *settings.Service satisfies it.
type SettingsSource interface{ Get() settings.Settings }

// Addresses are the host's detected network addresses, without ports.
// Any field may be empty when nothing matching is up.
type Addresses struct {
	LAN       string
	Tailscale string
	MAC       string
}

// NetworkSource reports the host's current addresses.
type NetworkSource interface{ Addresses() Addresses }

// DiskSource reports free and total bytes of the volume holding the agent's data.
type DiskSource interface {
	Usage() (free, total uint64, err error)
}

// CountsSource reports the number of rows per data table. *store.DB satisfies it.
type CountsSource interface {
	Counts() (map[string]int, error)
}

// SidecarSource reports the state of the supervised sidecar processes.
type SidecarSource interface{ Snapshot() sidecar.Report }

// CPUTimes are cumulative CPU time counters; only their ratio matters.
type CPUTimes struct {
	Idle  uint64 // time spent idle (including waiting for I/O)
	Total uint64 // time spent in all states
}

// MemInfo is physical memory in bytes.
type MemInfo struct {
	Total     uint64
	Available uint64 // memory that can be handed to programs without swapping
}

// HostSampler reads whole-host CPU and memory figures. A platform that cannot
// supply them returns ErrUnsupported.
type HostSampler interface {
	CPU() (CPUTimes, error)
	Memory() (MemInfo, error)
}

// TrafficSource reports process-lifetime cumulative transfer byte counters.
type TrafficSource interface {
	Totals() (rx, tx int64)
}

// Config wires a Service. Version, APIVersion, Started, Settings and Disk are
// required; every other source is optional and its output is left out (or zero)
// when absent.
type Config struct {
	Version     string
	APIVersion  int      // protocol revision advertised to clients
	Features    []string // optional capability ids; reported sorted
	GOOS        string   // operating system reported to clients; defaults to runtime.GOOS
	GOARCH      string   // CPU architecture reported in Status.Platform; defaults to runtime.GOARCH
	Port        string   // port appended to detected hosts to form host:port addresses
	Fingerprint string   // lowercase hex SHA-256 of the TLS leaf certificate
	Started     time.Time

	Clock      Clock // defaults to the system clock
	Settings   SettingsSource
	Network    NetworkSource
	Disk       DiskSource
	Counts     CountsSource
	Sidecars   SidecarSource
	Host       HostSampler   // defaults to the platform sampler
	Throughput TrafficSource // defaults to a counter that never moves

	// Logf receives unexpected sampler failures. Nil discards them.
	Logf func(format string, args ...any)
}

// HealthDetail holds the fields only an authenticated caller may see, so an
// unpaired caller cannot fingerprint or map the host.
type HealthDetail struct {
	Name             string   `json:"name"`
	Version          string   `json:"version"`
	APIVersion       int      `json:"apiVersion"`
	Features         []string `json:"features"`
	OS               string   `json:"os"`
	ReadOnly         bool     `json:"readOnly"`
	Address          string   `json:"address"`
	TailscaleAddress string   `json:"tailscaleAddress"`
	MACAddress       string   `json:"macAddress,omitempty"`
}

// Health is the liveness answer. Detail is nil (and flattened away in JSON)
// for an unauthenticated caller, leaving only Status.
type Health struct {
	Status string `json:"status"`
	*HealthDetail
}

// Status is the authenticated self-report: the contract's AgentStatus fields
// plus identity, addresses, fingerprint, row counts and sidecar state.
type Status struct {
	Name             string          `json:"name"`
	Version          string          `json:"version"`
	APIVersion       int             `json:"apiVersion"`
	UptimeSeconds    int64           `json:"uptimeSeconds"`
	Platform         string          `json:"platform"`
	FreeBytes        uint64          `json:"freeBytes"`
	TotalBytes       uint64          `json:"totalBytes"`
	Address          string          `json:"address"`
	TailscaleAddress string          `json:"tailscaleAddress"`
	MACAddress       string          `json:"macAddress,omitempty"`
	Fingerprint      string          `json:"fingerprint,omitempty"`
	Counts           map[string]int  `json:"counts,omitempty"`
	Sidecars         *sidecar.Report `json:"sidecars,omitempty"`
}

// Metrics is a live host reading. RxBytes and TxBytes are cumulative since the
// agent started; clients diff successive readings against TsMs to get a rate.
type Metrics struct {
	RxBytes    int64   `json:"rxBytes"`
	TxBytes    int64   `json:"txBytes"`
	CPUPercent float64 `json:"cpuPercent"`
	RAMPercent float64 `json:"ramPercent"`
	TsMs       int64   `json:"tsMs"`
}

// Service answers health, status and metrics queries. It is safe for
// concurrent use.
type Service struct {
	cfg      Config
	features []string

	cpuMu   sync.Mutex
	cpuPrev CPUTimes
}

// systemClock is the real clock.
type systemClock struct{}

func (systemClock) Now() time.Time { return time.Now() }

// New validates cfg and returns a Service. It copies the feature list, so later
// changes to the caller's slice do not reach the service.
func New(cfg Config) (*Service, error) {
	switch {
	case cfg.Settings == nil:
		return nil, errors.New("system: Settings source is required")
	case cfg.Disk == nil:
		return nil, errors.New("system: Disk source is required")
	case cfg.Started.IsZero():
		return nil, errors.New("system: Started time is required")
	case cfg.APIVersion <= 0:
		return nil, errors.New("system: APIVersion must be positive")
	}
	if cfg.GOOS == "" {
		cfg.GOOS = runtime.GOOS
	}
	if cfg.GOARCH == "" {
		cfg.GOARCH = runtime.GOARCH
	}
	if cfg.Clock == nil {
		cfg.Clock = systemClock{}
	}
	if cfg.Host == nil {
		cfg.Host = NewHostSampler()
	}
	if cfg.Throughput == nil {
		cfg.Throughput = NewThroughput()
	}
	features := append([]string(nil), cfg.Features...)
	sort.Strings(features)
	return &Service{cfg: cfg, features: features}, nil
}

// Health returns the liveness answer. With authenticated false only Status is
// filled in; with true the identity, version, features, OS, read-only flag and
// reachable addresses are included as well.
func (s *Service) Health(authenticated bool) Health {
	h := Health{Status: HealthOK}
	if !authenticated {
		return h
	}
	set := s.cfg.Settings.Get()
	addrs := s.addresses()
	h.HealthDetail = &HealthDetail{
		Name:             set.AgentName,
		Version:          s.cfg.Version,
		APIVersion:       s.cfg.APIVersion,
		Features:         append([]string{}, s.features...),
		OS:               s.cfg.GOOS,
		ReadOnly:         set.ReadOnly,
		Address:          joinAddress(addrs.LAN, s.cfg.Port),
		TailscaleAddress: joinAddress(addrs.Tailscale, s.cfg.Port),
		MACAddress:       addrs.MAC,
	}
	return h
}

// Status returns the agent's self-report. A failing disk or row-count source is
// returned as an error (wrapped) rather than reported as zeros.
func (s *Service) Status() (Status, error) {
	free, total, err := s.cfg.Disk.Usage()
	if err != nil {
		return Status{}, fmt.Errorf("system: disk usage: %w", err)
	}
	set := s.cfg.Settings.Get()
	addrs := s.addresses()
	uptime := s.cfg.Clock.Now().Sub(s.cfg.Started)
	if uptime < 0 {
		uptime = 0
	}
	st := Status{
		Name:             set.AgentName,
		Version:          s.cfg.Version,
		APIVersion:       s.cfg.APIVersion,
		UptimeSeconds:    int64(uptime / time.Second),
		Platform:         s.cfg.GOOS + "/" + s.cfg.GOARCH,
		FreeBytes:        free,
		TotalBytes:       total,
		Address:          joinAddress(addrs.LAN, s.cfg.Port),
		TailscaleAddress: joinAddress(addrs.Tailscale, s.cfg.Port),
		MACAddress:       addrs.MAC,
		Fingerprint:      s.cfg.Fingerprint,
	}
	if s.cfg.Counts != nil {
		counts, err := s.cfg.Counts.Counts()
		if err != nil {
			return Status{}, fmt.Errorf("system: row counts: %w", err)
		}
		st.Counts = counts
	}
	if s.cfg.Sidecars != nil {
		r := s.cfg.Sidecars.Snapshot()
		st.Sidecars = &r
	}
	return st, nil
}

// Metrics returns a live reading. CPU is the busy share since the previous call
// (since boot on the first call); RAM is used memory as a share of the total.
// A figure the host cannot supply is 0; an unexpected read failure is also 0
// and is passed to Config.Logf.
func (s *Service) Metrics() Metrics {
	rx, tx := s.cfg.Throughput.Totals()
	return Metrics{
		RxBytes:    rx,
		TxBytes:    tx,
		CPUPercent: s.cpuPercent(),
		RAMPercent: s.ramPercent(),
		TsMs:       s.cfg.Clock.Now().UnixMilli(),
	}
}

// cpuPercent turns two cumulative CPU readings into the busy percentage
// between them, comparing against the previous reading kept in the service. If
// the counters went backwards it falls back to the since-boot ratio.
func (s *Service) cpuPercent() float64 {
	cur, err := s.cfg.Host.CPU()
	if err != nil {
		s.logSampler("cpu", err)
		return 0
	}
	s.cpuMu.Lock()
	prev := s.cpuPrev
	s.cpuPrev = cur
	s.cpuMu.Unlock()

	if cur.Total < prev.Total || cur.Idle < prev.Idle {
		prev = CPUTimes{}
	}
	dTotal := cur.Total - prev.Total
	if dTotal == 0 {
		return 0
	}
	busy := (1 - float64(cur.Idle-prev.Idle)/float64(dTotal)) * percentScale
	return clampPercent(busy)
}

// ramPercent returns used memory as a percentage of total memory.
func (s *Service) ramPercent() float64 {
	m, err := s.cfg.Host.Memory()
	if err != nil {
		s.logSampler("memory", err)
		return 0
	}
	if m.Total == 0 || m.Available >= m.Total {
		return 0
	}
	return clampPercent(float64(m.Total-m.Available) / float64(m.Total) * percentScale)
}

// logSampler reports an unexpected sampler failure; an unsupported platform is
// expected and stays quiet.
func (s *Service) logSampler(what string, err error) {
	if errors.Is(err, ErrUnsupported) || s.cfg.Logf == nil {
		return
	}
	s.cfg.Logf("system: reading %s failed: %v", what, err)
}

// addresses returns the current detected addresses, or none without a source.
func (s *Service) addresses() Addresses {
	if s.cfg.Network == nil {
		return Addresses{}
	}
	return s.cfg.Network.Addresses()
}

// joinAddress builds host:port (bracketing IPv6). An empty host gives an empty
// address, and an empty port gives the bare host.
func joinAddress(host, port string) string {
	if host == "" {
		return ""
	}
	if port == "" {
		return host
	}
	return net.JoinHostPort(host, port)
}

// clampPercent limits v to the range 0..100.
func clampPercent(v float64) float64 {
	switch {
	case v < 0:
		return 0
	case v > percentScale:
		return percentScale
	}
	return v
}
