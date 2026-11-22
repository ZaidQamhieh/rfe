package netinfo

import (
	"net"
	"testing"
)

func iface(name string, mac string, cidrs ...string) ifaceInfo {
	ii := ifaceInfo{Name: name}
	if mac != "" {
		hw, err := net.ParseMAC(mac)
		if err != nil {
			panic(err)
		}
		ii.HardwareAddr = hw
	}
	for _, c := range cidrs {
		ip, n, err := net.ParseCIDR(c)
		if err != nil {
			panic(err)
		}
		n.IP = ip
		ii.Addrs = append(ii.Addrs, n)
	}
	return ii
}

// A tunnel that comes first in interface order must not win the LAN address or the Wake-on-LAN MAC.
func TestSelectPrefersPhysicalNICOverTunnels(t *testing.T) {
	for _, tun := range []string{"wg0", "tun0", "ppp0", "tailscale0", "WG-home"} {
		t.Run(tun, func(t *testing.T) {
			got := selectAddresses([]ifaceInfo{
				iface(tun, "", "10.8.0.2/24", "100.101.102.103/32"),
				iface("enp3s0", "aa:bb:cc:dd:ee:ff", "192.168.1.20/24"),
			})
			if got.LAN != "192.168.1.20" || got.MAC != "aa:bb:cc:dd:ee:ff" {
				t.Errorf("LAN=%q MAC=%q, want the physical NIC", got.LAN, got.MAC)
			}
			if got.Tailscale != "100.101.102.103" {
				t.Errorf("Tailscale=%q, want the tailnet address reported separately", got.Tailscale)
			}
		})
	}
}

// Only a tunnel has a private address: there is no LAN address to advertise.
func TestSelectNoLANWhenOnlyTunnels(t *testing.T) {
	got := selectAddresses([]ifaceInfo{iface("wg0", "", "10.8.0.2/24")})
	if got.LAN != "" || got.MAC != "" {
		t.Errorf("got LAN=%q MAC=%q from a tunnel", got.LAN, got.MAC)
	}
}

// Among real interfaces one with a MAC beats one without, whatever the order.
func TestSelectPrefersInterfaceWithMAC(t *testing.T) {
	got := selectAddresses([]ifaceInfo{
		iface("nomac0", "", "10.0.0.5/24"),
		iface("eth0", "02:00:00:00:00:01", "192.168.7.9/24"),
	})
	if got.LAN != "192.168.7.9" || got.MAC != "02:00:00:00:00:01" {
		t.Errorf("LAN=%q MAC=%q", got.LAN, got.MAC)
	}
	// With no MAC anywhere the first private address still wins (previous behaviour).
	got = selectAddresses([]ifaceInfo{iface("a0", "", "10.0.0.5/24"), iface("b0", "", "10.0.1.5/24")})
	if got.LAN != "10.0.0.5" || got.MAC != "" {
		t.Errorf("LAN=%q MAC=%q", got.LAN, got.MAC)
	}
}

// The exported predicate used by mDNS agrees with the one Detect uses.
func TestIsVirtualBridgeCoversVMAdapters(t *testing.T) {
	for _, n := range []string{"vmnet8", "vboxnet0", "lxcbr0", "docker0", "virbr0"} {
		if !IsVirtualBridge(n) {
			t.Errorf("%s should be a virtual bridge", n)
		}
	}
}
