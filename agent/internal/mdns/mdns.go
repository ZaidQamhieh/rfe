// Package mdns advertises the RFE agent via mDNS/DNS-SD so mobile apps
// can discover agents on the local network without manual IP entry.
package mdns

import (
	"fmt"
	"log"
	"net"
	"os"

	"github.com/ZaidQamhieh/rfe/agent/internal/netinfo"
	"github.com/libp2p/zeroconf/v2"
)

const (
	serviceType = "_rfe._tcp"
	domain      = "local."
)

// Service manages the mDNS advertisement lifetime.
type Service struct {
	server *zeroconf.Server
}

// Start begins advertising the RFE agent on the local network.
func Start(port int, version string) (*Service, error) {
	host, err := os.Hostname()
	if err != nil {
		host = "rfe"
	}

	txt := []string{
		fmt.Sprintf("version=%s", version),
		fmt.Sprintf("name=%s", host),
	}

	// Publish the machine's IPv4 addresses ourselves: left to zeroconf, an answer can carry only
	// the addresses of one interface (a VPN's), and a phone on the LAN never learns this PC's LAN address.
	var server *zeroconf.Server
	if ips := advertiseIPs(); len(ips) > 0 {
		server, err = zeroconf.RegisterProxy(host, serviceType, domain, port, host, ips, txt, nil)
	} else {
		server, err = zeroconf.Register(host, serviceType, domain, port, txt, nil)
	}
	if err != nil {
		return nil, fmt.Errorf("mdns register: %w", err)
	}

	log.Printf("mDNS: advertising %s on port %d", serviceType, port)
	return &Service{server: server}, nil
}

// advertiseIPs lists the IPv4 addresses worth announcing: those of interfaces that are up, not loopback,
// not container bridges or virtual pairs, and not link-local.
func advertiseIPs() []string {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil
	}
	var out []string
	for _, ifc := range ifaces {
		if ifc.Flags&net.FlagUp == 0 || ifc.Flags&net.FlagLoopback != 0 || skipInterface(ifc.Name) {
			continue
		}
		addrs, err := ifc.Addrs()
		if err != nil {
			continue
		}
		for _, a := range addrs {
			if ipn, ok := a.(*net.IPNet); ok {
				if ip := ipn.IP.To4(); ip != nil && !ip.IsLinkLocalUnicast() && !ip.IsUnspecified() {
					out = append(out, ip.String())
				}
			}
		}
	}
	return out
}

// skipInterface is true for interfaces a phone can never reach: container and VM bridges, veth pairs, libvirt.
// It shares netinfo's list so address detection and the mDNS announcement never disagree.
func skipInterface(name string) bool {
	return netinfo.IsVirtualBridge(name)
}

// StartAlias advertises a custom hostname (e.g. "rfedash") over mDNS,
// independent of the machine's real hostname, so a browser can reach the
// agent at https://<alias>.local:<port> instead of typing an IP or the
// machine's own <hostname>.local. ip is the address to publish for it — the
// caller resolves the current LAN address itself; Start takes none.
func StartAlias(alias string, port int, ip, version string) (*Service, error) {
	txt := []string{fmt.Sprintf("version=%s", version)}

	server, err := zeroconf.RegisterProxy(
		alias,        // instance name
		serviceType,  // service type
		domain,       // domain
		port,         // port
		alias,        // host — published as <alias>.local
		[]string{ip}, // ips
		txt,          // TXT records
		nil,          // interfaces (nil = all)
	)
	if err != nil {
		return nil, fmt.Errorf("mdns alias register: %w", err)
	}

	log.Printf("mDNS: advertising alias %s.local on port %d", alias, port)
	return &Service{server: server}, nil
}

// Stop shuts down the mDNS advertisement.
func (s *Service) Stop() {
	if s.server != nil {
		s.server.Shutdown()
		log.Println("mDNS: stopped")
	}
}
