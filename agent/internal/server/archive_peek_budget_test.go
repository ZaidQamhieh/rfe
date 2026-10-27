package server

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// Audit F-94: listing a .tar.gz must not inflate without bound.
func TestPeekTarGzStopsAtInflateBudget(t *testing.T) {
	var raw bytes.Buffer
	gz := gzip.NewWriter(&raw)
	tw := tar.NewWriter(gz)
	big := make([]byte, 1<<20) // compresses to almost nothing
	_ = tw.WriteHeader(&tar.Header{Name: "big.bin", Mode: 0o644, Size: int64(len(big)), Typeflag: tar.TypeReg})
	_, _ = tw.Write(big)
	_ = tw.WriteHeader(&tar.Header{Name: "after.txt", Mode: 0o644, Size: 1, Typeflag: tar.TypeReg})
	_, _ = tw.Write([]byte("x"))
	_ = tw.Close()
	_ = gz.Close()
	path := filepath.Join(t.TempDir(), "a.tar.gz")
	if err := os.WriteFile(path, raw.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	old := peekInflateBudget
	peekInflateBudget = 64 << 10
	defer func() { peekInflateBudget = old }()
	if _, err := peekTarGzFile(f, 100); !errors.Is(err, errPeekTooLarge) {
		t.Fatalf("err = %v, want errPeekTooLarge", err)
	}
}
