//go:build linux || darwin

package transfer

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/ZaidQamhieh/rfe/agent/internal/fsops"
	"github.com/ZaidQamhieh/rfe/agent/internal/store"
)

// benchOutEnv names the file TestUploadCompleteCost256MiB appends its numbers to. The test is a measurement,
// not a regression check, so it only runs when the variable is set (it writes 256 MiB twice over).
const benchOutEnv = "RFE_H6_BENCH_OUT"

// freeBytes returns the bytes available to unprivileged writers on the filesystem holding dir.
func freeBytes(t *testing.T, dir string) int64 {
	t.Helper()
	var st syscall.Statfs_t
	if err := syscall.Statfs(dir, &st); err != nil {
		t.Fatal(err)
	}
	return int64(st.Bavail) * int64(st.Bsize)
}

// TestUploadCompleteCost256MiB uploads a 256 MiB file in 4 MiB in-order chunks, completes it through the same
// publish call the HTTP handler makes, and records how long Complete took and how much disk the whole upload
// used at its peak (sampled every few milliseconds from the filesystem's free space), so a change to the
// complete path can be compared before and after. Run with RFE_H6_BENCH_OUT=<file> and TMPDIR on the disk
// under test.
func TestUploadCompleteCost256MiB(t *testing.T) {
	outPath := os.Getenv(benchOutEnv)
	if outPath == "" {
		t.Skip(benchOutEnv + " not set")
	}
	const (
		chunkSize = 4 << 20
		total     = 256 << 20
		chunks    = total / chunkSize
	)
	root := t.TempDir()
	dataDir := filepath.Join(root, "data")
	destDir := filepath.Join(root, "dest")
	for _, d := range []string{dataDir, destDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	db, err := store.Open(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	tm, err := New(db, filepath.Join(dataDir, "tmp"))
	if err != nil {
		t.Fatal(err)
	}
	ops := fsops.New([]string{root}, false)

	data := make([]byte, chunkSize)
	for i := range data {
		data[i] = byte(i * 31)
	}
	whole := sha256.New()
	for i := 0; i < chunks; i++ {
		whole.Write(data)
	}
	wholeHex := hex.EncodeToString(whole.Sum(nil))
	chunkHex := sha256hex(data)

	base := freeBytes(t, root)
	var (
		mu   sync.Mutex
		peak int64
	)
	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			used := base - freeBytes(t, root)
			mu.Lock()
			if used > peak {
				peak = used
			}
			mu.Unlock()
			select {
			case <-stop:
				return
			case <-time.After(5 * time.Millisecond):
			}
		}
	}()

	target := filepath.Join(destDir, "big.bin")
	uploadStart := time.Now()
	if _, err := tm.OpenSession("h6-bench", target, total, chunkSize, wholeHex, false, "dev"); err != nil {
		t.Fatal(err)
	}
	for n := 0; n < chunks; n++ {
		if err := tm.WriteChunk("h6-bench", n, data, chunkHex); err != nil {
			t.Fatal(err)
		}
	}
	uploadDur := time.Since(uploadStart)

	completeStart := time.Now()
	if _, _, err := tm.CompleteWithPublisher("h6-bench", func(path string, source io.Reader, overwrite bool) (os.FileInfo, error) {
		return ops.PublishReaderWith(path, source, overwrite, tm.PublishOptions("h6-bench"))
	}); err != nil {
		t.Fatal(err)
	}
	completeDur := time.Since(completeStart)
	close(stop)
	<-done

	mu.Lock()
	defer mu.Unlock()
	line := fmt.Sprintf("fs=%s size=%dMiB chunk=%dMiB upload_chunks=%s complete=%s peak_disk_used=%dMiB (%.2fx file size)\n",
		os.Getenv("TMPDIR"), total>>20, chunkSize>>20, uploadDur.Round(time.Millisecond), completeDur.Round(time.Millisecond),
		peak>>20, float64(peak)/float64(total))
	f, err := os.OpenFile(outPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := f.WriteString(line); err != nil {
		t.Fatal(err)
	}
	t.Log(line)
}
