package thumbs

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// rustConst reads the integer value of `const NAME` from a Rust sidecar source file.
func rustConst(t *testing.T, rel, name string) int64 {
	t.Helper()
	src, err := os.ReadFile(filepath.Join("..", "..", "..", "agent-rs", "crates", filepath.FromSlash(rel)))
	if err != nil {
		t.Skipf("Rust sources not available: %v", err)
	}
	m := regexp.MustCompile(`const ` + name + `: [a-z0-9]+ = ([0-9_]+);`).FindStringSubmatch(string(src))
	if m == nil {
		t.Fatalf("%s: no integer const %s", rel, name)
	}
	v, err := strconv.ParseInt(strings.ReplaceAll(m[1], "_", ""), 10, 64)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

// The Rust renderer is checked against the Go one, so these must stay equal or the two give different thumbnails
// and different "too large" answers.
func TestLimitsMatchRustThumbd(t *testing.T) {
	if got := rustConst(t, "rfe-thumbd/src/decode.rs", "MAX_PIXELS"); got != maxThumbPixels {
		t.Errorf("Go maxThumbPixels = %d, Rust MAX_PIXELS = %d", maxThumbPixels, got)
	}
	if got := rustConst(t, "rfe-thumbd/src/render.rs", "JPEG_QUALITY"); got != jpegQuality {
		t.Errorf("Go jpegQuality = %d, Rust JPEG_QUALITY = %d", jpegQuality, got)
	}
}
