package settings

import (
	"path/filepath"
	"testing"

	"github.com/ZaidQamhieh/rfe/agent/internal/store"
)

func newDB(t *testing.T) *store.DB {
	t.Helper()
	db, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func TestLoad_SeedsUnsetKeys(t *testing.T) {
	db := newDB(t)
	s, err := Load(db, true, []string{"/home/me"}, "my-pc")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if !s.IsReadOnly() {
		t.Fatal("expected readOnly seeded true")
	}
	if got := s.Roots(); len(got) != 1 || got[0] != filepath.Clean("/home/me") {
		t.Fatalf("expected roots [/home/me], got %v", got)
	}
	if s.AgentName() != "my-pc" {
		t.Fatalf("expected name my-pc, got %s", s.AgentName())
	}
}

func TestLoad_PreservesExplicitlyEmptyRootPolicy(t *testing.T) {
	db := newDB(t)
	if err := db.SetConfig(keyRoots, ""); err != nil {
		t.Fatal(err)
	}
	s, err := Load(db, false, []string{"/home/seed"}, "my-pc")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if got := s.Roots(); len(got) != 0 {
		t.Fatalf("explicitly empty roots were replaced by defaults: %#v", got)
	}
}

func TestSetters_PersistAndApply(t *testing.T) {
	db := newDB(t)
	s, err := Load(db, false, nil, "pc")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if err := s.SetReadOnly(true); err != nil {
		t.Fatalf("set ro: %v", err)
	}
	if err := s.SetRoots([]string{"/a", "/a", " /b "}); err != nil {
		t.Fatalf("set roots: %v", err)
	}
	if err := s.SetAgentName("renamed"); err != nil {
		t.Fatalf("set name: %v", err)
	}

	// In-memory reflects immediately.
	if !s.IsReadOnly() {
		t.Fatal("readOnly not applied in memory")
	}
	if got := s.Roots(); len(got) != 2 || got[0] != "/a" || got[1] != "/b" {
		t.Fatalf("roots not normalized/deduped: %v", got)
	}

	// Persisted: a fresh Load sees the new values, not the seeds.
	s2, err := Load(db, false, nil, "pc")
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if !s2.IsReadOnly() || s2.AgentName() != "renamed" || len(s2.Roots()) != 2 {
		t.Fatalf("persisted values wrong: ro=%v name=%s roots=%v",
			s2.IsReadOnly(), s2.AgentName(), s2.Roots())
	}
}

func TestBandwidth_DefaultsUnlimited(t *testing.T) {
	db := newDB(t)
	s, err := Load(db, false, nil, "pc")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if s.MaxUploadBytesPerSec() != 0 {
		t.Fatalf("expected default upload 0, got %d", s.MaxUploadBytesPerSec())
	}
	if s.MaxDownloadBytesPerSec() != 0 {
		t.Fatalf("expected default download 0, got %d", s.MaxDownloadBytesPerSec())
	}
}

func TestBandwidth_SetAndPersist(t *testing.T) {
	db := newDB(t)
	s, err := Load(db, false, nil, "pc")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if err := s.SetMaxUploadBytesPerSec(1_000_000); err != nil {
		t.Fatalf("set upload: %v", err)
	}
	if err := s.SetMaxDownloadBytesPerSec(5_000_000); err != nil {
		t.Fatalf("set download: %v", err)
	}
	if s.MaxUploadBytesPerSec() != 1_000_000 {
		t.Fatalf("expected upload 1000000, got %d", s.MaxUploadBytesPerSec())
	}
	if s.MaxDownloadBytesPerSec() != 5_000_000 {
		t.Fatalf("expected download 5000000, got %d", s.MaxDownloadBytesPerSec())
	}

	// Persisted: a fresh Load sees the new values.
	s2, err := Load(db, false, nil, "pc")
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if s2.MaxUploadBytesPerSec() != 1_000_000 {
		t.Fatalf("persisted upload wrong: %d", s2.MaxUploadBytesPerSec())
	}
	if s2.MaxDownloadBytesPerSec() != 5_000_000 {
		t.Fatalf("persisted download wrong: %d", s2.MaxDownloadBytesPerSec())
	}
}

func TestBandwidth_NegativeClampsToZero(t *testing.T) {
	db := newDB(t)
	s, err := Load(db, false, nil, "pc")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if err := s.SetMaxUploadBytesPerSec(-100); err != nil {
		t.Fatalf("set: %v", err)
	}
	if s.MaxUploadBytesPerSec() != 0 {
		t.Fatalf("expected negative clamped to 0, got %d", s.MaxUploadBytesPerSec())
	}
}

// TestAllowSharing_DefaultsFalseAndPersists verifies AllowSharing always
// seeds false (R1 is opt-in, not CLI-configurable) and that SetAllowSharing
// persists across a reload.
func TestAllowSharing_DefaultsFalseAndPersists(t *testing.T) {
	db := newDB(t)
	s, err := Load(db, false, nil, "pc")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if s.IsAllowSharing() {
		t.Fatal("expected allowSharing to default false")
	}

	if err := s.SetAllowSharing(true); err != nil {
		t.Fatalf("set: %v", err)
	}
	if !s.IsAllowSharing() {
		t.Fatal("expected in-memory allowSharing=true")
	}

	s2, err := Load(db, false, nil, "pc")
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if !s2.IsAllowSharing() {
		t.Fatal("expected persisted allowSharing=true to survive reload")
	}
}

// TestPhotoBackupRoot_DefaultsEmptyAndPersists mirrors AllowSharing: empty
// (unconfigured) by default, and SetPhotoBackupRoot survives a reload.
func TestPhotoBackupRoot_DefaultsEmptyAndPersists(t *testing.T) {
	db := newDB(t)
	s, err := Load(db, false, nil, "pc")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if got := s.PhotoBackupRoot(); got != "" {
		t.Fatalf("expected photoBackupRoot to default empty, got %q", got)
	}

	if err := s.SetPhotoBackupRoot("/home/pc/PhoneBackups"); err != nil {
		t.Fatalf("set: %v", err)
	}
	if got := s.PhotoBackupRoot(); got != "/home/pc/PhoneBackups" {
		t.Fatalf("expected in-memory photoBackupRoot to be set, got %q", got)
	}

	s2, err := Load(db, false, nil, "pc")
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if got := s2.PhotoBackupRoot(); got != "/home/pc/PhoneBackups" {
		t.Fatalf("expected persisted photoBackupRoot to survive reload, got %q", got)
	}
}

func TestLoad_DBValueWinsOverSeed(t *testing.T) {
	db := newDB(t)
	// First load seeds readOnly=true.
	if _, err := Load(db, true, nil, "a"); err != nil {
		t.Fatalf("load1: %v", err)
	}
	// Second load with a different seed must NOT override the persisted value.
	s, err := Load(db, false, nil, "b")
	if err != nil {
		t.Fatalf("load2: %v", err)
	}
	if !s.IsReadOnly() {
		t.Fatal("expected persisted readOnly=true to win over seed=false")
	}
	if s.AgentName() != "a" {
		t.Fatalf("expected persisted name 'a', got %s", s.AgentName())
	}
}

func TestNormalizeRootsMakesRelativeRootsAbsolute(t *testing.T) {
	got := normalizeRoots([]string{"rel/dir", "/abs/dir/", "rel/dir"})
	want, err := filepath.Abs("rel/dir")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0] != want || got[1] != filepath.Clean("/abs/dir") {
		t.Fatalf("got %v, want [%s %s]", got, want, filepath.Clean("/abs/dir"))
	}
}
