package transfer

import (
	"io"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/ZaidQamhieh/rfe/agent/internal/fsops"
)

// killedSource yields a few bytes and then panics, unwinding the publish without any of its own cleanup:
// the in-process stand-in for the agent being killed mid-copy.
type killedSource struct {
	r    io.Reader
	sent bool
}

func (k *killedSource) Read(p []byte) (int, error) {
	if k.sent {
		panic("simulated kill during the publish copy")
	}
	k.sent = true
	return k.r.Read(p[:4])
}

// publisherFor is how the server publishes: through the request's Ops with this session's publish intent.
func publisherFor(tm *Manager, ops *fsops.Ops, id string) func(string, io.Reader, bool) (os.FileInfo, error) {
	return func(path string, src io.Reader, overwrite bool) (os.FileInfo, error) {
		return ops.PublishReaderWith(path, src, overwrite, tm.PublishOptions(id))
	}
}

// completeKilled runs a completion that is killed in the middle of the publish copy.
func completeKilled(t *testing.T, tm *Manager, ops *fsops.Ops, id string) {
	t.Helper()
	defer func() {
		if recover() == nil {
			t.Fatal("the simulated kill did not happen")
		}
	}()
	_, _, _ = tm.CompleteWithPublisher(id, func(path string, src io.Reader, overwrite bool) (os.FileInfo, error) {
		return publisherFor(tm, ops, id)(path, &killedSource{r: src}, overwrite)
	})
}

func intentCount(t *testing.T, tm *Manager) int {
	t.Helper()
	rows, err := tm.db.ListPublishIntentsBefore(time.Now().Add(100 * 365 * 24 * time.Hour).Unix())
	if err != nil {
		t.Fatal(err)
	}
	return len(rows)
}

func dirNames(t *testing.T, dir string) []string {
	t.Helper()
	ents, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, e := range ents {
		out = append(out, e.Name())
	}
	return out
}

// pastGrace is a sweep time safely beyond the intent age gate.
func pastGrace() time.Time { return time.Now().Add(publishIntentMinAge + time.Minute) }

// Killed mid-copy: the hidden temp and its intent stay until a restart plus the hourly sweep clear them
// (never before the age gate); a complete after that publishes exactly one file.
func TestCrashedPublishTempIsSweptAfterRestartAndCompleteAgainYieldsOneFile(t *testing.T) {
	tm, db, dataDir := setupManager(t)
	ops := fsops.New([]string{dataDir}, false)
	dest := filepath.Join(dataDir, "dest")
	target := filepath.Join(dest, "f.bin")
	data := []byte("hello world, hello again")
	qa2specOpen(t, tm, "sess-crash", target, data, 8, false, "dev-a")
	qa2specWriteChunks(t, tm, "sess-crash", data, 8)

	completeKilled(t, tm, ops, "sess-crash")
	temp := filepath.Join(dest, ".rfe-upload-sess-crash")
	if b, err := os.ReadFile(temp); err != nil || len(b) == 0 {
		t.Fatalf("temp after the kill: %q, %v", b, err)
	}
	if intentCount(t, tm) != 1 {
		t.Fatalf("intents after the kill = %d, want 1", intentCount(t, tm))
	}

	tm2, err := New(db, filepath.Join(dataDir, "tmp")) // restart
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tm2.CleanupStaleSessions(time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(temp); err != nil || intentCount(t, tm2) != 1 {
		t.Fatalf("a young publish was swept: stat err %v, intents %d", err, intentCount(t, tm2))
	}
	if _, err := tm2.CleanupStaleSessions(pastGrace()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(temp); !os.IsNotExist(err) {
		t.Fatalf("the temp survived the sweep: %v", err)
	}
	if intentCount(t, tm2) != 0 {
		t.Fatal("the intent survived the sweep")
	}

	if _, _, err := tm2.CompleteWithPublisher("sess-crash", publisherFor(tm2, ops, "sess-crash")); err != nil {
		t.Fatalf("complete again: %v", err)
	}
	if names := dirNames(t, dest); !slices.Equal(names, []string{"f.bin"}) {
		t.Fatalf("destination folder = %v, want one final file", names)
	}
	if b, _ := os.ReadFile(target); string(b) != string(data) {
		t.Fatalf("final content = %q", b)
	}
	if intentCount(t, tm2) != 0 {
		t.Fatal("an intent is left after a successful publish")
	}
}

// Without a sweep in between, the retried complete reuses the deterministic temp: one final file, no leftover.
func TestCompleteAgainReusesTheCrashedTemp(t *testing.T) {
	tm, _, dataDir := setupManager(t)
	ops := fsops.New([]string{dataDir}, false)
	dest := filepath.Join(dataDir, "dest")
	data := []byte("hello world, hello again")
	qa2specOpen(t, tm, "sess-retry", filepath.Join(dest, "f.bin"), data, 8, true, "dev-a")
	qa2specWriteChunks(t, tm, "sess-retry", data, 8)

	completeKilled(t, tm, ops, "sess-retry")
	if _, _, err := tm.CompleteWithPublisher("sess-retry", publisherFor(tm, ops, "sess-retry")); err != nil {
		t.Fatalf("retried complete: %v", err)
	}
	if names := dirNames(t, dest); !slices.Equal(names, []string{"f.bin"}) {
		t.Fatalf("destination folder = %v, want one final file", names)
	}
	if intentCount(t, tm) != 0 {
		t.Fatal("an intent is left after a successful publish")
	}
}

// A user's own ".rfe-upload-*" file has no intent row, so no sweep touches it; only the recorded temp goes.
func TestSweepNeverTouchesAnUnrecordedPrefixedFile(t *testing.T) {
	tm, _, dataDir := setupManager(t)
	ops := fsops.New([]string{dataDir}, false)
	dest := filepath.Join(dataDir, "dest")
	data := []byte("hello world, hello again")
	qa2specOpen(t, tm, "sess-a", filepath.Join(dest, "f.bin"), data, 8, false, "dev-a")
	qa2specWriteChunks(t, tm, "sess-a", data, 8)
	completeKilled(t, tm, ops, "sess-a")
	mine := filepath.Join(dest, ".rfe-upload-x")
	if err := os.WriteFile(mine, []byte("my own file"), 0o644); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-48 * time.Hour)
	if err := os.Chtimes(mine, old, old); err != nil {
		t.Fatal(err)
	}

	if _, err := tm.CleanupStaleSessions(pastGrace()); err != nil {
		t.Fatal(err)
	}
	if b, err := os.ReadFile(mine); err != nil || string(b) != "my own file" {
		t.Fatalf("the user's file: %q, %v", b, err)
	}
	if _, err := os.Lstat(filepath.Join(dest, ".rfe-upload-sess-a")); !os.IsNotExist(err) {
		t.Fatalf("the recorded temp survived: %v", err)
	}
}

// Startup reconcile (New) clears a recorded temp whose intent is old enough, just like the hourly sweep.
func TestStartupReconcileRemovesAnOldRecordedTemp(t *testing.T) {
	tm, db, dataDir := setupManager(t)
	dest := filepath.Join(dataDir, "dest")
	if err := os.MkdirAll(dest, 0o755); err != nil {
		t.Fatal(err)
	}
	realDest, _ := filepath.EvalSymlinks(dest)
	temp := filepath.Join(dest, ".rfe-upload-old-session")
	if err := os.WriteFile(temp, []byte("partial"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := db.PutPublishIntent("old-session", realDest, ".rfe-upload-old-session", time.Now().Add(-3*time.Hour).Unix()); err != nil {
		t.Fatal(err)
	}
	if _, err := New(db, filepath.Join(dataDir, "tmp")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(temp); !os.IsNotExist(err) {
		t.Fatalf("temp survived the startup reconcile: %v", err)
	}
	if intentCount(t, tm) != 0 {
		t.Fatal("intent survived the startup reconcile")
	}
}

// A session that is publishing right now (its completion lease is held) is never swept, however old its row.
func TestSweepSkipsASessionThatIsPublishing(t *testing.T) {
	tm, db, dataDir := setupManager(t)
	dest := filepath.Join(dataDir, "dest")
	if err := os.MkdirAll(dest, 0o755); err != nil {
		t.Fatal(err)
	}
	realDest, _ := filepath.EvalSymlinks(dest)
	data := []byte("hello world")
	qa2specOpen(t, tm, "sess-live", filepath.Join(dest, "f.bin"), data, 4, false, "dev-a")
	qa2specWriteChunks(t, tm, "sess-live", data, 4)
	temp := filepath.Join(dest, ".rfe-upload-sess-live")
	if err := os.WriteFile(temp, []byte("copy in progress"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := db.PutPublishIntent("sess-live", realDest, ".rfe-upload-sess-live", time.Now().Add(-3*time.Hour).Unix()); err != nil {
		t.Fatal(err)
	}
	_, release := qa2specHoldCompletion(t, tm, "sess-live")

	if _, err := tm.CleanupStaleSessions(pastGrace()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(temp); err != nil || intentCount(t, tm) != 1 {
		t.Fatalf("a publishing session was swept: stat err %v, intents %d", err, intentCount(t, tm))
	}
	release()
}

// The recorded folder swapped for a symlink, or the temp name for one, can never make the sweep delete
// anything outside the recorded folder.
func TestSweepSymlinkSwapCannotRedirectTheDelete(t *testing.T) {
	for _, swap := range []string{"folder", "name"} {
		t.Run(swap, func(t *testing.T) {
			tm, db, dataDir := setupManager(t)
			dest := filepath.Join(dataDir, "dest")
			if err := os.MkdirAll(dest, 0o755); err != nil {
				t.Fatal(err)
			}
			realDest, _ := filepath.EvalSymlinks(dest)
			name := ".rfe-upload-sess-sw"
			victimDir := t.TempDir()
			victim := filepath.Join(victimDir, name)
			if err := os.WriteFile(victim, []byte("precious"), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := db.PutPublishIntent("sess-sw", realDest, name, time.Now().Add(-3*time.Hour).Unix()); err != nil {
				t.Fatal(err)
			}
			switch swap {
			case "folder":
				if err := os.Rename(dest, dest+".moved"); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(victimDir, dest); err != nil {
					t.Skipf("symlinks unavailable: %v", err)
				}
			case "name":
				if err := os.Symlink(victim, filepath.Join(dest, name)); err != nil {
					t.Skipf("symlinks unavailable: %v", err)
				}
			}
			_, _ = tm.CleanupStaleSessions(pastGrace())
			if b, err := os.ReadFile(victim); err != nil || string(b) != "precious" {
				t.Fatalf("the victim file was touched: %q, %v", b, err)
			}
			if intentCount(t, tm) != 0 {
				t.Fatal("a refused intent must be dropped, not retried every hour")
			}
		})
	}
}

// A row that does not name this session's own temp (a tampered or foreign row) is dropped without deleting anything.
func TestSweepIgnoresARowThatIsNotTheSessionsOwnTemp(t *testing.T) {
	tm, db, dataDir := setupManager(t)
	victim := filepath.Join(dataDir, "notes.txt")
	if err := os.WriteFile(victim, []byte("keep me"), 0o644); err != nil {
		t.Fatal(err)
	}
	realDir, _ := filepath.EvalSymlinks(dataDir)
	old := time.Now().Add(-3 * time.Hour).Unix()
	for id, name := range map[string]string{"s1": "notes.txt", "s2": ".rfe-upload-someone-else", "s3": "../notes.txt"} {
		if err := db.PutPublishIntent(id, realDir, name, old); err != nil {
			t.Fatal(err)
		}
	}
	other := filepath.Join(dataDir, ".rfe-upload-someone-else")
	if err := os.WriteFile(other, []byte("other session"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, _ = tm.CleanupStaleSessions(pastGrace())
	for _, p := range []string{victim, other} {
		if _, err := os.Lstat(p); err != nil {
			t.Fatalf("%s was removed: %v", filepath.Base(p), err)
		}
	}
	if intentCount(t, tm) != 0 {
		t.Fatal("foreign rows must be dropped")
	}
}
