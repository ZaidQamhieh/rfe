package store

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// A crash mid-append can leave the log ending in a fragment with no newline. The next append must not glue
// its JSON onto that fragment: every later replay would see a malformed line in the middle of the file.
func TestAppendRevocationAfterTornLineKeepsLogReadable(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, RevocationsLogFile)
	at := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	if err := AppendRevocation(dir, "dev-a", RevocationRevoke, at); err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(`{"device_id":"dev-torn","time":"2026-01-0`); err != nil {
		t.Fatal(err)
	}
	f.Close()

	if err := AppendRevocation(dir, "dev-b", RevocationRemove, at); err != nil {
		t.Fatal(err)
	}
	if err := AppendRevocation(dir, "dev-c", RevocationRevoke, at); err != nil {
		t.Fatal(err)
	}
	got, err := ReadRevocations(path)
	if err != nil {
		t.Fatalf("ReadRevocations after append onto a torn line: %v", err)
	}
	if len(got) != 3 || got[0].DeviceID != "dev-a" || got[1].DeviceID != "dev-b" || got[2].DeviceID != "dev-c" {
		t.Fatalf("log = %+v, want dev-a, dev-b, dev-c", got)
	}
}

// A final line that is complete JSON but lost only its newline is a real entry: it must be kept.
func TestAppendRevocationKeepsCompleteLineMissingNewline(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, RevocationsLogFile)
	line := `{"device_id":"dev-a","time":"2026-01-02T03:04:05Z","kind":"revoke"}`
	if err := os.WriteFile(path, []byte(line), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := AppendRevocation(dir, "dev-b", RevocationRemove, time.Now()); err != nil {
		t.Fatal(err)
	}
	got, err := ReadRevocations(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].DeviceID != "dev-a" || got[1].DeviceID != "dev-b" {
		t.Fatalf("log = %+v, want dev-a then dev-b", got)
	}
}
