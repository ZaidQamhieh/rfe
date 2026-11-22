package settings

import (
	"errors"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/ZaidQamhieh/rfe/agent/internal/next/store"
)

// spyStore wraps the real database, counting writes and optionally failing them.
type spyStore struct {
	*store.DB
	writes  int
	failSet error
}

func (s *spyStore) SetSettings(kv map[string]string) error {
	s.writes++
	if s.failSet != nil {
		return s.failSet
	}
	return s.DB.SetSettings(kv)
}

// openDB opens a database in a temp dir.
func openDB(t *testing.T) (*store.DB, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "agent-v3.db")
	db, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db, path
}

// mustOpen opens the Service or fails the test.
func mustOpen(t *testing.T, st Store, opts Options) *Service {
	t.Helper()
	s, err := Open(st, opts)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	return s
}

func ptr[T any](v T) *T { return &v }

func TestNewDatabaseDefaultsToRFEFilesUnderHome(t *testing.T) {
	db, _ := openDB(t)
	home := t.TempDir()
	s := mustOpen(t, db, Options{HomeDir: home, AgentName: "kitchen-pc"})
	got := s.Get()
	want := Settings{Roots: []string{filepath.Join(home, DefaultFilesFolderName)}, AgentName: "kitchen-pc"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("defaults = %+v, want %+v", got, want)
	}
	if got.Unrestricted() {
		t.Fatal("a default database must not be unrestricted")
	}
	rows, _ := db.Settings()
	if rows[KeyRoots] != want.Roots[0] || rows[KeyReadOnly] != "false" || rows[KeyAllowSharing] != "false" || rows[KeyAgentName] != "kitchen-pc" {
		t.Fatalf("seeded rows = %v", rows)
	}
	if ok, err := Initialised(db); err != nil || !ok {
		t.Fatalf("Initialised = %v, %v after Open", ok, err)
	}
}

func TestNoHomeAndNoExplicitRootsIsRefusedNotUnrestricted(t *testing.T) {
	db, _ := openDB(t)
	if ok, _ := Initialised(db); ok {
		t.Fatal("a fresh database reports Initialised")
	}
	if _, err := Open(db, Options{}); !errors.Is(err, ErrNoDefaultRoot) {
		t.Fatalf("err = %v, want ErrNoDefaultRoot", err)
	}
	if rows, _ := db.Settings(); len(rows) != 0 {
		t.Fatalf("a refused Open wrote %v", rows)
	}
}

func TestDefaultNameWhenNoneGiven(t *testing.T) {
	db, _ := openDB(t)
	if n := mustOpen(t, db, Options{HomeDir: t.TempDir(), AgentName: "  "}).Get().AgentName; n != DefaultAgentName {
		t.Fatalf("name = %q, want %q", n, DefaultAgentName)
	}
}

func TestExplicitUnrestrictedIsKeptAcrossReopen(t *testing.T) {
	db, path := openDB(t)
	s := mustOpen(t, db, Options{HomeDir: t.TempDir(), RootsExplicit: true, SeedRoots: nil})
	if !s.Get().Unrestricted() {
		t.Fatal("explicit empty roots must be unrestricted")
	}
	rows, _ := db.Settings()
	if v, ok := rows[KeyRoots]; !ok || v != "" {
		t.Fatalf("roots row = %q present=%v, want an empty row", v, ok)
	}
	db.Close()
	again, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer again.Close()
	// A later start without the explicit flag must not re-seed the default folder.
	if got := mustOpen(t, again, Options{HomeDir: t.TempDir()}).Get(); !got.Unrestricted() {
		t.Fatalf("reopen re-seeded roots: %v", got.Roots)
	}
}

func TestExplicitRootsSeedAreCleaned(t *testing.T) {
	db, _ := openDB(t)
	a, b := t.TempDir(), t.TempDir()
	s := mustOpen(t, db, Options{RootsExplicit: true, SeedRoots: []string{a + "/", " ", b, a}})
	if got := s.Get().Roots; !reflect.DeepEqual(got, []string{a, b}) {
		t.Fatalf("roots = %v, want [%s %s]", got, a, b)
	}
}

func TestImportedPolicyIsPreservedUnderTheOldKeys(t *testing.T) {
	db, _ := openDB(t)
	a, b, photo := t.TempDir(), t.TempDir(), t.TempDir()
	// The literal key names the old agent and the legacy import use.
	imported := map[string]string{
		"roots": a + "\n" + b, "readOnly": "true", "agentName": "Old PC", "allowSharing": "true",
		"maxUploadBytesPerSec": "1000", "maxDownloadBytesPerSec": "2000", "photoBackupRoot": photo,
	}
	if err := db.ImportAll(store.ImportData{Settings: imported}); err != nil {
		t.Fatal(err)
	}
	s := mustOpen(t, db, Options{HomeDir: t.TempDir(), AgentName: "ignored", ReadOnly: false})
	want := Settings{ReadOnly: true, Roots: []string{a, b}, AgentName: "Old PC", AllowSharing: true,
		PhotoBackupRoot: photo, MaxUploadBytesPerSec: 1000, MaxDownloadBytesPerSec: 2000}
	if got := s.Get(); !reflect.DeepEqual(got, want) {
		t.Fatalf("imported = %+v, want %+v", got, want)
	}
	// Opening must not have rewritten any imported row.
	if rows, _ := db.Settings(); !reflect.DeepEqual(rows, imported) {
		t.Fatalf("rows after Open = %v, want the imported rows untouched", rows)
	}
}

func TestImportedEmptyRootsStayUnrestricted(t *testing.T) {
	db, _ := openDB(t)
	if err := db.ImportAll(store.ImportData{Settings: map[string]string{"roots": ""}}); err != nil {
		t.Fatal(err)
	}
	if !mustOpen(t, db, Options{HomeDir: t.TempDir()}).Get().Unrestricted() {
		t.Fatal("an imported empty roots row must stay unrestricted")
	}
}

func TestMissingKeysAreSeededAroundImportedOnes(t *testing.T) {
	db, _ := openDB(t)
	a := t.TempDir()
	if err := db.ImportAll(store.ImportData{Settings: map[string]string{"roots": a}}); err != nil {
		t.Fatal(err)
	}
	s := mustOpen(t, db, Options{ReadOnly: true, AgentName: "seed"})
	if got := s.Get(); !got.ReadOnly || got.AgentName != "seed" || !reflect.DeepEqual(got.Roots, []string{a}) {
		t.Fatalf("settings = %+v", got)
	}
}

func TestUnreadableStoredValuesAreErrorsNamingTheKey(t *testing.T) {
	for key, bad := range map[string]string{
		KeyReadOnly: "maybe", KeyAllowSharing: "1", KeyMaxUploadBPS: "-5", KeyMaxDownloadBPS: "fast",
	} {
		db, _ := openDB(t)
		if err := db.ImportAll(store.ImportData{Settings: map[string]string{KeyRoots: "", key: bad}}); err != nil {
			t.Fatal(err)
		}
		if _, err := Open(db, Options{}); err == nil || !strings.Contains(err.Error(), key) {
			t.Errorf("%s=%q: err = %v, want an error naming the key", key, bad, err)
		}
	}
}

func TestUpdateValidation(t *testing.T) {
	db, _ := openDB(t)
	s := mustOpen(t, db, Options{HomeDir: t.TempDir(), AgentName: "pc"})
	before := s.Get()
	abs := t.TempDir()
	cases := []struct {
		name  string
		field string
		u     Update
	}{
		{"blank name", KeyAgentName, Update{AgentName: ptr("   ")}},
		{"control in name", KeyAgentName, Update{AgentName: ptr("a\nb")}},
		{"long name", KeyAgentName, Update{AgentName: ptr(strings.Repeat("x", MaxAgentNameRunes+1))}},
		{"relative root", KeyRoots, Update{Roots: ptr([]string{abs, "docs/work"})}},
		{"blank root", KeyRoots, Update{Roots: ptr([]string{" "})}},
		{"newline root", KeyRoots, Update{Roots: ptr([]string{abs + "\n/etc"})}},
		{"long root", KeyRoots, Update{Roots: ptr([]string{abs + "/" + strings.Repeat("d", MaxPathBytes)})}},
		{"too many roots", KeyRoots, Update{Roots: ptr(make([]string, MaxRoots+1))}},
		{"relative photo root", KeyPhotoBackupRoot, Update{PhotoBackupRoot: ptr("photos")}},
		{"negative upload", KeyMaxUploadBPS, Update{MaxUploadBytesPerSec: ptr(int64(-1))}},
		{"negative download", KeyMaxDownloadBPS, Update{MaxDownloadBytesPerSec: ptr(int64(-1))}},
		// A valid field next to an invalid one must not be applied either.
		{"mixed", KeyAgentName, Update{ReadOnly: ptr(true), AgentName: ptr("")}},
	}
	for _, c := range cases {
		_, err := s.Update(c.u)
		var ie *InvalidError
		if !errors.Is(err, ErrInvalid) || !errors.As(err, &ie) || ie.Field != c.field {
			t.Errorf("%s: err = %v, want ErrInvalid on %s", c.name, err, c.field)
		}
		if got := s.Get(); !reflect.DeepEqual(got, before) {
			t.Errorf("%s: settings changed to %+v", c.name, got)
		}
	}
	rows, _ := db.Settings()
	if rows[KeyReadOnly] != "false" || rows[KeyAgentName] != "pc" {
		t.Fatalf("a refused update reached the database: %v", rows)
	}
}

func TestUpdateAppliesPersistsAndSurvivesReopen(t *testing.T) {
	db, path := openDB(t)
	s := mustOpen(t, db, Options{HomeDir: t.TempDir(), AgentName: "pc"})
	a, b, photo := t.TempDir(), t.TempDir(), t.TempDir()
	got, err := s.Update(Update{
		ReadOnly: ptr(true), Roots: ptr([]string{a + "/", b, a}), AgentName: ptr("  Den PC "), AllowSharing: ptr(true),
		PhotoBackupRoot: ptr(photo), MaxUploadBytesPerSec: ptr(int64(500)), MaxDownloadBytesPerSec: ptr(int64(0)),
	})
	if err != nil {
		t.Fatal(err)
	}
	want := Settings{ReadOnly: true, Roots: []string{a, b}, AgentName: "Den PC", AllowSharing: true,
		PhotoBackupRoot: photo, MaxUploadBytesPerSec: 500}
	if !reflect.DeepEqual(got, want) || !reflect.DeepEqual(s.Get(), want) {
		t.Fatalf("after update = %+v, want %+v", got, want)
	}
	db.Close()
	again, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer again.Close()
	if reopened := mustOpen(t, again, Options{HomeDir: t.TempDir()}).Get(); !reflect.DeepEqual(reopened, want) {
		t.Fatalf("after reopen = %+v, want %+v", reopened, want)
	}
}

func TestUpdateToUnrestrictedThenBackToFolders(t *testing.T) {
	db, _ := openDB(t)
	s := mustOpen(t, db, Options{HomeDir: t.TempDir()})
	got, err := s.Update(Update{Roots: ptr([]string{})})
	if err != nil || !got.Unrestricted() {
		t.Fatalf("explicit empty roots: %+v, %v", got, err)
	}
	a := t.TempDir()
	if got, err = s.Update(Update{Roots: ptr([]string{a})}); err != nil || got.Unrestricted() || got.Roots[0] != a {
		t.Fatalf("restoring roots: %+v, %v", got, err)
	}
	if got, err = s.Update(Update{PhotoBackupRoot: ptr("")}); err != nil || got.PhotoBackupRoot != "" {
		t.Fatalf("clearing photo root: %+v, %v", got, err)
	}
}

func TestNotificationsCarryOldAndNewAndSkipNoOps(t *testing.T) {
	db, _ := openDB(t)
	spy := &spyStore{DB: db}
	s := mustOpen(t, spy, Options{HomeDir: t.TempDir()})
	baseline := spy.writes
	var changes []Change
	cancel := s.Subscribe(func(c Change) {
		changes = append(changes, c)
		_ = s.Get() // reading from a subscriber must not deadlock
	})
	if _, err := s.Update(Update{ReadOnly: ptr(false), MaxUploadBytesPerSec: ptr(int64(0))}); err != nil {
		t.Fatal(err)
	}
	if len(changes) != 0 || spy.writes != baseline {
		t.Fatalf("a no-op update notified %d times and wrote %d times", len(changes), spy.writes-baseline)
	}
	if _, err := s.Update(Update{ReadOnly: ptr(true)}); err != nil {
		t.Fatal(err)
	}
	if len(changes) != 1 || changes[0].Old.ReadOnly || !changes[0].New.ReadOnly {
		t.Fatalf("changes = %+v, want one old=false new=true", changes)
	}
	cancel()
	if _, err := s.Update(Update{ReadOnly: ptr(false)}); err != nil {
		t.Fatal(err)
	}
	if len(changes) != 1 {
		t.Fatalf("a cancelled subscriber was called again: %d", len(changes))
	}
}

func TestFailedWriteChangesNothingAndNotifiesNobody(t *testing.T) {
	db, _ := openDB(t)
	spy := &spyStore{DB: db}
	s := mustOpen(t, spy, Options{HomeDir: t.TempDir()})
	before := s.Get()
	called := false
	s.Subscribe(func(Change) { called = true })
	spy.failSet = errors.New("disk full")
	_, err := s.Update(Update{ReadOnly: ptr(true)})
	if err == nil || !strings.Contains(err.Error(), "disk full") || errors.Is(err, ErrInvalid) {
		t.Fatalf("err = %v, want the store's error", err)
	}
	if called || !reflect.DeepEqual(s.Get(), before) {
		t.Fatalf("failed write notified=%v, settings=%+v", called, s.Get())
	}
}

func TestGetReturnsACopy(t *testing.T) {
	db, _ := openDB(t)
	a := t.TempDir()
	s := mustOpen(t, db, Options{RootsExplicit: true, SeedRoots: []string{a}})
	s.Get().Roots[0] = "/tampered"
	if got := s.Get().Roots[0]; got != a {
		t.Fatalf("root = %q, caller changed the service's state", got)
	}
}

func TestConcurrentUpdatesAndReads(t *testing.T) {
	db, _ := openDB(t)
	s := mustOpen(t, db, Options{HomeDir: t.TempDir()})
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(2)
		go func(n int64) {
			defer wg.Done()
			if _, err := s.Update(Update{MaxUploadBytesPerSec: ptr(n)}); err != nil {
				t.Error(err)
			}
		}(int64(i))
		go func() { defer wg.Done(); _ = s.Get() }()
	}
	wg.Wait()
	rows, _ := db.Settings()
	if got := s.Get().MaxUploadBytesPerSec; rows[KeyMaxUploadBPS] != "" && rows[KeyMaxUploadBPS] != strconv.FormatInt(got, 10) {
		t.Fatalf("memory %d and database %q disagree", got, rows[KeyMaxUploadBPS])
	}
}
