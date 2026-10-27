package server

import (
	"testing"
	"time"
)

// F-04: chunk bodies are buffered under a shared byte budget; a chunk that cannot get room in time is refused.
func TestByteBudget_RefusesWhenFullAndRecoversOnRelease(t *testing.T) {
	b := newByteBudget(10)
	if !b.acquire(6, time.Millisecond, nil) {
		t.Fatal("first reservation should fit")
	}
	if b.acquire(6, 20*time.Millisecond, nil) {
		t.Fatal("second reservation must not fit while the first is held")
	}
	go func() {
		time.Sleep(10 * time.Millisecond)
		b.release(6)
	}()
	if !b.acquire(6, time.Second, nil) {
		t.Fatal("a waiting reservation should proceed once room is released")
	}
}

func TestByteBudget_StopsWaitingWhenCancelled(t *testing.T) {
	b := newByteBudget(1)
	b.acquire(1, time.Millisecond, nil)
	done := make(chan struct{})
	close(done)
	if b.acquire(1, time.Minute, done) {
		t.Fatal("cancelled wait must give up")
	}
}
