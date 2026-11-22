package system

import (
	"io"
	"sync/atomic"
)

// Direction says which counter a CountingWriter feeds.
type Direction int

const (
	// DirectionRx counts bytes received by the agent (uploads).
	DirectionRx Direction = iota
	// DirectionTx counts bytes sent by the agent (downloads).
	DirectionTx
)

// Throughput holds process-lifetime cumulative received and sent byte counters.
// It keeps no history: clients diff readings over time to get a rate.
type Throughput struct {
	rx, tx atomic.Int64
}

// NewThroughput returns counters starting at zero.
func NewThroughput() *Throughput { return &Throughput{} }

// AddRx adds n received bytes. A non-positive n is ignored so the cumulative
// counter can never go down.
func (t *Throughput) AddRx(n int64) {
	if n > 0 {
		t.rx.Add(n)
	}
}

// AddTx adds n sent bytes; a non-positive n is ignored.
func (t *Throughput) AddTx(n int64) {
	if n > 0 {
		t.tx.Add(n)
	}
}

// Totals returns the cumulative received and sent byte counts.
func (t *Throughput) Totals() (rx, tx int64) { return t.rx.Load(), t.tx.Load() }

// countingWriter forwards writes and tallies the bytes the target accepted.
type countingWriter struct {
	w   io.Writer
	add func(int64)
}

// Write passes p to the target and counts only the bytes it reports written.
func (c countingWriter) Write(p []byte) (int, error) {
	n, err := c.w.Write(p)
	c.add(int64(n))
	return n, err
}

// CountingWriter wraps w so every byte written through it is added to the
// counter for dir. It is the one place a download or upload body needs hooking.
func (t *Throughput) CountingWriter(w io.Writer, dir Direction) io.Writer {
	add := t.AddTx
	if dir == DirectionRx {
		add = t.AddRx
	}
	return countingWriter{w: w, add: add}
}
