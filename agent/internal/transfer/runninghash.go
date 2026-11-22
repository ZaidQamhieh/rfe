package transfer

import (
	"crypto/sha256"
	"encoding/hex"
	"hash"
	"io"
	"os"
	"sync"

	"github.com/ZaidQamhieh/rfe/agent/internal/store"
)

// onWholeFileRehash is a test seam: complete calls it each time it has to read the whole temp file again to
// hash it, which it only does when no running hash covers the session. Production code never sets it.
var onWholeFileRehash func()

// wholeHash is the SHA-256 of one session's file, advanced over the chunks in index order as they are
// committed, so completing the upload need not read the whole file again. A chunk that arrives ahead of its
// turn is remembered and hashed (read back from the temp file) once the chunks before it are in. Any read
// failure marks it unusable, and complete then falls back to hashing the file itself.
type wholeHash struct {
	mu    sync.Mutex
	h     hash.Hash
	next  int          // index of the next chunk to feed into h
	ready map[int]bool // committed chunks past next, not yet hashed
	bad   bool
}

// wholeHashSet holds the running hashes of the sessions this process opened. A session resumed after a restart
// has none, because chunks written before the restart were never seen. The zero value is ready to use.
type wholeHashSet struct {
	mu sync.Mutex
	m  map[string]*wholeHash
}

// start begins a running hash for session id (an existing one is kept).
func (s *wholeHashSet) start(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.m == nil {
		s.m = make(map[string]*wholeHash)
	}
	if s.m[id] == nil {
		s.m[id] = &wholeHash{h: sha256.New(), ready: make(map[int]bool)}
	}
}

func (s *wholeHashSet) get(id string) *wholeHash {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.m[id]
}

// drop forgets session id's running hash.
func (s *wholeHashSet) drop(id string) {
	s.mu.Lock()
	delete(s.m, id)
	s.mu.Unlock()
}

// retainOnly forgets the running hash of every session not in keep, so hashes of sessions removed by another
// path never accumulate.
func (s *wholeHashSet) retainOnly(keep map[string]bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for id := range s.m {
		if !keep[id] {
			delete(s.m, id)
		}
	}
}

// noteChunk records that chunk n (whose bytes are data) is committed in the temp file at tempPath and advances
// the running hash over every chunk that is now in order. Chunks other than n are read back from the file.
func (s *wholeHashSet) noteChunk(id string, t *store.Transfer, tempPath string, n int, data []byte) {
	w := s.get(id)
	if w == nil {
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.bad || n < w.next || w.ready[n] {
		return
	}
	w.ready[n] = true
	var (
		f   *os.File
		buf []byte
	)
	defer func() {
		if f != nil {
			_ = f.Close()
		}
	}()
	for w.ready[w.next] {
		k := w.next
		chunk := data
		if k != n {
			if f == nil {
				var err error
				if f, err = os.Open(tempPath); err != nil {
					w.bad = true
					return
				}
				buf = make([]byte, t.ChunkSize)
			}
			want := t.ExpectedChunkLen(k)
			if want < 0 {
				w.bad = true
				return
			}
			if _, err := f.ReadAt(buf[:want], int64(k)*int64(t.ChunkSize)); err != nil && !(err == io.EOF && want == 0) {
				w.bad = true
				return
			}
			chunk = buf[:want]
		}
		w.h.Write(chunk)
		delete(w.ready, k)
		w.next++
	}
}

// sum returns the hex SHA-256 of the whole file when the running hash has covered all total chunks.
func (s *wholeHashSet) sum(id string, total int) (string, bool) {
	w := s.get(id)
	if w == nil {
		return "", false
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.bad || w.next != total {
		return "", false
	}
	return hex.EncodeToString(w.h.Sum(nil)), true
}
