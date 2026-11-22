package daemon

import (
	"context"
	"errors"
	"net"
	"os/signal"
	"strconv"
	"strings"
	"syscall"

	"github.com/ZaidQamhieh/rfe/agent/internal/mdns"
)

// Ports and names of the optional listeners and announcements.
const (
	// webPort is the port of the second listener, so https://<name>.local works
	// without a port suffix (binding it needs CAP_NET_BIND_SERVICE or root).
	webPort = "443"
	// webAliasHost is the friendly mDNS name of the web companion.
	webAliasHost = "rfedash"
)

// isLoopbackHost reports whether a listen host can only be reached from this
// machine, so advertising it on the LAN would point phones at nothing.
func isLoopbackHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// webBindAddr derives the second listener's address: the same host as the
// primary listener (so a loopback restriction carries over), port 443.
func webBindAddr(primary string) string {
	if host, _, err := net.SplitHostPort(primary); err == nil && host != "" {
		return net.JoinHostPort(host, webPort)
	}
	return ":" + webPort
}

// startMDNS advertises the agent on the primary port unless it listens on
// loopback only. It returns a stop function, or nil when nothing started.
func (d *Daemon) startMDNS(addr string) func() {
	host, portStr, err := net.SplitHostPort(addr)
	if err != nil {
		return nil
	}
	if isLoopbackHost(host) {
		d.logf("mDNS: not advertising, the agent listens on loopback only (%s)", addr)
		return nil
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		return nil
	}
	svc, err := mdns.Start(port, d.opts.Version)
	if err != nil {
		d.logf("mDNS: failed to start: %v", err)
		return nil
	}
	return svc.Stop
}

// startAlias advertises the web companion name for the LAN address; nil when
// there is none or it fails.
func (d *Daemon) startAlias(lan string) func() {
	ip, portStr, err := net.SplitHostPort(lan)
	if err != nil || ip == "" || isLoopbackHost(ip) {
		return nil
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		return nil
	}
	svc, err := mdns.StartAlias(webAliasHost, port, ip, d.opts.Version)
	if err != nil {
		d.logf("mDNS alias: failed to start: %v", err)
		return nil
	}
	return svc.Stop
}

// Run builds the daemon, serves on opts.Addr (and best-effort on port 443 of the
// same host), announces it over mDNS and blocks until SIGINT or SIGTERM or a
// listener fails, then shuts down gracefully.
func Run(opts Options) error {
	d, err := New(opts)
	if err != nil {
		return err
	}
	defer d.Close()
	logf := d.logf
	logf("agent %q  cert-fingerprint=%s", d.settings.Get().AgentName, d.fingerprint)
	lan, ts := reachableAddresses(opts.Addr)
	logf("reachable at  lan=%s  tailscale=%s", orNone(lan), orNone(ts))

	ln, err := net.Listen("tcp", opts.Addr)
	if err != nil {
		return err
	}
	failed := make(chan error, 1)
	go func() {
		logf("listening on https://%s/v1", ln.Addr())
		failed <- d.Serve(ln)
	}()
	if wln, werr := net.Listen("tcp", webBindAddr(opts.Addr)); werr != nil {
		logf("web listener on %s not started (%v); the agent is still reachable on its main port", webBindAddr(opts.Addr), werr)
	} else {
		go func() {
			if err := d.Serve(wln); err != nil {
				logf("web listener stopped: %v", err)
			}
		}()
	}
	if stop := d.startMDNS(opts.Addr); stop != nil {
		defer stop()
	}
	if stop := d.startAlias(lan); stop != nil {
		defer stop()
	}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()
	select {
	case <-ctx.Done():
	case err := <-failed:
		if err != nil && !errors.Is(err, net.ErrClosed) {
			return err
		}
	}
	sctx, scancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer scancel()
	if err := d.Shutdown(sctx); err != nil {
		logf("shutdown: %v", err)
	}
	logf("agent stopped")
	return nil
}

// orNone names an address that was not detected.
func orNone(s string) string {
	if s == "" {
		return "(none detected)"
	}
	return s
}
