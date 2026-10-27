// Package netinfo detects the host's reachable network addresses so the agent
// can tell the phone how to find it — both on the LAN and over Tailscale —
// without the user having to type IPs by hand.
package netinfo

import (
	"net"
	"strings"
)

// tailscaleRange is the CGNAT block Tailscale assigns tailnet IPs from
// (100.64.0.0/10). Identifying addresses by range works the same on every
// platform, unlike matching interface names such as "tailscale0"/"Tailscale".
var tailscaleRange = mustCIDR("100.64.0.0/10")

func mustCIDR(s string) *net.IPNet {
	_, n, err := net.ParseCIDR(s)
	if err != nil {
		panic(err)
	}
	return n
}

// NetworkInfo holds the host's detected network addresses and the MAC address
// of the LAN interface (for Wake-on-LAN).
type NetworkInfo struct {
	LAN       string // LAN IPv4 address, e.g. "192.168.1.20"
	Tailscale string // Tailscale IPv4 address, e.g. "100.x.y.z"
	MAC       string // hardware address of the LAN interface, e.g. "aa:bb:cc:dd:ee:ff"
}

// LocalAddresses inspects the machine's network interfaces and returns the
// best-guess LAN IPv4 address and Tailscale IPv4 address. Either may be empty
// if no matching interface is up. When multiple candidates exist the first one
// found wins — good enough for display/pairing purposes.
func LocalAddresses() (lan string, tailscale string) {
	info := Detect()
	return info.LAN, info.Tailscale
}

// Detect inspects the machine's network interfaces and returns addresses plus
// the MAC of the LAN interface (needed by the app for Wake-on-LAN when the
// host is asleep). Either address may be empty if no matching interface is up.
func Detect() NetworkInfo {
	var info NetworkInfo
	ifaces, err := net.Interfaces()
	if err != nil {
		return info
	}
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 || virtualBridge(iface.Name) {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, a := range addrs {
			ipNet, ok := a.(*net.IPNet)
			if !ok {
				continue
			}
			ip4 := ipNet.IP.To4()
			if ip4 == nil {
				continue
			}
			switch {
			case tailscaleRange.Contains(ip4):
				if info.Tailscale == "" {
					info.Tailscale = ip4.String()
				}
			case ip4.IsPrivate():
				if info.LAN == "" {
					info.LAN = ip4.String()
					if len(iface.HardwareAddr) > 0 {
						info.MAC = iface.HardwareAddr.String()
					}
				}
			}
		}
	}
	return info
}

// virtualBridge reports container and VM bridge interfaces (docker0, br-*,
// virbr0, veth*, ...). Their private addresses are not the address a phone on
// the LAN can reach, and their MAC is not the one Wake-on-LAN needs.
func virtualBridge(name string) bool {
	for _, p := range []string{"docker", "br-", "virbr", "veth", "vmnet", "vboxnet", "cni", "flannel", "podman"} {
		if strings.HasPrefix(name, p) {
			return true
		}
	}
	return false
}
