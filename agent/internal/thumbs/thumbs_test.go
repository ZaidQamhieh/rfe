package thumbs

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func writeTestPNG(t *testing.T, path string, width, height int, c color.NRGBA) {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			img.SetNRGBA(x, y, c)
		}
	}
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := png.Encode(file, img); err != nil {
		file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
}

func writePNGConfigHeader(t *testing.T, path string, width, height uint32) {
	t.Helper()
	var b bytes.Buffer
	b.Write([]byte("\x89PNG\r\n\x1a\n"))
	_ = binary.Write(&b, binary.BigEndian, uint32(13))
	typ := []byte("IHDR")
	b.Write(typ)
	var data bytes.Buffer
	_ = binary.Write(&data, binary.BigEndian, width)
	_ = binary.Write(&data, binary.BigEndian, height)
	data.Write([]byte{8, 2, 0, 0, 0})
	b.Write(data.Bytes())
	_ = binary.Write(&b, binary.BigEndian, crc32.ChecksumIEEE(append(typ, data.Bytes()...)))
	if err := os.WriteFile(path, b.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestRenderResizesAndRejectsUnsupportedSources(t *testing.T) {
	src := filepath.Join(t.TempDir(), "source.png")
	writeTestPNG(t, src, 80, 40, color.NRGBA{R: 210, G: 30, B: 20, A: 255})
	data, err := Render(src, 20)
	if err != nil {
		t.Fatalf("Render resized image: %v", err)
	}
	cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || format != "jpeg" || cfg.Width != 20 || cfg.Height != 10 {
		t.Fatalf("rendered thumbnail config = (%+v,%q,%v), want 20x10 JPEG", cfg, format, err)
	}
	data, err = Render(src, 0)
	if err != nil {
		t.Fatalf("Render with default size: %v", err)
	}
	cfg, err = jpeg.DecodeConfig(bytes.NewReader(data))
	if err != nil || cfg.Width != 80 || cfg.Height != 40 {
		t.Fatalf("default-size render = (%+v,%v), want source-sized JPEG", cfg, err)
	}
	if _, err := Render(filepath.Join(t.TempDir(), "missing.png"), 20); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing image error = %v", err)
	}
	if _, err := Render(t.TempDir(), 20); !errors.Is(err, ErrNotSupported) {
		t.Fatalf("directory source error = %v, want ErrNotSupported", err)
	}
	textPath := filepath.Join(t.TempDir(), "not-image.txt")
	if err := os.WriteFile(textPath, []byte("not an image"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Render(textPath, 20); !errors.Is(err, ErrNotSupported) {
		t.Fatalf("invalid image error = %v, want ErrNotSupported", err)
	}
	truncatedPNG := filepath.Join(t.TempDir(), "truncated.png")
	writePNGConfigHeader(t, truncatedPNG, 10, 10)
	if _, err := Render(truncatedPNG, 20); !errors.Is(err, ErrNotSupported) {
		t.Fatalf("truncated image error = %v, want ErrNotSupported", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := render(ctx, src, 20); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled render error = %v", err)
	}
	oversized := filepath.Join(t.TempDir(), "oversized.png")
	f, err := os.Create(oversized)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(maxThumbSourceBytes + 1); err != nil {
		f.Close()
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := Render(oversized, 20); !errors.Is(err, ErrNotSupported) {
		t.Fatalf("oversized source error = %v, want ErrNotSupported", err)
	}
	tooManyPixels := filepath.Join(t.TempDir(), "too-many-pixels.png")
	writePNGConfigHeader(t, tooManyPixels, 10001, 4000)
	if _, err := Render(tooManyPixels, 20); !errors.Is(err, ErrNotSupported) {
		t.Fatalf("oversized pixel-count error = %v, want ErrNotSupported", err)
	}
}

func TestWaitForCallCompletionAndCanceledWaiters(t *testing.T) {
	renderer := &Renderer{inFlight: make(map[string]*renderCall)}
	workCtx, workCancel := context.WithCancel(context.Background())
	completed := &renderCall{done: make(chan struct{}), data: []byte("jpeg"), err: errors.New("render failed"), ctx: workCtx, cancel: workCancel, finished: true}
	close(completed.done)
	data, err := renderer.waitForCall(context.Background(), "complete", completed)
	if string(data) != "jpeg" || err == nil || err.Error() != "render failed" {
		t.Fatalf("completed render result = (%q,%v)", data, err)
	}
	workCancel()

	waitingCtx, cancelWaiting := context.WithCancel(context.Background())
	waitingWorkCtx, cancelWork := context.WithCancel(context.Background())
	call := &renderCall{done: make(chan struct{}), ctx: waitingWorkCtx, cancel: cancelWork, waiters: 2}
	renderer.inFlight["shared"] = call
	cancelWaiting()
	if _, err := renderer.waitForCall(waitingCtx, "shared", call); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled waiter error = %v", err)
	}
	if call.waiters != 1 || renderer.inFlight["shared"] != call || waitingWorkCtx.Err() != nil {
		t.Fatalf("one caller detached shared render incorrectly: waiters=%d inFlight=%v ctx=%v", call.waiters, renderer.inFlight["shared"] == call, waitingWorkCtx.Err())
	}

	lastCtx, cancelLast := context.WithCancel(context.Background())
	lastWorkCtx, cancelLastWork := context.WithCancel(context.Background())
	last := &renderCall{done: make(chan struct{}), ctx: lastWorkCtx, cancel: cancelLastWork, waiters: 1}
	renderer.inFlight["last"] = last
	cancelLast()
	if _, err := renderer.waitForCall(lastCtx, "last", last); !errors.Is(err, context.Canceled) {
		t.Fatalf("last canceled waiter error = %v", err)
	}
	if _, ok := renderer.inFlight["last"]; ok || !errors.Is(lastWorkCtx.Err(), context.Canceled) {
		t.Fatalf("last waiter did not cancel and detach work: mapped=%v ctx=%v", ok, lastWorkCtx.Err())
	}
}

func TestRendererCachesAndInvalidatesOnSourceChange(t *testing.T) {
	root := t.TempDir()
	cacheDir := filepath.Join(root, "cache")
	renderer, err := New(cacheDir)
	if err != nil {
		t.Fatalf("New renderer: %v", err)
	}
	src := filepath.Join(root, "source.png")
	writeTestPNG(t, src, 40, 20, color.NRGBA{R: 255, A: 255})
	old := time.Unix(1000, 0)
	if err := os.Chtimes(src, old, old); err != nil {
		t.Fatal(err)
	}
	first, err := renderer.Get(src, 16)
	if err != nil {
		t.Fatal(err)
	}
	second, err := renderer.GetContext(context.Background(), src, 16)
	if err != nil || !bytes.Equal(first, second) {
		t.Fatalf("cache hit = (%v, %v), bytes equal=%v", err, nil, bytes.Equal(first, second))
	}
	srcInfo, err := os.Stat(src)
	if err != nil {
		t.Fatal(err)
	}
	cachePath := renderer.cachePath(src, 16, srcInfo)
	if got, err := os.ReadFile(cachePath); err != nil || !bytes.Equal(got, first) {
		t.Fatalf("cached thumbnail = %d bytes, err %v", len(got), err)
	}
	if err := renderer.writeCache(cachePath, []byte("replacement")); err != nil {
		t.Fatal(err)
	}
	if got, err := os.ReadFile(cachePath); err != nil || string(got) != "replacement" {
		t.Fatalf("atomic cache replacement = (%q, %v)", got, err)
	}
	writeTestPNG(t, src, 40, 20, color.NRGBA{B: 255, A: 255})
	newer := old.Add(time.Hour)
	if err := os.Chtimes(src, newer, newer); err != nil {
		t.Fatal(err)
	}
	updated, err := renderer.Get(src, 16)
	if err != nil {
		t.Fatalf("render changed source version: %v", err)
	}
	if bytes.Equal(first, updated) {
		t.Fatal("changed source content produced the old cached thumbnail")
	}
	entries, err := os.ReadDir(cacheDir)
	if err != nil || len(entries) != 2 {
		t.Fatalf("cache entries after source change = %d, %v; want two versioned entries", len(entries), err)
	}
}

func TestRendererPrunesOnlyOldOwnedRegularCacheFiles(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "cache")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	oldPath := filepath.Join(dir, strings.Repeat("a", 64)+"_128_1.jpg")
	newPath := filepath.Join(dir, strings.Repeat("b", 64)+"_128_2.jpg")
	for _, path := range []string{oldPath, newPath} {
		if err := os.WriteFile(path, nil, 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Truncate(path, 300<<20); err != nil {
			t.Fatal(err)
		}
	}
	old := time.Unix(1000, 0)
	newer := old.Add(time.Hour)
	if err := os.Chtimes(oldPath, old, old); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(newPath, newer, newer); err != nil {
		t.Fatal(err)
	}
	unknown := filepath.Join(dir, "keep-me.data")
	if err := os.WriteFile(unknown, []byte("unknown"), 0o600); err != nil {
		t.Fatal(err)
	}
	linkedTarget := filepath.Join(t.TempDir(), "external.jpg")
	if err := os.WriteFile(linkedTarget, []byte("external"), 0o600); err != nil {
		t.Fatal(err)
	}
	linkPath := filepath.Join(dir, strings.Repeat("c", 64)+"_128_3.jpg")
	if err := os.Symlink(linkedTarget, linkPath); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	dirPath := filepath.Join(dir, strings.Repeat("d", 64)+"_128_4.jpg")
	if err := os.Mkdir(dirPath, 0o700); err != nil {
		t.Fatal(err)
	}
	renderer, err := New(dir)
	if err != nil {
		t.Fatalf("New renderer: %v", err)
	}
	if _, err := os.Stat(oldPath); !os.IsNotExist(err) {
		t.Fatalf("oldest owned cache file was not pruned (stat error %v)", err)
	}
	for _, path := range []string{newPath, unknown, linkPath, dirPath, linkedTarget} {
		if _, err := os.Lstat(path); err != nil {
			t.Errorf("pruning removed or followed non-owned/non-old entry %q: %v", path, err)
		}
	}
	if renderer.cacheBytes != 300<<20 {
		t.Fatalf("tracked cache bytes = %d, want %d", renderer.cacheBytes, int64(300<<20))
	}
}

func TestCacheFileRecognitionAtomicWriteAndConstructorErrors(t *testing.T) {
	valid := strings.Repeat("a", 64) + "_256_123.jpg"
	current := strings.Repeat("a", 64) + "_256_123_4096.jpg"
	if !isRendererCacheFile(current) {
		t.Errorf("the current four-part cache name %q must be recognised, or pruning never sees new files", current)
	}
	for _, name := range []string{valid, "short_256_1.jpg", strings.Repeat("g", 64) + "_256_1.jpg", strings.Repeat("a", 64) + "_x_1.jpg", strings.Repeat("a", 64) + "_1_nope.jpg", strings.Repeat("a", 64) + "_256_1.png"} {
		want := name == valid
		if got := isRendererCacheFile(name); got != want {
			t.Errorf("isRendererCacheFile(%q) = %v, want %v", name, got, want)
		}
	}
	dir := t.TempDir()
	destination := filepath.Join(dir, "result.jpg")
	if err := writeAtomic(destination, []byte("complete")); err != nil {
		t.Fatalf("writeAtomic: %v", err)
	}
	if got, err := os.ReadFile(destination); err != nil || string(got) != "complete" {
		t.Fatalf("atomic result = (%q, %v)", got, err)
	}
	if err := os.WriteFile(filepath.Join(dir, "file"), []byte("not a directory"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := New(filepath.Join(dir, "file", "child")); err == nil {
		t.Fatal("New accepted a cache path below a regular file")
	}
	filePath := filepath.Join(dir, "target-dir")
	if err := os.Mkdir(filePath, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := writeAtomic(filePath, []byte("cannot replace directory")); err == nil {
		t.Fatal("writeAtomic unexpectedly replaced a directory")
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".thumb-") {
			t.Errorf("failed atomic write left temporary file %q", entry.Name())
		}
	}
}

func waitForBudgetWaiter(t *testing.T, budget *decodeBudget) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		budget.mu.Lock()
		count := len(budget.waiters)
		budget.mu.Unlock()
		if count > 0 {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("decode budget waiter was not queued")
}

func TestDecodeBudgetLimitCancellationQueueAndRelease(t *testing.T) {
	budget := newDecodeBudget(10)
	if err := budget.acquire(context.Background(), 0); err == nil {
		t.Fatal("zero-weight reservation was accepted")
	}
	if err := budget.acquire(context.Background(), 11); err == nil {
		t.Fatal("over-budget reservation was accepted")
	}
	if err := budget.acquire(context.Background(), 8); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	started := make(chan struct{})
	result := make(chan error, 1)
	go func() {
		close(started)
		result <- budget.acquire(ctx, 5)
	}()
	<-started
	waitForBudgetWaiter(t, budget)
	cancel()
	if err := <-result; !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled queued reservation = %v", err)
	}
	budget.mu.Lock()
	used, queued := budget.used, len(budget.waiters)
	budget.mu.Unlock()
	if used != 8 || queued != 0 {
		t.Fatalf("cancellation leaked reservation: used=%d queued=%d", used, queued)
	}
	budget.release(8)

	if err := budget.acquire(context.Background(), 8); err != nil {
		t.Fatal(err)
	}
	result = make(chan error, 1)
	go func() { result <- budget.acquire(context.Background(), 5) }()
	waitForBudgetWaiter(t, budget)
	budget.release(8)
	if err := <-result; err != nil {
		t.Fatalf("queued reservation after release: %v", err)
	}
	budget.mu.Lock()
	used = budget.used
	budget.mu.Unlock()
	if used != 5 {
		t.Fatalf("granted reservation weight = %d, want 5", used)
	}
	budget.release(5)
	budget.mu.Lock()
	used = budget.used
	budget.mu.Unlock()
	if used != 0 {
		t.Fatalf("release left %d bytes reserved", used)
	}
}

// F-54: a replaced file whose modification time was restored must not be served from the old cache entry.
func TestRendererCacheKeyChangesWhenFileIsReplacedWithSameMtime(t *testing.T) {
	root := t.TempDir()
	renderer, err := New(filepath.Join(root, "cache"))
	if err != nil {
		t.Fatal(err)
	}
	src := filepath.Join(root, "a.png")
	old := time.Unix(1000, 0)
	writeTestPNG(t, src, 40, 20, color.NRGBA{R: 255, A: 255})
	if err := os.Chtimes(src, old, old); err != nil {
		t.Fatal(err)
	}
	red, err := renderer.Get(src, 16)
	if err != nil {
		t.Fatal(err)
	}
	writeTestPNG(t, src, 60, 30, color.NRGBA{B: 255, A: 255})
	if err := os.Chtimes(src, old, old); err != nil {
		t.Fatal(err)
	}
	blue, err := renderer.Get(src, 16)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(red, blue) {
		t.Fatal("a different file with the same name and mtime was served from the cache")
	}
}
