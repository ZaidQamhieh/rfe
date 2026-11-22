package sidecar

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// rustConst reads the integer value of `const NAME` from a Rust source file, so a limit the Go agent and a Rust
// sidecar must keep equal is checked against the real source instead of a second copy in a test.
func rustConst(t *testing.T, rel, name string) int64 {
	t.Helper()
	src, err := os.ReadFile(filepath.Join("..", "..", "..", "agent-rs", "crates", filepath.FromSlash(rel)))
	if err != nil {
		t.Skipf("Rust sources not available: %v", err)
	}
	m := regexp.MustCompile(`const ` + name + `: [a-z0-9]+ = ([0-9_]+)(?: << ([0-9]+))?;`).FindStringSubmatch(string(src))
	if m == nil {
		t.Fatalf("%s: no integer const %s", rel, name)
	}
	v, err := strconv.ParseInt(strings.ReplaceAll(m[1], "_", ""), 10, 64)
	if err != nil {
		t.Fatal(err)
	}
	if m[2] != "" {
		shift, _ := strconv.Atoi(m[2])
		v <<= shift
	}
	return v
}

// The frame cap is part of the wire protocol: a frame one side accepts and the other refuses breaks the stream.
func TestFrameCapMatchesRustSidecars(t *testing.T) {
	if got := rustConst(t, "rfe-proto/src/lib.rs", "MAX_FRAME_BYTES"); got != maxFrameBytes {
		t.Fatalf("Go maxFrameBytes = %d, Rust MAX_FRAME_BYTES = %d", maxFrameBytes, got)
	}
}
