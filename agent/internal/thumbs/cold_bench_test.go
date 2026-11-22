package thumbs

import (
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

// BenchmarkColdThumbs renders 32 distinct 2000x1500 JPEGs into an empty cache from 16 callers at once: the
// cold-grid case the decode cap (maxConcurrentDecodes) governs. ns/op is the time for the whole batch.
func BenchmarkColdThumbs(b *testing.B) { benchCold(b, false) }

// BenchmarkColdThumbsSidecar is the same batch rendered by rfe-thumbd (skipped when it is not built).
func BenchmarkColdThumbsSidecar(b *testing.B) { benchCold(b, true) }

func benchCold(b *testing.B, sidecarOn bool) {
	src := b.TempDir()
	bw, bh := 2000, 1500
	if os.Getenv("RFE_BENCH_BIG") != "" {
		bw, bh = 4000, 3000 // 12 MP, a phone photo
	}
	var remote SidecarRemote
	if sidecarOn {
		remote = newRemote(b)
	}
	const n = 32
	for i := 0; i < n; i++ {
		img := image.NewRGBA(image.Rect(0, 0, bw, bh))
		for y := 0; y < bh; y += 3 {
			for x := 0; x < bw; x++ {
				img.Set(x, y, color.RGBA{uint8(x + i), uint8(y), uint8(x ^ y), 255})
			}
		}
		f, err := os.Create(filepath.Join(src, fmt.Sprintf("p%d.jpg", i)))
		if err != nil {
			b.Fatal(err)
		}
		if err := jpeg.Encode(f, img, &jpeg.Options{Quality: 85}); err != nil {
			b.Fatal(err)
		}
		f.Close()
	}
	b.ResetTimer()
	for it := 0; it < b.N; it++ {
		rn, err := New(b.TempDir())
		if err != nil {
			b.Fatal(err)
		}
		if sidecarOn {
			rn.UseRemote(remote)
		}
		var wg sync.WaitGroup
		work := make(chan int, n)
		for i := 0; i < n; i++ {
			work <- i
		}
		close(work)
		for w := 0; w < 16; w++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for i := range work {
					if _, err := rn.Get(filepath.Join(src, fmt.Sprintf("p%d.jpg", i)), 256); err != nil {
						b.Error(err)
					}
				}
			}()
		}
		wg.Wait()
	}
}
