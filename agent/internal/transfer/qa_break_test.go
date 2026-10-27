package transfer

// Break tests for the resumable upload engine, written by qa-break and not run by it.
//
// Run from agent/:
//   go test ./internal/transfer/ -run TestQABreak -count=1 -v
//
// Reuses setupManager and sha256hex from transfer_test.go. Each test's doc
// comment names the requirement (requirements.md item number) or code comment
// it checks. Open-question behaviour (zero-byte uploads, chunk-count caps) is
// asserted only as safety or liveness. Tests that hold about 128 GiB or 64 GiB
// of declared bytes use sparse temp files and are skipped under -short.

import (
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
)

// qaBreakSplit cuts content into chunkSize pieces, the last one possibly short.
func qaBreakSplit(content []byte, chunkSize int) [][]byte {
	var out [][]byte
	for off := 0; off < len(content); off += chunkSize {
		end := off + chunkSize
		if end > len(content) {
			end = len(content)
		}
		out = append(out, content[off:end])
	}
	return out
}

// qaBreakOpen opens an overwrite=false session for content and returns its id.
func qaBreakOpen(t *testing.T, tm *Manager, target string, content []byte, chunkSize int) string {
	t.Helper()
	id := uuid.New().String()
	if _, err := tm.OpenSession(id, target, int64(len(content)), chunkSize, sha256hex(content), false, ""); err != nil {
		t.Fatalf("OpenSession: %v", err)
	}
	return id
}

// Requirement 27: a chunk body must have exactly the length its index requires,
// and an index outside 0..totalChunks-1 is refused. Refused chunks are not
// stored. Source: transfer.go:566-577 (doc); store/transfers.go:48-63.
// Label: code failure.
func TestQABreak_ChunkLengthAndIndexEdges(t *testing.T) {
	tm, _, dataDir := setupManager(t)
	content := []byte("0123456789") // 10 bytes, chunkSize 4: lengths 4, 4, 2
	id := qaBreakOpen(t, tm, filepath.Join(dataDir, "geo.bin"), content, 4)
	parts := qaBreakSplit(content, 4)
	cases := []struct {
		name string
		n    int
		body []byte
		want error
	}{
		{"short non-last chunk", 0, parts[0][:3], ErrChunkWrongSize},
		{"long non-last chunk", 1, append(append([]byte{}, parts[1]...), 'x'), ErrChunkWrongSize},
		{"short last chunk", 2, parts[2][:1], ErrChunkWrongSize},
		{"long last chunk", 2, append(append([]byte{}, parts[2]...), 'x'), ErrChunkWrongSize},
		{"index equal to total", 3, []byte("zz"), ErrChunkOutOfRange},
		{"negative index", -1, parts[0], ErrChunkOutOfRange},
	}
	for _, c := range cases {
		err := tm.WriteChunk(id, c.n, c.body, sha256hex(c.body))
		if !errors.Is(err, c.want) {
			t.Errorf("%s: WriteChunk = %v, want %v", c.name, err, c.want)
		}
	}
	st, err := tm.Status(id)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if len(st.ReceivedChunks) != 0 {
		t.Fatalf("refused chunks were stored: %v", st.ReceivedChunks)
	}
}

// Positive control for requirement 27: when the size is an exact multiple of
// chunkSize, the last chunk is a full chunkSize and must be accepted. Source:
// store/transfers.go:59-61. Label: code failure.
func TestQABreak_ExactMultipleLastChunkAccepted(t *testing.T) {
	tm, _, dataDir := setupManager(t)
	content := []byte("ABCDEFGH") // 8 bytes, chunkSize 4: two full chunks
	target := filepath.Join(dataDir, "even.bin")
	id := qaBreakOpen(t, tm, target, content, 4)
	for n, p := range qaBreakSplit(content, 4) {
		if err := tm.WriteChunk(id, n, p, sha256hex(p)); err != nil {
			t.Fatalf("chunk %d refused: %v", n, err)
		}
	}
	if _, _, err := tm.Complete(id); err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if got, err := os.ReadFile(target); err != nil || string(got) != "ABCDEFGH" {
		t.Fatalf("published file = %q, %v", got, err)
	}
}

// Requirement 28: resending a received chunk whose hash matches succeeds and
// must not rewrite the stored bytes. Source: transfer.go:546-547 (doc: idempotent),
// 589-594; protocol/openapi.yaml:1370-1372. Label: code failure.
func TestQABreak_ResentChunkDoesNotRewriteBytes(t *testing.T) {
	tm, _, dataDir := setupManager(t)
	content := []byte("AAAABBBB")
	target := filepath.Join(dataDir, "dup.bin")
	id := qaBreakOpen(t, tm, target, content, 4)
	if err := tm.WriteChunk(id, 0, []byte("AAAA"), sha256hex([]byte("AAAA"))); err != nil {
		t.Fatalf("first write: %v", err)
	}
	if err := tm.WriteChunk(id, 0, []byte("CCCC"), sha256hex([]byte("CCCC"))); err != nil {
		t.Fatalf("resend with a matching hash = %v, want success", err)
	}
	raw, err := os.ReadFile(filepath.Join(tm.tempDir, id+".tmp"))
	if err != nil {
		t.Fatalf("read temp: %v", err)
	}
	if len(raw) < 4 || string(raw[:4]) != "AAAA" {
		t.Fatalf("resent chunk rewrote the stored bytes: %q", raw)
	}
	if err := tm.WriteChunk(id, 1, []byte("BBBB"), sha256hex([]byte("BBBB"))); err != nil {
		t.Fatalf("second chunk: %v", err)
	}
	if _, _, err := tm.Complete(id); err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if got, err := os.ReadFile(target); err != nil || string(got) != "AAAABBBB" {
		t.Fatalf("published file = %q, %v; want AAAABBBB", got, err)
	}
}

// Duplicate and parallel chunk writes: every write of every chunk succeeds, and
// the assembled file equals the source. Source: transfer.go:545-548 (idempotent),
// 596-607 (WriteAt per chunk); transfer.go:226-260 (activity leases). Label: code failure.
func TestQABreak_ConcurrentChunkWritesAssemble(t *testing.T) {
	tm, _, dataDir := setupManager(t)
	content := make([]byte, 64)
	for i := range content {
		content[i] = byte('a' + i%26)
	}
	target := filepath.Join(dataDir, "parallel.bin")
	id := qaBreakOpen(t, tm, target, content, 8)
	parts := qaBreakSplit(content, 8)
	const repeats = 3
	var wg sync.WaitGroup
	errs := make(chan error, len(parts)*repeats)
	for n := range parts {
		for rep := 0; rep < repeats; rep++ {
			wg.Add(1)
			go func(n int) {
				defer wg.Done()
				errs <- tm.WriteChunk(id, n, parts[n], sha256hex(parts[n]))
			}(n)
		}
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Errorf("concurrent WriteChunk: %v", err)
		}
	}
	if _, _, err := tm.Complete(id); err != nil {
		t.Fatalf("Complete after concurrent writes: %v", err)
	}
	if got, err := os.ReadFile(target); err != nil || string(got) != string(content) {
		t.Fatalf("assembled file differs from the source (err=%v)", err)
	}
}

// Completion must not publish while an in-process chunk operation holds the
// session lease: Complete returns ErrSessionActive, nothing is published, and
// the session completes after release. Source: transfer.go:216-222, 262-296;
// requirements.md 35 (mutual exclusion). Label: code failure.
func TestQABreak_CompleteRefusedWhileChunkLeaseHeld(t *testing.T) {
	tm, _, dataDir := setupManager(t)
	content := []byte("hello")
	target := filepath.Join(dataDir, "lease.bin")
	id := qaBreakOpen(t, tm, target, content, 5)
	if err := tm.WriteChunk(id, 0, content, sha256hex(content)); err != nil {
		t.Fatalf("WriteChunk: %v", err)
	}
	release, err := tm.BeginActivity(id)
	if err != nil {
		t.Fatalf("BeginActivity: %v", err)
	}
	if _, _, err := tm.Complete(id); !errors.Is(err, ErrSessionActive) {
		release()
		t.Fatalf("Complete under an active lease = %v, want ErrSessionActive", err)
	}
	if _, statErr := os.Stat(target); !os.IsNotExist(statErr) {
		release()
		t.Fatalf("target published while a lease was held (err=%v)", statErr)
	}
	release()
	if _, _, err := tm.Complete(id); err != nil {
		t.Fatalf("Complete after release: %v", err)
	}
	if got, err := os.ReadFile(target); err != nil || string(got) != "hello" {
		t.Fatalf("published file = %q, %v", got, err)
	}
}

// Requirement 35: DELETE during an active chunk operation returns
// ErrSessionActive and removes nothing: no row and no temp file. Source:
// requirements.md 35; transfer.go:312-340 (DeleteSession). Label: code failure.
func TestQABreak_DeleteRefusedWhileLeaseHeld(t *testing.T) {
	tm, _, dataDir := setupManager(t)
	content := []byte("hello")
	id := qaBreakOpen(t, tm, filepath.Join(dataDir, "del.bin"), content, 5)
	temp := filepath.Join(tm.tempDir, id+".tmp")
	release, err := tm.BeginActivity(id)
	if err != nil {
		t.Fatalf("BeginActivity: %v", err)
	}
	if err := tm.DeleteSession(id); !errors.Is(err, ErrSessionActive) {
		release()
		t.Fatalf("DeleteSession under an active lease = %v, want ErrSessionActive", err)
	}
	if _, statErr := os.Stat(temp); statErr != nil {
		release()
		t.Fatalf("temp file removed during an active lease: %v", statErr)
	}
	if _, lookErr := tm.Lookup(id); lookErr != nil {
		release()
		t.Fatalf("row removed during an active lease: %v", lookErr)
	}
	release()
	if err := tm.DeleteSession(id); err != nil {
		t.Fatalf("DeleteSession after release: %v", err)
	}
	if _, statErr := os.Stat(temp); !os.IsNotExist(statErr) {
		t.Fatalf("temp file survived DeleteSession (err=%v)", statErr)
	}
}

// Completion race: two concurrent Complete calls for one session publish exactly
// once. The loser gets ErrSessionActive or ErrNotOpen. Source: transfer.go:262-296
// (exclusive completion lease), 623-637 (status check). Label: code failure.
func TestQABreak_ConcurrentCompleteSucceedsOnce(t *testing.T) {
	tm, _, dataDir := setupManager(t)
	content := []byte("payload")
	target := filepath.Join(dataDir, "race.bin")
	id := qaBreakOpen(t, tm, target, content, 16)
	if err := tm.WriteChunk(id, 0, content, sha256hex(content)); err != nil {
		t.Fatalf("WriteChunk: %v", err)
	}
	results := make(chan error, 2)
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _, err := tm.Complete(id)
			results <- err
		}()
	}
	wg.Wait()
	close(results)
	wins := 0
	for err := range results {
		if err == nil {
			wins++
		}
	}
	if wins != 1 {
		t.Fatalf("%d concurrent Complete calls succeeded, want exactly 1", wins)
	}
	if got, err := os.ReadFile(target); err != nil || string(got) != "payload" {
		t.Fatalf("published file = %q, %v", got, err)
	}
}

// Requirement 30: a whole-file SHA-256 mismatch returns ErrFileMismatch and
// publishes nothing, even though every chunk hash matches. The status "failed"
// is code behaviour outside the contract and is not asserted. Source:
// requirements.md 30; transfer.go:652-678. Label: code failure.
func TestQABreak_WholeFileHashMismatchPublishesNothing(t *testing.T) {
	tm, _, dataDir := setupManager(t)
	actual := []byte("abcdef")
	target := filepath.Join(dataDir, "bad.bin")
	id := uuid.New().String()
	if _, err := tm.OpenSession(id, target, int64(len(actual)), 3, sha256hex([]byte("xxxxxx")), false, ""); err != nil {
		t.Fatalf("OpenSession: %v", err)
	}
	for n, p := range qaBreakSplit(actual, 3) {
		if err := tm.WriteChunk(id, n, p, sha256hex(p)); err != nil {
			t.Fatalf("chunk %d: %v", n, err)
		}
	}
	if _, _, err := tm.Complete(id); !errors.Is(err, ErrFileMismatch) {
		t.Fatalf("Complete with a wrong whole-file hash = %v, want ErrFileMismatch", err)
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatalf("target published despite a hash mismatch (err=%v)", err)
	}
}

// Open question 15 (zero-byte upload) is undefined by requirements. Safety and
// liveness only: the session is refused at open, or it completes to an empty
// file. It must never stay open and uncompletable. Source: transfer.go:162-165
// and store/transfers.go:46 (current behaviour: one empty chunk). Label: code failure.
func TestQABreak_ZeroByteUploadNeverStuck(t *testing.T) {
	tm, _, dataDir := setupManager(t)
	target := filepath.Join(dataDir, "empty.bin")
	id := uuid.New().String()
	if _, err := tm.OpenSession(id, target, 0, 64, sha256hex(nil), false, ""); err != nil {
		if _, statErr := os.Stat(target); !os.IsNotExist(statErr) {
			t.Fatalf("refused zero-byte open left a target (err=%v)", statErr)
		}
		return
	}
	_ = tm.WriteChunk(id, 0, []byte{}, sha256hex(nil))
	if _, _, err := tm.Complete(id); err != nil {
		t.Fatalf("zero-byte session can never complete: %v", err)
	}
	if got, err := os.ReadFile(target); err != nil || len(got) != 0 {
		t.Fatalf("zero-byte published file = %q, %v; want an empty file", got, err)
	}
}

// Chunk geometry at session open: an accepted session must have at least one
// chunk. With chunkSize = MaxInt, size+chunkSize-1 overflows and the count goes
// negative. The 32 MiB HTTP cap (requirements.md 33; openapi.yaml:1248-1250) is
// enforced in the server, not here, so the manager must refuse or compute a
// valid count itself. Source: transfer.go:162-165. Label: code failure (medium confidence).
func TestQABreak_HugeChunkSizeKeepsValidChunkCount(t *testing.T) {
	tm, _, dataDir := setupManager(t)
	id := uuid.New().String()
	content := []byte{0, 0}
	if _, err := tm.OpenSession(id, filepath.Join(dataDir, "huge.bin"), int64(len(content)), math.MaxInt, sha256hex(content), false, ""); err != nil {
		return
	}
	got, err := tm.Lookup(id)
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	if got.TotalChunks < 1 {
		t.Fatalf("session accepted with %d chunks", got.TotalChunks)
	}
}

// Requirement 33: refused session requests create no session row and no temp
// file: negative size, size above 64 GiB, chunkSize <= 0, and an existing
// destination with overwrite=false. Source: requirements.md 33;
// transfer.go:138-157 (checks run before the row at line 180). Label: code failure.
func TestQABreak_RefusedOpenCreatesNoSession(t *testing.T) {
	tm, _, dataDir := setupManager(t)
	existing := filepath.Join(dataDir, "exists.bin")
	if err := os.WriteFile(existing, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name      string
		size      int64
		chunkSize int
		target    string
	}{
		{"negative size", -1, 64, filepath.Join(dataDir, "a.bin")},
		{"size above 64 GiB", MaxTransferSize + 1, 64, filepath.Join(dataDir, "b.bin")},
		{"zero chunk size", 4, 0, filepath.Join(dataDir, "c.bin")},
		{"negative chunk size", 4, -4, filepath.Join(dataDir, "d.bin")},
		{"destination exists", 4, 4, existing},
	}
	for _, c := range cases {
		id := uuid.New().String()
		if _, err := tm.OpenSession(id, c.target, c.size, c.chunkSize, sha256hex([]byte("abcd")), false, ""); err == nil {
			t.Errorf("%s: OpenSession accepted the request", c.name)
			continue
		}
		if _, lookErr := tm.Lookup(id); !errors.Is(lookErr, ErrNotFound) {
			t.Errorf("%s: a session row exists after refusal (err=%v)", c.name, lookErr)
		}
		if _, statErr := os.Stat(filepath.Join(tm.tempDir, id+".tmp")); !os.IsNotExist(statErr) {
			t.Errorf("%s: a temp file exists after refusal (err=%v)", c.name, statErr)
		}
	}
}

// Requirement 34: a fifth open session from one device returns ErrQuotaExceeded
// (429), and another device is not affected. Source: requirements.md 34;
// transfer.go:73-78 and 180-191. Label: code failure.
func TestQABreak_FifthSessionFromOneDeviceRefused(t *testing.T) {
	tm, _, dataDir := setupManager(t)
	open := func(dev string, i int) error {
		id := uuid.New().String()
		target := filepath.Join(dataDir, fmt.Sprintf("%s-%d.bin", dev, i))
		_, err := tm.OpenSession(id, target, 4, 4, sha256hex([]byte("abcd")), false, dev)
		return err
	}
	for i := 0; i < MaxActiveTransfersPerDevice; i++ {
		if err := open("dev-a", i); err != nil {
			t.Fatalf("session %d of dev-a refused: %v", i+1, err)
		}
	}
	if err := open("dev-a", 99); !errors.Is(err, ErrQuotaExceeded) {
		t.Fatalf("fifth session from dev-a = %v, want ErrQuotaExceeded", err)
	}
	if err := open("dev-b", 0); err != nil {
		t.Fatalf("dev-b blocked by dev-a's session limit: %v", err)
	}
}

// Requirement 34: a device whose reserved bytes would exceed 64 GiB gets
// ErrQuotaExceeded (429). Declares 40 GiB then 30 GiB from one device. Uses
// sparse temp files; skipped under -short. Source: requirements.md 34;
// transfer.go:73-78. Label: needs environment (sparse file support) and code failure.
func TestQABreak_DeviceReservedBytesCapped(t *testing.T) {
	if testing.Short() {
		t.Skip("declares 70 GiB of sparse temp files; run without -short")
	}
	tm, _, dataDir := setupManager(t)
	if _, err := tm.OpenSession(uuid.New().String(), filepath.Join(dataDir, "big1.bin"), 40<<30, 1<<20, sha256hex(nil), false, "dev-a"); err != nil {
		t.Fatalf("first 40 GiB session refused: %v", err)
	}
	_, err := tm.OpenSession(uuid.New().String(), filepath.Join(dataDir, "big2.bin"), 30<<30, 1<<20, sha256hex(nil), false, "dev-a")
	if !errors.Is(err, ErrQuotaExceeded) {
		t.Fatalf("40 GiB + 30 GiB from one device = %v, want ErrQuotaExceeded", err)
	}
}

// Requirement 34: the host's reserved bytes are capped at 128 GiB across
// devices. Exactly 128 GiB is not "exceeding" and must be admitted; one more
// byte must be refused. Uses sparse temp files; skipped under -short.
// Source: requirements.md 34; transfer.go:76-78. Label: needs environment
// (sparse file support) and code failure.
func TestQABreak_HostReservedBytesCappedAcrossDevices(t *testing.T) {
	if testing.Short() {
		t.Skip("declares about 128 GiB of sparse temp files; run without -short")
	}
	tm, _, dataDir := setupManager(t)
	open := func(dev string, size int64) error {
		target := filepath.Join(dataDir, dev+".bin")
		_, err := tm.OpenSession(uuid.New().String(), target, size, 1<<20, sha256hex(nil), false, dev)
		return err
	}
	if err := open("dev-a", 60<<30); err != nil {
		t.Fatalf("dev-a 60 GiB refused: %v", err)
	}
	if err := open("dev-b", 60<<30); err != nil {
		t.Fatalf("dev-b 60 GiB refused: %v", err)
	}
	if err := open("dev-c", 8<<30); err != nil {
		t.Fatalf("host total of exactly 128 GiB refused: %v", err)
	}
	if err := open("dev-d", 1); !errors.Is(err, ErrQuotaExceeded) {
		t.Fatalf("host total of 128 GiB + 1 byte = %v, want ErrQuotaExceeded", err)
	}
}

// Requirement 36: an open session idle for seven days is removed with its temp
// file, and is kept before that boundary. Source: requirements.md 36;
// transfer.go:80-82 and 345-349. Label: code failure.
func TestQABreak_IdleSessionExpiresAfterSevenDays(t *testing.T) {
	tm, _, dataDir := setupManager(t)
	id := qaBreakOpen(t, tm, filepath.Join(dataDir, "idle.bin"), []byte("abcd"), 4)
	temp := filepath.Join(tm.tempDir, id+".tmp")
	if _, err := tm.CleanupStaleSessions(time.Now().Add(TransferStaleAfter - time.Hour)); err != nil {
		t.Fatalf("CleanupStaleSessions before the boundary: %v", err)
	}
	if _, err := tm.Lookup(id); err != nil {
		t.Fatalf("session removed before seven days: %v", err)
	}
	if _, err := tm.CleanupStaleSessions(time.Now().Add(TransferStaleAfter + time.Second)); err != nil {
		t.Fatalf("CleanupStaleSessions after the boundary: %v", err)
	}
	if _, err := tm.Lookup(id); !errors.Is(err, ErrNotFound) {
		t.Fatalf("session still present after seven days: %v", err)
	}
	if _, err := os.Stat(temp); !os.IsNotExist(err) {
		t.Fatalf("temp file survived expiry (err=%v)", err)
	}
}

// Requirement 36: a status (owner resume) request restarts the seven-day clock.
// The check runs two seconds short of seven days after the status call, so the
// row must still exist. Source: requirements.md 36; transfer.go:523-543 and
// 216-246 (beginActivity touches the row). Label: code failure.
func TestQABreak_StatusRestartsSevenDayClock(t *testing.T) {
	tm, _, dataDir := setupManager(t)
	id := qaBreakOpen(t, tm, filepath.Join(dataDir, "touch.bin"), []byte("abcd"), 4)
	if _, err := tm.Status(id); err != nil {
		t.Fatalf("Status: %v", err)
	}
	if _, err := tm.CleanupStaleSessions(time.Now().Add(TransferStaleAfter - 2*time.Second)); err != nil {
		t.Fatalf("CleanupStaleSessions: %v", err)
	}
	if _, err := tm.Lookup(id); err != nil {
		t.Fatalf("status did not restart the seven-day clock: %v", err)
	}
}

// Requirement 35: DELETE of a completed session keeps the published file, and
// DELETE of a session never removes a file it did not create. Source:
// requirements.md 35; transfer.go:312-340 (DeleteSession removes only the temp path).
// Label: code failure.
func TestQABreak_DeleteNeverRemovesPublishedOrForeignFile(t *testing.T) {
	tm, _, dataDir := setupManager(t)

	done := filepath.Join(dataDir, "done.bin")
	content := []byte("done")
	id := qaBreakOpen(t, tm, done, content, 4)
	if err := tm.WriteChunk(id, 0, content, sha256hex(content)); err != nil {
		t.Fatalf("WriteChunk: %v", err)
	}
	if _, _, err := tm.Complete(id); err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if err := tm.DeleteSession(id); err != nil {
		t.Fatalf("DeleteSession(completed): %v", err)
	}
	if got, err := os.ReadFile(done); err != nil || string(got) != "done" {
		t.Fatalf("published file lost by DELETE: %q, %v", got, err)
	}

	foreign := filepath.Join(dataDir, "foreign.bin")
	id2 := qaBreakOpen(t, tm, foreign, []byte("abcd"), 4)
	if err := os.WriteFile(foreign, []byte("mine"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := tm.DeleteSession(id2); err != nil {
		t.Fatalf("DeleteSession(open): %v", err)
	}
	if got, err := os.ReadFile(foreign); err != nil || string(got) != "mine" {
		t.Fatalf("DELETE touched a file it did not create: %q, %v", got, err)
	}
}
