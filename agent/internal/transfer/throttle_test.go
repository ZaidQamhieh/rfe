package transfer

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"
)

func TestThrottledReader_Unlimited(t *testing.T) {
	src := bytes.NewReader([]byte("hello world"))
	tr := NewThrottledReader(src, 0)
	got, err := io.ReadAll(tr)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(got) != "hello world" {
		t.Fatalf("got %q", got)
	}
}

func TestThrottledReader_Throttled(t *testing.T) {
	// 100 bytes at 1000 bytes/sec should take ~100ms.
	data := strings.Repeat("x", 100)
	src := bytes.NewReader([]byte(data))
	tr := NewThrottledReader(src, 1000)
	start := time.Now()
	got, err := io.ReadAll(tr)
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(got) != data {
		t.Fatalf("data mismatch")
	}
	// Should take at least 50ms (some tolerance for scheduling).
	if elapsed < 50*time.Millisecond {
		t.Fatalf("expected throttled read to take >=50ms, took %v", elapsed)
	}
}

func TestThrottledReadSeeker_Seek(t *testing.T) {
	data := []byte("abcdefghij")
	src := bytes.NewReader(data)
	trs := NewThrottledReadSeeker(src, 0)

	// Seek to offset 5.
	pos, err := trs.Seek(5, io.SeekStart)
	if err != nil {
		t.Fatalf("seek: %v", err)
	}
	if pos != 5 {
		t.Fatalf("expected pos 5, got %d", pos)
	}

	got, err := io.ReadAll(trs)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(got) != "fghij" {
		t.Fatalf("got %q after seek", got)
	}
}

func TestThrottledReaderStopsSleepingWhenContextEnds(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	// 5 B/s: an empty bucket sleeps about a second per read.
	r := NewThrottledReader(strings.NewReader(strings.Repeat("x", 100)), 5).WithContext(ctx)
	buf := make([]byte, 10)
	_, _ = r.Read(buf)
	cancel()
	start := time.Now()
	_, err := r.Read(buf)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("want context.Canceled, got %v", err)
	}
	if time.Since(start) > 200*time.Millisecond {
		t.Fatalf("a cancelled read still slept %v", time.Since(start))
	}
}
