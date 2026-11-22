package legacyfixtures

import (
	"database/sql"
	"fmt"
	"sort"
	"strings"
	"testing"
)

// LatestVersion is the PRAGMA user_version the current store.Open migrates to
// (len(migrations) in store/migrations.go). Fixtures exist for 0..LatestVersion.
const LatestVersion = 12

// schemaV0 is the oldest shape: the original CREATE TABLEs, before any column
// was added by a later patch and before PRAGMA user_version was in use.
// The store's migration list is unexported and cannot be truncated from here,
// so every shape below is written out by hand, mirroring migrations.go.
const schemaV0 = `
CREATE TABLE devices (
    id          TEXT PRIMARY KEY,
    label       TEXT NOT NULL,
    token_hash  TEXT NOT NULL UNIQUE,
    created     INTEGER NOT NULL,
    last_seen   INTEGER NOT NULL,
    revoked     INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE config (
    key   TEXT PRIMARY KEY,
    value TEXT NOT NULL
);
CREATE TABLE pairing_codes (
    code    TEXT PRIMARY KEY,
    expires INTEGER NOT NULL
);
CREATE TABLE transfers (
    id               TEXT PRIMARY KEY,
    target_path      TEXT NOT NULL,
    total_size       INTEGER NOT NULL,
    chunk_size       INTEGER NOT NULL,
    sha256           TEXT NOT NULL,
    received_chunks  TEXT NOT NULL DEFAULT '[]',
    status           TEXT NOT NULL DEFAULT 'open',
    temp_path        TEXT NOT NULL,
    total_chunks     INTEGER NOT NULL
);
CREATE TABLE share_tokens (
    token_hash  TEXT PRIMARY KEY,
    path        TEXT NOT NULL,
    created     INTEGER NOT NULL,
    expires     INTEGER NOT NULL
);
CREATE TABLE share_log (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    token_hash   TEXT NOT NULL,
    path         TEXT NOT NULL,
    minted_at    INTEGER NOT NULL,
    expires_at   INTEGER NOT NULL,
    served_at    INTEGER,
    requester_ip TEXT
);
CREATE TABLE users (
    username      TEXT PRIMARY KEY,
    password_hash TEXT NOT NULL,
    created       INTEGER NOT NULL
);
`

// schemaSteps[i] is the DDL that takes a database from user_version i to i+1,
// copied from migration step i+1 (data-only effects of a step are not applied:
// fixture rows are inserted after the schema, in the shape of that version).
var schemaSteps = [LatestVersion]string{
	// 1: baseline columns and indexes.
	`
ALTER TABLE devices ADD COLUMN client_id TEXT NOT NULL DEFAULT '';
ALTER TABLE devices ADD COLUMN last_address TEXT NOT NULL DEFAULT '';
ALTER TABLE devices ADD COLUMN last_version TEXT NOT NULL DEFAULT '';
ALTER TABLE devices ADD COLUMN jail_root TEXT NOT NULL DEFAULT '';
ALTER TABLE devices ADD COLUMN read_only INTEGER NOT NULL DEFAULT 0;
ALTER TABLE devices ADD COLUMN public_key TEXT NOT NULL DEFAULT '';
ALTER TABLE devices ADD COLUMN via_login INTEGER NOT NULL DEFAULT 0;
ALTER TABLE devices ADD COLUMN username TEXT NOT NULL DEFAULT '';
ALTER TABLE pairing_codes ADD COLUMN jail_root TEXT NOT NULL DEFAULT '';
ALTER TABLE pairing_codes ADD COLUMN read_only INTEGER NOT NULL DEFAULT 0;
ALTER TABLE transfers ADD COLUMN device_id TEXT NOT NULL DEFAULT '';
ALTER TABLE transfers ADD COLUMN updated_at INTEGER NOT NULL DEFAULT 0;
ALTER TABLE transfers ADD COLUMN overwrite INTEGER NOT NULL DEFAULT 0;
ALTER TABLE share_tokens ADD COLUMN device_id TEXT NOT NULL DEFAULT '';
CREATE UNIQUE INDEX idx_devices_client_id ON devices(client_id) WHERE client_id != '';
CREATE INDEX idx_transfers_status_updated ON transfers(status, updated_at);
CREATE INDEX idx_transfers_device_updated ON transfers(device_id, updated_at);
`,
	// 2: one row per received chunk; the JSON column is gone.
	`
CREATE TABLE transfer_chunks (
    transfer_id TEXT NOT NULL,
    chunk_no    INTEGER NOT NULL,
    PRIMARY KEY (transfer_id, chunk_no)
) WITHOUT ROWID;
ALTER TABLE transfers DROP COLUMN received_chunks;
`,
	// 3: audit log.
	`
CREATE TABLE audit_log (
    id     INTEGER PRIMARY KEY AUTOINCREMENT,
    at     INTEGER NOT NULL,
    action TEXT    NOT NULL,
    actor  TEXT    NOT NULL DEFAULT '',
    target TEXT    NOT NULL DEFAULT '',
    detail TEXT    NOT NULL DEFAULT ''
);
`,
	// 4: app grants.
	`
ALTER TABLE devices ADD COLUMN view_apps INTEGER NOT NULL DEFAULT 0;
ALTER TABLE devices ADD COLUMN launch_apps INTEGER NOT NULL DEFAULT 0;
`,
	// 5: login-device binding cleanup is data only; the shape does not change.
	``,
	// 6: file capabilities.
	`
ALTER TABLE devices ADD COLUMN can_browse INTEGER NOT NULL DEFAULT 1;
ALTER TABLE devices ADD COLUMN can_download INTEGER NOT NULL DEFAULT 0;
ALTER TABLE devices ADD COLUMN can_upload INTEGER NOT NULL DEFAULT 0;
ALTER TABLE devices ADD COLUMN can_modify INTEGER NOT NULL DEFAULT 0;
ALTER TABLE devices ADD COLUMN can_delete INTEGER NOT NULL DEFAULT 0;
ALTER TABLE devices ADD COLUMN can_share INTEGER NOT NULL DEFAULT 0;
`,
	// 7: transfer completion time.
	`
ALTER TABLE transfers ADD COLUMN terminal_at INTEGER NOT NULL DEFAULT 0;
CREATE INDEX idx_transfers_terminal_at ON transfers(status, terminal_at);
`,
	// 8: approve-on-PC pair requests.
	`
CREATE TABLE pair_requests (
    id           TEXT PRIMARY KEY,
    label        TEXT NOT NULL,
    client_id    TEXT NOT NULL DEFAULT '',
    public_key   TEXT NOT NULL DEFAULT '',
    client_nonce TEXT NOT NULL,
    remote_ip    TEXT NOT NULL DEFAULT '',
    status       TEXT NOT NULL DEFAULT 'pending',
    created      INTEGER NOT NULL,
    expires      INTEGER NOT NULL
);
CREATE INDEX idx_pair_requests_status ON pair_requests(status, expires);
`,
	// 9: pairing hardening.
	`
ALTER TABLE pairing_codes ADD COLUMN can_register INTEGER NOT NULL DEFAULT 0;
ALTER TABLE pair_requests ADD COLUMN nonce_commit TEXT NOT NULL DEFAULT '';
`,
	// 10: account-server trust record and the login_issuer mark on devices.
	`
CREATE TABLE trusted_account_server (
    id          INTEGER PRIMARY KEY CHECK (id = 1),
    name        TEXT NOT NULL,
    public_key  TEXT NOT NULL,
    fingerprint TEXT NOT NULL,
    created     INTEGER NOT NULL
);
ALTER TABLE devices ADD COLUMN login_issuer TEXT NOT NULL DEFAULT '';
`,
	// 11: federated_login_at, the time an account server last vouched for a device.
	`
ALTER TABLE devices ADD COLUMN federated_login_at INTEGER;
`,
	// 12: account_epoch (the issuer's revocation counter), the highest epoch seen per trusted account
	// server and the epoch each federated device was minted under.
	`
CREATE TABLE account_epoch (
    id    INTEGER PRIMARY KEY CHECK (id = 1),
    epoch INTEGER NOT NULL DEFAULT 0
);
INSERT INTO account_epoch (id, epoch) VALUES (1, 0);
ALTER TABLE trusted_account_server ADD COLUMN highest_epoch INTEGER NOT NULL DEFAULT 0;
ALTER TABLE devices ADD COLUMN federated_epoch INTEGER NOT NULL DEFAULT 0;
`,
}

// createSchema builds an empty database in the shape of the given
// PRAGMA user_version by applying the hand-written DDL steps up to it.
func createSchema(t testing.TB, db *sql.DB, version int) {
	t.Helper()
	if _, err := db.Exec(schemaV0); err != nil {
		t.Fatalf("legacyfixtures: create v0 schema: %v", err)
	}
	for i := 0; i < version; i++ {
		if schemaSteps[i] == "" {
			continue
		}
		if _, err := db.Exec(schemaSteps[i]); err != nil {
			t.Fatalf("legacyfixtures: schema step %d: %v", i+1, err)
		}
	}
	if _, err := db.Exec(fmt.Sprintf(`PRAGMA user_version = %d`, version)); err != nil {
		t.Fatalf("legacyfixtures: set user_version: %v", err)
	}
}

// insertRow inserts one row, keeping only the columns this version of the
// table has. That lets one description of a row serve every schema version:
// columns added by later migration steps are silently left out of old shapes.
func insertRow(t testing.TB, db *sql.DB, table string, row map[string]any) {
	t.Helper()
	rows, err := db.Query(`SELECT name FROM pragma_table_info(?)`, table)
	if err != nil {
		t.Fatalf("legacyfixtures: columns of %s: %v", table, err)
	}
	have := map[string]bool{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			rows.Close()
			t.Fatalf("legacyfixtures: columns of %s: %v", table, err)
		}
		have[name] = true
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		t.Fatalf("legacyfixtures: columns of %s: %v", table, err)
	}
	rows.Close()

	var cols []string
	for name := range row {
		if have[name] {
			cols = append(cols, name)
		}
	}
	sort.Strings(cols)
	args := make([]any, len(cols))
	for i, c := range cols {
		args[i] = row[c]
	}
	marks := strings.TrimSuffix(strings.Repeat("?,", len(cols)), ",")
	// table and column names come from this package's own literals, never from input.
	q := fmt.Sprintf(`INSERT INTO %s (%s) VALUES (%s)`, table, strings.Join(cols, ","), marks)
	if _, err := db.Exec(q, args...); err != nil {
		t.Fatalf("legacyfixtures: insert into %s: %v", table, err)
	}
}
