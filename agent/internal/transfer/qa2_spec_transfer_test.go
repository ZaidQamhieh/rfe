package transfer

// Spec tests (QA2) for the upload transfer engine: FR-20 to FR-28, FR-32, FR-36,
// QR-1 to QR-3. Expectations come from the requirements; the Manager is only the
// entry point. HTTP status codes and grant checks are server-side and not tested here.
//
// The QR-1 byte-limit tests write no bytes: OpenSession only truncates the temp
// file to the declared size, so the reservation is sparse.

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/ZaidQamhieh/rfe/agent/internal/fsops"
	"github.com/ZaidQamhieh/rfe/agent/internal/store"
)

// qa2specErrDenied is returned by a test publisher that refuses to publish.
var qa2specErrDenied = errors.New("qa2spec publisher refused")

// qa2specErrHeld is returned by the publisher that holds a completion open.
var qa2specErrHeld = errors.New("qa2spec held publisher")

// qa2specSum returns the lowercase hex SHA-256 of b.
func qa2specSum(b []byte) string {
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}

// qa2specChunk returns chunk n of data split into chunkSize pieces.
func qa2specChunk(data []byte, chunkSize, n int) []byte {
	start := n * chunkSize
	end := start + chunkSize
	if end > len(data) {
		end = len(data)
	}
	return data[start:end]
}

// qa2specChunkCount returns the number of chunks of size bytes split into chunkSize pieces.
func qa2specChunkCount(size, chunkSize int) int {
	return (size + chunkSize - 1) / chunkSize
}

// qa2specContains reports whether xs holds v.
func qa2specContains(xs []int, v int) bool {
	for _, x := range xs {
		if x == v {
			return true
		}
	}
	return false
}

// qa2specPathExists reports whether p exists, without following a final symlink.
func qa2specPathExists(p string) bool {
	_, err := os.Lstat(p)
	return err == nil
}

// qa2specReadFile returns the content of path.
func qa2specReadFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(b)
}

// qa2specRow returns the session row for id, or nil when there is none.
func qa2specRow(t *testing.T, db *store.DB, id string) *store.Transfer {
	t.Helper()
	row, err := db.GetTransfer(id)
	if err != nil {
		t.Fatalf("GetTransfer(%s): %v", id, err)
	}
	return row
}

// qa2specOpen opens an upload session for data with the correct whole-file hash.
func qa2specOpen(t *testing.T, tm *Manager, id, target string, data []byte, chunkSize int, overwrite bool, device string) {
	t.Helper()
	if _, err := tm.OpenSession(id, target, int64(len(data)), chunkSize, qa2specSum(data), overwrite, device); err != nil {
		t.Fatalf("OpenSession(%s): %v", id, err)
	}
}

// qa2specWriteChunks sends every chunk of data except those listed in skip.
func qa2specWriteChunks(t *testing.T, tm *Manager, id string, data []byte, chunkSize int, skip ...int) {
	t.Helper()
	for n := 0; n < qa2specChunkCount(len(data), chunkSize); n++ {
		if qa2specContains(skip, n) {
			continue
		}
		c := qa2specChunk(data, chunkSize, n)
		if err := tm.WriteChunk(id, n, c, qa2specSum(c)); err != nil {
			t.Fatalf("WriteChunk(%s, %d): %v", id, n, err)
		}
	}
}

// qa2specHoldCompletion starts CompleteWithPublisher for id and returns once the
// publisher is running, so the completion lease is held. The publisher blocks
// until release is called. The lease is released when the test ends.
func qa2specHoldCompletion(t *testing.T, tm *Manager, id string) (done chan error, release func()) {
	t.Helper()
	started := make(chan struct{})
	gate := make(chan struct{})
	var once sync.Once
	release = func() { once.Do(func() { close(gate) }) }
	t.Cleanup(release)
	done = make(chan error, 1)
	go func() {
		_, _, err := tm.CompleteWithPublisher(id, func(string, io.Reader, bool) (os.FileInfo, error) {
			close(started)
			<-gate
			return nil, qa2specErrHeld
		})
		done <- err
	}()
	select {
	case <-started:
	case <-time.After(10 * time.Second):
		t.Fatal("completion did not reach its publisher within 10s")
	}
	return done, release
}

// qa2specWait waits for a completion result after release.
func qa2specWait(t *testing.T, done chan error) error {
	t.Helper()
	select {
	case err := <-done:
		return err
	case <-time.After(10 * time.Second):
		t.Fatal("completion did not return within 10s after release")
		return nil
	}
}

// TestQA2SpecFR20_OpenRefusesExistingDestination checks the open-time overwrite rule.
func TestQA2SpecFR20_OpenRefusesExistingDestination(t *testing.T) {
	// FR-20: "Given a file exists at the destination path, when POST /transfers is sent with overwrite omitted, then the response is 409 CONFLICT and no session is created."
	// Boundary: overwrite=true may open over the same destination (UC-UPLOAD replaces only after the checks pass).
	// catches: a session is opened over an existing file, or a row is left behind after refusal.
	tm, db, dataDir := setupManager(t)
	target := filepath.Join(dataDir, "exists.bin")
	if err := os.WriteFile(target, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	data := []byte("new bytes")

	_, err := tm.OpenSession("qa2-conflict", target, int64(len(data)), 4, qa2specSum(data), false, "dev-a")
	if !errors.Is(err, ErrDestinationExists) {
		t.Errorf("OpenSession over existing destination: err = %v, want ErrDestinationExists", err)
	}
	if qa2specRow(t, db, "qa2-conflict") != nil {
		t.Errorf("session row created for a refused open")
	}

	if _, err := tm.OpenSession("qa2-overwrite", target, int64(len(data)), 4, qa2specSum(data), true, "dev-a"); err != nil {
		t.Errorf("OpenSession with overwrite=true over existing destination refused: %v", err)
	}
}

// TestQA2SpecFR21_ChunkWithWrongHashRefused checks the per-chunk hash.
func TestQA2SpecFR21_ChunkWithWrongHashRefused(t *testing.T) {
	// FR-21: "Given a chunk body whose SHA-256 differs from X-Chunk-Sha256, when PUT /transfers/{id}/chunks/{n} is sent, then the response is 409 with code CHUNK_HASH_MISMATCH and is not 204."
	// The package returns ErrChunkMismatch; the 409 mapping is server-side.
	// catches: a chunk with the wrong hash is stored or reported as received.
	tm, db, dataDir := setupManager(t)
	data := []byte("abcdefgh")
	id := "qa2-badchunk"
	qa2specOpen(t, tm, id, filepath.Join(dataDir, "bad.bin"), data, 4, false, "dev-a")
	c0 := qa2specChunk(data, 4, 0)

	if err := tm.WriteChunk(id, 0, c0, qa2specSum([]byte("not the chunk"))); !errors.Is(err, ErrChunkMismatch) {
		t.Errorf("WriteChunk with wrong hash: err = %v, want ErrChunkMismatch", err)
	}
	received, err := db.ChunkNumbers(id)
	if err != nil {
		t.Fatalf("ChunkNumbers: %v", err)
	}
	if qa2specContains(received, 0) {
		t.Errorf("chunk 0 recorded as received despite the hash mismatch")
	}

	// Boundary: the matching hash is accepted.
	if err := tm.WriteChunk(id, 0, c0, qa2specSum(c0)); err != nil {
		t.Errorf("WriteChunk with matching hash refused: %v", err)
	}
}

// TestQA2SpecFR22_WholeFileMismatchNotPublished checks the whole-file hash at complete.
func TestQA2SpecFR22_WholeFileMismatchNotPublished(t *testing.T) {
	// FR-22: "Given uploaded bytes whose SHA-256 differs from the value given at open, when POST /transfers/{id}/complete is sent, then the response is 422 with code HASH_MISMATCH and no file is published at the destination."
	// The package returns ErrFileMismatch; the 422 mapping is server-side.
	// catches: a file whose hash does not match is published.
	tm, _, dataDir := setupManager(t)
	data := []byte("abcdefgh")
	id := "qa2-hashbad"
	target := filepath.Join(dataDir, "hashbad.bin")
	if _, err := tm.OpenSession(id, target, int64(len(data)), 4, qa2specSum([]byte("zzzzzzzz")), false, "dev-a"); err != nil {
		t.Fatalf("OpenSession: %v", err)
	}
	qa2specWriteChunks(t, tm, id, data, 4)

	if _, _, err := tm.Complete(id); !errors.Is(err, ErrFileMismatch) {
		t.Errorf("Complete with wrong whole-file hash: err = %v, want ErrFileMismatch", err)
	}
	if qa2specPathExists(target) {
		t.Errorf("file published at %s despite the hash mismatch", target)
	}
}

// TestQA2SpecFR23_CompletePublishesVerifiedFile checks a successful complete.
func TestQA2SpecFR23_CompletePublishesVerifiedFile(t *testing.T) {
	// FR-23: "Given all chunks received and the whole-file hash matching, when POST /transfers/{id}/complete is sent, then the response is 200 with sha256 and verified=true, and the file exists at the destination."
	// The sha256 and verified fields are added by the server and are not asserted here.
	// catches: the file is not published, has the wrong bytes, or the session stays open.
	tm, db, dataDir := setupManager(t)
	data := []byte("hello world")
	id := "qa2-complete"
	target := filepath.Join(dataDir, "complete.bin")
	qa2specOpen(t, tm, id, target, data, 4, false, "dev-a")
	qa2specWriteChunks(t, tm, id, data, 4)

	info, path, err := tm.Complete(id)
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if info == nil || path != target {
		t.Errorf("Complete returned info=%v path=%q, want a file info for %s", info, path, target)
	}
	if got := qa2specReadFile(t, target); got != string(data) {
		t.Errorf("published bytes = %q, want %q", got, string(data))
	}
	if row := qa2specRow(t, db, id); row == nil || row.Status != "completed" {
		t.Errorf("session status after complete = %+v, want completed", row)
	}
}

// TestQA2SpecFR24_IncompleteRefusedAndSessionStaysOpen checks complete with missing chunks.
func TestQA2SpecFR24_IncompleteRefusedAndSessionStaysOpen(t *testing.T) {
	// FR-24: "Given chunk n was never received, when POST /transfers/{id}/complete is sent, then the response is 409 with code UPLOAD_INCOMPLETE; and given chunk n is then sent and complete is called again, then the result is as in FR-23."
	// The package returns ErrIncomplete; the 409 mapping is server-side.
	// catches: a premature complete publishes or closes the session.
	tm, db, dataDir := setupManager(t)
	data := []byte("hello world") // three chunks of 4, 4 and 3 bytes
	id := "qa2-incomplete"
	target := filepath.Join(dataDir, "incomplete.bin")
	qa2specOpen(t, tm, id, target, data, 4, false, "dev-a")
	qa2specWriteChunks(t, tm, id, data, 4, 2)

	if _, _, err := tm.Complete(id); !errors.Is(err, ErrIncomplete) {
		t.Fatalf("Complete with chunk 2 missing: err = %v, want ErrIncomplete", err)
	}
	if row := qa2specRow(t, db, id); row == nil || row.Status != "open" {
		t.Errorf("session after premature complete = %+v, want status open", row)
	}
	if qa2specPathExists(target) {
		t.Errorf("destination published while a chunk is missing")
	}

	// Send the missing chunk; chunks already received are no-ops.
	qa2specWriteChunks(t, tm, id, data, 4)
	if _, _, err := tm.Complete(id); err != nil {
		t.Fatalf("Complete after sending the missing chunk: %v", err)
	}
	if got := qa2specReadFile(t, target); got != string(data) {
		t.Errorf("published bytes = %q, want %q", got, string(data))
	}
}

// TestQA2SpecFR25_DestinationAppearingDuringUploadConflicts checks the late-conflict rule.
func TestQA2SpecFR25_DestinationAppearingDuringUploadConflicts(t *testing.T) {
	// FR-25: "Given the destination file is created after a session opened with overwrite=false, when POST /transfers/{id}/complete is sent, then the response is 409 CONFLICT and the destination's bytes are unchanged."
	// Two paths are tested: Complete (the engine's own publish) and CompleteWithPublisher with the fsops publisher that the server uses.
	// For the publisher path the conflict may be either sentinel (engine ErrDestinationExists or fsops ErrConflict); both mean "destination already exists" and map to 409 CONFLICT.
	// catches: a file that appeared during the upload is replaced.
	data := []byte("hello world")
	for _, usePublisher := range []bool{false, true} {
		name := "complete"
		if usePublisher {
			name = "publisher"
		}
		t.Run(name, func(t *testing.T) {
			tm, _, dataDir := setupManager(t)
			target := filepath.Join(dataDir, "late.bin")
			id := "qa2-late-" + name
			qa2specOpen(t, tm, id, target, data, 4, false, "dev-a")
			qa2specWriteChunks(t, tm, id, data, 4)
			if err := os.WriteFile(target, []byte("other"), 0o644); err != nil {
				t.Fatal(err)
			}

			var err error
			if usePublisher {
				ops := fsops.New([]string{dataDir}, false)
				_, _, err = tm.CompleteWithPublisher(id, ops.PublishReader)
				if err == nil {
					t.Fatal("publisher path replaced a destination created during the upload")
				}
				if !errors.Is(err, ErrDestinationExists) && !errors.Is(err, fsops.ErrConflict) {
					t.Errorf("publisher path: err = %v, want a destination-exists conflict", err)
				}
			} else {
				_, _, err = tm.Complete(id)
				if err == nil {
					t.Fatal("Complete replaced a destination created during the upload")
				}
				if !errors.Is(err, ErrDestinationExists) {
					t.Errorf("Complete: err = %v, want ErrDestinationExists", err)
				}
			}
			if got := qa2specReadFile(t, target); got != "other" {
				t.Errorf("destination bytes = %q, want unchanged %q", got, "other")
			}
		})
	}
}

// TestQA2SpecFR26_OverwriteRefusedByPublisherKeepsDestination checks an overwrite=true session whose grant is refused at publish.
func TestQA2SpecFR26_OverwriteRefusedByPublisherKeepsDestination(t *testing.T) {
	// FR-26: "Given the modify grant is removed before complete on an overwrite=true session, when POST /transfers/{id}/complete is sent, then the response is 403 with code CAPABILITY_DENIED and the existing destination is unchanged."
	// A refusing publisher stands in for the server's grant re-check; the grant check and the 403 code are server-side and not tested here.
	// catches: the existing destination is replaced before the publisher refuses.
	tm, _, dataDir := setupManager(t)
	data := []byte("hello world")
	target := filepath.Join(dataDir, "grant.bin")
	if err := os.WriteFile(target, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	id := "qa2-grant"
	qa2specOpen(t, tm, id, target, data, 4, true, "dev-a")
	qa2specWriteChunks(t, tm, id, data, 4)

	deny := func(string, io.Reader, bool) (os.FileInfo, error) { return nil, qa2specErrDenied }
	if _, _, err := tm.CompleteWithPublisher(id, deny); !errors.Is(err, qa2specErrDenied) {
		t.Errorf("CompleteWithPublisher with refusing publisher: err = %v, want the publisher's refusal", err)
	}
	if got := qa2specReadFile(t, target); got != "old" {
		t.Errorf("destination bytes = %q, want unchanged %q", got, "old")
	}
}

// TestQA2SpecFR27_DeleteSessionKeepsPublishedFile checks DELETE /transfers/{id}.
func TestQA2SpecFR27_DeleteSessionKeepsPublishedFile(t *testing.T) {
	// FR-27: "Given a session whose file is published at path P, when DELETE /transfers/{id} is sent for that session, then P still exists with its bytes."
	// The open-session half of the same rule is also checked: the row and its temp file are removed.
	// catches: deleting a session removes a published destination, or leaves the temp file or row behind.
	tm, db, dataDir := setupManager(t)
	data := []byte("published bytes")

	openID := "qa2-del-open"
	qa2specOpen(t, tm, openID, filepath.Join(dataDir, "never.bin"), data, 4, false, "dev-a")
	row := qa2specRow(t, db, openID)
	if row == nil {
		t.Fatal("open session row missing")
	}
	if !qa2specPathExists(row.TempPath) {
		t.Fatalf("temp file %s missing before delete", row.TempPath)
	}
	if err := tm.DeleteSession(openID); err != nil {
		t.Errorf("DeleteSession(open): %v", err)
	}
	if qa2specRow(t, db, openID) != nil {
		t.Errorf("session row still present after delete")
	}
	if qa2specPathExists(row.TempPath) {
		t.Errorf("temp file %s left behind after delete", row.TempPath)
	}

	pubID := "qa2-del-pub"
	target := filepath.Join(dataDir, "published.bin")
	qa2specOpen(t, tm, pubID, target, data, 4, false, "dev-a")
	qa2specWriteChunks(t, tm, pubID, data, 4)
	if _, _, err := tm.Complete(pubID); err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if err := tm.DeleteSession(pubID); err != nil {
		t.Fatalf("DeleteSession(completed): %v", err)
	}
	if got := qa2specReadFile(t, target); got != string(data) {
		t.Errorf("published file = %q after session delete, want %q", got, string(data))
	}
}

// TestQA2SpecFR28_DeleteDuringCompletionReturnsActive checks delete while completion holds the session.
func TestQA2SpecFR28_DeleteDuringCompletionReturnsActive(t *testing.T) {
	// FR-28: "Given a completion in progress holds the session, when DELETE /transfers/{id} is sent, then the response is 409 with code TRANSFER_ACTIVE and the session still exists."
	// The package returns ErrSessionActive; the 409 mapping is server-side.
	// catches: the row is removed while completion holds the session.
	tm, db, dataDir := setupManager(t)
	data := []byte("hello world")
	id := "qa2-del-active"
	qa2specOpen(t, tm, id, filepath.Join(dataDir, "active.bin"), data, 4, false, "dev-a")
	qa2specWriteChunks(t, tm, id, data, 4)
	done, release := qa2specHoldCompletion(t, tm, id)

	if err := tm.DeleteSession(id); !errors.Is(err, ErrSessionActive) {
		t.Errorf("DeleteSession during completion: err = %v, want ErrSessionActive", err)
	}
	if qa2specRow(t, db, id) == nil {
		t.Errorf("session removed while completion held it")
	}
	release()
	_ = qa2specWait(t, done)
}

// TestQA2SpecFR32_ChunkPutDuringCompletionRefused checks chunk writes while completion holds the session.
func TestQA2SpecFR32_ChunkPutDuringCompletionRefused(t *testing.T) {
	// FR-32: "Given a completion in progress holds the session, when PUT /transfers/{id}/chunks/{n} is sent, then the response is 409 with code TRANSFER_ACTIVE."
	// The chunk resent here is already received, so an unblocked write would succeed as a no-op; a refusal is therefore a real check.
	// catches: a chunk write is accepted while completion holds the session.
	tm, _, dataDir := setupManager(t)
	data := []byte("hello world")
	id := "qa2-chunk-active"
	qa2specOpen(t, tm, id, filepath.Join(dataDir, "chunkactive.bin"), data, 4, false, "dev-a")
	qa2specWriteChunks(t, tm, id, data, 4)
	done, release := qa2specHoldCompletion(t, tm, id)

	c0 := qa2specChunk(data, 4, 0)
	if err := tm.WriteChunk(id, 0, c0, qa2specSum(c0)); !errors.Is(err, ErrSessionActive) {
		t.Errorf("WriteChunk during completion: err = %v, want ErrSessionActive", err)
	}
	release()
	_ = qa2specWait(t, done)
}

// TestQA2SpecFR36_CompleteWritesNoAuditEntry checks that a successful upload completion appends no audit entry.
func TestQA2SpecFR36_CompleteWritesNoAuditEntry(t *testing.T) {
	// FR-36: "Given a delete, rename, move or upload completion has succeeded, when GET /v1/audit is read, then no entry records that operation."
	// At package level only the upload completion writes to the store; delete, rename and move take no store handle and are not tested here.
	// catches: Complete appends an audit row for the file operation.
	tm, db, dataDir := setupManager(t)
	data := []byte("audit me")
	id := "qa2-audit"
	qa2specOpen(t, tm, id, filepath.Join(dataDir, "audited.bin"), data, 4, false, "dev-a")
	qa2specWriteChunks(t, tm, id, data, 4)

	before, err := db.AuditEntries(500, 0)
	if err != nil {
		t.Fatalf("AuditEntries before: %v", err)
	}
	if _, _, err := tm.Complete(id); err != nil {
		t.Fatalf("Complete: %v", err)
	}
	after, err := db.AuditEntries(500, 0)
	if err != nil {
		t.Fatalf("AuditEntries after: %v", err)
	}
	if len(after) != len(before) {
		t.Errorf("audit entries went from %d to %d across a successful upload completion", len(before), len(after))
	}
}

// TestQA2SpecQR1_DeviceSessionLimitFifthRefused checks the per-device session count.
func TestQA2SpecQR1_DeviceSessionLimitFifthRefused(t *testing.T) {
	// QR-1: "Given a device holding 4 open sessions, when POST /transfers is sent, then the response is 429 with code RESOURCE_LIMIT."
	// The package returns ErrQuotaExceeded; the 429 mapping is server-side. The fourth session must open (boundary).
	// catches: a fifth open session is admitted for one device.
	tm, db, dataDir := setupManager(t)
	sum := qa2specSum([]byte("x"))
	for i := 0; i < 4; i++ {
		id := fmt.Sprintf("qa2-dev-a-%d", i)
		if _, err := tm.OpenSession(id, filepath.Join(dataDir, id+".bin"), 1, 1, sum, false, "qa2-dev-a"); err != nil {
			t.Fatalf("session %d of 4 for one device refused: %v", i+1, err)
		}
	}
	id := "qa2-dev-a-4"
	if _, err := tm.OpenSession(id, filepath.Join(dataDir, id+".bin"), 1, 1, sum, false, "qa2-dev-a"); !errors.Is(err, ErrQuotaExceeded) {
		t.Errorf("fifth session for one device: err = %v, want ErrQuotaExceeded", err)
	}
	if qa2specRow(t, db, id) != nil {
		t.Errorf("fifth session row created")
	}
}

// TestQA2SpecQR1_DeviceByteLimit checks the per-device reserved-byte limit.
func TestQA2SpecQR1_DeviceByteLimit(t *testing.T) {
	// QR-1: "Opening an upload session must return 429 RESOURCE_LIMIT when the device already holds ... 64 GiB reserved."
	// 64 GiB = 68719476736 bytes. A session of exactly 64 GiB is at the limit and must open; one more byte must be refused.
	// Sparse only: OpenSession truncates the temp file and writes no bytes, so this test allocates no data.
	// catches: the device admits more than 64 GiB of reservations, or refuses the exact limit.
	tm, _, dataDir := setupManager(t)
	const gib = int64(1) << 30
	chunk := 32 << 20
	if _, err := tm.OpenSession("qa2-bytes-1", filepath.Join(dataDir, "b1.bin"), 64*gib, chunk, qa2specSum(nil), false, "qa2-bytes-dev"); err != nil {
		t.Fatalf("64 GiB session (at the device byte limit) refused: %v", err)
	}
	if _, err := tm.OpenSession("qa2-bytes-2", filepath.Join(dataDir, "b2.bin"), 1, 1, qa2specSum([]byte("x")), false, "qa2-bytes-dev"); !errors.Is(err, ErrQuotaExceeded) {
		t.Errorf("1-byte session with device at 64 GiB reserved: err = %v, want ErrQuotaExceeded", err)
	}
}

// TestQA2SpecQR1_HostSessionLimit checks the host-wide session count.
func TestQA2SpecQR1_HostSessionLimit(t *testing.T) {
	// QR-1: "given the host holds 16 open sessions from other devices, when POST /transfers is sent, then the response is 429 RESOURCE_LIMIT."
	// Sixteen sessions are opened from sixteen devices; the seventeenth from a new device must be refused.
	// catches: the host admits a seventeenth open session.
	tm, _, dataDir := setupManager(t)
	sum := qa2specSum([]byte("x"))
	for i := 0; i < 16; i++ {
		id := fmt.Sprintf("qa2-host-%02d", i)
		dev := fmt.Sprintf("qa2-hdev-%02d", i)
		if _, err := tm.OpenSession(id, filepath.Join(dataDir, id+".bin"), 1, 1, sum, false, dev); err != nil {
			t.Fatalf("host session %d of 16 refused: %v", i+1, err)
		}
	}
	if _, err := tm.OpenSession("qa2-host-16", filepath.Join(dataDir, "qa2-host-16.bin"), 1, 1, sum, false, "qa2-hdev-16"); !errors.Is(err, ErrQuotaExceeded) {
		t.Errorf("seventeenth host session: err = %v, want ErrQuotaExceeded", err)
	}
}

// TestQA2SpecQR1_HostByteLimit checks the host-wide reserved-byte limit.
func TestQA2SpecQR1_HostByteLimit(t *testing.T) {
	// QR-1: "given the host holds ... 128 GiB reserved" refuses the next open. 64 GiB + 64 GiB = 128 GiB is the limit: the second 64 GiB session must open, and one more byte from a third device must be refused.
	// Sparse only: no bytes are written (see TestQA2SpecQR1_DeviceByteLimit).
	// catches: the host admits more than 128 GiB of reservations.
	tm, _, dataDir := setupManager(t)
	const gib = int64(1) << 30
	chunk := 32 << 20
	if _, err := tm.OpenSession("qa2-hb-1", filepath.Join(dataDir, "hb1.bin"), 64*gib, chunk, qa2specSum(nil), false, "qa2-hb-dev-1"); err != nil {
		t.Fatalf("first 64 GiB session refused: %v", err)
	}
	if _, err := tm.OpenSession("qa2-hb-2", filepath.Join(dataDir, "hb2.bin"), 64*gib, chunk, qa2specSum(nil), false, "qa2-hb-dev-2"); err != nil {
		t.Fatalf("second 64 GiB session (host reaches 128 GiB) refused: %v", err)
	}
	if _, err := tm.OpenSession("qa2-hb-3", filepath.Join(dataDir, "hb3.bin"), 1, 1, qa2specSum([]byte("x")), false, "qa2-hb-dev-3"); !errors.Is(err, ErrQuotaExceeded) {
		t.Errorf("1-byte session with host at 128 GiB reserved: err = %v, want ErrQuotaExceeded", err)
	}
}

// TestQA2SpecQR2_OversizeDeclarationRefused checks the declared-size cap.
func TestQA2SpecQR2_OversizeDeclarationRefused(t *testing.T) {
	// QR-2: "Given declared size 68719476737, when POST /transfers is sent, then the response is 413 PAYLOAD_TOO_LARGE."
	// 68719476737 is 64 GiB + 1. The package returns ErrTooLarge; the 413 mapping is server-side. The chunkSize cap (400 BAD_REQUEST) is HTTP-level and is not tested here.
	// catches: a declaration above 64 GiB opens a session.
	tm, db, dataDir := setupManager(t)
	id := "qa2-oversize"
	_, err := tm.OpenSession(id, filepath.Join(dataDir, "huge.bin"), 68719476737, 4, qa2specSum(nil), false, "dev-a")
	if !errors.Is(err, ErrTooLarge) {
		t.Errorf("OpenSession with 64 GiB + 1 bytes: err = %v, want ErrTooLarge", err)
	}
	if qa2specRow(t, db, id) != nil {
		t.Errorf("session row created for an oversize declaration")
	}
}

// TestQA2SpecQR3_StaleSessionSweptAfterSevenDays checks the 7-day stale-session sweep.
func TestQA2SpecQR3_StaleSessionSweptAfterSevenDays(t *testing.T) {
	// QR-3: "Given a session whose last activity is 7 days old, when the hourly sweep runs, then the session is eligible for removal."
	// The sweep is run directly (CleanupStaleSessions). Eligibility is taken as the sweep removing the row. A session one minute past 7 days is swept; one at 6 days 23 hours is kept.
	// catches: a session past the 7-day retention survives the sweep, or a fresh session is swept.
	tm, db, dataDir := setupManager(t)
	data := []byte("data")
	qa2specOpen(t, tm, "qa2-stale-old", filepath.Join(dataDir, "stale-old.bin"), data, 4, false, "dev-a")
	qa2specOpen(t, tm, "qa2-stale-edge", filepath.Join(dataDir, "stale-edge.bin"), data, 4, false, "dev-a")

	now := time.Now()
	if err := db.TouchOpenTransfer("qa2-stale-old", now.Add(-(7*24*time.Hour + time.Minute)).Unix()); err != nil {
		t.Fatalf("age session: %v", err)
	}
	if err := db.TouchOpenTransfer("qa2-stale-edge", now.Add(-167*time.Hour).Unix()); err != nil { // 6 days 23 hours
		t.Fatalf("age session: %v", err)
	}
	if _, err := tm.CleanupStaleSessions(now); err != nil {
		t.Fatalf("CleanupStaleSessions: %v", err)
	}
	if qa2specRow(t, db, "qa2-stale-old") != nil {
		t.Errorf("session idle for 7 days + 1 minute survived the sweep")
	}
	if qa2specRow(t, db, "qa2-stale-edge") == nil {
		t.Errorf("session idle for 6 days 23 hours was swept")
	}
}

// TestQA2SpecQR3_OrphanTempSweptAfterSevenDays checks age-gated orphan temp cleanup.
func TestQA2SpecQR3_OrphanTempSweptAfterSevenDays(t *testing.T) {
	// QR-3: "given an unreferenced temporary file older than 7 days, when cleanup runs, then the file is removed."
	// A temp file older than 7 days (8 days) is removed; a recent one (1 day) is kept.
	// catches: an old unreferenced temp file survives cleanup, or a recent one is removed.
	tm, _, _ := setupManager(t)
	now := time.Now()
	old := filepath.Join(tm.tempDir, "qa2-orphan-old.tmp")
	recent := filepath.Join(tm.tempDir, "qa2-orphan-recent.tmp")
	for _, p := range []string{old, recent} {
		if err := os.WriteFile(p, []byte("partial"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Chtimes(old, now.Add(-8*24*time.Hour), now.Add(-8*24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(recent, now.Add(-24*time.Hour), now.Add(-24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, err := tm.CleanupStaleSessions(now); err != nil {
		t.Fatalf("CleanupStaleSessions: %v", err)
	}
	if qa2specPathExists(old) {
		t.Errorf("orphan temp file older than 7 days was not removed")
	}
	if !qa2specPathExists(recent) {
		t.Errorf("orphan temp file younger than 7 days was removed")
	}
}
