package transfer

// Break tests (qa3) for the upload engine: a restart over the same state
// directory, a temp file lost or altered across it, a symlink planted at the
// destination after the open-time check, a failure after publish, a cancel after
// a refused publish, a chunk write that fails, and two sessions contending for one
// new path. Written by qa-break; not run by it.
//
// Run from agent/:
//   go test ./internal/transfer/ -run TestQA3Break -count=1 -v -timeout 300s
//
// A restart is simulated in-process: the store is closed, reopened on the same data
// directory, and a new Manager is built on the same temp directory. No process is
// killed, so a crash between two syscalls is not reproduced. Forced interleavings
// use the publisher callback. The one unforced race is labelled in its docstring.

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/ZaidQamhieh/rfe/agent/internal/store"
)

// qa3Sum returns the lowercase hex SHA-256 of b.
func qa3Sum(b []byte) string {
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}

// qa3Part returns chunk n of content split into chunkSize pieces.
func qa3Part(content []byte, chunkSize, n int) []byte {
	start := n * chunkSize
	end := start + chunkSize
	if end > len(content) {
		end = len(content)
	}
	return content[start:end]
}

// qa3OpenStore opens the store on dir and closes it when the test ends. A second
// Close is harmless because database/sql's Close is idempotent.
func qa3OpenStore(t *testing.T, dir string) *store.DB {
	t.Helper()
	db, err := store.Open(dir)
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// qa3Absent requires that nothing exists at path (Lstat, so a dangling symlink counts).
func qa3Absent(t *testing.T, path, what string) {
	t.Helper()
	if _, err := os.Lstat(path); !os.IsNotExist(err) {
		t.Errorf("%s still exists at %s (lstat err=%v)", what, path, err)
	}
}

// qa3RestartWith opens a three-chunk session (4-byte chunks) over content, writes
// the chunks listed in written, closes the store and reopens it on the same data
// directory with a new Manager, which is what a restarted process does. It returns
// the new Manager, the session id, the destination and the temp path.
func qa3RestartWith(t *testing.T, content []byte, written []int) (*Manager, string, string, string) {
	t.Helper()
	dataDir := t.TempDir()
	tempDir := filepath.Join(dataDir, "tmp")
	target := filepath.Join(dataDir, "restarted.bin")
	id := "qa3-restart"

	db1 := qa3OpenStore(t, dataDir)
	tm1, err := New(db1, tempDir)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := tm1.OpenSession(id, target, int64(len(content)), 4, qa3Sum(content), false, "dev-a"); err != nil {
		t.Fatalf("OpenSession: %v", err)
	}
	for _, n := range written {
		p := qa3Part(content, 4, n)
		if err := tm1.WriteChunk(id, n, p, qa3Sum(p)); err != nil {
			t.Fatalf("WriteChunk %d: %v", n, err)
		}
	}
	if err := db1.Close(); err != nil {
		t.Fatalf("close store before restart: %v", err)
	}

	db2 := qa3OpenStore(t, dataDir)
	tm2, err := New(db2, tempDir)
	if err != nil {
		t.Fatalf("New after restart: %v", err)
	}
	return tm2, id, target, filepath.Join(tempDir, id+".tmp")
}

// TestQA3Break_RestartResumesWithoutLosingChunks: a session with chunks 0 and 2
// received is carried across a restart of the store and manager. Expected: the
// status after restart lists exactly [0 2]; a sweep right after restart keeps the
// live session and its temp file; the missing chunk completes the upload; and the
// published file equals the source. Label: code failure. Basis: the restart
// requirement ("resume or clean failure"), transfer.go:1-7 (the resume flow) and
// openapi.yaml GET /transfers/{id} ("missing chunks, for resume"). Confidence: high.
func TestQA3Break_RestartResumesWithoutLosingChunks(t *testing.T) {
	dataDir := t.TempDir()
	tempDir := filepath.Join(dataDir, "tmp")
	target := filepath.Join(dataDir, "resume.bin")
	content := []byte("0123456789ab") // 12 bytes in three 4-byte chunks
	id := "qa3-resume"

	db1 := qa3OpenStore(t, dataDir)
	tm1, err := New(db1, tempDir)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := tm1.OpenSession(id, target, int64(len(content)), 4, qa3Sum(content), false, "dev-a"); err != nil {
		t.Fatalf("OpenSession: %v", err)
	}
	for _, n := range []int{0, 2} {
		p := qa3Part(content, 4, n)
		if err := tm1.WriteChunk(id, n, p, qa3Sum(p)); err != nil {
			t.Fatalf("WriteChunk %d: %v", n, err)
		}
	}
	if err := db1.Close(); err != nil {
		t.Fatalf("close store: %v", err)
	}

	db2 := qa3OpenStore(t, dataDir)
	tm2, err := New(db2, tempDir)
	if err != nil {
		t.Fatalf("New after restart: %v", err)
	}
	st, err := tm2.Status(id)
	if err != nil {
		t.Fatalf("Status after restart: %v", err)
	}
	if want := []int{0, 2}; !reflect.DeepEqual(st.ReceivedChunks, want) {
		t.Fatalf("received after restart = %v, want %v", st.ReceivedChunks, want)
	}
	if _, err := tm2.CleanupStaleSessions(time.Now()); err != nil {
		t.Fatalf("CleanupStaleSessions after restart: %v", err)
	}
	if _, err := tm2.Lookup(id); err != nil {
		t.Fatalf("live session removed by the sweep after restart: %v", err)
	}
	if _, err := os.Stat(filepath.Join(tempDir, id+".tmp")); err != nil {
		t.Fatalf("temp file of a live session removed after restart: %v", err)
	}
	p1 := qa3Part(content, 4, 1)
	if err := tm2.WriteChunk(id, 1, p1, qa3Sum(p1)); err != nil {
		t.Fatalf("WriteChunk 1 after restart: %v", err)
	}
	if _, _, err := tm2.Complete(id); err != nil {
		t.Fatalf("Complete after restart: %v", err)
	}
	got, err := os.ReadFile(target)
	if err != nil || !bytes.Equal(got, content) {
		t.Errorf("published bytes after restart = %q (err %v), want %q", got, err, content)
	}
}

// TestQA3Break_RestartWithLostOrAlteredTempNeverPublishes: after a restart the temp
// file is either gone or changed on disk. Subtests: the temp file is lost after
// every chunk was recorded (Complete must fail); the temp file is lost before the
// last chunk (the chunk must not be recorded, and Complete reports incomplete); the
// temp bytes were altered (Complete reports a whole-file mismatch). In every case
// no destination is published and the engine does not recreate the temp file.
// Label: code failure. Basis: the restart requirement ("never a corrupt published
// file"); transfer.go:599 (the temp file is opened without O_CREATE, so a lost file
// is not recreated); transfer.go:655-680 (the whole-file hash runs before publish);
// transfer.go:735-739 (publish follows verification only). Confidence: high.
func TestQA3Break_RestartWithLostOrAlteredTempNeverPublishes(t *testing.T) {
	content := []byte("0123456789ab")

	t.Run("temp lost after every chunk was recorded", func(t *testing.T) {
		tm, id, target, temp := qa3RestartWith(t, content, []int{0, 1, 2})
		if err := os.Remove(temp); err != nil {
			t.Fatalf("remove temp: %v", err)
		}
		if _, _, err := tm.Complete(id); err == nil {
			t.Error("Complete succeeded although the temp file of every chunk was lost")
		}
		qa3Absent(t, target, "destination")
		qa3Absent(t, temp, "temp file (must not be recreated)")
	})

	t.Run("temp lost before the last chunk", func(t *testing.T) {
		tm, id, target, temp := qa3RestartWith(t, content, []int{0, 1})
		if err := os.Remove(temp); err != nil {
			t.Fatalf("remove temp: %v", err)
		}
		p2 := qa3Part(content, 4, 2)
		if err := tm.WriteChunk(id, 2, p2, qa3Sum(p2)); err == nil {
			t.Error("WriteChunk reported success into a lost temp file")
		}
		st, err := tm.Status(id)
		if err != nil {
			t.Fatalf("Status: %v", err)
		}
		if want := []int{0, 1}; !reflect.DeepEqual(st.ReceivedChunks, want) {
			t.Errorf("received after a failed write = %v, want %v", st.ReceivedChunks, want)
		}
		if _, _, err := tm.Complete(id); !errors.Is(err, ErrIncomplete) {
			t.Errorf("Complete = %v, want ErrIncomplete", err)
		}
		qa3Absent(t, target, "destination")
		qa3Absent(t, temp, "temp file (must not be recreated)")
	})

	t.Run("temp bytes altered on disk", func(t *testing.T) {
		tm, id, target, temp := qa3RestartWith(t, content, []int{0, 1})
		f, err := os.OpenFile(temp, os.O_WRONLY, 0)
		if err != nil {
			t.Fatalf("open temp for corruption: %v", err)
		}
		if _, err := f.WriteAt([]byte("X"), 0); err != nil {
			_ = f.Close()
			t.Fatalf("corrupt temp: %v", err)
		}
		if err := f.Close(); err != nil {
			t.Fatalf("close corrupted temp: %v", err)
		}
		p2 := qa3Part(content, 4, 2)
		if err := tm.WriteChunk(id, 2, p2, qa3Sum(p2)); err != nil {
			t.Fatalf("WriteChunk 2: %v", err)
		}
		if _, _, err := tm.Complete(id); !errors.Is(err, ErrFileMismatch) {
			t.Errorf("Complete over altered bytes = %v, want ErrFileMismatch", err)
		}
		qa3Absent(t, target, "destination")
		qa3Absent(t, temp, "temp file after a mismatch")
	})
}

// TestQA3Break_RecordingFailureAfterPublishKeepsExactBytesAndNoTemp: the publisher
// writes the verified bytes to the destination and then the store is closed, so
// recording the completion fails. Expected: Complete returns an error (the engine
// surfaces the persistence error, transfer.go:716-718), the destination holds the
// exact verified bytes and not a partial copy, and the temp file is gone. The
// publisher here is a stand-in that writes the bytes in one call, so this checks the
// engine's handling after publish, not the atomicity of the real publisher.
// Label: code failure. Basis: transfer.go:716-718 (a recording failure after the
// bytes are on disk is returned, not dropped), transfer.go:735-739 (publish moves
// the verified file onto its final path), and openapi.yaml /transfers/{id}/complete
// (a 200 means the file is integrity-verified; the destination is never partial).
// Confidence: high.
func TestQA3Break_RecordingFailureAfterPublishKeepsExactBytesAndNoTemp(t *testing.T) {
	dataDir := t.TempDir()
	tempDir := filepath.Join(dataDir, "tmp")
	db := qa3OpenStore(t, dataDir)
	tm, err := New(db, tempDir)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	content := []byte("payload-after-publish")
	target := filepath.Join(dataDir, "recorded.bin")
	id := "qa3-record-fail"
	if _, err := tm.OpenSession(id, target, int64(len(content)), len(content), qa3Sum(content), false, "dev-a"); err != nil {
		t.Fatalf("OpenSession: %v", err)
	}
	if err := tm.WriteChunk(id, 0, content, qa3Sum(content)); err != nil {
		t.Fatalf("WriteChunk: %v", err)
	}

	publish := func(path string, r io.Reader, _ bool) (os.FileInfo, error) {
		data, err := io.ReadAll(r)
		if err != nil {
			return nil, err
		}
		if err := os.WriteFile(path, data, 0o644); err != nil {
			return nil, err
		}
		// The bytes are on disk; the store fails before the completion is recorded.
		_ = db.Close()
		return nil, nil
	}
	if _, _, err := tm.CompleteWithPublisher(id, publish); err == nil {
		t.Error("completion reported success although recording it failed")
	}
	got, err := os.ReadFile(target)
	if err != nil || !bytes.Equal(got, content) {
		t.Errorf("destination after a recording failure = %q (err %v), want exact %q", got, err, content)
	}
	qa3Absent(t, filepath.Join(tempDir, id+".tmp"), "temp file after publish")
}

// TestQA3Break_RefusedPublishThenCancelLeavesNoTempAndNoDestination: a publish is
// refused, either because a destination appeared during the upload (ErrDestinationExists)
// or because the write failed (a full-disk stand-in). The session then stays open
// until the owner cancels it. Expected: the refusal is returned, the destination is
// untouched by the refusal, DeleteSession removes the temp file and the row, and
// the destination still holds the other writer's bytes (or is absent). Label: code
// failure. Basis: openapi.yaml DELETE /transfers/{id} ("removes its temporary upload
// file; it never deletes a file already published") and /transfers/{id}/complete
// ("left untouched rather than being replaced"); transfer.go:688-695 (a publisher
// error leaves the destination alone). Confidence: high.
func TestQA3Break_RefusedPublishThenCancelLeavesNoTempAndNoDestination(t *testing.T) {
	cases := []struct {
		name        string
		destination []byte // nil: the destination is absent
		refusal     error
	}{
		{"conflict appeared during upload", []byte("other"), ErrDestinationExists},
		{"write failed (full-disk stand-in)", nil, errors.New("no space left on device")},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dataDir := t.TempDir()
			tempDir := filepath.Join(dataDir, "tmp")
			db := qa3OpenStore(t, dataDir)
			tm, err := New(db, tempDir)
			if err != nil {
				t.Fatalf("New: %v", err)
			}
			content := []byte("refused!")
			target := filepath.Join(dataDir, "refused.bin")
			id := "qa3-refused"
			if _, err := tm.OpenSession(id, target, int64(len(content)), len(content), qa3Sum(content), false, "dev-a"); err != nil {
				t.Fatalf("OpenSession: %v", err)
			}
			if err := tm.WriteChunk(id, 0, content, qa3Sum(content)); err != nil {
				t.Fatalf("WriteChunk: %v", err)
			}
			if c.destination != nil {
				if err := os.WriteFile(target, c.destination, 0o644); err != nil {
					t.Fatal(err)
				}
			}

			deny := func(string, io.Reader, bool) (os.FileInfo, error) { return nil, c.refusal }
			if _, _, err := tm.CompleteWithPublisher(id, deny); !errors.Is(err, c.refusal) {
				t.Fatalf("CompleteWithPublisher = %v, want the refusal %v", err, c.refusal)
			}
			if err := tm.DeleteSession(id); err != nil {
				t.Fatalf("DeleteSession after the refused publish: %v", err)
			}
			qa3Absent(t, filepath.Join(tempDir, id+".tmp"), "temp file after cancel")
			if row, err := db.GetTransfer(id); err != nil || row != nil {
				t.Errorf("session row remains after cancel: %+v (err %v)", row, err)
			}
			if c.destination == nil {
				qa3Absent(t, target, "destination")
				return
			}
			if got, err := os.ReadFile(target); err != nil || string(got) != string(c.destination) {
				t.Errorf("destination after cancel = %q (err %v), want the other writer's %q", got, err, c.destination)
			}
		})
	}
}

// TestQA3Break_SymlinkAtDestinationNeverRedirectsPublish: a symlink is planted at
// the destination after the session opened, the check that ran at open time having
// passed. Subtests: a dangling link to a path outside the destination with
// overwrite=false (the complete must be refused, and the outside path must not be
// created); an overwrite=true session whose destination is replaced by a link to an
// existing outside file (the outside file must keep its bytes). This exercises the
// engine's default publish, not the server's rooted publisher, which
// fsops/rootedpublish_test.go and transfer_rooting_test.go already cover. Skipped on
// Windows. Label: code failure. Basis: openapi.yaml /transfers/{id}/complete
// ("a file that appeared at the destination during the upload yields 409 CONFLICT and
// is left untouched"); transfer.go:748-754 (os.Link and the O_EXCL copy refuse an
// existing name, including a link, so nothing is written through it); and CLAUDE.md
// "File operations use rooted filesystem handles to prevent symlink escapes" for the
// outside-file invariant. Confidence: high.
func TestQA3Break_SymlinkAtDestinationNeverRedirectsPublish(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink fixture")
	}

	t.Run("dangling link planted, overwrite=false", func(t *testing.T) {
		dataDir := t.TempDir()
		tempDir := filepath.Join(dataDir, "tmp")
		db := qa3OpenStore(t, dataDir)
		tm, err := New(db, tempDir)
		if err != nil {
			t.Fatalf("New: %v", err)
		}
		outside := t.TempDir()
		victim := filepath.Join(outside, "created-through-link.bin") // does not exist
		target := filepath.Join(dataDir, "swapped.bin")
		content := []byte("upload!!")
		id := "qa3-dangling"
		if _, err := tm.OpenSession(id, target, int64(len(content)), len(content), qa3Sum(content), false, "dev-a"); err != nil {
			t.Fatalf("OpenSession: %v", err)
		}
		if err := tm.WriteChunk(id, 0, content, qa3Sum(content)); err != nil {
			t.Fatalf("WriteChunk: %v", err)
		}
		if err := os.Symlink(victim, target); err != nil {
			t.Fatalf("plant symlink: %v", err)
		}

		if _, _, err := tm.Complete(id); !errors.Is(err, ErrDestinationExists) {
			t.Errorf("Complete through a planted symlink = %v, want ErrDestinationExists", err)
		}
		qa3Absent(t, victim, "file created through the planted symlink")
		if fi, err := os.Lstat(target); err != nil {
			t.Errorf("planted symlink disappeared: %v", err)
		} else if fi.Mode()&os.ModeSymlink == 0 {
			t.Errorf("planted symlink was replaced by %v", fi.Mode())
		}
	})

	t.Run("overwrite=true, destination swapped for a link to an outside file", func(t *testing.T) {
		dataDir := t.TempDir()
		tempDir := filepath.Join(dataDir, "tmp")
		db := qa3OpenStore(t, dataDir)
		tm, err := New(db, tempDir)
		if err != nil {
			t.Fatalf("New: %v", err)
		}
		outside := t.TempDir()
		victim := filepath.Join(outside, "keep.bin")
		if err := os.WriteFile(victim, []byte("keep-me"), 0o600); err != nil {
			t.Fatal(err)
		}
		target := filepath.Join(dataDir, "swapped-ow.bin")
		content := []byte("upload!!")
		id := "qa3-swapped-ow"
		if _, err := tm.OpenSession(id, target, int64(len(content)), len(content), qa3Sum(content), true, "dev-a"); err != nil {
			t.Fatalf("OpenSession: %v", err)
		}
		if err := tm.WriteChunk(id, 0, content, qa3Sum(content)); err != nil {
			t.Fatalf("WriteChunk: %v", err)
		}
		if err := os.Symlink(victim, target); err != nil {
			t.Fatalf("plant symlink: %v", err)
		}
		_, _, _ = tm.Complete(id) // the outcome is not asserted; the outside file is
		got, err := os.ReadFile(victim)
		if err != nil || string(got) != "keep-me" {
			t.Errorf("file outside the destination changed through the symlink: %q (err %v)", got, err)
		}
	})
}

// TestQA3Break_TwoSessionsForOneNewPathPublishExactlyOneWinner: two overwrite=false
// sessions, each fully uploaded, complete at the same time to the same new path.
// Expected: exactly one complete succeeds, the other returns ErrDestinationExists,
// and the destination holds exactly one session's bytes (no interleaving). Label:
// code failure. Confidence: medium. Unforced race: the two completions are released
// together with a start barrier, but their interleaving is not forced, so a pass
// shows only that no corruption or double success occurred in this run. Basis:
// openapi.yaml /transfers (overwrite=false is re-checked atomically at complete, so
// a file appearing during the upload conflicts) and transfer.go:744-750 (os.Link
// decides atomically, so exactly one name is created).
func TestQA3Break_TwoSessionsForOneNewPathPublishExactlyOneWinner(t *testing.T) {
	dataDir := t.TempDir()
	tempDir := filepath.Join(dataDir, "tmp")
	db := qa3OpenStore(t, dataDir)
	tm, err := New(db, tempDir)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	target := filepath.Join(dataDir, "contested.bin")
	a, b := []byte("AAAAAAAA"), []byte("BBBBBBBB")
	for _, s := range []struct {
		id      string
		content []byte
		dev     string
	}{{"qa3-contest-a", a, "dev-a"}, {"qa3-contest-b", b, "dev-b"}} {
		if _, err := tm.OpenSession(s.id, target, int64(len(s.content)), len(s.content), qa3Sum(s.content), false, s.dev); err != nil {
			t.Fatalf("OpenSession(%s): %v", s.id, err)
		}
		if err := tm.WriteChunk(s.id, 0, s.content, qa3Sum(s.content)); err != nil {
			t.Fatalf("WriteChunk(%s): %v", s.id, err)
		}
	}

	start := make(chan struct{})
	errs := make(chan error, 2)
	var wg sync.WaitGroup
	for _, id := range []string{"qa3-contest-a", "qa3-contest-b"} {
		wg.Add(1)
		go func(id string) {
			defer wg.Done()
			<-start
			_, _, err := tm.Complete(id)
			errs <- err
		}(id)
	}
	close(start)
	wg.Wait()
	close(errs)

	wins, losses := 0, 0
	for err := range errs {
		switch {
		case err == nil:
			wins++
		case errors.Is(err, ErrDestinationExists):
			losses++
		default:
			t.Errorf("unexpected Complete error: %v", err)
		}
	}
	if wins != 1 || losses != 1 {
		t.Fatalf("wins=%d losses=%d, want exactly one of each", wins, losses)
	}
	got, err := os.ReadFile(target)
	if err != nil || (!bytes.Equal(got, a) && !bytes.Equal(got, b)) {
		t.Errorf("destination is not exactly one session's bytes: %q (err %v)", got, err)
	}
}

// TestQA3Break_FailedChunkWriteIsNotCountedAsReceived: after chunk 0 is stored, the
// temp file is replaced by a directory, so chunk 1 cannot be written. Expected:
// WriteChunk returns an error, the status lists only chunk 0, Complete reports that
// chunks are missing (ErrIncomplete) and publishes nothing. Label: code failure.
// Basis: transfer.go:599-608 (the bytes are written before the chunk is recorded, so
// a failed write is never recorded) and openapi.yaml chunk PUT 204 ("Chunk stored").
// Confidence: high.
func TestQA3Break_FailedChunkWriteIsNotCountedAsReceived(t *testing.T) {
	dataDir := t.TempDir()
	tempDir := filepath.Join(dataDir, "tmp")
	db := qa3OpenStore(t, dataDir)
	tm, err := New(db, tempDir)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	content := []byte("ABCDEFGH")
	target := filepath.Join(dataDir, "failed-write.bin")
	id := "qa3-failed-write"
	if _, err := tm.OpenSession(id, target, int64(len(content)), 4, qa3Sum(content), false, "dev-a"); err != nil {
		t.Fatalf("OpenSession: %v", err)
	}
	p0 := qa3Part(content, 4, 0)
	if err := tm.WriteChunk(id, 0, p0, qa3Sum(p0)); err != nil {
		t.Fatalf("WriteChunk 0: %v", err)
	}
	temp := filepath.Join(tempDir, id+".tmp")
	if err := os.Remove(temp); err != nil {
		t.Fatalf("remove temp: %v", err)
	}
	if err := os.Mkdir(temp, 0o700); err != nil {
		t.Fatalf("mkdir in place of temp: %v", err)
	}

	p1 := qa3Part(content, 4, 1)
	if err := tm.WriteChunk(id, 1, p1, qa3Sum(p1)); err == nil {
		t.Fatal("WriteChunk 1 reported stored although its bytes could not be written")
	}
	st, err := tm.Status(id)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if want := []int{0}; !reflect.DeepEqual(st.ReceivedChunks, want) {
		t.Errorf("received after a failed write = %v, want %v", st.ReceivedChunks, want)
	}
	if _, _, err := tm.Complete(id); !errors.Is(err, ErrIncomplete) {
		t.Errorf("Complete = %v, want ErrIncomplete", err)
	}
	qa3Absent(t, target, "destination")
}
