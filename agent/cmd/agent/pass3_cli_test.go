//go:build linux

package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ZaidQamhieh/rfe/agent/internal/settings"
	"github.com/ZaidQamhieh/rfe/agent/internal/store"
)

// Pass 3: admin subcommands accept flags before AND after positional arguments,
// "--" ends flag parsing, and an unknown flag or a surplus positional is an error
// that names the argument (it is never silently ignored). These tests never touch
// the real ~/.rfe: HOME is a temp dir and each test asserts nothing was
// created under it when -data was given.

const pass3PairID = "abcd1234-0000-0000-0000-000000000001"

// pass3Home points HOME at a temp dir and clears RFE_DATA_DIR, so the default data
// dir resolves to <fake home>/.rfe.
func pass3Home(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("RFE_DATA_DIR", "")
	return home
}

// pass3NothingInHome fails if anything was created under the fake HOME.
func pass3NothingInHome(t *testing.T, home string) {
	t.Helper()
	if _, err := os.Lstat(filepath.Join(home, ".rfe")); !os.IsNotExist(err) {
		t.Errorf("default data dir %s/.rfe was touched (lstat err = %v)", home, err)
	}
	entries, err := os.ReadDir(home)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf("fake HOME is not empty: %v", entries)
	}
}

// pass3Run runs an admin subcommand in-process and returns its stdout, stderr and error.
func pass3Run(t *testing.T, cmd string, args ...string) (stdout, stderr string, err error) {
	t.Helper()
	errFile, ferr := os.CreateTemp(t.TempDir(), "stderr-*")
	if ferr != nil {
		t.Fatal(ferr)
	}
	old := os.Stderr
	os.Stderr = errFile
	stdout, err = captureStdout(t, func() error { return runAdmin(cmd, args) })
	os.Stderr = old
	if _, serr := errFile.Seek(0, io.SeekStart); serr != nil {
		t.Fatal(serr)
	}
	b, rerr := io.ReadAll(errFile)
	if rerr != nil {
		t.Fatal(rerr)
	}
	_ = errFile.Close()
	return stdout, string(b), err
}

// pass3Seed creates a data dir with one device, one waiting pair request and the
// folder settings (root is the single allowed root).
func pass3Seed(t *testing.T) (dir, root string) {
	t.Helper()
	dir = initializeAdminData(t)
	root = t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "child"), 0o700); err != nil {
		t.Fatal(err)
	}
	db, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.CreateDevice("dev-aaaa1111", "phone", "tok-1"); err != nil {
		t.Fatal(err)
	}
	if _, err := settings.Load(db, false, []string{root}, "test"); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	req := store.PairRequest{
		ID: pass3PairID, Label: "phone", ClientID: "client-1", PublicKey: "pk",
		NonceCommit: "c", RemoteIP: "10.0.0.5", Created: now.Unix(), Expires: now.Add(time.Hour).Unix(),
	}
	if err := db.CreatePairRequest(req); err != nil {
		t.Fatal(err)
	}
	if _, err := db.RevealPairRequest(pass3PairID, "nonce"); err != nil {
		t.Fatal(err)
	}
	return dir, root
}

// pass3Snapshot renders every piece of DB state the admin commands can change.
func pass3Snapshot(t *testing.T, dir string) string {
	t.Helper()
	db, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var sb strings.Builder
	devs, err := db.ListDevices()
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range devs {
		fmt.Fprintf(&sb, "dev %s revoked=%v ro=%v jail=%q\n", d.ID, d.Revoked, d.ReadOnly, d.JailRoot)
	}
	pr, ok, err := db.GetPairRequest(pass3PairID)
	if err != nil {
		t.Fatal(err)
	}
	fmt.Fprintf(&sb, "pair ok=%v status=%s\n", ok, pr.Status)
	audit, err := db.AuditEntries(1000, 0)
	if err != nil {
		t.Fatal(err)
	}
	fmt.Fprintf(&sb, "audit=%d\n", len(audit))
	root, _ := db.GetConfig("photoBackupRoot")
	fmt.Fprintf(&sb, "backup=%q\n", root)
	return sb.String()
}

// pass3Placements returns the positionals with "-data <dir>" inserted at every
// position, plus the "-data <dir> -- <positionals>" form.
const pass3DirMark = "@DATA@"

// pass3Fill substitutes the data dir for pass3DirMark in a placements template.
func pass3Fill(template []string, dir string) []string {
	out := make([]string, len(template))
	for i, a := range template {
		out[i] = strings.ReplaceAll(a, pass3DirMark, dir)
	}
	return out
}

func pass3Placements(dir string, pos ...string) [][]string {
	var out [][]string
	for k := 0; k <= len(pos); k++ {
		var a []string
		a = append(a, pos[:k]...)
		a = append(a, "-data", dir)
		a = append(a, pos[k:]...)
		out = append(out, a)
	}
	out = append(out, append([]string{"-data", dir, "--"}, pos...))
	out = append(out, append(append([]string{}, pos...), "-data="+dir)) // -flag=value form, after the positionals
	return out
}

func TestParseInterspersedHelper(t *testing.T) {
	newFS := func() (*flag.FlagSet, *string, *bool, *int) {
		fs := flag.NewFlagSet("t", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		return fs, fs.String("data", "", ""), fs.Bool("v", false, ""), fs.Int("n", 1, "")
	}
	cases := []struct {
		name    string
		args    []string
		pos     []string
		data    string
		v       bool
		n       int
		errPart string
	}{
		{name: "none", args: nil, pos: nil, n: 1},
		{name: "flags first", args: []string{"-data", "D", "a", "b"}, pos: []string{"a", "b"}, data: "D", n: 1},
		{name: "flags last", args: []string{"a", "b", "-data", "D"}, pos: []string{"a", "b"}, data: "D", n: 1},
		{name: "flags between", args: []string{"a", "-data", "D", "b", "-n", "3"}, pos: []string{"a", "b"}, data: "D", n: 3},
		{name: "double dash prefix on flag", args: []string{"a", "--data", "D"}, pos: []string{"a"}, data: "D", n: 1},
		{name: "equals form", args: []string{"a", "-data=D", "-n=2"}, pos: []string{"a"}, data: "D", n: 2},
		{name: "bool flag does not eat next arg", args: []string{"-v", "a"}, pos: []string{"a"}, v: true, n: 1},
		{name: "bool flag after positional", args: []string{"a", "-v", "b"}, pos: []string{"a", "b"}, v: true, n: 1},
		{name: "terminator", args: []string{"-data", "D", "--", "a", "-b"}, pos: []string{"a", "-b"}, data: "D", n: 1},
		{name: "terminator after positional", args: []string{"a", "--", "-data", "D"}, pos: []string{"a", "-data", "D"}, n: 1},
		{name: "terminator as flag value", args: []string{"-data", "--", "a"}, pos: []string{"a"}, data: "--", n: 1},
		{name: "lone dash is positional", args: []string{"-", "a"}, pos: []string{"-", "a"}, n: 1},
		{name: "empty positional kept", args: []string{"a", "", "-data", "D"}, pos: []string{"a", ""}, data: "D", n: 1},
		{name: "unknown flag first", args: []string{"-bogus"}, errPart: "bogus"},
		{name: "unknown flag after positional", args: []string{"a", "-bogus"}, errPart: "bogus"},
		{name: "unknown long flag after positional", args: []string{"a", "--bogus=1"}, errPart: "bogus"},
		{name: "bad int", args: []string{"a", "-n", "abc"}, errPart: "abc"},
		{name: "flag missing value", args: []string{"a", "-data"}, errPart: "data"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fs, data, v, n := newFS()
			pos, err := parseInterspersed(fs, tc.args)
			if tc.errPart != "" {
				if err == nil || !strings.Contains(err.Error(), tc.errPart) {
					t.Fatalf("err = %v, want one naming %q", err, tc.errPart)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if len(pos) == 0 && len(tc.pos) == 0 {
				// nil and empty are the same
			} else if !reflect.DeepEqual(pos, tc.pos) {
				t.Errorf("positional = %q, want %q", pos, tc.pos)
			}
			if *data != tc.data || *v != tc.v || *n != tc.n {
				t.Errorf("flags = (%q, %v, %d), want (%q, %v, %d)", *data, *v, *n, tc.data, tc.v, tc.n)
			}
		})
	}
	t.Run("help is flag.ErrHelp", func(t *testing.T) {
		fs, _, _, _ := newFS()
		if _, err := parseInterspersed(fs, []string{"a", "-h"}); err != flag.ErrHelp {
			t.Fatalf("err = %v, want flag.ErrHelp", err)
		}
	})
}

// The reported bug: `pair accept <id> -data D` used the default data dir.
func TestPass3PairAcceptFlagPlacement(t *testing.T) {
	home := pass3Home(t)
	for _, verbWord := range []string{"accept", "reject"} {
		want := "approved"
		if verbWord == "reject" {
			want = "rejected"
		}
		for i, template := range pass3Placements(pass3DirMark, pass3PairID) {
			dir, _ := pass3Seed(t)
			args := pass3Fill(template, dir)
			t.Run(fmt.Sprintf("%s/%d", verbWord, i), func(t *testing.T) {
				if _, stderr, err := pass3Run(t, "pair", append([]string{verbWord}, args...)...); err != nil {
					t.Fatalf("pair %s %q: %v (stderr %q)", verbWord, args, err, stderr)
				}
				if got := pass3Snapshot(t, dir); !strings.Contains(got, "status="+want) {
					t.Fatalf("request not %s after pair %s %q:\n%s", want, verbWord, args, got)
				}
			})
		}
	}
	pass3NothingInHome(t, home)
}

func TestPass3PairAcceptAfterIDExactReport(t *testing.T) {
	home := pass3Home(t)
	dir, _ := pass3Seed(t)
	out, _, err := pass3Run(t, "pair", "accept", pass3PairID[:8], "-data", dir)
	if err != nil || !strings.Contains(out, "Approved") {
		t.Fatalf("out=%q err=%v", out, err)
	}
	pass3NothingInHome(t, home)
}

func TestPass3PairRequestsFlagAfter(t *testing.T) {
	home := pass3Home(t)
	dir, _ := pass3Seed(t)
	out, _, err := pass3Run(t, "pair", "requests", "-data", dir)
	if err != nil || !strings.Contains(out, "phone") {
		t.Fatalf("out=%q err=%v", out, err)
	}
	if _, stderr, err := pass3Run(t, "pair", "requests", "extra", "-data", dir); err == nil || !strings.Contains(err.Error(), "extra") || !strings.Contains(strings.ToLower(stderr), "usage") {
		t.Fatalf("surplus positional on `pair requests`: err=%v stderr=%q", err, stderr)
	}
	pass3NothingInHome(t, home)
}

// Every rejected invocation: non-zero (error), names the argument, prints usage, leaves the DB untouched.
func TestPass3RejectionsNameTheArgumentAndLeaveDBUntouched(t *testing.T) {
	home := pass3Home(t)
	dir, root := pass3Seed(t)
	before := pass3Snapshot(t, dir)
	childPath := filepath.Join(root, "child")
	cases := []struct {
		name string
		cmd  string
		args []string
		want string // must appear in the error
	}{
		{"pair accept unknown flag after id", "pair", []string{"accept", pass3PairID, "-data", dir, "-bogusflag"}, "bogusflag"},
		{"pair accept unknown flag before id", "pair", []string{"accept", "-bogusflag", pass3PairID, "-data", dir}, "bogusflag"},
		{"pair accept surplus positional", "pair", []string{"accept", pass3PairID, "extra", "-data", dir}, "extra"},
		{"pair accept empty id", "pair", []string{"accept", "", "-data", dir}, "empty"},
		{"pair accept id only after dash dash", "pair", []string{"accept", "-data", dir, "--", pass3PairID, "-data"}, "-data"},
		{"pair accept flag missing value", "pair", []string{"accept", pass3PairID, "-data"}, "data"},
		{"pair reject surplus", "pair", []string{"reject", pass3PairID, "x1", "-data", dir}, "x1"},
		{"pair mint surplus positional", "pair", []string{"-data", dir, "surplus"}, "surplus"},
		{"pair mint bad ttl", "pair", []string{"-data", dir, "-ttl", "notaduration"}, "notaduration"},
		{"pair mint unknown flag", "pair", []string{"-data", dir, "-nope"}, "nope"},
		{"revoke unknown flag", "revoke", []string{"dev-aaaa", "-data", dir, "-nope"}, "nope"},
		{"revoke surplus", "revoke", []string{"dev-aaaa", "other", "-data", dir}, "other"},
		{"revoke missing id", "revoke", []string{"-data", dir}, "device-id"},
		{"remove surplus", "remove", []string{"dev-aaaa", "other", "-data", dir}, "other"},
		{"remove missing id", "remove", []string{"-data", dir}, "device-id"},
		{"remove unknown flag", "remove", []string{"dev-aaaa", "-data", dir, "--nope"}, "nope"},
		{"jail surplus", "jail", []string{"dev-aaaa", childPath, "third", "-data", dir}, "third"},
		{"jail missing path", "jail", []string{"dev-aaaa", "-data", dir}, "usage"},
		{"jail unknown flag", "jail", []string{"dev-aaaa", childPath, "-data", dir, "-zzz"}, "zzz"},
		{"readonly surplus", "readonly", []string{"dev-aaaa", "on", "off", "-data", dir}, "off"},
		{"readonly missing value", "readonly", []string{"dev-aaaa", "-data", dir}, "usage"},
		{"readonly unknown flag", "readonly", []string{"dev-aaaa", "on", "-data", dir, "-zzz"}, "zzz"},
		{"allow surplus", "allow", []string{"dev-aaaa", "all", "none", "-data", dir}, "none"},
		{"allow missing list", "allow", []string{"dev-aaaa", "-data", dir}, "usage"},
		{"allow unknown flag", "allow", []string{"dev-aaaa", "all", "-data", dir, "-zzz"}, "zzz"},
		{"backup-dir surplus", "backup-dir", []string{"-data", dir, "none", "extra"}, "extra"},
		{"backup-dir unknown flag", "backup-dir", []string{"none", "-data", dir, "-zzz"}, "zzz"},
		{"devices surplus", "devices", []string{"-data", dir, "extra"}, "extra"},
		{"devices unknown flag", "devices", []string{"-data", dir, "-zzz"}, "zzz"},
		{"audit surplus", "audit", []string{"-data", dir, "extra"}, "extra"},
		{"audit bad number", "audit", []string{"-n", "abc", "-data", dir}, "abc"},
		{"audit bad number after data", "audit", []string{"-data", dir, "-n", "abc"}, "abc"},
		{"status surplus", "status", []string{"-data", dir, "extra"}, "extra"},
		{"status unknown flag", "status", []string{"-data", dir, "-zzz"}, "zzz"},
		{"adduser surplus", "adduser", []string{"alice", "bob", "-data", dir, "-password", "correct-horse-battery"}, "bob"},
		{"adduser missing name", "adduser", []string{"-data", dir, "-password", "correct-horse-battery"}, "username"},
		{"adduser unknown flag", "adduser", []string{"alice", "-data", dir, "-zzz"}, "zzz"},
		{"uninstall surplus", "uninstall", []string{"extra"}, "extra"},
		{"start surplus", "start", []string{"extra"}, "extra"},
		{"stop surplus", "stop", []string{"-data", dir}, "-data"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, stderr, err := pass3Run(t, tc.cmd, tc.args...)
			if err == nil {
				t.Fatalf("%s %q succeeded; it must be an error", tc.cmd, tc.args)
			}
			if !strings.Contains(err.Error(), tc.want) && !strings.Contains(stderr, tc.want) {
				t.Errorf("error %q (stderr %q) does not name %q", err, stderr, tc.want)
			}
			if !strings.Contains(strings.ToLower(stderr+err.Error()), "usage") {
				t.Errorf("no usage printed: err=%q stderr=%q", err, stderr)
			}
			if after := pass3Snapshot(t, dir); after != before {
				t.Errorf("DB changed by a rejected command:\nbefore:\n%s\nafter:\n%s", before, after)
			}
		})
	}
	pass3NothingInHome(t, home)
}

// Each device command works with -data in every position, and the effect lands in the -data DB.
func TestPass3DeviceCommandsFlagPlacement(t *testing.T) {
	home := pass3Home(t)
	type tc struct {
		name  string
		cmd   string
		pos   func(root string) []string
		check func(t *testing.T, snap string)
	}
	cases := []tc{
		{"revoke", "revoke", func(string) []string { return []string{"dev-aaaa"} },
			func(t *testing.T, s string) { mustContain(t, s, "revoked=true") }},
		{"remove", "remove", func(string) []string { return []string{"dev-aaaa"} },
			func(t *testing.T, s string) { mustNotContain(t, s, "dev dev-aaaa1111") }},
		{"readonly on", "readonly", func(string) []string { return []string{"dev-aaaa", "on"} },
			func(t *testing.T, s string) { mustContain(t, s, "ro=true") }},
		{"jail", "jail", func(root string) []string { return []string{"dev-aaaa", filepath.Join(root, "child")} },
			func(t *testing.T, s string) { mustContain(t, s, "child") }},
		{"allow", "allow", func(string) []string { return []string{"dev-aaaa", "browse,download"} },
			func(t *testing.T, s string) { mustContain(t, s, "audit=1") }},
		{"backup-dir set", "backup-dir", func(root string) []string { return []string{filepath.Join(root, "child", "bk")} },
			func(t *testing.T, s string) { mustContain(t, s, "bk") }},
	}
	for _, c := range cases {
		for i := range pass3Placements(pass3DirMark, c.pos("")...) {
			dir, root := pass3Seed(t)
			args := pass3Fill(pass3Placements(pass3DirMark, c.pos(root)...)[i], dir)
			t.Run(fmt.Sprintf("%s/%d", c.name, i), func(t *testing.T) {
				if _, stderr, err := pass3Run(t, c.cmd, args...); err != nil {
					t.Fatalf("%s %q: %v (stderr %q)", c.cmd, args, err, stderr)
				}
				c.check(t, pass3Snapshot(t, dir))
			})
		}
	}
	pass3NothingInHome(t, home)
}

func mustContain(t *testing.T, s, sub string) {
	t.Helper()
	if !strings.Contains(s, sub) {
		t.Errorf("state lacks %q:\n%s", sub, s)
	}
}

func mustNotContain(t *testing.T, s, sub string) {
	t.Helper()
	if strings.Contains(s, sub) {
		t.Errorf("state unexpectedly has %q:\n%s", sub, s)
	}
}

func TestPass3ReadOnlyOffAndAuditCountFlagAfter(t *testing.T) {
	home := pass3Home(t)
	dir, _ := pass3Seed(t)
	if _, _, err := pass3Run(t, "readonly", "dev-aaaa", "on", "-data", dir); err != nil {
		t.Fatal(err)
	}
	if _, _, err := pass3Run(t, "readonly", "dev-aaaa", "off", "-data", dir); err != nil {
		t.Fatal(err)
	}
	mustContain(t, pass3Snapshot(t, dir), "ro=false")
	out, _, err := pass3Run(t, "audit", "-n", "1", "-data", dir)
	if err != nil || strings.Count(strings.TrimSpace(out), "\n") != 1 { // header + exactly one entry
		t.Fatalf("audit -n 1 -data: err=%v out=%q", err, out)
	}
	out, _, err = pass3Run(t, "audit", "-data", dir, "-n", "1")
	if err != nil || strings.Count(strings.TrimSpace(out), "\n") != 1 {
		t.Fatalf("audit -data -n 1: err=%v out=%q", err, out)
	}
	pass3NothingInHome(t, home)
}

func TestPass3AddUserFlagsAfterUsername(t *testing.T) {
	home := pass3Home(t)
	dir, _ := pass3Seed(t)
	if _, _, err := pass3Run(t, "adduser", "alice", "-data", dir, "-password", "correct-horse-battery"); err != nil {
		t.Fatal(err)
	}
	db, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if has, err := db.HasAnyUser(); err != nil || !has {
		t.Fatalf("user was not created in the -data DB: has=%v err=%v", has, err)
	}
	pass3NothingInHome(t, home)
}

// Default data dir selection: no -data -> $RFE_DATA_DIR, else <HOME>/.rfe; -data always wins.
func TestPass3DefaultDataDirSelection(t *testing.T) {
	home := pass3Home(t)
	fake := filepath.Join(home, ".rfe")
	if err := os.Mkdir(fake, 0o700); err != nil {
		t.Fatal(err)
	}
	db, err := store.Open(fake)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.CreateDevice("home-device-1", "from-home", "tok-h"); err != nil {
		t.Fatal(err)
	}
	_ = db.Close()
	explicit, _ := pass3Seed(t)

	// No -data, no env: the fake HOME's .rfe is used.
	out, _, err := pass3Run(t, "devices")
	if err != nil || !strings.Contains(out, "from-home") {
		t.Fatalf("default dir: out=%q err=%v", out, err)
	}
	// Explicit -data beats HOME, in either position, and does not read the default DB.
	for _, args := range [][]string{{"-data", explicit}, {"-data=" + explicit}} {
		out, _, err = pass3Run(t, "devices", args...)
		if err != nil || strings.Contains(out, "from-home") || !strings.Contains(out, "phone") {
			t.Fatalf("explicit %v: out=%q err=%v", args, out, err)
		}
	}
	// $RFE_DATA_DIR beats HOME; -data beats $RFE_DATA_DIR.
	t.Setenv("RFE_DATA_DIR", explicit)
	out, _, err = pass3Run(t, "devices")
	if err != nil || !strings.Contains(out, "phone") || strings.Contains(out, "from-home") {
		t.Fatalf("env dir: out=%q err=%v", out, err)
	}
	t.Setenv("RFE_DATA_DIR", filepath.Join(home, "does-not-exist"))
	if _, _, err = pass3Run(t, "devices", "-data", explicit); err != nil {
		t.Fatalf("-data should beat RFE_DATA_DIR: %v", err)
	}
	// pair accept with no -data resolves the default (HOME) dir: the id is not there, so it is an error about
	// the request, not about parsing, and the explicit dir's request stays pending.
	t.Setenv("RFE_DATA_DIR", "")
	_, _, err = pass3Run(t, "pair", "accept", pass3PairID)
	if err == nil {
		t.Fatal("accepting an id that only exists in another data dir must fail")
	}
	mustContain(t, pass3Snapshot(t, explicit), "status=pending")
}

// A missing default data dir is reported, not created.
func TestPass3MissingDefaultDataDirIsNotCreated(t *testing.T) {
	home := pass3Home(t)
	if _, _, err := pass3Run(t, "pair", "accept", pass3PairID); err == nil {
		t.Fatal("pair accept on a missing data dir should fail")
	}
	if _, err := os.Lstat(filepath.Join(home, ".rfe")); !os.IsNotExist(err) {
		t.Errorf("pair accept created the missing default data dir (err=%v)", err)
	}
	if _, _, err := pass3Run(t, "pair", "accept"); err == nil {
		t.Fatal("pair accept with no id should fail")
	}
	if _, err := os.Lstat(filepath.Join(home, ".rfe", "agent.db")); !os.IsNotExist(err) {
		t.Errorf("an agent.db was created in the fake default data dir (err=%v)", err)
	}
}

// -h prints usage and is not an error; the help text documents the flag rule.
func TestPass3HelpIsNotAnErrorAndDocumentsTheRule(t *testing.T) {
	home := pass3Home(t)
	dir, _ := pass3Seed(t)
	for _, args := range [][]string{{"revoke", "-h"}, {"pair", "accept", "-h"}, {"jail", "a", "--help"}} {
		_, stderr, err := pass3Run(t, args[0], args[1:]...)
		if err != nil {
			t.Errorf("%v: -h returned error %v", args, err)
		}
		if !strings.Contains(strings.ToLower(stderr), "usage") {
			t.Errorf("%v: no usage on stderr: %q", args, stderr)
		}
	}
	_ = dir
	out, _, err := pass3Run(t, "help")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"before or after", "--"} {
		if !strings.Contains(out, want) {
			t.Errorf("help text does not document %q:\n%s", want, out)
		}
	}
	pass3NothingInHome(t, home)
}

// pass3AddRequest adds one more waiting (revealed) pair request to a seeded data dir.
func pass3AddRequest(t *testing.T, dir, id string) {
	t.Helper()
	db, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	now := time.Now()
	req := store.PairRequest{ID: id, Label: "tablet", ClientID: "client-" + id, PublicKey: "pk", NonceCommit: "c",
		RemoteIP: "10.0.0.6", Created: now.Unix(), Expires: now.Add(time.Hour).Unix()}
	if err := db.CreatePairRequest(req); err != nil {
		t.Fatal(err)
	}
	if _, err := db.RevealPairRequest(id, "nonce"); err != nil {
		t.Fatal(err)
	}
}

// pass3Statuses returns request id -> status for the two ids used by these tests.
func pass3Statuses(t *testing.T, dir string, ids ...string) map[string]string {
	t.Helper()
	db, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	out := map[string]string{}
	for _, id := range ids {
		r, ok, err := db.GetPairRequest(id)
		if err != nil || !ok {
			t.Fatalf("request %s: ok=%v err=%v", id, ok, err)
		}
		out[id] = r.Status
	}
	return out
}

// `pair accept|reject` with no id keeps the documented [id] form: it answers the only
// waiting request, and errors (changing nothing) when 0 or more than 1 are waiting.
func TestPass3PairDecideWithoutID(t *testing.T) {
	home := pass3Home(t)
	const other = "ffff9999-0000-0000-0000-000000000002"

	for _, c := range []struct{ verbWord, want string }{{"accept", "approved"}, {"reject", "rejected"}} {
		// exactly one waiting: no id acts on it
		for _, template := range [][]string{{"-data", pass3DirMark}, {"-data=" + pass3DirMark}} {
			dir, _ := pass3Seed(t)
			args := pass3Fill(template, dir)
			if _, stderr, err := pass3Run(t, "pair", append([]string{c.verbWord}, args...)...); err != nil {
				t.Fatalf("pair %s %q with one waiting: %v (%s)", c.verbWord, args, err, stderr)
			}
			if got := pass3Statuses(t, dir, pass3PairID)[pass3PairID]; got != c.want {
				t.Fatalf("pair %s with one waiting: status = %s, want %s", c.verbWord, got, c.want)
			}
		}

		// zero waiting: error, nothing changes
		dir, _ := pass3Seed(t)
		if _, _, err := pass3Run(t, "pair", "reject", pass3PairID, "-data", dir); err != nil {
			t.Fatal(err)
		}
		before := pass3Snapshot(t, dir)
		if _, _, err := pass3Run(t, "pair", c.verbWord, "-data", dir); err == nil || !strings.Contains(err.Error(), "0 waiting") {
			t.Fatalf("pair %s with none waiting: err = %v, want a `0 waiting requests` error", c.verbWord, err)
		}
		if after := pass3Snapshot(t, dir); after != before {
			t.Fatalf("DB changed when nothing was waiting:\n%s\n%s", before, after)
		}

		// two waiting: error, both stay pending; an id (before or after the flag) still picks one
		dir, _ = pass3Seed(t)
		pass3AddRequest(t, dir, other)
		if _, _, err := pass3Run(t, "pair", c.verbWord, "-data", dir); err == nil || !strings.Contains(err.Error(), "2 waiting") {
			t.Fatalf("pair %s with two waiting: err = %v, want a `2 waiting requests` error", c.verbWord, err)
		}
		for id, st := range pass3Statuses(t, dir, pass3PairID, other) {
			if st != "pending" {
				t.Fatalf("request %s = %s after a refused no-id %s; both must stay pending", id, st, c.verbWord)
			}
		}
		if _, _, err := pass3Run(t, "pair", c.verbWord, "-data", dir, other[:4]); err != nil {
			t.Fatal(err)
		}
		st := pass3Statuses(t, dir, pass3PairID, other)
		if st[other] != c.want || st[pass3PairID] != "pending" {
			t.Fatalf("id prefix picked the wrong request: %v", st)
		}

		// more than one positional is still an error that names the extra one
		dir, _ = pass3Seed(t)
		if _, stderr, err := pass3Run(t, "pair", c.verbWord, pass3PairID, "second", "-data", dir); err == nil || !strings.Contains(err.Error(), "second") || !strings.Contains(stderr, "usage") {
			t.Fatalf("two positionals: err=%v stderr=%q", err, stderr)
		}
		if got := pass3Statuses(t, dir, pass3PairID)[pass3PairID]; got != "pending" {
			t.Fatalf("a refused invocation changed the request to %s", got)
		}
	}
	pass3NothingInHome(t, home)
}

func TestPass3HelpShowsOptionalPairID(t *testing.T) {
	pass3Home(t)
	out, _, err := pass3Run(t, "help")
	if err != nil || !strings.Contains(out, "pair accept|reject [id]") {
		t.Fatalf("help should show the optional [id] form: err=%v\n%s", err, out)
	}
}

// The daemon takes only flags: flags may come in any order, and a leftover argument is an
// error that names it (it used to be dropped, so `rfe -data D devices` started the daemon).
func TestPass3ServeArgs(t *testing.T) {
	home := pass3Home(t)
	ok := []struct {
		name string
		args []string
		addr string
		data string
		ro   bool
	}{
		{"no args", nil, ":8765", filepath.Join(home, ".rfe"), false},
		{"flags only", []string{"-addr", "127.0.0.1:0", "-read-only", "-data", "/tmp/x"}, "127.0.0.1:0", "/tmp/x", true},
		{"equals form and double dash flag", []string{"--addr=127.0.0.1:0", "-data=/tmp/y"}, "127.0.0.1:0", "/tmp/y", false},
		{"explicit empty roots", []string{"-roots", "", "-name", "n"}, ":8765", filepath.Join(home, ".rfe"), false},
	}
	for _, tc := range ok {
		t.Run(tc.name, func(t *testing.T) {
			var f serveFlags
			err := discardStderr(t, func() (e error) { f, e = parseServeArgs(tc.args); return })
			if err != nil {
				t.Fatal(err)
			}
			if f.addr != tc.addr || filepath.Clean(f.dataDir) != tc.data || f.readOnly != tc.ro {
				t.Errorf("got addr=%q data=%q ro=%v", f.addr, f.dataDir, f.readOnly)
			}
		})
	}
	bad := []struct {
		name string
		args []string
		want string // the error must name this
		hint bool   // and carry the admin-subcommand hint
	}{
		{"admin sub after flags", []string{"-data", "D", "devices"}, "devices", true},
		{"admin sub after flags, flag after", []string{"-data", "D", "devices", "-addr", "x"}, "devices", true},
		{"serve plus stray word and flag", []string{"foo", "-addr", "x"}, "foo", false},
		{"serve twice", []string{"serve"}, "serve", false},
		{"after double dash", []string{"-addr", "127.0.0.1:0", "--", "pair"}, "pair", true},
		{"unknown flag", []string{"-bogus"}, "bogus", false},
		{"bad bool value", []string{"-read-only=maybe"}, "maybe", false},
		{"flag missing value", []string{"-addr"}, "addr", false},
	}
	for _, tc := range bad {
		t.Run(tc.name, func(t *testing.T) {
			err := discardStderr(t, func() (e error) { _, e = parseServeArgs(tc.args); return })
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want one naming %q", err, tc.want)
			}
			hint := strings.Contains(err.Error(), "admin subcommands come first: rfe <sub> -data D")
			if hint != tc.hint {
				t.Errorf("admin-subcommand hint present = %v, want %v (err %q)", hint, tc.hint, err)
			}
		})
	}
	t.Run("every admin name gets the hint", func(t *testing.T) {
		for _, sub := range adminCommandNames {
			err := discardStderr(t, func() (e error) { _, e = parseServeArgs([]string{"-data", "D", sub}); return })
			if err == nil || !strings.Contains(err.Error(), "admin subcommands come first") {
				t.Errorf("%s: err = %v", sub, err)
			}
		}
	})
	t.Run("help is flag.ErrHelp", func(t *testing.T) {
		err := discardStderr(t, func() (e error) { _, e = parseServeArgs([]string{"-h"}); return })
		if err != flag.ErrHelp {
			t.Fatalf("err = %v", err)
		}
	})
	pass3NothingInHome(t, home)
}

// adminCommandNames must only list words runAdmin dispatches. Each is run with a bogus flag,
// which every subcommand refuses before doing anything, so no service or data is touched.
func TestPass3AdminCommandNamesAreDispatched(t *testing.T) {
	home := pass3Home(t)
	for _, sub := range adminCommandNames {
		_, _, err := pass3Run(t, sub, "--zz-bogus")
		if err != nil && strings.Contains(err.Error(), "unknown command") {
			t.Errorf("%q is in adminCommandNames but runAdmin does not know it", sub)
		}
	}
	pass3NothingInHome(t, home)
}

// pair and pair requests check the data dir before loading the cert, so a
// missing default dir (or a mistyped -data) is reported, not created with a
// fresh cert in it.
func TestPass4PairMissingDataDirIsNotCreated(t *testing.T) {
	home := pass3Home(t)
	typo := filepath.Join(t.TempDir(), "no-such-dir")
	for _, args := range [][]string{
		{"pair"},
		{"pair", "requests"},
		{"pair", "-data", typo},
		{"pair", "requests", "-data", typo},
	} {
		_, _, err := pass3Run(t, args[0], args[1:]...)
		if err == nil || !strings.Contains(err.Error(), "not found") {
			t.Errorf("%v: err = %v, want data dir not found", args, err)
		}
	}
	if _, err := os.Lstat(typo); !os.IsNotExist(err) {
		t.Errorf("pair created the -data dir %s (lstat err = %v)", typo, err)
	}
	pass3NothingInHome(t, home)
}
