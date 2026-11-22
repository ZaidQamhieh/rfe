package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"

	"github.com/ZaidQamhieh/rfe/agent/internal/fsops"
	"github.com/ZaidQamhieh/rfe/agent/internal/transfer"
)

// oversizeForDisk is a declared upload size (60 GiB, under the 64 GiB product cap) that the disk behind dir
// cannot hold, or 0 when dir has more free space than that and the test would really allocate it.
func oversizeForDisk(t *testing.T, dir string) int64 {
	t.Helper()
	const declared = int64(60) << 30
	var fs syscall.Statfs_t
	if err := syscall.Statfs(dir, &fs); err != nil {
		t.Fatalf("statfs: %v", err)
	}
	if free := int64(fs.Bavail) * int64(fs.Bsize); free >= declared {
		return 0
	}
	return declared
}

// TestOpenTransferHandler_DiskFullIs507WithRetryAfter opens a session whose declared size the disk behind the
// temp dir cannot hold. The reservation fails at open with a real ENOSPC: 507 INSUFFICIENT_STORAGE, Retry-After
// set, and no session left behind. Skipped when the temp dir has room for the declared size (nothing to fill).
func TestOpenTransferHandler_DiskFullIs507WithRetryAfter(t *testing.T) {
	transfer.SetSparseReservations(false) // this test is about the real reservation
	t.Cleanup(func() { transfer.SetSparseReservations(true) })
	db, st := newTestDeps(t)
	tmp := t.TempDir()
	size := oversizeForDisk(t, tmp)
	if size == 0 {
		t.Skip("temp dir has more free space than the 60 GiB the test declares, so the disk cannot be filled")
	}
	tm, err := transfer.New(db, tmp)
	if err != nil {
		t.Fatal(err)
	}
	ops := fsops.NewWithSettings(st)

	target := filepath.Join(t.TempDir(), "big.bin")
	body := `{"path":"` + target + `","size":` + strconv.FormatInt(size, 10) + `,"sha256":"` + sha256hex(nil) + `","chunkSize":1048576}`
	rr := httptest.NewRecorder()
	openTransferHandler(tm, ops)(rr, httptest.NewRequest(http.MethodPost, "/v1/transfers", strings.NewReader(body)))

	if rr.Code != http.StatusInsufficientStorage {
		t.Fatalf("status = %d, want 507: %s", rr.Code, rr.Body.String())
	}
	if ra := rr.Header().Get("Retry-After"); ra == "" {
		t.Error("507 carries no Retry-After header")
	}
	var apiErr struct{ Code string }
	if err := json.Unmarshal(rr.Body.Bytes(), &apiErr); err != nil || apiErr.Code != "INSUFFICIENT_STORAGE" {
		t.Errorf("body = %s, want code INSUFFICIENT_STORAGE", rr.Body.String())
	}
	if open, err := db.ListOpenTransfers(); err != nil || len(open) != 0 {
		t.Errorf("open sessions after the refused open = (%d, %v), want none", len(open), err)
	}
}
