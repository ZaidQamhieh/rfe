// Package transfer — throttle.go provides a rate-limited io.ReadSeeker wrapper.
package transfer

import (
	"context"
	"io"
	"time"
)

// ThrottledReader wraps an io.Reader and limits throughput to at most
// bytesPerSec bytes per second. A zero or negative bytesPerSec disables
// throttling (pass-through).
type ThrottledReader struct {
	r           io.Reader
	bytesPerSec int64
	bucket      int64     // tokens available
	lastFill    time.Time // last time the bucket was refilled
	ctx         context.Context
}

// WithContext makes a sleeping read return ctx's error as soon as ctx ends, so a cancelled transfer does not linger.
func (t *ThrottledReader) WithContext(ctx context.Context) *ThrottledReader {
	t.ctx = ctx
	return t
}

// NewThrottledReader creates a rate-limited reader. bytesPerSec <= 0 means
// unlimited (the reader passes through without sleeping).
func NewThrottledReader(r io.Reader, bytesPerSec int64) *ThrottledReader {
	return &ThrottledReader{
		r:           r,
		bytesPerSec: bytesPerSec,
		lastFill:    time.Now(),
	}
}

func (t *ThrottledReader) Read(p []byte) (int, error) {
	if t.bytesPerSec <= 0 {
		return t.r.Read(p)
	}

	// Refill tokens based on elapsed time.
	now := time.Now()
	elapsed := now.Sub(t.lastFill)
	t.lastFill = now
	t.bucket += int64(elapsed.Seconds() * float64(t.bytesPerSec))
	if t.bucket > t.bytesPerSec {
		t.bucket = t.bytesPerSec
	}

	// If we have no tokens, sleep until we earn enough for a small read.
	if t.bucket <= 0 {
		// Sleep for the time it takes to earn min(len(p), bytesPerSec/10) tokens.
		want := int64(len(p))
		if want > t.bytesPerSec/10 {
			want = t.bytesPerSec / 10
		}
		if want < 1 {
			want = 1
		}
		sleepDur := time.Duration(float64(want) / float64(t.bytesPerSec) * float64(time.Second))
		if sleepDur < time.Millisecond {
			sleepDur = time.Millisecond
		}
		if t.ctx != nil {
			timer := time.NewTimer(sleepDur)
			select {
			case <-t.ctx.Done():
				timer.Stop()
				return 0, t.ctx.Err()
			case <-timer.C:
			}
		} else {
			time.Sleep(sleepDur)
		}
		t.bucket += want
		t.lastFill = time.Now()
	}

	// Limit the read to available tokens.
	limit := int(t.bucket)
	if limit > len(p) {
		limit = len(p)
	}
	if limit < 1 {
		limit = 1
	}

	n, err := t.r.Read(p[:limit])
	t.bucket -= int64(n)
	return n, err
}

// ThrottledReadSeeker wraps an io.ReadSeeker with rate limiting.
// Seek is passed through; reads are throttled.
type ThrottledReadSeeker struct {
	ThrottledReader
	seeker io.Seeker
}

// NewThrottledReadSeeker creates a rate-limited ReadSeeker.
func NewThrottledReadSeeker(rs io.ReadSeeker, bytesPerSec int64) *ThrottledReadSeeker {
	return &ThrottledReadSeeker{
		ThrottledReader: *NewThrottledReader(rs, bytesPerSec),
		seeker:          rs,
	}
}

// Seek delegates to the underlying seeker.
func (t *ThrottledReadSeeker) Seek(offset int64, whence int) (int64, error) {
	return t.seeker.Seek(offset, whence)
}
