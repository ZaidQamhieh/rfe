// Package netinfo detects the host's reachable network addresses so the agent
// can tell the phone how to find it — both on the LAN and over Tailscale —
// without the user having to type IPs by hand.
package netinfo

import (
	"log"
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

// Detect inspects the machine's network interfaces and returns addresses plus
// the MAC of the LAN interface (needed by the app for Wake-on-LAN when the
// host is asleep). Either address may be empty if no matching interface is up.
func Detect() NetworkInfo {
	ifaces, err := net.Interfaces()
	if err != nil {
		log.Printf("netinfo: listing interfaces: %v", err)
		return NetworkInfo{}
	}
	var up []ifaceInfo
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 || virtualBridge(iface.Name) {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			log.Printf("netinfo: listing addresses of %s: %v", iface.Name, err)
			continue
		}
		up = append(up, ifaceInfo{Name: iface.Name, HardwareAddr: iface.HardwareAddr, Addrs: addrs})
	}
	return selectAddresses(up)
}

// ifaceInfo is the part of a network interface that address selection looks at.
type ifaceInfo struct {
	Name         string
	HardwareAddr net.HardwareAddr
	Addrs        []net.Addr
}

// selectAddresses picks the Tailscale address (the first one in the tailnet range, on any interface) and the LAN
// address with its MAC. Tunnel interfaces (tun, wg, tailscale, ppp) are never the LAN: a phone on the Wi-Fi cannot
// reach their addresses and they have no Wake-on-LAN MAC. Of the remaining interfaces the first private IPv4
// address on one that has a MAC wins; without any MAC the first private address is used.
func selectAddresses(ifaces []ifaceInfo) NetworkInfo {
	var info NetworkInfo
	lanHasMAC := false
	for _, iface := range ifaces {
		tunnel := tunnelInterface(iface.Name)
		for _, a := range iface.Addrs {
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
			case ip4.IsPrivate() && !tunnel:
				hasMAC := len(iface.HardwareAddr) > 0
				if info.LAN == "" || (hasMAC && !lanHasMAC) {
					info.LAN = ip4.String()
					info.MAC = ""
					if hasMAC {
						info.MAC = iface.HardwareAddr.String()
					}
					lanHasMAC = hasMAC
				}
			}
		}
	}
	return info
}

// tunnelInterface reports VPN and point-to-point interfaces (tun*, wg*, tailscale*, ppp*; case-insensitive for
// Windows adapter names) whose private addresses are not the host's LAN address.
func tunnelInterface(name string) bool {
	lower := strings.ToLower(name)
	for _, p := range []string{"tun", "wg", "tailscale", "ppp"} {
		if strings.HasPrefix(lower, p) {
			return true
		}
	}
	return false
}

// IsVirtualBridge is virtualBridge for other packages, so the mDNS advertiser and the address detector skip the
// same interfaces.
func IsVirtualBridge(name string) bool { return virtualBridge(name) }

// virtualBridge reports container and VM bridge interfaces (docker0, br-*,
// virbr0, veth*, ...). Their private addresses are not the address a phone on
// the LAN can reach, and their MAC is not the one Wake-on-LAN needs.
func virtualBridge(name string) bool {
	for _, p := range []string{"docker", "br-", "virbr", "veth", "vmnet", "vboxnet", "cni", "flannel", "podman", "lxc"} {
		if strings.HasPrefix(name, p) {
			return true
		}
	}
	return false
}
