// Command agent is the RFE host service. It serves the file
// API to paired mobile devices over TLS, reachable on the LAN or via Tailscale.
//
// Invoked with no arguments (or a leading flag, or "serve") it runs the daemon.
// Other first arguments are admin subcommands — see admin.go (setup, pair,
// devices, revoke, remove, status).
package main

import (
	"context"
	"crypto/tls"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/ZaidQamhieh/rfe/agent/internal/fsops"
	"github.com/ZaidQamhieh/rfe/agent/internal/mdns"
	"github.com/ZaidQamhieh/rfe/agent/internal/netinfo"
	"github.com/ZaidQamhieh/rfe/agent/internal/pairing"
	"github.com/ZaidQamhieh/rfe/agent/internal/security"
	"github.com/ZaidQamhieh/rfe/agent/internal/server"
	"github.com/ZaidQamhieh/rfe/agent/internal/settings"
	"github.com/ZaidQamhieh/rfe/agent/internal/store"
	"github.com/ZaidQamhieh/rfe/agent/internal/transfer"
)

var version = "1.0.0"

func main() {
	args := os.Args[1:]
	// Backward compatible: no args, a leading flag, or "serve" runs the daemon —
	// so the existing systemd unit (`rfe -addr ... -name ...`) is unchanged.
	if len(args) == 0 || strings.HasPrefix(args[0], "-") || args[0] == "serve" {
		if len(args) > 0 && args[0] == "serve" {
			args = args[1:]
		}
		runServe(args)
		return
	}
	// Otherwise dispatch an admin subcommand (admin.go).
	if err := runAdmin(args[0], args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

// serveFlags holds the parsed `-addr`/`-name`/`-data`/`-read-only`/`-roots`
// flags for runServe.
type serveFlags struct {
	addr            string
	name            string
	dataDir         string
	readOnly        bool
	roots           string
	rootsExplicit   bool
	dataDirExplicit bool // -data was given; the daemon then never moves a pre-1.0 folder
}

// adminCommandNames are the subcommand words runAdmin dispatches (admin.go); used to
// explain a misplaced one on the serve command line.
var adminCommandNames = []string{"pair", "devices", "revoke", "remove", "jail", "readonly", "allow", "backup-dir", "status", "audit", "adduser", "install", "setup", "uninstall", "start", "stop", "help"}

// parseServeFlags parses the daemon flags and exits like flag.ExitOnError did when they are
// bad: status 2 with the problem and usage on stderr, status 0 for -h.
func parseServeFlags(args []string) serveFlags {
	flags, err := parseServeArgs(args)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			os.Exit(0)
		}
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(2)
	}
	return flags
}

// parseServeArgs is parseServeFlags returning its error. The daemon takes no positional
// arguments: flags may appear anywhere, and a leftover argument is an error naming it
// (never ignored, e.g. `rfe -data D devices` must not start the daemon).
func parseServeArgs(args []string) (serveFlags, error) {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	addr := fs.String("addr", ":8765", "listen address (host:port)")
	name := fs.String("name", hostName(), "agent display name shown to the phone")
	dataDir := fs.String("data", defaultDataDir(), "directory for certs, db, and state (precedence: -data > $RFE_DATA_DIR > ~/.rfe)")
	readOnly := fs.Bool("read-only", false, "reject all write operations")
	roots := fs.String("roots", defaultAllowedRoot(), "comma-separated allowed roots (default: ~/RFE Files; explicitly empty = allow all)")
	pos, err := parseInterspersed(fs, args)
	if err != nil {
		return serveFlags{}, err
	}
	if len(pos) > 0 {
		for _, sub := range adminCommandNames {
			if pos[0] == sub {
				return serveFlags{}, fmt.Errorf("unexpected argument %q: admin subcommands come first: rfe <sub> -data D", pos[0])
			}
		}
		return serveFlags{}, fmt.Errorf("unexpected argument %q: the daemon takes only flags (usage: rfe [serve] [-addr <addr>] [-name <name>] [-data <dir>] [-read-only] [-roots <list>])", pos[0])
	}
	rootsExplicit, dataDirExplicit := false, false
	fs.Visit(func(parsed *flag.Flag) {
		switch parsed.Name {
		case "roots":
			rootsExplicit = true
		case "data":
			dataDirExplicit = true
		}
	})
	return serveFlags{
		addr:            *addr,
		name:            *name,
		dataDir:         *dataDir,
		readOnly:        *readOnly,
		roots:           *roots,
		rootsExplicit:   rootsExplicit,
		dataDirExplicit: dataDirExplicit,
	}, nil
}

// serveDirs holds the on-disk directories runServe creates under dataDir.
type serveDirs struct {
	tempDir       string
	thumbCacheDir string
	updatesDir    string
	trashDir      string
}

// prepareServeDirs creates (and returns) the working directories the agent
// needs under dataDir, fatal-exiting if any can't be created.
func prepareServeDirs(dataDir string) serveDirs {
	d := serveDirs{
		tempDir:       filepath.Join(dataDir, "transfers"),
		thumbCacheDir: filepath.Join(dataDir, "thumbs"),
		updatesDir:    filepath.Join(dataDir, "updates"),
		trashDir:      defaultTrashDir(dataDir),
	}
	if err := os.MkdirAll(d.thumbCacheDir, 0o700); err != nil {
		log.Fatalf("thumb cache dir: %v", err)
	}
	if err := os.MkdirAll(d.updatesDir, 0o755); err != nil {
		log.Fatalf("updates dir: %v", err)
	}
	if err := os.MkdirAll(d.trashDir, 0o700); err != nil {
		log.Fatalf("trash dir: %v", err)
	}
	return d
}

// parseSeedRoots splits the comma-separated -roots flag value. It only seeds
// the DB on first run — the DB wins on every subsequent start.
func parseSeedRoots(roots string) []string {
	var seedRoots []string
	if roots == "" {
		return seedRoots
	}
	for _, r := range strings.Split(roots, ",") {
		if r = strings.TrimSpace(r); r != "" {
			seedRoots = append(seedRoots, r)
		}
	}
	return seedRoots
}

// defaultAllowedRoot keeps a brand-new agent inside a dedicated RFE Files
// folder under the signed-in user's home unless the operator explicitly
// supplies another root or -roots "". Existing databases preserve their policy.
func defaultAllowedRoot() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return defaultSetupRoot(home)
}

// newHTTPServer builds the *http.Server for the agent's TLS listener.
func newHTTPServer(addr string, handler http.Handler, cert tls.Certificate) *http.Server {
	return &http.Server{
		Addr:    addr,
		Handler: handler,
		TLSConfig: &tls.Config{
			Certificates: []tls.Certificate{cert},
			MinVersion:   tls.VersionTLS12,
		},
		// Headers should arrive quickly regardless of payload size.
		ReadHeaderTimeout: 10 * time.Second,
		// Large file uploads/downloads stream through this server (chunked
		// transfers and /v1/content), so Read/Write timeouts must be generous
		// enough to cover slow links transferring big files. 30 minutes
		// comfortably covers a multi-GB transfer over a slow connection
		// without leaving truly-stuck connections open indefinitely.
		ReadTimeout:  30 * time.Minute,
		WriteTimeout: 30 * time.Minute,
		// IdleTimeout bounds how long a keep-alive connection can sit idle
		// between requests; 2 minutes is plenty for normal browsing/polling
		// while freeing resources from abandoned connections.
		IdleTimeout: 2 * time.Minute,
	}
}

// webListenAddr is a second, best-effort HTTPS listener sharing the primary
// listener's handler and cert, so the agent is reachable at
// https://<name>.local with no port suffix (browsers default to 443).
// Binding a port below 1024 needs CAP_NET_BIND_SERVICE (or root); when that's
// not set up, startWebListener fails soft and the agent keeps running on the
// primary --addr listener only.
const webListenAddr = ":443"

// webListenBindAddr derives the port-443 listener's bind address from
// primaryAddr: the same explicit host if one was given (an operator
// restricting the primary listener, e.g. "127.0.0.1:8765", must have that
// restriction carry over), or webListenAddr (all interfaces) if primaryAddr's
// host is empty — matching that its own bind was already unrestricted.
func webListenBindAddr(primaryAddr string) string {
	if host, _, err := net.SplitHostPort(primaryAddr); err == nil && host != "" {
		return net.JoinHostPort(host, "443")
	}
	return webListenAddr
}

// startWebListener attempts to bind port 443 on the same host primaryAddr
// bound to — an explicit restriction there (e.g. "-addr 127.0.0.1:8765" to
// keep the agent off the LAN) must carry over to this second listener too,
// rather than this always binding all interfaces regardless (PR-61). A bare
// port with no host (the default, e.g. ":8765") means "all interfaces" was
// already the operator's own choice, so this listener keeps that behavior.
// Returns nil if the bind failed, so the caller can skip it in
// waitForShutdown.
func startWebListener(primaryAddr string, handler http.Handler, cert tls.Certificate) *http.Server {
	bindAddr := webListenBindAddr(primaryAddr)
	ln, err := net.Listen("tcp", bindAddr)
	if err != nil {
		log.Printf("web listener on %s not started (%v) — the agent is still reachable on its "+
			"primary port; run `sudo setcap cap_net_bind_service=+ep <agent binary>` to enable "+
			"port-free https:// access", bindAddr, err)
		return nil
	}
	srv := newHTTPServer(bindAddr, handler, cert)
	go func() {
		log.Printf("also listening on https://%s/v1  (no port needed)", bindAddr)
		if err := srv.ServeTLS(ln, "", ""); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Printf("web listener stopped: %v", err)
		}
	}()
	return srv
}

// startMDNS advertises the agent over mDNS on addr's port, if resolvable. It
// returns a stop func to be deferred by the caller, or nil if mDNS didn't
// start (invalid port or start error, both logged and non-fatal).
func startMDNS(addr, version string) func() {
	host, portStr, splitErr := net.SplitHostPort(addr)
	if splitErr != nil {
		return nil
	}
	if isLoopbackHost(host) {
		log.Printf("mDNS: not advertising, the agent listens on loopback only (%s)", addr)
		return nil
	}
	mdnsPort, convErr := strconv.Atoi(portStr)
	if convErr != nil {
		return nil
	}
	mdnsSvc, mdnsErr := mdns.Start(mdnsPort, version)
	if mdnsErr != nil {
		log.Printf("mDNS: failed to start: %v", mdnsErr)
		return nil
	}
	return mdnsSvc.Stop
}

// isLoopbackHost reports whether a listen host can only be reached from this
// machine, so advertising it on the LAN would point phones at nothing.
func isLoopbackHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// webAliasHost is the friendly mDNS hostname the web companion is also
// reachable at (https://<webAliasHost>.local:<port>), independent of the
// machine's own hostname.
const webAliasHost = "rfedash"

// startWebAlias advertises webAliasHost.local pointing at lanAddr's IP, if
// lanAddr was resolved. Same non-fatal-on-failure shape as startMDNS.
func startWebAlias(lanAddr, version string) func() {
	ip, portStr, splitErr := net.SplitHostPort(lanAddr)
	if splitErr != nil || ip == "" || isLoopbackHost(ip) {
		return nil
	}
	port, convErr := strconv.Atoi(portStr)
	if convErr != nil {
		return nil
	}
	aliasSvc, aliasErr := mdns.StartAlias(webAliasHost, port, ip, version)
	if aliasErr != nil {
		log.Printf("mDNS alias: failed to start: %v", aliasErr)
		return nil
	}
	return aliasSvc.Stop
}

// waitForShutdown blocks until SIGINT/SIGTERM, then gracefully shuts each of
// srvs down. Nil entries (e.g. an optional listener that never started) are
// skipped.
func waitForShutdown(srvs ...*http.Server) {
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	for _, srv := range srvs {
		if srv == nil {
			continue
		}
		if err := srv.Shutdown(ctx); err != nil {
			log.Printf("shutdown: %v", err)
		}
	}
	log.Println("agent stopped")
}

func runServe(args []string) {
	startTime := time.Now()
	flags := parseServeFlags(args)
	if !flags.dataDirExplicit {
		moveLegacyInstall(true)
		flags.dataDir = defaultDataDir()
	}
	if flags.roots == "" && !flags.rootsExplicit {
		log.Fatal("cannot determine the signed-in user's home folder; pass -roots <path>, or explicitly pass an empty -roots value for unrestricted access")
	}
	if !flags.rootsExplicit {
		if err := ensureDefaultShareRoot(flags.roots); err != nil {
			log.Fatalf("default allowed folder: %v", err)
		}
	}

	if err := os.MkdirAll(flags.dataDir, 0o700); err != nil {
		log.Fatalf("data dir: %v", err)
	}

	// The data folder holds the TLS key and database; keep it out of every
	// device's reach even when a shared root contains it.
	fsops.ProtectDir(flags.dataDir)

	cert, err := security.LoadOrCreateCert(flags.dataDir)
	if err != nil {
		log.Fatalf("tls: %v", err)
	}
	fingerprint := security.Fingerprint(cert)
	log.Printf("agent %q  cert-fingerprint=%s", flags.name, fingerprint)

	lanAddr, tsAddr, macAddr := reachableAddresses(flags.addr)
	log.Printf("reachable at  lan=%s  tailscale=%s  mac=%s", orNone(lanAddr), orNone(tsAddr), orNone(macAddr))

	db, err := store.Open(flags.dataDir)
	if err != nil {
		log.Fatalf("store: %v", err)
	}
	defer db.Close()

	dirs := prepareServeDirs(flags.dataDir)

	tm, err := transfer.New(db, dirs.tempDir)
	if err != nil {
		log.Fatalf("transfer: %v", err)
	}
	stopTransferCleanup := tm.StartStaleCleanup(0)
	defer stopTransferCleanup()

	pm := pairing.New(db, lanAddr, tsAddr, fingerprint)
	log.Printf("run `rfe pair` to add a device")

	st, err := settings.Load(db, flags.readOnly, parseSeedRoots(flags.roots), flags.name)
	if err != nil {
		log.Fatalf("settings: %v", err)
	}

	// R1: periodically delete expired one-time share tokens (T6).
	server.StartShareSweeper(db)

	handler, err := server.New(server.Config{
		Name:             st.AgentName(),
		Version:          version,
		CertFingerprint:  fingerprint,
		Address:          lanAddr,
		TailscaleAddress: tsAddr,
		MACAddress:       macAddr,
		ThumbCacheDir:    dirs.thumbCacheDir,
		Settings:         st,
		UpdatesDir:       dirs.updatesDir,
		TrashDir:         dirs.trashDir,
		StartTime:        startTime,
		DataDir:          flags.dataDir,
		OnPairRequest:    pairPromptHandler(db),
	}, db, pm, tm)
	if err != nil {
		log.Fatalf("server: %v", err)
	}

	srv := newHTTPServer(flags.addr, handler, cert)

	statsCtx, cancelStats := context.WithCancel(context.Background())
	statsDone := make(chan struct{})
	go func() {
		defer close(statsDone)
		server.PublishSidecarStats(statsCtx, flags.dataDir)
	}()
	// PublishSidecarStats writes once more on cancel; wait so the final counters reach disk before exit.
	defer func() {
		cancelStats()
		<-statsDone
	}()

	go func() {
		log.Printf("listening on https://%s/v1  (LAN + Tailscale)", flags.addr)
		// Cert/key are already in TLSConfig, so empty paths are correct here.
		if err := srv.ListenAndServeTLS("", ""); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("serve: %v", err)
		}
	}()

	webSrv := startWebListener(flags.addr, handler, cert)

	if stopMDNS := startMDNS(flags.addr, version); stopMDNS != nil {
		defer stopMDNS()
	}
	if stopAlias := startWebAlias(lanAddr, version); stopAlias != nil {
		defer stopAlias()
	}

	waitForShutdown(srv, webSrv)
}

func hostName() string {
	h, err := os.Hostname()
	if err != nil {
		return "host-agent"
	}
	return h
}

// defaultDataDir returns the data directory used when -data is not given:
// $RFE_DATA_DIR if set, otherwise ~/.rfe (matching the deployed systemd
// service and release.sh, which publish OTA updates to ~/.rfe/updates/).
// Falls back to the OS config dir if the home directory can't be resolved.
//
// This is the single source of truth for the default data dir, shared by the
// daemon (runServe) and the admin CLI (adminDataDir in admin.go) so that
// `rfe` and `rfe devices`/`pair`/etc. always operate on the same
// database by default. Precedence: -data flag > $RFE_DATA_DIR > ~/.rfe.
func defaultDataDir() string {
	if env := os.Getenv("RFE_DATA_DIR"); env != "" {
		return env
	}
	home, err := os.UserHomeDir()
	if err != nil {
		if dir, cfgErr := os.UserConfigDir(); cfgErr == nil {
			return filepath.Join(dir, "rfe")
		}
		return dataDirName
	}
	newDir, oldDir := filepath.Join(home, dataDirName), filepath.Join(home, legacyDataDirName)
	// A pre-1.0 install keeps its data in ~/.rfe-agent until the daemon or `rfe install` moves it (legacy.go).
	if legacyDataDirInUse(oldDir, newDir) {
		return oldDir
	}
	return newDir
}

// defaultTrashDir returns the trash store root. On Linux it is the user's real
// desktop trash ($XDG_DATA_HOME/Trash, else ~/.local/share/Trash) so app-side
// deletes also appear in the desktop's Trash; on other platforms (and when the
// home dir can't be resolved) it falls back to a managed dir under dataDir.
func defaultTrashDir(dataDir string) string {
	if runtime.GOOS == "linux" {
		base := os.Getenv("XDG_DATA_HOME")
		if base == "" {
			if home, err := os.UserHomeDir(); err == nil {
				base = filepath.Join(home, ".local", "share")
			}
		}
		if base != "" {
			return filepath.Join(base, "Trash")
		}
	}
	return filepath.Join(dataDir, "trash")
}

// reachableAddresses turns the listen address into the concrete host:port
// pairs the phone can actually dial. If the user bound to a specific host
// (e.g. "192.168.1.5:8765") that's trusted as-is and treated as the LAN
// address; a wildcard bind ("0.0.0.0:8765", ":8765") is resolved to the
// machine's real LAN and Tailscale IPs via netinfo. Also returns the MAC
// address of the LAN interface (for Wake-on-LAN support).
func reachableAddresses(listenAddr string) (lan, tailscale, mac string) {
	host, port, err := net.SplitHostPort(listenAddr)
	if err != nil {
		return "", "", ""
	}
	if host != "" && host != "0.0.0.0" && host != "::" {
		return listenAddr, "", ""
	}
	info := netinfo.Detect()
	if info.LAN != "" {
		lan = net.JoinHostPort(info.LAN, port)
	}
	if info.Tailscale != "" {
		tailscale = net.JoinHostPort(info.Tailscale, port)
	}
	return lan, tailscale, info.MAC
}

func orNone(s string) string {
	if s == "" {
		return "(none detected)"
	}
	return s
}
