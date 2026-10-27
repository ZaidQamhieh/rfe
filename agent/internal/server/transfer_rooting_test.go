package server

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/ZaidQamhieh/rfe/agent/internal/fsops"
	"github.com/ZaidQamhieh/rfe/agent/internal/transfer"
	"github.com/google/uuid"
)

func TestOpenSessionWithStatUsesEffectiveJail(t *testing.T) {
	parent := t.TempDir()
	jail := filepath.Join(parent, "jail")
	outside := filepath.Join(parent, "outside")
	if err := os.Mkdir(jail, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(outside, []byte("outside"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(jail, "existing.bin")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("cannot create symlink on this platform: %v", err)
	}

	tm, _ := newTestTransferManager(t)
	ops := fsops.New([]string{jail}, false)
	if _, err := tm.OpenSessionWithStat(uuid.NewString(), link, 1, 1, "unused", false, "", ops.Stat); !errors.Is(err, fsops.ErrForbidden) {
		t.Fatalf("OpenSessionWithStat error = %v, want ErrForbidden", err)
	}
}

func createTransferForCompletion(t *testing.T, target string, overwrite bool, content []byte) (*transfer.Manager, string, *fsops.Ops) {
	t.Helper()
	tm, _ := newTestTransferManager(t)
	opsRoot := filepath.Dir(target)
	ops := fsops.New([]string{opsRoot}, false)
	id := uuid.NewString()
	if _, err := tm.OpenSessionWithStat(id, target, int64(len(content)), len(content), sha256hex(content), overwrite, "", ops.Stat); err != nil {
		t.Fatalf("open transfer session: %v", err)
	}
	if err := tm.WriteChunk(id, 0, content, sha256hex(content)); err != nil {
		t.Fatalf("write transfer chunk: %v", err)
	}
	return tm, id, ops
}

func completeTestTransfer(t *testing.T, tm *transfer.Manager, ops *fsops.Ops, id string) *httptest.ResponseRecorder {
	t.Helper()
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/transfers/"+id+"/complete", nil)
	req = asAdmin(withURLParam(req, map[string]string{"id": id}))
	completeTransferHandler(tm, ops)(rr, req)
	return rr
}

func TestCompleteTransferHandlerRejectsParentSymlinkSwapOutsideJail(t *testing.T) {
	jail := t.TempDir()
	targetDir := filepath.Join(jail, "destination")
	if err := os.Mkdir(targetDir, 0o755); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(targetDir, "upload.bin")
	content := []byte("verified upload")
	tm, _ := newTestTransferManager(t)
	ops := fsops.New([]string{jail}, false)
	id := uuid.NewString()
	if _, err := tm.OpenSessionWithStat(id, target, int64(len(content)), len(content), sha256hex(content), true, "", ops.Stat); err != nil {
		t.Fatalf("open transfer session: %v", err)
	}
	if err := tm.WriteChunk(id, 0, content, sha256hex(content)); err != nil {
		t.Fatalf("write transfer chunk: %v", err)
	}

	outside := t.TempDir()
	backupDir := filepath.Join(jail, "destination-original")
	if err := os.Rename(targetDir, backupDir); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, targetDir); err != nil {
		t.Skipf("cannot create directory symlink on this platform: %v", err)
	}

	rr := completeTestTransfer(t, tm, ops, id)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403; body: %s", rr.Code, rr.Body.String())
	}
	if _, err := os.Lstat(filepath.Join(outside, "upload.bin")); !os.IsNotExist(err) {
		t.Fatalf("outside destination was created or stat failed: %v", err)
	}
}

func TestCompleteTransferHandlerNoReplaceSurvivesDestinationRace(t *testing.T) {
	jail := t.TempDir()
	target := filepath.Join(jail, "upload.bin")
	content := []byte("verified upload")
	tm, id, ops := createTransferForCompletion(t, target, false, content)
	const appeared = "created while the client was uploading"
	if err := os.WriteFile(target, []byte(appeared), 0o600); err != nil {
		t.Fatal(err)
	}

	rr := completeTestTransfer(t, tm, ops, id)
	if rr.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409; body: %s", rr.Code, rr.Body.String())
	}
	got, err := os.ReadFile(target)
	if err != nil || string(got) != appeared {
		t.Fatalf("destination after no-replace conflict = (%q, %v)", got, err)
	}
}
