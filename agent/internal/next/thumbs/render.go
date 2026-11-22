package thumbs

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	"sync"

	"github.com/disintegration/imaging"
)

// errOverBudget means one decode alone would need more memory than the budget.
var errOverBudget = errors.New("thumbnail decode exceeds the memory budget")

// GoRenderer is the built-in, in-process Renderer (the oracle for any
// sidecar). It reads the image size from the header first and refuses images
// over its pixel cap before decoding them, and it reserves an estimate of the
// decode memory from a shared budget so many large images cannot run together.
type GoRenderer struct {
	maxPixels int64
	budget    *budget
}

// NewGoRenderer builds a renderer that accepts images up to maxPixels
// (width x height) and keeps the estimated memory of simultaneous decodes
// within memoryBudget bytes.
func NewGoRenderer(maxPixels, memoryBudget int64) *GoRenderer {
	return &GoRenderer{maxPixels: maxPixels, budget: &budget{limit: memoryBudget}}
}

// Render decodes source (JPEG, PNG, GIF, TIFF or BMP), applies its EXIF
// orientation, fits it inside maxSize x maxSize without upscaling and encodes
// it as a JPEG. A source it cannot or may not decode is ErrNotAvailable.
func (g *GoRenderer) Render(ctx context.Context, source []byte, maxSize int) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(source))
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrNotAvailable, err)
	}
	if cfg.Width <= 0 || cfg.Height <= 0 {
		return nil, fmt.Errorf("%w: invalid image dimensions %dx%d", ErrNotAvailable, cfg.Width, cfg.Height)
	}
	width, height := int64(cfg.Width), int64(cfg.Height)
	if width > g.maxPixels/height {
		return nil, fmt.Errorf("%w: %dx%d exceeds the pixel limit", ErrNotAvailable, cfg.Width, cfg.Height)
	}
	weight := width * height * decodeBytesPerPixel
	if err := g.budget.acquire(ctx, weight); err != nil {
		if errors.Is(err, errOverBudget) {
			return nil, fmt.Errorf("%w: %v", ErrNotAvailable, err)
		}
		return nil, err
	}
	defer g.budget.release(weight)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	img, err := imaging.Decode(bytes.NewReader(source), imaging.AutoOrientation(true))
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrNotAvailable, err)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	thumb := imaging.Fit(img, maxSize, maxSize, imaging.Lanczos)
	var out bytes.Buffer
	if err := jpeg.Encode(&out, thumb, &jpeg.Options{Quality: JPEGQuality}); err != nil {
		return nil, fmt.Errorf("thumbs: encode jpeg: %w", err)
	}
	return out.Bytes(), nil
}

// budget is a weighted, first-come-first-served memory reservation shared by
// all renders of one GoRenderer. A cancelled waiter leaves the queue without
// consuming anything.
type budget struct {
	mu    sync.Mutex
	limit int64
	used  int64
	queue []*waiter
}

// waiter is one reservation request waiting in the queue.
type waiter struct {
	weight  int64
	ready   chan struct{}
	granted bool
}

// acquire blocks until weight bytes are reserved or ctx ends. A weight above
// the whole budget fails at once with errOverBudget.
func (b *budget) acquire(ctx context.Context, weight int64) error {
	if weight <= 0 || weight > b.limit {
		return fmt.Errorf("%w: needs %d of %d", errOverBudget, weight, b.limit)
	}
	w := &waiter{weight: weight, ready: make(chan struct{})}
	b.mu.Lock()
	b.queue = append(b.queue, w)
	b.dispatchLocked()
	b.mu.Unlock()

	select {
	case <-w.ready:
		return nil
	case <-ctx.Done():
		b.mu.Lock()
		if w.granted {
			b.used -= w.weight
		} else {
			for i, q := range b.queue {
				if q == w {
					b.queue = append(b.queue[:i], b.queue[i+1:]...)
					break
				}
			}
		}
		b.dispatchLocked()
		b.mu.Unlock()
		return ctx.Err()
	}
}

// release returns a reservation and admits the next waiters that now fit.
func (b *budget) release(weight int64) {
	b.mu.Lock()
	b.used -= weight
	b.dispatchLocked()
	b.mu.Unlock()
}

// dispatchLocked grants the head of the queue while it fits (FIFO: a large
// request is never starved by smaller ones behind it).
func (b *budget) dispatchLocked() {
	for len(b.queue) > 0 {
		w := b.queue[0]
		if b.used+w.weight > b.limit {
			return
		}
		b.queue = b.queue[1:]
		b.used += w.weight
		w.granted = true
		close(w.ready)
	}
}
