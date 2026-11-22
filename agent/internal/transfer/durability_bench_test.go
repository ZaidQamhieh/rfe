package transfer

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/ZaidQamhieh/rfe/agent/internal/store"
)

// benchManager is setupManager for a benchmark.
func benchManager(b *testing.B) *Manager {
	b.Helper()
	dataDir := b.TempDir()
	db, err := store.Open(dataDir)
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { db.Close() })
	tm, err := New(db, filepath.Join(dataDir, "tmp"))
	if err != nil {
		b.Fatal(err)
	}
	return tm
}

// BenchmarkUploadChunks256MiB measures the chunk path of a 256 MiB upload in 4 MiB chunks (the mobile client's
// size) with 1 and 3 chunks in flight. "legacy" reproduces the old behaviour (sparse Truncate at open, no data
// sync); "durable" is the shipped path (fallocate at open, fdatasync once per batch, chunk recorded afterwards).
// Run on a real disk by pointing TMPDIR at it: a tmpfs makes the sync free and hides the cost.
func BenchmarkUploadChunks256MiB(b *testing.B) {
	const (
		chunkSize = 4 << 20
		total     = 256 << 20
		chunks    = total / chunkSize
	)
	data := make([]byte, chunkSize)
	for i := range data {
		data[i] = byte(i * 31)
	}
	sum := sha256hex(data)

	for _, mode := range []string{"legacy", "durable"} {
		for _, parallel := range []int{1, 3} {
			b.Run(fmt.Sprintf("%s/parallel=%d", mode, parallel), func(b *testing.B) {
				if mode == "legacy" {
					oldSync, oldReserve := syncData, reserveSpace
					syncData = func(*os.File) error { return nil }
					reserveSpace = func(f *os.File, size int64) error { return f.Truncate(size) }
					b.Cleanup(func() { syncData, reserveSpace = oldSync, oldReserve })
				}
				tm := benchManager(b)
				b.SetBytes(total)
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					id := fmt.Sprintf("bench-%s-%d-%d", mode, parallel, i)
					if _, err := tm.OpenSession(id, filepath.Join(b.TempDir(), "out.bin"), total, chunkSize, sum, false, "dev"); err != nil {
						b.Fatal(err)
					}
					next := make(chan int, chunks)
					for n := 0; n < chunks; n++ {
						next <- n
					}
					close(next)
					var wg sync.WaitGroup
					for w := 0; w < parallel; w++ {
						wg.Add(1)
						go func() {
							defer wg.Done()
							for n := range next {
								if err := tm.WriteChunk(id, n, data, sum); err != nil {
									b.Error(err)
									return
								}
							}
						}()
					}
					wg.Wait()
					b.StopTimer()
					if err := tm.DeleteSession(id); err != nil {
						b.Fatal(err)
					}
					b.StartTimer()
				}
			})
		}
	}
}
