// Package daemon is the composition root of agent-next: it builds every
// service of the new agent from a data directory, adapts them to the interfaces
// of the HTTP layer (package api), serves the web companion beside the API, and
// runs the TLS listener. Nothing here decides behaviour; each rule lives in the
// service it belongs to.
package daemon

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"sync"
	"time"

	"github.com/ZaidQamhieh/rfe/agent/internal/events"
	"github.com/ZaidQamhieh/rfe/agent/internal/netinfo"
	"github.com/ZaidQamhieh/rfe/agent/internal/next/api"
	"github.com/ZaidQamhieh/rfe/agent/internal/next/apps"
	"github.com/ZaidQamhieh/rfe/agent/internal/next/audit"
	"github.com/ZaidQamhieh/rfe/agent/internal/next/hostops"
	"github.com/ZaidQamhieh/rfe/agent/internal/next/legacyimport"
	"github.com/ZaidQamhieh/rfe/agent/internal/next/pairing"
	"github.com/ZaidQamhieh/rfe/agent/internal/next/rootfs"
	"github.com/ZaidQamhieh/rfe/agent/internal/next/settings"
	"github.com/ZaidQamhieh/rfe/agent/internal/next/store"
	"github.com/ZaidQamhieh/rfe/agent/internal/next/system"
	"github.com/ZaidQamhieh/rfe/agent/internal/next/thumbs"
	"github.com/ZaidQamhieh/rfe/agent/internal/next/transfer"
	"github.com/ZaidQamhieh/rfe/agent/internal/security"
	"github.com/ZaidQamhieh/rfe/agent/internal/sidecar"
	"github.com/ZaidQamhieh/rfe/agent/internal/webui"
)

const (
	// APIVersion is the protocol revision this agent speaks (protocol/compat.md).
	APIVersion  = 3
	DefaultAddr = ":8765"
	EnvDataDir  = "RFE_DATA_DIR"

	dataDirName     = ".rfe"
	fallbackAgent   = "host-agent"
	fallbackCfgName = "rfe"

	transfersDirName = "transfers"
	thumbsDirName    = "thumbs"
	updatesDirName   = "updates"
	trashDirName     = "trash"
	linuxTrashParent = "Trash"

	dataDirPerm    os.FileMode = 0o700
	updatesDirPerm os.FileMode = 0o755
	rootDirPerm    os.FileMode = 0o755

	shareURLScheme = "https://"

	// Server timeouts, as the old agent: headers arrive quickly, while file
	// transfers stream for a long time on slow links.
	readHeaderTimeout = 10 * time.Second
	transferTimeout   = 30 * time.Minute
	idleTimeout       = 2 * time.Minute
	shutdownTimeout   = 10 * time.Second
	tlsMinVersion     = tls.VersionTLS12

	featurePairRequest = "pairRequest"
)

// features are the optional capability ids reported to clients.
var features = []string{featurePairRequest}

// ErrNeedsImport is returned when the data directory holds an old database but
// no new one: the owner must run the import first.
var ErrNeedsImport = errors.New("this data directory has an old agent database that was not imported yet: run `agent-next import --check` and then `agent-next import`")

// Options configures a Daemon.
type Options struct {
	Addr          string // listen address; its port is advertised
	Name          string // display name of a new database
	DataDir       string
	Roots         []string // folders seeding a new database
	RootsExplicit bool     // Roots was chosen by the owner (empty means unrestricted)
	ReadOnly      bool     // seeds a new database
	Version       string

	// TrashDir overrides the trash folder (default: the desktop trash on Linux,
	// else <data>/trash). Restarter overrides the host restart (tests).
	TrashDir  string
	Restarter api.Restarter
	Logf      func(format string, args ...any) // default log.Printf
}

// Daemon is the running agent: its services and its handler.
type Daemon struct {
	opts        Options
	logf        func(string, ...any)
	db          *store.DB
	settings    *settings.Service
	hub         *hub
	events      *events.Hub
	pairing     *pairing.Service
	transfers   *transfer.Manager
	sidecars    *sidecarSet
	search      *searchAdapter
	cert        tls.Certificate
	fingerprint string
	apiCfg      api.Config
	handler     http.Handler

	stopSweeper func()
	cancelSub   func()

	mu      sync.Mutex
	servers []*http.Server
	once    sync.Once
}

// DefaultDataDir is the data directory when -data is not given: $RFE_DATA_DIR,
// else ~/.rfe (the old agent's folder, so the certificate and the pairing
// carry over), else the OS config folder.
func DefaultDataDir() string {
	if env := os.Getenv(EnvDataDir); env != "" {
		return env
	}
	home, err := os.UserHomeDir()
	if err != nil {
		if dir, cfgErr := os.UserConfigDir(); cfgErr == nil {
			return filepath.Join(dir, fallbackCfgName)
		}
		return dataDirName
	}
	return filepath.Join(home, dataDirName)
}

// DefaultName is the display name when -name is not given: the host name.
func DefaultName() string {
	if h, err := os.Hostname(); err == nil && h != "" {
		return h
	}
	return fallbackAgent
}

// defaultTrashDir is the trash folder: the desktop trash on Linux so deletions
// from the app show up there, else a folder under the data directory.
func defaultTrashDir(dataDir string) string {
	if runtime.GOOS == "linux" {
		base := os.Getenv("XDG_DATA_HOME")
		if base == "" {
			if home, err := os.UserHomeDir(); err == nil {
				base = filepath.Join(home, ".local", "share")
			}
		}
		if base != "" {
			return filepath.Join(base, linuxTrashParent)
		}
	}
	return filepath.Join(dataDir, trashDirName)
}

// checkImported refuses to continue on a data directory that holds an old
// database and no new one, so a fresh empty database never hides the old data.
func checkImported(dataDir string) error {
	_, newErr := os.Stat(filepath.Join(dataDir, legacyimport.NewDBName))
	if newErr == nil {
		return nil
	}
	if _, oldErr := os.Stat(filepath.Join(dataDir, legacyimport.OldDBName)); oldErr == nil {
		return ErrNeedsImport
	}
	return nil
}

// reachableAddresses turns the listen address into the host:port pairs a phone
// can dial: a named host is the LAN address as given, a wildcard bind is
// resolved to the machine's LAN and Tailscale addresses.
func reachableAddresses(listenAddr string) (lan, tailscale string) {
	host, port, err := net.SplitHostPort(listenAddr)
	if err != nil {
		return "", ""
	}
	if host != "" && host != "0.0.0.0" && host != "::" {
		return listenAddr, ""
	}
	info := netinfo.Detect()
	if info.LAN != "" {
		lan = net.JoinHostPort(info.LAN, port)
	}
	if info.Tailscale != "" {
		tailscale = net.JoinHostPort(info.Tailscale, port)
	}
	return lan, tailscale
}

// New builds every service and the handler. The caller must Close the Daemon.
// Any failure releases what was already built.
func New(opts Options) (d *Daemon, err error) {
	if opts.Logf == nil {
		opts.Logf = log.Printf
	}
	logf := opts.Logf
	if err := os.MkdirAll(opts.DataDir, dataDirPerm); err != nil {
		return nil, fmt.Errorf("data dir: %w", err)
	}
	if err := checkImported(opts.DataDir); err != nil {
		return nil, err
	}
	cert, err := security.LoadOrCreateCert(opts.DataDir)
	if err != nil {
		return nil, fmt.Errorf("tls: %w", err)
	}
	// built is a separate variable: `return nil, err` would reset the named result d
	// to nil before this deferred cleanup runs and Close would hit a nil pointer.
	built := &Daemon{opts: opts, logf: logf, cert: cert, fingerprint: security.Fingerprint(cert)}
	defer func() {
		if err != nil {
			built.Close()
		}
	}()
	if err = built.buildServices(); err != nil {
		return nil, err
	}
	if err = built.buildAPI(); err != nil {
		return nil, err
	}
	return built, nil
}

// buildServices opens the database and creates the services that need no HTTP wiring.
func (d *Daemon) buildServices() (err error) {
	o := d.opts
	if d.db, err = store.Open(filepath.Join(o.DataDir, legacyimport.NewDBName)); err != nil {
		return fmt.Errorf("store: %w", err)
	}
	home, _ := os.UserHomeDir()
	if d.settings, err = settings.Open(d.db, settings.Options{
		HomeDir: home, RootsExplicit: o.RootsExplicit, SeedRoots: o.Roots, ReadOnly: o.ReadOnly, AgentName: o.Name,
	}); err != nil {
		return fmt.Errorf("settings: %w", err)
	}
	if !o.RootsExplicit && home != "" {
		def := filepath.Join(home, settings.DefaultFilesFolderName)
		if slices.ContainsFunc(d.settings.Get().Roots, func(r string) bool { return filepath.Clean(r) == def }) {
			if err = os.MkdirAll(def, rootDirPerm); err != nil {
				return fmt.Errorf("default folder: %w", err)
			}
		}
	}
	trashDir := o.TrashDir
	if trashDir == "" {
		trashDir = defaultTrashDir(o.DataDir)
	}
	if d.hub, err = newHub(d.db, d.settings, []string{o.DataDir}, trashDir, d.logf); err != nil {
		return fmt.Errorf("folders: %w", err)
	}
	d.sidecars = startSidecars(func() []string { return d.settings.Get().Roots }, d.logf)
	d.cancelSub = d.settings.Subscribe(func(ch settings.Change) {
		d.hub.onSettings(ch)
		if !slices.Equal(ch.Old.Roots, ch.New.Roots) {
			d.sidecars.rebuildIndex()
		}
	})
	d.search = &searchAdapter{hub: d.hub, settings: d.settings, indexer: d.sidecars.indexer()}
	return nil
}

// buildAPI creates the services that sit behind the HTTP layer, assembles its
// configuration and builds the handler (API under /v1, web companion elsewhere).
func (d *Daemon) buildAPI() error {
	o := d.opts
	auditLog := audit.New(d.db, audit.Options{OnError: func(action string, err error) {
		d.logf("audit: %s not recorded: %v", action, err)
	}})
	lan, ts := reachableAddresses(o.Addr)
	pair, err := pairing.New(pairing.Options{
		Store: d.db, Fingerprint: d.fingerprint, AgentName: d.settings.Get().AgentName,
		Address: lan, TailscaleAddress: ts, Audit: auditLog,
		OnPairRequest: func(p pairing.Prompt) {
			d.logf("pairing request %s from %q at %s: match code %s; approve it from a signed-in device", p.ID, p.Label, p.IP, p.SAS)
		},
	})
	if err != nil {
		return fmt.Errorf("pairing: %w", err)
	}
	d.pairing = pair

	_, port, _ := net.SplitHostPort(o.Addr)
	sys, err := system.New(system.Config{
		Version: o.Version, APIVersion: APIVersion, Features: features, Port: port,
		Fingerprint: d.fingerprint, Started: time.Now(), Settings: d.settings,
		Network: system.DetectedNetwork{}, Disk: system.DiskAt(o.DataDir), Counts: d.db,
		Sidecars: system.ProcessSidecars{}, Logf: d.logf,
	})
	if err != nil {
		return fmt.Errorf("system: %w", err)
	}

	if d.transfers, err = transfer.New(transfer.Config{Dir: filepath.Join(o.DataDir, transfersDirName), Store: d.db, Logf: d.logf}); err != nil {
		return fmt.Errorf("transfer: %w", err)
	}
	d.stopSweeper = d.transfers.StartSweeper(transfer.DefaultSweepInterval)

	thumbCfg := thumbs.Config{CacheDir: filepath.Join(o.DataDir, thumbsDirName), Notify: sidecar.Note, Logf: d.logf}
	if r := d.sidecars.renderer(); r != nil {
		thumbCfg.Renderer = r
	}
	thumbSvc, err := thumbs.New(thumbCfg)
	if err != nil {
		return fmt.Errorf("thumbs: %w", err)
	}

	updatesDir := filepath.Join(o.DataDir, updatesDirName)
	if err := os.MkdirAll(updatesDir, updatesDirPerm); err != nil {
		return fmt.Errorf("updates dir: %w", err)
	}
	var restarter api.Restarter = hostops.NewRestarter()
	if o.Restarter != nil {
		restarter = o.Restarter
	}

	d.events = events.NewHub()
	d.apiCfg = api.Config{
		Pairing: pair, Devices: d.db, System: sys, Policy: d.settings,
		Apps:      apps.New(apps.Config{Audit: auditLog}),
		Files:     &filesProvider{hub: d.hub},
		Transfers: &transferAdapter{mgr: d.transfers, hub: d.hub},
		Uploads:   d.db,
		Misc: api.MiscServices{
			Search: d.search, Thumbs: thumbSvc, ThumbSets: d.thumbSet,
			Shares:       &shareAdapter{hub: d.hub},
			ShareBaseURL: func() string { return shareBase(lan) },
			Settings:     d.settings, Audit: auditLog,
		},
		Host: api.HostServices{
			Users: d.db, Logs: hostops.Journal{}, Restart: restarter,
			Wake: hostops.NewWaker(hostops.UDPBroadcast{}, hostops.WakerOptions{}), Updates: hostops.NewUpdates(updatesDir),
			Backups: backupStatus{dataDir: o.DataDir, bundleRoot: os.Getenv("RFE_BUNDLE_ROOT"), logf: d.logf},
		},
		Events:       d.events,
		ValidateJail: d.hub.validateJail,
		Logger:       log.New(logWriter{d.logf}, "", 0),
	}
	apiHandler, err := api.New(d.apiCfg)
	if err != nil {
		return fmt.Errorf("api: %w", err)
	}
	mux := http.NewServeMux()
	mux.Handle(api.PathPrefix+"/", apiHandler)
	mux.Handle("/", webui.Handler())
	d.handler = mux
	return nil
}

// shareBase is the scheme and address a share link starts with; empty makes
// the API use the Host of the request.
func shareBase(lan string) string {
	if lan == "" {
		return ""
	}
	return shareURLScheme + lan
}

// thumbSet gives the thumbnail service the folders of a caller confined to jail.
// The Sets are cached by the hub, so releasing one is a no-op.
func (d *Daemon) thumbSet(jail string) (*rootfs.Set, func(), error) {
	set, err := d.hub.setFor(jail)
	return set, func() {}, err
}

// logWriter turns the log.Logger the API writes to into calls of a logf function.
type logWriter struct{ logf func(string, ...any) }

// Write logs one line.
func (w logWriter) Write(p []byte) (int, error) {
	w.logf("%s", string(p))
	return len(p), nil
}

// Handler returns the daemon's HTTP handler: the API under /v1 and the web
// companion everywhere else.
func (d *Daemon) Handler() http.Handler { return d.handler }

// Fingerprint returns the lowercase hex SHA-256 of the TLS certificate.
func (d *Daemon) Fingerprint() string { return d.fingerprint }

// newServer builds the TLS http.Server for the handler.
func (d *Daemon) newServer(addr string) *http.Server {
	srv := &http.Server{
		Addr:    addr,
		Handler: d.handler,
		TLSConfig: &tls.Config{
			Certificates: []tls.Certificate{d.cert},
			MinVersion:   tlsMinVersion,
		},
		ReadHeaderTimeout: readHeaderTimeout,
		ReadTimeout:       transferTimeout,
		WriteTimeout:      transferTimeout,
		IdleTimeout:       idleTimeout,
	}
	d.mu.Lock()
	d.servers = append(d.servers, srv)
	d.mu.Unlock()
	return srv
}

// Serve answers HTTPS (and HTTP/2) on ln until Shutdown; it returns nil after a
// clean shutdown.
func (d *Daemon) Serve(ln net.Listener) error {
	err := d.newServer(ln.Addr().String()).ServeTLS(ln, "", "")
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

// Shutdown stops every listener, waiting for requests in flight until ctx ends.
func (d *Daemon) Shutdown(ctx context.Context) error {
	// Open event streams never finish on their own and http.Server.Shutdown waits
	// for them: ending the hub ends every stream first.
	if d.events != nil {
		d.events.Close()
	}
	d.mu.Lock()
	srvs := slices.Clone(d.servers)
	d.mu.Unlock()
	var errs []error
	for _, s := range srvs {
		if err := s.Shutdown(ctx); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// Close releases every service. It is safe to call twice.
func (d *Daemon) Close() {
	d.once.Do(func() {
		if d.stopSweeper != nil {
			d.stopSweeper()
		}
		if d.transfers != nil {
			if err := d.transfers.Close(); err != nil {
				d.logf("transfer: close: %v", err)
			}
		}
		if d.sidecars != nil {
			d.sidecars.stop()
		}
		if d.cancelSub != nil {
			d.cancelSub()
		}
		if d.events != nil {
			d.events.Close()
		}
		if d.hub != nil {
			d.hub.close()
		}
		if d.db != nil {
			if err := d.db.Close(); err != nil {
				d.logf("store: close: %v", err)
			}
		}
	})
}
