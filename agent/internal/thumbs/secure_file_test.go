package thumbs

import (
	"bytes"
	"context"
	"image/color"
	"image/jpeg"
	"os"
	"path/filepath"
	"testing"
)

func TestGetFileContextRendersOpenedFileAfterPathReplacement(t *testing.T) {
	dir := t.TempDir()
	sourcePath := filepath.Join(dir, "source.png")
	movedPath := filepath.Join(dir, "opened-source.png")
	writeTestPNG(t, sourcePath, 24, 24, color.NRGBA{R: 240, G: 20, B: 10, A: 255})

	opened, err := os.Open(sourcePath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(sourcePath, movedPath); err != nil {
		opened.Close()
		t.Skipf("cannot rename an open source on this platform: %v", err)
	}
	writeTestPNG(t, sourcePath, 24, 24, color.NRGBA{B: 240, G: 20, R: 10, A: 255})

	renderer, err := New(filepath.Join(dir, "cache"))
	if err != nil {
		t.Fatal(err)
	}
	data, err := renderer.GetFileContext(context.Background(), opened, sourcePath, 24)
	if err != nil {
		t.Fatalf("render opened source: %v", err)
	}
	if _, err := opened.Stat(); err == nil {
		t.Fatal("GetFileContext did not consume and close its file handle")
	}

	decoded, err := jpeg.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("decode rendered JPEG: %v", err)
	}
	r, g, b, _ := decoded.At(12, 12).RGBA()
	if r <= b {
		t.Fatalf("render followed the replaced path instead of the opened file: center RGB=(%d,%d,%d)", r>>8, g>>8, b>>8)
	}

	// Cache hits also consume the supplied descriptor without starting another
	// path-based read.
	second, err := os.Open(movedPath)
	if err != nil {
		t.Fatal(err)
	}
	data2, err := renderer.GetFileContext(context.Background(), second, sourcePath, 24)
	if err != nil || !bytes.Equal(data, data2) {
		t.Fatalf("cache hit = (%v, bytes equal=%v)", err, bytes.Equal(data, data2))
	}
	if _, err := second.Stat(); err == nil {
		t.Fatal("cache hit did not consume and close its file handle")
	}
}
