package transfer

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/ZaidQamhieh/rfe/agent/internal/next/authz"
	"github.com/ZaidQamhieh/rfe/agent/internal/next/files"
	"github.com/ZaidQamhieh/rfe/agent/internal/next/rootfs"
	"github.com/ZaidQamhieh/rfe/agent/internal/next/store"
)

// recorder keeps an ordered list of event names, safe for concurrent use.
type recorder struct {
	mu     sync.Mutex
	events []string
}

func (r *recorder) add(e string) {
	r.mu.Lock()
	r.events = append(r.events, e)
	r.mu.Unlock()
}

func (r *recorder) take() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := r.events
	r.events = nil
	return out
}

// memStore is an in-memory Store. It outlives a Manager, so a test can build a
// second Manager on it to stand for a restart after a crash.
type memStore struct {
	mu     sync.Mutex
	rows   map[string]*store.Upload
	chunks map[string]map[int]bool
	rec    *recorder

	failFinishCompleted bool // the next FinishUpload(completed) fails once
}

func newMemStore(rec *recorder) *memStore {
	return &memStore{rows: map[string]*store.Upload{}, chunks: map[string]map[int]bool{}, rec: rec}
}

var errInjected = errors.New("injected store failure")

func (s *memStore) OpenUploads() ([]store.Upload, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []store.Upload
	for _, u := range s.rows {
		if u.Status == store.UploadOpen {
			c := *u
			c.Chunks = nil
			out = append(out, c)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func (s *memStore) UploadByID(id string) (*store.Upload, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	u, ok := s.rows[id]
	if !ok {
		return nil, nil
	}
	c := *u
	c.Chunks = nil
	return &c, nil
}

func (s *memStore) InsertUpload(u store.Upload) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.rows[u.ID]; ok {
		return errors.New("duplicate id")
	}
	u.Chunks = nil
	s.rows[u.ID] = &u
	s.chunks[u.ID] = map[int]bool{}
	return nil
}

func (s *memStore) ChunkNumbers(id string) ([]int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []int
	for n := range s.chunks[id] {
		out = append(out, n)
	}
	sort.Ints(out)
	return out, nil
}

func (s *memStore) HasChunk(id string, n int) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.chunks[id][n], nil
}

func (s *memStore) AddChunk(id string, n int) error {
	s.rec.add("row")
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.chunks[id] == nil {
		return errors.New("no such session")
	}
	s.chunks[id][n] = true
	return nil
}

func (s *memStore) DeleteChunks(id string, nums []int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, n := range nums {
		delete(s.chunks[id], n)
	}
	return nil
}

func (s *memStore) TouchUpload(id string, at int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if u, ok := s.rows[id]; ok {
		u.UpdatedAt = at
	}
	return nil
}

func (s *memStore) FinishUpload(id, status string, at int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if status == store.UploadCompleted && s.failFinishCompleted {
		s.failFinishCompleted = false
		return errInjected
	}
	if u, ok := s.rows[id]; ok {
		u.Status, u.UpdatedAt, u.TerminalAt = status, at, at
	}
	return nil
}

func (s *memStore) DeleteUpload(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.rows, id)
	delete(s.chunks, id)
	return nil
}

func (s *memStore) DeleteTerminalBefore(cutoff int64) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for id, u := range s.rows {
		if u.Status != store.UploadOpen && u.TerminalAt < cutoff {
			delete(s.rows, id)
			delete(s.chunks, id)
			n++
		}
	}
	return n, nil
}

// fakeClock is a settable clock.
type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	c.t = c.t.Add(d)
	c.mu.Unlock()
}

// The callers used by the tests.
var (
	devA  = authz.Principal{DeviceID: "dev-a"}
	devB  = authz.Principal{DeviceID: "dev-b"}
	admin = authz.Principal{DeviceID: "dev-admin", Admin: true}
)

// rig is one Manager over a memStore, a temp directory and a file service over
// a separate allowed folder (root).
type rig struct {
	t     *testing.T
	root  string // allowed folder; uploads are published here
	dir   string // temp directory of the Manager
	st    *memStore
	rec   *recorder
	svc   *files.Service
	clock *fakeClock
	cfg   Config
	m     *Manager
}

// newRig builds a rig; tune may change the Config before the Manager starts.
func newRig(t *testing.T, tune ...func(*Config)) *rig {
	t.Helper()
	rec := &recorder{}
	r := &rig{t: t, root: t.TempDir(), dir: filepath.Join(t.TempDir(), "transfers"), rec: rec,
		st: newMemStore(rec), clock: &fakeClock{t: time.Now().Truncate(time.Second)}}
	set, err := rootfs.New([]string{r.root}, rootfs.Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = set.Close() })
	r.svc = files.New(set)
	r.cfg = Config{Dir: r.dir, Store: r.st, Now: r.clock.Now, Logf: t.Logf}
	for _, f := range tune {
		f(&r.cfg)
	}
	r.restart()
	return r
}

// restart builds a new Manager on the same store and directory, as after a crash
// (the old Manager is simply dropped).
func (r *rig) restart() {
	r.t.Helper()
	m, err := New(r.cfg)
	if err != nil {
		r.t.Fatal(err)
	}
	r.m = m
}

// target returns a path inside the allowed folder.
func (r *rig) target(name string) string { return filepath.Join(r.root, name) }

// open opens a session for data in chunks of chunkSize.
func (r *rig) open(p authz.Principal, id, name string, data []byte, chunkSize int, overwrite bool) *store.Upload {
	r.t.Helper()
	u, err := r.m.Open(p, r.svc, r.request(id, name, data, chunkSize, overwrite))
	if err != nil {
		r.t.Fatalf("Open: %v", err)
	}
	return u
}

func (r *rig) request(id, name string, data []byte, chunkSize int, overwrite bool) OpenRequest {
	return OpenRequest{ID: id, Path: r.target(name), Size: int64(len(data)), ChunkSize: chunkSize,
		SHA256: sum(data), Overwrite: overwrite}
}

// send writes chunk n of data (split by chunkSize).
func (r *rig) send(p authz.Principal, id string, data []byte, chunkSize, n int) error {
	lo, hi := chunkBounds(len(data), chunkSize, n)
	return r.m.WriteChunk(p, id, n, data[lo:hi], sum(data[lo:hi]))
}

// sendAll writes every chunk, failing the test on an error.
func (r *rig) sendAll(p authz.Principal, id string, data []byte, chunkSize int) {
	r.t.Helper()
	for n := 0; n < totalChunksFor(int64(len(data)), chunkSize); n++ {
		if err := r.send(p, id, data, chunkSize, n); err != nil {
			r.t.Fatalf("chunk %d: %v", n, err)
		}
	}
}

func (r *rig) tempFile(id string) string { return filepath.Join(r.dir, id+TempSuffix) }

func chunkBounds(size, chunkSize, n int) (int, int) {
	lo := n * chunkSize
	hi := lo + chunkSize
	if hi > size {
		hi = size
	}
	return lo, hi
}

// sum returns the lowercase hex SHA-256 of b.
func sum(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

// payload returns n deterministic, non-uniform bytes.
func payload(n int) []byte {
	b := make([]byte, n)
	for i := range b {
		b[i] = byte(i*31 + i/251 + 1)
	}
	return b
}

func exists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}
