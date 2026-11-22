package fsops

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func BenchmarkListDir20k(b *testing.B) {
	dir := b.TempDir()
	for i := 0; i < 20000; i++ {
		os.WriteFile(filepath.Join(dir, fmt.Sprintf("f%06d.txt", i)), nil, 0o644)
	}
	o := New([]string{dir}, false)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := o.ListDir(dir, "", 200); err != nil {
			b.Fatal(err)
		}
	}
}
