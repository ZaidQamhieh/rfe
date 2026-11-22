package server

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"testing"

	"github.com/ZaidQamhieh/rfe/agent/internal/fsops"
)

// BenchmarkLiveSearch20k walks 20 folders of 1000 files for a name that matches 20 of them.
func BenchmarkLiveSearch20k(b *testing.B) {
	root := b.TempDir()
	for d := 0; d < 20; d++ {
		dir := filepath.Join(root, fmt.Sprintf("dir%02d", d))
		if err := os.MkdirAll(dir, 0o755); err != nil {
			b.Fatal(err)
		}
		for f := 0; f < 1000; f++ {
			name := fmt.Sprintf("File_%04d.txt", f)
			if f == 7 {
				name = "NeedleHere.txt"
			}
			if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o644); err != nil {
				b.Fatal(err)
			}
		}
	}
	ops := fsops.New([]string{root}, false)
	filters, _, _ := parseSearchFilters(url.Values{"q": {"needle"}})
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var results []fsops.Entry
		var hit bool
		walkForMatches(context.Background(), ops, root, filters, 100, &results, &hit)
		if len(results) != 20 {
			b.Fatalf("got %d results", len(results))
		}
	}
}
