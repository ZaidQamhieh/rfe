package transfer

import (
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/ZaidQamhieh/rfe/agent/internal/fsops"
)

func metricsOf(t *testing.T, tm *Manager) Metrics {
	t.Helper()
	m, err := tm.Metrics(time.Now())
	if err != nil {
		t.Fatalf("Metrics: %v", err)
	}
	return m
}

// complete on a session that is already completed or failed counts, including the idempotent replay.
func TestMetricsCompleteOnTerminalSession(t *testing.T) {
	tm, _, dataDir := setupManager(t)
	data := []byte("hello metrics")
	qa2specOpen(t, tm, "sess-term", filepath.Join(dataDir, "out", "f.bin"), data, 8, false, "dev-a")
	qa2specWriteChunks(t, tm, "sess-term", data, 8)
	if _, _, err := tm.Complete("sess-term"); err != nil {
		t.Fatal(err)
	}
	if got := metricsOf(t, tm).CompleteOnTerminalSession; got != 0 {
		t.Fatalf("counter after the first complete = %d, want 0", got)
	}
	if _, _, err := tm.Complete("sess-term"); !errors.Is(err, ErrNotOpen) {
		t.Fatalf("second complete = %v, want ErrNotOpen", err)
	}
	if got := metricsOf(t, tm).CompleteOnTerminalSession; got != 1 {
		t.Fatalf("counter after complete on a completed row = %d, want 1", got)
	}

	// A failed row counts as well.
	qa2specOpen(t, tm, "sess-fail", filepath.Join(dataDir, "out", "g.bin"), []byte("expected"), 8, false, "dev-a")
	qa2specWriteChunks(t, tm, "sess-fail", []byte("DIFFERNT"), 8)
	if _, _, err := tm.Complete("sess-fail"); !errors.Is(err, ErrFileMismatch) {
		t.Fatalf("complete = %v, want ErrFileMismatch", err)
	}
	if _, _, err := tm.Complete("sess-fail"); !errors.Is(err, ErrNotOpen) {
		t.Fatalf("complete on failed = %v, want ErrNotOpen", err)
	}
	if got := metricsOf(t, tm).CompleteOnTerminalSession; got != 2 {
		t.Fatalf("counter after complete on a failed row = %d, want 2", got)
	}

	// An unknown id is not a terminal session.
	if _, _, err := tm.Complete("no-such-session"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("complete unknown = %v", err)
	}
	if got := metricsOf(t, tm).CompleteOnTerminalSession; got != 2 {
		t.Fatalf("counter moved on an unknown id: %d", got)
	}
}

// HASH_MISMATCH with every chunk marked received counts once per failed session.
func TestMetricsHashMismatchAllChunksReceived(t *testing.T) {
	tm, _, dataDir := setupManager(t)
	qa2specOpen(t, tm, "sess-hash", filepath.Join(dataDir, "out", "h.bin"), []byte("expected"), 4, false, "dev-a")
	// An incomplete upload is not a hash mismatch.
	if _, _, err := tm.Complete("sess-hash"); !errors.Is(err, ErrIncomplete) {
		t.Fatalf("complete = %v, want ErrIncomplete", err)
	}
	if got := metricsOf(t, tm).HashMismatchAllChunksReceived; got != 0 {
		t.Fatalf("counter moved on an incomplete upload: %d", got)
	}
	qa2specWriteChunks(t, tm, "sess-hash", []byte("DIFFERNT"), 4)
	if _, _, err := tm.Complete("sess-hash"); !errors.Is(err, ErrFileMismatch) {
		t.Fatalf("complete = %v, want ErrFileMismatch", err)
	}
	if got := metricsOf(t, tm).HashMismatchAllChunksReceived; got != 1 {
		t.Fatalf("counter = %d, want 1", got)
	}
}

// A restart that fails a session whose temp file is gone or short counts it; a healthy one does not.
func TestMetricsSessionsResetByReconcile(t *testing.T) {
	tm, db, dataDir := setupManager(t)
	broken := openWithChunk(t, tm, dataDir)
	openWithChunk(t, tm, dataDir) // a healthy session next to it
	if err := os.Remove(broken.TempPath); err != nil {
		t.Fatal(err)
	}
	tm2, err := New(db, filepath.Dir(broken.TempPath))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if got := metricsOf(t, tm2).SessionsResetByReconcile; got != 1 {
		t.Fatalf("counter after one broken and one healthy session = %d, want 1", got)
	}
	tm3, err := New(db, filepath.Dir(broken.TempPath))
	if err != nil {
		t.Fatal(err)
	}
	if got := metricsOf(t, tm3).SessionsResetByReconcile; got != 0 {
		t.Fatalf("counter on a restart with nothing to reset = %d, want 0", got)
	}
}

// A publish temp the sweep removes from a destination folder counts; a young one left alone does not.
func TestMetricsPublishTempFoundInDest(t *testing.T) {
	tm, db, dataDir := setupManager(t)
	ops := fsops.New([]string{dataDir}, false)
	data := []byte("hello world, hello again")
	qa2specOpen(t, tm, "sess-sweep", filepath.Join(dataDir, "dest", "f.bin"), data, 8, false, "dev-a")
	qa2specWriteChunks(t, tm, "sess-sweep", data, 8)
	completeKilled(t, tm, ops, "sess-sweep")

	tm2, err := New(db, filepath.Join(dataDir, "tmp"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tm2.CleanupStaleSessions(time.Now()); err != nil {
		t.Fatal(err)
	}
	if got := metricsOf(t, tm2).PublishTempFoundInDest; got != 0 {
		t.Fatalf("counter after a sweep that found only a young temp = %d, want 0", got)
	}
	if _, err := tm2.CleanupStaleSessions(pastGrace()); err != nil {
		t.Fatal(err)
	}
	if got := metricsOf(t, tm2).PublishTempFoundInDest; got != 1 {
		t.Fatalf("counter after the sweep removed the stale temp = %d, want 1", got)
	}
}

// A disk-full write of a chunk counts; another write failure does not, and the chunk is not recorded.
func TestMetricsEnospcWriteChunk(t *testing.T) {
	if _, err := os.Stat("/dev/full"); err != nil {
		t.Skip("/dev/full is not available")
	}
	tm, _, dataDir := setupManager(t)
	data := []byte("12345678")
	qa2specOpen(t, tm, "sess-full", filepath.Join(dataDir, "out", "f.bin"), data, 8, false, "dev-a")
	sess, err := tm.Lookup("sess-full")
	if err != nil {
		t.Fatal(err)
	}

	// Another failure first: the temp file is gone.
	if err := os.Remove(sess.TempPath); err != nil {
		t.Fatal(err)
	}
	if err := tm.WriteChunk("sess-full", 0, data, sha256hex(data)); err == nil {
		t.Fatal("WriteChunk without a temp file succeeded")
	}
	if got := metricsOf(t, tm).EnospcWriteChunk; got != 0 {
		t.Fatalf("counter moved on a non-ENOSPC failure: %d", got)
	}

	// The temp name now leads to a device that answers every write with ENOSPC.
	if err := os.Symlink("/dev/full", sess.TempPath); err != nil {
		t.Fatal(err)
	}
	err = tm.WriteChunk("sess-full", 0, data, sha256hex(data))
	if !errors.Is(err, syscall.ENOSPC) {
		t.Fatalf("WriteChunk = %v, want ENOSPC", err)
	}
	if got := metricsOf(t, tm).EnospcWriteChunk; got != 1 {
		t.Fatalf("counter = %d, want 1", got)
	}
	if has, _ := tm.db.HasChunk("sess-full", 0); has {
		t.Fatal("a chunk that hit ENOSPC was recorded as received")
	}
}

// A disk-full error from the publish step counts; another publish failure does not.
func TestMetricsEnospcPublish(t *testing.T) {
	tm, _, dataDir := setupManager(t)
	data := []byte("12345678")
	qa2specOpen(t, tm, "sess-pub", filepath.Join(dataDir, "out", "f.bin"), data, 8, false, "dev-a")
	qa2specWriteChunks(t, tm, "sess-pub", data, 8)

	fail := func(cause error) func(string, io.Reader, bool) (os.FileInfo, error) {
		return func(path string, _ io.Reader, _ bool) (os.FileInfo, error) {
			return nil, &fs.PathError{Op: "write", Path: path, Err: cause}
		}
	}
	if _, _, err := tm.CompleteWithPublisher("sess-pub", fail(syscall.EIO)); err == nil {
		t.Fatal("publish failure was swallowed")
	}
	if got := metricsOf(t, tm).EnospcPublish; got != 0 {
		t.Fatalf("counter moved on a non-ENOSPC failure: %d", got)
	}
	_, _, err := tm.CompleteWithPublisher("sess-pub", fail(syscall.ENOSPC))
	if !errors.Is(err, syscall.ENOSPC) {
		t.Fatalf("complete = %v, want ENOSPC", err)
	}
	if got := metricsOf(t, tm).EnospcPublish; got != 1 {
		t.Fatalf("counter = %d, want 1", got)
	}
	// The failed publish leaves the session open, as before.
	if got := statusOf(t, tm.db, "sess-pub"); got != "open" {
		t.Fatalf("status after a failed publish = %q, want open", got)
	}
}

// Open sessions and the age of the least recently active one are computed when the metrics are read.
func TestMetricsOpenSessionGauges(t *testing.T) {
	tm, _, dataDir := setupManager(t)
	if m := metricsOf(t, tm); m.OpenSessions != 0 || m.OldestOpenSessionAgeSeconds != 0 {
		t.Fatalf("empty manager gauges = %+v", m)
	}
	for _, id := range []string{"sess-a", "sess-b"} {
		data := []byte("12345678")
		qa2specOpen(t, tm, id, filepath.Join(dataDir, "out", id), data, 4, false, "dev-a")
		if err := tm.WriteChunk(id, 0, data[:4], sha256hex(data[:4])); err != nil {
			t.Fatal(err)
		}
	}
	m, err := tm.Metrics(time.Now().Add(2 * time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if m.OpenSessions != 2 {
		t.Fatalf("OpenSessions = %d, want 2", m.OpenSessions)
	}
	if want := int64((2 * time.Hour).Seconds()); m.OldestOpenSessionAgeSeconds < want || m.OldestOpenSessionAgeSeconds > want+30 {
		t.Fatalf("OldestOpenSessionAgeSeconds = %d, want about %d", m.OldestOpenSessionAgeSeconds, want)
	}
}
