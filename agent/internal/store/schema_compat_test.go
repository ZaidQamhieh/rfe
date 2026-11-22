package store

import (
	"bytes"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// querier is what both *sql.DB and *sql.Tx offer for reading.
type querier interface {
	QueryRow(query string, args ...any) *sql.Row
	Query(query string, args ...any) (*sql.Rows, error)
}

// dumpState renders everything that defines a database's schema state: user_version, every schema object
// and the schema_meta rows. Two databases with the same dump are in the same migration state.
func dumpState(t *testing.T, q querier) string {
	t.Helper()
	var b strings.Builder
	var uv int
	if err := q.QueryRow("PRAGMA user_version").Scan(&uv); err != nil {
		t.Fatalf("read user_version: %v", err)
	}
	fmt.Fprintf(&b, "user_version=%d\n", uv)
	rows, err := q.Query("SELECT type, name, COALESCE(sql,'') FROM sqlite_master ORDER BY type, name")
	if err != nil {
		t.Fatalf("read sqlite_master: %v", err)
	}
	var hasMeta bool
	for rows.Next() {
		var typ, name, ddl string
		if err := rows.Scan(&typ, &name, &ddl); err != nil {
			t.Fatal(err)
		}
		hasMeta = hasMeta || name == "schema_meta"
		fmt.Fprintf(&b, "%s %s %s\n", typ, name, ddl)
	}
	rows.Close()
	if hasMeta {
		mr, err := q.Query("SELECT key, value FROM schema_meta ORDER BY key")
		if err != nil {
			t.Fatalf("read schema_meta: %v", err)
		}
		for mr.Next() {
			var k string
			var v int
			if err := mr.Scan(&k, &v); err != nil {
				t.Fatal(err)
			}
			fmt.Fprintf(&b, "meta %s=%d\n", k, v)
		}
		mr.Close()
	}
	return b.String()
}

// rawDump opens agent.db in dir without migrating (plain driver, no store.Open) and dumps its state.
func rawDump(t *testing.T, dir string) string {
	t.Helper()
	raw, err := sql.Open("sqlite", filepath.Join(dir, DBFile))
	if err != nil {
		t.Fatal(err)
	}
	defer raw.Close()
	raw.SetMaxOpenConns(1)
	return dumpState(t, raw)
}

// rawExec runs statements on agent.db in dir without migrating it, then closes it (checkpointing the WAL).
func rawExec(t *testing.T, dir string, stmts ...string) {
	t.Helper()
	raw, err := sql.Open("sqlite", filepath.Join(dir, DBFile))
	if err != nil {
		t.Fatal(err)
	}
	raw.SetMaxOpenConns(1)
	for _, s := range stmts {
		if _, err := raw.Exec(s); err != nil {
			raw.Close()
			t.Fatalf("%s: %v", s, err)
		}
	}
	if err := raw.Close(); err != nil {
		t.Fatal(err)
	}
}

// freshHeadDir returns a data dir whose database was created and migrated to head, then closed.
func freshHeadDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	db, err := Open(dir)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := db.CreateDevice("dev-1", "phone", "token-1"); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	return dir
}

func readDB(t *testing.T, dir string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, DBFile))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// setMigrateHook installs fn as the before-commit hook for the test and removes it afterwards.
func setMigrateHook(t *testing.T, fn func(step int, tx *sql.Tx) error) {
	t.Helper()
	migrateBeforeCommit.Store(&fn)
	t.Cleanup(func() { migrateBeforeCommit.Store(nil) })
}

// Every migration so far is additive, so a migrated database still opens on any older build: min_compatible 0.
func TestAdditiveChainLeavesMinCompatibleZero(t *testing.T) {
	db, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	uv, minCompat, err := db.SchemaState()
	if err != nil {
		t.Fatal(err)
	}
	if uv != len(migrations) || uv != SchemaVersion() || minCompat != 0 {
		t.Fatalf("user_version=%d min_compatible=%d, want %d and 0", uv, minCompat, len(migrations))
	}
	for i, m := range migrations {
		if m.breaking {
			t.Errorf("migration %d is marked breaking but the shipped chain is additive", i+1)
		}
	}
}

// Migrations run with synchronous=FULL (2), so each step is durable before the next begins.
func TestOpenUsesSynchronousFull(t *testing.T) {
	db, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var sync int
	if err := db.db.QueryRow("PRAGMA synchronous").Scan(&sync); err != nil || sync != 2 {
		t.Fatalf("synchronous = %d (%v), want 2 (FULL)", sync, err)
	}
}

// A database that needs a newer agent is refused with ErrSchemaTooNew and the file is not touched.
func TestOpenRefusesWhenMinCompatibleExceedsSupport(t *testing.T) {
	dir := freshHeadDir(t)
	rawExec(t, dir,
		fmt.Sprintf("PRAGMA user_version = %d", len(migrations)+3),
		fmt.Sprintf("UPDATE schema_meta SET value=%d WHERE key='min_compatible'", len(migrations)+1))
	before := readDB(t, dir)
	db, err := Open(dir)
	if db != nil {
		db.Close()
		t.Fatal("Open returned a database for a schema that is too new")
	}
	if !errors.Is(err, ErrSchemaTooNew) {
		t.Fatalf("err = %v, want ErrSchemaTooNew", err)
	}
	if !strings.Contains(err.Error(), "newer agent") || !strings.Contains(err.Error(), fmt.Sprint(len(migrations)+1)) {
		t.Fatalf("message does not say what to do or which level is needed: %v", err)
	}
	if after := readDB(t, dir); !bytes.Equal(before, after) {
		t.Fatalf("agent.db changed (%d -> %d bytes) although the open was refused", len(before), len(after))
	}
}

// A newer user_version whose min_compatible this build still satisfies (a rollback) opens, runs no
// migration, and changes nothing in the file.
func TestOpenReadsThroughNewerCompatibleDatabase(t *testing.T) {
	dir := freshHeadDir(t)
	future := len(migrations) + 4
	rawExec(t, dir,
		fmt.Sprintf("PRAGMA user_version = %d", future),
		"CREATE TABLE from_the_future (id INTEGER PRIMARY KEY, note TEXT)",
		"INSERT INTO from_the_future (note) VALUES ('newer agent data')")
	before := readDB(t, dir)
	setMigrateHook(t, func(step int, tx *sql.Tx) error {
		t.Errorf("migration step %d ran against a newer, compatible database", step)
		return nil
	})
	db, err := Open(dir)
	if err != nil {
		t.Fatalf("open newer compatible database: %v", err)
	}
	devs, err := db.ListDevices()
	if err != nil || len(devs) != 1 || devs[0].ID != "dev-1" {
		t.Fatalf("devices through the older build = %v, %v", devs, err)
	}
	uv, minCompat, err := db.SchemaState()
	if err != nil || uv != future || minCompat != 0 {
		t.Fatalf("state = %d/%d (%v), want %d/0", uv, minCompat, err, future)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if after := readDB(t, dir); !bytes.Equal(before, after) {
		t.Fatalf("agent.db changed (%d -> %d bytes) by opening a newer compatible database", len(before), len(after))
	}
}

// A step marked breaking raises min_compatible to its index in the same transaction as user_version, and an
// older build (fewer steps) then refuses the file.
func TestBreakingMigrationRaisesMinCompatibleAtomically(t *testing.T) {
	orig := migrations
	t.Cleanup(func() { migrations = orig })
	breakingStep := len(orig) + 1
	migrations = append(append([]migration{}, orig...), migration{
		up:       func(tx *sql.Tx) error { _, err := tx.Exec("DROP TABLE pair_requests"); return err },
		breaking: true,
	})

	// A failure before commit leaves neither the drop, the version nor the new min_compatible behind.
	dir := freshHeadDirOn(t, orig)
	setMigrateHook(t, func(step int, tx *sql.Tx) error {
		if step == breakingStep {
			return errors.New("crash before commit")
		}
		return nil
	})
	if db, err := Open(dir); err == nil {
		db.Close()
		t.Fatal("expected the hooked step to fail")
	}
	migrateBeforeCommit.Store(nil)
	if got := rawDump(t, dir); !strings.Contains(got, fmt.Sprintf("user_version=%d\n", len(orig))) ||
		!strings.Contains(got, "meta min_compatible=0") || !strings.Contains(got, "pair_requests") {
		t.Fatalf("a rolled-back breaking step left state behind:\n%s", got)
	}

	db, err := Open(dir)
	if err != nil {
		t.Fatalf("migrate with the breaking step: %v", err)
	}
	uv, minCompat, err := db.SchemaState()
	db.Close()
	if err != nil || uv != breakingStep || minCompat != breakingStep {
		t.Fatalf("state = %d/%d (%v), want %d/%d", uv, minCompat, err, breakingStep, breakingStep)
	}

	// The build that only knows the original steps must now refuse the file.
	migrations = orig
	before := readDB(t, dir)
	if db, err := Open(dir); !errors.Is(err, ErrSchemaTooNew) {
		if db != nil {
			db.Close()
		}
		t.Fatalf("older build err = %v, want ErrSchemaTooNew", err)
	}
	if !bytes.Equal(before, readDB(t, dir)) {
		t.Fatal("the refusal modified the file")
	}
}

// freshHeadDirOn is freshHeadDir for the chain steps (the package-level chain may be extended by the caller).
func freshHeadDirOn(t *testing.T, steps []migration) string {
	t.Helper()
	saved := migrations
	migrations = steps
	defer func() { migrations = saved }()
	return freshHeadDir(t)
}

// Stopping the chain just before the commit of any step N leaves the database exactly at step N-1 (same
// version, same schema objects, same schema_meta), and the next Open completes it to head.
func TestMigrationCrashAtEveryStep(t *testing.T) {
	// Reference: the state after each step as seen from inside its own transaction on a clean run.
	want := map[int]string{0: "user_version=0\n"}
	setMigrateHook(t, func(step int, tx *sql.Tx) error {
		want[step] = dumpState(t, tx)
		return nil
	})
	clean, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	clean.Close()
	migrateBeforeCommit.Store(nil)
	head := len(migrations)
	if len(want) != head+1 {
		t.Fatalf("recorded %d states, want %d", len(want), head+1)
	}

	for n := 1; n <= head; n++ {
		t.Run(fmt.Sprintf("step%d", n), func(t *testing.T) {
			dir := t.TempDir()
			setMigrateHook(t, func(step int, tx *sql.Tx) error {
				if step == n {
					return fmt.Errorf("simulated crash before commit of step %d", step)
				}
				return nil
			})
			db, err := Open(dir)
			if err == nil {
				db.Close()
				t.Fatal("Open succeeded although the step was aborted")
			}
			if !strings.Contains(err.Error(), "simulated crash") {
				t.Fatalf("unexpected error: %v", err)
			}
			if got := rawDump(t, dir); got != want[n-1] {
				t.Fatalf("after aborting step %d the database is not exactly at step %d:\n--- got\n%s--- want\n%s", n, n-1, got, want[n-1])
			}

			migrateBeforeCommit.Store(nil)
			db, err = Open(dir)
			if err != nil {
				t.Fatalf("reopen after the crash: %v", err)
			}
			uv, minCompat, err := db.SchemaState()
			db.Close()
			if err != nil || uv != head || minCompat != 0 {
				t.Fatalf("after reopen: user_version=%d min_compatible=%d (%v), want %d/0", uv, minCompat, err, head)
			}
			if got := rawDump(t, dir); got != want[head] {
				t.Fatalf("completed database differs from a clean migration:\n--- got\n%s--- want\n%s", got, want[head])
			}
		})
	}
}
