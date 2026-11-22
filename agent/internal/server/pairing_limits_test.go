package server

import (
	"archive/zip"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ZaidQamhieh/rfe/agent/internal/fsops"
)

// Requirement 19 (decided at the 2026-10-26 closure): POST /trash/restore is a
// batch. A well-formed request answers 200 with one result per id; an invalid
// id is that item's ok=false BAD_REQUEST, never a success, and the valid ids
// in the same batch are restored. A malformed request is a request-level 400.
func TestClosureR19_RestoreMixedBatch(t *testing.T) {
	ops, root := newFsFixture(t)
	trashDir := filepath.Join(root, ".trash")
	if err := os.Mkdir(trashDir, 0o755); err != nil {
		t.Fatal(err)
	}
	ops.MoveToTrash([]string{filepath.Join(root, "a.txt")}, trashDir)
	items, err := fsops.ListTrash(trashDir)
	if err != nil || len(items) != 1 {
		t.Fatalf("trash items = %v, %v", items, err)
	}
	// A sentinel outside the store that the "../" id would name.
	sentinel := filepath.Join(root, "sentinel.txt")
	if err := os.WriteFile(sentinel, []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}
	invalid := []string{"../sentinel.txt", "..", ".", "a/b", `a\b`, "/etc/passwd", ""}
	ids := append([]string{items[0].ID}, invalid...)
	body, _ := json.Marshal(map[string]any{"ids": ids})
	rr := httptest.NewRecorder()
	restoreTrashHandler(ops, trashDir)(rr, httptest.NewRequest(http.MethodPost, "/v1/trash/restore", strings.NewReader(string(body))))
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rr.Code, rr.Body.String())
	}
	var got struct {
		Results []fsops.BatchResult `json:"results"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Results) != len(ids) {
		t.Fatalf("results = %d, want one per id (%d): %s", len(got.Results), len(ids), rr.Body.String())
	}
	// The agent reports resolved paths (macOS temp dirs sit behind /var -> /private/var).
	realRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	if r := got.Results[0]; !r.OK || r.Error != nil || r.Path != filepath.Join(realRoot, "a.txt") {
		t.Errorf("valid id result = %+v, want ok at %s", r, filepath.Join(realRoot, "a.txt"))
	}
	for i, r := range got.Results[1:] {
		if r.OK || r.Error == nil || r.Error.Code != "BAD_REQUEST" || r.Path != invalid[i] {
			t.Errorf("invalid id %q result = %+v, want ok=false BAD_REQUEST echoing the id", invalid[i], r)
		}
	}
	if b, _ := os.ReadFile(sentinel); string(b) != "keep" {
		t.Errorf("sentinel outside the store changed: %q", b)
	}
	if _, err := os.Stat(filepath.Join(root, "a.txt")); err != nil {
		t.Errorf("valid id not restored: %v", err)
	}
}

func TestClosureR19_RestoreRequestLevelErrors(t *testing.T) {
	ops, root := newFsFixture(t)
	trashDir := filepath.Join(root, ".trash")
	for _, body := range []string{``, `{`, `{}`, `null`, `{"ids":[]}`, `{"ids":"x"}`} {
		rr := httptest.NewRecorder()
		restoreTrashHandler(ops, trashDir)(rr, httptest.NewRequest(http.MethodPost, "/v1/trash/restore", strings.NewReader(body)))
		if rr.Code != http.StatusBadRequest {
			t.Errorf("body %q: status = %d, want 400", body, rr.Code)
		}
	}
	// DELETE /trash keeps its request-level 400 for an invalid id.
	rr := httptest.NewRecorder()
	emptyTrashHandler(ops, trashDir)(rr, httptest.NewRequest(http.MethodDelete, "/v1/trash", strings.NewReader(`{"ids":["../x"]}`)))
	if rr.Code != http.StatusBadRequest {
		t.Errorf("DELETE /trash invalid id: status = %d, want 400", rr.Code)
	}
}

// Requirement 23: POST /fs/extract answers 403 FORBIDDEN for an absolute entry name.
func TestClosureR23_ExtractAbsoluteEntryIs403(t *testing.T) {
	ops, root := newFsFixture(t)
	archive := filepath.Join(root, "abs.zip")
	f, err := os.Create(archive)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	w, _ := zw.CreateHeader(&zip.FileHeader{Name: "/r23-handler-abs.txt"})
	_, _ = w.Write([]byte("x"))
	_ = zw.Close()
	_ = f.Close()
	dest := filepath.Join(root, "out")
	body, _ := json.Marshal(map[string]string{"archive": archive, "destDir": dest})
	rr := httptest.NewRecorder()
	extractHandler(ops)(rr, httptest.NewRequest(http.MethodPost, "/v1/fs/extract", strings.NewReader(string(body))))
	if rr.Code != http.StatusForbidden || !strings.Contains(rr.Body.String(), `"FORBIDDEN"`) {
		t.Fatalf("status = %d body %s, want 403 FORBIDDEN", rr.Code, rr.Body.String())
	}
	if strings.Contains(rr.Body.String(), root) {
		t.Errorf("error echoes a host path: %s", rr.Body.String())
	}
	if _, err := os.Lstat(dest); !os.IsNotExist(err) {
		t.Errorf("destDir left behind: %v", err)
	}
}
