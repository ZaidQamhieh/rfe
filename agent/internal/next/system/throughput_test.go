package system

import (
	"sync"
	"testing"
)

func TestThroughputAccumulatesAndIgnoresNegative(t *testing.T) {
	tp := NewThroughput()
	if rx, tx := tp.Totals(); rx != 0 || tx != 0 {
		t.Fatalf("fresh totals = %d/%d", rx, tx)
	}
	tp.AddRx(10)
	tp.AddTx(4)
	tp.AddRx(-5) // a bad caller must not make a cumulative counter go down
	tp.AddTx(-1)
	if rx, tx := tp.Totals(); rx != 10 || tx != 4 {
		t.Errorf("totals = %d/%d, want 10/4", rx, tx)
	}
}

func TestThroughputConcurrent(t *testing.T) {
	tp := NewThroughput()
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				tp.AddRx(2)
				tp.AddTx(3)
			}
		}()
	}
	wg.Wait()
	if rx, tx := tp.Totals(); rx != 3200 || tx != 4800 {
		t.Errorf("totals = %d/%d, want 3200/4800", rx, tx)
	}
}

func TestThroughputWriterCountsBytesWritten(t *testing.T) {
	tp := NewThroughput()
	var sink countSink
	w := tp.CountingWriter(&sink, DirectionTx)
	n, err := w.Write([]byte("hello"))
	if err != nil || n != 5 {
		t.Fatalf("write = %d, %v", n, err)
	}
	// A short write counts only what was written.
	sink.limit = 2
	n, _ = w.Write([]byte("world"))
	if n != 2 {
		t.Fatalf("short write n = %d", n)
	}
	if rx, tx := tp.Totals(); rx != 0 || tx != 7 {
		t.Errorf("totals = %d/%d, want 0/7", rx, tx)
	}
	w2 := tp.CountingWriter(&countSink{}, DirectionRx)
	_, _ = w2.Write([]byte("abc"))
	if rx, _ := tp.Totals(); rx != 3 {
		t.Errorf("rx = %d, want 3", rx)
	}
}

// countSink accepts writes, optionally truncating each to limit bytes.
type countSink struct{ limit int }

func (c *countSink) Write(p []byte) (int, error) {
	if c.limit > 0 && len(p) > c.limit {
		return c.limit, nil
	}
	return len(p), nil
}
