//go:build !linux

package system

// unsupportedSampler reports ErrUnsupported: CPU and memory figures come from
// Linux /proc, so elsewhere Metrics shows 0 for them (as the contract states).
type unsupportedSampler struct{}

// NewHostSampler returns the sampler for this platform, which supplies nothing.
func NewHostSampler() HostSampler { return unsupportedSampler{} }

// CPU always reports ErrUnsupported.
func (unsupportedSampler) CPU() (CPUTimes, error) { return CPUTimes{}, ErrUnsupported }

// Memory always reports ErrUnsupported.
func (unsupportedSampler) Memory() (MemInfo, error) { return MemInfo{}, ErrUnsupported }
