// Package thumbs renders and caches JPEG thumbnails for the rewritten agent.
//
// A Service opens the source file through a caller-supplied rootfs.Set (so a
// symlink or ".." can never reach outside the allowed folders, and the agent's
// protected data directory is never read), keeps rendered thumbnails in a
// bounded on-disk cache keyed by the file's identity (path, size, modification
// time and, where the system has one, device and inode), and renders cache
// misses with a Renderer. The default Renderer is a pure-Go decoder; a
// sandboxed one (the Rust rfe-thumbd sidecar, see SidecarRenderer) can be put
// in front of it with Config.Renderer.
//
// The package knows nothing about HTTP. The API layer maps its errors:
// rootfs.ErrOutsideRoots and rootfs.ErrProtected to 403, fs.ErrNotExist and
// ErrNotAvailable to 404 NOT_AVAILABLE, ErrBusy to 429 THUMB_BUSY, ErrBadSize
// to 400, a cancelled context to nothing, and anything else to 500. Result
// carries the ETag and Last-Modified values for the response headers.
package thumbs

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"sync"
	"time"

	"github.com/ZaidQamhieh/rfe/agent/internal/next/rootfs"
)

// Errors a Service (and a Renderer) returns. Test with errors.Is.
var (
	// ErrNotAvailable: no thumbnail exists for this file (not an image, a
	// corrupt or oversized one, a folder, or a file a renderer crashed on).
	ErrNotAvailable = errors.New("thumbnail not available for this file")

	// ErrBadSize: the requested size is negative.
	ErrBadSize = errors.New("thumbnail size must not be negative")

	// ErrBusy: the service, or the sandboxed renderer, is at capacity. The
	// caller should ask the client to retry shortly. It never falls back to
	// the in-process decoder.
	ErrBusy = errors.New("thumbnail work is at capacity")

	// ErrUnsupported: a Renderer cannot decode this input, but another may;
	// the Service then tries the built-in decoder.
	ErrUnsupported = errors.New("renderer cannot decode this source")

	// ErrRenderFailed: a Renderer's decoder failed or panicked on this input,
	// or it answered something unusable. Final: the input is not decoded
	// in-process, because that would run the suspect bytes unsandboxed.
	ErrRenderFailed = errors.New("thumbnail renderer failed to render this file")

	// ErrInterrupted: a Renderer died while it held this input. The first time
	// the error is transient; a second time on the same file version marks the
	// file as one the renderer cannot survive (ErrNotAvailable from then on).
	ErrInterrupted = errors.New("thumbnail renderer was interrupted")

	// ErrDown: a Renderer is not running. The Service uses the built-in
	// decoder, except for a file the renderer already crashed on.
	ErrDown = errors.New("thumbnail renderer is not running")

	// ErrRendererUnavailable: a Renderer is untrustworthy right now (repeated
	// start failures). Final: it is not treated as merely absent.
	ErrRendererUnavailable = errors.New("thumbnail renderer is unavailable")
)

// Defaults for the zero values of Config, and the fixed output contract.
const (
	// ContentType is the media type of every thumbnail.
	ContentType = "image/jpeg"
	// JPEGQuality is the quality the built-in renderer encodes with.
	JPEGQuality = 80
	// DefaultSize is the bucket used when the caller passes no size.
	DefaultSize = 256

	// DefaultMaxSourceBytes is the largest source file that is decoded.
	DefaultMaxSourceBytes int64 = 64 << 20
	// DefaultMaxPixels is the largest decoded image (width x height) accepted:
	// the decompression-bomb guard, checked from the header before decoding.
	DefaultMaxPixels int64 = 40_000_000
	// DefaultDecodeMemoryBudget bounds the estimated memory of all decodes in
	// progress together.
	DefaultDecodeMemoryBudget int64 = 384 << 20
	// DefaultMaxDecodes bounds the renders running at once.
	DefaultMaxDecodes = 4
	// DefaultMaxRequests bounds the requests in flight (cache reads and callers
	// waiting for a shared render included); more fail fast with ErrBusy.
	DefaultMaxRequests = 8
	// DefaultMaxCacheBytes is the cache size cap.
	DefaultMaxCacheBytes int64 = 512 << 20
	// DefaultMaxCacheAge is how long an entry may go unused before eviction.
	DefaultMaxCacheAge = 30 * 24 * time.Hour

	// decodeBytesPerPixel estimates the memory one decode needs per source
	// pixel (the decoded image plus the resize working data).
	decodeBytesPerPixel = 8
)

// sizeBuckets are the thumbnail sizes (longest side, pixels) the service
// renders. A request is rounded up to the next bucket so the cache holds a few
// variants per file, not one per distinct number a client sends.
var sizeBuckets = [...]int{64, 128, 256, 512, 1024}

// Event names passed to Config.Notify; they match protocol/sidecar.md.
const (
	eventUnsupported = "thumbd.unsupported-format"
	eventFallback    = "thumbd.fallback"
	eventInterrupted = "thumbd.interrupted"
	eventPoisoned    = "thumbd.poisoned-file"
	eventFailed      = "thumbd.render-failed"
	eventBreaker     = "thumbd.breaker-open-refused"
	eventRestarting  = "thumbd.restarting-refused"
	eventBusy        = "thumbd.busy"
	eventUnknown     = "thumbd.unexpected-error"
)

// Buckets returns the sizes a request can be rounded to, smallest first.
func Buckets() []int {
	out := make([]int, len(sizeBuckets))
	copy(out, sizeBuckets[:])
	return out
}

// MaxSize is the largest bucket; a bigger request is clamped to it.
func MaxSize() int { return sizeBuckets[len(sizeBuckets)-1] }

// Bucket validates a requested size and returns the bucket to render: zero
// means "not given" (DefaultSize), a negative size is ErrBadSize, a size above
// the largest bucket is clamped to it, anything else rounds up to a bucket.
func Bucket(size int) (int, error) {
	if size < 0 {
		return 0, ErrBadSize
	}
	if size == 0 {
		return DefaultSize, nil
	}
	for _, b := range sizeBuckets {
		if size <= b {
			return b, nil
		}
	}
	return MaxSize(), nil
}

// Renderer turns the bytes of a source image into a JPEG whose longest side is
// at most maxSize (never upscaled, EXIF orientation applied). Errors must wrap
// one of the package's sentinels (ErrUnsupported, ErrNotAvailable,
// ErrRenderFailed, ErrBusy, ErrInterrupted, ErrDown, ErrRendererUnavailable)
// so the Service knows whether another renderer may try; any other error is
// final. It must stop when ctx is cancelled.
type Renderer interface {
	Render(ctx context.Context, source []byte, maxSize int) ([]byte, error)
}

// Config describes a Service. Zero limits take the Default constants.
type Config struct {
	// CacheDir holds the cached thumbnails; created when missing. Required.
	CacheDir string
	// Renderer, when set, is tried before the built-in decoder.
	Renderer Renderer
	// Notify, when set, receives named events (fallbacks, refusals) so the
	// daemon can count them (sidecar.Note).
	Notify func(event string)
	// Logf receives diagnostics (a decoder panic); log.Printf when nil.
	Logf func(format string, args ...any)
	// Now is the clock; time.Now when nil. Tests inject their own.
	Now func() time.Time

	MaxSourceBytes     int64
	MaxPixels          int64
	DecodeMemoryBudget int64
	MaxDecodes         int
	MaxRequests        int
	MaxCacheBytes      int64
	MaxCacheAge        time.Duration
}

// Result is a thumbnail and the values for its response headers.
type Result struct {
	Data        []byte    // JPEG bytes; do not modify
	ContentType string    // always ContentType
	ETag        string    // weak validator, quoted, names source version + bucket
	ModTime     time.Time // source modification time, for Last-Modified
	Size        int       // the bucket that was rendered
	Cached      bool      // served from the cache without rendering
}

// Service renders and caches thumbnails. Safe for concurrent use.
type Service struct {
	cfg     Config
	cache   *cache
	local   Renderer
	crashes crashLog

	requests chan struct{} // request slots
	decodes  chan struct{} // render slots

	mu     sync.Mutex
	flight map[string]*flight
}

// flight is one shared render: callers asking for the same source version and
// bucket wait on it instead of rendering again.
type flight struct {
	done    chan struct{}
	data    []byte
	err     error
	ctx     context.Context
	cancel  context.CancelFunc
	waiters int
}

// New validates cfg, fills in defaults and opens the cache directory.
func New(cfg Config) (*Service, error) {
	if cfg.CacheDir == "" {
		return nil, errors.New("thumbs: cache dir is required")
	}
	var err error
	if cfg.MaxSourceBytes, err = limit("MaxSourceBytes", cfg.MaxSourceBytes, DefaultMaxSourceBytes); err != nil {
		return nil, err
	}
	if cfg.MaxPixels, err = limit("MaxPixels", cfg.MaxPixels, DefaultMaxPixels); err != nil {
		return nil, err
	}
	if cfg.DecodeMemoryBudget, err = limit("DecodeMemoryBudget", cfg.DecodeMemoryBudget, DefaultDecodeMemoryBudget); err != nil {
		return nil, err
	}
	if cfg.MaxCacheBytes, err = limit("MaxCacheBytes", cfg.MaxCacheBytes, DefaultMaxCacheBytes); err != nil {
		return nil, err
	}
	maxAge, err := limit("MaxCacheAge", int64(cfg.MaxCacheAge), int64(DefaultMaxCacheAge))
	if err != nil {
		return nil, err
	}
	cfg.MaxCacheAge = time.Duration(maxAge)
	decodes, err := limit("MaxDecodes", int64(cfg.MaxDecodes), DefaultMaxDecodes)
	if err != nil {
		return nil, err
	}
	requests, err := limit("MaxRequests", int64(cfg.MaxRequests), DefaultMaxRequests)
	if err != nil {
		return nil, err
	}
	cfg.MaxDecodes, cfg.MaxRequests = int(decodes), int(requests)
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.Logf == nil {
		cfg.Logf = log.Printf
	}
	c, err := newCache(cfg.CacheDir, cfg.MaxCacheBytes, cfg.MaxCacheAge, cfg.Now)
	if err != nil {
		return nil, err
	}
	return &Service{
		cfg:      cfg,
		cache:    c,
		local:    NewGoRenderer(cfg.MaxPixels, cfg.DecodeMemoryBudget),
		requests: make(chan struct{}, cfg.MaxRequests),
		decodes:  make(chan struct{}, cfg.MaxDecodes),
		flight:   make(map[string]*flight),
	}, nil
}

// limit returns def for zero, an error for a negative value, else v.
func limit(name string, v, def int64) (int64, error) {
	switch {
	case v < 0:
		return 0, fmt.Errorf("thumbs: %s must not be negative", name)
	case v == 0:
		return def, nil
	}
	return v, nil
}

// Close releases the cache directory handle. Calls in flight finish normally.
func (s *Service) Close() error { return s.cache.close() }

// note reports a named event to Config.Notify when one is set.
func (s *Service) note(event string) {
	if s.cfg.Notify != nil {
		s.cfg.Notify(event)
	}
}

// Get returns the thumbnail of the file at path, opened through set, with its
// longest side at most the bucket for size (see Bucket). Concurrent calls for
// the same file version and bucket share one render. A cancelled ctx stops the
// wait and, when no other caller still waits, the render.
func (s *Service) Get(ctx context.Context, set *rootfs.Set, path string, size int) (*Result, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	bucket, err := Bucket(size)
	if err != nil {
		return nil, err
	}
	if set == nil {
		return nil, errors.New("thumbs: no file set")
	}
	select {
	case s.requests <- struct{}{}:
		defer func() { <-s.requests }()
	default:
		return nil, ErrBusy
	}

	h, err := set.Open(path)
	if err != nil {
		return nil, fmt.Errorf("thumbs: %w", err)
	}
	file, err := h.Open()
	if err != nil {
		return nil, fmt.Errorf("thumbs: %w", err)
	}
	handedOver := false
	defer func() {
		if !handedOver {
			_ = file.Close()
		}
	}()
	info, err := file.Stat()
	if err != nil {
		return nil, fmt.Errorf("thumbs: %w", err)
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("%w: not a regular file", ErrNotAvailable)
	}
	if info.Size() > s.cfg.MaxSourceBytes {
		return nil, fmt.Errorf("%w: source exceeds %d bytes", ErrNotAvailable, s.cfg.MaxSourceBytes)
	}
	ver := versionOf(h.Path(), info)
	name := ver.cacheName(bucket)

	if data, ok := s.cache.get(name); ok {
		return ver.result(data, bucket, true), nil
	}
	f, leader := s.joinFlight(name)
	if leader {
		handedOver = true
		go s.runFlight(f, name, file, ver, bucket)
	}
	data, err := s.waitFlight(ctx, name, f)
	if err != nil {
		return nil, err
	}
	return ver.result(data, bucket, false), nil
}

// joinFlight returns the live render for name, or registers a new one and says
// the caller must run it (leader).
func (s *Service) joinFlight(name string) (f *flight, leader bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if f, ok := s.flight[name]; ok && f.ctx.Err() == nil {
		f.waiters++
		return f, false
	}
	ctx, cancel := context.WithCancel(context.Background())
	f = &flight{done: make(chan struct{}), ctx: ctx, cancel: cancel, waiters: 1}
	s.flight[name] = f
	return f, true
}

// waitFlight waits for the render. If ctx ends first the caller detaches, and
// the render is cancelled once nobody is left waiting for it.
func (s *Service) waitFlight(ctx context.Context, name string, f *flight) ([]byte, error) {
	select {
	case <-f.done:
		return f.data, f.err
	case <-ctx.Done():
		s.mu.Lock()
		select {
		case <-f.done:
		default:
			f.waiters--
			if f.waiters == 0 {
				if s.flight[name] == f {
					delete(s.flight, name)
				}
				f.cancel()
			}
		}
		s.mu.Unlock()
		return nil, ctx.Err()
	}
}

// runFlight does the render in its own goroutine, turns a panic in a decoder
// (a crafted image can cause one) into ErrNotAvailable instead of ending the
// process, publishes the result and wakes every waiter.
func (s *Service) runFlight(f *flight, name string, file *os.File, ver version, bucket int) {
	var data []byte
	var err error
	func() {
		defer func() {
			if r := recover(); r != nil {
				s.cfg.Logf("thumbnail decoder panicked: %v", r)
				data, err = nil, fmt.Errorf("%w: decoder panicked", ErrNotAvailable)
			}
		}()
		data, err = s.work(f.ctx, name, file, ver, bucket)
	}()
	s.mu.Lock()
	f.data, f.err = data, err
	if s.flight[name] == f {
		delete(s.flight, name)
	}
	s.mu.Unlock()
	close(f.done)
	f.cancel()
}

// work reads the source under a render slot, renders it and stores the result
// in the cache, unless the file changed while it was read or the render was
// cancelled. It closes file.
func (s *Service) work(ctx context.Context, name string, file *os.File, ver version, bucket int) ([]byte, error) {
	defer file.Close()
	select {
	case s.decodes <- struct{}{}:
		defer func() { <-s.decodes }()
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	source, err := io.ReadAll(io.LimitReader(file, s.cfg.MaxSourceBytes+1))
	if err != nil {
		return nil, fmt.Errorf("thumbs: read source: %w", err)
	}
	if int64(len(source)) > s.cfg.MaxSourceBytes {
		return nil, fmt.Errorf("%w: source exceeds %d bytes", ErrNotAvailable, s.cfg.MaxSourceBytes)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	// A file rewritten during the read must not be cached under its old identity.
	after, statErr := file.Stat()
	unchanged := statErr == nil && ver.same(after)

	data, err := s.render(ctx, source, ver, bucket)
	if err != nil {
		return nil, err
	}
	if unchanged && ctx.Err() == nil {
		if err := s.cache.put(name, data); err != nil {
			s.cfg.Logf("thumbnail cache write failed: %v", err) // serving the thumbnail does not need the cache
		}
	}
	return data, nil
}

// render tries the configured Renderer first and the built-in decoder only
// where the error says that is safe: the renderer cannot decode the format, or
// is not running (and has not crashed on this file). Every other error is final.
func (s *Service) render(ctx context.Context, source []byte, ver version, bucket int) ([]byte, error) {
	if s.cfg.Renderer == nil {
		return s.local.Render(ctx, source, bucket)
	}
	key := ver.crashKey()
	if s.crashes.poisoned(key) {
		return nil, fmt.Errorf("%w: the renderer crashed on this file before", ErrNotAvailable)
	}
	data, err := s.cfg.Renderer.Render(ctx, source, bucket)
	switch {
	case err == nil:
		return data, nil
	case ctx.Err() != nil:
		return nil, ctx.Err()
	case errors.Is(err, ErrNotAvailable):
		return nil, err
	case errors.Is(err, ErrBusy):
		s.note(eventBusy)
		return nil, err
	case errors.Is(err, ErrUnsupported):
		s.note(eventUnsupported) // expected: the built-in decoder handles it
		return s.local.Render(ctx, source, bucket)
	case errors.Is(err, ErrInterrupted):
		s.note(eventInterrupted)
		if s.crashes.strike(key) {
			s.note(eventPoisoned)
			return nil, fmt.Errorf("%w: the renderer crashed on this file twice", ErrNotAvailable)
		}
		return nil, err
	case errors.Is(err, ErrRenderFailed):
		s.note(eventFailed)
		return nil, err
	case errors.Is(err, ErrRendererUnavailable):
		s.note(eventBreaker)
		return nil, err
	case errors.Is(err, ErrDown):
		if s.crashes.struck(key) {
			s.note(eventRestarting)
			return nil, fmt.Errorf("%w: restarting after a crash on this file: %w", ErrRendererUnavailable, err)
		}
		s.note(eventFallback)
		return s.local.Render(ctx, source, bucket)
	default:
		// An error nobody classified: fail closed rather than decode untrusted
		// bytes unsandboxed on a guess.
		s.note(eventUnknown)
		return nil, fmt.Errorf("%w: %w", ErrRenderFailed, err)
	}
}
