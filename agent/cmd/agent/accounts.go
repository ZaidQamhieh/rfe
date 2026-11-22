package main

import (
	"bufio"
	"context"
	"crypto/ed25519"
	"crypto/subtle"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/ZaidQamhieh/rfe/agent/internal/security"
	"github.com/ZaidQamhieh/rfe/agent/internal/store"
)

// accountsConfirmInput is where "rfe accounts trust" reads the owner's yes/no from; tests replace it.
var accountsConfirmInput io.Reader = os.Stdin

const (
	accountsFetchTimeout = 10 * time.Second
	accountsMaxResponse  = 64 << 10
	accountsMaxName      = 64
	accountsMaxSubject   = 128 // the longest account name a federated login accepts
	accountsUsage        = "accounts trust <host:port> [--yes --fingerprint <hex>] | untrust | status | enable [--name <name>] | disable | rotate-key | revoke-all | deny <issuer-fingerprint> <account> | allow <issuer-fingerprint> <account> | denied"
)

// cmdAccounts dispatches the "rfe accounts" subcommands: trust, untrust, status, enable, disable, rotate-key,
// revoke-all, deny, allow, denied.
func cmdAccounts(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("missing action (usage: rfe %s)", accountsUsage)
	}
	action, rest := args[0], args[1:]
	switch action {
	case "trust":
		return cmdAccountsTrust(rest)
	case "untrust":
		return cmdAccountsUntrust(rest)
	case "status":
		return cmdAccountsStatus(rest)
	case "enable":
		return cmdAccountsEnable(rest)
	case "disable":
		return cmdAccountsDisable(rest)
	case "rotate-key":
		return cmdAccountsRotateKey(rest)
	case "revoke-all":
		return cmdAccountsRevokeAll(rest)
	case "deny":
		return cmdAccountsDenyAllow(rest, true)
	case "allow":
		return cmdAccountsDenyAllow(rest, false)
	case "denied":
		return cmdAccountsDenied(rest)
	case "-h", "-help", "--help":
		fmt.Fprintf(os.Stderr, "usage: rfe %s\n", accountsUsage)
		return flag.ErrHelp
	}
	return fmt.Errorf("unknown accounts action %q (usage: rfe %s)", action, accountsUsage)
}

// parseAccountsHost accepts only "host:port" (no scheme, user info or path) so the key is always
// fetched from exactly the machine the owner typed.
func parseAccountsHost(s string) (string, error) {
	if s == "" || strings.ContainsAny(s, "/@?#\\ ") {
		return "", fmt.Errorf("%q is not a host:port (no scheme, user or path)", s)
	}
	host, port, err := net.SplitHostPort(s)
	if err != nil || host == "" {
		return "", fmt.Errorf("%q is not a host:port", s)
	}
	if n, err := strconv.Atoi(port); err != nil || n < 1 || n > 65535 {
		return "", fmt.Errorf("%q has an invalid port", s)
	}
	return s, nil
}

// fetchAccountKey asks the agent at hostPort for its account-server public key. TLS verification is off
// on purpose: agents use self-signed certificates, and what is trusted is the signing key, whose
// fingerprint this command computes locally and the owner compares with the one shown on the account
// server's own machine before confirming (the same trust-on-first-use step as pairing).
func fetchAccountKey(hostPort string) (name, keyB64 string, err error) {
	client := &http.Client{
		Timeout:       accountsFetchTimeout,
		Transport:     &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true, MinVersion: tls.VersionTLS12}}, // #nosec G402 -- see above
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	ctx, cancel := context.WithTimeout(context.Background(), accountsFetchTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://"+hostPort+"/v1/accounts/key", nil)
	if err != nil {
		return "", "", err
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", "", fmt.Errorf("could not reach %s: %w", hostPort, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, accountsMaxResponse))
	if err != nil {
		return "", "", fmt.Errorf("reading the answer from %s: %w", hostPort, err)
	}
	var out struct {
		Name      string `json:"name"`
		PublicKey string `json:"publicKey"`
		Code      string `json:"code"`
		Message   string `json:"message"`
	}
	if jerr := json.Unmarshal(body, &out); jerr != nil && resp.StatusCode == http.StatusOK {
		return "", "", fmt.Errorf("%s did not answer like an account server", hostPort)
	}
	if resp.StatusCode != http.StatusOK {
		why := out.Code
		if why == "" {
			why = "HTTP " + strconv.Itoa(resp.StatusCode)
		}
		return "", "", fmt.Errorf("%s refused: %s (is it an agent with the account server turned on? run \"rfe accounts enable\" there)", hostPort, why)
	}
	return out.Name, out.PublicKey, nil
}

// cleanAccountName turns the server's self-reported name into a safe display label, falling back to fallback.
func cleanAccountName(name, fallback string) string {
	name = strings.TrimSpace(name)
	if name == "" || len(name) > accountsMaxName {
		return fallback
	}
	for _, r := range name {
		if unicode.IsControl(r) {
			return fallback
		}
	}
	return name
}

// normaliseFingerprint lower-cases a hex fingerprint and drops the separators people paste with it (colons,
// spaces, dashes), so "AA:BB" and "aabb" compare equal.
func normaliseFingerprint(s string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r == ':' || r == '-' || unicode.IsSpace(r):
			return -1
		}
		return unicode.ToLower(r)
	}, s)
}

// cmdAccountsTrust fetches the account server's key, shows its fingerprint (computed here, never taken
// from the server's own claim) and pins the key. Without --yes the owner confirms interactively; --yes
// skips that question only when --fingerprint names the fingerprint the owner compared, so a script can
// never pin a key it was not shown. A fingerprint that does not match refuses and changes nothing.
func cmdAccountsTrust(args []string) error {
	fs := newAdminFlags("accounts trust", "accounts trust <host:port> [--yes --fingerprint <hex>] [-data <dir>]")
	data := fs.String("data", "", "agent data dir")
	yes := fs.Bool("yes", false, "pin the key without asking; requires --fingerprint")
	wantFp := fs.String("fingerprint", "", "the key fingerprint you compared on the account server's machine (required with --yes)")
	pos, err := fs.parse(args, 1, 1)
	if err != nil {
		return err
	}
	if *yes && normaliseFingerprint(*wantFp) == "" {
		return errors.New("--yes needs --fingerprint <hex>: the fingerprint \"rfe accounts status\" prints on the account server; nothing was changed")
	}
	hostPort, err := parseAccountsHost(pos[0])
	if err != nil {
		return err
	}
	db, err := openAdminStore(adminDataDir(*data))
	if err != nil {
		return err
	}
	defer db.Close()

	name, keyB64, err := fetchAccountKey(hostPort)
	if err != nil {
		return err
	}
	raw, derr := base64.StdEncoding.DecodeString(keyB64)
	if derr != nil || len(raw) != ed25519.PublicKeySize {
		return fmt.Errorf("%s sent a key that is not an Ed25519 public key", hostPort)
	}
	fp := security.KeyFingerprint(ed25519.PublicKey(raw))
	if want := normaliseFingerprint(*wantFp); want != "" && subtle.ConstantTimeCompare([]byte(want), []byte(fp)) != 1 {
		return fmt.Errorf("%s's key fingerprint is %s, not the one given with --fingerprint; nothing was changed", hostPort, fp)
	}
	name = cleanAccountName(name, hostPort)
	fmt.Printf("Account server: %s (%s)\nKey fingerprint: %s\n", name, hostPort, fp)
	fmt.Println("Compare this fingerprint with the one \"rfe accounts status\" prints on that machine.")
	fmt.Println("Trusting it lets that server's accounts log in to this PC with full owner access.")
	if !*yes {
		fmt.Print("Trust this account server? [y/N] ")
		line, _ := bufio.NewReader(accountsConfirmInput).ReadString('\n')
		if answer := strings.ToLower(strings.TrimSpace(line)); answer != "y" && answer != "yes" {
			fmt.Println()
			return errors.New("not confirmed; nothing was changed")
		}
	}
	if _, err := db.SetTrustedAccountServer(name, keyB64); err != nil {
		return err
	}
	cliAudit(db, store.AuditAccountServer, name, "trusted account server key "+fp)
	fmt.Println("Trusted. Sessions from any previously trusted server were ended.")
	return nil
}

// cmdAccountsUntrust forgets the pinned account server and ends every session it vouched for.
func cmdAccountsUntrust(args []string) error {
	fs := newAdminFlags("accounts untrust", "accounts untrust [-data <dir>]")
	data := fs.String("data", "", "agent data dir")
	if _, err := fs.parse(args, 0, 0); err != nil {
		return err
	}
	db, err := openAdminStore(adminDataDir(*data))
	if err != nil {
		return err
	}
	defer db.Close()
	trusted, err := db.TrustedAccountServer()
	if err != nil {
		return err
	}
	if trusted == nil {
		fmt.Println("No account server is trusted.")
		return nil
	}
	if err := db.RemoveTrustedAccountServer(); err != nil {
		return err
	}
	cliAudit(db, store.AuditAccountServer, trusted.Name, "no longer trusting account server key "+trusted.Fingerprint)
	fmt.Printf("No longer trusting %s (%s). Its sessions on this PC were ended.\n", trusted.Name, trusted.Fingerprint)
	return nil
}

// cmdAccountsStatus prints whether this agent is an account server and which server it trusts.
func cmdAccountsStatus(args []string) error {
	fs := newAdminFlags("accounts status", "accounts status [-data <dir>]  (after \"accounts rotate-key\" the fingerprint shown here is the new one; members must re-pin it with \"accounts trust\")")
	data := fs.String("data", "", "agent data dir")
	if _, err := fs.parse(args, 0, 0); err != nil {
		return err
	}
	db, err := openAdminStore(adminDataDir(*data))
	if err != nil {
		return err
	}
	defer db.Close()
	role, err := db.AccountServer()
	if err != nil {
		return err
	}
	if role.Enabled {
		key, err := db.AccountSigningKey()
		if err != nil {
			return err
		}
		name := role.Name
		if name == "" {
			name = "(agent name)"
		}
		fmt.Printf("Account server role: on, name %s\nSigning key fingerprint: %s\n", name, security.KeyFingerprint(key.Public().(ed25519.PublicKey)))
	} else {
		fmt.Println("Account server role: off")
	}
	trusted, err := db.TrustedAccountServer()
	if err != nil {
		return err
	}
	if trusted == nil {
		fmt.Println("Trusted account server: none")
		return nil
	}
	fmt.Printf("Trusted account server: %s, key fingerprint %s\n", trusted.Name, trusted.Fingerprint)
	return nil
}

// cmdAccountsEnable turns the account-server role on (creating its signing key the first time).
func cmdAccountsEnable(args []string) error {
	fs := newAdminFlags("accounts enable", "accounts enable [--name <name>] [-data <dir>]")
	data := fs.String("data", "", "agent data dir")
	name := fs.String("name", "", "display name peers see (default: the agent's name)")
	if _, err := fs.parse(args, 0, 0); err != nil {
		return err
	}
	label := strings.TrimSpace(*name)
	if label != "" && cleanAccountName(label, "") == "" {
		return fmt.Errorf("--name must be at most %d characters without control characters", accountsMaxName)
	}
	db, err := openAdminStore(adminDataDir(*data))
	if err != nil {
		return err
	}
	defer db.Close()
	cur, err := db.AccountServer()
	if err != nil {
		return err
	}
	if label == "" {
		label = cur.Name
	}
	if err := db.SetAccountServer(true, label); err != nil {
		return err
	}
	key, err := db.AccountSigningKey()
	if err != nil {
		return err
	}
	fp := security.KeyFingerprint(key.Public().(ed25519.PublicKey))
	cliAudit(db, store.AuditAccountServer, label, "account server role enabled, key "+fp)
	fmt.Printf("Account server role on. Key fingerprint: %s\n", fp)
	return nil
}

// cmdAccountsRotateKey replaces this agent's account-server signing key with a new one, keeping the role
// as it was, and prints the new fingerprint. Members that pinned the old key must re-pin the new one.
func cmdAccountsRotateKey(args []string) error {
	fs := newAdminFlags("accounts rotate-key", "accounts rotate-key [-data <dir>]")
	data := fs.String("data", "", "agent data dir")
	if _, err := fs.parse(args, 0, 0); err != nil {
		return err
	}
	db, err := openAdminStore(adminDataDir(*data))
	if err != nil {
		return err
	}
	defer db.Close()
	role, err := db.AccountServer()
	if err != nil {
		return err
	}
	key, err := db.RotateAccountKey()
	if err != nil {
		return err
	}
	fp := security.KeyFingerprint(key.Public().(ed25519.PublicKey))
	cliAudit(db, store.AuditAccountServer, role.Name, "account server key rotated, new key "+fp)
	fmt.Printf("Account server key rotated. New key fingerprint: %s\n", fp)
	fmt.Println("WARNING: every member that trusted the old key can no longer log in with this server's accounts.")
	fmt.Println("On each member run \"rfe accounts trust <host:port>\" and compare the fingerprint above; that ends its sessions from this server.")
	return nil
}

// cmdAccountsRevokeAll raises this agent's account epoch: every member that sees the new epoch ends the
// sessions it minted from this server's assertions, and refuses assertions signed before it.
func cmdAccountsRevokeAll(args []string) error {
	fs := newAdminFlags("accounts revoke-all", "accounts revoke-all [-data <dir>]")
	data := fs.String("data", "", "agent data dir")
	if _, err := fs.parse(args, 0, 0); err != nil {
		return err
	}
	db, err := openAdminStore(adminDataDir(*data))
	if err != nil {
		return err
	}
	defer db.Close()
	epoch, err := db.BumpAccountEpoch("revoke-all from CLI")
	if err != nil {
		return err
	}
	fmt.Printf("Account epoch raised to %d.\n", epoch)
	fmt.Println("Each member ends this server's sessions the next time one of its users logs in with it, and at the latest when the 10-minute session lapses.")
	return nil
}

// cmdAccountsDenyAllow adds (deny=true) or removes (deny=false) one account on the deny list. The account is
// named by the issuing account server's key fingerprint (as shown by "rfe accounts status") and its name there.
// Denying also ends the account's sessions on this PC.
func cmdAccountsDenyAllow(args []string, deny bool) error {
	verb := "allow"
	if deny {
		verb = "deny"
	}
	fs := newAdminFlags("accounts "+verb, "accounts "+verb+" <issuer-fingerprint> <account> [-data <dir>]")
	data := fs.String("data", "", "agent data dir")
	pos, err := fs.parse(args, 2, 2)
	if err != nil {
		return err
	}
	issuer, sub := normaliseFingerprint(pos[0]), pos[1]
	if issuer == "" || !validDenySubject(sub) {
		return fmt.Errorf("need an issuer fingerprint and an account name (usage: rfe accounts %s <issuer-fingerprint> <account>)", verb)
	}
	db, err := openAdminStore(adminDataDir(*data))
	if err != nil {
		return err
	}
	defer db.Close()
	if deny {
		err = db.DenyAccount(issuer, sub)
	} else {
		err = db.AllowAccount(issuer, sub)
	}
	if err != nil {
		return err
	}
	fp := issuer
	if len(fp) > store.AuditFingerprintLen {
		fp = fp[:store.AuditFingerprintLen]
	}
	cliAudit(db, store.AuditAccountServer, sub+"@"+fp, "deny list: "+verb+" from CLI")
	if deny {
		fmt.Printf("Denied %s (issuer %s). Its sessions on this PC were ended and it can no longer log in here.\n", sub, issuer)
	} else {
		fmt.Printf("Allowed %s (issuer %s). It can log in here again.\n", sub, issuer)
	}
	return nil
}

// validDenySubject reports whether s can be an account name an account server vouches for: non-empty,
// within the length a login accepts, and free of control characters.
func validDenySubject(s string) bool {
	if s == "" || len(s) > accountsMaxSubject {
		return false
	}
	for _, r := range s {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}

// cmdAccountsDenied prints the deny list.
func cmdAccountsDenied(args []string) error {
	fs := newAdminFlags("accounts denied", "accounts denied [-data <dir>]")
	data := fs.String("data", "", "agent data dir")
	if _, err := fs.parse(args, 0, 0); err != nil {
		return err
	}
	db, err := openAdminStore(adminDataDir(*data))
	if err != nil {
		return err
	}
	defer db.Close()
	list, err := db.DeniedAccounts()
	if err != nil {
		return err
	}
	if len(list) == 0 {
		fmt.Println("No members are denied.")
		return nil
	}
	for _, d := range list {
		fmt.Printf("%s\t%s\t%s\n", d.Issuer, d.Sub, d.Created.Format("2006-01-02 15:04"))
	}
	return nil
}

// cmdAccountsDisable turns the role off; the signing key stays so servers that pinned it keep working
// if it is switched back on.
func cmdAccountsDisable(args []string) error {
	fs := newAdminFlags("accounts disable", "accounts disable [-data <dir>]")
	data := fs.String("data", "", "agent data dir")
	if _, err := fs.parse(args, 0, 0); err != nil {
		return err
	}
	db, err := openAdminStore(adminDataDir(*data))
	if err != nil {
		return err
	}
	defer db.Close()
	cur, err := db.AccountServer()
	if err != nil {
		return err
	}
	if err := db.SetAccountServer(false, cur.Name); err != nil {
		return err
	}
	cliAudit(db, store.AuditAccountServer, cur.Name, "account server role disabled")
	fmt.Println("Account server role off.")
	return nil
}
