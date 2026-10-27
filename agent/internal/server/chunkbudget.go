package server

import (
	"sync"
	"time"
)

// chunkBufferBytes caps how much chunk body data all uploads together may hold in memory at once. A
// chunk is read whole before it is hashed and written, so without a cap many parallel PUTs from
// upload-capable devices each cost up to maxChunkSize (audit F-04).
var chunkBudget = newByteBudget(256 << 20)

// chunkWait is how long a chunk waits for room before the agent answers 429 CHUNK_BUSY.
var chunkWait = 5 * time.Second

type byteBudget struct {
	mu   sync.Mutex
	free int64
	wake chan struct{}
}

func newByteBudget(n int64) *byteBudget {
	return &byteBudget{free: n, wake: make(chan struct{})}
}

// acquire reserves n bytes, waiting up to wait or until done is closed. It reports whether it got them.
func (b *byteBudget) acquire(n int64, wait time.Duration, done <-chan struct{}) bool {
	timer := time.NewTimer(wait)
	defer timer.Stop()
	for {
		b.mu.Lock()
		if b.free >= n {
			b.free -= n
			b.mu.Unlock()
			return true
		}
		wake := b.wake
		b.mu.Unlock()
		select {
		case <-wake:
		case <-timer.C:
			return false
		case <-done:
			return false
		}
	}
}

func (b *byteBudget) release(n int64) {
	b.mu.Lock()
	b.free += n
	close(b.wake)
	b.wake = make(chan struct{})
	b.mu.Unlock()
}
