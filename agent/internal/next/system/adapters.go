package system

import (
	"github.com/ZaidQamhieh/rfe/agent/internal/netinfo"
	"github.com/ZaidQamhieh/rfe/agent/internal/sidecar"
)

// DetectedNetwork is the NetworkSource backed by the host's real interfaces:
// the LAN IPv4 address and its MAC, and the Tailscale IPv4 address.
type DetectedNetwork struct{}

// Addresses inspects the machine's interfaces on every call, so a network
// change shows up without a restart.
func (DetectedNetwork) Addresses() Addresses {
	n := netinfo.Detect()
	return Addresses{LAN: n.LAN, Tailscale: n.Tailscale, MAC: n.MAC}
}

// ProcessSidecars is the SidecarSource backed by the sidecar supervisors
// running in this process.
type ProcessSidecars struct{}

// Snapshot returns the counters of every supervised sidecar and the event tallies.
func (ProcessSidecars) Snapshot() sidecar.Report { return sidecar.Snapshot() }

// pathDisk is a DiskSource for the volume that holds one path.
type pathDisk struct{ path string }

// DiskAt returns a DiskSource measuring the volume that holds path. The path is
// only used for the measurement and never appears in any output.
func DiskAt(path string) DiskSource { return pathDisk{path: path} }

// Usage returns the bytes available to the agent's user and the volume's total size.
func (d pathDisk) Usage() (free, total uint64, err error) { return statDisk(d.path) }
