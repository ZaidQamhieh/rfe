package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ZaidQamhieh/rfe/agent/internal/backup"
	"github.com/ZaidQamhieh/rfe/agent/internal/launcher"
	"github.com/ZaidQamhieh/rfe/agent/internal/pairing"
	"github.com/ZaidQamhieh/rfe/agent/internal/transfer"
)

// backupsRouter builds the real router with the given data dir and launcher root, plus an admin and an ordinary token.
func backupsRouter(t *testing.T, dataDir, bundleRoot string) http.Handler {
	t.Helper()
	db, st := newTestDeps(t)
	tm, err := transfer.New(db, t.TempDir())
	if err != nil {
		t.Fatalf("transfer.New: %v", err)
	}
	h, err := New(Config{Name: "backups", Settings: st, TrashDir: t.TempDir(), ThumbCacheDir: t.TempDir(),
		UpdatesDir: t.TempDir(), StartTime: time.Now(), DataDir: dataDir, BundleRoot: bundleRoot},
		db, pairing.New(db, "127.0.0.1", "", "fp"), tm)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := db.RegisterAccount("admin-dev", "admin-dev", "tok-admin", "", "owner", "hash"); err != nil {
		t.Fatalf("RegisterAccount: %v", err)
	}
	if _, err := db.UpsertDevice("plain-dev", "plain-dev", "tok-plain", "", false); err != nil {
		t.Fatalf("UpsertDevice: %v", err)
	}
	return h
}

func getBackups(h http.Handler, token string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, "/v1/system/backups", nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	return rr
}

func TestBackupsStatusAdminOnly(t *testing.T) {
	h := backupsRouter(t, t.TempDir(), "")
	for token, want := range map[string]int{"": http.StatusUnauthorized, "tok-plain": http.StatusForbidden, "tok-admin": http.StatusOK} {
		if got := getBackups(h, token).Code; got != want {
			t.Errorf("token %q: want %d, got %d", token, want, got)
		}
	}
}

func TestBackupsStatusReportsStatusAndLauncherWithoutHostPaths(t *testing.T) {
	data := t.TempDir()
	root := t.TempDir()
	taken := time.Date(2026, 5, 1, 3, 0, 0, 0, time.UTC)
	if err := backup.RecordBackup(data, backup.BackupInfo{Time: taken, Label: "daily", Path: "backups/rfe-backup-daily-x.tar.gz", Size: 1234, OK: true}); err != nil {
		t.Fatal(err)
	}
	if err := backup.UpdateStatus(data, func(r *backup.StatusReport) {
		r.LastVerification = &backup.VerificationInfo{Time: taken, OK: false, Archive: "a.tar.gz",
			Error: backup.ScrubError(&os.PathError{Op: "open", Path: filepath.Join(t.TempDir(), "secret-dir", "f.db"), Err: os.ErrPermission}, data)}
	}); err != nil {
		t.Fatal(err)
	}
	if err := launcher.NewStore(root).Write(launcher.State{Current: "1.0.0", Previous: "0.9.0", Pending: "1.1.0", Bad: []string{"0.8.0"}}); err != nil {
		t.Fatal(err)
	}
	rr := getBackups(backupsRouter(t, data, root), "tok-admin")
	if rr.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", rr.Code, rr.Body)
	}
	var got struct {
		LastBackup       *backup.BackupInfo       `json:"last_backup"`
		LastVerification *backup.VerificationInfo `json:"last_verification"`
		Launcher         *launcherInfo            `json:"launcher"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.LastBackup == nil || got.LastBackup.Size != 1234 || !got.LastBackup.OK || got.LastBackup.Label != "daily" {
		t.Errorf("last_backup = %+v", got.LastBackup)
	}
	if got.LastVerification == nil || got.LastVerification.OK {
		t.Errorf("last_verification = %+v", got.LastVerification)
	}
	if got.Launcher == nil || got.Launcher.Current != "1.0.0" || got.Launcher.Pending != "1.1.0" || len(got.Launcher.Bad) != 1 {
		t.Errorf("launcher = %+v", got.Launcher)
	}
	body := rr.Body.String()
	for _, leak := range []string{data, root, os.TempDir(), "secret-dir"} {
		if strings.Contains(body, leak) {
			t.Errorf("response leaks %q: %s", leak, body)
		}
	}
}

func TestBackupsStatusEmptyAndBrokenLauncherState(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, launcher.StateFile), []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	rr := getBackups(backupsRouter(t, t.TempDir(), root), "tok-admin")
	if rr.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", rr.Code)
	}
	var got map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got["last_backup"] != nil || got["launcher"] != nil || got["launcher_error"] == nil {
		t.Errorf("unexpected body %v", got)
	}
	if strings.Contains(rr.Body.String(), root) {
		t.Errorf("response leaks the launcher root")
	}
}
