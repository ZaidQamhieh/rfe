package daemon

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/ZaidQamhieh/rfe/agent/internal/next/legacyimport"
	"github.com/ZaidQamhieh/rfe/agent/internal/next/pairing"
	"github.com/ZaidQamhieh/rfe/agent/internal/next/store"
	"github.com/ZaidQamhieh/rfe/agent/internal/security"
)

// ErrNoOldDatabase is returned by RunImport when the data directory has no old database.
var ErrNoOldDatabase = errors.New("no old agent database (agent.db) in the data directory")

// Texts printed by the import command.
const (
	msgCheckHeader  = "Check only: nothing was written. Importing would carry over:"
	msgImportHeader = "Imported into " + legacyimport.NewDBName + ":"
	msgNotImported  = "Left behind on purpose (one-time codes and pending requests):"
	msgDropped      = "Chunk rows dropped because their temporary file is too short: %d"
	msgWarnings     = "Adjusted or dropped:"
	msgOldVersion   = "Old database schema version: %d"
	msgNoProblems   = "No problems found."
	msgImportNext   = "Run `agent-next import` to do it."
)

// printCounts writes "name: n" lines sorted by name under a heading, when any.
func printCounts(w io.Writer, heading string, counts map[string]int) {
	if len(counts) == 0 {
		return
	}
	fmt.Fprintln(w, heading)
	names := make([]string, 0, len(counts))
	for n := range counts {
		names = append(names, n)
	}
	slices.Sort(names)
	for _, n := range names {
		fmt.Fprintf(w, "  %s: %d\n", n, counts[n])
	}
}

// RunImport imports the old database of dataDir into the new one, or with
// check only reports what that would do. Check never writes into the data
// directory, and either mode leaves the old files untouched. The report is
// written to out.
func RunImport(dataDir string, check bool, out io.Writer) error {
	oldPath := filepath.Join(dataDir, legacyimport.OldDBName)
	if _, err := os.Stat(oldPath); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return ErrNoOldDatabase
		}
		return err
	}
	opts := legacyimport.Options{Logf: func(format string, args ...any) {
		fmt.Fprintf(out, "warning: "+format+"\n", args...)
	}}
	var rep *legacyimport.Report
	var err error
	if check {
		rep, err = legacyimport.Check(oldPath, dataDir, opts)
	} else {
		rep, err = legacyimport.Import(oldPath, filepath.Join(dataDir, legacyimport.NewDBName), dataDir, opts)
	}
	if err != nil {
		return err
	}
	if check {
		fmt.Fprintln(out, msgCheckHeader)
	} else {
		fmt.Fprintln(out, msgImportHeader)
	}
	fmt.Fprintf(out, msgOldVersion+"\n", rep.OldUserVersion)
	printCounts(out, "Rows:", rep.Counts)
	printCounts(out, msgNotImported, rep.NotImported)
	if rep.DroppedChunks > 0 {
		fmt.Fprintf(out, msgDropped+"\n", rep.DroppedChunks)
	}
	if len(rep.Warnings) > 0 {
		fmt.Fprintln(out, msgWarnings)
		for _, w := range rep.Warnings {
			fmt.Fprintln(out, "  "+strings.TrimSpace(w))
		}
	} else {
		fmt.Fprintln(out, msgNoProblems)
	}
	if check {
		fmt.Fprintln(out, msgImportNext)
	}
	return nil
}

// RunPair mints a one-time pairing code on the data directory's database and
// prints it: an owner code (it creates the account) while no account exists,
// else an ordinary code that pairs a browse-only device.
func RunPair(dataDir string, out io.Writer) error {
	if err := checkImported(dataDir); err != nil {
		return err
	}
	if err := os.MkdirAll(dataDir, dataDirPerm); err != nil {
		return err
	}
	cert, err := security.LoadOrCreateCert(dataDir)
	if err != nil {
		return err
	}
	db, err := store.Open(filepath.Join(dataDir, legacyimport.NewDBName))
	if err != nil {
		return err
	}
	defer db.Close()
	svc, err := pairing.New(pairing.Options{Store: db, Fingerprint: security.Fingerprint(cert)})
	if err != nil {
		return err
	}
	hasUser, err := db.HasAnyUser()
	if err != nil {
		return err
	}
	var code pairing.MintedCode
	kind := "owner"
	if hasUser {
		kind = "device"
		code, err = svc.Mint(0)
	} else {
		code, err = svc.MintOwner(0)
	}
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "pairing code (%s): %s\nexpires in %s\n", kind, code.Code, (time.Duration(code.ExpiresInSeconds) * time.Second).String())
	return nil
}
