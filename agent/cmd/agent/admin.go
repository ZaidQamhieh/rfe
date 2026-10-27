package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/skip2/go-qrcode"
	"golang.org/x/term"

	"github.com/ZaidQamhieh/rfe/agent/internal/pairing"
	"github.com/ZaidQamhieh/rfe/agent/internal/security"
	"github.com/ZaidQamhieh/rfe/agent/internal/server"
	"github.com/ZaidQamhieh/rfe/agent/internal/settings"
	"github.com/ZaidQamhieh/rfe/agent/internal/store"
)

// runAdmin dispatches an admin subcommand. Each command operates on the agent's
// data dir (DB + cert) directly, so it works whether or not the daemon is up.
func runAdmin(cmd string, args []string) error {
	// -h/--help on a subcommand already printed its usage; that is not a failure.
	if err := dispatchAdmin(cmd, args); err != nil && !errors.Is(err, flag.ErrHelp) {
		return err
	}
	return nil
}

func dispatchAdmin(cmd string, args []string) error {
	switch cmd {
	case "pair":
		return cmdPair(args)
	case "devices":
		return cmdDevices(args)
	case "revoke":
		return cmdRevokeOrRemove(args, false)
	case "remove":
		return cmdRevokeOrRemove(args, true)
	case "jail":
		return cmdJail(args)
	case "readonly":
		return cmdReadonly(args)
	case "allow":
		return cmdAllow(args)
	case "backup-dir":
		return cmdBackupDir(args)
	case "status":
		return cmdStatus(args)
	case "audit":
		return cmdAudit(args)
	case "adduser":
		return cmdAddUser(args)
	case "install":
		return cmdInstall(args)
	case "setup":
		return cmdSetup(args)
	case "uninstall", "start", "stop":
		// These take no arguments; a stray one is an error, never ignored.
		if err := rejectAdminArgs(cmd, args); err != nil {
			return err
		}
		switch cmd {
		case "uninstall":
			return cmdUninstall(args)
		case "start":
			return cmdStart(args)
		default:
			return cmdStop(args)
		}
	case "help", "-h", "--help":
		printAdminUsage(os.Stdout)
		return nil
	default:
		printAdminUsage(os.Stderr)
		return fmt.Errorf("unknown command %q", cmd)
	}
}

func printAdminUsage(w *os.File) {
	fmt.Fprint(w, `rfe — RFE host agent

Usage:
  rfe [serve] [flags]      run the host daemon (default)
  rfe pair [-ttl 1h]       mint a pairing code + QR for a new phone
  rfe pair requests        list phones waiting for approval (match codes)
  rfe pair accept|reject [id]   answer a waiting phone (no id: the only waiting one)
  rfe devices              list paired devices
  rfe revoke <id>          block a device (accepts a unique id prefix)
  rfe remove <id>          permanently delete a device
  rfe jail <id> <path>     confine a device to <path> (empty "" clears it)
  rfe allow <id> <list>    set what a device may do: any of browse,download,upload,
                                  modify,delete,share, or all / none (replaces its grants)
  rfe backup-dir [<path>|none]  show or set the folder phone photo backups go to
                                  (must be inside an allowed folder; restart the agent to apply)
  rfe readonly <id> <on|off>  allow browse/download but block all writes
  rfe status               show name, addresses, fingerprint, devices
  rfe audit [-n 50]        show the account/device/share audit trail
  rfe adduser <username>   create the account used to log in from the
                                  phone app / web companion (prompts for a
                                  password, hidden input). Alternatively, use
                                  the Register tab in the app/web companion
                                  with a code from "rfe pair" to do
                                  this without the terminal.
  rfe install              set up per-user auto-start at login
                                  (systemd --user unit on Linux, launchd
                                  LaunchAgent on macOS, Scheduled Task on
                                  Windows — no root/admin required)
  rfe setup [-root <path>] first-run setup: restrict access to a folder,
                                  install the per-user service, and print a
                                  one-time pairing QR (defaults to your home)
  rfe start                start the installed per-user agent
  rfe stop                 stop it until you start it or next login
  rfe uninstall            stop and remove the auto-start entry

Common flags: -data <dir> (or $RFE_DATA_DIR; default ~/.rfe)

Flags may be given before or after a command's arguments:
  rfe pair accept -data D <id>   and   rfe pair accept <id> -data D
are the same. "--" ends the flags (everything after it is an argument, so a
value starting with "-" can be passed). An unknown flag, a missing argument or
an extra argument is an error naming it; nothing is ever silently ignored.
`)
}

// adminFlags is a flag.FlagSet for one admin subcommand. Parsing is interspersed:
// flags may come before, between or after the positional arguments, "--" ends
// flag parsing, and the number of positionals is checked, so no argument is
// silently dropped. Errors are returned (never os.Exit) after the usage is
// printed to stderr.
type adminFlags struct {
	*flag.FlagSet
	usage string
}

func newAdminFlags(name, usage string) *adminFlags {
	a := &adminFlags{FlagSet: flag.NewFlagSet(name, flag.ContinueOnError), usage: usage}
	a.SetOutput(os.Stderr)
	a.Usage = func() {
		fmt.Fprintf(a.Output(), "usage: rfe %s\n", a.usage)
		a.PrintDefaults()
	}
	return a
}

// parse parses args and returns the positional arguments, which must number
// between min and max. -h/--help returns flag.ErrHelp (handled by runAdmin).
func (a *adminFlags) parse(args []string, min, max int) ([]string, error) {
	pos, err := parseInterspersed(a.FlagSet, args)
	if err != nil {
		return nil, err // the flag package already printed the problem and the usage
	}
	if len(pos) > max {
		a.Usage()
		return nil, fmt.Errorf("unexpected argument %q (usage: rfe %s)", pos[max], a.usage)
	}
	if len(pos) < min {
		a.Usage()
		return nil, fmt.Errorf("missing argument (usage: rfe %s)", a.usage)
	}
	return pos, nil
}

// parseInterspersed parses fs's flags wherever they appear in args and returns the
// remaining positional arguments in order. The standard library stops at the first
// non-flag argument, which silently turned `accept <id> -data D` into positionals;
// this handles one flag at a time instead. "--" ends flag parsing: everything after
// it is positional. A lone "-" and any argument not starting with "-" is positional;
// any other argument starting with "-" is a flag, and an unknown or malformed one is
// an error from fs (fs must use flag.ContinueOnError).
func parseInterspersed(fs *flag.FlagSet, args []string) ([]string, error) {
	var positional []string
	for i := 0; i < len(args); {
		a := args[i]
		if a == "--" {
			return append(positional, args[i+1:]...), nil
		}
		if len(a) < 2 || a[0] != '-' {
			positional = append(positional, a)
			i++
			continue
		}
		// A flag, plus its value when that is the next argument (not for -f=v or bool flags).
		n := 1
		name := strings.TrimLeft(a, "-")
		if !strings.Contains(name, "=") && i+1 < len(args) {
			if f := fs.Lookup(name); f != nil {
				if b, ok := f.Value.(interface{ IsBoolFlag() bool }); !ok || !b.IsBoolFlag() {
					n = 2
				}
			}
		}
		if err := fs.Parse(args[i : i+n]); err != nil {
			return nil, err
		}
		i += n
	}
	return positional, nil
}

// rejectAdminArgs is for subcommands that take no arguments at all.
func rejectAdminArgs(cmd string, args []string) error {
	if len(args) == 0 {
		return nil
	}
	if args[0] == "-h" || args[0] == "-help" || args[0] == "--help" {
		printAdminUsage(os.Stderr)
		return flag.ErrHelp
	}
	return fmt.Errorf("unexpected argument %q (usage: rfe %s takes no arguments)", args[0], cmd)
}

// adminDataDir resolves the data dir: -data flag > $RFE_DATA_DIR > ~/.rfe.
// Delegates to defaultDataDir (main.go) so the admin CLI and the daemon always
// agree on which DB to open by default.
func adminDataDir(flagVal string) string {
	if flagVal != "" {
		return flagVal
	}
	return defaultDataDir()
}

func openAdminStore(dataDir string) (*store.DB, error) {
	if _, err := os.Stat(dataDir); err != nil {
		return nil, fmt.Errorf("data dir %q not found (is the agent set up? use -data)", dataDir)
	}
	return store.Open(dataDir)
}

// cmdPair mints a single-use pairing code and prints it with a scannable QR.
func cmdPair(args []string) error {
	if len(args) > 0 {
		switch args[0] {
		case "requests":
			return cmdPairRequests(args[1:])
		case "accept":
			return cmdPairDecide(args[1:], true)
		case "reject":
			return cmdPairDecide(args[1:], false)
		}
	}
	fs := newAdminFlags("pair", "pair [-ttl 1h] [-owner] [-addr <addr>] [-data <dir>]   (also: pair requests | pair accept [id] | pair reject [id])")
	data := fs.String("data", "", "agent data dir")
	addr := fs.String("addr", ":8765", "listen address the phone will dial (for the QR)")
	ttl := fs.Duration("ttl", pairing.DefaultTTL, "how long the code stays valid")
	owner := fs.Bool("owner", false, "mint a code that can also create the owner account (first-time setup)")
	if _, err := fs.parse(args, 0, 0); err != nil {
		return err
	}

	dir := adminDataDir(*data)
	// Check the data dir before LoadOrCreateCert, which would create it (and a
	// cert) for a mistyped -data or an agent that was never set up.
	db, err := openAdminStore(dir)
	if err != nil {
		return err
	}
	defer db.Close()
	cert, err := security.LoadOrCreateCert(dir)
	if err != nil {
		return fmt.Errorf("cert: %w", err)
	}
	fingerprint := security.Fingerprint(cert)
	lan, ts, _ := reachableAddresses(*addr)

	pm := pairing.New(db, lan, ts, fingerprint)
	mint := pm.Mint
	if *owner {
		mint = pm.MintOwner
	}
	code, payload, err := mint(*ttl)
	if err != nil {
		return fmt.Errorf("mint: %w", err)
	}

	fmt.Printf("Pairing code: %s   (expires in %s)\n", code, *ttl)
	fmt.Printf("Fingerprint:  %s\n", fingerprint)
	fmt.Printf("LAN:          %s\n", orNone(lan))
	fmt.Printf("Tailscale:    %s\n", orNone(ts))
	fmt.Println()

	qr, err := qrcode.New(payload.JSON(), qrcode.Medium)
	if err == nil {
		fmt.Println(qr.ToSmallString(false))
	} else {
		fmt.Printf("(could not draw the QR code: %v; enter the code and address by hand)\n", err)
	}
	fmt.Println("Scan in the app: Add computer → Scan QR.")
	if !*owner {
		if has, herr := db.HasAnyUser(); herr == nil && !has {
			fmt.Println("No account exists yet. To create the owner account from the app, run `rfe pair --owner` instead.")
		}
	}
	return nil
}

// cmdPairRequests lists phones waiting for approval, with their match codes.
func cmdPairRequests(args []string) error {
	fs := newAdminFlags("pair requests", "pair requests [-data <dir>]")
	data := fs.String("data", "", "agent data dir")
	if _, err := fs.parse(args, 0, 0); err != nil {
		return err
	}
	dir := adminDataDir(*data)
	db, err := openAdminStore(dir) // before LoadOrCreateCert, which creates the dir
	if err != nil {
		return err
	}
	defer db.Close()
	cert, err := security.LoadOrCreateCert(dir)
	if err != nil {
		return fmt.Errorf("cert: %w", err)
	}
	list, err := db.ListPendingPairRequests()
	if err != nil {
		return err
	}
	if len(list) == 0 {
		fmt.Println("No pairing requests waiting.")
		return nil
	}
	for _, r := range list {
		sas, err := pairing.SAS(security.Fingerprint(cert), r.ClientNonce, r.ID)
		if err != nil {
			// Never show a blank match code next to a request the owner might approve.
			fmt.Printf("%s  %-24s  from %-15s  match code unavailable (%v): do not approve\n", r.ID[:8], r.Label, r.RemoteIP, err)
			continue
		}
		fmt.Printf("%s  %-24s  from %-15s  match code %s\n", r.ID[:8], r.Label, r.RemoteIP, sas)
		if d, ok, _ := db.ClientDeviceByID(r.ClientID); ok && (d.Revoked || (d.PublicKey != "" && d.PublicKey != r.PublicKey)) {
			fmt.Printf("          replaces the paired device %q: new key and token, access reset to browse only\n", d.Label)
		}
	}
	fmt.Println("Approve with `rfe pair accept <id>` only if the code matches the one on the phone.")
	return nil
}

// cmdPairDecide approves or rejects a waiting request by (a prefix of) its id.
// With no id it answers the only waiting request (an error when 0 or more than 1 wait).
func cmdPairDecide(args []string, approve bool) error {
	word := "reject"
	if approve {
		word = "accept"
	}
	fs := newAdminFlags("pair "+word, "pair "+word+" [id] [-data <dir>]")
	data := fs.String("data", "", "agent data dir")
	pos, err := fs.parse(args, 0, 1)
	if err != nil {
		return err
	}
	if len(pos) > 0 && strings.TrimSpace(pos[0]) == "" {
		fs.Usage()
		return fmt.Errorf("the request id is empty; give the id from `rfe pair requests`")
	}
	db, err := openAdminStore(adminDataDir(*data))
	if err != nil {
		return err
	}
	defer db.Close()
	list, err := db.ListPendingPairRequests()
	if err != nil {
		return err
	}
	var match []string
	for _, r := range list {
		if len(pos) == 0 || strings.HasPrefix(r.ID, pos[0]) {
			match = append(match, r.ID)
		}
	}
	if len(match) != 1 {
		return fmt.Errorf("%d waiting requests match; give the id from `rfe pair requests`", len(match))
	}
	if err := db.DecidePairRequest(match[0], approve); err != nil {
		return err
	}
	if approve {
		fmt.Println("Approved. The phone finishes pairing within a couple of seconds.")
	} else {
		fmt.Println("Rejected.")
	}
	return nil
}

// cmdAddUser creates the account used by POST /v1/login (an additional way
// to obtain a device token, alongside the one-time pairing code above — not
// a replacement for it). Fails if the username already exists rather than
// silently overwriting its password.
func cmdAddUser(args []string) error {
	fs := newAdminFlags("adduser", "adduser <username> [-password <pw>] [-data <dir>]")
	data := fs.String("data", "", "agent data dir")
	passwordFlag := fs.String("password", "", "password (visible in ps and shell history; prefer the RFE_PASSWORD environment variable, or omit to be prompted with hidden input)")
	pos, err := fs.parse(args, 1, 1)
	if err != nil {
		return err
	}
	username := pos[0]

	password := *passwordFlag
	if password != "" {
		fmt.Fprintln(os.Stderr, "warning: -password is visible in the process list and shell history; use RFE_PASSWORD or the prompt instead")
	} else {
		password = os.Getenv("RFE_PASSWORD")
	}
	if password == "" {
		fmt.Print("Password: ")
		passwordBytes, err := term.ReadPassword(int(os.Stdin.Fd()))
		fmt.Println()
		if err != nil {
			return fmt.Errorf("read password: %w", err)
		}
		password = strings.TrimSpace(string(passwordBytes))
	}
	if len(password) < 8 {
		return fmt.Errorf("password must be at least 8 characters")
	}
	if len(password) > security.MaxPasswordBytes {
		return fmt.Errorf("password must be at most %d bytes", security.MaxPasswordBytes)
	}

	hash, err := security.HashPassword(password)
	if err != nil {
		return fmt.Errorf("hash password: %w", err)
	}

	db, err := openAdminStore(adminDataDir(*data))
	if err != nil {
		return err
	}
	defer db.Close()

	if err := db.CreateUser(username, hash); err != nil {
		return fmt.Errorf("create user %q (does it already exist?): %w", username, err)
	}
	fmt.Printf("Account %q created — log in from the phone app or web companion with this username and password.\n", username)
	return nil
}

// cmdDevices lists paired devices as a table.
func cmdDevices(args []string) error {
	fs := newAdminFlags("devices", "devices [-data <dir>]")
	data := fs.String("data", "", "agent data dir")
	if _, err := fs.parse(args, 0, 0); err != nil {
		return err
	}

	db, err := openAdminStore(adminDataDir(*data))
	if err != nil {
		return err
	}
	defer db.Close()

	devices, err := db.ListDevices()
	if err != nil {
		return err
	}
	if len(devices) == 0 {
		fmt.Println("No paired devices. Run `rfe pair` to add one.")
		return nil
	}

	tw := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
	fmt.Fprintln(tw, "ID\tLABEL\tSTATUS\tACCESS\tLAST SEEN")
	for _, d := range devices {
		status := "active"
		if d.Revoked {
			status = "revoked"
		}
		access := "read-write"
		if d.ReadOnly {
			access = "read-only"
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n",
			shortID(d.ID), d.Label, status, access, humanizeSince(d.LastSeen))
	}
	return tw.Flush()
}

// cmdAudit prints the audit trail newest-first — the PC-side counterpart of
// GET /v1/audit, so the host owner can read it without a paired device.
func cmdAudit(args []string) error {
	fs := newAdminFlags("audit", "audit [-n 50] [-data <dir>]")
	data := fs.String("data", "", "agent data dir")
	n := fs.Int("n", 50, "number of entries to show")
	if _, err := fs.parse(args, 0, 0); err != nil {
		return err
	}

	db, err := openAdminStore(adminDataDir(*data))
	if err != nil {
		return err
	}
	defer db.Close()

	entries, err := db.AuditEntries(*n, 0)
	if err != nil {
		return err
	}
	if len(entries) == 0 {
		fmt.Println("No audit entries yet.")
		return nil
	}

	tw := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
	fmt.Fprintln(tw, "WHEN\tACTION\tACTOR\tTARGET\tDETAIL")
	for _, e := range entries {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n",
			e.At.Format("2006-01-02 15:04"), e.Action, e.Actor, e.Target, e.Detail)
	}
	return tw.Flush()
}

// cmdRevokeOrRemove revokes (remove=false) or permanently deletes (remove=true)
// the device whose id (or unique id prefix) is given.
func cmdRevokeOrRemove(args []string, remove bool) error {
	fs := newAdminFlags(verb(remove), verb(remove)+" <device-id> [-data <dir>]")
	data := fs.String("data", "", "agent data dir")
	pos, err := fs.parse(args, 1, 1)
	if err != nil {
		return err
	}

	db, err := openAdminStore(adminDataDir(*data))
	if err != nil {
		return err
	}
	defer db.Close()

	id, err := db.ResolveDeviceID(pos[0])
	if err != nil {
		return err
	}
	label := deviceLabel(db, id)

	if remove {
		if err := db.DeleteDevice(id); err != nil {
			return err
		}
		cliAudit(db, store.AuditDeviceRemoved, id, "")
		fmt.Printf("Removed %q (%s)\n", label, shortID(id))
	} else {
		if err := db.RevokeDevice(id); err != nil {
			return err
		}
		cliAudit(db, store.AuditDeviceRevoked, id, "")
		fmt.Printf("Revoked %q (%s)\n", label, shortID(id))
	}
	return nil
}

// cmdJail sets (or clears) a device's per-device path jail (H2). This used to
// be reachable via PATCH /v1/devices/{id} from the app; that route now
// returns 403 for all app callers (device access limits are a PC-side
// concern), so this CLI command is the only way to configure it.
//
// Pass an empty string ("") as <path> to clear a device's per-device jail
// (it then falls back to the agent's configured global roots, as before).
// A non-empty <path> must be an absolute path that resolves within the
// agent's configured global roots — the same containment rule the daemon
// enforces, via server.ValidateJailRoot/server.SetDeviceJail.
func cmdJail(args []string) error {
	fs := newAdminFlags("jail", "jail <device-id> <path> [-data <dir>]  (pass \"\" for <path> to clear)")
	data := fs.String("data", "", "agent data dir")
	pos, err := fs.parse(args, 2, 2)
	if err != nil {
		return err
	}

	dir := adminDataDir(*data)
	db, err := openAdminStore(dir)
	if err != nil {
		return err
	}
	defer db.Close()

	id, err := db.ResolveDeviceID(pos[0])
	if err != nil {
		return err
	}
	label := deviceLabel(db, id)

	// Load the agent's configured global roots the same way the daemon
	// does (settings.Load reads the persisted "roots" config key once it
	// exists; passing nil seed roots here is a no-op against an existing
	// DB — it only seeds on a brand-new store, which the daemon's first
	// run would already have done).
	if ok, err := settings.Initialised(db); err != nil {
		return fmt.Errorf("settings: %w", err)
	} else if !ok {
		return fmt.Errorf("this agent has not been started yet; run it (or `rfe setup`) once so its folder settings exist")
	}
	st, err := settings.Load(db, false, nil, "")
	if err != nil {
		return fmt.Errorf("settings: %w", err)
	}

	jailRoot := pos[1]
	clean, err := server.SetDeviceJail(db, id, jailRoot, st.Roots(), st.IsReadOnly())
	if err != nil {
		return err
	}

	cliAudit(db, store.AuditDeviceUpdated, id, "jail="+clean)
	if clean == "" {
		fmt.Printf("Cleared jail for %q (%s)\n", label, shortID(id))
	} else {
		fmt.Printf("Jailed %q (%s) to %s\n", label, shortID(id), clean)
	}
	return nil
}

// cmdReadonly sets or clears a device's per-device read-only flag (#8). Like
// jail, this is a PC-side concern with no app-facing route. A read-only device
// can browse and download but every filesystem write is rejected.
func cmdReadonly(args []string) error {
	fs := newAdminFlags("readonly", "readonly <device-id> <on|off> [-data <dir>]")
	data := fs.String("data", "", "agent data dir")
	pos, err := fs.parse(args, 2, 2)
	if err != nil {
		return err
	}

	db, err := openAdminStore(adminDataDir(*data))
	if err != nil {
		return err
	}
	defer db.Close()

	id, err := db.ResolveDeviceID(pos[0])
	if err != nil {
		return err
	}
	ro, err := parseOnOff(pos[1])
	if err != nil {
		return err
	}
	if err := db.SetDeviceReadOnly(id, ro); err != nil {
		return err
	}
	label := deviceLabel(db, id)
	cliAudit(db, store.AuditDeviceUpdated, id, fmt.Sprintf("readOnly=%v", ro))
	if ro {
		fmt.Printf("Set %q (%s) to read-only\n", label, shortID(id))
	} else {
		fmt.Printf("Set %q (%s) to read-write\n", label, shortID(id))
	}
	return nil
}

// parseOnOff accepts on/off (and true/false, 1/0, yes/no, case-insensitive).
func parseOnOff(s string) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "on", "true", "1", "yes":
		return true, nil
	case "off", "false", "0", "no":
		return false, nil
	default:
		return false, fmt.Errorf("invalid value %q: use on or off", s)
	}
}

// cmdStatus prints a one-glance summary of the agent.
func cmdStatus(args []string) error {
	fs := newAdminFlags("status", "status [-addr <addr>] [-data <dir>]")
	data := fs.String("data", "", "agent data dir")
	addr := fs.String("addr", ":8765", "listen address (for resolving reachable IPs)")
	if _, err := fs.parse(args, 0, 0); err != nil {
		return err
	}

	dir := adminDataDir(*data)
	db, err := openAdminStore(dir)
	if err != nil {
		return err
	}
	defer db.Close()

	name, _ := db.GetConfig("agentName")
	if name == "" {
		name = hostName()
	}
	lan, ts, _ := reachableAddresses(*addr)
	devices, _ := db.ListDevices()
	active := 0
	for _, d := range devices {
		if !d.Revoked {
			active++
		}
	}

	fingerprint := "(no cert yet)"
	if cert, err := security.LoadOrCreateCert(dir); err == nil {
		fingerprint = security.Fingerprint(cert)
	}

	fmt.Printf("name:        %s\n", name)
	fmt.Printf("version:     %s\n", version)
	fmt.Printf("LAN:         %s\n", orNone(lan))
	fmt.Printf("Tailscale:   %s\n", orNone(ts))
	fmt.Printf("fingerprint: %s\n", fingerprint)
	fmt.Printf("devices:     %d active, %d total\n", active, len(devices))
	service, err := serviceStatus()
	if err != nil {
		return fmt.Errorf("read local service status: %w", err)
	}
	fmt.Printf("service:     %s\n", service)
	printSidecarStatus(dir)
	return nil
}

func printSidecarStatus(dir string) {
	for i, line := range server.SidecarReport() {
		label := "sidecars:"
		if i > 0 {
			label = ""
		}
		fmt.Printf("%-12s %s\n", label, line)
	}
	rep, age, err := server.ReadSidecarStats(dir)
	if err != nil {
		return
	}
	state := fmt.Sprintf("counters as of %s ago", age.Round(time.Second))
	if age > 2*time.Minute {
		state += " (stale: the agent is not running)"
	}
	fmt.Printf("%-12s %s\n", "", state)
	for _, st := range rep.Sidecars {
		running := "down"
		if st.Running {
			running = "running " + st.Version
		}
		breaker := ""
		if st.BreakerOpen {
			breaker = ", BREAKER OPEN"
		}
		fmt.Printf("%-12s %s: %s; starts %d, start failures %d, crashes %d, breaker opens %d%s\n",
			"", st.Name, running, st.Starts, st.StartFailures, st.Crashes, st.BreakerOpens, breaker)
	}
	names := make([]string, 0, len(rep.Events))
	for k := range rep.Events {
		names = append(names, k)
	}
	sort.Strings(names)
	if len(names) == 0 {
		fmt.Printf("%-12s fallbacks: none\n", "")
	}
	for _, k := range names {
		fmt.Printf("%-12s event %s: %d\n", "", k, rep.Events[k])
	}
}

// --- helpers ---

func verb(remove bool) string {
	if remove {
		return "remove"
	}
	return "revoke"
}

func shortID(id string) string {
	if len(id) > 8 {
		return id[:8]
	}
	return id
}

func deviceLabel(db *store.DB, id string) string {
	devices, err := db.ListDevices()
	if err != nil {
		return "device"
	}
	for _, d := range devices {
		if d.ID == id {
			return d.Label
		}
	}
	return "device"
}

// humanizeSince renders a coarse relative time like "2m ago" / "3h ago".
func humanizeSince(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	}
}

// parseAllow turns "download,upload" (or "all" / "none") into the six file grants.
func parseAllow(list string) (grants [6]bool, err error) {
	names := []string{"browse", "download", "upload", "modify", "delete", "share"}
	switch strings.ToLower(strings.TrimSpace(list)) {
	case "all":
		return [6]bool{true, true, true, true, true, true}, nil
	case "none", "":
		return grants, nil
	}
	for _, part := range strings.Split(list, ",") {
		part = strings.ToLower(strings.TrimSpace(part))
		found := false
		for i, n := range names {
			if n == part {
				grants[i], found = true, true
			}
		}
		if !found {
			return grants, fmt.Errorf("unknown permission %q (use %s, all or none)", part, strings.Join(names, ", "))
		}
	}
	return grants, nil
}

// cmdAllow replaces a device's file permissions.
func cmdAllow(args []string) error {
	fs := newAdminFlags("allow", "allow <device-id> <browse,download,upload,modify,delete,share|all|none> [-data <dir>]")
	data := fs.String("data", "", "agent data dir")
	pos, err := fs.parse(args, 2, 2)
	if err != nil {
		return err
	}
	g, err := parseAllow(pos[1])
	if err != nil {
		return err
	}
	db, err := openAdminStore(adminDataDir(*data))
	if err != nil {
		return err
	}
	defer db.Close()
	id, err := db.ResolveDeviceID(pos[0])
	if err != nil {
		return err
	}
	if err := db.SetDeviceFilePermissions(id, g[0], g[1], g[2], g[3], g[4], g[5]); err != nil {
		return err
	}
	cliAudit(db, store.AuditDeviceUpdated, id, "permissions="+pos[1])
	fmt.Printf("Set %q (%s) permissions to %s\n", deviceLabel(db, id), shortID(id), pos[1])
	return nil
}

// backupDirInRoots reports whether dir is one of the allowed roots or inside one (no roots means unrestricted).
func backupDirInRoots(dir string, roots []string) bool {
	if len(roots) == 0 {
		return true
	}
	for _, r := range roots {
		rel, err := filepath.Rel(filepath.Clean(r), dir)
		if err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

// cmdBackupDir shows or sets the destination folder for phone photo backup.
func cmdBackupDir(args []string) error {
	fs := newAdminFlags("backup-dir", "backup-dir [<path>|none] [-data <dir>]")
	data := fs.String("data", "", "agent data dir")
	pos, err := fs.parse(args, 0, 1)
	if err != nil {
		return err
	}
	db, err := openAdminStore(adminDataDir(*data))
	if err != nil {
		return err
	}
	defer db.Close()
	if len(pos) == 0 {
		cur, _ := db.GetConfig("photoBackupRoot")
		fmt.Println("photo backup folder:", orNone(cur))
		return nil
	}
	arg := strings.TrimSpace(pos[0])
	if arg == "none" || arg == "" {
		if err := db.SetConfig("photoBackupRoot", ""); err != nil {
			return err
		}
		fmt.Println("Photo backup folder cleared. Restart the agent to apply.")
		return nil
	}
	dir, err := filepath.Abs(arg)
	if err != nil {
		return err
	}
	rootsCSV, _ := db.GetConfig("roots")
	var roots []string
	for _, r := range strings.Split(rootsCSV, "\n") {
		if r = strings.TrimSpace(r); r != "" {
			roots = append(roots, r)
		}
	}
	if !backupDirInRoots(dir, roots) {
		return fmt.Errorf("%s is outside the folders this agent shares (%s); phones could not write there", dir, strings.Join(roots, ", "))
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	if err := db.SetConfig("photoBackupRoot", dir); err != nil {
		return err
	}
	fmt.Println("Photo backup folder set to", dir, "— restart the agent to apply.")
	return nil
}

// cliActor is the audit actor for changes made from the host's own terminal.
const cliActor = "host owner (CLI)"

// cliAudit records a device change made through the admin CLI, so the audit
// trail matches what the same change through the HTTP API would have written.
func cliAudit(db *store.DB, action, target, detail string) {
	if err := db.AppendAudit(action, cliActor, target, detail); err != nil {
		fmt.Fprintf(os.Stderr, "warning: could not record the audit entry: %v\n", err)
	}
}
