package thumbs

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ZaidQamhieh/rfe/agent/internal/next/rootfs"
	"github.com/ZaidQamhieh/rfe/agent/internal/sidecar"
)

// waitLimit bounds every wait on another goroutine in these tests.
const waitLimit = 5 * time.Second

// fakeRenderer is a Renderer whose behaviour a test supplies; it counts calls.
type fakeRenderer struct {
	mu    sync.Mutex
	calls int
	fn    func(ctx context.Context, source []byte, maxSize int) ([]byte, error)
}

func (f *fakeRenderer) Render(ctx context.Context, source []byte, maxSize int) ([]byte, error) {
	f.mu.Lock()
	f.calls++
	f.mu.Unlock()
	return f.fn(ctx, source, maxSize)
}

func (f *fakeRenderer) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

// passThrough renders with the built-in renderer, so a fake can count real renders.
func passThrough() func(context.Context, []byte, int) ([]byte, error) {
	g := NewGoRenderer(DefaultMaxPixels, DefaultDecodeMemoryBudget)
	return g.Render
}

// failWith makes a fake that always returns err.
func failWith(err error) *fakeRenderer {
	return &fakeRenderer{fn: func(context.Context, []byte, int) ([]byte, error) { return nil, err }}
}

type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *fakeClock) Add(d time.Duration) {
	c.mu.Lock()
	c.t = c.t.Add(d)
	c.mu.Unlock()
}

type events struct {
	mu   sync.Mutex
	list []string
}

func (e *events) Notify(name string) {
	e.mu.Lock()
	e.list = append(e.list, name)
	e.mu.Unlock()
}

func (e *events) has(name string) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, n := range e.list {
		if n == name {
			return true
		}
	}
	return false
}

type env struct {
	t     *testing.T
	svc   *Service
	set   *rootfs.Set
	root  string // the folder the set allows
	cache string
	ev    *events
}

// newEnv builds a Service over fresh temp dirs and a Set limited to root.
func newEnv(t *testing.T, mutate func(*Config)) *env {
	t.Helper()
	root, cacheDir := t.TempDir(), filepath.Join(t.TempDir(), "cache")
	set, err := rootfs.New([]string{root}, rootfs.Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = set.Close() })
	ev := &events{}
	cfg := Config{CacheDir: cacheDir, Notify: ev.Notify, Logf: t.Logf}
	if mutate != nil {
		mutate(&cfg)
	}
	svc, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = svc.Close() })
	return &env{t: t, svc: svc, set: set, root: root, cache: cacheDir, ev: ev}
}

// writePNG writes a w x h gradient PNG tinted by seed and returns its path.
func (e *env) writePNG(name string, w, h int, seed uint8) string {
	e.t.Helper()
	p := filepath.Join(e.root, name)
	if err := os.WriteFile(p, pngBytes(e.t, w, h, seed), 0o600); err != nil {
		e.t.Fatal(err)
	}
	return p
}

func pngBytes(t *testing.T, w, h int, seed uint8) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.NRGBA{R: uint8(x), G: uint8(y), B: seed, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// bombPNG is a valid small PNG whose header claims w x h pixels: the decoder
// reads the size from the header, so a size check can reject it before any
// pixel memory is allocated.
func bombPNG(t *testing.T, w, h uint32) []byte {
	t.Helper()
	data := pngBytes(t, 2, 2, 0)
	const ihdr = 8 + 4 // signature + length of the IHDR chunk; the chunk type follows
	binary.BigEndian.PutUint32(data[ihdr+4:], w)
	binary.BigEndian.PutUint32(data[ihdr+8:], h)
	crc := crc32.ChecksumIEEE(data[ihdr : ihdr+4+13]) // chunk type + 13 data bytes
	binary.BigEndian.PutUint32(data[ihdr+4+13:], crc)
	return data
}

func (e *env) get(path string, size int) (*Result, error) {
	return e.svc.Get(context.Background(), e.set, path, size)
}

func (e *env) mustGet(path string, size int) *Result {
	e.t.Helper()
	res, err := e.get(path, size)
	if err != nil {
		e.t.Fatalf("Get(%s, %d): %v", filepath.Base(path), size, err)
	}
	return res
}

// cacheFiles lists the thumbnail files in the cache dir with their sizes.
func cacheFiles(t *testing.T, dir string) map[string]int64 {
	t.Helper()
	list, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]int64{}
	for _, e := range list {
		if isCacheName(e.Name()) {
			info, err := e.Info()
			if err != nil {
				t.Fatal(err)
			}
			out[e.Name()] = info.Size()
		}
	}
	return out
}

// cacheNameOf is the cache file name of a source file at a bucket.
func cacheNameOf(t *testing.T, path string, bucket int) string {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return versionOf(path, info).cacheName(bucket)
}

func dims(t *testing.T, data []byte) (int, int) {
	t.Helper()
	cfg, err := jpeg.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("result is not a JPEG: %v", err)
	}
	return cfg.Width, cfg.Height
}

func TestHitAndMiss(t *testing.T) {
	counter := &fakeRenderer{fn: passThrough()}
	e := newEnv(t, func(c *Config) { c.Renderer = counter })
	p := e.writePNG("a.png", 300, 200, 1)

	first := e.mustGet(p, 256)
	if first.Cached {
		t.Fatal("first request reported a cache hit")
	}
	if w, h := dims(t, first.Data); w != 256 || h < 170 || h > 171 {
		t.Fatalf("thumbnail is %dx%d, want 256x170 or 171 (aspect kept, longest side 256)", w, h)
	}
	if first.ContentType != ContentType || first.Size != 256 {
		t.Fatalf("ContentType=%q Size=%d", first.ContentType, first.Size)
	}
	second := e.mustGet(p, 256)
	if !second.Cached {
		t.Fatal("second request was not served from the cache")
	}
	if !bytes.Equal(first.Data, second.Data) || first.ETag != second.ETag {
		t.Fatal("cached answer differs from the rendered one")
	}
	if n := counter.count(); n != 1 {
		t.Fatalf("renderer ran %d times, want 1", n)
	}
	if got := len(cacheFiles(t, e.cache)); got != 1 {
		t.Fatalf("%d files in the cache, want 1", got)
	}
}

func TestResultHeaders(t *testing.T) {
	e := newEnv(t, nil)
	p := e.writePNG("a.png", 100, 100, 1)
	want := time.Date(2024, 5, 6, 7, 8, 9, 0, time.UTC)
	if err := os.Chtimes(p, want, want); err != nil {
		t.Fatal(err)
	}
	res := e.mustGet(p, 0)
	if !res.ModTime.Equal(want) {
		t.Fatalf("ModTime %v, want the source mtime %v", res.ModTime, want)
	}
	if !strings.HasPrefix(res.ETag, `W/"`) || !strings.HasSuffix(res.ETag, `"`) || len(res.ETag) < 8 {
		t.Fatalf("ETag %q is not a quoted weak validator", res.ETag)
	}
	other := e.mustGet(p, 512)
	if other.ETag == res.ETag {
		t.Fatal("different buckets share an ETag")
	}
}

func TestChangedFileInvalidates(t *testing.T) {
	e := newEnv(t, nil)
	p := e.writePNG("a.png", 300, 200, 1)
	old := e.mustGet(p, 256)

	// Same size on disk and different pixels, only the mtime moves.
	later := time.Now().Add(time.Hour)
	if err := os.Chtimes(p, later, later); err != nil {
		t.Fatal(err)
	}
	touched := e.mustGet(p, 256)
	if touched.Cached || touched.ETag == old.ETag {
		t.Fatalf("a changed mtime served the old thumbnail (cached=%v)", touched.Cached)
	}

	// New content of another size under the same name and mtime.
	if err := os.WriteFile(p, pngBytes(t, 120, 120, 9), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(p, later, later); err != nil {
		t.Fatal(err)
	}
	fresh := e.mustGet(p, 256)
	if fresh.Cached {
		t.Fatal("a changed file was served from the cache")
	}
	if w, h := dims(t, fresh.Data); w != 120 || h != 120 {
		t.Fatalf("thumbnail is %dx%d, want the new 120x120 image", w, h)
	}
	if again := e.mustGet(p, 256); !again.Cached {
		t.Fatal("unchanged file should hit the cache")
	}
}

func TestBucketValidation(t *testing.T) {
	cases := []struct {
		in   int
		want int
	}{{0, DefaultSize}, {1, 64}, {64, 64}, {65, 128}, {200, 256}, {256, 256}, {257, 512}, {1000, 1024}, {1024, 1024}, {5000, 1024}}
	for _, c := range cases {
		got, err := Bucket(c.in)
		if err != nil || got != c.want {
			t.Errorf("Bucket(%d) = %d, %v; want %d", c.in, got, err, c.want)
		}
	}
	if _, err := Bucket(-1); !errors.Is(err, ErrBadSize) {
		t.Errorf("Bucket(-1) error %v, want ErrBadSize", err)
	}
	if b := Buckets(); b[len(b)-1] != MaxSize() {
		t.Errorf("Buckets %v do not end at MaxSize %d", b, MaxSize())
	}

	e := newEnv(t, nil)
	p := e.writePNG("big.png", 2000, 1000, 1)
	if _, err := e.get(p, -5); !errors.Is(err, ErrBadSize) {
		t.Fatalf("negative size: %v, want ErrBadSize", err)
	}
	res := e.mustGet(p, 100)
	if w, h := dims(t, res.Data); w != 128 || h != 64 || res.Size != 128 {
		t.Fatalf("size 100 gave %dx%d (bucket %d), want 128x64 (bucket 128)", w, h, res.Size)
	}
	small := e.writePNG("small.png", 50, 40, 1)
	if w, h := dims(t, e.mustGet(small, 512).Data); w != 50 || h != 40 {
		t.Fatalf("a small image was upscaled to %dx%d", w, h)
	}
}

func TestNewRejectsBadConfig(t *testing.T) {
	if _, err := New(Config{}); err == nil {
		t.Error("empty cache dir accepted")
	}
	if _, err := New(Config{CacheDir: t.TempDir(), MaxCacheBytes: -1}); err == nil {
		t.Error("negative limit accepted")
	}
}

func TestDecompressionBombRefused(t *testing.T) {
	e := newEnv(t, func(c *Config) { c.MaxPixels = 1_000_000 })
	huge := filepath.Join(e.root, "bomb.png")
	if err := os.WriteFile(huge, bombPNG(t, 100_000, 100_000), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := e.get(huge, 256); !errors.Is(err, ErrNotAvailable) {
		t.Fatalf("bomb: %v, want ErrNotAvailable", err)
	}
	if n := len(cacheFiles(t, e.cache)); n != 0 {
		t.Fatalf("a refused image left %d cache files", n)
	}
	// An image within the cap still renders on the same service.
	ok := e.writePNG("ok.png", 500, 500, 1)
	if _, err := e.get(ok, 256); err != nil {
		t.Fatalf("image under the pixel cap: %v", err)
	}
	// A cap that the memory budget cannot cover is refused, not waited on forever.
	tight := newEnv(t, func(c *Config) { c.DecodeMemoryBudget = 1000 })
	if _, err := tight.get(tight.writePNG("x.png", 100, 100, 1), 256); !errors.Is(err, ErrNotAvailable) {
		t.Fatalf("over the memory budget: %v, want ErrNotAvailable", err)
	}
}

func TestSourceByteCap(t *testing.T) {
	counter := &fakeRenderer{fn: passThrough()}
	const limit = 200
	e := newEnv(t, func(c *Config) { c.MaxSourceBytes = limit; c.Renderer = counter })
	p := e.writePNG("a.png", 300, 300, 1)
	if info, _ := os.Stat(p); info.Size() <= limit {
		t.Fatalf("test image is only %d bytes", info.Size())
	}
	if _, err := e.get(p, 256); !errors.Is(err, ErrNotAvailable) {
		t.Fatalf("oversized source: %v, want ErrNotAvailable", err)
	}
	if counter.count() != 0 {
		t.Fatal("an oversized source reached a renderer")
	}
}

func TestNotAnImage(t *testing.T) {
	e := newEnv(t, nil)
	txt := filepath.Join(e.root, "a.txt")
	if err := os.WriteFile(txt, []byte("hello"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := e.get(txt, 256); !errors.Is(err, ErrNotAvailable) {
		t.Fatalf("text file: %v, want ErrNotAvailable", err)
	}
	dir := filepath.Join(e.root, "d")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := e.get(dir, 256); !errors.Is(err, ErrNotAvailable) {
		t.Fatalf("folder: %v, want ErrNotAvailable", err)
	}
	if _, err := e.get(filepath.Join(e.root, "missing.png"), 256); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("missing file: %v, want fs.ErrNotExist", err)
	}
	if _, err := e.get("relative.png", 256); !errors.Is(err, rootfs.ErrInvalidPath) {
		t.Fatalf("relative path: %v, want rootfs.ErrInvalidPath", err)
	}
}

func TestJailEscapeAndProtected(t *testing.T) {
	counter := &fakeRenderer{fn: passThrough()}
	e := newEnv(t, func(c *Config) { c.Renderer = counter })

	outside := t.TempDir()
	secret := filepath.Join(outside, "secret.png")
	if err := os.WriteFile(secret, pngBytes(t, 50, 50, 1), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := e.get(secret, 256); !errors.Is(err, rootfs.ErrOutsideRoots) {
		t.Fatalf("path outside the roots: %v, want ErrOutsideRoots", err)
	}
	link := filepath.Join(e.root, "link.png")
	if err := os.Symlink(secret, link); err != nil {
		t.Skipf("cannot create symlinks here: %v", err)
	}
	if _, err := e.get(link, 256); !errors.Is(err, rootfs.ErrOutsideRoots) {
		t.Fatalf("symlink out of the root: %v, want ErrOutsideRoots", err)
	}
	dirLink := filepath.Join(e.root, "dir")
	if err := os.Symlink(outside, dirLink); err != nil {
		t.Fatal(err)
	}
	if _, err := e.get(filepath.Join(dirLink, "secret.png"), 256); !errors.Is(err, rootfs.ErrOutsideRoots) {
		t.Fatalf("symlinked folder: %v, want ErrOutsideRoots", err)
	}
	if counter.count() != 0 {
		t.Fatal("a refused source reached a renderer")
	}

	// The agent's data directory is unreachable even when everything else is.
	data := t.TempDir()
	hidden := filepath.Join(data, "photo.png")
	if err := os.WriteFile(hidden, pngBytes(t, 50, 50, 1), 0o600); err != nil {
		t.Fatal(err)
	}
	all, err := rootfs.Unrestricted(rootfs.Options{Protected: []string{data}})
	if err != nil {
		t.Fatal(err)
	}
	defer all.Close()
	if _, err := e.svc.Get(context.Background(), all, hidden, 256); !errors.Is(err, rootfs.ErrProtected) {
		t.Fatalf("protected data dir: %v, want ErrProtected", err)
	}
	if _, err := e.svc.Get(context.Background(), all, secret, 256); err != nil {
		t.Fatalf("unrestricted set should reach an ordinary file: %v", err)
	}
}

func TestCancel(t *testing.T) {
	started := make(chan struct{})
	stopped := make(chan struct{})
	blocker := &fakeRenderer{fn: func(ctx context.Context, _ []byte, _ int) ([]byte, error) {
		close(started)
		<-ctx.Done()
		close(stopped)
		return nil, ctx.Err()
	}}
	e := newEnv(t, func(c *Config) { c.Renderer = blocker })
	p := e.writePNG("a.png", 100, 100, 1)

	ctx, cancel := context.WithCancel(context.Background())
	errc := make(chan error, 1)
	go func() {
		_, err := e.svc.Get(ctx, e.set, p, 256)
		errc <- err
	}()
	waitFor(t, started, "the renderer to start")
	cancel()
	select {
	case err := <-errc:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("cancelled Get returned %v, want context.Canceled", err)
		}
	case <-time.After(waitLimit):
		t.Fatal("Get did not return after cancel")
	}
	waitFor(t, stopped, "the render to be cancelled")
	if n := len(cacheFiles(t, e.cache)); n != 0 {
		t.Fatalf("a cancelled render left %d cache files", n)
	}

	pre, preCancel := context.WithCancel(context.Background())
	preCancel()
	if _, err := e.svc.Get(pre, e.set, p, 256); !errors.Is(err, context.Canceled) {
		t.Fatalf("already-cancelled context: %v", err)
	}
	if blocker.count() != 1 {
		t.Fatalf("renderer called %d times, want 1", blocker.count())
	}
}

func waitFor(t *testing.T, ch <-chan struct{}, what string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(waitLimit):
		t.Fatalf("timed out waiting for %s", what)
	}
}

func TestSharedRenderAndRequestLimit(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	inner := passThrough()
	var startOnce sync.Once
	gate := &fakeRenderer{fn: func(ctx context.Context, src []byte, size int) ([]byte, error) {
		startOnce.Do(func() { close(started) })
		select {
		case <-release:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
		return inner(ctx, src, size)
	}}
	const waiters = 3
	e := newEnv(t, func(c *Config) { c.Renderer = gate; c.MaxRequests = waiters })
	p := e.writePNG("a.png", 100, 100, 1)
	other := e.writePNG("b.png", 100, 100, 2)

	results := make(chan *Result, waiters)
	errs := make(chan error, waiters)
	for i := 0; i < waiters; i++ {
		go func() {
			res, err := e.svc.Get(context.Background(), e.set, p, 256)
			results <- res
			errs <- err
		}()
	}
	waitFor(t, started, "the render to start")
	deadline := time.Now().Add(waitLimit)
	for e.waitersOf() < waiters {
		if time.Now().After(deadline) {
			t.Fatalf("only %d of %d callers joined the shared render", e.waitersOf(), waiters)
		}
		time.Sleep(time.Millisecond) // polling for the other goroutines to register, bounded by deadline
	}
	// Every request slot is taken, so the next caller is refused at once.
	if _, err := e.get(other, 256); !errors.Is(err, ErrBusy) {
		t.Fatalf("request over the limit: %v, want ErrBusy", err)
	}
	close(release)
	var first *Result
	for i := 0; i < waiters; i++ {
		if err := <-errs; err != nil {
			t.Fatal(err)
		}
		res := <-results
		if first == nil {
			first = res
		} else if !bytes.Equal(first.Data, res.Data) {
			t.Fatal("callers of one shared render got different bytes")
		}
	}
	if n := gate.count(); n != 1 {
		t.Fatalf("renderer ran %d times for %d callers, want 1", n, waiters)
	}
	// The slots are free again.
	if _, err := e.get(other, 256); err != nil {
		t.Fatalf("after the burst: %v", err)
	}
}

// waitersOf returns the most waiters any live render has.
func (e *env) waitersOf() int {
	e.svc.mu.Lock()
	defer e.svc.mu.Unlock()
	most := 0
	for _, f := range e.svc.flight {
		most = max(most, f.waiters)
	}
	return most
}

func TestConcurrentSameFileUnderRace(t *testing.T) {
	e := newEnv(t, nil)
	p := e.writePNG("a.png", 400, 300, 1)
	const callers = 16
	var wg sync.WaitGroup
	datas := make([][]byte, callers)
	errs := make([]error, callers)
	for i := 0; i < callers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			var res *Result
			res, errs[i] = e.svc.Get(context.Background(), e.set, p, 256)
			if res != nil {
				datas[i] = res.Data
			}
		}()
	}
	wg.Wait()
	var ref []byte
	for i := range datas {
		if errs[i] != nil {
			if !errors.Is(errs[i], ErrBusy) { // more callers than request slots may be refused
				t.Fatalf("caller %d: %v", i, errs[i])
			}
			continue
		}
		if ref == nil {
			ref = datas[i]
		} else if !bytes.Equal(datas[i], ref) {
			t.Fatalf("caller %d got different bytes", i)
		}
	}
	if ref == nil {
		t.Fatal("no caller got a thumbnail")
	}
}

func TestRendererFallbackAndFinalErrors(t *testing.T) {
	sidecarBusy := mapSidecarError(&sidecar.RemoteError{Code: "BUSY", Message: "queue full"})
	sidecarBad := mapSidecarError(&sidecar.RemoteError{Code: "BAD_REQUEST", Message: "bad frame"})
	cases := []struct {
		name         string
		err          error
		wantFallback bool  // the built-in decoder produced the thumbnail
		wantErr      error // when not falling back
		event        string
	}{
		{"unsupported format", fmt.Errorf("%w: webp variant", ErrUnsupported), true, nil, eventUnsupported},
		{"sidecar down", fmt.Errorf("%w: not started", ErrDown), true, nil, eventFallback},
		{"sidecar busy", sidecarBusy, false, ErrBusy, eventBusy},
		{"sidecar bad request", sidecarBad, false, ErrRenderFailed, eventFailed},
		{"render failed", fmt.Errorf("%w: decoder panic", ErrRenderFailed), false, ErrRenderFailed, eventFailed},
		{"breaker open", fmt.Errorf("%w: breaker", ErrRendererUnavailable), false, ErrRendererUnavailable, eventBreaker},
		{"unclassified error", errors.New("frame decode: boom"), false, ErrRenderFailed, eventUnknown},
		{"no thumbnail exists", fmt.Errorf("%w: too large", ErrNotAvailable), false, ErrNotAvailable, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			e := newEnv(t, func(cfg *Config) { cfg.Renderer = failWith(c.err) })
			p := e.writePNG("a.png", 100, 100, 1)
			res, err := e.get(p, 256)
			if c.wantFallback {
				if err != nil {
					t.Fatalf("expected the built-in decoder to take over, got %v", err)
				}
				if w, _ := dims(t, res.Data); w != 100 {
					t.Fatalf("fallback thumbnail is %d wide, want 100", w)
				}
			} else {
				if !errors.Is(err, c.wantErr) {
					t.Fatalf("error %v, want %v", err, c.wantErr)
				}
				if e.ev.has(eventFallback) || e.ev.has(eventUnsupported) {
					t.Fatal("the built-in decoder was used for a final error")
				}
				if n := len(cacheFiles(t, e.cache)); n != 0 {
					t.Fatalf("a failed render left %d cache files", n)
				}
			}
			if c.event != "" && !e.ev.has(c.event) {
				t.Fatalf("event %q was not reported", c.event)
			}
		})
	}
}

// TestSidecarBusyDoesNotDecodeInProcess is the regression for the old
// renderer: a BUSY answer from the sandboxed sidecar fell through to the
// in-process decoder.
func TestSidecarBusyDoesNotDecodeInProcess(t *testing.T) {
	caller := callerFunc(func(context.Context, string, any, []byte, any) ([]byte, error) {
		return nil, &sidecar.RemoteError{Code: "BUSY", Message: "more than 256 MiB queued"}
	})
	e := newEnv(t, func(c *Config) { c.Renderer = SidecarRenderer{Caller: caller} })
	p := e.writePNG("a.png", 100, 100, 1)
	if _, err := e.get(p, 256); !errors.Is(err, ErrBusy) {
		t.Fatalf("busy sidecar: %v, want ErrBusy", err)
	}
	if e.ev.has(eventFallback) {
		t.Fatal("a busy sidecar sent the file to the in-process decoder")
	}
	// Once the sidecar has room the same file renders through it.
	caller2 := callerFunc(func(_ context.Context, op string, req any, body []byte, _ any) ([]byte, error) {
		if op != sidecarOp || len(body) == 0 {
			return nil, fmt.Errorf("unexpected call %q with %d body bytes", op, len(body))
		}
		return passThroughBytes(body, req.(map[string]int)["maxSize"])
	})
	e.svc.cfg.Renderer = SidecarRenderer{Caller: caller2}
	if _, err := e.get(p, 256); err != nil {
		t.Fatalf("sidecar with room: %v", err)
	}
}

func passThroughBytes(body []byte, size int) ([]byte, error) {
	return NewGoRenderer(DefaultMaxPixels, DefaultDecodeMemoryBudget).Render(context.Background(), body, size)
}

type callerFunc func(ctx context.Context, op string, req any, body []byte, resp any) ([]byte, error)

func (f callerFunc) CallBody(ctx context.Context, op string, req any, body []byte, resp any) ([]byte, error) {
	return f(ctx, op, req, body, resp)
}

func TestMapSidecarError(t *testing.T) {
	both := func(a, b error) error { return fmt.Errorf("%w: %w", a, b) }
	cases := []struct {
		name string
		in   error
		want error
	}{
		{"NOT_SUPPORTED", &sidecar.RemoteError{Code: "NOT_SUPPORTED"}, ErrUnsupported},
		{"TOO_LARGE", &sidecar.RemoteError{Code: "TOO_LARGE"}, ErrNotAvailable},
		{"BUSY", &sidecar.RemoteError{Code: "BUSY"}, ErrBusy},
		{"BAD_REQUEST", &sidecar.RemoteError{Code: "BAD_REQUEST"}, ErrRenderFailed},
		{"INTERNAL", &sidecar.RemoteError{Code: "INTERNAL"}, ErrRenderFailed},
		{"unknown code", &sidecar.RemoteError{Code: "SOMETHING_NEW"}, ErrRenderFailed},
		{"down", sidecar.ErrUnavailable, ErrDown},
		{"interrupted", both(sidecar.ErrUnavailable, sidecar.ErrInterrupted), ErrInterrupted},
		{"breaker", both(sidecar.ErrUnavailable, sidecar.ErrBreakerOpen), ErrRendererUnavailable},
	}
	for _, c := range cases {
		if got := mapSidecarError(c.in); !errors.Is(got, c.want) {
			t.Errorf("%s: mapped to %v, want %v", c.name, got, c.want)
		}
	}
	plain := errors.New("decode response: bad json")
	if got := mapSidecarError(plain); !errors.Is(got, plain) || errors.Is(got, ErrDown) || errors.Is(got, ErrUnsupported) {
		t.Errorf("a protocol error was mapped to %v; it must stay unclassified", got)
	}
}

func TestSidecarRendererChecksJPEG(t *testing.T) {
	garbage := callerFunc(func(context.Context, string, any, []byte, any) ([]byte, error) {
		return []byte("not a jpeg at all"), nil
	})
	if _, err := (SidecarRenderer{Caller: garbage}).Render(context.Background(), []byte("x"), 128); !errors.Is(err, ErrRenderFailed) {
		t.Fatalf("non-JPEG answer: %v, want ErrRenderFailed", err)
	}
}

func TestInterruptedTwiceIsPoisoned(t *testing.T) {
	var mode struct {
		sync.Mutex
		err error
	}
	mode.err = fmt.Errorf("%w: sidecar died", ErrInterrupted)
	r := &fakeRenderer{fn: func(context.Context, []byte, int) ([]byte, error) {
		mode.Lock()
		defer mode.Unlock()
		return nil, mode.err
	}}
	e := newEnv(t, func(c *Config) { c.Renderer = r })
	p := e.writePNG("a.png", 100, 100, 1)

	if _, err := e.get(p, 256); !errors.Is(err, ErrInterrupted) || errors.Is(err, ErrNotAvailable) {
		t.Fatalf("first interruption: %v, want a transient ErrInterrupted", err)
	}
	// While the sidecar restarts, a file it already died on is not decoded here.
	mode.Lock()
	mode.err = fmt.Errorf("%w: restarting", ErrDown)
	mode.Unlock()
	if _, err := e.get(p, 256); !errors.Is(err, ErrRendererUnavailable) {
		t.Fatalf("down after a strike: %v, want ErrRendererUnavailable (no in-process fallback)", err)
	}
	if !e.ev.has(eventRestarting) || e.ev.has(eventFallback) {
		t.Fatal("a struck file took the in-process path")
	}
	// A file never struck still falls back while the sidecar is down.
	clean := e.writePNG("b.png", 100, 100, 2)
	if _, err := e.get(clean, 256); err != nil {
		t.Fatalf("clean file with the sidecar down: %v", err)
	}
	mode.Lock()
	mode.err = fmt.Errorf("%w: sidecar died again", ErrInterrupted)
	mode.Unlock()
	if _, err := e.get(p, 256); !errors.Is(err, ErrNotAvailable) {
		t.Fatalf("second interruption: %v, want ErrNotAvailable (poisoned)", err)
	}
	before := r.count()
	if _, err := e.get(p, 256); !errors.Is(err, ErrNotAvailable) {
		t.Fatalf("poisoned file: %v", err)
	}
	if r.count() != before {
		t.Fatal("a poisoned file was sent to the renderer again")
	}
	// Replacing the file gives it a fresh chance.
	later := time.Now().Add(time.Hour)
	if err := os.WriteFile(p, pngBytes(t, 90, 90, 5), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(p, later, later); err != nil {
		t.Fatal(err)
	}
	mode.Lock()
	mode.err = fmt.Errorf("%w: unsupported", ErrUnsupported)
	mode.Unlock()
	if _, err := e.get(p, 256); err != nil {
		t.Fatalf("changed file after poisoning: %v", err)
	}
}

func TestEvictionBySizeIsLeastRecentlyUsed(t *testing.T) {
	// Identical images in four files render to the same number of bytes.
	probe := newEnv(t, nil)
	unit := int64(len(probe.mustGet(probe.writePNG("p.png", 200, 200, 1), 256).Data))
	capacity := unit*3 + unit/2 // room for three and a half

	clock := &fakeClock{t: time.Now()}
	e := newEnv(t, func(c *Config) { c.Now = clock.Now; c.MaxCacheBytes = capacity })
	a := e.writePNG("a.png", 200, 200, 1)
	b := e.writePNG("b.png", 200, 200, 1)
	c := e.writePNG("c.png", 200, 200, 1)
	d := e.writePNG("d.png", 200, 200, 1)
	for _, p := range []string{a, b, c} {
		clock.Add(time.Minute)
		e.mustGet(p, 256)
	}
	clock.Add(time.Minute)
	if !e.mustGet(a, 256).Cached { // a is now the most recently used, b the oldest
		t.Fatal("a should be a cache hit")
	}
	clock.Add(time.Minute)
	e.mustGet(d, 256) // four entries do not fit

	files := cacheFiles(t, e.cache)
	var total int64
	for _, sz := range files {
		total += sz
	}
	if total > capacity {
		t.Fatalf("cache holds %d bytes, over its cap", total)
	}
	if _, ok := files[cacheNameOf(t, b, 256)]; ok {
		t.Fatal("the least recently used entry (b) survived eviction")
	}
	for name, p := range map[string]string{"a": a, "c": c, "d": d} {
		if _, ok := files[cacheNameOf(t, p, 256)]; !ok {
			t.Fatalf("recent entry %s was evicted", name)
		}
	}
}

func TestEvictionByAge(t *testing.T) {
	clock := &fakeClock{t: time.Now()}
	e := newEnv(t, func(c *Config) { c.Now = clock.Now; c.MaxCacheAge = time.Hour })
	a := e.writePNG("a.png", 100, 100, 1)
	b := e.writePNG("b.png", 100, 100, 2)
	c := e.writePNG("c.png", 100, 100, 3)
	e.mustGet(a, 256)
	e.mustGet(b, 256)

	clock.Add(30 * time.Minute)
	if !e.mustGet(a, 256).Cached {
		t.Fatal("entry within its age limit should hit")
	}
	clock.Add(45 * time.Minute) // a was used 45 minutes ago, b 75 minutes ago
	if e.mustGet(b, 256).Cached {
		t.Fatal("entry past its age limit was served")
	}
	clock.Add(2 * time.Hour)
	e.mustGet(c, 256) // the periodic check on this write drops everything older than the limit
	files := cacheFiles(t, e.cache)
	for name, p := range map[string]string{"a": a, "b": b} {
		if _, ok := files[cacheNameOf(t, p, 256)]; ok {
			t.Fatalf("expired entry %s is still on disk", name)
		}
	}
	if _, ok := files[cacheNameOf(t, c, 256)]; !ok {
		t.Fatal("the new entry is missing")
	}
}

func TestCacheLeavesForeignFilesAndClearsTemps(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "cache")
	if err := os.MkdirAll(filepath.Join(dir, "sub"), 0o700); err != nil {
		t.Fatal(err)
	}
	foreign := filepath.Join(dir, "notes.txt")
	temp := filepath.Join(dir, tmpPrefix+"deadbeef")
	for _, p := range []string{foreign, temp} {
		if err := os.WriteFile(p, bytes.Repeat([]byte("x"), 4096), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	e := newEnv(t, func(c *Config) { c.CacheDir = dir; c.MaxCacheBytes = 1 })
	e.mustGet(e.writePNG("a.png", 100, 100, 1), 256) // bigger than the cap: served, not stored
	e.mustGet(e.writePNG("b.png", 100, 100, 2), 256)
	if _, err := os.Stat(foreign); err != nil {
		t.Fatalf("a foreign file was removed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "sub")); err != nil {
		t.Fatalf("a folder was removed: %v", err)
	}
	if _, err := os.Stat(temp); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("a half-written file survived start-up: %v", err)
	}
	if n := len(cacheFiles(t, dir)); n != 0 {
		t.Fatalf("%d entries stored although each exceeds the cap", n)
	}
}

func TestCacheIgnoresSymlinkedEntry(t *testing.T) {
	e := newEnv(t, nil)
	p := e.writePNG("a.png", 100, 100, 1)
	name := cacheNameOf(t, p, 256)
	decoy := filepath.Join(t.TempDir(), "decoy.jpg")
	if err := os.WriteFile(decoy, []byte("DECOY"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(decoy, filepath.Join(e.cache, name)); err != nil {
		t.Skipf("cannot create symlinks here: %v", err)
	}
	res := e.mustGet(p, 256)
	if bytes.Contains(res.Data, []byte("DECOY")) || res.Cached {
		t.Fatal("a symlink planted in the cache dir was served")
	}
}
