package thumbs

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/ZaidQamhieh/rfe/agent/internal/sidecar"
)

func thumbdBinary(t *testing.T) string {
	t.Helper()
	name := "rfe-thumbd"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	dirs := []string{os.Getenv("RFE_SIDECAR_DIR")}
	for _, sub := range []string{"release", "debug"} {
		dirs = append(dirs, filepath.Join("..", "..", "..", "agent-rs", "target", sub))
	}
	for _, d := range dirs {
		if d == "" {
			continue
		}
		p := filepath.Join(d, name)
		if st, err := os.Stat(p); err == nil && st.Mode().IsRegular() {
			return p
		}
	}
	t.Skip("rfe-thumbd is not built")
	return ""
}

func newRemote(t *testing.T) SidecarRemote {
	t.Helper()
	sup := sidecar.NewSupervisor(sidecar.Config{Name: "rfe-thumbd", Path: thumbdBinary(t), Logf: t.Logf}, nil)
	sup.Run()
	t.Cleanup(sup.Stop)
	deadline := time.Now().Add(10 * time.Second)
	for {
		if _, err := sup.Client(); err == nil {
			return SidecarRemote{Sup: sup}
		}
		if time.Now().After(deadline) {
			t.Fatal("rfe-thumbd did not start")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

const fixtureDir = "../../../agent-rs/crates/rfe-thumbd/tests/data"

func decodeJPEG(t *testing.T, b []byte) image.Image {
	t.Helper()
	img, err := jpeg.Decode(bytes.NewReader(b))
	if err != nil {
		t.Fatalf("not a JPEG: %v", err)
	}
	return img
}

// psnr is the peak signal-to-noise ratio of two same-size images over RGB, in dB (higher is closer).
func psnr(a, b image.Image) float64 {
	r := a.Bounds()
	var sum float64
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			ar, ag, ab, _ := a.At(x, y).RGBA()
			br, bg, bb, _ := b.At(x, y).RGBA()
			for _, d := range [3]float64{float64(ar>>8) - float64(br>>8), float64(ag>>8) - float64(bg>>8), float64(ab>>8) - float64(bb>>8)} {
				sum += d * d
			}
		}
	}
	mse := sum / float64(r.Dx()*r.Dy()*3)
	if mse == 0 {
		return 99
	}
	return 10 * math.Log10(255*255/mse)
}

// bigPhoto is a 4000x3000 gradient with fine texture, the kind of source where scaling quality shows.
func bigPhoto() []byte {
	w, h := 4000, 3000
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			n := uint8((x*7 + y*13) % 23)
			img.SetRGBA(x, y, color.RGBA{R: uint8(x*255/w) ^ n, G: uint8(y*255/h) + n/2, B: uint8((x + y) * 255 / (w + h)), A: 255})
		}
	}
	var buf bytes.Buffer
	_ = jpeg.Encode(&buf, img, &jpeg.Options{Quality: 88})
	return buf.Bytes()
}

// TestSidecarMatchesGoRenderer renders the same sources with the built-in decoder and with rfe-thumbd: sizes must be
// identical (so cached thumbnails and layouts do not change) and the pictures must be near-identical.
func TestSidecarMatchesGoRenderer(t *testing.T) {
	remote := newRemote(t)
	dir := t.TempDir()
	big := filepath.Join(dir, "big.jpg")
	if err := os.WriteFile(big, bigPhoto(), 0o644); err != nil {
		t.Fatal(err)
	}
	type tc struct {
		path    string
		max     int
		minPSNR float64
	}
	cases := []tc{
		{filepath.Join(fixtureDir, "photo.jpg"), 128, 30},
		{filepath.Join(fixtureDir, "photo.jpg"), 1024, 35},
		{filepath.Join(fixtureDir, "orient6.jpg"), 128, 30},
		{filepath.Join(fixtureDir, "orient3.jpg"), 128, 30},
		{filepath.Join(fixtureDir, "gray.jpg"), 128, 30},
		{filepath.Join(fixtureDir, "cmyk.jpg"), 128, 28},
		{filepath.Join(fixtureDir, "progressive.jpg"), 128, 30},
		{filepath.Join(fixtureDir, "alpha.png"), 100, 30},
		{filepath.Join(fixtureDir, "palette.png"), 128, 30},
		{filepath.Join(fixtureDir, "gray16.png"), 64, 30},
		{filepath.Join(fixtureDir, "anim.gif"), 64, 27},
		{filepath.Join(fixtureDir, "photo.tiff"), 128, 30},
		{filepath.Join(fixtureDir, "deflate.tiff"), 128, 30},
		{filepath.Join(fixtureDir, "raw.tiff"), 128, 30},
		{filepath.Join(fixtureDir, "gray.tiff"), 128, 30},
		{filepath.Join(fixtureDir, "rgba.tiff"), 128, 30},
		{filepath.Join(fixtureDir, "photo.bmp"), 128, 30},
		{filepath.Join(fixtureDir, "pal.bmp"), 128, 30},
		{filepath.Join(fixtureDir, "bits32.bmp"), 128, 30},
		{filepath.Join(fixtureDir, "topdown.bmp"), 128, 30},
		{big, 256, 30},
		{big, 1024, 30},
	}
	for _, c := range cases {
		t.Run(fmt.Sprintf("%s@%d", filepath.Base(c.path), c.max), func(t *testing.T) {
			want, err := Render(c.path, c.max)
			if err != nil {
				t.Fatalf("built-in renderer: %v", err)
			}
			src, err := os.ReadFile(c.path)
			if err != nil {
				t.Fatal(err)
			}
			got, err := remote.Render(context.Background(), src, c.max)
			if err != nil {
				t.Fatalf("sidecar: %v", err)
			}
			a, b := decodeJPEG(t, want), decodeJPEG(t, got)
			if a.Bounds() != b.Bounds() {
				t.Fatalf("size: built-in %v, sidecar %v", a.Bounds(), b.Bounds())
			}
			p := psnr(a, b)
			t.Logf("PSNR %.1f dB, %d bytes (built-in %d)", p, len(got), len(want))
			if p < c.minPSNR {
				t.Errorf("PSNR %.1f dB below %.1f", p, c.minPSNR)
			}
			if len(got) > len(want)*3/2+2048 {
				t.Errorf("sidecar thumbnail is %d bytes, built-in %d", len(got), len(want))
			}
		})
	}
}

func TestSidecarHandlesWhatGoCannotAndRefusesWhatNobodyCan(t *testing.T) {
	remote := newRemote(t)
	webp, err := os.ReadFile(filepath.Join(fixtureDir, "lossy.webp"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Render(filepath.Join(fixtureDir, "lossy.webp"), 128); err == nil {
		t.Fatal("expected the built-in renderer to refuse WebP")
	}
	out, err := remote.Render(context.Background(), webp, 128)
	if err != nil {
		t.Fatalf("sidecar WebP: %v", err)
	}
	if b := decodeJPEG(t, out).Bounds(); b.Dx() != 128 || b.Dy() != 96 {
		t.Fatalf("webp thumbnail is %v", b)
	}
	junk, _ := os.ReadFile(filepath.Join(fixtureDir, "notimage.bin"))
	if _, err := remote.Render(context.Background(), junk, 128); err != errRemoteUnsupported {
		t.Fatalf("junk: %v", err)
	}
}

// TestRendererFallsBackWhenTheSidecarIsGone checks the renderer still answers, with the built-in decoder, when the
// sidecar is dead, and still reports ErrNotSupported for what nothing can decode.
func TestRendererFallsBackWhenTheSidecarIsGone(t *testing.T) {
	sup := sidecar.NewSupervisor(sidecar.Config{Name: "rfe-thumbd", Path: "/nonexistent/rfe-thumbd"}, nil)
	sup.Run()
	defer sup.Stop()
	rn, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	rn.UseRemote(SidecarRemote{Sup: sup})
	data, err := rn.Get(filepath.Join(fixtureDir, "photo.jpg"), 128)
	if err != nil || len(data) == 0 {
		t.Fatalf("fallback render failed: %v", err)
	}
	if _, err := rn.Get(filepath.Join(fixtureDir, "notimage.bin"), 128); err == nil {
		t.Fatal("expected an error for a non-image")
	}
}

func TestRendererUsesTheSidecarAndSurvivesItsDeath(t *testing.T) {
	remote := newRemote(t)
	rn, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	rn.UseRemote(remote)
	path := filepath.Join(fixtureDir, "lossy.webp")
	if _, err := rn.Get(path, 96); err != nil {
		t.Fatalf("WebP through the renderer: %v (only the sidecar can decode it)", err)
	}
	// Kill the process; the next request falls back (WebP then fails, JPEG still works).
	c, err := remote.Sup.Client()
	if err != nil {
		t.Fatal(err)
	}
	p, _ := os.FindProcess(c.Pid())
	_ = p.Kill()
	time.Sleep(50 * time.Millisecond)
	if _, err := rn.Get(filepath.Join(fixtureDir, "photo.jpg"), 96); err != nil {
		t.Fatalf("JPEG after the sidecar died: %v", err)
	}
}

// TestBenchThumbs measures both renderers on real photos: RFE_BENCH_DIR=<dir with photos> RFE_BENCH_IMPL=go|rust.
// Four renders run at a time, like the server allows.
func TestBenchThumbs(t *testing.T) {
	dir := os.Getenv("RFE_BENCH_DIR")
	if dir == "" {
		t.Skip("RFE_BENCH_DIR not set")
	}
	var files []string
	_ = filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err == nil && d.Type().IsRegular() {
			switch filepath.Ext(p) {
			case ".jpg", ".jpeg", ".JPG", ".png":
				if st, e := d.Info(); e == nil && st.Size() > 300<<10 && len(files) < 80 {
					files = append(files, p)
				}
			}
		}
		return nil
	})
	if len(files) == 0 {
		t.Skip("no photos")
	}
	var remote Remote
	if os.Getenv("RFE_BENCH_IMPL") == "rust" {
		remote = newRemote(t)
	}
	sem := make(chan struct{}, 4)
	times := make([]time.Duration, len(files))
	var total int64
	done := make(chan struct{})
	start := time.Now()
	for i, p := range files {
		sem <- struct{}{}
		go func(i int, p string) {
			defer func() { <-sem; done <- struct{}{} }()
			s := time.Now()
			if remote != nil {
				src, _ := os.ReadFile(p)
				if _, err := remote.Render(context.Background(), src, 256); err != nil {
					t.Errorf("%s: %v", p, err)
				}
			} else if _, err := Render(p, 256); err != nil {
				t.Errorf("%s: %v", p, err)
			}
			times[i] = time.Since(s)
		}(i, p)
	}
	for range files {
		<-done
	}
	elapsed := time.Since(start)
	for _, p := range files {
		if st, err := os.Stat(p); err == nil {
			total += st.Size()
		}
	}
	sorted := append([]time.Duration(nil), times...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	rss := func(pid string) int64 {
		b, _ := os.ReadFile("/proc/" + pid + "/status")
		for _, l := range strings.Split(string(b), "\n") {
			if strings.HasPrefix(l, "VmHWM:") {
				var kb int64
				fmt.Sscanf(strings.TrimSpace(strings.TrimPrefix(l, "VmHWM:")), "%d", &kb)
				return kb
			}
		}
		return 0
	}
	side := int64(0)
	if r, ok := remote.(SidecarRemote); ok {
		if c, err := r.Sup.Client(); err == nil {
			side = rss(fmt.Sprint(c.Pid()))
		}
	}
	t.Logf("BENCH impl=%s photos=%d (%d MB) total=%v  %.1f photos/s  median=%v p95=%v  goProcessPeakRSS=%dMB sidecarPeakRSS=%dMB",
		os.Getenv("RFE_BENCH_IMPL"), len(files), total>>20, elapsed.Round(time.Millisecond), float64(len(files))/elapsed.Seconds(),
		sorted[len(sorted)/2].Round(time.Millisecond), sorted[len(sorted)*95/100].Round(time.Millisecond), rss("self")/1024, side/1024)
}
