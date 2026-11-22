package transfer

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ZaidQamhieh/rfe/agent/internal/next/api"
	"github.com/ZaidQamhieh/rfe/agent/internal/next/authz"
	"github.com/ZaidQamhieh/rfe/agent/internal/next/store"
)

// Sizes used by the tests: three chunks, the last one short.
const (
	testChunk = 1000
	testSize  = 2500
)

// gate makes the temp-file sync hook block while it is on, so a test can hold a
// chunk write in progress.
type gate struct {
	on      atomic.Bool
	entered chan struct{}
	release chan struct{}
}

func newGate() *gate { return &gate{entered: make(chan struct{}, 64), release: make(chan struct{})} }

func (g *gate) sync(f *os.File) error {
	if g.on.Load() {
		g.entered <- struct{}{}
		<-g.release
	}
	return f.Sync()
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func chunksOf(t *testing.T, r *rig, id string) []int {
	t.Helper()
	got, err := r.st.ChunkNumbers(id)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func TestHappyPath(t *testing.T) {
	r := newRig(t)
	data := payload(testSize)
	u := r.open(devA, "up1", "out.bin", data, testChunk, false)
	if u.TotalChunks != 3 || u.Status != store.UploadOpen || u.TempName != "up1.tmp" {
		t.Fatalf("session = %+v", u)
	}
	if !exists(r.tempFile("up1")) {
		t.Fatal("temp file not at <dir>/<id>.tmp")
	}
	r.sendAll(devA, "up1", data, testChunk)
	st, err := r.m.Status(devA, "up1")
	if err != nil || !reflect.DeepEqual(st.Chunks, []int{0, 1, 2}) {
		t.Fatalf("Status = %+v, %v", st, err)
	}
	entry, err := r.m.Complete(devA, r.svc, "up1")
	if err != nil {
		t.Fatal(err)
	}
	if entry.Size != testSize || !bytes.Equal(mustRead(t, r.target("out.bin")), data) {
		t.Fatalf("published file wrong: %+v", entry)
	}
	if exists(r.tempFile("up1")) {
		t.Fatal("temp file left after publish")
	}
	row, _ := r.st.UploadByID("up1")
	if row.Status != store.UploadCompleted || row.TerminalAt != r.clock.Now().Unix() {
		t.Fatalf("row = %+v", row)
	}
	if _, err := r.m.Complete(devA, r.svc, "up1"); !errors.Is(err, api.ErrTransferNotOpen) {
		t.Fatalf("second Complete = %v, want ErrTransferNotOpen", err)
	}
	if err := r.send(devA, "up1", data, testChunk, 0); !errors.Is(err, api.ErrTransferNotOpen) {
		t.Fatalf("chunk after completion = %v, want ErrTransferNotOpen", err)
	}
}

func TestZeroByteUpload(t *testing.T) {
	r := newRig(t)
	u := r.open(devA, "empty", "empty.bin", nil, testChunk, false)
	if u.TotalChunks != 1 {
		t.Fatalf("TotalChunks = %d, want 1", u.TotalChunks)
	}
	if err := r.m.WriteChunk(devA, "empty", 0, nil, sum(nil)); err != nil {
		t.Fatal(err)
	}
	if _, err := r.m.Complete(devA, r.svc, "empty"); err != nil {
		t.Fatal(err)
	}
	if b := mustRead(t, r.target("empty.bin")); len(b) != 0 {
		t.Fatalf("published %d bytes, want 0", len(b))
	}
}

func TestResumeAfterCrash(t *testing.T) {
	r := newRig(t)
	data := payload(testSize)
	r.open(devA, "up1", "out.bin", data, testChunk, false)
	for _, n := range []int{0, 2} {
		if err := r.send(devA, "up1", data, testChunk, n); err != nil {
			t.Fatal(err)
		}
	}
	r.restart() // the first Manager is dropped without Close
	st, err := r.m.Status(devA, "up1")
	if err != nil || !reflect.DeepEqual(st.Chunks, []int{0, 2}) {
		t.Fatalf("after restart Status = %+v, %v", st, err)
	}
	if _, err := r.m.Complete(devA, r.svc, "up1"); !errors.Is(err, api.ErrUploadIncomplete) {
		t.Fatalf("early Complete = %v, want ErrUploadIncomplete", err)
	}
	if err := r.send(devA, "up1", data, testChunk, 1); err != nil {
		t.Fatal(err)
	}
	if _, err := r.m.Complete(devA, r.svc, "up1"); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(mustRead(t, r.target("out.bin")), data) {
		t.Fatal("resumed upload differs from the data")
	}
}

func TestImportedSessionsAreReconciledOnStart(t *testing.T) {
	r := newRig(t)
	data := payload(testSize)
	base := store.Upload{DeviceID: "dev-a", TotalSize: testSize, ChunkSize: testChunk, TotalChunks: 3,
		SHA256: strings.ToUpper(sum(data)), Status: store.UploadOpen}
	add := func(id, tempName string, totalChunks int, chunks ...int) {
		u := base
		u.ID, u.TempName, u.TargetPath, u.TotalChunks = id, tempName, r.target(id+".bin"), totalChunks
		if err := r.st.InsertUpload(u); err != nil {
			t.Fatal(err)
		}
		for _, n := range chunks {
			r.st.chunks[id][n] = true
		}
	}
	if err := os.WriteFile(r.tempFile("imp1"), data[:2*testChunk], 0o600); err != nil {
		t.Fatal(err)
	}
	add("imp1", "imp1.tmp", 3, 0, 1, 2, 7) // chunk 2 lies beyond the file, 7 is out of range
	add("imp2", "imp2.tmp", 3, 0, 1)       // temp file missing
	add("imp3", "../evil.tmp", 3)          // temp name escapes the directory
	add("imp4", "imp4.tmp", 9)             // chunk count does not match the geometry
	r.restart()

	if got := chunksOf(t, r, "imp1"); !reflect.DeepEqual(got, []int{0, 1}) {
		t.Fatalf("imp1 chunks = %v, want [0 1]", got)
	}
	if row, _ := r.st.UploadByID("imp1"); row.UpdatedAt != r.clock.Now().Unix() {
		t.Fatalf("imp1 updated_at = %d, want the import time %d", row.UpdatedAt, r.clock.Now().Unix())
	}
	if info, err := os.Stat(r.tempFile("imp1")); err != nil || info.Size() != testSize {
		t.Fatalf("imp1 temp size = %v, %v; want %d", info, err, testSize)
	}
	if got := chunksOf(t, r, "imp2"); len(got) != 0 || !exists(r.tempFile("imp2")) {
		t.Fatalf("imp2 chunks = %v (want none), temp exists = %v", got, exists(r.tempFile("imp2")))
	}
	for _, id := range []string{"imp3", "imp4"} {
		if row, _ := r.st.UploadByID(id); row.Status != store.UploadFailed {
			t.Fatalf("%s status = %q, want failed", id, row.Status)
		}
	}
	// The imported session resumes: send the discarded chunk and complete; the
	// stored hash is uppercase.
	if err := r.send(devA, "imp1", data, testChunk, 2); err != nil {
		t.Fatal(err)
	}
	if _, err := r.m.Complete(devA, r.svc, "imp1"); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(mustRead(t, r.target("imp1.bin")), data) {
		t.Fatal("imported session published wrong bytes")
	}
}

func TestChunkHashChecks(t *testing.T) {
	r := newRig(t)
	data := payload(testSize)
	r.open(devA, "up1", "out.bin", data, testChunk, false)
	chunk := data[:testChunk]

	if err := r.m.WriteChunk(devA, "up1", 0, chunk, sum(data[testChunk:2*testChunk])); !errors.Is(err, api.ErrChunkHashMismatch) {
		t.Fatalf("bad hash = %v, want ErrChunkHashMismatch", err)
	}
	if got := chunksOf(t, r, "up1"); len(got) != 0 {
		t.Fatalf("rows after a rejected chunk: %v", got)
	}
	if !bytes.Equal(mustRead(t, r.tempFile("up1"))[:testChunk], make([]byte, testChunk)) {
		t.Fatal("a rejected chunk was written to the temp file")
	}
	if err := r.m.WriteChunk(devA, "up1", 0, chunk, "not-hex"); !errors.Is(err, api.ErrBadRequest) {
		t.Fatalf("malformed hash = %v, want ErrBadRequest", err)
	}
	// Uppercase digest of the right bytes is accepted.
	if err := r.m.WriteChunk(devA, "up1", 0, chunk, strings.ToUpper(sum(chunk))); err != nil {
		t.Fatalf("uppercase chunk hash: %v", err)
	}
	// Sending it again is a no-op.
	if err := r.m.WriteChunk(devA, "up1", 0, chunk, sum(chunk)); err != nil {
		t.Fatalf("repeated chunk: %v", err)
	}
	if got := chunksOf(t, r, "up1"); !reflect.DeepEqual(got, []int{0}) {
		t.Fatalf("rows = %v", got)
	}
}

func TestChunkShapeChecks(t *testing.T) {
	r := newRig(t)
	data := payload(testSize)
	r.open(devA, "up1", "out.bin", data, testChunk, false)
	for name, c := range map[string]struct {
		n    int
		data []byte
	}{
		"negative index":  {-1, data[:testChunk]},
		"index too large": {3, data[:testChunk]},
		"short chunk":     {0, data[:testChunk-1]},
		"long last chunk": {2, data[:testChunk]},
	} {
		if err := r.m.WriteChunk(devA, "up1", c.n, c.data, sum(c.data)); !errors.Is(err, api.ErrBadRequest) {
			t.Errorf("%s: %v, want ErrBadRequest", name, err)
		}
	}
	if got := chunksOf(t, r, "up1"); len(got) != 0 {
		t.Fatalf("rows after rejected chunks: %v", got)
	}
}

func TestWholeFileHash(t *testing.T) {
	t.Run("mismatch fails the session", func(t *testing.T) {
		r := newRig(t)
		data := payload(testSize)
		req := r.request("up1", "out.bin", data, testChunk, false)
		req.SHA256 = sum([]byte("something else"))
		if _, err := r.m.Open(devA, r.svc, req); err != nil {
			t.Fatal(err)
		}
		r.sendAll(devA, "up1", data, testChunk)
		if _, err := r.m.Complete(devA, r.svc, "up1"); !errors.Is(err, api.ErrHashMismatch) {
			t.Fatalf("Complete = %v, want ErrHashMismatch", err)
		}
		if row, _ := r.st.UploadByID("up1"); row.Status != store.UploadFailed {
			t.Fatalf("status = %q, want failed", row.Status)
		}
		if exists(r.tempFile("up1")) || exists(r.target("out.bin")) {
			t.Fatal("temp file or target present after a hash failure")
		}
		if err := r.send(devA, "up1", data, testChunk, 0); !errors.Is(err, api.ErrTransferNotOpen) {
			t.Fatalf("chunk after failure = %v, want ErrTransferNotOpen", err)
		}
	})
	t.Run("uppercase digest is accepted", func(t *testing.T) {
		r := newRig(t)
		data := payload(testSize)
		req := r.request("up1", "out.bin", data, testChunk, false)
		req.SHA256 = strings.ToUpper(req.SHA256)
		if _, err := r.m.Open(devA, r.svc, req); err != nil {
			t.Fatal(err)
		}
		r.sendAll(devA, "up1", data, testChunk)
		if _, err := r.m.Complete(devA, r.svc, "up1"); err != nil {
			t.Fatalf("Complete with an uppercase digest: %v", err)
		}
	})
}

func TestOwnerRule(t *testing.T) {
	r := newRig(t)
	data := payload(testSize)
	r.open(devA, "up1", "out.bin", data, testChunk, false)
	// A legacy row with no owner.
	if err := r.st.InsertUpload(store.Upload{ID: "legacy", TargetPath: r.target("l.bin"), TotalSize: 1, ChunkSize: 1,
		TotalChunks: 1, SHA256: sum([]byte{1}), Status: store.UploadOpen, TempName: "legacy.tmp", UpdatedAt: 1}); err != nil {
		t.Fatal(err)
	}
	for name, p := range map[string]struct{ who, id string }{
		"other device":     {"b", "up1"},
		"unknown id":       {"a", "nope"},
		"no device on row": {"empty", "legacy"},
	} {
		pr := devB
		switch p.who {
		case "a":
			pr = devA
		case "empty":
			pr.DeviceID = ""
		}
		if _, err := r.m.Status(pr, p.id); !errors.Is(err, api.ErrNotFound) {
			t.Errorf("%s: Status = %v, want ErrNotFound", name, err)
		}
		if err := r.m.WriteChunk(pr, p.id, 0, data[:testChunk], sum(data[:testChunk])); !errors.Is(err, api.ErrNotFound) {
			t.Errorf("%s: WriteChunk = %v, want ErrNotFound", name, err)
		}
		if _, err := r.m.Complete(pr, r.svc, p.id); !errors.Is(err, api.ErrNotFound) {
			t.Errorf("%s: Complete = %v, want ErrNotFound", name, err)
		}
		if err := r.m.Delete(pr, p.id); !errors.Is(err, api.ErrNotFound) {
			t.Errorf("%s: Delete = %v, want ErrNotFound", name, err)
		}
		if _, err := r.m.Lookup(pr, p.id); !errors.Is(err, api.ErrNotFound) {
			t.Errorf("%s: Lookup = %v, want ErrNotFound", name, err)
		}
	}
	if len(chunksOf(t, r, "up1")) != 0 {
		t.Fatal("a refused caller wrote a chunk")
	}
	if _, err := r.m.Open(devA, r.svc, OpenRequest{}); !errors.Is(err, api.ErrBadRequest) {
		t.Fatalf("empty request = %v, want ErrBadRequest", err)
	}
	if _, err := r.m.Open(authz.Principal{}, r.svc, r.request("x", "x.bin", data, testChunk, false)); !errors.Is(err, api.ErrForbidden) {
		t.Fatalf("Open without a device = %v, want ErrForbidden", err)
	}
	// An admin may read, write, complete and delete another device's session,
	// and the legacy row.
	for n := 0; n < 3; n++ {
		if err := r.send(admin, "up1", data, testChunk, n); err != nil {
			t.Fatalf("admin WriteChunk: %v", err)
		}
	}
	if _, err := r.m.Complete(admin, r.svc, "up1"); err != nil {
		t.Fatalf("admin Complete: %v", err)
	}
	if _, err := r.m.Status(admin, "legacy"); err != nil {
		t.Fatalf("admin Status of a legacy row: %v", err)
	}
	if err := r.m.Delete(admin, "legacy"); err != nil {
		t.Fatalf("admin Delete: %v", err)
	}
}

func TestParallelChunks(t *testing.T) {
	r := newRig(t)
	const chunk, n = 1000, 32
	data := payload(chunk*n - 123)
	r.open(devA, "up1", "out.bin", data, chunk, false)
	var wg sync.WaitGroup
	errs := make(chan error, 2*n)
	for i := 0; i < n; i++ {
		for dup := 0; dup < 2; dup++ { // each chunk twice at the same time
			wg.Add(1)
			go func() {
				defer wg.Done()
				errs <- r.send(devA, "up1", data, chunk, i)
			}()
		}
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if got := chunksOf(t, r, "up1"); len(got) != n {
		t.Fatalf("%d chunk rows, want %d", len(got), n)
	}
	if _, err := r.m.Complete(devA, r.svc, "up1"); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(mustRead(t, r.target("out.bin")), data) {
		t.Fatal("parallel upload differs from the data")
	}
}

func TestOverwrite(t *testing.T) {
	data := payload(testSize)
	t.Run("false refuses an existing target at open", func(t *testing.T) {
		r := newRig(t)
		if err := os.WriteFile(r.target("out.bin"), []byte("old"), 0o644); err != nil {
			t.Fatal(err)
		}
		_, err := r.m.Open(devA, r.svc, r.request("up1", "out.bin", data, testChunk, false))
		if !errors.Is(err, api.ErrConflict) {
			t.Fatalf("Open = %v, want ErrConflict", err)
		}
		if row, _ := r.st.UploadByID("up1"); row != nil {
			t.Fatal("a refused open left a row")
		}
	})
	t.Run("false never replaces a file that appears during the upload", func(t *testing.T) {
		r := newRig(t)
		r.open(devA, "up1", "out.bin", data, testChunk, false)
		r.sendAll(devA, "up1", data, testChunk)
		if err := os.WriteFile(r.target("out.bin"), []byte("newcomer"), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := r.m.Complete(devA, r.svc, "up1"); !errors.Is(err, api.ErrConflict) {
			t.Fatalf("Complete = %v, want ErrConflict", err)
		}
		if string(mustRead(t, r.target("out.bin"))) != "newcomer" {
			t.Fatal("the existing file was replaced")
		}
		if row, _ := r.st.UploadByID("up1"); row.Status != store.UploadOpen {
			t.Fatalf("status = %q, want open", row.Status)
		}
	})
	t.Run("true replaces", func(t *testing.T) {
		r := newRig(t)
		if err := os.WriteFile(r.target("out.bin"), []byte("old"), 0o644); err != nil {
			t.Fatal(err)
		}
		r.open(devA, "up1", "out.bin", data, testChunk, true)
		r.sendAll(devA, "up1", data, testChunk)
		if _, err := r.m.Complete(devA, r.svc, "up1"); err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(mustRead(t, r.target("out.bin")), data) {
			t.Fatal("the file was not replaced")
		}
	})
}

func TestCompleteRecoversWhenRecordingFailed(t *testing.T) {
	r := newRig(t)
	data := payload(testSize)
	r.open(devA, "up1", "out.bin", data, testChunk, false)
	r.sendAll(devA, "up1", data, testChunk)
	r.st.failFinishCompleted = true
	entry, err := r.m.Complete(devA, r.svc, "up1")
	if !errors.Is(err, errInjected) || entry == nil {
		t.Fatalf("Complete = %v, %v; want the entry and the store error", entry, err)
	}
	if row, _ := r.st.UploadByID("up1"); row.Status != store.UploadOpen {
		t.Fatalf("status = %q, want open", row.Status)
	}
	// Retrying finds the same bytes at the target and finishes.
	if _, err := r.m.Complete(devA, r.svc, "up1"); err != nil {
		t.Fatalf("retry Complete: %v", err)
	}
	if row, _ := r.st.UploadByID("up1"); row.Status != store.UploadCompleted {
		t.Fatalf("status = %q, want completed", row.Status)
	}
	if exists(r.tempFile("up1")) {
		t.Fatal("temp file left after the retry")
	}
}

func TestIdempotentOpen(t *testing.T) {
	r := newRig(t)
	data := payload(testSize)
	req := r.request("up1", "out.bin", data, testChunk, false)
	r.open(devA, "up1", "out.bin", data, testChunk, false)
	if err := r.send(devA, "up1", data, testChunk, 1); err != nil {
		t.Fatal(err)
	}
	again, err := r.m.Open(devA, r.svc, req)
	if err != nil || !reflect.DeepEqual(again.Chunks, []int{1}) {
		t.Fatalf("re-open = %+v, %v; want the session with chunk 1", again, err)
	}
	changed := req
	changed.Size++
	if _, err := r.m.Open(devA, r.svc, changed); !errors.Is(err, api.ErrConflict) {
		t.Fatalf("re-open with other fields = %v, want ErrConflict", err)
	}
	if _, err := r.m.Open(devB, r.svc, req); !errors.Is(err, api.ErrConflict) {
		t.Fatalf("re-open by another device = %v, want ErrConflict", err)
	}
}

func TestOpenValidationAndLimits(t *testing.T) {
	r := newRig(t, func(c *Config) {
		c.Limits = DefaultLimits()
		c.Limits.MaxTransferSize = 10_000
		c.Limits.MaxActivePerDevice = 2
		c.Limits.MaxReservedPerDevice = 5_000
		c.Limits.MaxActivePerHost = 3
	})
	data := payload(testSize)
	good := r.request("ok", "ok.bin", data, testChunk, false)
	bad := func(mut func(*OpenRequest)) error {
		req := good
		mut(&req)
		_, err := r.m.Open(devA, r.svc, req)
		return err
	}
	cases := map[string]struct {
		mut  func(*OpenRequest)
		want error
	}{
		"id with a slash":   {func(q *OpenRequest) { q.ID = "a/b" }, api.ErrBadRequest},
		"id dot dot":        {func(q *OpenRequest) { q.ID = ".." }, api.ErrBadRequest},
		"empty path":        {func(q *OpenRequest) { q.Path = "" }, api.ErrBadRequest},
		"negative size":     {func(q *OpenRequest) { q.Size = -1 }, api.ErrBadRequest},
		"zero chunk size":   {func(q *OpenRequest) { q.ChunkSize = 0 }, api.ErrBadRequest},
		"huge chunk size":   {func(q *OpenRequest) { q.ChunkSize = MaxChunkSize + 1 }, api.ErrBadRequest},
		"short hash":        {func(q *OpenRequest) { q.SHA256 = "abc" }, api.ErrBadRequest},
		"declared too big":  {func(q *OpenRequest) { q.Size = 10_001 }, api.ErrPayloadTooLarge},
		"over device bytes": {func(q *OpenRequest) { q.Size = 5_001 }, api.ErrResourceLimit},
	}
	for name, c := range cases {
		if err := bad(c.mut); !errors.Is(err, c.want) {
			t.Errorf("%s: %v, want %v", name, err, c.want)
		}
	}
	for _, id := range []string{"s1", "s2"} {
		req := good
		req.ID, req.Path, req.Size, req.SHA256 = id, r.target(id), 10, sum(payload(10))
		if _, err := r.m.Open(devA, r.svc, req); err != nil {
			t.Fatalf("open %s: %v", id, err)
		}
	}
	third := good
	third.ID, third.Path = "s3", r.target("s3")
	if _, err := r.m.Open(devA, r.svc, third); !errors.Is(err, api.ErrResourceLimit) {
		t.Fatalf("third session of one device = %v, want ErrResourceLimit", err)
	}
	if _, err := r.m.Open(devB, r.svc, third); err != nil {
		t.Fatalf("another device's first session: %v", err)
	}
	fourth := third
	fourth.ID, fourth.Path = "s4", r.target("s4")
	if _, err := r.m.Open(admin, r.svc, fourth); !errors.Is(err, api.ErrResourceLimit) {
		t.Fatalf("session over the host limit = %v, want ErrResourceLimit", err)
	}
	// A refused open leaves nothing behind.
	if exists(r.tempFile("s4")) {
		t.Fatal("temp file of a refused session")
	}
}

func TestChunkCountIsBounded(t *testing.T) {
	r := newRig(t)
	req := r.request("many", "many.bin", nil, 1, false)
	req.Size = MaxChunksPerUpload + 1
	if _, err := r.m.Open(devA, r.svc, req); !errors.Is(err, api.ErrBadRequest) {
		t.Fatalf("Open with %d one-byte chunks = %v, want ErrBadRequest", req.Size, err)
	}
	if row, _ := r.st.UploadByID("many"); row != nil || exists(r.tempFile("many")) {
		t.Fatal("a refused open left a row or a temp file")
	}
}

func TestChunkIsSyncedBeforeItsRowIsRecorded(t *testing.T) {
	var r *rig
	var atSync []byte
	var failSync atomic.Bool
	r = newRig(t, func(c *Config) {
		c.SyncFile = func(f *os.File) error {
			r.rec.add("sync")
			b, err := os.ReadFile(f.Name())
			if err != nil {
				return err
			}
			atSync = b
			if failSync.Load() {
				return errInjected
			}
			return f.Sync()
		}
	})
	data := payload(testSize)
	r.open(devA, "up1", "out.bin", data, testChunk, false)
	r.rec.take()
	if err := r.send(devA, "up1", data, testChunk, 1); err != nil {
		t.Fatal(err)
	}
	if got := r.rec.take(); !reflect.DeepEqual(got, []string{"sync", "row"}) {
		t.Fatalf("order = %v, want [sync row]", got)
	}
	if !bytes.Equal(atSync[testChunk:2*testChunk], data[testChunk:2*testChunk]) {
		t.Fatal("the chunk bytes were not in the file when it was synced")
	}
	// A failed sync is never acknowledged and leaves no row.
	failSync.Store(true)
	if err := r.send(devA, "up1", data, testChunk, 2); !errors.Is(err, errInjected) {
		t.Fatalf("send with failing sync = %v, want the sync error", err)
	}
	if got := r.rec.take(); !reflect.DeepEqual(got, []string{"sync"}) {
		t.Fatalf("events after a failed sync = %v, want [sync]", got)
	}
	if got := chunksOf(t, r, "up1"); !reflect.DeepEqual(got, []int{1}) {
		t.Fatalf("rows = %v, want [1]", got)
	}
}

func TestDrainWaitsForChunkWrites(t *testing.T) {
	g := newGate()
	r := newRig(t, func(c *Config) { c.SyncFile = g.sync })
	data := payload(testSize)
	r.open(devA, "up1", "out.bin", data, testChunk, false)
	g.on.Store(true)
	written := make(chan error, 1)
	go func() { written <- r.send(devA, "up1", data, testChunk, 0) }()
	<-g.entered // the write is in progress, before its fsync returns

	drained := make(chan error, 1)
	go func() { drained <- r.m.Close() }()
	for { // wait until the drain has closed the engine to new work
		r.m.mu.Lock()
		closed := r.m.closed
		r.m.mu.Unlock()
		if closed {
			break
		}
	}
	select {
	case err := <-drained:
		t.Fatalf("Close returned (%v) while a chunk write was running", err)
	default:
	}
	// A drain whose context ends first reports it and leaves the write running.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := r.m.Drain(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("Drain with a cancelled context = %v", err)
	}
	if err := r.send(devA, "up1", data, testChunk, 1); !errors.Is(err, ErrClosed) {
		t.Fatalf("new chunk during drain = %v, want ErrClosed", err)
	}
	close(g.release)
	if err := <-written; err != nil {
		t.Fatalf("the in-flight chunk: %v", err)
	}
	if err := <-drained; err != nil {
		t.Fatalf("Close: %v", err)
	}
	if got := chunksOf(t, r, "up1"); !reflect.DeepEqual(got, []int{0}) {
		t.Fatalf("rows = %v, want the acknowledged chunk 0", got)
	}
	if _, err := r.m.Status(devA, "up1"); err != nil {
		t.Fatalf("Status after drain: %v", err)
	}
	if _, err := r.m.Open(devA, r.svc, r.request("up2", "o2.bin", data, testChunk, false)); !errors.Is(err, ErrClosed) {
		t.Fatalf("Open after drain = %v, want ErrClosed", err)
	}
}

func TestDeleteRefusedWhileWriting(t *testing.T) {
	g := newGate()
	r := newRig(t, func(c *Config) { c.SyncFile = g.sync })
	data := payload(testSize)
	r.open(devA, "up1", "out.bin", data, testChunk, false)
	g.on.Store(true)
	written := make(chan error, 1)
	go func() { written <- r.send(devA, "up1", data, testChunk, 0) }()
	<-g.entered
	if err := r.m.Delete(devA, "up1"); !errors.Is(err, api.ErrTransferActive) {
		t.Fatalf("Delete during a write = %v, want ErrTransferActive", err)
	}
	if _, err := r.m.Complete(devA, r.svc, "up1"); !errors.Is(err, api.ErrTransferActive) {
		t.Fatalf("Complete during a write = %v, want ErrTransferActive", err)
	}
	close(g.release)
	if err := <-written; err != nil {
		t.Fatal(err)
	}
	if err := r.m.Delete(devA, "up1"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if row, _ := r.st.UploadByID("up1"); row != nil || exists(r.tempFile("up1")) {
		t.Fatal("row or temp file left after Delete")
	}
}

func TestStaleSweep(t *testing.T) {
	g := newGate()
	r := newRig(t, func(c *Config) { c.SyncFile = g.sync })
	data := payload(testSize)
	for _, id := range []string{"old", "touched", "busy"} {
		r.open(devA, id, id+".bin", data, testChunk, false)
	}
	// "busy" has a chunk write in progress from the start (it refreshed its
	// time then, and the write is still running when the sweep happens).
	g.on.Store(true)
	written := make(chan error, 1)
	go func() { written <- r.send(devA, "busy", data, testChunk, 0) }()
	<-g.entered
	r.clock.Advance(StaleAfter - time.Hour)
	if _, err := r.m.Status(devA, "touched"); err != nil { // refreshes activity
		t.Fatal(err)
	}
	// An orphan temp file nobody names (old) and a young one.
	oldOrphan, youngOrphan := filepath.Join(r.dir, "orphan.tmp"), filepath.Join(r.dir, "young.tmp")
	for _, p := range []string{oldOrphan, youngOrphan} {
		if err := os.WriteFile(p, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	past := r.clock.Now().Add(-StaleAfter - time.Hour)
	if err := os.Chtimes(oldOrphan, past, past); err != nil {
		t.Fatal(err)
	}
	r.clock.Advance(2 * time.Hour) // "old" and "busy" are now past StaleAfter
	if err := os.Chtimes(youngOrphan, r.clock.Now(), r.clock.Now()); err != nil {
		t.Fatal(err)
	}
	if n, err := r.m.Sweep(r.clock.Now()); err != nil || n != 2 {
		t.Fatalf("Sweep = %d, %v; want 2 (the old session and the old orphan)", n, err)
	}
	close(g.release)
	if err := <-written; err != nil {
		t.Fatal(err)
	}
	for id, want := range map[string]bool{"old": false, "touched": true, "busy": true} {
		row, _ := r.st.UploadByID(id)
		if (row != nil) != want || exists(r.tempFile(id)) != want {
			t.Errorf("%s: row present = %v, temp present = %v, want both %v", id, row != nil, exists(r.tempFile(id)), want)
		}
	}
	if exists(oldOrphan) || !exists(youngOrphan) {
		t.Fatalf("old orphan present = %v, young orphan present = %v", exists(oldOrphan), exists(youngOrphan))
	}
}

func TestTerminalHistoryIsPruned(t *testing.T) {
	r := newRig(t)
	data := payload(testSize)
	r.open(devA, "up1", "out.bin", data, testChunk, false)
	r.sendAll(devA, "up1", data, testChunk)
	if _, err := r.m.Complete(devA, r.svc, "up1"); err != nil {
		t.Fatal(err)
	}
	r.clock.Advance(TerminalRetention - time.Hour)
	if _, err := r.m.Sweep(r.clock.Now()); err != nil {
		t.Fatal(err)
	}
	if row, _ := r.st.UploadByID("up1"); row == nil {
		t.Fatal("history pruned before the retention time")
	}
	r.clock.Advance(2 * time.Hour)
	if n, err := r.m.Sweep(r.clock.Now()); err != nil || n != 1 {
		t.Fatalf("Sweep = %d, %v; want 1", n, err)
	}
	if row, _ := r.st.UploadByID("up1"); row != nil {
		t.Fatal("history kept past the retention time")
	}
}

func TestStartAndStopSweeper(t *testing.T) {
	r := newRig(t)
	stop := r.m.StartSweeper(time.Hour)
	stop()
	stop() // may be called twice
	if err := r.m.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestDropDeviceRemovesOnlyThatDevicesUnusedSessions(t *testing.T) {
	r := newRig(t)
	data := payload(testSize)
	r.open(devA, "a1", "a1.bin", data, testChunk, false)
	r.open(devB, "b1", "b1.bin", data, testChunk, false)
	r.m.DropDevice(devA.DeviceID)
	if u, _ := r.m.cfg.Store.UploadByID("a1"); u != nil {
		t.Fatal("the dropped device's session is still there")
	}
	if exists(r.tempFile("a1")) {
		t.Fatal("the dropped device's temp file is still there")
	}
	if u, _ := r.m.cfg.Store.UploadByID("b1"); u == nil {
		t.Fatal("another device's session was dropped")
	}
}

func TestDropDeviceRemovesAnInUseSessionWhenItsRequestEnds(t *testing.T) {
	r := newRig(t)
	r.open(devA, "a1", "a1.bin", payload(testSize), testChunk, false)
	_, release, err := r.m.begin(devA, "a1", writeLease)
	if err != nil {
		t.Fatal(err)
	}
	r.m.DropDevice(devA.DeviceID)
	if u, _ := r.m.cfg.Store.UploadByID("a1"); u == nil {
		t.Fatal("an in-use session was removed under its request")
	}
	release()
	if u, _ := r.m.cfg.Store.UploadByID("a1"); u != nil {
		t.Fatal("the dropped device's session survived the end of its last request")
	}
	if exists(r.tempFile("a1")) {
		t.Fatal("its temp file survived")
	}
}
