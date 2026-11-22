package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/ZaidQamhieh/rfe/agent/internal/security"
	"github.com/ZaidQamhieh/rfe/agent/internal/sidecar"
	"github.com/ZaidQamhieh/rfe/agent/internal/store"
)

// Check statuses in a selfcheckReport. Only fail makes the report fail; absent is a helper that is simply
// not installed (or switched off), skipped is a check that could not run because an earlier one failed.
const (
	checkPass    = "pass"
	checkFail    = "fail"
	checkAbsent  = "absent"
	checkSkipped = "skipped"
)

// Names of the checks, in the order they run.
const (
	checkBaseline  = "baseline"
	checkMigrate   = "migrate"
	checkIntegrity = "integrity"
	checkIdentity  = "identity-unchanged"
	checkTokens    = "tokens-unchanged"
	checkHelperFmt = "helper-%s"
)

// errSelfcheckFailed is returned by "rfe selfcheck" when any check failed; the per-check lines are already
// printed, so it only turns the exit status non-zero.
var errSelfcheckFailed = errors.New("selfcheck failed")

// selfcheckHelpers are the short names of the helper programs (rfe-indexd, rfe-thumbd) the daemon can start.
var selfcheckHelpers = []string{"indexd", "thumbd"}

// selfcheckAfterMigrate, when set (tests only), runs after the database was migrated and checked and before
// the identity and token hashes are compared again, so a test can change a file "mid-run".
var selfcheckAfterMigrate func(dir string)

// selfcheckCheck is the outcome of one check.
type selfcheckCheck struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	Detail string `json:"detail"`
}

// selfcheckReport is the outcome of runSelfcheck: OK is true only when no check failed.
type selfcheckReport struct {
	Dir    string           `json:"dir"`
	OK     bool             `json:"ok"`
	Checks []selfcheckCheck `json:"checks"`
}

// add appends one check result.
func (r *selfcheckReport) add(name, status, format string, args ...any) {
	r.Checks = append(r.Checks, selfcheckCheck{Name: name, Status: status, Detail: fmt.Sprintf(format, args...)})
}

// status returns the status recorded for the named check ("" when it did not run).
func (r *selfcheckReport) status(name string) string {
	for _, c := range r.Checks {
		if c.Name == name {
			return c.Status
		}
	}
	return ""
}

// err summarises the failed checks as one error, or nil when the report is OK.
func (r *selfcheckReport) err() error {
	if r.OK {
		return nil
	}
	var bad []string
	for _, c := range r.Checks {
		if c.Status == checkFail {
			bad = append(bad, c.Name+": "+c.Detail)
		}
	}
	return fmt.Errorf("selfcheck of %s failed: %s", r.Dir, strings.Join(bad, "; "))
}

// identityFiles are the files of the data dir that make this agent the one phones trust; a migration must
// never change them. The account-server key exists only on an agent that acts as an account server.
var identityFiles = []string{security.CertFile, security.KeyFile, store.AccountKeyFile}

// identityDigests returns the SHA-256 (lowercase hex) of each identity file in dir, or "absent" for a file that
// does not exist. It never creates anything.
func identityDigests(dir string) (map[string]string, error) {
	out := map[string]string{}
	for _, name := range identityFiles {
		f, err := os.Open(filepath.Join(dir, name))
		if errors.Is(err, fs.ErrNotExist) {
			out[name] = "absent"
			continue
		}
		if err != nil {
			return nil, err
		}
		h := sha256.New()
		_, err = io.Copy(h, f)
		f.Close()
		if err != nil {
			return nil, fmt.Errorf("%s: %w", name, err)
		}
		out[name] = hex.EncodeToString(h.Sum(nil))
	}
	return out, nil
}

// sameDigests lists the names whose digest differs between a and b.
func sameDigests(a, b map[string]string) (changed []string) {
	for _, name := range identityFiles {
		if a[name] != b[name] {
			changed = append(changed, name)
		}
	}
	return changed
}

// runSelfcheck is the whole upgrade self-test, run against ONE data directory that must be a COPY (a
// restored backup, say): it migrates agent.db in place, so it must never be pointed at a directory a running
// daemon uses. It records the identity files and token hashes, opens and migrates the database to this
// build's schema, runs integrity_check, proves the recorded files and hashes are unchanged, and starts each
// helper program that is installed to complete its handshake. A missing helper is "absent", not a failure.
// Phase 5 (the upgrade orchestrator) calls this same function.
func runSelfcheck(dir string) (rep selfcheckReport) {
	rep.Dir = dir
	defer func() {
		rep.OK = true
		for _, c := range rep.Checks {
			if c.Status == checkFail {
				rep.OK = false
			}
		}
	}()
	if info, err := os.Stat(filepath.Join(dir, store.DBFile)); err != nil || !info.Mode().IsRegular() {
		rep.add(checkBaseline, checkFail, "no %s in %s", store.DBFile, dir)
		return rep
	}

	idBefore, idErr := identityDigests(dir)
	tokBefore, tokErr := store.TokenHashDigest(dir)
	switch {
	case idErr != nil:
		rep.add(checkBaseline, checkFail, "read identity files: %v", idErr)
	case tokErr != nil:
		rep.add(checkBaseline, checkFail, "read token hashes: %v", tokErr)
	default:
		rep.add(checkBaseline, checkPass, "recorded %d identity files and the token-hash digest %s", len(identityFiles), tokBefore[:12])
	}

	db, err := store.Open(dir)
	switch {
	case err != nil && errors.Is(err, store.ErrSchemaTooNew):
		rep.add(checkMigrate, checkFail, "%v", err)
	case err != nil:
		rep.add(checkMigrate, checkFail, "open and migrate: %v", err)
	default:
		uv, minCompat, stErr := db.SchemaState()
		if stErr != nil {
			rep.add(checkMigrate, checkFail, "read schema state: %v", stErr)
		} else {
			rep.add(checkMigrate, checkPass, "user_version %d, min_compatible %d (this build: %d)", uv, minCompat, store.SchemaVersion())
		}
		if icErr := db.IntegrityCheck(); icErr != nil {
			rep.add(checkIntegrity, checkFail, "%v", icErr)
		} else {
			rep.add(checkIntegrity, checkPass, "integrity_check ok")
		}
		if cErr := db.Close(); cErr != nil {
			rep.add(checkMigrate, checkFail, "close database: %v", cErr)
		}
	}
	if err != nil {
		rep.add(checkIntegrity, checkSkipped, "database did not open")
	}

	if selfcheckAfterMigrate != nil {
		selfcheckAfterMigrate(dir)
	}

	if idErr != nil {
		rep.add(checkIdentity, checkSkipped, "no baseline")
	} else if idAfter, e := identityDigests(dir); e != nil {
		rep.add(checkIdentity, checkFail, "re-read identity files: %v", e)
	} else if changed := sameDigests(idBefore, idAfter); len(changed) > 0 {
		rep.add(checkIdentity, checkFail, "changed during the run: %s", strings.Join(changed, ", "))
	} else {
		rep.add(checkIdentity, checkPass, "%s unchanged", strings.Join(identityFiles, ", "))
	}
	if tokErr != nil {
		rep.add(checkTokens, checkSkipped, "no baseline")
	} else if tokAfter, e := store.TokenHashDigest(dir); e != nil {
		rep.add(checkTokens, checkFail, "re-read token hashes: %v", e)
	} else if tokAfter != tokBefore {
		rep.add(checkTokens, checkFail, "token-hash digest changed from %s to %s", tokBefore[:12], tokAfter[:12])
	} else {
		rep.add(checkTokens, checkPass, "token-hash digest unchanged")
	}

	for _, short := range selfcheckHelpers {
		rep.Checks = append(rep.Checks, checkHelper(short))
	}
	return rep
}

// sidecarsDisabled reports whether RFE_SIDECARS switches the helpers off, as the daemon reads it.
func sidecarsDisabled() bool {
	for _, v := range strings.Split(strings.ToLower(strings.TrimSpace(os.Getenv("RFE_SIDECARS"))), ",") {
		switch strings.TrimSpace(v) {
		case "off", "none", "0", "false":
			return true
		}
	}
	return false
}

// checkHelper locates the helper program short (for example "indexd") the way the daemon does and, when it
// is installed and allowed to run, starts it and completes the version handshake. Not installed, switched
// off, or found but not allowed to run (no manifest beside it, so the daemon would not start it either) is
// "absent"; a refused file or a failed handshake is a failure.
func checkHelper(short string) selfcheckCheck {
	name := sidecar.FileName(short)
	check := selfcheckCheck{Name: fmt.Sprintf(checkHelperFmt, short)}
	if sidecarsDisabled() {
		check.Status, check.Detail = checkAbsent, name+": switched off by RFE_SIDECARS"
		return check
	}
	loc, ok, err := sidecar.LocateToRun(short, os.Getenv("RFE_SIDECARS") != "")
	switch {
	case errors.Is(err, sidecar.ErrUnverifiedNotAllowed):
		check.Status, check.Detail = checkAbsent, fmt.Sprintf("%s: not used by the daemon (%v)", name, err)
		return check
	case err != nil:
		check.Status, check.Detail = checkFail, fmt.Sprintf("%s refused: %v", name, err)
		return check
	case !ok:
		check.Status, check.Detail = checkAbsent, name+": not installed"
		return check
	}
	cfg := sidecar.Config{Name: name, Path: loc.Path}
	if loc.Verified {
		cfg.ExpectVersion, cfg.SHA256 = loc.Version, loc.SHA256
	}
	c, err := sidecar.Start(cfg)
	if err != nil {
		check.Status, check.Detail = checkFail, fmt.Sprintf("%s handshake: %v", name, err)
		return check
	}
	version := c.Version()
	if err := c.Close(); err != nil {
		check.Status, check.Detail = checkFail, fmt.Sprintf("%s stop: %v", name, err)
		return check
	}
	check.Status, check.Detail = checkPass, fmt.Sprintf("%s handshake ok, version %s", name, version)
	return check
}

// selfcheckAsTrialCheck adapts runSelfcheck to the callback backup.TrialRestore takes: nil when every check
// passed, otherwise an error naming the failed ones.
func selfcheckAsTrialCheck(dir string) error {
	rep := runSelfcheck(dir)
	return rep.err()
}

// cmdSelfcheck implements "rfe selfcheck -data <dir> [-json]". -data is required (never defaulted to the
// live data dir) because the check migrates the database in place: it must be given a copy.
func cmdSelfcheck(args []string) error {
	fs := newAdminFlags("selfcheck", "selfcheck -data <dir> [-json]")
	data := fs.String("data", "", "COPY of an agent data dir to test (required; it is migrated in place)")
	asJSON := fs.Bool("json", false, "print the report as JSON")
	if _, err := fs.parse(args, 0, 0); err != nil {
		return err
	}
	if *data == "" {
		return errors.New("selfcheck needs -data <dir>: it migrates that database in place, so give it a COPY (a restored backup), never the data dir a running agent uses")
	}
	rep := runSelfcheck(*data)
	if *asJSON {
		out, err := json.MarshalIndent(rep, "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(out))
	} else {
		for _, c := range rep.Checks {
			fmt.Printf("%-7s %-20s %s\n", strings.ToUpper(c.Status), c.Name, c.Detail)
		}
		if rep.OK {
			fmt.Println("selfcheck: all checks passed")
		}
	}
	if !rep.OK {
		return errSelfcheckFailed
	}
	return nil
}
