package backup

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ZaidQamhieh/rfe/agent/internal/store"
)

func makeArchive(t *testing.T) string {
	t.Helper()
	dir, _ := newDataDir(t)
	archive, _, err := Create(dir, t.TempDir(), Options{Label: LabelManual, AgentVersion: "test"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	return archive
}

// The check runs on a real restored copy; with no tmpDir the copy is removed afterwards.
func TestTrialRestoreRunsCheckOnRestoredCopyAndCleansUp(t *testing.T) {
	archive := makeArchive(t)
	var seen string
	m, err := TrialRestore(archive, "", func(dataDir string) error {
		seen = dataDir
		for _, f := range []string{DBMember, CertMember, KeyMember} {
			if _, err := os.Stat(filepath.Join(dataDir, f)); err != nil {
				return err
			}
		}
		db, err := store.Open(dataDir)
		if err != nil {
			return err
		}
		defer db.Close()
		devs, err := db.ListDevices()
		if err != nil || len(devs) != 3 {
			return errors.New("restored database does not hold the three devices")
		}
		return nil
	})
	if err != nil || m == nil || m.UserVersion != store.SchemaVersion() {
		t.Fatalf("TrialRestore = %+v, %v", m, err)
	}
	if seen == "" {
		t.Fatal("check was not called")
	}
	if _, err := os.Stat(seen); !os.IsNotExist(err) {
		t.Fatalf("trial data dir %s still exists (stat err %v)", seen, err)
	}
}

// A failing check is returned wrapped, with the manifest, and a caller-given tmpDir keeps the restored data.
func TestTrialRestorePropagatesCheckFailureAndKeepsGivenDir(t *testing.T) {
	archive := makeArchive(t)
	tmp := t.TempDir()
	boom := errors.New("selfcheck says no")
	m, err := TrialRestore(archive, tmp, func(string) error { return boom })
	if !errors.Is(err, boom) || m == nil {
		t.Fatalf("TrialRestore = %+v, %v; want the check's error and the manifest", m, err)
	}
	if _, err := os.Stat(filepath.Join(tmp, "data", DBMember)); err != nil {
		t.Fatalf("restored data not left in the given directory: %v", err)
	}
}

// A broken archive never reaches the check, and a nil check is refused.
func TestTrialRestoreRefusesBadInput(t *testing.T) {
	bogus := filepath.Join(t.TempDir(), "nope.tar.gz")
	if err := os.WriteFile(bogus, []byte("not an archive"), 0o600); err != nil {
		t.Fatal(err)
	}
	called := false
	if _, err := TrialRestore(bogus, "", func(string) error { called = true; return nil }); err == nil || called {
		t.Fatalf("bad archive: err=%v called=%v", err, called)
	}
	if _, err := TrialRestore(makeArchive(t), "", nil); err == nil || !strings.Contains(err.Error(), "check") {
		t.Fatalf("nil check err = %v", err)
	}
}
