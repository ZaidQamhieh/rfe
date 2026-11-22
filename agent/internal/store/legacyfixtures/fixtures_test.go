package legacyfixtures

import (
	"database/sql"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/ZaidQamhieh/rfe/agent/internal/store"
)

// update rewrites testdata/v<N>.json from the expectations computed in Go.
// Review the diff by hand: the golden files are the contract a future importer
// is tested against.
var update = flag.Bool("update", false, "rewrite the golden JSON files")

// goldenPath is the checked-in expectation for one fixture version.
func goldenPath(version int) string {
	return filepath.Join("testdata", fmt.Sprintf("v%d.json", version))
}

// rawDB opens an agent.db for plain SQL inspection, without migrating it.
func rawDB(t *testing.T, path string) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { db.Close() })
	return db
}

// count runs a SELECT COUNT(*) style query and returns the number.
func count(t *testing.T, db *sql.DB, query string, args ...any) int {
	t.Helper()
	var n int
	if err := db.QueryRow(query, args...).Scan(&n); err != nil {
		t.Fatalf("%s: %v", query, err)
	}
	return n
}

// TestFixtureShapes proves each fixture really is the old schema it claims:
// PRAGMA user_version matches and the tables/columns each migration step adds
// are present only from that step on.
func TestFixtureShapes(t *testing.T) {
	for v := 0; v <= LatestVersion; v++ {
		t.Run(fmt.Sprintf("v%d", v), func(t *testing.T) {
			f := Build(t, t.TempDir(), v)
			db := rawDB(t, f.DBPath)
			if got := count(t, db, `PRAGMA user_version`); got != v {
				t.Fatalf("user_version = %d, want %d", got, v)
			}
			col := func(table, column string) bool {
				return count(t, db, `SELECT COUNT(*) FROM pragma_table_info(?) WHERE name=?`, table, column) > 0
			}
			table := func(name string) bool {
				return count(t, db, `SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?`, name) > 0
			}
			checks := []struct {
				what string
				got  bool
				want bool
			}{
				{"devices.client_id", col("devices", "client_id"), v >= 1},
				{"transfers.received_chunks", col("transfers", "received_chunks"), v < 2},
				{"transfer_chunks", table("transfer_chunks"), v >= 2},
				{"audit_log", table("audit_log"), v >= 3},
				{"devices.view_apps", col("devices", "view_apps"), v >= 4},
				{"devices.can_browse", col("devices", "can_browse"), v >= 6},
				{"transfers.terminal_at", col("transfers", "terminal_at"), v >= 7},
				{"pair_requests", table("pair_requests"), v >= 8},
				{"pairing_codes.can_register", col("pairing_codes", "can_register"), v >= 9},
				{"trusted_account_server", table("trusted_account_server"), v >= 10},
				{"devices.login_issuer", col("devices", "login_issuer"), v >= 10},
				{"devices.federated_login_at", col("devices", "federated_login_at"), v >= 11},
				{"account_epoch", table("account_epoch"), v >= 12},
				{"trusted_account_server.highest_epoch", col("trusted_account_server", "highest_epoch"), v >= 12},
				{"devices.federated_epoch", col("devices", "federated_epoch"), v >= 12},
			}
			for _, c := range checks {
				if c.got != c.want {
					t.Errorf("%s present = %v, want %v", c.what, c.got, c.want)
				}
			}
			for _, u := range f.Expected.Uploads {
				if u.TempFile == nil {
					continue
				}
				fi, err := os.Stat(filepath.Join(f.DataDir, filepath.FromSlash(u.TempFile.Name)))
				if err != nil {
					t.Fatalf("temp file for %s: %v", u.ID, err)
				}
				if fi.Size() != u.TempFile.Size {
					t.Errorf("temp file for %s is %d bytes, want %d", u.ID, fi.Size(), u.TempFile.Size)
				}
			}
		})
	}
}

// TestFixtureMigratesToGolden opens every fixture with the current store.Open,
// checks each fixture token through DeviceByToken, and compares the rows the
// migrated database holds with the checked-in golden JSON.
func TestFixtureMigratesToGolden(t *testing.T) {
	for v := 0; v <= LatestVersion; v++ {
		t.Run(fmt.Sprintf("v%d", v), func(t *testing.T) {
			f := Build(t, t.TempDir(), v)
			if *update {
				writeGolden(t, f.Expected)
			}
			golden := readGolden(t, v)
			if !reflect.DeepEqual(golden, f.Expected) {
				t.Fatalf("computed expectations differ from %s (run with -update and review the diff)", goldenPath(v))
			}

			st, err := store.Open(f.DataDir)
			if err != nil {
				t.Fatalf("store.Open: %v", err)
			}
			checkTokens(t, st, golden)
			checkSharesAndChunks(t, st, golden)
			if err := st.Close(); err != nil {
				t.Fatalf("close: %v", err)
			}

			db := rawDB(t, f.DBPath)
			if got := count(t, db, `PRAGMA user_version`); got != store.SchemaVersion() {
				t.Fatalf("user_version after Open = %d, store head is %d", got, store.SchemaVersion())
			}
			observed := observe(t, db, f.DataDir, golden)
			compareSections(t, golden, observed)
		})
	}
}

// checkTokens authenticates every fixture token the way the server does and
// checks the stored grants, key pin and flags that come back.
func checkTokens(t *testing.T, st *store.DB, want Expected) {
	t.Helper()
	for _, w := range want.Devices {
		d, err := st.DeviceByToken(w.Token)
		if err != nil {
			t.Fatalf("DeviceByToken(%s): %v", w.ID, err)
		}
		if !w.Authenticates {
			if d != nil {
				t.Errorf("device %s must not authenticate, got %+v", w.ID, d)
			}
			continue
		}
		if d == nil {
			t.Errorf("device %s: token did not authenticate", w.ID)
			continue
		}
		got := DeviceRow{
			ID: d.ID, Label: d.Label, TokenHash: d.TokenHash, Created: d.Created.Unix(), LastSeen: d.LastSeen.Unix(),
			Revoked: d.Revoked, LastAddress: d.LastAddress, LastVersion: d.LastVersion, JailRoot: d.JailRoot,
			ReadOnly: d.ReadOnly, PublicKey: d.PublicKey, ViaLogin: d.ViaLogin, ViewApps: d.ViewApps,
			LaunchApps: d.LaunchApps, CanBrowse: d.CanBrowse, CanDownload: d.CanDownload, CanUpload: d.CanUpload,
			CanModify: d.CanModify, CanDelete: d.CanDelete, CanShare: d.CanShare,
		}
		// Fields the Device struct does not carry are compared in observe().
		w.ClientID, w.Username, w.Token, w.Authenticates = "", "", "", false
		if !reflect.DeepEqual(got, w) {
			t.Errorf("device %s via DeviceByToken:\n got  %+v\n want %+v", w.ID, got, w)
		}
	}
	if d, err := st.DeviceByToken("fixture-token-that-was-never-issued"); err != nil || d != nil {
		t.Errorf("unknown token authenticated: %+v, %v", d, err)
	}
}

// checkSharesAndChunks reads shares and upload chunk rows through the store API.
func checkSharesAndChunks(t *testing.T, st *store.DB, want Expected) {
	t.Helper()
	for _, s := range want.Shares {
		got, err := st.GetShareToken(s.TokenHash)
		if err != nil || got == nil {
			t.Errorf("share %s not found: %v", s.Path, err)
			continue
		}
		if got.Path != s.Path || got.DeviceID != s.DeviceID || got.Expires.Unix() != s.Expires {
			t.Errorf("share %s: got %+v, want %+v", s.Path, got, s)
		}
	}
	for _, u := range want.Uploads {
		got, err := st.ChunkNumbers(u.ID)
		if err != nil {
			t.Fatalf("ChunkNumbers(%s): %v", u.ID, err)
		}
		if len(got) != len(u.Chunks) || (len(got) > 0 && !reflect.DeepEqual(got, u.Chunks)) {
			t.Errorf("chunks of %s = %v, want %v", u.ID, got, u.Chunks)
		}
	}
}

// observe reads every imported table of a migrated agent.db with plain SQL.
// Plaintext tokens and the authenticates flag are not stored in the database;
// they are copied from the golden so the sections compare as a whole.
func observe(t *testing.T, db *sql.DB, dataDir string, golden Expected) Expected {
	t.Helper()
	out := Expected{Version: golden.Version, MigratedUserVersion: golden.MigratedUserVersion, Config: map[string]string{}}
	query := func(q string, args []any, scan func(*sql.Rows) error) {
		t.Helper()
		rows, err := db.Query(q, args...)
		if err != nil {
			t.Fatalf("%s: %v", q, err)
		}
		defer rows.Close()
		for rows.Next() {
			if err := scan(rows); err != nil {
				t.Fatalf("%s: scan: %v", q, err)
			}
		}
		if err := rows.Err(); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	query(`SELECT username,password_hash,created FROM users ORDER BY username`, nil, func(r *sql.Rows) error {
		var u UserRow
		err := r.Scan(&u.Username, &u.PasswordHash, &u.Created)
		out.Users = append(out.Users, u)
		return err
	})
	devices := map[string]DeviceRow{}
	for _, d := range golden.Devices {
		devices[d.ID] = d
	}
	query(`SELECT id,label,token_hash,created,last_seen,revoked,client_id,last_address,last_version,jail_root,read_only,
public_key,via_login,username,view_apps,launch_apps,can_browse,can_download,can_upload,can_modify,can_delete,can_share
FROM devices ORDER BY id`, nil, func(r *sql.Rows) error {
		var d DeviceRow
		err := r.Scan(&d.ID, &d.Label, &d.TokenHash, &d.Created, &d.LastSeen, &d.Revoked, &d.ClientID, &d.LastAddress,
			&d.LastVersion, &d.JailRoot, &d.ReadOnly, &d.PublicKey, &d.ViaLogin, &d.Username, &d.ViewApps, &d.LaunchApps,
			&d.CanBrowse, &d.CanDownload, &d.CanUpload, &d.CanModify, &d.CanDelete, &d.CanShare)
		d.Token, d.Authenticates = devices[d.ID].Token, devices[d.ID].Authenticates
		out.Devices = append(out.Devices, d)
		return err
	})
	shareTokens := map[string]string{}
	for _, s := range golden.Shares {
		shareTokens[s.TokenHash] = s.Token
	}
	query(`SELECT token_hash,path,created,expires,device_id FROM share_tokens ORDER BY path`, nil, func(r *sql.Rows) error {
		var s ShareRow
		err := r.Scan(&s.TokenHash, &s.Path, &s.Created, &s.Expires, &s.DeviceID)
		s.Token = shareTokens[s.TokenHash]
		out.Shares = append(out.Shares, s)
		return err
	})
	query(`SELECT id,token_hash,path,minted_at,expires_at,served_at,requester_ip FROM share_log ORDER BY id`, nil, func(r *sql.Rows) error {
		var s ShareLogRow
		err := r.Scan(&s.ID, &s.TokenHash, &s.Path, &s.MintedAt, &s.ExpiresAt, &s.ServedAt, &s.RequesterIP)
		out.ShareLog = append(out.ShareLog, s)
		return err
	})
	wantUploads := map[string]UploadRow{}
	for _, u := range golden.Uploads {
		wantUploads[u.ID] = u
	}
	query(`SELECT id,target_path,total_size,chunk_size,total_chunks,sha256,status,temp_path,device_id,updated_at,overwrite,terminal_at
FROM transfers ORDER BY id`, nil, func(r *sql.Rows) error {
		var u UploadRow
		var tempPath string
		err := r.Scan(&u.ID, &u.TargetPath, &u.TotalSize, &u.ChunkSize, &u.TotalChunks, &u.SHA256, &u.Status, &tempPath,
			&u.DeviceID, &u.UpdatedAt, &u.Overwrite, &u.TerminalAt)
		if err != nil {
			return err
		}
		rel, relErr := filepath.Rel(dataDir, tempPath)
		if relErr != nil {
			return relErr
		}
		u.TempPath = filepath.ToSlash(rel)
		if fi, statErr := os.Stat(tempPath); statErr == nil {
			u.TempFile = &TempFile{Name: u.TempPath, Size: fi.Size()}
		}
		// terminal_at is stamped with the migration time for rows that were
		// already finished: check that it is set, then compare as "set".
		if w := wantUploads[u.ID]; w.TerminalAtSetAtMigration {
			if u.TerminalAt <= 0 {
				t.Errorf("upload %s: terminal_at = %d, want the migration time", u.ID, u.TerminalAt)
			}
			u.TerminalAt, u.TerminalAtSetAtMigration = 0, true
		}
		out.Uploads = append(out.Uploads, u)
		return nil
	})
	for i := range out.Uploads {
		query(`SELECT chunk_no FROM transfer_chunks WHERE transfer_id=? ORDER BY chunk_no`, []any{out.Uploads[i].ID}, func(r *sql.Rows) error {
			var n int
			err := r.Scan(&n)
			out.Uploads[i].Chunks = append(out.Uploads[i].Chunks, n)
			return err
		})
	}
	query(`SELECT id,at,action,actor,target,detail FROM audit_log ORDER BY id`, nil, func(r *sql.Rows) error {
		var a AuditRow
		err := r.Scan(&a.ID, &a.At, &a.Action, &a.Actor, &a.Target, &a.Detail)
		out.Audit = append(out.Audit, a)
		return err
	})
	query(`SELECT key,value FROM config`, nil, func(r *sql.Rows) error {
		var k, v string
		err := r.Scan(&k, &v)
		out.Config[k] = v
		return err
	})
	out.NotImported = NotImported{
		PairingCodes: count(t, db, `SELECT COUNT(*) FROM pairing_codes`),
		PairRequests: count(t, db, `SELECT COUNT(*) FROM pair_requests`),
	}
	out.Counts = map[string]int{
		"users": len(out.Users), "devices": len(out.Devices), "share_tokens": len(out.Shares),
		"share_log": len(out.ShareLog), "uploads": len(out.Uploads), "audit_log": len(out.Audit),
		"settings": len(out.Config),
	}
	for _, u := range out.Uploads {
		out.Counts["upload_chunks"] += len(u.Chunks)
	}
	return out
}

// compareSections reports each differing section of the golden and the
// observed rows separately, so a failure names the table.
func compareSections(t *testing.T, want, got Expected) {
	t.Helper()
	sections := []struct {
		name      string
		want, got any
	}{
		{"counts", want.Counts, got.Counts},
		{"users", want.Users, got.Users},
		{"devices", want.Devices, got.Devices},
		{"shares", want.Shares, got.Shares},
		{"share_log", want.ShareLog, got.ShareLog},
		{"uploads", want.Uploads, got.Uploads},
		{"audit_log", want.Audit, got.Audit},
		{"settings", want.Config, got.Config},
		{"not_imported", want.NotImported, got.NotImported},
	}
	for _, s := range sections {
		if reflect.DeepEqual(s.want, s.got) {
			continue
		}
		w, _ := json.MarshalIndent(s.want, "", " ")
		g, _ := json.MarshalIndent(s.got, "", " ")
		t.Errorf("section %s differs\nwant %s\n got %s", s.name, w, g)
	}
}

// readGolden loads and decodes testdata/v<N>.json.
func readGolden(t *testing.T, version int) Expected {
	t.Helper()
	raw, err := os.ReadFile(goldenPath(version))
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}
	var e Expected
	if err := json.Unmarshal(raw, &e); err != nil {
		t.Fatalf("decode %s: %v", goldenPath(version), err)
	}
	return e
}

// writeGolden stores the computed expectations as indented JSON.
func writeGolden(t *testing.T, e Expected) {
	t.Helper()
	raw, err := json.MarshalIndent(e, "", "  ")
	if err != nil {
		t.Fatalf("encode golden: %v", err)
	}
	if err := os.MkdirAll("testdata", 0o755); err != nil {
		t.Fatalf("mkdir testdata: %v", err)
	}
	if err := os.WriteFile(goldenPath(e.Version), append(raw, '\n'), 0o644); err != nil {
		t.Fatalf("write golden: %v", err)
	}
}
