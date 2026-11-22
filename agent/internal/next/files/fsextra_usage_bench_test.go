package files

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/ZaidQamhieh/rfe/agent/internal/next/rootfs"
)

// BenchmarkUsage20k sizes a tree of 20 folders with 1000 files each.
func BenchmarkUsage20k(b *testing.B) {
	root := b.TempDir()
	set, err := rootfs.New([]string{root}, rootfs.Options{})
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { _ = set.Close() })
	svc := New(set)
	for d := 0; d < 20; d++ {
		dir := filepath.Join(root, fmt.Sprintf("d%02d", d))
		if err := os.MkdirAll(dir, 0o755); err != nil {
			b.Fatal(err)
		}
		for f := 0; f < 1000; f++ {
			if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("f%04d", f)), []byte("x"), 0o644); err != nil {
				b.Fatal(err)
			}
		}
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := svc.Usage(context.Background(), root); err != nil {
			b.Fatal(err)
		}
	}
}
