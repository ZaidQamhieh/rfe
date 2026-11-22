package upgrade

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/ZaidQamhieh/rfe/agent/internal/backup"
	"github.com/ZaidQamhieh/rfe/agent/internal/bundle"
	"github.com/ZaidQamhieh/rfe/agent/internal/launcher"
	"github.com/ZaidQamhieh/rfe/agent/internal/security"
	"github.com/ZaidQamhieh/rfe/agent/internal/store"
)

var t0 = time.Date(2026, 3, 1, 10, 0, 0, 0, time.UTC)

// newData builds a data dir the way the daemon does (identity, database, three devices) and closes the database.
func newData(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "data")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := security.LoadOrCreateCert(dir); err != nil {
		t.Fatal(err)
	}
	db, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"dev-a", "dev-b", "dev-c"} {
		if err := db.CreateDevice(id, "phone "+id, "token-"+id); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	return dir
}

func snapshot(t *testing.T, dir string, at time.Time) string {
	t.Helper()
	path, _, err := backup.Create(dir, filepath.Join(dir, DefaultBackupSubdir), backup.Options{Label: backup.LabelPremigration, Now: func() time.Time { return at }})
	if err != nil {
		t.Fatal(err)
	}
	return path
}

func deviceStates(t *testing.T, dir string) map[string]bool {
	t.Helper()
	db, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	devs, err := db.ListDevices()
	if err != nil {
		t.Fatal(err)
	}
	m := map[string]bool{}
	for _, d := range devs {
		m[d.ID] = d.Revoked
	}
	return m
}

func read(t *testing.T, p string) []byte {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// bundleAt makes root/versions/<v> a verifying bundle whose agent file holds script.
func bundleAt(t *testing.T, root, v, script string) {
	t.Helper()
	dir := filepath.Join(root, launcher.VersionsDir, v)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, launcher.AgentFileName()), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := bundle.Build(dir, v, runtime.GOOS, runtime.GOARCH); err != nil {
		t.Fatal(err)
	}
}

func TestRestoreSnapshotSwapsInNewDirAndKeepsTheOldOne(t *testing.T) {
	dir := newData(t)
	cert := read(t, filepath.Join(dir, "agent-cert.pem"))
	key := read(t, filepath.Join(dir, "agent-key.pem"))
	snapshot(t, dir, t0)
	// After the snapshot: dev-d pairs, dev-a is revoked (the revocation log records it).
	db, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(10 * time.Millisecond)
	if err := db.CreateDevice("dev-d", "late", "token-d"); err != nil {
		t.Fatal(err)
	}
	if err := db.RevokeDevice("dev-a"); err != nil {
		t.Fatal(err)
	}
	db.Close()
	res, err := RestoreSnapshot(RestoreOptions{DataDir: dir, Now: func() time.Time { return t0.Add(time.Hour) }})
	if err != nil {
		t.Fatal(err)
	}
	if res.Revocations < 1 || res.OldDir != dir+".before-restore-20260301T110000Z" {
		t.Fatalf("%+v", res)
	}
	got := deviceStates(t, dir)
	if _, late := got["dev-d"]; late || !got["dev-a"] || len(got) != 3 || got["dev-b"] || got["dev-c"] {
		t.Fatalf("restored devices %v: dev-d must be gone and the revocation of dev-a kept", got)
	}
	if !bytes.Equal(read(t, filepath.Join(dir, "agent-cert.pem")), cert) || !bytes.Equal(read(t, filepath.Join(dir, "agent-key.pem")), key) {
		t.Fatal("the certificate and key must come back unchanged")
	}
	if old := deviceStates(t, res.OldDir); len(old) != 4 {
		t.Fatalf("the old data dir must be kept untouched, devices %v", old)
	}
	if _, err := os.Stat(dir + ".restored-20260301T110000Z"); !os.IsNotExist(err) {
		t.Fatalf("the temporary restore dir must be gone: %v", err)
	}
}

func TestRestoreSnapshotFailuresLeaveTheLiveDirAlone(t *testing.T) {
	dir := newData(t)
	before := read(t, filepath.Join(dir, store.DBFile))
	if _, err := RestoreSnapshot(RestoreOptions{DataDir: dir}); !errors.Is(err, ErrNoSnapshot) {
		t.Fatalf("no snapshot: %v", err)
	}
	snapshot(t, dir, t0)
	boom := errors.New("validation says no")
	_, err := RestoreSnapshot(RestoreOptions{DataDir: dir, Now: func() time.Time { return t0 }, Validate: func(string) error { return boom }})
	if !errors.Is(err, boom) {
		t.Fatalf("validate: %v", err)
	}
	entries, _ := filepath.Glob(dir + ".*")
	if len(entries) != 0 || !bytes.Equal(read(t, filepath.Join(dir, store.DBFile)), before) {
		t.Fatalf("a failed validation must leave nothing behind: %v", entries)
	}
	// An existing target name is never overwritten.
	if err := os.Mkdir(dir+".before-restore-20260301T100000Z", 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := RestoreSnapshot(RestoreOptions{DataDir: dir, Now: func() time.Time { return t0 }}); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("collision: %v", err)
	}
}

func setState(t *testing.T, root string, s launcher.State) *launcher.Store {
	t.Helper()
	st := launcher.NewStore(root)
	if err := st.Write(s); err != nil {
		t.Fatal(err)
	}
	return st
}

func TestApplyWritesPendingAfterSnapshotAndExits75(t *testing.T) {
	root, data := t.TempDir(), newData(t)
	bundleAt(t, root, "v1", "one")
	bundleAt(t, root, "v2", "two")
	st := setState(t, root, launcher.State{Current: "v1"})
	var order []string
	opt := ApplyOptions{Root: root, DataDir: data, Version: "v2", AgentVersion: "1.0.0", Now: func() time.Time { return t0 },
		Idle:          func(context.Context) bool { order = append(order, "idle"); return true },
		StopListening: func(context.Context) error { order = append(order, "stop"); return nil },
		Exit: func(c int) {
			order = append(order, "exit")
			if c != ExitApplied {
				t.Errorf("exit code %d", c)
			}
		}}
	if err := Apply(context.Background(), opt); err != nil {
		t.Fatal(err)
	}
	if strings.Join(order, ",") != "idle,stop,exit" {
		t.Fatalf("order %v", order)
	}
	if s, _ := st.Read(); s.Pending != "v2" || s.Current != "v1" {
		t.Fatalf("%+v", s)
	}
	arch, err := backup.Newest(filepath.Join(data, DefaultBackupSubdir), backup.LabelPremigration)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := backup.Verify(arch); err != nil {
		t.Fatal(err)
	}
	if ExitApplied != 75 {
		t.Fatalf("ExitApplied = %d", ExitApplied)
	}
}

func TestApplyRefusesBusyTamperedAndMissing(t *testing.T) {
	root, data := t.TempDir(), newData(t)
	bundleAt(t, root, "v1", "one")
	bundleAt(t, root, "v2", "two")
	st := setState(t, root, launcher.State{Current: "v1"})
	stopped, exited := false, false
	base := ApplyOptions{Root: root, DataDir: data, Version: "v2", Now: func() time.Time { return t0 },
		StopListening: func(context.Context) error { stopped = true; return nil }, Exit: func(int) { exited = true }}
	busy := base
	busy.Idle = func(context.Context) bool { return false }
	if err := Apply(context.Background(), busy); !errors.Is(err, ErrBusy) {
		t.Fatalf("busy: %v", err)
	}
	os.WriteFile(filepath.Join(root, launcher.VersionsDir, "v2", launcher.AgentFileName()), []byte("evil"), 0o755)
	if err := Apply(context.Background(), base); err == nil {
		t.Fatal("a tampered bundle was applied")
	}
	missing := base
	missing.Version = "v9"
	if err := Apply(context.Background(), missing); err == nil {
		t.Fatal("a missing version was applied")
	}
	if stopped || exited {
		t.Fatalf("nothing may stop or exit before the checks pass: stopped %v exited %v", stopped, exited)
	}
	if s, _ := st.Read(); s.Pending != "" {
		t.Fatalf("%+v", s)
	}
	if _, err := os.Stat(filepath.Join(data, DefaultBackupSubdir)); !os.IsNotExist(err) {
		t.Fatalf("no snapshot may be taken before the checks pass: %v", err)
	}
}

func TestApplyFailureAfterStopExitsNonZeroAndSetsNothing(t *testing.T) {
	root := t.TempDir()
	bundleAt(t, root, "v1", "one")
	bundleAt(t, root, "v2", "two")
	st := setState(t, root, launcher.State{Current: "v1"})
	codes := []int{}
	err := Apply(context.Background(), ApplyOptions{Root: root, DataDir: filepath.Join(t.TempDir(), "no-data"), Version: "v2",
		StopListening: func(context.Context) error { return nil }, Exit: func(c int) { codes = append(codes, c) }})
	if err == nil || len(codes) != 1 || codes[0] != 1 {
		t.Fatalf("err %v exit codes %v", err, codes)
	}
	if s, _ := st.Read(); s.Pending != "" {
		t.Fatalf("%+v", s)
	}
	// A listener that cannot be stopped is a plain error: nothing exited, nothing recorded.
	codes = nil
	err = Apply(context.Background(), ApplyOptions{Root: root, DataDir: newData(t), Version: "v2",
		StopListening: func(context.Context) error { return errors.New("stuck") }, Exit: func(c int) { codes = append(codes, c) }})
	if err == nil || len(codes) != 0 {
		t.Fatalf("err %v exit codes %v", err, codes)
	}
}

func TestApplyWhenIdleWaitsForIdle(t *testing.T) {
	root, data := t.TempDir(), newData(t)
	bundleAt(t, root, "v1", "one")
	bundleAt(t, root, "v2", "two")
	st := setState(t, root, launcher.State{Current: "v1"})
	calls := 0
	opt := ApplyOptions{Root: root, DataDir: data, Version: "v2", Now: func() time.Time { return t0 },
		Idle: func(context.Context) bool { calls++; return calls >= 3 }}
	if err := ApplyWhenIdle(context.Background(), opt, time.Millisecond); err != nil {
		t.Fatal(err)
	}
	if s, _ := st.Read(); calls != 3 || s.Pending != "v2" {
		t.Fatalf("calls %d state %+v", calls, s)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	never := opt
	never.Idle = func(context.Context) bool { return false }
	if err := ApplyWhenIdle(ctx, never, time.Millisecond); err == nil {
		t.Fatal("a cancelled wait must be an error")
	}
}

func TestDowngradeCompatibleOnlyMovesThePointer(t *testing.T) {
	root, data := t.TempDir(), newData(t)
	bundleAt(t, root, "v1", "one")
	bundleAt(t, root, "v2", "two")
	st := setState(t, root, launcher.State{Current: "v2", Previous: "v1"})
	before := read(t, filepath.Join(data, store.DBFile))
	res, err := Downgrade(DowngradeOptions{Root: root, DataDir: data, Target: "v1",
		Schema: func(string, string) (launcher.SchemaInfo, error) {
			return launcher.SchemaInfo{UserVersion: 5, MinCompatible: 2, SupportedLevel: 4}, nil
		}})
	if err != nil || res.Restored {
		t.Fatalf("%+v %v", res, err)
	}
	if s, _ := st.Read(); s.Current != "v1" || s.Previous != "v2" {
		t.Fatalf("%+v", s)
	}
	if !bytes.Equal(read(t, filepath.Join(data, store.DBFile)), before) {
		t.Fatal("a pointer-only downgrade must not touch the database")
	}
}

func TestDowngradeIncompatibleRestoresSnapshot(t *testing.T) {
	root, data := t.TempDir(), newData(t)
	bundleAt(t, root, "v1", "one")
	bundleAt(t, root, "v2", "two")
	st := setState(t, root, launcher.State{Current: "v2", Previous: "v1"})
	snapshot(t, data, t0)
	db, err := store.Open(data)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.CreateDevice("dev-d", "late", "token-d"); err != nil {
		t.Fatal(err)
	}
	db.Close()
	var asked []string
	res, err := Downgrade(DowngradeOptions{Root: root, DataDir: data, Target: "v1", Now: func() time.Time { return t0.Add(time.Hour) },
		Schema: func(agentDir, dataDir string) (launcher.SchemaInfo, error) {
			asked = append(asked, filepath.Base(agentDir)+":"+filepath.Base(dataDir))
			if dataDir == data { // the live database was migrated past what v1 reads
				return launcher.SchemaInfo{UserVersion: 9, MinCompatible: 9, SupportedLevel: 4}, nil
			}
			return launcher.SchemaInfo{UserVersion: 3, MinCompatible: 1, SupportedLevel: 4}, nil
		}})
	if err != nil || !res.Restored || res.OldDir == "" {
		t.Fatalf("%+v %v", res, err)
	}
	if len(asked) != 2 || !strings.HasPrefix(asked[1], "v1:data.restored-") {
		t.Fatalf("the restored dir must be validated with v1's check: %v", asked)
	}
	if got := deviceStates(t, data); len(got) != 3 {
		t.Fatalf("devices %v", got)
	}
	if s, _ := st.Read(); s.Current != "v1" {
		t.Fatalf("%+v", s)
	}
}

func TestDowngradeRefusals(t *testing.T) {
	root, data := t.TempDir(), newData(t)
	bundleAt(t, root, "v1", "one")
	bundleAt(t, root, "v2", "two")
	bundleAt(t, root, "v3", "three")
	st := setState(t, root, launcher.State{Current: "v2", Previous: "v1", Bad: []string{"v3"}})
	schema := func(string, string) (launcher.SchemaInfo, error) { return launcher.SchemaInfo{SupportedLevel: 9}, nil }
	opt := DowngradeOptions{Root: root, DataDir: data, Target: "v1", Schema: schema}
	lock, err := launcher.AcquireLock(st.LockPath())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Downgrade(opt); !errors.Is(err, ErrLauncherRunning) {
		t.Fatalf("lock held: %v", err)
	}
	lock.Release()
	for name, target := range map[string]string{"current": "v2", "bad": "v3", "missing": "v9"} {
		o := opt
		o.Target = target
		if _, err := Downgrade(o); err == nil {
			t.Errorf("%s target accepted", name)
		}
	}
	os.WriteFile(filepath.Join(root, launcher.VersionsDir, "v1", launcher.AgentFileName()), []byte("evil"), 0o755)
	if _, err := Downgrade(opt); err == nil {
		t.Error("tampered target accepted")
	}
	if s, _ := st.Read(); s.Current != "v2" {
		t.Fatalf("a refused downgrade must not change the state: %+v", s)
	}
	// With no snapshot to restore, an incompatible database fails cleanly and keeps the state.
	bundleAt(t, root, "v1", "one")
	opt.Schema = func(string, string) (launcher.SchemaInfo, error) {
		return launcher.SchemaInfo{MinCompatible: 9, SupportedLevel: 1}, nil
	}
	if _, err := Downgrade(opt); !errors.Is(err, ErrNoSnapshot) {
		t.Fatalf("%v", err)
	}
	if s, _ := st.Read(); s.Current != "v2" {
		t.Fatalf("%+v", s)
	}
}

func TestTrialFuncRunsSelfcheckOnACopy(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the fake agent is a shell script, which Windows cannot run as rfe.exe")
	}
	data := newData(t)
	before := deviceStates(t, data)
	script := func(body string) string {
		d := t.TempDir()
		if err := os.WriteFile(filepath.Join(d, launcher.AgentFileName()), []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
			t.Fatal(err)
		}
		return d
	}
	// The fake selfcheck insists on a database, and deletes it: only the copy may be affected.
	pass := script(`[ "$1" = selfcheck ] && [ "$2" = -data ] && [ -f "$3/agent.db" ] && rm -f "$3/agent.db" && exit 0; exit 9`)
	if err := TrialFunc(data, "", 0, t.Logf)(pass); err != nil {
		t.Fatal(err)
	}
	if got := deviceStates(t, data); len(got) != len(before) {
		t.Fatalf("the live data changed: %v", got)
	}
	fail := script(`echo migration exploded; exit 5`)
	if err := TrialFunc(data, "", 0, t.Logf)(fail); err == nil || !strings.Contains(err.Error(), "migration exploded") {
		t.Fatalf("a failing selfcheck must fail the trial with its output: %v", err)
	}
	if err := TrialFunc(filepath.Join(t.TempDir(), "none"), "", 0, t.Logf)(fail); err != nil {
		t.Fatalf("with no database there is nothing to migrate: %v", err)
	}
	if err := TrialFunc(data, "", 0, t.Logf)(t.TempDir()); err == nil {
		t.Fatal("a bundle without an agent binary must fail the trial")
	}
}
