//go:build linux

// Regression tests for defects found in the 2026-10-20 audit
// (docs/audit/2026-10-20-audit.md). Each one failed before its fix.
package server

import (
	"archive/zip"
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/ZaidQamhieh/rfe/agent/internal/fsops"
)

// F-03: downloading a FIFO inside the root blocks the request forever instead of
// being refused.
func TestAuditRepro_DownloadFIFOReturnsPromptly(t *testing.T) {
	root := t.TempDir()
	fifo := filepath.Join(root, "pipe")
	if err := syscall.Mkfifo(fifo, 0o644); err != nil {
		t.Fatal(err)
	}
	ops := fsops.New([]string{root}, false)
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() { done <- getContent(ops, fifo, nil) }()
	select {
	case rr := <-done:
		if rr.Code == http.StatusOK {
			t.Fatalf("FIFO served with 200")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("download of a FIFO is blocked after 3s (request and goroutine leak)")
	}
}

// F-05: expiresInSeconds is multiplied by time.Second before the 24h cap
// comparison; 18446744074 s overflows int64 to ~0.29 s and passes the check.
func TestAuditRepro_ShareExpiryOverflowRejected(t *testing.T) {
	ops, root := newFsFixture(t)
	db, st := newTestDeps(t)
	if err := st.SetAllowSharing(true); err != nil {
		t.Fatal(err)
	}
	handler := mintShareHandler(Config{Address: "127.0.0.1:8765", Settings: st}, db, ops)
	body := `{"path":"` + filepath.Join(root, "a.txt") + `","expiresInSeconds":18446744074}`
	rr := httptest.NewRecorder()
	handler(rr, httptest.NewRequest(http.MethodPost, "/v1/share/mint", strings.NewReader(body)))
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expiresInSeconds above the 24h cap accepted: %d %s", rr.Code, rr.Body.String())
	}
}

// F-70: ListDir and Checksum open a FIFO with a blocking open(2) and never return.
func TestAuditRepro_ListDirAndChecksumFIFOReturnPromptly(t *testing.T) {
	root := t.TempDir()
	fifo := filepath.Join(root, "pipe")
	if err := syscall.Mkfifo(fifo, 0o644); err != nil {
		t.Fatal(err)
	}
	ops := fsops.New([]string{root}, false)
	for name, call := range map[string]func(){
		"ListDir":  func() { _, _ = ops.ListDir(fifo, "", 10) },
		"Checksum": func() { _, _ = ops.Checksum(fifo, "sha256") },
	} {
		done := make(chan struct{})
		go func() { call(); close(done) }()
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			t.Errorf("%s on a FIFO blocked for over 3s", name)
		}
	}
}

// F-89: Move(src, destDir inside src, overwrite=true) deletes a subtree of the source, then fails.
func TestAuditRepro_MoveIntoItselfOverwriteKeepsData(t *testing.T) {
	root := t.TempDir()
	keep := filepath.Join(root, "a", "b", "a", "keep.txt")
	if err := os.MkdirAll(filepath.Dir(keep), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keep, []byte("precious"), 0o644); err != nil {
		t.Fatal(err)
	}
	ops := fsops.New([]string{root}, false)
	res := ops.Move([]string{filepath.Join(root, "a")}, filepath.Join(root, "a", "b"), false, true)
	t.Logf("result: %+v", res)
	if _, err := os.Stat(keep); err != nil {
		t.Fatalf("data inside the source was deleted by a failing move: %v", err)
	}
}

// F-90: Extract silently replaces an existing file of the same name.
func TestAuditRepro_ExtractDoesNotClobberExisting(t *testing.T) {
	root := t.TempDir()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, _ := zw.Create("doc.txt")
	_, _ = w.Write([]byte("from-archive"))
	_ = zw.Close()
	if err := os.WriteFile(filepath.Join(root, "pack.zip"), buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(root, "out")
	if err := os.MkdirAll(dest, 0o755); err != nil {
		t.Fatal(err)
	}
	orig := filepath.Join(dest, "doc.txt")
	if err := os.WriteFile(orig, []byte("precious"), 0o644); err != nil {
		t.Fatal(err)
	}
	ops := fsops.New([]string{root}, false)
	_, _ = ops.Extract(filepath.Join(root, "pack.zip"), dest)
	if b, _ := os.ReadFile(orig); string(b) != "precious" {
		t.Fatalf("existing file replaced by Extract: now %q", b)
	}
}

// F-119: Move over a dangling symlink at the destination replaces it with no conflict.
func TestAuditRepro_MoveOverDanglingSymlinkConflicts(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "x")
	dir := filepath.Join(root, "d")
	if err := os.WriteFile(src, []byte("data"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("nowhere", filepath.Join(dir, "x")); err != nil {
		t.Fatal(err)
	}
	ops := fsops.New([]string{root}, false)
	res := ops.Move([]string{src}, dir, false, false)
	if len(res) == 1 && res[0].Error == nil {
		t.Fatalf("Move replaced the dangling symlink without a CONFLICT (Rename uses lstat and would refuse)")
	}
}

// F-132: Chmod opens a FIFO with a blocking open.
func TestAuditRepro_ChmodFIFOReturnsPromptly(t *testing.T) {
	root := t.TempDir()
	fifo := filepath.Join(root, "pipe")
	if err := syscall.Mkfifo(fifo, 0o644); err != nil {
		t.Fatal(err)
	}
	ops := fsops.New([]string{root}, false)
	done := make(chan struct{})
	go func() { _ = ops.Chmod(fifo, 0o600); close(done) }()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("Chmod on a FIFO blocked for over 3s")
	}
}

// F-133: the handler passes a raw 32-bit octal value to fs.FileMode, so Go's type bits
// (setuid is 1<<23) were reachable: mode "40000755" set setuid while "4755" was dropped.
func TestAuditRepro_ChmodModeAboveSevenSevenSevenRejected(t *testing.T) {
	root := t.TempDir()
	f := filepath.Join(root, "f.sh")
	if err := os.WriteFile(f, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	handler := chmodHandler(fsops.New([]string{root}, false))
	for _, mode := range []string{"40000755", "4755", "1777"} {
		rr := httptest.NewRecorder()
		body := `{"path":"` + f + `","mode":"` + mode + `"}`
		handler(rr, httptest.NewRequest(http.MethodPost, "/v1/fs/chmod", strings.NewReader(body)))
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("mode %s accepted with %d, want 400", mode, rr.Code)
		}
	}
	if st, _ := os.Stat(f); st.Mode().Perm() != 0o644 || st.Mode()&(os.ModeSetuid|os.ModeSetgid|os.ModeSticky) != 0 {
		t.Fatalf("mode changed by a rejected request: %v", st.Mode())
	}
}
