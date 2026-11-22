//go:build !windows

package fsops

import (
	"errors"
	"net"
	"os"
	"path/filepath"
	"testing"
)

// P2-8: Checksum of a unix socket is refused with ErrNotRegular, not a raw ENXIO.
func TestPass2ChecksumSocketNotRegular(t *testing.T) {
	dir, err := os.MkdirTemp("", "rfe-sock")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	sock := filepath.Join(dir, "s.sock")
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Skipf("cannot create a unix socket: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	ops := New([]string{dir}, false)
	if _, err := ops.Checksum(sock, "sha256"); !errors.Is(err, ErrNotRegular) {
		t.Errorf("Checksum(socket) err = %v, want ErrNotRegular", err)
	}
}
