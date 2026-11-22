package fsops

// Spec tests (QA2) for the download and integrity engine: FR-29 (partial, the
// bytes that GET /content returns) and FR-31 (GET /fs/checksum with sha256).

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestQA2SpecFR31_ChecksumIsHexSHA256 checks the sha256 checksum value.
func TestQA2SpecFR31_ChecksumIsHexSHA256(t *testing.T) {
	// FR-31: "Given a file with known bytes, when GET /fs/checksum?path=<file> is sent, then the checksum equals the hex SHA-256 of those bytes."
	// The algo default is applied by the route; here algo is passed explicitly as sha256.
	// catches: a different algorithm or encoding is returned.
	ops, root := setupJail(t)
	path := filepath.Join(root, "sum.bin")
	content := "checksum payload"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte(content))
	want := hex.EncodeToString(sum[:])

	got, err := ops.Checksum(path, "sha256")
	if err != nil {
		t.Fatalf("Checksum: %v", err)
	}
	if !strings.EqualFold(got, want) {
		t.Errorf("Checksum = %q, want hex SHA-256 %q", got, want)
	}
}

// TestQA2SpecFR29_OpenReturnsFileBytesUnchanged checks the bytes the content read returns.
func TestQA2SpecFR29_OpenReturnsFileBytesUnchanged(t *testing.T) {
	// FR-29 (partial): "Given a file, when GET /content?path=<file> is sent without Range or gzip, then the response is 200 and the body equals the file's bytes."
	// Only the engine side is tested: Ops.Open returns the file's bytes and leaves the file unchanged.
	// The 206 Range response, the gzip rule and the 200 status are HTTP-level and are not tested here.
	// catches: the read returns other bytes, or changes the file.
	ops, root := setupJail(t)
	path := filepath.Join(root, "download.txt")
	content := "download me"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	f, err := ops.Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	b, err := io.ReadAll(f)
	_ = f.Close()
	if err != nil {
		t.Fatalf("read opened file: %v", err)
	}
	if string(b) != content {
		t.Errorf("bytes read = %q, want %q", string(b), content)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != content {
		t.Errorf("file changed by the read: %q", string(after))
	}
}
