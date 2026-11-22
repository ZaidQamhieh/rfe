package thumbs

import (
	"image/color"
	"os"
	"path/filepath"
	"testing"
)

// The in-process path encodes the JPEG in memory. It must not need a writable system temp directory, which it
// used to create, write and read back for every cache miss.
func TestInProcessRenderDoesNotUseTheSystemTempDir(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "no-such-tmp")
	for _, key := range []string{"TMPDIR", "TMP", "TEMP"} {
		t.Setenv(key, missing)
	}
	src := filepath.Join(t.TempDir(), "p.png")
	writeTestPNG(t, src, 40, 20, color.NRGBA{R: 200, G: 30, B: 30, A: 255})
	rn, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	data, err := rn.Get(src, 16)
	if err != nil {
		t.Fatalf("Get with an unusable temp dir: %v", err)
	}
	if len(data) < 4 || data[0] != 0xff || data[1] != 0xd8 {
		t.Fatalf("not a JPEG: % x", data[:min(4, len(data))])
	}
	if _, err := os.Stat(missing); !os.IsNotExist(err) {
		t.Errorf("the render created the temp directory path: %v", err)
	}
}
