package transfer

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ZaidQamhieh/rfe/agent/internal/store"
)

// The declared whole-file hash and each chunk hash are hex: upper case is the same value.
func TestUppercaseHexHashesAreAccepted(t *testing.T) {
	tm, _, dataDir := setupManager(t)
	content := []byte("hex digits have no case")
	upper := strings.ToUpper(sha256hex(content))
	target := filepath.Join(dataDir, "out.bin")

	if _, err := tm.OpenSession("upper-hex", target, int64(len(content)), 1024, upper, false, ""); err != nil {
		t.Fatalf("OpenSession: %v", err)
	}
	if err := tm.WriteChunk("upper-hex", 0, content, upper); err != nil {
		t.Fatalf("WriteChunk with an upper-case chunk hash: %v", err)
	}
	if _, _, err := tm.Complete("upper-hex"); err != nil {
		t.Fatalf("Complete with an upper-case file hash: %v", err)
	}
	if got, err := os.ReadFile(target); err != nil || string(got) != string(content) {
		t.Fatalf("published file = %q, err %v", got, err)
	}
}

// complete() must open only the manager-owned temp name, like WriteChunk: a row whose
// persisted temp path points elsewhere is refused and that file is never published.
func TestCompleteRefusesForeignTempPath(t *testing.T) {
	tm, db, dataDir := setupManager(t)
	content := []byte("not the manager's file")
	foreign := filepath.Join(dataDir, "foreign.bin")
	if err := os.WriteFile(foreign, content, 0o600); err != nil {
		t.Fatalf("seed: %v", err)
	}
	target := filepath.Join(dataDir, "published.bin")
	row := &store.Transfer{
		ID: "foreign-temp", TargetPath: target, TotalSize: int64(len(content)), ChunkSize: 1024,
		SHA256: sha256hex(content), TotalChunks: 1, TempPath: foreign, Status: store.TransferOpen,
	}
	if err := db.CreateTransfer(row); err != nil {
		t.Fatalf("CreateTransfer: %v", err)
	}
	if err := db.MarkChunkReceived("foreign-temp", 0); err != nil {
		t.Fatalf("MarkChunkReceived: %v", err)
	}

	if _, _, err := tm.Complete("foreign-temp"); err == nil {
		t.Fatal("Complete must refuse a temp path the manager does not own")
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatalf("nothing may be published from a foreign temp path, stat err = %v", err)
	}
	if _, err := os.Stat(foreign); err != nil {
		t.Fatalf("the foreign file must be left alone: %v", err)
	}
}
