package main

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ZaidQamhieh/rfe/agent/internal/backup"
	"github.com/ZaidQamhieh/rfe/agent/internal/bundle"
	"github.com/ZaidQamhieh/rfe/agent/internal/launcher"
	"github.com/ZaidQamhieh/rfe/agent/internal/store"
)

func fixedNow() time.Time { return time.Date(2026, 4, 5, 6, 7, 8, 0, time.UTC) }

func premigrationArchives(t *testing.T, dir string) []string {
	t.Helper()
	m, err := filepath.Glob(filepath.Join(dir, backup.SubdirName, "rfe-backup-premigration-*.tar.gz"))
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func TestBackupsEnabledOnlyOffWhenAskedTo(t *testing.T) {
	for v, want := range map[string]bool{"": true, "on": true, "1": true, "off": false, "OFF": false, " false ": false, "0": false, "no": false} {
		if got := backupsEnabled(func(string) string { return v }); got != want {
			t.Errorf("RFE_BACKUPS=%q: got %v, want %v", v, got, want)
		}
	}
}

func TestPremigrationSnapshotOfOldSchemaIsTakenFromAClosedCopy(t *testing.T) {
	dir := initializeAdminData(t)
	db, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.CreateDevice("dev-1", "phone", "tok-1"); err != nil {
		t.Fatal(err)
	}
	db.Close()
	downgradeSchema(t, dir)
	before, _ := os.ReadFile(filepath.Join(dir, store.DBFile))

	if err := premigrationSnapshot(dir, fixedNow); err != nil {
		t.Fatal(err)
	}
	arch := premigrationArchives(t, dir)
	if len(arch) != 1 {
		t.Fatalf("premigration archives = %v, want one", arch)
	}
	m, err := backup.Verify(arch[0])
	if err != nil {
		t.Fatalf("archive does not verify: %v", err)
	}
	if m.UserVersion >= store.SchemaVersion() {
		t.Errorf("archive holds user_version %d, want the old one (< %d)", m.UserVersion, store.SchemaVersion())
	}
	if after, _ := os.ReadFile(filepath.Join(dir, store.DBFile)); string(after) != string(before) {
		t.Error("the live database file was modified by the snapshot")
	}
	if left, _ := filepath.Glob(filepath.Join(dir, backup.SubdirName, ".premigration-*")); len(left) != 0 {
		t.Errorf("staging folder left behind: %v", left)
	}
	rep, err := backup.Status(dir)
	if err != nil || rep.LastBackup == nil || !rep.LastBackup.OK || rep.LastBackup.Label != backup.LabelPremigration {
		t.Fatalf("status = %+v, %v", rep.LastBackup, err)
	}
}

func TestPremigrationSnapshotSkippedWhenCurrentOrNoDatabase(t *testing.T) {
	empty := t.TempDir()
	if err := premigrationSnapshot(empty, fixedNow); err != nil {
		t.Fatalf("no database: %v", err)
	}
	if rep, _ := backup.Status(empty); rep.LastBackup != nil {
		t.Errorf("a fresh install recorded a backup: %+v", rep.LastBackup)
	}
	dir := initializeAdminData(t)
	if err := premigrationSnapshot(dir, fixedNow); err != nil {
		t.Fatal(err)
	}
	if got := premigrationArchives(t, dir); len(got) != 0 {
		t.Errorf("a current database was snapshotted: %v", got)
	}
}

func TestPremigrationSnapshotFailureIsRecordedAndReturned(t *testing.T) {
	dir := initializeAdminData(t)
	downgradeSchema(t, dir)
	if err := os.Remove(filepath.Join(dir, "agent-key.pem")); err != nil {
		t.Fatal(err)
	}
	err := premigrationSnapshot(dir, fixedNow)
	if err == nil {
		t.Fatal("a snapshot that cannot be taken must be reported")
	}
	rep, serr := backup.Status(dir)
	if serr != nil || rep.LastBackup == nil || rep.LastBackup.OK || rep.LastBackup.Error == "" {
		t.Fatalf("status = %+v, %v", rep.LastBackup, serr)
	}
	if strings.Contains(rep.LastBackup.Error, dir) {
		t.Errorf("recorded error leaks the data dir: %s", rep.LastBackup.Error)
	}
}

func TestAuditTriggerMapsPairingAndRevocationOnly(t *testing.T) {
	var mu sync.Mutex
	var got []string
	h := auditTrigger(func(l string) { mu.Lock(); got = append(got, l); mu.Unlock() })
	for _, a := range []string{store.AuditPair, store.AuditRegister, store.AuditDeviceRevoked, store.AuditDeviceRemoved, store.AuditLogin, "share_mint"} {
		h(a)
	}
	if strings.Join(got, ",") != "pair,pair,revoke,revoke" {
		t.Fatalf("labels = %v", got)
	}
}

func TestAuditEventsReachTheSchedulerThroughTheStoreHook(t *testing.T) {
	dir := initializeAdminData(t)
	db, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	created := make(chan string, 4)
	sch := backup.NewScheduler(backup.SchedulerConfig{DataDir: dir, AgentVersion: "t", Debounce: time.Millisecond,
		Create: func(dataDir, dest string, opt backup.Options) (string, *backup.Manifest, error) {
			created <- opt.Label
			p := filepath.Join(dest, backup.ArchiveName(opt.Label, opt.Now()))
			return p, &backup.Manifest{}, os.MkdirAll(dest, 0o700)
		},
		Verify: func(string) (*backup.Manifest, error) { return &backup.Manifest{}, nil }})
	db.SetAuditActionHook(auditTrigger(sch.Trigger))
	if err := db.AppendAudit(store.AuditDeviceRevoked, "test", "dev-1", ""); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go sch.Run(ctx)
	deadline := time.After(10 * time.Second)
	for {
		select {
		case l := <-created:
			if l == backup.LabelRevoke {
				return
			}
		case <-deadline:
			t.Fatal("no revoke backup was requested by the audit event")
		}
	}
}

func TestOffsiteWithoutPassphraseIsRecordedAsAConfigurationError(t *testing.T) {
	dir := t.TempDir()
	env := map[string]string{envBackupOffsiteDir: filepath.Join(t.TempDir(), "usb")}
	newBackupScheduler(dir, func(k string) string { return env[k] }, t.Logf)
	rep, err := backup.Status(dir)
	if err != nil || !rep.Offsite.Configured || rep.Offsite.OK || !strings.Contains(rep.Offsite.Error, envBackupPassphrase) {
		t.Fatalf("offsite = %+v, %v", rep.Offsite, err)
	}
}

func TestFindStagedVersionPicksNewestUnusedGoodVersion(t *testing.T) {
	root := t.TempDir()
	for _, v := range []string{"1.0.0", "1.1.0", "1.2.0", "1.3.0", "0.9.0", "not a version"} {
		if err := os.MkdirAll(filepath.Join(root, launcher.VersionsDir, v), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, launcher.VersionsDir, "1.4.0"), nil, 0o644); err != nil { // a file, not a dir
		t.Fatal(err)
	}
	s := launcher.NewStore(root)
	if err := s.Write(launcher.State{Current: "1.0.0", Bad: []string{"1.3.0"}}); err != nil {
		t.Fatal(err)
	}
	if got, err := findStagedVersion(root, "1.0.0"); err != nil || got != "1.2.0" {
		t.Fatalf("staged = %q, %v; want 1.2.0 (1.3.0 is bad, 1.4.0 is not a directory)", got, err)
	}
	if got, _ := findStagedVersion(root, "1.2.0"); got != "" {
		t.Errorf("a running build must not be offered itself or older: %q", got)
	}
	if err := s.Write(launcher.State{Current: "1.0.0", Pending: "1.1.0"}); err != nil {
		t.Fatal(err)
	}
	if got, _ := findStagedVersion(root, "1.0.0"); got != "" {
		t.Errorf("with a version already pending nothing is staged: %q", got)
	}
}

func TestAutoApplyDefaultsOff(t *testing.T) {
	for v, want := range map[string]bool{"": false, "0": false, "off": false, "1": true, "true": true, "ON": true} {
		if got := autoApplyEnabled(func(string) string { return v }); got != want {
			t.Errorf("RFE_AUTO_APPLY=%q: got %v, want %v", v, got, want)
		}
	}
}

func TestStartAutoApplyWaitsForIdleThenAppliesAndExits75(t *testing.T) {
	root, data := t.TempDir(), initializeAdminData(t)
	stageBundle(t, root, "1.0.0")
	stageBundle(t, root, "1.1.0")
	if err := launcher.NewStore(root).Write(launcher.State{Current: "1.0.0"}); err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	idle := false
	stopped := false
	exited := make(chan int, 1)
	stop := startAutoApply(autoApplyConfig{Root: root, DataDir: data, Running: "1.0.0", Logf: t.Logf, Scan: time.Millisecond, Poll: time.Millisecond,
		Idle:          func(context.Context) bool { mu.Lock(); defer mu.Unlock(); return idle },
		StopListening: func(context.Context) error { mu.Lock(); stopped = true; mu.Unlock(); return nil },
		Exit:          func(code int) { exited <- code }})
	defer stop()
	select {
	case code := <-exited:
		t.Fatalf("exited with %d while the agent was busy", code)
	case <-time.After(100 * time.Millisecond):
	}
	mu.Lock()
	idle = true
	mu.Unlock()
	select {
	case code := <-exited:
		if code != 75 {
			t.Fatalf("exit code %d, want 75", code)
		}
	case <-time.After(20 * time.Second):
		t.Fatal("Apply did not run once the agent was idle")
	}
	mu.Lock()
	defer mu.Unlock()
	if !stopped {
		t.Error("listeners were not stopped before exit")
	}
	if st, err := launcher.NewStore(root).Read(); err != nil || st.Pending != "1.1.0" {
		t.Errorf("state = %+v, %v; want pending 1.1.0", st, err)
	}
}

func TestStartAutoApplyDoesNotRetryAFailedVersion(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, launcher.VersionsDir, "1.1.0"), 0o755); err != nil { // no agent file: Apply must refuse
		t.Fatal(err)
	}
	if err := launcher.NewStore(root).Write(launcher.State{Current: "1.0.0"}); err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	msgs := 0
	stop := startAutoApply(autoApplyConfig{Root: root, DataDir: t.TempDir(), Running: "1.0.0", Scan: time.Millisecond, Poll: time.Millisecond,
		Idle: func(context.Context) bool { return true },
		Exit: func(int) { t.Error("exit called for a bundle that cannot be applied") },
		Logf: func(f string, a ...any) {
			if strings.Contains(f, "was not applied") {
				mu.Lock()
				msgs++
				mu.Unlock()
			}
		}})
	time.Sleep(300 * time.Millisecond)
	stop()
	mu.Lock()
	defer mu.Unlock()
	if msgs != 1 {
		t.Errorf("failure logged %d times, want once (not retried)", msgs)
	}
}

// downgradeSchema rewrites the closed database's user_version to 1, as a database left by an older build would have.
func downgradeSchema(t *testing.T, dir string) {
	t.Helper()
	raw, err := sql.Open("sqlite", filepath.Join(dir, store.DBFile))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := raw.Exec("PRAGMA user_version=1"); err != nil {
		t.Fatal(err)
	}
	if err := raw.Close(); err != nil {
		t.Fatal(err)
	}
	if uv, _, err := store.ReadSchemaState(dir); err != nil || uv != 1 {
		t.Fatalf("downgrade did not take: user_version %d, %v", uv, err)
	}
}

// stageBundle builds a genuine version bundle (agent file plus manifest) at <root>/versions/<v>.
func stageBundle(t *testing.T, root, v string) {
	t.Helper()
	dir := filepath.Join(root, launcher.VersionsDir, v)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, launcher.AgentFileName()), []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := bundle.Build(dir, v, runtime.GOOS, runtime.GOARCH); err != nil {
		t.Fatal(err)
	}
}
