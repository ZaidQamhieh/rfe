// Command agent-next is the rewritten RFE agent, built beside the old one.
//
//	agent-next [serve] [-addr A] [-name N] [-data D] [-roots R] [-read-only]
//	agent-next import [--check] [-data D]
//	agent-next pair [-data D]
//
// serve (the default) runs the daemon. import carries the old agent.db over to
// the new database once; --check only reports what it would do and writes
// nothing. pair prints a one-time pairing code.
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"strings"

	"github.com/ZaidQamhieh/rfe/agent/internal/next/daemon"
)

// version is the agent version; the release build sets it with -ldflags "-X main.version=...".
var version = "1.0.0"

// Subcommand words, flag names and exit codes.
const (
	cmdServe  = "serve"
	cmdImport = "import"
	cmdPair   = "pair"

	flagAddr     = "addr"
	flagName     = "name"
	flagData     = "data"
	flagReadOnly = "read-only"
	flagRoots    = "roots"
	flagCheck    = "check"

	exitOK    = 0
	exitFail  = 1
	exitUsage = 2

	rootSeparator = ","
)

// main runs the command line and exits with its status.
func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr)) }

// run dispatches to a subcommand: the first argument when it names one, else serve.
func run(args []string, stdout, stderr io.Writer) int {
	cmd := cmdServe
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		cmd, args = args[0], args[1:]
	}
	var err error
	switch cmd {
	case cmdServe:
		err = runServe(args, stderr)
	case cmdImport:
		err = runImport(args, stdout, stderr)
	case cmdPair:
		err = runPair(args, stdout, stderr)
	default:
		fmt.Fprintf(stderr, "unknown command %q (use %s, %s or %s)\n", cmd, cmdServe, cmdImport, cmdPair)
		return exitUsage
	}
	switch {
	case err == nil:
		return exitOK
	case errors.Is(err, flag.ErrHelp):
		return exitOK
	case errors.Is(err, errUsage):
		return exitUsage
	}
	fmt.Fprintln(stderr, "error:", err)
	return exitFail
}

// errUsage marks an error already explained on stderr as a command-line mistake.
var errUsage = errors.New("usage")

// newFlags makes a flag set that reports to stderr and returns its errors.
func newFlags(name string, stderr io.Writer) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(stderr)
	return fs
}

// parse parses args and refuses leftover words, which would be silently ignored.
func parse(fs *flag.FlagSet, args []string, stderr io.Writer) error {
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return err
		}
		return errUsage
	}
	if fs.NArg() > 0 {
		fmt.Fprintf(stderr, "unexpected argument %q\n", fs.Arg(0))
		return errUsage
	}
	return nil
}

// splitRoots splits the comma-separated -roots value, dropping empty items.
func splitRoots(s string) []string {
	var out []string
	for _, r := range strings.Split(s, rootSeparator) {
		if r = strings.TrimSpace(r); r != "" {
			out = append(out, r)
		}
	}
	return out
}

// runServe parses the daemon flags and runs the daemon until it is stopped.
func runServe(args []string, stderr io.Writer) error {
	fs := newFlags(cmdServe, stderr)
	addr := fs.String(flagAddr, daemon.DefaultAddr, "listen address (host:port)")
	name := fs.String(flagName, daemon.DefaultName(), "agent display name shown to the phone")
	dataDir := fs.String(flagData, daemon.DefaultDataDir(), "directory for certificates, database and state (precedence: -data > $"+daemon.EnvDataDir+" > ~/.rfe)")
	readOnly := fs.Bool(flagReadOnly, false, "reject all write operations (seeds a new database)")
	roots := fs.String(flagRoots, "", "comma-separated allowed folders for a new database (default: the RFE Files folder; explicitly empty = allow all)")
	if err := parse(fs, args, stderr); err != nil {
		return err
	}
	rootsExplicit := false
	fs.Visit(func(f *flag.Flag) {
		if f.Name == flagRoots {
			rootsExplicit = true
		}
	})
	return daemon.Run(daemon.Options{
		Addr: *addr, Name: *name, DataDir: *dataDir, Roots: splitRoots(*roots), RootsExplicit: rootsExplicit,
		ReadOnly: *readOnly, Version: version, Logf: log.Printf,
	})
}

// runImport imports (or with --check only reports on) the old database.
func runImport(args []string, stdout, stderr io.Writer) error {
	fs := newFlags(cmdImport, stderr)
	dataDir := fs.String(flagData, daemon.DefaultDataDir(), "data directory holding the old agent.db")
	check := fs.Bool(flagCheck, false, "only report what would be imported; write nothing")
	if err := parse(fs, args, stderr); err != nil {
		return err
	}
	return daemon.RunImport(*dataDir, *check, stdout)
}

// runPair prints a one-time pairing code.
func runPair(args []string, stdout, stderr io.Writer) error {
	fs := newFlags(cmdPair, stderr)
	dataDir := fs.String(flagData, daemon.DefaultDataDir(), "data directory")
	if err := parse(fs, args, stderr); err != nil {
		return err
	}
	return daemon.RunPair(*dataDir, stdout)
}
