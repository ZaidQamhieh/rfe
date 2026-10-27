package transfer

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ZaidQamhieh/rfe/agent/internal/store"
	"github.com/google/uuid"
)

func setupManager(t *testing.T) (*Manager, *store.DB, string) {
	t.Helper()
	dataDir := t.TempDir()
	db, err := store.Open(dataDir)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	tempDir := filepath.Join(dataDir, "tmp")
	tm, err := New(db, tempDir)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return tm, db, dataDir
}

func TestCleanupStaleSessionsPrunesOldTerminalHistory(t *testing.T) {
	tm, db, dataDir := setupManager(t)
	old := &store.Transfer{
		ID: "old-completed", TargetPath: filepath.Join(dataDir, "uploaded.bin"),
		TotalSize: 4, ChunkSize: 4, SHA256: sha256hex([]byte("data")),
		TempPath: filepath.Join(dataDir, "tmp", "old-completed.tmp"), TotalChunks: 1,
	}
	if err := db.CreateTransfer(old); err != nil {
		t.Fatalf("create terminal transfer: %v", err)
	}
	if err := db.SetTransferStatus(old.ID, "completed"); err != nil {
		t.Fatalf("mark terminal: %v", err)
	}
	open := &store.Transfer{
		ID: "still-open", TargetPath: filepath.Join(dataDir, "open.bin"),
		TotalSize: 4, ChunkSize: 4, SHA256: sha256hex([]byte("data")),
		TempPath: filepath.Join(dataDir, "tmp", "still-open.tmp"), TotalChunks: 1,
	}
	if err := db.CreateTransfer(open); err != nil {
		t.Fatalf("create open transfer: %v", err)
	}
	cleanupAt := time.Now().Add(store.TransferHistoryRetention + time.Second)
	if err := db.TouchOpenTransfer(open.ID, cleanupAt.Unix()); err != nil {
		t.Fatalf("keep open transfer fresh: %v", err)
	}

	removed, err := tm.CleanupStaleSessions(cleanupAt)
	if err != nil || removed != 1 {
		t.Fatalf("cleanup = (%d, %v), want one expired terminal row", removed, err)
	}
	if got, err := db.GetTransfer(old.ID); err != nil || got != nil {
		t.Fatalf("old terminal history remains: transfer=%+v err=%v", got, err)
	}
	if got, err := db.GetTransfer(open.ID); err != nil || got == nil || got.Status != "open" {
		t.Fatalf("open transfer was pruned: transfer=%+v err=%v", got, err)
	}
}

// TestOpenSession verifies a session is created and retrievable.
func TestOpenSession(t *testing.T) {
	tm, _, dataDir := setupManager(t)
	target := filepath.Join(dataDir, "out", "file.bin")
	id := uuid.New().String()
	content := []byte("hello world")
	hash := sha256hex(content)

	sess, err := tm.OpenSession(id, target, int64(len(content)), 64, hash, false, "")
	if err != nil {
		t.Fatalf("OpenSession: %v", err)
	}
	if sess.Status != "open" {
		t.Fatalf("expected open, got %s", sess.Status)
	}
	if sess.TotalChunks != 1 {
		t.Fatalf("expected 1 chunk, got %d", sess.TotalChunks)
	}

	// Retrieve.
	got, err := tm.Status(id)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if got.ID != id {
		t.Fatalf("id mismatch")
	}
}

// TestOpenSession_DestinationExists verifies that OpenSession returns
// ErrDestinationExists (not a generic error) when overwrite=false and the
// target path already exists on disk.
func TestOpenSession_DestinationExists(t *testing.T) {
	tm, _, dataDir := setupManager(t)
	target := filepath.Join(dataDir, "existing.bin")
	if err := os.WriteFile(target, []byte("already here"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	content := []byte("hello world")
	id := uuid.New().String()
	_, err := tm.OpenSession(id, target, int64(len(content)), 64, sha256hex(content), false, "")
	if !errors.Is(err, ErrDestinationExists) {
		t.Fatalf("expected ErrDestinationExists, got %v", err)
	}
}

// TestOpenSession_OverwriteAllowsExistingDestination verifies that
// overwrite=true bypasses the destination-exists check.
func TestOpenSession_OverwriteAllowsExistingDestination(t *testing.T) {
	tm, _, dataDir := setupManager(t)
	target := filepath.Join(dataDir, "existing.bin")
	if err := os.WriteFile(target, []byte("already here"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	content := []byte("hello world")
	id := uuid.New().String()
	if _, err := tm.OpenSession(id, target, int64(len(content)), 64, sha256hex(content), true, ""); err != nil {
		t.Fatalf("OpenSession with overwrite: %v", err)
	}
}

// TestSingleChunkUploadComplete tests the happy path for a small single-chunk file.
func TestSingleChunkUploadComplete(t *testing.T) {
	tm, _, dataDir := setupManager(t)
	target := filepath.Join(dataDir, "out.bin")
	content := []byte("the quick brown fox")
	hash := sha256hex(content)
	id := uuid.New().String()

	_, err := tm.OpenSession(id, target, int64(len(content)), 1024, hash, false, "")
	if err != nil {
		t.Fatalf("OpenSession: %v", err)
	}

	chunkHash := sha256hex(content)
	if err := tm.WriteChunk(id, 0, content, chunkHash); err != nil {
		t.Fatalf("WriteChunk: %v", err)
	}

	// Status should show chunk 0 received.
	sess, err := tm.Status(id)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if len(sess.ReceivedChunks) != 1 || sess.ReceivedChunks[0] != 0 {
		t.Fatalf("expected [0], got %v", sess.ReceivedChunks)
	}

	// Complete.
	_, finalPath, err := tm.Complete(id)
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if _, err := os.Stat(finalPath); err != nil {
		t.Fatalf("final file missing: %v", err)
	}
	// Verify content.
	data, err := os.ReadFile(finalPath)
	if err != nil {
		t.Fatalf("read final: %v", err)
	}
	if string(data) != string(content) {
		t.Fatalf("content mismatch: got %q want %q", data, content)
	}
}

// TestMultiChunkResume tests the multi-chunk resume scenario:
// open session, upload some chunks, query missing chunks, upload remaining, complete.
func TestMultiChunkResume(t *testing.T) {
	tm, _, dataDir := setupManager(t)
	target := filepath.Join(dataDir, "multi.bin")

	// Build a 30-byte payload in 3 chunks of 10.
	chunk0 := []byte("0123456789")
	chunk1 := []byte("abcdefghij")
	chunk2 := []byte("ABCDEFGHIJ")
	content := append(append(chunk0, chunk1...), chunk2...)
	fileHash := sha256hex(content)
	id := uuid.New().String()

	_, err := tm.OpenSession(id, target, 30, 10, fileHash, false, "")
	if err != nil {
		t.Fatalf("OpenSession: %v", err)
	}

	// Upload chunks 0 and 2 (skip 1 to simulate partial upload).
	if err := tm.WriteChunk(id, 0, chunk0, sha256hex(chunk0)); err != nil {
		t.Fatalf("WriteChunk 0: %v", err)
	}
	if err := tm.WriteChunk(id, 2, chunk2, sha256hex(chunk2)); err != nil {
		t.Fatalf("WriteChunk 2: %v", err)
	}

	// Status should report chunks 0 and 2 received.
	sess, err := tm.Status(id)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if len(sess.ReceivedChunks) != 2 {
		t.Fatalf("expected 2 received, got %v", sess.ReceivedChunks)
	}

	// Derive missing chunks.
	receivedSet := map[int]bool{}
	for _, c := range sess.ReceivedChunks {
		receivedSet[c] = true
	}
	for i := 0; i < sess.TotalChunks; i++ {
		if !receivedSet[i] {
			// Missing chunk — upload it.
			if err := tm.WriteChunk(id, i, chunk1, sha256hex(chunk1)); err != nil {
				t.Fatalf("WriteChunk missing %d: %v", i, err)
			}
		}
	}

	// Idempotency: re-upload chunk 0 — should succeed.
	if err := tm.WriteChunk(id, 0, chunk0, sha256hex(chunk0)); err != nil {
		t.Fatalf("idempotent WriteChunk 0: %v", err)
	}

	// Complete.
	_, finalPath, err := tm.Complete(id)
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	data, _ := os.ReadFile(finalPath)
	if string(data) != string(content) {
		t.Fatalf("content mismatch")
	}
}

// TestHashMismatch verifies Complete rejects a file whose hash doesn't match.
func TestHashMismatch(t *testing.T) {
	tm, _, dataDir := setupManager(t)
	target := filepath.Join(dataDir, "bad.bin")
	content := []byte("real content")
	wrongHash := sha256hex([]byte("wrong content"))
	id := uuid.New().String()

	_, err := tm.OpenSession(id, target, int64(len(content)), 1024, wrongHash, false, "")
	if err != nil {
		t.Fatalf("OpenSession: %v", err)
	}
	if err := tm.WriteChunk(id, 0, content, sha256hex(content)); err != nil {
		t.Fatalf("WriteChunk: %v", err)
	}
	_, _, err = tm.Complete(id)
	if err == nil {
		t.Fatal("expected hash mismatch error")
	}
}

// TestHashMismatchRemovesTempFile verifies that when Complete's whole-file
// hash check fails, the leftover <id>.tmp file is removed rather than left
// behind on disk forever.
func TestHashMismatchRemovesTempFile(t *testing.T) {
	tm, _, dataDir := setupManager(t)
	target := filepath.Join(dataDir, "bad.bin")
	content := []byte("real content")
	wrongHash := sha256hex([]byte("wrong content"))
	id := uuid.New().String()

	sess, err := tm.OpenSession(id, target, int64(len(content)), 1024, wrongHash, false, "")
	if err != nil {
		t.Fatalf("OpenSession: %v", err)
	}
	if err := tm.WriteChunk(id, 0, content, sha256hex(content)); err != nil {
		t.Fatalf("WriteChunk: %v", err)
	}

	tempPath := sess.TempPath
	if _, err := os.Stat(tempPath); err != nil {
		t.Fatalf("expected temp file to exist before Complete: %v", err)
	}

	_, _, err = tm.Complete(id)
	if err == nil {
		t.Fatal("expected hash mismatch error")
	}

	if _, err := os.Stat(tempPath); !os.IsNotExist(err) {
		t.Fatalf("expected temp file %s to be removed after failed Complete, stat err=%v", tempPath, err)
	}
}

// TestChunkHashMismatch verifies WriteChunk rejects bad chunk data.
func TestChunkHashMismatch(t *testing.T) {
	tm, _, dataDir := setupManager(t)
	target := filepath.Join(dataDir, "chunkerr.bin")
	content := []byte("some data")
	id := uuid.New().String()

	_, err := tm.OpenSession(id, target, int64(len(content)), 1024, sha256hex(content), false, "")
	if err != nil {
		t.Fatalf("OpenSession: %v", err)
	}
	err = tm.WriteChunk(id, 0, content, "0000000000000000000000000000000000000000000000000000000000000000")
	if err == nil {
		t.Fatal("expected chunk hash mismatch error")
	}
}

func sha256hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// TestCopyAcross covers the cross-filesystem fallback path used by moveFile
// when os.Rename fails with EXDEV (destination on a different mount). EXDEV
// itself needs two real filesystems, but the copy+rename+remove logic runs on
// one: content must land at dst verbatim and the source temp must be gone.
func TestCopyAcross(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src.tmp")
	dst := filepath.Join(dir, "sub", "final.jpg")
	want := []byte("photo bytes \x00\x01 across a mount")

	if err := os.WriteFile(src, want, 0o600); err != nil {
		t.Fatalf("write src: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		t.Fatalf("mkdir dst: %v", err)
	}
	if err := copyAcross(src, dst); err != nil {
		t.Fatalf("copyAcross: %v", err)
	}

	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("read dst: %v", err)
	}
	if string(got) != string(want) {
		t.Fatalf("dst content = %q, want %q", got, want)
	}
	if _, err := os.Stat(src); !os.IsNotExist(err) {
		t.Fatalf("src should be removed, stat err = %v", err)
	}
	// No stray temp files left behind in dst's directory.
	entries, _ := os.ReadDir(filepath.Dir(dst))
	if len(entries) != 1 {
		t.Fatalf("expected only final.jpg in dest dir, got %d entries", len(entries))
	}
}

// TestComplete_NoReplaceWhenTargetAppearsDuringUpload is the PR-50
// regression: OpenSession's overwrite=false check runs before the bytes are
// uploaded, so a file created in the meantime used to be silently replaced by
// Complete's rename. The publish must fail instead, leaving the file intact.
func TestComplete_NoReplaceWhenTargetAppearsDuringUpload(t *testing.T) {
	tm, _, dataDir := setupManager(t)
	target := filepath.Join(dataDir, "raced.bin")
	content := []byte("uploaded bytes")
	id := uuid.New().String()

	// Target does not exist yet: the session opens with overwrite=false.
	if _, err := tm.OpenSession(id, target, int64(len(content)), 1024, sha256hex(content), false, ""); err != nil {
		t.Fatalf("OpenSession: %v", err)
	}
	if err := tm.WriteChunk(id, 0, content, sha256hex(content)); err != nil {
		t.Fatalf("WriteChunk: %v", err)
	}

	// Someone else creates the target while the upload is in flight.
	existing := []byte("do not clobber me")
	if err := os.WriteFile(target, existing, 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	if _, _, err := tm.Complete(id); !errors.Is(err, ErrDestinationExists) {
		t.Fatalf("want ErrDestinationExists, got %v", err)
	}
	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(got) != string(existing) {
		t.Fatalf("target was clobbered: got %q, want %q", got, existing)
	}
}

// TestComplete_OverwriteReplacesTarget: the same race with overwrite=true is
// the client explicitly asking to replace, and must still succeed.
func TestComplete_OverwriteReplacesTarget(t *testing.T) {
	tm, _, dataDir := setupManager(t)
	target := filepath.Join(dataDir, "replaced.bin")
	content := []byte("uploaded bytes")
	id := uuid.New().String()

	if _, err := tm.OpenSession(id, target, int64(len(content)), 1024, sha256hex(content), true, ""); err != nil {
		t.Fatalf("OpenSession: %v", err)
	}
	if err := tm.WriteChunk(id, 0, content, sha256hex(content)); err != nil {
		t.Fatalf("WriteChunk: %v", err)
	}
	if err := os.WriteFile(target, []byte("old"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if _, _, err := tm.Complete(id); err != nil {
		t.Fatalf("Complete with overwrite: %v", err)
	}
	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(got) != string(content) {
		t.Fatalf("overwrite did not replace target: got %q", got)
	}
}

// TestWriteChunk_RejectsOutOfRangeIndex is the PR-12 regression: the chunk
// index becomes a WriteAt offset, so an unchecked one lets a client seek far
// beyond the declared size and materialise a sparse file of its choosing.
func TestWriteChunk_RejectsOutOfRangeIndex(t *testing.T) {
	tm, _, dataDir := setupManager(t)
	target := filepath.Join(dataDir, "out.bin")
	content := []byte("small")
	id := uuid.New().String()
	if _, err := tm.OpenSession(id, target, int64(len(content)), 1024, sha256hex(content), false, ""); err != nil {
		t.Fatalf("OpenSession: %v", err)
	}

	for _, n := range []int{-1, 1, 1 << 20} {
		if err := tm.WriteChunk(id, n, content, sha256hex(content)); !errors.Is(err, ErrChunkOutOfRange) {
			t.Fatalf("chunk %d: want ErrChunkOutOfRange, got %v", n, err)
		}
	}

	// The temp file must not have grown past the declared size.
	st, err := os.Stat(filepath.Join(dataDir, "tmp", id+".tmp"))
	if err != nil {
		t.Fatalf("stat temp: %v", err)
	}
	if st.Size() != int64(len(content)) {
		t.Fatalf("out-of-range chunk resized the temp file to %d, want %d", st.Size(), len(content))
	}
}

// TestWriteChunk_RejectsWrongLength is PR-12's other half: the body cap only
// bounds the maximum, so a short chunk would leave a hole of zeros inside the
// file that only the whole-file hash would (much later) catch.
func TestWriteChunk_RejectsWrongLength(t *testing.T) {
	tm, _, dataDir := setupManager(t)
	target := filepath.Join(dataDir, "out.bin")
	// Two chunks: 4 bytes each, 6 bytes total => chunk 0 wants 4, chunk 1 wants 2.
	content := []byte("abcdef")
	id := uuid.New().String()
	if _, err := tm.OpenSession(id, target, 6, 4, sha256hex(content), false, ""); err != nil {
		t.Fatalf("OpenSession: %v", err)
	}

	short := []byte("ab")
	if err := tm.WriteChunk(id, 0, short, sha256hex(short)); !errors.Is(err, ErrChunkWrongSize) {
		t.Fatalf("short first chunk: want ErrChunkWrongSize, got %v", err)
	}
	// Correct geometry is accepted.
	if err := tm.WriteChunk(id, 0, content[:4], sha256hex(content[:4])); err != nil {
		t.Fatalf("full first chunk: %v", err)
	}
	if err := tm.WriteChunk(id, 1, content[4:], sha256hex(content[4:])); err != nil {
		t.Fatalf("final short chunk: %v", err)
	}
}

// TestOpenSession_RejectsOversizedDeclaration: OpenSession truncates to the
// declared size, so an unbounded declaration is a free sparse file (PR-12).
func TestOpenSession_RejectsOversizedDeclaration(t *testing.T) {
	tm, _, dataDir := setupManager(t)
	target := filepath.Join(dataDir, "huge.bin")
	id := uuid.New().String()
	_, err := tm.OpenSession(id, target, maxTransferSize+1, 1024, "deadbeef", false, "")
	if !errors.Is(err, ErrTooLarge) {
		t.Fatalf("want ErrTooLarge, got %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(dataDir, "tmp", id+".tmp")); !os.IsNotExist(statErr) {
		t.Fatal("rejected session still created a temp file")
	}
}

// Audit F-03: completing early must report the missing chunks and leave the session open
// so the upload can be finished.
func TestComplete_EarlyKeepsSessionOpen(t *testing.T) {
	tm, _, dataDir := setupManager(t)
	target := filepath.Join(dataDir, "two.bin")
	content := []byte("0123456789abcdef")
	id := uuid.New().String()
	if _, err := tm.OpenSession(id, target, int64(len(content)), 8, sha256hex(content), false, ""); err != nil {
		t.Fatalf("OpenSession: %v", err)
	}
	if err := tm.WriteChunk(id, 0, content[:8], sha256hex(content[:8])); err != nil {
		t.Fatalf("WriteChunk 0: %v", err)
	}
	if _, _, err := tm.Complete(id); !errors.Is(err, ErrIncomplete) {
		t.Fatalf("early Complete = %v, want ErrIncomplete", err)
	}
	if err := tm.WriteChunk(id, 1, content[8:], sha256hex(content[8:])); err != nil {
		t.Fatalf("WriteChunk 1 after an early complete: %v", err)
	}
	if _, _, err := tm.Complete(id); err != nil {
		t.Fatalf("Complete after sending the rest: %v", err)
	}
	if b, _ := os.ReadFile(target); string(b) != string(content) {
		t.Fatalf("final content %q", b)
	}
}

// An uploaded file follows the process umask like any file the user saves, not a private 0600 (audit F-22).
func TestUploadedFileFollowsUmask(t *testing.T) {
	tm, _, dataDir := setupManager(t)
	target := filepath.Join(dataDir, "mode.bin")
	content := []byte("mode check")
	id := uuid.New().String()
	if _, err := tm.OpenSession(id, target, int64(len(content)), 1024, sha256hex(content), false, ""); err != nil {
		t.Fatal(err)
	}
	if err := tm.WriteChunk(id, 0, content, sha256hex(content)); err != nil {
		t.Fatal(err)
	}
	_, finalPath, err := tm.Complete(id)
	if err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(finalPath)
	if err != nil {
		t.Fatal(err)
	}
	// A plain file created with 0666 shows what this process's umask leaves.
	ref := filepath.Join(dataDir, "ref.bin")
	if err := os.WriteFile(ref, nil, 0o666); err != nil {
		t.Fatal(err)
	}
	want, err := os.Stat(ref)
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode().Perm() != want.Mode().Perm() {
		t.Fatalf("uploaded file mode = %o, want the umask's %o", st.Mode().Perm(), want.Mode().Perm())
	}
}
