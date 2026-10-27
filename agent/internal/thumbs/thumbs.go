// Package thumbs renders and caches JPEG thumbnails for image files.
//
// Rendering is done with a pure-Go decoder/resizer (github.com/disintegration/imaging),
// which supports JPEG, PNG, and GIF source images (no WebP/HEIC/etc — callers
// should treat ErrNotSupported as "no thumbnail available" and fall back to a
// generic icon). Results are cached on disk under <cacheDir> keyed by a hash of
// the source path, the requested size, and the source file's modification time,
// so re-rendering only happens when the source changes or the size differs.
package thumbs

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	"io"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/disintegration/imaging"
)

// ErrNotSupported is returned when the source file is not a decodable image
// (e.g. unsupported format, or decode failure).
var ErrNotSupported = errors.New("thumbnail not available for this file")

// jpegQuality is the quality used when re-encoding thumbnails.
const jpegQuality = 80

// Decode bounds (PR-09): reject oversized sources before a full decode so a
// crafted or huge image can't exhaust memory/CPU, and cap concurrent decodes.
// imaging registers the jpeg/png/gif/tiff/bmp decoders image.DecodeConfig uses.
const (
	maxThumbSourceBytes = 64 << 20 // 64 MiB on-disk source cap
	maxThumbPixels      = 40_000_000
	// A conservative estimate for the decoded source plus resize working data.
	decodeBytesPerPixel  = 8
	decodeMemoryBudget   = 384 << 20
	maxConcurrentDecodes = 4
	maxThumbCacheBytes   = 512 << 20
)

// fullDecodeBudget bounds aggregate memory pressure across simultaneous image
// decodes. It admits a large image only when the estimated working set fits.
var fullDecodeBudget = newDecodeBudget(decodeMemoryBudget)

// fullDecodeSem bounds CPU-heavy decode, resize, and encode work independently
// of the weighted memory budget. In particular, many tiny images each use
// little memory and could otherwise start an unbounded number of decoders.
var fullDecodeSem = make(chan struct{}, maxConcurrentDecodes)

func acquireDecodeSlot(ctx context.Context, sem chan struct{}) (func(), error) {
	select {
	case sem <- struct{}{}:
		return func() { <-sem }, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// headerDecodeSem bounds the lightweight config-decoding phase too, including
// work that cannot be interrupted once an image decoder starts reading.
var headerDecodeSem = make(chan struct{}, 8)

// Renderer renders and caches image thumbnails on disk.
type Renderer struct {
	cacheDir   string
	mu         sync.Mutex // in-flight renders
	inFlight   map[string]*renderCall
	cacheMu    sync.Mutex // cache writes and pruning
	cacheBytes int64
	remote     Remote // optional out-of-process renderer tried before the built-in decoder
	crashes    crashLog
}

// renderCall holds the result of one cache miss while concurrent callers for
// the same source version and requested size wait for it to finish.
type renderCall struct {
	done     chan struct{}
	data     []byte
	err      error
	ctx      context.Context
	cancel   context.CancelFunc
	waiters  int
	finished bool
}

type decodeWaiter struct {
	weight  int64
	ready   chan struct{}
	granted bool
}

type decodeBudget struct {
	mu      sync.Mutex
	limit   int64
	used    int64
	waiters []*decodeWaiter
}

func newDecodeBudget(limit int64) *decodeBudget {
	return &decodeBudget{limit: limit}
}

// acquire waits in FIFO order for a weighted memory reservation. Canceling a
// render while it waits removes its waiter without consuming budget.
func (b *decodeBudget) acquire(ctx context.Context, weight int64) error {
	if weight <= 0 || weight > b.limit {
		return fmt.Errorf("thumbnail decode estimate %d exceeds memory budget %d", weight, b.limit)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	waiter := &decodeWaiter{weight: weight, ready: make(chan struct{})}
	b.mu.Lock()
	if len(b.waiters) == 0 && b.used+weight <= b.limit {
		b.used += weight
		waiter.granted = true
		close(waiter.ready)
	} else {
		b.waiters = append(b.waiters, waiter)
		b.dispatchLocked()
	}
	b.mu.Unlock()

	select {
	case <-waiter.ready:
		if err := ctx.Err(); err != nil {
			b.release(waiter.weight)
			return err
		}
		return nil
	case <-ctx.Done():
		b.mu.Lock()
		if waiter.granted {
			b.used -= waiter.weight
		} else {
			for i, queued := range b.waiters {
				if queued == waiter {
					b.waiters = append(b.waiters[:i], b.waiters[i+1:]...)
					break
				}
			}
		}
		b.dispatchLocked()
		b.mu.Unlock()
		return ctx.Err()
	}
}

func (b *decodeBudget) release(weight int64) {
	b.mu.Lock()
	b.used -= weight
	b.dispatchLocked()
	b.mu.Unlock()
}

func (b *decodeBudget) dispatchLocked() {
	for len(b.waiters) > 0 {
		waiter := b.waiters[0]
		if b.used+waiter.weight > b.limit {
			return
		}
		b.waiters = b.waiters[1:]
		b.used += waiter.weight
		waiter.granted = true
		close(waiter.ready)
	}
}

// New creates a Renderer that stores cached thumbnails under cacheDir.
// The directory is created if it doesn't already exist.
func New(cacheDir string) (*Renderer, error) {
	if err := os.MkdirAll(cacheDir, 0o700); err != nil {
		return nil, fmt.Errorf("create thumb cache dir: %w", err)
	}
	rn := &Renderer{cacheDir: cacheDir, inFlight: make(map[string]*renderCall)}
	rn.cacheMu.Lock()
	rn.pruneCacheLocked()
	rn.cacheMu.Unlock()
	return rn, nil
}

// Get returns JPEG-encoded thumbnail bytes for srcPath, resized so its
// longest side is at most maxSize pixels.
//
// On a cache hit the cached bytes are returned directly. On a miss the image
// is decoded, resized, re-encoded as JPEG, written to the cache (atomically),
// and returned.
//
// Returns ErrNotSupported (wrapped) if srcPath isn't a decodable image.
func (rn *Renderer) Get(srcPath string, maxSize int) ([]byte, error) {
	return rn.GetContext(context.Background(), srcPath, maxSize)
}

// GetContext returns a cached thumbnail or joins one shared render for all
// concurrent callers requesting the same source version and size. A canceled
// caller detaches; shared work remains alive while another caller still waits.
func (rn *Renderer) GetContext(ctx context.Context, srcPath string, maxSize int) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	f, err := os.Open(srcPath)
	if err != nil {
		return nil, err
	}
	return rn.GetFileContext(ctx, f, srcPath, maxSize)
}

// GetFileContext returns a thumbnail for an already-open source file. It
// consumes and closes source, including when it joins an in-flight render or
// returns a cache hit. Callers that enforce filesystem access boundaries
// should open the file through that boundary and pass the handle here.
func (rn *Renderer) GetFileContext(ctx context.Context, source *os.File, cacheKey string, maxSize int) ([]byte, error) {
	if source == nil {
		return nil, fmt.Errorf("thumbnail source file is nil")
	}
	owned := false
	defer func() {
		if !owned {
			_ = source.Close()
		}
	}()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	info, err := source.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("%w: source is not a regular file", ErrNotSupported)
	}

	cachePath := rn.cachePath(cacheKey, maxSize, info)

	if data, err := os.ReadFile(cachePath); err == nil {
		return data, nil
	}

	rn.mu.Lock()
	if rn.inFlight == nil {
		rn.inFlight = make(map[string]*renderCall)
	}
	call, ok := rn.inFlight[cachePath]
	if ok && call.ctx.Err() == nil && !call.finished {
		call.waiters++
		rn.mu.Unlock()
		_ = source.Close()
		owned = true
		return rn.waitForCall(ctx, cachePath, call)
	}
	workCtx, cancel := context.WithCancel(context.Background())
	call = &renderCall{done: make(chan struct{}), ctx: workCtx, cancel: cancel, waiters: 1}
	rn.inFlight[cachePath] = call
	rn.mu.Unlock()

	owned = true
	go rn.renderCall(cachePath, source, maxSize, call)
	return rn.waitForCall(ctx, cachePath, call)
}

func (rn *Renderer) waitForCall(ctx context.Context, cachePath string, call *renderCall) ([]byte, error) {
	select {
	case <-call.done:
		return call.data, call.err
	case <-ctx.Done():
		rn.mu.Lock()
		if !call.finished && call.waiters > 0 {
			call.waiters--
			if call.waiters == 0 {
				if rn.inFlight[cachePath] == call {
					delete(rn.inFlight, cachePath)
				}
				call.cancel()
			}
		}
		rn.mu.Unlock()
		return nil, ctx.Err()
	}
}

func (rn *Renderer) renderCall(cachePath string, source *os.File, maxSize int, call *renderCall) {
	data, renderErr := rn.safeRenderOpened(call.ctx, source, maxSize)
	if renderErr == nil && call.ctx.Err() == nil {
		if err := rn.writeCache(cachePath, data); err != nil {
			// Cache write failures shouldn't prevent serving the thumbnail.
		}
	}
	rn.mu.Lock()
	call.data = data
	call.err = renderErr
	call.finished = true
	if rn.inFlight[cachePath] == call {
		delete(rn.inFlight, cachePath)
	}
	close(call.done)
	call.cancel()
	rn.mu.Unlock()
}

// safeRenderOpened runs the decoder and turns a panic in it (a crafted image
// can trip one in the image libraries) into ErrNotSupported. renderCall runs in
// its own goroutine, outside the HTTP recoverer, so an unrecovered panic here
// would end the whole agent process.
func (rn *Renderer) safeRenderOpened(ctx context.Context, source *os.File, maxSize int) (data []byte, err error) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("thumbnail decoder panicked: %v", r)
			data, err = nil, ErrNotSupported
		}
	}()
	return rn.renderOpened(ctx, source, maxSize)
}

// Render decodes the image at srcPath, resizes it so its longest side is at
// most maxSize pixels (preserving aspect ratio, never upscaling beyond the
// original), and re-encodes it as a JPEG at jpegQuality.
//
// Returns ErrNotSupported (wrapped) if the file can't be decoded as an image.
func Render(srcPath string, maxSize int) ([]byte, error) {
	return render(context.Background(), srcPath, maxSize)
}

func render(ctx context.Context, srcPath string, maxSize int) ([]byte, error) {
	cf, err := os.Open(srcPath)
	if err != nil {
		return nil, err
	}
	return renderOpened(ctx, cf, maxSize)
}

// renderOpened tries the remote renderer first (when one is set) and otherwise decodes in-process.
func (rn *Renderer) renderOpened(ctx context.Context, cf *os.File, maxSize int) ([]byte, error) {
	if rn.remote != nil {
		if info, err := cf.Stat(); err == nil && info.Mode().IsRegular() {
			if maxSize <= 0 {
				maxSize = 256
			}
			if data, final, err := rn.renderWithRemote(ctx, cf, info.Size(), maxSize); final {
				cf.Close()
				return data, err
			}
		}
	}
	return renderOpened(ctx, cf, maxSize)
}

func renderOpened(ctx context.Context, cf *os.File, maxSize int) ([]byte, error) {
	defer cf.Close()
	if maxSize <= 0 {
		maxSize = 256
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	// Bound the source before decoding (PR-09): on-disk size, then decoded
	// pixel dimensions read from the header without a full decode.
	info, err := cf.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("%w: source is not a regular file", ErrNotSupported)
	}
	if info.Size() > maxThumbSourceBytes {
		return nil, fmt.Errorf("%w: source exceeds %d bytes", ErrNotSupported, int64(maxThumbSourceBytes))
	}
	if _, err := cf.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}
	select {
	case headerDecodeSem <- struct{}{}:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	cfg, _, cfgErr := image.DecodeConfig(cf)
	<-headerDecodeSem
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if cfgErr != nil {
		return nil, fmt.Errorf("%w: %v", ErrNotSupported, cfgErr)
	}
	if cfg.Width <= 0 || cfg.Height <= 0 {
		return nil, fmt.Errorf("%w: invalid image dimensions %dx%d", ErrNotSupported, cfg.Width, cfg.Height)
	}
	width, height := int64(cfg.Width), int64(cfg.Height)
	if width > maxThumbPixels/height {
		return nil, fmt.Errorf("%w: %dx%d exceeds pixel budget", ErrNotSupported, cfg.Width, cfg.Height)
	}
	releaseDecodeSlot, err := acquireDecodeSlot(ctx, fullDecodeSem)
	if err != nil {
		return nil, err
	}
	defer releaseDecodeSlot()
	pixels := width * height

	decodeWeight := pixels * decodeBytesPerPixel
	if err := fullDecodeBudget.acquire(ctx, decodeWeight); err != nil {
		return nil, err
	}
	defer fullDecodeBudget.release(decodeWeight)
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	if _, err := cf.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}
	src, err := imaging.Decode(cf, imaging.AutoOrientation(true))
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrNotSupported, err)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	thumb := imaging.Fit(src, maxSize, maxSize, imaging.Lanczos)

	tmp, err := os.CreateTemp("", "rfe-thumb-*.jpg")
	if err != nil {
		return nil, fmt.Errorf("create temp file: %w", err)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)

	if err := jpeg.Encode(tmp, thumb, &jpeg.Options{Quality: jpegQuality}); err != nil {
		tmp.Close()
		return nil, fmt.Errorf("encode jpeg: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return nil, fmt.Errorf("close temp file: %w", err)
	}

	data, err := os.ReadFile(tmpPath)
	if err != nil {
		return nil, fmt.Errorf("read encoded thumbnail: %w", err)
	}
	return data, nil
}

// cachePath returns the on-disk path for a cached thumbnail keyed by the
// source path, requested size, and the source's modification time (so stale
// cache entries are naturally bypassed when the source file changes).
func (rn *Renderer) cachePath(srcPath string, maxSize int, info os.FileInfo) string {
	// The key names the file's content version: path, modification time, size and (where the OS has
	// one) device and inode, so a replaced file with a restored mtime is not served stale.
	sum := sha256.Sum256([]byte(srcPath + "\x00" + fileIdentity(info)))
	key := fmt.Sprintf("%s_%d_%d_%d.jpg", hex.EncodeToString(sum[:]), maxSize, info.ModTime().UnixNano(), info.Size())
	return filepath.Join(rn.cacheDir, key)
}

// writeCache atomically saves one rendered thumbnail and prunes the cache if
// its aggregate size exceeds the configured byte cap.
func (rn *Renderer) writeCache(path string, data []byte) error {
	rn.cacheMu.Lock()
	defer rn.cacheMu.Unlock()

	var replacedBytes int64
	if info, err := os.Lstat(path); err == nil && info.Mode().IsRegular() && isRendererCacheFile(filepath.Base(path)) {
		replacedBytes = info.Size()
	}
	if err := writeAtomic(path, data); err != nil {
		return err
	}
	rn.cacheBytes += int64(len(data)) - replacedBytes
	if rn.cacheBytes > maxThumbCacheBytes {
		rn.pruneCacheLocked()
	}
	return nil
}

type cachedFile struct {
	path    string
	name    string
	size    int64
	modTime int64
}

// pruneCacheLocked removes the oldest renderer-owned regular files until the
// cache is at or below its byte cap. Unknown files, directories, and symlinks
// are never followed or removed.
func (rn *Renderer) pruneCacheLocked() {
	entries, err := os.ReadDir(rn.cacheDir)
	if err != nil {
		return
	}

	files := make([]cachedFile, 0, len(entries))
	var total int64
	for _, entry := range entries {
		if !isRendererCacheFile(entry.Name()) {
			continue
		}
		info, err := entry.Info()
		if err != nil || !info.Mode().IsRegular() {
			continue
		}
		files = append(files, cachedFile{
			path:    filepath.Join(rn.cacheDir, entry.Name()),
			name:    entry.Name(),
			size:    info.Size(),
			modTime: info.ModTime().UnixNano(),
		})
		total += info.Size()
	}

	if total > maxThumbCacheBytes {
		sort.Slice(files, func(i, j int) bool {
			if files[i].modTime == files[j].modTime {
				return files[i].name < files[j].name
			}
			return files[i].modTime < files[j].modTime
		})
		for _, file := range files {
			if total <= maxThumbCacheBytes {
				break
			}
			if err := os.Remove(file.path); err == nil {
				total -= file.size
			}
		}
	}
	rn.cacheBytes = total
}

func isRendererCacheFile(name string) bool {
	if !strings.HasSuffix(name, ".jpg") {
		return false
	}
	parts := strings.Split(strings.TrimSuffix(name, ".jpg"), "_")
	// <digest>_<size>_<mtime>[_<bytes>]: the byte count is part of the key since F-54; older files lack it.
	if (len(parts) != 3 && len(parts) != 4) || len(parts[0]) != sha256.Size*2 {
		return false
	}
	if _, err := hex.DecodeString(parts[0]); err != nil {
		return false
	}
	if _, err := strconv.Atoi(parts[1]); err != nil {
		return false
	}
	for _, p := range parts[2:] {
		if _, err := strconv.ParseInt(p, 10, 64); err != nil {
			return false
		}
	}
	return true
}

// writeAtomic writes data to path via a temp file + rename, matching the
// atomic-write pattern used elsewhere in the agent (see internal/transfer).
func writeAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".thumb-*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpPath)
		return err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		os.Remove(tmpPath)
		return err
	}
	return nil
}
