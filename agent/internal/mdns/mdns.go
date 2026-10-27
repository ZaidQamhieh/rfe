// Package mdns advertises the RFE agent via mDNS/DNS-SD so mobile apps
// can discover agents on the local network without manual IP entry.
package mdns

import (
	"context"
	"fmt"
	"log"
	"os"

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

	server, err := zeroconf.Register(
		host,        // instance name
		serviceType, // service type
		domain,      // domain
		port,        // port
		txt,         // TXT records
		nil,         // interfaces (nil = all)
	)
	if err != nil {
		return nil, fmt.Errorf("mdns register: %w", err)
	}

	log.Printf("mDNS: advertising %s on port %d", serviceType, port)
	return &Service{server: server}, nil
}

// StartAlias advertises a custom hostname (e.g. "rfedash") over mDNS,
// independent of the machine's real hostname, so a browser can reach the
// agent at https://<alias>.local:<port> instead of typing an IP or the
// machine's own <hostname>.local. ip is the address to publish for it — the
// caller resolves the current LAN address, same as it already does for
// Start's reachability logging.
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

// Discover searches for RFE agents on the local network for the given
// duration. Returns discovered entries. Intended for testing.
func Discover(ctx context.Context) ([]*zeroconf.ServiceEntry, error) {
	entries := make(chan *zeroconf.ServiceEntry)
	browseDone := make(chan error, 1)
	go func() {
		browseDone <- zeroconf.Browse(ctx, serviceType, domain, entries)
	}()

	var results []*zeroconf.ServiceEntry
	entriesOpen := true
	browseFinished := false
	for entriesOpen || !browseFinished {
		select {
		case entry, ok := <-entries:
			if !ok {
				entriesOpen = false
				continue
			}
			results = append(results, entry)
		case err := <-browseDone:
			if err != nil {
				return nil, err
			}
			browseFinished = true
			browseDone = nil
		}
	}

	return results, nil
}
