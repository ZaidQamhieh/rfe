package thumbs

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestAcquireDecodeSlotBoundsConcurrentWork(t *testing.T) {
	const capacity = 2
	sem := make(chan struct{}, capacity)
	var active atomic.Int32
	var maximum atomic.Int32
	var wg sync.WaitGroup

	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			release, err := acquireDecodeSlot(context.Background(), sem)
			if err != nil {
				t.Errorf("acquireDecodeSlot: %v", err)
				return
			}
			defer release()
			current := active.Add(1)
			for observed := maximum.Load(); current > observed && !maximum.CompareAndSwap(observed, current); observed = maximum.Load() {
			}
			time.Sleep(10 * time.Millisecond)
			active.Add(-1)
		}()
	}

	wg.Wait()
	if got := maximum.Load(); got < 2 || got > capacity {
		t.Fatalf("maximum simultaneous work = %d, want 2..%d", got, capacity)
	}
}

func TestAcquireDecodeSlotCanCancelWhileQueued(t *testing.T) {
	sem := make(chan struct{}, 1)
	sem <- struct{}{}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		release, err := acquireDecodeSlot(ctx, sem)
		if err == nil {
			release()
		}
		done <- err
	}()
	cancel()

	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("queued acquisition error = %v, want context.Canceled", err)
		}
	case <-time.After(time.Second):
		t.Fatal("queued acquisition did not return after cancellation")
	}
}
