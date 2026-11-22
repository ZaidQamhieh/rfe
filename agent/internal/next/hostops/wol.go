package hostops

import (
	"errors"
	"fmt"
	"net"
	"sync"
	"time"
)

// Wake-on-LAN constants. A magic packet is six 0xFF bytes followed by the MAC
// sixteen times.
const (
	macBytes          = 6
	magicRepeats      = 16
	magicHeaderByte   = 0xFF
	wakeNetwork       = "udp4"
	wakeTarget        = "255.255.255.255:9"
	DefaultWakeBudget = 10 // packets per device per window
	DefaultWakeWindow = time.Minute
	wakeMaxDevices    = 1024 // bound on devices tracked by the limiter
	unknownDeviceKey  = "<unknown>"
)

// Errors of Waker.Wake.
var (
	// ErrInvalidMAC means the text is not a 6-byte hardware address.
	ErrInvalidMAC = errors.New("MAC must be 6-byte colon-separated")
	// ErrWakeRateLimited means the device used up its packet budget.
	ErrWakeRateLimited = errors.New("too many Wake-on-LAN requests")
)

// PacketSender transmits one magic packet.
type PacketSender interface {
	Send(packet []byte) error
}

// UDPBroadcast sends the packet as a UDP broadcast to the discard port.
type UDPBroadcast struct {
	// Target is "host:port"; empty means the limited broadcast address, port 9.
	Target string
}

// Send dials the target and writes the packet.
func (u UDPBroadcast) Send(packet []byte) error {
	target := u.Target
	if target == "" {
		target = wakeTarget
	}
	conn, err := net.Dial(wakeNetwork, target)
	if err != nil {
		return fmt.Errorf("wol: dial: %w", err)
	}
	defer conn.Close()
	if _, err := conn.Write(packet); err != nil {
		return fmt.Errorf("wol: write: %w", err)
	}
	return nil
}

// MagicPacket builds the Wake-on-LAN packet for a 6-byte MAC.
func MagicPacket(mac net.HardwareAddr) []byte {
	packet := make([]byte, 0, macBytes*(1+magicRepeats))
	for i := 0; i < macBytes; i++ {
		packet = append(packet, magicHeaderByte)
	}
	for i := 0; i < magicRepeats; i++ {
		packet = append(packet, mac...)
	}
	return packet
}

// WakerOptions tune a Waker; zero fields take the defaults.
type WakerOptions struct {
	Budget int           // packets per device per Window (default DefaultWakeBudget)
	Window time.Duration // default DefaultWakeWindow
	Now    func() time.Time
}

// wakeWindow is one device's count since start.
type wakeWindow struct {
	start time.Time
	hits  int
}

// Waker validates a MAC, rate-limits each device (enough to wake a machine, not
// enough to use the agent as a LAN broadcast amplifier) and sends the packet.
type Waker struct {
	send PacketSender
	opt  WakerOptions
	mu   sync.Mutex
	seen map[string]wakeWindow
}

// NewWaker returns a Waker that transmits through send.
func NewWaker(send PacketSender, opt WakerOptions) *Waker {
	if opt.Budget < 1 {
		opt.Budget = DefaultWakeBudget
	}
	if opt.Window <= 0 {
		opt.Window = DefaultWakeWindow
	}
	if opt.Now == nil {
		opt.Now = time.Now
	}
	return &Waker{send: send, opt: opt, seen: make(map[string]wakeWindow)}
}

// Wake sends a magic packet for rawMAC on behalf of deviceID and returns the
// MAC in canonical form (for the audit trail). The budget is checked before the
// MAC, so a flood of bad requests is limited too. It returns ErrWakeRateLimited,
// ErrInvalidMAC, or the sender's error.
func (w *Waker) Wake(deviceID, rawMAC string) (string, error) {
	if !w.allow(deviceID) {
		return "", ErrWakeRateLimited
	}
	mac, err := net.ParseMAC(rawMAC)
	if err != nil || len(mac) != macBytes {
		return "", ErrInvalidMAC
	}
	if err := w.send.Send(MagicPacket(mac)); err != nil {
		return "", err
	}
	return mac.String(), nil
}

// allow counts one request by key and reports whether it is within the budget.
// The table is bounded: when full, expired windows are dropped and, if it is
// still full, a new device is refused until a slot ages out.
func (w *Waker) allow(key string) bool {
	if key == "" {
		key = unknownDeviceKey
	}
	now := w.opt.Now()
	w.mu.Lock()
	defer w.mu.Unlock()
	e, ok := w.seen[key]
	if ok && now.Sub(e.start) < w.opt.Window {
		if e.hits >= w.opt.Budget {
			return false
		}
		e.hits++
		w.seen[key] = e
		return true
	}
	if !ok && len(w.seen) >= wakeMaxDevices {
		for k, old := range w.seen {
			if now.Sub(old.start) >= w.opt.Window {
				delete(w.seen, k)
			}
		}
		if len(w.seen) >= wakeMaxDevices {
			return false
		}
	}
	w.seen[key] = wakeWindow{start: now, hits: 1}
	return true
}
