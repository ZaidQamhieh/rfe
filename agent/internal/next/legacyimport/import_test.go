package legacyimport_test

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/ZaidQamhieh/rfe/agent/internal/next/legacyimport"
	nstore "github.com/ZaidQamhieh/rfe/agent/internal/next/store"
	oldstore "github.com/ZaidQamhieh/rfe/agent/internal/store"
	"github.com/ZaidQamhieh/rfe/agent/internal/store/legacyfixtures"
	_ "modernc.org/sqlite" // the driver the tests use to edit a fixture
)

// importTime is the injected "now" every test uses, far from any fixture instant.
var importTime = time.Unix(1800000000, 0)

// build writes a legacy data dir of the given user_version and returns it and the new db path.
func build(t *testing.T, version int) (legacyfixtures.Fixture, string) {
	t.Helper()
	dir := t.TempDir()
	f := legacyfixtures.Build(t, dir, version)
	return f, filepath.Join(dir, legacyimport.NewDBName)
}

// hashFile is the SHA-256 of a file's bytes.
func hashFile(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// dirState maps every path under dir (relative) to "dir" or the file's hash.
func dirState(t *testing.T, dir string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(dir, p)
		if d.IsDir() {
			out[rel] = "dir"
		} else {
			out[rel] = hashFile(t, p)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// opts returns options with the fixed clock and a logger collecting lines.
func opts(lines *[]string) legacyimport.Options {
	o := legacyimport.Options{Now: func() time.Time { return importTime }}
	if lines != nil {
		o.Logf = func(format string, args ...any) { *lines = append(*lines, fmt.Sprintf(format, args...)) }
	}
	return o
}

// chunkEnd is the offset just past chunk n of a golden upload.
func chunkEnd(u legacyfixtures.UploadRow, n int) int64 {
	end := int64(n+1) * int64(u.ChunkSize)
	if end > u.TotalSize {
		end = u.TotalSize
	}
	return end
}

// wantChunks is what an import must keep of a golden upload: an open session
// keeps only chunks its temp file really holds; finished ones keep all.
func wantChunks(u legacyfixtures.UploadRow) []int {
	var out []int
	for _, n := range u.Chunks {
		if u.Status == "open" {
			var size int64
			if u.TempFile != nil {
				size = u.TempFile.Size
			}
			if chunkEnd(u, n) > size {
				continue
			}
		}
		out = append(out, n)
	}
	return out
}

func TestImportMatchesGoldenForEveryOldVersion(t *testing.T) {
	for v := 0; v <= legacyfixtures.LatestVersion; v++ {
		t.Run(fmt.Sprintf("v%d", v), func(t *testing.T) {
			f, newPath := build(t, v)
			exp := f.Expected
			before := dirState(t, f.DataDir)
			oldHash := hashFile(t, f.DBPath)

			var lines []string
			rep, err := legacyimport.Import(f.DBPath, newPath, f.DataDir, opts(&lines))
			if err != nil {
				t.Fatalf("Import: %v", err)
			}

			// The old database and the temp files are untouched; the only new file is the new database.
			if got := hashFile(t, f.DBPath); got != oldHash {
				t.Errorf("agent.db bytes changed: %s -> %s", oldHash, got)
			}
			after := dirState(t, f.DataDir)
			delete(after, legacyimport.NewDBName)
			if !reflect.DeepEqual(before, after) {
				t.Errorf("data dir changed besides the new database:\nbefore %v\nafter  %v", before, after)
			}
			if runtime.GOOS != "windows" {
				if fi, err := os.Stat(newPath); err != nil || fi.Mode().Perm()&0o077 != 0 {
					t.Errorf("new database mode = %v, %v; want private (no group/other bits)", fi.Mode(), err)
				}
			}

			db, err := nstore.Open(newPath)
			if err != nil {
				t.Fatalf("open new db: %v", err)
			}
			defer db.Close()
			if uv, _ := db.UserVersion(); uv != 1 {
				t.Errorf("user_version = %d, want 1", uv)
			}
			meta, _ := db.MetaValues()
			if meta["imported_from_user_version"] != fmt.Sprint(v) || meta["schema_version"] != "1" ||
				meta["imported_at"] != fmt.Sprint(importTime.Unix()) {
				t.Errorf("meta = %v", meta)
			}

			checkCounts(t, db, rep, exp)
			checkUsers(t, db, exp)
			checkDevices(t, db, f)
			checkShares(t, db, exp)
			checkUploads(t, db, exp)
			checkAuditAndSettings(t, db, exp)

			if rep.OldUserVersion != v {
				t.Errorf("report OldUserVersion = %d, want %d", rep.OldUserVersion, v)
			}
			if rep.NotImported["pairing_codes"] != exp.NotImported.PairingCodes || rep.NotImported["pair_requests"] != exp.NotImported.PairRequests {
				t.Errorf("report NotImported = %v, golden %+v", rep.NotImported, exp.NotImported)
			}
		})
	}
}

// checkCounts compares the table counts with the golden ones (chunk rows
// adjusted for the temp files) and with what the report promised.
func checkCounts(t *testing.T, db *nstore.DB, rep *legacyimport.Report, exp legacyfixtures.Expected) {
	t.Helper()
	chunks, dropped := 0, 0
	for _, u := range exp.Uploads {
		chunks += len(wantChunks(u))
		dropped += len(u.Chunks) - len(wantChunks(u))
	}
	want := map[string]int{
		"users": exp.Counts["users"], "devices": exp.Counts["devices"], "share_links": exp.Counts["share_tokens"],
		"share_events": exp.Counts["share_log"], "uploads": exp.Counts["uploads"], "upload_chunks": chunks,
		"audit_log": exp.Counts["audit_log"], "settings": exp.Counts["settings"],
	}
	got, err := db.Counts()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("counts = %v, want %v", got, want)
	}
	if !reflect.DeepEqual(rep.Counts, got) {
		t.Errorf("report counts %v differ from the database %v", rep.Counts, got)
	}
	if rep.DroppedChunks != dropped {
		t.Errorf("DroppedChunks = %d, want %d", rep.DroppedChunks, dropped)
	}
}

// checkUsers requires every account and password hash to be copied exactly.
func checkUsers(t *testing.T, db *nstore.DB, exp legacyfixtures.Expected) {
	t.Helper()
	got, err := db.Users()
	if err != nil {
		t.Fatal(err)
	}
	var want []nstore.User
	for _, u := range exp.Users {
		want = append(want, nstore.User{Username: u.Username, PasswordHash: u.PasswordHash, Created: u.Created})
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("users = %+v, want %+v", got, want)
	}
}

// checkDevices requires every device, its grants, its token hash and whether
// its fixture token authenticates to match the golden file.
func checkDevices(t *testing.T, db *nstore.DB, f legacyfixtures.Fixture) {
	t.Helper()
	accounts := map[string]bool{}
	for _, u := range f.Expected.Users {
		accounts[u.Username] = true
	}
	devices, err := db.Devices()
	if err != nil {
		t.Fatal(err)
	}
	byID := map[string]nstore.Device{}
	for _, d := range devices {
		byID[d.ID] = d
	}
	for _, g := range f.Expected.Devices {
		got, ok := byID[g.ID]
		if !ok {
			t.Errorf("device %s missing", g.ID)
			continue
		}
		origin := nstore.OriginCode
		if g.ViaLogin {
			origin = nstore.OriginLogin
		}
		username := g.Username
		if !accounts[username] {
			username = "" // a name without an account cannot be stored (foreign key)
		}
		want := nstore.Device{
			ID: g.ID, Label: g.Label, TokenHash: g.TokenHash, Origin: origin, Username: username,
			ClientID: g.ClientID, PublicKey: g.PublicKey, Created: g.Created, LastSeen: g.LastSeen,
			LastAddress: g.LastAddress, LastVersion: g.LastVersion, Revoked: g.Revoked, JailRoot: g.JailRoot,
			ReadOnly: g.ReadOnly, CanBrowse: g.CanBrowse, CanDownload: g.CanDownload, CanUpload: g.CanUpload,
			CanModify: g.CanModify, CanDelete: g.CanDelete, CanShare: g.CanShare, ViewApps: g.ViewApps, LaunchApps: g.LaunchApps,
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("device %s:\n got  %+v\n want %+v", g.ID, got, want)
		}
		tok := f.Tokens[g.ID]
		if nstore.HashToken(tok) != g.TokenHash {
			t.Errorf("device %s: fixture token hashes to %s, golden says %s", g.ID, nstore.HashToken(tok), g.TokenHash)
		}
		auth, err := db.DeviceByToken(tok)
		if err != nil {
			t.Errorf("DeviceByToken(%s): %v", g.ID, err)
		} else if (auth != nil) != g.Authenticates {
			t.Errorf("device %s: authenticates = %v, golden says %v", g.ID, auth != nil, g.Authenticates)
		} else if auth != nil && auth.Revoked != g.Revoked {
			t.Errorf("device %s: revoked = %v through the token lookup, golden %v", g.ID, auth.Revoked, g.Revoked)
		}
	}
}

// checkShares compares share links and share history with the golden rows.
func checkShares(t *testing.T, db *nstore.DB, exp legacyfixtures.Expected) {
	t.Helper()
	links, err := db.ShareLinks()
	if err != nil {
		t.Fatal(err)
	}
	byHash := map[string]nstore.ShareLink{}
	for _, l := range links {
		byHash[l.TokenHash] = l
	}
	for _, s := range exp.Shares {
		want := nstore.ShareLink{TokenHash: s.TokenHash, Path: s.Path, DeviceID: s.DeviceID, Created: s.Created, Expires: s.Expires}
		if got := byHash[s.TokenHash]; !reflect.DeepEqual(got, want) {
			t.Errorf("share link %s: got %+v, want %+v", s.Path, got, want)
		}
		if nstore.HashToken(s.Token) != s.TokenHash {
			t.Errorf("share token for %s does not hash to its stored hash", s.Path)
		}
	}
	events, err := db.ShareEvents()
	if err != nil {
		t.Fatal(err)
	}
	var want []nstore.ShareEvent
	for _, l := range exp.ShareLog {
		want = append(want, nstore.ShareEvent{ID: l.ID, TokenHash: l.TokenHash, Path: l.Path, MintedAt: l.MintedAt,
			ExpiresAt: l.ExpiresAt, ServedAt: l.ServedAt, RequesterIP: l.RequesterIP})
	}
	if !reflect.DeepEqual(events, want) {
		t.Errorf("share events = %+v, want %+v", events, want)
	}
}

// checkUploads compares each upload with the golden row and the L1/L2/L4 rules.
func checkUploads(t *testing.T, db *nstore.DB, exp legacyfixtures.Expected) {
	t.Helper()
	got, err := db.Uploads()
	if err != nil {
		t.Fatal(err)
	}
	byID := map[string]nstore.Upload{}
	for _, u := range got {
		byID[u.ID] = u
	}
	if len(got) != len(exp.Uploads) {
		t.Errorf("%d uploads, golden has %d", len(got), len(exp.Uploads))
	}
	for _, g := range exp.Uploads {
		u := byID[g.ID]
		status := g.Status
		if status != "open" && status != "completed" && status != "failed" {
			status = "failed" // L4
		}
		updated := g.UpdatedAt
		if updated == 0 {
			updated = importTime.Unix() // L2
		}
		if u.Status != status || u.DeviceID != g.DeviceID || u.TargetPath != g.TargetPath || u.TotalSize != g.TotalSize ||
			u.ChunkSize != g.ChunkSize || u.TotalChunks != g.TotalChunks || u.SHA256 != g.SHA256 || u.Overwrite != g.Overwrite ||
			u.TempName != path.Base(g.TempPath) || u.UpdatedAt != updated {
			t.Errorf("upload %s: got %+v, golden %+v (expected status %s, updated_at %d)", g.ID, u, g, status, updated)
		}
		switch {
		case status == "open":
			if u.TerminalAt != 0 {
				t.Errorf("upload %s is open but has terminal_at %d", g.ID, u.TerminalAt)
			}
		case g.TerminalAt != 0:
			if u.TerminalAt != g.TerminalAt {
				t.Errorf("upload %s terminal_at = %d, golden %d", g.ID, u.TerminalAt, g.TerminalAt)
			}
		case u.TerminalAt == 0:
			t.Errorf("upload %s is finished but has terminal_at 0", g.ID)
		}
		if want := wantChunks(g); !reflect.DeepEqual(u.Chunks, want) {
			t.Errorf("upload %s chunks = %v, want %v (temp file %+v)", g.ID, u.Chunks, want, g.TempFile)
		}
	}
}

// checkAuditAndSettings requires audit ids and settings to be copied exactly.
func checkAuditAndSettings(t *testing.T, db *nstore.DB, exp legacyfixtures.Expected) {
	t.Helper()
	audit, err := db.AuditEntries()
	if err != nil {
		t.Fatal(err)
	}
	var want []nstore.AuditEntry
	for _, a := range exp.Audit {
		want = append(want, nstore.AuditEntry{ID: a.ID, At: a.At, Action: a.Action, Actor: a.Actor, Target: a.Target, Detail: a.Detail})
	}
	if !reflect.DeepEqual(audit, want) {
		t.Errorf("audit = %+v, want %+v", audit, want)
	}
	settings, err := db.Settings()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(settings, exp.Config) {
		t.Errorf("settings = %v, want %v", settings, exp.Config)
	}
}

func TestStagesRunInOrderAndEachFailureLeavesNothing(t *testing.T) {
	f, newPath := build(t, 5)
	var seen []string
	o := opts(nil)
	o.Hook = func(stage string) error { seen = append(seen, stage); return nil }
	if _, err := legacyimport.Import(f.DBPath, newPath, f.DataDir, o); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(seen, legacyimport.Stages) {
		t.Fatalf("stages ran as %v, want %v", seen, legacyimport.Stages)
	}
	if err := os.Remove(newPath); err != nil {
		t.Fatal(err)
	}

	before := dirState(t, f.DataDir)
	boom := errors.New("injected failure")
	for _, stage := range legacyimport.Stages {
		t.Run(stage, func(t *testing.T) {
			o := opts(nil)
			o.Hook = func(s string) error {
				if s == stage {
					return boom
				}
				return nil
			}
			if _, err := legacyimport.Import(f.DBPath, newPath, f.DataDir, o); !errors.Is(err, boom) {
				t.Fatalf("Import with a failure at %s: err = %v, want the injected one", stage, err)
			}
			if after := dirState(t, f.DataDir); !reflect.DeepEqual(before, after) {
				t.Fatalf("a failure at %s left files behind:\nbefore %v\nafter  %v", stage, before, after)
			}
		})
	}
	// After every failure a plain rerun must succeed.
	if _, err := legacyimport.Import(f.DBPath, newPath, f.DataDir, opts(nil)); err != nil {
		t.Fatalf("rerun after the failures: %v", err)
	}
	if _, err := os.Stat(newPath); err != nil {
		t.Fatalf("rerun did not produce %s: %v", newPath, err)
	}
}

func TestStaleImportingFilesFromACrashAreReplaced(t *testing.T) {
	f, newPath := build(t, 9)
	for _, suffix := range []string{"", "-wal", "-shm", "-journal"} {
		if err := os.WriteFile(newPath+".importing"+suffix, []byte("junk from a killed run"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := legacyimport.Import(f.DBPath, newPath, f.DataDir, opts(nil)); err != nil {
		t.Fatalf("Import with stale files: %v", err)
	}
	entries, _ := os.ReadDir(f.DataDir)
	for _, e := range entries {
		if strings.Contains(e.Name(), ".importing") {
			t.Errorf("leftover %s after a successful import", e.Name())
		}
	}
}

func TestImportRefusesWhenNewDatabaseExists(t *testing.T) {
	f, newPath := build(t, 9)
	if err := os.WriteFile(newPath, []byte("keep me"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := legacyimport.Import(f.DBPath, newPath, f.DataDir, opts(nil))
	if !errors.Is(err, legacyimport.ErrAlreadyImported) {
		t.Fatalf("err = %v, want ErrAlreadyImported", err)
	}
	if b, _ := os.ReadFile(newPath); string(b) != "keep me" {
		t.Fatalf("existing new database was modified: %q", b)
	}
}

func TestImportRejectsMissingOrCorruptOldDatabase(t *testing.T) {
	dir := t.TempDir()
	newPath := filepath.Join(dir, legacyimport.NewDBName)
	if _, err := legacyimport.Import(filepath.Join(dir, "agent.db"), newPath, dir, opts(nil)); err == nil {
		t.Fatal("Import of a missing agent.db succeeded")
	}
	if err := os.WriteFile(filepath.Join(dir, "agent.db"), []byte("this is not a database, not even close to one"), 0o600); err != nil {
		t.Fatal(err)
	}
	before := dirState(t, dir)
	if _, err := legacyimport.Import(filepath.Join(dir, "agent.db"), newPath, dir, opts(nil)); err == nil {
		t.Fatal("Import of a corrupt agent.db succeeded")
	}
	if after := dirState(t, dir); !reflect.DeepEqual(before, after) {
		t.Fatalf("failed import changed the directory: %v -> %v", before, after)
	}
}

// editOld runs SQL against the fixture's old database before the import.
func editOld(t *testing.T, f legacyfixtures.Fixture, stmt string, args ...any) {
	t.Helper()
	db, err := sql.Open("sqlite", f.DBPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(stmt, args...); err != nil {
		t.Fatalf("%s: %v", stmt, err)
	}
}

func TestInvalidSettingStopsImportNamingTheKey(t *testing.T) {
	cases := []struct{ key, value string }{
		{"maxUploadBytesPerSec", "fast"},
		{"maxDownloadBytesPerSec", "-5"},
		{"readOnly", "maybe"},
		{"allowSharing", "yes"},
		{"agentName", "bad\x00name"},
	}
	for _, c := range cases {
		t.Run(c.key, func(t *testing.T) {
			f, newPath := build(t, 9)
			editOld(t, f, `INSERT OR REPLACE INTO config (key,value) VALUES (?,?)`, c.key, c.value)
			before := dirState(t, f.DataDir)
			_, err := legacyimport.Import(f.DBPath, newPath, f.DataDir, opts(nil))
			if !errors.Is(err, legacyimport.ErrInvalidSetting) || !strings.Contains(err.Error(), c.key) {
				t.Fatalf("err = %v, want ErrInvalidSetting naming %q", err, c.key)
			}
			if after := dirState(t, f.DataDir); !reflect.DeepEqual(before, after) {
				t.Fatalf("failed import left files: %v -> %v", before, after)
			}
		})
	}
}

func TestUnknownConfigKeyIsCopiedWithAWarning(t *testing.T) {
	f, newPath := build(t, 9)
	editOld(t, f, `INSERT INTO config (key,value) VALUES ('futureKey','x')`)
	var lines []string
	if _, err := legacyimport.Import(f.DBPath, newPath, f.DataDir, opts(&lines)); err != nil {
		t.Fatal(err)
	}
	db, _ := nstore.Open(newPath)
	defer db.Close()
	if s, _ := db.Settings(); s["futureKey"] != "x" {
		t.Errorf("unknown key not copied: %v", s)
	}
	if !containsLine(lines, "futureKey") {
		t.Errorf("no warning naming futureKey in %v", lines)
	}
}

// containsLine reports whether any line holds every given fragment.
func containsLine(lines []string, fragments ...string) bool {
	for _, l := range lines {
		ok := true
		for _, f := range fragments {
			ok = ok && strings.Contains(l, f)
		}
		if ok {
			return true
		}
	}
	return false
}

func TestAdjustmentsAreLogged(t *testing.T) {
	f, newPath := build(t, 9)
	var lines []string
	rep, err := legacyimport.Import(f.DBPath, newPath, f.DataDir, opts(&lines))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range [][]string{
		{"up-odd-status", "paused", "failed"}, // L4
		{"up-open-beyond", "dropped"},         // L1
		{"dev-orphan", "ghost"},               // account missing
	} {
		if !containsLine(lines, want...) {
			t.Errorf("no log line with %v in %v", want, lines)
		}
	}
	if !reflect.DeepEqual(lines, prefixed(rep.Warnings)) {
		t.Errorf("logger lines and report warnings differ:\n%v\n%v", lines, rep.Warnings)
	}
}

// prefixed adds the logger's prefix to each warning.
func prefixed(w []string) []string {
	var out []string
	for _, s := range w {
		out = append(out, "legacy import: "+s)
	}
	return out
}

func TestMissingTempFileDropsAllChunksOfThatOpenUpload(t *testing.T) {
	f, newPath := build(t, 9)
	if err := os.Remove(filepath.Join(f.DataDir, "transfers", "up-open-resume.tmp")); err != nil {
		t.Fatal(err)
	}
	if _, err := legacyimport.Import(f.DBPath, newPath, f.DataDir, opts(nil)); err != nil {
		t.Fatal(err)
	}
	db, _ := nstore.Open(newPath)
	defer db.Close()
	ups, _ := db.Uploads()
	found := false
	for _, u := range ups {
		if u.ID == "up-open-resume" {
			found = true
			if len(u.Chunks) != 0 || u.Status != "open" {
				t.Errorf("up-open-resume = %+v, want an open session with no chunk rows", u)
			}
		}
	}
	if !found {
		t.Error("up-open-resume row was lost")
	}
}

func TestCheckWritesNothingAndMatchesImport(t *testing.T) {
	for v := 0; v <= legacyfixtures.LatestVersion; v++ {
		t.Run(fmt.Sprintf("v%d", v), func(t *testing.T) {
			f, newPath := build(t, v)
			before := dirState(t, f.DataDir)
			checked, err := legacyimport.Check(f.DBPath, f.DataDir, opts(nil))
			if err != nil {
				t.Fatalf("Check: %v", err)
			}
			if after := dirState(t, f.DataDir); !reflect.DeepEqual(before, after) {
				t.Fatalf("Check changed the data dir:\nbefore %v\nafter  %v", before, after)
			}
			if _, err := os.Stat(newPath); !errors.Is(err, fs.ErrNotExist) {
				t.Fatalf("Check created %s (%v)", newPath, err)
			}
			imported, err := legacyimport.Import(f.DBPath, newPath, f.DataDir, opts(nil))
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(checked, imported) {
				t.Errorf("Check report %+v differs from Import report %+v", checked, imported)
			}
		})
	}
}

func TestOldDatabaseInWALModeIsReadWithoutChangingIt(t *testing.T) {
	f, newPath := build(t, 9)
	// The old store opens in WAL mode; open and close it once so the file header says WAL.
	old, err := oldstore.Open(f.DataDir)
	if err != nil {
		t.Fatal(err)
	}
	if err := old.Close(); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(f.DBPath)
	if err != nil {
		t.Fatal(err)
	}
	if raw[18] != 2 || raw[19] != 2 {
		t.Fatalf("fixture is not in WAL mode (header bytes %d %d); the test would prove nothing", raw[18], raw[19])
	}
	oldHash := hashFile(t, f.DBPath)
	rep, err := legacyimport.Import(f.DBPath, newPath, f.DataDir, opts(nil))
	if err != nil {
		t.Fatalf("Import of a WAL-mode database: %v", err)
	}
	if got := hashFile(t, f.DBPath); got != oldHash {
		t.Errorf("agent.db bytes changed: %s -> %s", oldHash, got)
	}
	if rep.Counts["devices"] != f.Expected.Counts["devices"] {
		t.Errorf("devices imported = %d, want %d", rep.Counts["devices"], f.Expected.Counts["devices"])
	}
	for _, side := range []string{"-wal", "-shm"} {
		if _, err := os.Stat(newPath + side); err == nil {
			t.Errorf("new database left a %s file", side)
		}
	}
}
