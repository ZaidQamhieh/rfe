package main

import (
	"bufio"
	"database/sql"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ZaidQamhieh/rfe/agent/internal/backup"
	"github.com/ZaidQamhieh/rfe/agent/internal/security"
	"github.com/ZaidQamhieh/rfe/agent/internal/sidecar"
	"github.com/ZaidQamhieh/rfe/agent/internal/store"
)

// fakeSidecarEnv makes the test binary act as a helper program: it announces itself and waits for stdin to close.
const fakeSidecarEnv = "RFE_FAKE_HELPER"

// TestMain lets the tests start this very binary as a fake rfe-indexd (see fakeHelperMain).
func TestMain(m *testing.M) {
	if mode := os.Getenv(fakeSidecarEnv); mode != "" {
		fakeHelperMain(mode)
		return
	}
	os.Exit(m.Run())
}

// fakeHelperMain writes the hello frame the helper protocol starts with (4-byte big-endian length, kind byte
// 0 for JSON, payload) naming itself rfe-indexd, with the protocol number "badproto" corrupts, then waits.
func fakeHelperMain(mode string) {
	proto := sidecar.ProtoVersion
	if mode == "badproto" {
		proto = 99
	}
	payload, _ := json.Marshal(map[string]any{"kind": "hello", "proto": proto, "name": "rfe-indexd", "version": "1.0.0"})
	frame := make([]byte, 5, 5+len(payload))
	binary.BigEndian.PutUint32(frame, uint32(len(payload)+1))
	frame[4] = 0
	os.Stdout.Write(append(frame, payload...))
	r := bufio.NewReader(os.Stdin)
	for {
		if _, err := r.ReadByte(); err != nil {
			return
		}
	}
}

// selfcheckSource builds a data dir with an identity, a database and a few paired devices.
func selfcheckSource(t *testing.T) string {
	t.Helper()
	dir := initializeAdminData(t)
	db, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"dev-1", "dev-2", "dev-3"} {
		if err := db.CreateDevice(id, "phone "+id, "token-"+id); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	return dir
}

// noHelpers points helper discovery at an empty directory so no helper is found beside the test binary.
func noHelpers(t *testing.T) {
	t.Helper()
	t.Setenv(sidecar.DirEnv, t.TempDir())
	t.Setenv("RFE_SIDECARS", "")
}

func requireStatus(t *testing.T, rep selfcheckReport, name, want string) {
	t.Helper()
	if got := rep.status(name); got != want {
		t.Fatalf("check %s = %q, want %q; report: %+v", name, got, want, rep.Checks)
	}
}

// A restored backup passes every check; absent helpers are reported absent, not failed.
func TestSelfcheckPassesOnRestoredBackup(t *testing.T) {
	noHelpers(t)
	src := selfcheckSource(t)
	archive, _, err := backup.Create(src, t.TempDir(), backup.Options{Label: backup.LabelManual, AgentVersion: "test"})
	if err != nil {
		t.Fatal(err)
	}
	var rep selfcheckReport
	m, err := backup.TrialRestore(archive, "", func(dir string) error {
		rep = runSelfcheck(dir)
		return rep.err()
	})
	if err != nil || m == nil {
		t.Fatalf("trial restore + selfcheck: %v; report %+v", err, rep.Checks)
	}
	if !rep.OK {
		t.Fatalf("report not OK: %+v", rep.Checks)
	}
	for _, name := range []string{checkBaseline, checkMigrate, checkIntegrity, checkIdentity, checkTokens} {
		requireStatus(t, rep, name, checkPass)
	}
	requireStatus(t, rep, "helper-indexd", checkAbsent)
	requireStatus(t, rep, "helper-thumbd", checkAbsent)
	if err := selfcheckAsTrialCheck(src); err != nil {
		t.Fatalf("selfcheckAsTrialCheck on a copy-equivalent dir: %v", err)
	}
}

// Changing one byte of the certificate while the run is in progress fails the identity check.
func TestSelfcheckFailsWhenCertChangesMidRun(t *testing.T) {
	noHelpers(t)
	dir := selfcheckSource(t)
	selfcheckAfterMigrate = func(dir string) {
		p := filepath.Join(dir, security.CertFile)
		b, err := os.ReadFile(p)
		if err != nil {
			t.Error(err)
			return
		}
		b[len(b)/2] ^= 0x01
		if err := os.WriteFile(p, b, 0o600); err != nil {
			t.Error(err)
		}
	}
	t.Cleanup(func() { selfcheckAfterMigrate = nil })
	rep := runSelfcheck(dir)
	if rep.OK {
		t.Fatalf("selfcheck passed although the certificate changed: %+v", rep.Checks)
	}
	requireStatus(t, rep, checkIdentity, checkFail)
	requireStatus(t, rep, checkMigrate, checkPass)
	var found bool
	for _, c := range rep.Checks {
		found = found || (c.Name == checkIdentity && strings.Contains(c.Detail, security.CertFile))
	}
	if !found {
		t.Fatalf("identity failure does not name the certificate: %+v", rep.Checks)
	}
}

// A database page that no longer matches its index is caught by integrity_check, and the command exits non-zero.
func TestSelfcheckFailsOnCorruptDatabase(t *testing.T) {
	noHelpers(t)
	dir := selfcheckSource(t)
	db, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 400; i++ {
		if err := db.AppendAudit("test", "actor", "target", strings.Repeat("x", 200)); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	corruptTable(t, dir, "audit_log")

	rep := runSelfcheck(dir)
	if rep.OK {
		t.Fatalf("selfcheck passed on a corrupted database: %+v", rep.Checks)
	}
	requireStatus(t, rep, checkIntegrity, checkFail)

	out, err := captureStdout(t, func() error { return cmdSelfcheck([]string{"-data", dir}) })
	if !errors.Is(err, errSelfcheckFailed) || !strings.Contains(out, "FAIL") {
		t.Fatalf("cmdSelfcheck err=%v out=%s", err, out)
	}
}

// corruptTable overwrites the cells of the b-tree page holding table's root, leaving the database openable
// (page 1 and the schema are intact) but internally inconsistent.
func corruptTable(t *testing.T, dir, table string) {
	t.Helper()
	root, pageSize := tableRootAndPageSize(t, dir, table)
	f, err := os.OpenFile(filepath.Join(dir, store.DBFile), os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	// Keep the 12-byte page header; fill the cell pointers and cells that follow it with 0xFF.
	const pageHeaderBytes = 12
	junk := make([]byte, pageSize-pageHeaderBytes)
	for i := range junk {
		junk[i] = 0xFF
	}
	if _, err := f.WriteAt(junk, int64(root-1)*int64(pageSize)+pageHeaderBytes); err != nil {
		t.Fatal(err)
	}
}

// tableRootAndPageSize reads a table's root page number and the database page size without migrating.
func tableRootAndPageSize(t *testing.T, dir, table string) (root, pageSize int) {
	t.Helper()
	raw, err := sql.Open("sqlite", filepath.Join(dir, store.DBFile))
	if err != nil {
		t.Fatal(err)
	}
	defer raw.Close()
	raw.SetMaxOpenConns(1)
	if err := raw.QueryRow("SELECT rootpage FROM sqlite_master WHERE name=?", table).Scan(&root); err != nil {
		t.Fatal(err)
	}
	if err := raw.QueryRow("PRAGMA page_size").Scan(&pageSize); err != nil {
		t.Fatal(err)
	}
	return root, pageSize
}

// setSchemaTooNew marks the database as needing an agent newer than this build.
func setSchemaTooNew(t *testing.T, dir string) {
	t.Helper()
	raw, err := sql.Open("sqlite", filepath.Join(dir, store.DBFile))
	if err != nil {
		t.Fatal(err)
	}
	raw.SetMaxOpenConns(1)
	next := store.SchemaVersion() + 1
	for _, q := range []string{
		fmt.Sprintf("PRAGMA user_version = %d", next),
		fmt.Sprintf("UPDATE schema_meta SET value=%d WHERE key='min_compatible'", next),
	} {
		if _, err := raw.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	if err := raw.Close(); err != nil {
		t.Fatal(err)
	}
}

// The report is machine readable with -json, the text form has one line per check, and -data is required.
func TestSelfcheckCommandJSONAndRequiresData(t *testing.T) {
	noHelpers(t)
	dir := selfcheckSource(t)
	out, err := captureStdout(t, func() error { return runAdmin("selfcheck", []string{"-data", dir, "-json"}) })
	if err != nil {
		t.Fatalf("selfcheck -json: %v\n%s", err, out)
	}
	var rep selfcheckReport
	if err := json.Unmarshal([]byte(out), &rep); err != nil || !rep.OK || rep.Dir != dir || len(rep.Checks) < 6 {
		t.Fatalf("bad JSON report (%v): %s", err, out)
	}
	out, err = captureStdout(t, func() error { return runAdmin("selfcheck", []string{"-data", dir}) })
	if err != nil || strings.Count(out, "PASS") < 5 || !strings.Contains(out, "ABSENT") {
		t.Fatalf("text report: %v\n%s", err, out)
	}
	// No -data: never defaults to the live data dir.
	if err := discardStderr(t, func() error { return runAdmin("selfcheck", nil) }); err == nil || !strings.Contains(err.Error(), "-data") {
		t.Fatalf("selfcheck without -data err = %v", err)
	}
	// A directory with no database fails instead of creating one.
	empty := t.TempDir()
	if rep := runSelfcheck(empty); rep.OK {
		t.Fatalf("selfcheck passed on an empty directory: %+v", rep.Checks)
	}
	if _, err := os.Stat(filepath.Join(empty, store.DBFile)); !os.IsNotExist(err) {
		t.Fatalf("selfcheck created a database in an empty directory (stat err %v)", err)
	}
}

// A database that needs a newer agent fails the migrate check and is left byte-identical.
func TestSelfcheckReportsSchemaTooNew(t *testing.T) {
	noHelpers(t)
	dir := selfcheckSource(t)
	setSchemaTooNew(t, dir)
	before, _ := os.ReadFile(filepath.Join(dir, store.DBFile))
	rep := runSelfcheck(dir)
	requireStatus(t, rep, checkMigrate, checkFail)
	if rep.OK {
		t.Fatal("selfcheck passed on a schema that is too new")
	}
	after, _ := os.ReadFile(filepath.Join(dir, store.DBFile))
	if string(before) != string(after) {
		t.Fatal("selfcheck modified a database it refused")
	}
}

// An installed helper that completes its handshake passes; one that answers with the wrong protocol fails.
func TestSelfcheckHelperHandshake(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	helperDir := t.TempDir()
	if err := os.Symlink(exe, filepath.Join(helperDir, sidecar.FileName("indexd"))); err != nil {
		t.Skipf("cannot symlink the test binary: %v", err)
	}
	t.Setenv(sidecar.DirEnv, helperDir)
	t.Setenv("RFE_SIDECARS", "indexd")
	dir := selfcheckSource(t)

	t.Setenv(fakeSidecarEnv, "ok")
	rep := runSelfcheck(dir)
	requireStatus(t, rep, "helper-indexd", checkPass)
	requireStatus(t, rep, "helper-thumbd", checkAbsent)
	if !rep.OK {
		t.Fatalf("report not OK with a working helper: %+v", rep.Checks)
	}

	t.Setenv(fakeSidecarEnv, "badproto")
	rep = runSelfcheck(dir)
	requireStatus(t, rep, "helper-indexd", checkFail)
	if rep.OK {
		t.Fatalf("selfcheck passed although the helper handshake failed: %+v", rep.Checks)
	}

	t.Setenv("RFE_SIDECARS", "off")
	rep = runSelfcheck(dir)
	requireStatus(t, rep, "helper-indexd", checkAbsent)
}
