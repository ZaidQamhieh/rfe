package api

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"
	"time"
)

// fakeClock is a clock that only moves when a paced reader sleeps.
type fakeClock struct {
	t      time.Time
	slept  time.Duration
	sleeps int
}

func (c *fakeClock) now() time.Time { return c.t }

func (c *fakeClock) sleep(_ context.Context, d time.Duration) error {
	c.t = c.t.Add(d)
	c.slept += d
	c.sleeps++
	return nil
}

// pacedOver builds a paced reader over data driven by a fake clock.
func pacedOver(data []byte, rate int64, c *fakeClock) *pacedReader {
	return &pacedReader{r: bytes.NewReader(data), rate: rate, ctx: context.Background(), now: c.now, sleep: c.sleep}
}

func TestPacedReaderTakesExactlyBytesOverRate(t *testing.T) {
	const rate, size = 1000, 5000
	c := &fakeClock{t: time.Unix(1_700_000_000, 0)}
	got, err := io.ReadAll(pacedOver(bytes.Repeat([]byte("a"), size), rate, c))
	if err != nil || len(got) != size {
		t.Fatalf("read %d bytes, err %v", len(got), err)
	}
	if want := size * time.Second / rate; c.slept != want {
		t.Fatalf("slept %v for %d bytes at %d B/s, want %v", c.slept, size, rate, want)
	}
}

func TestPacedReaderHandsOutSmallSlices(t *testing.T) {
	const rate = 1000
	c := &fakeClock{t: time.Unix(1_700_000_000, 0)}
	p := pacedOver(bytes.Repeat([]byte("a"), 4096), rate, c)
	n, err := p.Read(make([]byte, 4096))
	if err != nil || int64(n) != rate/throttleSlicesPerSecond {
		t.Fatalf("first read = %d, %v, want %d", n, err, rate/throttleSlicesPerSecond)
	}
}

func TestPacedReaderStopsWhenTheRequestEnds(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	p := newPacedReader(bytes.NewReader(make([]byte, 1000)), 10, ctx)
	if _, err := io.ReadAll(p); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}

func TestPacedReaderZeroRateIsPassThrough(t *testing.T) {
	src := bytes.NewReader([]byte("abc"))
	if got := paceReader(src, 0, context.Background()); got != io.Reader(src) {
		t.Fatalf("a zero rate must return the reader itself, got %T", got)
	}
	if got := paceReadSeeker(src, -1, context.Background()); got != io.ReadSeeker(src) {
		t.Fatalf("a negative rate must return the reader itself, got %T", got)
	}
}

func TestPacedReadSeekerRestartsThePaceAfterASeek(t *testing.T) {
	const rate = 1000
	c := &fakeClock{t: time.Unix(1_700_000_000, 0)}
	src := bytes.NewReader(bytes.Repeat([]byte("a"), 2000))
	rs := &pacedReadSeeker{pacedReader: &pacedReader{r: src, rate: rate, ctx: context.Background(), now: c.now, sleep: c.sleep}, seeker: src}
	if _, err := io.CopyN(io.Discard, rs, 1000); err != nil {
		t.Fatal(err)
	}
	first := c.slept
	if _, err := rs.Seek(0, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	if _, err := io.CopyN(io.Discard, rs, 1000); err != nil {
		t.Fatal(err)
	}
	if c.slept-first != first {
		t.Fatalf("second pass slept %v, first %v: a seek must restart the pace", c.slept-first, first)
	}
}

func TestSharedPacerMakesParallelReadersShareOneRate(t *testing.T) {
	var sp sharedPacer
	now := time.Unix(1000, 0)
	// Two readers each read 100 bytes at 100 B/s at the same instant: the second waits for the first's share too.
	w1 := sp.book(100, 100, now)
	w2 := sp.book(100, 100, now)
	if w1 != time.Second || w2 != 2*time.Second {
		t.Fatalf("waits = %v, %v; want 1s, 2s", w1, w2)
	}
	// After the timeline has drained, a read waits only for its own bytes.
	if w := sp.book(50, 100, now.Add(10*time.Second)); w != 500*time.Millisecond {
		t.Fatalf("idle wait = %v", w)
	}
}
