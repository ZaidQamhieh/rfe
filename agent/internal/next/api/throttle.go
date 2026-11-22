package api

import (
	"context"
	"io"
	"sync"
	"time"
)

// throttleSlicesPerSecond splits one second of allowed bytes into this many
// reads, so a limited transfer flows in small steady pieces, not one burst a second.
const throttleSlicesPerSecond = 10

// pacedReader hands out the bytes of r no faster than rate bytes per second,
// counted from its first read: after moving N bytes it waits until N/rate seconds
// have passed. now and sleep are the clock; sleep returns early with the context's
// error when ctx ends.
type pacedReader struct {
	r       io.Reader
	rate    int64
	ctx     context.Context
	now     func() time.Time
	sleep   func(ctx context.Context, d time.Duration) error
	started bool
	start   time.Time
	moved   int64
	shared  *sharedPacer // when set, the limit is shared with every other reader on it
}

// sharedPacer makes the bytes of all concurrent transfers in one direction fit
// one rate: each read books its share of the timeline, so parallel chunk
// uploads together stay under the limit instead of each getting the full rate.
type sharedPacer struct {
	mu   sync.Mutex
	next time.Time // when the timeline is free again
}

// book reserves n bytes at rate and returns how long the caller must wait from now.
func (sp *sharedPacer) book(n int, rate int64, now time.Time) time.Duration {
	sp.mu.Lock()
	defer sp.mu.Unlock()
	if sp.next.Before(now) {
		sp.next = now
	}
	sp.next = sp.next.Add(time.Duration(float64(n) / float64(rate) * float64(time.Second)))
	return sp.next.Sub(now)
}

// paceReaderShared is paceReader whose limit is shared through sp.
func paceReaderShared(r io.Reader, rate int64, ctx context.Context, sp *sharedPacer) io.Reader {
	if rate <= 0 {
		return r
	}
	p := newPacedReader(r, rate, ctx)
	p.shared = sp
	return p
}

// paceReadSeekerShared is paceReadSeeker whose limit is shared through sp.
func paceReadSeekerShared(rs io.ReadSeeker, rate int64, ctx context.Context, sp *sharedPacer) io.ReadSeeker {
	if rate <= 0 {
		return rs
	}
	p := newPacedReader(rs, rate, ctx)
	p.shared = sp
	return &pacedReadSeeker{pacedReader: p, seeker: rs}
}

// newPacedReader returns a paced reader on the real clock.
func newPacedReader(r io.Reader, rate int64, ctx context.Context) *pacedReader {
	return &pacedReader{r: r, rate: rate, ctx: ctx, now: time.Now, sleep: sleepContext}
}

// paceReader limits r to rate bytes per second; a rate of 0 or less is no limit
// and returns r itself.
func paceReader(r io.Reader, rate int64, ctx context.Context) io.Reader {
	if rate <= 0 {
		return r
	}
	return newPacedReader(r, rate, ctx)
}

// paceReadSeeker is paceReader for a body that must also seek (a Range answer).
func paceReadSeeker(rs io.ReadSeeker, rate int64, ctx context.Context) io.ReadSeeker {
	if rate <= 0 {
		return rs
	}
	return &pacedReadSeeker{pacedReader: newPacedReader(rs, rate, ctx), seeker: rs}
}

// sleepContext waits d, or until ctx ends, whichever comes first.
func sleepContext(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// Read reads at most a slice of the rate from the source, then waits until the
// bytes moved so far fit the rate.
func (p *pacedReader) Read(b []byte) (int, error) {
	if slice := max(p.rate/throttleSlicesPerSecond, 1); int64(len(b)) > slice {
		b = b[:slice]
	}
	if !p.started {
		p.started, p.start = true, p.now()
	}
	n, err := p.r.Read(b)
	if n <= 0 {
		return n, err
	}
	p.moved += int64(n)
	if p.shared != nil {
		if wait := p.shared.book(n, p.rate, p.now()); wait > 0 {
			if serr := p.sleep(p.ctx, wait); serr != nil {
				return n, serr
			}
		}
		return n, err
	}
	due := p.start.Add(time.Duration(float64(p.moved) / float64(p.rate) * float64(time.Second)))
	if wait := due.Sub(p.now()); wait > 0 {
		if serr := p.sleep(p.ctx, wait); serr != nil {
			return n, serr
		}
	}
	return n, err
}

// pacedReadSeeker is a pacedReader over a source that can seek. A seek starts a
// new count, so the first read after it is paced from zero.
type pacedReadSeeker struct {
	*pacedReader
	seeker io.Seeker
}

// Seek moves the source and restarts the pace.
func (p *pacedReadSeeker) Seek(offset int64, whence int) (int64, error) {
	p.started, p.moved = false, 0
	return p.seeker.Seek(offset, whence)
}

// downloadRate is the live download limit in bytes per second (0 = unlimited),
// read from the settings on every call so a change applies to the next request.
func (s *Server) downloadRate() int64 { return s.cfg.Policy.Get().MaxDownloadBytesPerSec }

// uploadRate is the live upload limit in bytes per second (0 = unlimited).
func (s *Server) uploadRate() int64 { return s.cfg.Policy.Get().MaxUploadBytesPerSec }
