package mdns

import "testing"

func TestSkipInterface(t *testing.T) {
	for _, n := range []string{"docker0", "br-ea2dede86102", "veth44b9a19", "virbr0"} {
		if !skipInterface(n) {
			t.Errorf("%s should be skipped", n)
		}
	}
	for _, n := range []string{"enp6s0", "wlan0", "tailscale0", "eth0"} {
		if skipInterface(n) {
			t.Errorf("%s should be announced", n)
		}
	}
}

func TestAdvertiseIPsAreIPv4Only(t *testing.T) {
	for _, ip := range advertiseIPs() {
		if len(ip) == 0 || ip[0] == '[' || containsColon(ip) {
			t.Errorf("not an IPv4 address: %q", ip)
		}
	}
}

func containsColon(s string) bool {
	for _, c := range s {
		if c == ':' {
			return true
		}
	}
	return false
}
