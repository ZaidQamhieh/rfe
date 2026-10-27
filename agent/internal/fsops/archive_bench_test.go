package fsops

import (
	"compress/flate"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"time"

	kflate "github.com/klauspost/compress/flate"
)

type nopCounter struct{ n int64 }

func (c *nopCounter) Write(p []byte) (int, error) { c.n += int64(len(p)); return len(p), nil }

// TestZipCompressorSpeed compares the standard library's deflate with klauspost/compress on a real directory.
// It only runs when RFE_BENCH_DIR is set:
//
//	RFE_BENCH_DIR=$HOME/go-sdk go test ./internal/fsops -run TestZipCompressorSpeed -v
func TestZipCompressorSpeed(t *testing.T) {
	dir := os.Getenv("RFE_BENCH_DIR")
	if dir == "" {
		t.Skip("RFE_BENCH_DIR not set")
	}
	var files []string
	var total int64
	_ = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err == nil && d.Type().IsRegular() {
			if info, err := d.Info(); err == nil && info.Size() < 64<<20 {
				files = append(files, p)
				total += info.Size()
			}
		}
		if total > 1<<30 {
			return filepath.SkipAll
		}
		return nil
	})
	t.Logf("corpus: %d files, %d MB", len(files), total>>20)

	run := func(name string, mk func(io.Writer) (io.WriteCloser, error)) {
		var out nopCounter
		start := time.Now()
		var in int64
		for _, f := range files {
			w, err := mk(&out) // one deflate stream per file, like zip
			if err != nil {
				t.Fatal(err)
			}
			r, err := os.Open(f)
			if err != nil {
				continue
			}
			n, _ := io.Copy(w, r)
			in += n
			r.Close()
			w.Close()
		}
		el := time.Since(start)
		t.Logf("%-22s %6.1f MB/s  out %5.1f%% of in  (%v)", name, float64(in)/1e6/el.Seconds(), 100*float64(out.n)/float64(in), el.Round(time.Millisecond))
	}
	// Warm the page cache so disk speed is not what is measured.
	for _, f := range files {
		if b, err := os.ReadFile(f); err == nil {
			_ = b
		}
	}
	run("std level 6 (today)", func(w io.Writer) (io.WriteCloser, error) { return flate.NewWriter(w, flate.DefaultCompression) })
	run("std level 1", func(w io.Writer) (io.WriteCloser, error) { return flate.NewWriter(w, flate.BestSpeed) })
	run("klauspost level 6", func(w io.Writer) (io.WriteCloser, error) { return kflate.NewWriter(w, 6) })
	run("klauspost level 5", func(w io.Writer) (io.WriteCloser, error) { return kflate.NewWriter(w, 5) })
	run("klauspost level 1", func(w io.Writer) (io.WriteCloser, error) { return kflate.NewWriter(w, 1) })
	run("klauspost level 2", func(w io.Writer) (io.WriteCloser, error) { return kflate.NewWriter(w, 2) })
}
