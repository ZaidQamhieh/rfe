package mdns

import "testing"

// VM host-only adapters (VMware vmnet, VirtualBox vboxnet) and LXC bridges are not reachable from a phone.
func TestSkipInterfaceVirtualMachineAdapters(t *testing.T) {
	for _, n := range []string{"vmnet1", "vmnet8", "vboxnet0", "lxcbr0"} {
		if !skipInterface(n) {
			t.Errorf("%s should be skipped", n)
		}
	}
}
