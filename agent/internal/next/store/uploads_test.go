package store

import (
	"reflect"
	"sync"
	"testing"
)

// uploadStore is a local copy of the persistence interface that package
// transfer needs (transfer.Store); the assertion below keeps *DB in step.
type uploadStore interface {
	OpenUploads() ([]Upload, error)
	UploadByID(id string) (*Upload, error)
	InsertUpload(u Upload) error
	ChunkNumbers(id string) ([]int, error)
	HasChunk(id string, n int) (bool, error)
	AddChunk(id string, n int) error
	DeleteChunks(id string, nums []int) error
	TouchUpload(id string, at int64) error
	FinishUpload(id, status string, at int64) error
	DeleteUpload(id string) error
	DeleteTerminalBefore(cutoff int64) (int, error)
}

var _ uploadStore = (*DB)(nil)

func newUpload(id string) Upload {
	return Upload{ID: id, DeviceID: "dev-a", TargetPath: "/t/" + id, TotalSize: 10, ChunkSize: 4, TotalChunks: 3,
		SHA256: "abc", Overwrite: true, Status: UploadOpen, TempName: id + ".tmp", UpdatedAt: 100}
}

func mustInsert(t *testing.T, db *DB, u Upload) {
	t.Helper()
	if err := db.InsertUpload(u); err != nil {
		t.Fatalf("InsertUpload %s: %v", u.ID, err)
	}
}

func TestInsertUploadRoundTripAndDuplicate(t *testing.T) {
	db, _ := openTemp(t)
	u := newUpload("a")
	mustInsert(t, db, u)
	got, err := db.UploadByID("a")
	if err != nil || got == nil || !reflect.DeepEqual(*got, u) {
		t.Fatalf("UploadByID = %+v, %v; want %+v", got, err, u)
	}
	if err := db.InsertUpload(u); err == nil {
		t.Fatal("duplicate InsertUpload: want an error")
	}
	if got, err := db.UploadByID("missing"); got != nil || err != nil {
		t.Fatalf("UploadByID(missing) = %v, %v; want nil, nil", got, err)
	}
}

func TestOpenUploadsOnlyOpenOrdered(t *testing.T) {
	db, _ := openTemp(t)
	for _, id := range []string{"c", "a", "b"} {
		mustInsert(t, db, newUpload(id))
	}
	if err := db.FinishUpload("b", UploadCompleted, 200); err != nil {
		t.Fatal(err)
	}
	if err := db.AddChunk("a", 1); err != nil {
		t.Fatal(err)
	}
	got, err := db.OpenUploads()
	if err != nil || len(got) != 2 || got[0].ID != "a" || got[1].ID != "c" {
		t.Fatalf("OpenUploads = %+v, %v; want a, c", got, err)
	}
	if got[0].Chunks != nil {
		t.Fatalf("OpenUploads filled Chunks: %v", got[0].Chunks)
	}
}

func TestChunksIdempotentAndAscending(t *testing.T) {
	db, _ := openTemp(t)
	mustInsert(t, db, newUpload("a"))
	for _, n := range []int{2, 0, 2, 1, 0} {
		if err := db.AddChunk("a", n); err != nil {
			t.Fatalf("AddChunk %d: %v", n, err)
		}
	}
	got, err := db.ChunkNumbers("a")
	if err != nil || !reflect.DeepEqual(got, []int{0, 1, 2}) {
		t.Fatalf("ChunkNumbers = %v, %v; want [0 1 2]", got, err)
	}
	if ok, err := db.HasChunk("a", 1); !ok || err != nil {
		t.Fatalf("HasChunk(1) = %v, %v", ok, err)
	}
	if ok, err := db.HasChunk("a", 7); ok || err != nil {
		t.Fatalf("HasChunk(7) = %v, %v", ok, err)
	}
	if err := db.DeleteChunks("a", []int{0, 2, 9}); err != nil {
		t.Fatal(err)
	}
	got, _ = db.ChunkNumbers("a")
	if !reflect.DeepEqual(got, []int{1}) {
		t.Fatalf("after DeleteChunks = %v; want [1]", got)
	}
	if err := db.AddChunk("nope", 0); err == nil {
		t.Fatal("AddChunk on an unknown upload: want an error")
	}
}

func TestDeleteUploadRemovesChunks(t *testing.T) {
	db, _ := openTemp(t)
	mustInsert(t, db, newUpload("a"))
	mustInsert(t, db, newUpload("b"))
	for _, id := range []string{"a", "b"} {
		if err := db.AddChunk(id, 0); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.DeleteUpload("a"); err != nil {
		t.Fatal(err)
	}
	if u, _ := db.UploadByID("a"); u != nil {
		t.Fatal("row a still present")
	}
	if got, _ := db.ChunkNumbers("a"); len(got) != 0 {
		t.Fatalf("chunks of a remain: %v", got)
	}
	if got, _ := db.ChunkNumbers("b"); !reflect.DeepEqual(got, []int{0}) {
		t.Fatalf("chunks of b = %v; want [0]", got)
	}
	if err := db.DeleteUpload("a"); err != nil {
		t.Fatalf("deleting a missing upload: %v", err)
	}
}

func TestTouchAndFinish(t *testing.T) {
	db, _ := openTemp(t)
	mustInsert(t, db, newUpload("a"))
	if err := db.TouchUpload("a", 150); err != nil {
		t.Fatal(err)
	}
	if u, _ := db.UploadByID("a"); u.UpdatedAt != 150 || u.TerminalAt != 0 {
		t.Fatalf("after touch: %+v", u)
	}
	if err := db.FinishUpload("a", UploadCompleted, 300); err != nil {
		t.Fatal(err)
	}
	u, _ := db.UploadByID("a")
	if u.Status != UploadCompleted || u.UpdatedAt != 300 || u.TerminalAt != 300 {
		t.Fatalf("after finish: %+v", u)
	}
	if err := db.TouchUpload("a", 400); err != nil {
		t.Fatal(err)
	}
	if u, _ := db.UploadByID("a"); u.UpdatedAt != 300 {
		t.Fatalf("touch changed a finished row: %+v", u)
	}
	if err := db.FinishUpload("a", UploadOpen, 500); err == nil {
		t.Fatal("FinishUpload with status open: want an error")
	}
}

func TestDeleteTerminalBefore(t *testing.T) {
	db, _ := openTemp(t)
	for _, id := range []string{"done-old", "fail-old", "done-new", "open-old"} {
		mustInsert(t, db, newUpload(id))
		if err := db.AddChunk(id, 0); err != nil {
			t.Fatal(err)
		}
	}
	for id, c := range map[string]struct {
		status string
		at     int64
	}{"done-old": {UploadCompleted, 10}, "fail-old": {UploadFailed, 20}, "done-new": {UploadCompleted, 30}} {
		if err := db.FinishUpload(id, c.status, c.at); err != nil {
			t.Fatal(err)
		}
	}
	n, err := db.DeleteTerminalBefore(30) // strictly before: done-new (30) stays
	if err != nil || n != 2 {
		t.Fatalf("DeleteTerminalBefore = %d, %v; want 2", n, err)
	}
	for id, want := range map[string]bool{"done-old": false, "fail-old": false, "done-new": true, "open-old": true} {
		if u, _ := db.UploadByID(id); (u != nil) != want {
			t.Fatalf("%s present = %v; want %v", id, u != nil, want)
		}
	}
	if got, _ := db.ChunkNumbers("done-old"); len(got) != 0 {
		t.Fatalf("chunks of a deleted row remain: %v", got)
	}
	if got, _ := db.ChunkNumbers("open-old"); len(got) != 1 {
		t.Fatalf("chunks of a kept row = %v", got)
	}
}

func TestConcurrentAddChunk(t *testing.T) {
	db, _ := openTemp(t)
	mustInsert(t, db, newUpload("a"))
	const workers, chunks = 8, 20
	var wg sync.WaitGroup
	errs := make(chan error, workers*chunks)
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for n := 0; n < chunks; n++ { // every worker records every chunk
				if err := db.AddChunk("a", n); err != nil {
					errs <- err
				}
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatalf("AddChunk: %v", err)
	}
	got, err := db.ChunkNumbers("a")
	if err != nil || len(got) != chunks {
		t.Fatalf("ChunkNumbers = %v, %v; want %d numbers", got, err, chunks)
	}
	for i, n := range got {
		if n != i {
			t.Fatalf("chunk %d = %d; not ascending and complete: %v", i, n, got)
		}
	}
}
