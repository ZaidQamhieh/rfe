// Package legacyfixtures builds old-format agent.db files (PRAGMA user_version
// 0 through 9) filled with realistic rows, for testing a one-time importer of
// the old store. All names, tokens and hashes are fake test values.
package legacyfixtures

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"testing"

	_ "modernc.org/sqlite" // pure-Go SQLite driver, the same one the store uses
)

// Fixed instants (unix seconds) so the files and golden JSON never change.
const (
	epoch     = 1700000000
	farFuture = 4102444800 // 2100-01-01: a share token that never expires in a test
)

// Obviously fake bcrypt-shaped strings: the importer must copy them byte for byte.
const (
	hashAlice = "$2a$10$FAKEFIXTUREHASHALICEaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	hashBob   = "$2a$10$FAKEFIXTUREHASHBOBbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
)

// Fixture is one built legacy data directory plus what an importer must find.
type Fixture struct {
	Version     int               // PRAGMA user_version of the file as written
	DataDir     string            // directory holding agent.db and transfers/
	DBPath      string            // DataDir/agent.db
	Tokens      map[string]string // device id -> plaintext bearer token
	ShareTokens map[string]string // share path -> plaintext share token
	PairingCode string            // plaintext pairing code (never imported)
	Expected    Expected          // rows as the migrated (latest old) shape holds them
}

// Expected is the golden content: what the database holds after the current
// store.Open has migrated the fixture up to MigratedUserVersion.
type Expected struct {
	Version             int               `json:"version"`
	MigratedUserVersion int               `json:"migrated_user_version"`
	Counts              map[string]int    `json:"counts"`
	Users               []UserRow         `json:"users"`
	Devices             []DeviceRow       `json:"devices"`
	Shares              []ShareRow        `json:"shares"`
	ShareLog            []ShareLogRow     `json:"share_log"`
	Uploads             []UploadRow       `json:"uploads"`
	Audit               []AuditRow        `json:"audit_log"`
	Config              map[string]string `json:"settings"`
	NotImported         NotImported       `json:"not_imported"`
}

// UserRow is one login account; the password hash must be copied unchanged.
type UserRow struct {
	Username     string `json:"username"`
	PasswordHash string `json:"password_hash"`
	Created      int64  `json:"created"`
}

// DeviceRow is one paired device. Token is the plaintext fixture token and
// Authenticates says whether DeviceByToken returns the device for it.
type DeviceRow struct {
	ID            string `json:"id"`
	Label         string `json:"label"`
	Token         string `json:"token"`
	TokenHash     string `json:"token_hash"`
	Authenticates bool   `json:"authenticates"`
	Created       int64  `json:"created"`
	LastSeen      int64  `json:"last_seen"`
	Revoked       bool   `json:"revoked"`
	ClientID      string `json:"client_id"`
	LastAddress   string `json:"last_address"`
	LastVersion   string `json:"last_version"`
	JailRoot      string `json:"jail_root"`
	ReadOnly      bool   `json:"read_only"`
	PublicKey     string `json:"public_key"`
	ViaLogin      bool   `json:"via_login"`
	Username      string `json:"username"`
	ViewApps      bool   `json:"view_apps"`
	LaunchApps    bool   `json:"launch_apps"`
	CanBrowse     bool   `json:"can_browse"`
	CanDownload   bool   `json:"can_download"`
	CanUpload     bool   `json:"can_upload"`
	CanModify     bool   `json:"can_modify"`
	CanDelete     bool   `json:"can_delete"`
	CanShare      bool   `json:"can_share"`
}

// ShareRow is one share token (only its SHA-256 hash is stored).
type ShareRow struct {
	Token     string `json:"token"`
	TokenHash string `json:"token_hash"`
	Path      string `json:"path"`
	Created   int64  `json:"created"`
	Expires   int64  `json:"expires"`
	DeviceID  string `json:"device_id"`
}

// ShareLogRow is one share mint/serve history row.
type ShareLogRow struct {
	ID          int64   `json:"id"`
	TokenHash   string  `json:"token_hash"`
	Path        string  `json:"path"`
	MintedAt    int64   `json:"minted_at"`
	ExpiresAt   int64   `json:"expires_at"`
	ServedAt    *int64  `json:"served_at"`
	RequesterIP *string `json:"requester_ip"`
}

// TempFile is the partial upload file kept under <data>/transfers/.
type TempFile struct {
	Name string `json:"name"` // slash path relative to the data dir
	Size int64  `json:"size"`
}

// UploadRow is one upload session (old table `transfers`) with its received
// chunk numbers (old table `transfer_chunks`) and its temp file, if any.
// A session finished before user_version 7 gets terminal_at at migration time,
// recorded as TerminalAtSetAtMigration instead of a value.
type UploadRow struct {
	ID                       string    `json:"id"`
	TargetPath               string    `json:"target_path"`
	TotalSize                int64     `json:"total_size"`
	ChunkSize                int       `json:"chunk_size"`
	TotalChunks              int       `json:"total_chunks"`
	SHA256                   string    `json:"sha256"`
	Status                   string    `json:"status"`
	TempPath                 string    `json:"temp_path"` // relative to the data dir
	DeviceID                 string    `json:"device_id"`
	UpdatedAt                int64     `json:"updated_at"`
	Overwrite                bool      `json:"overwrite"`
	TerminalAt               int64     `json:"terminal_at"`
	TerminalAtSetAtMigration bool      `json:"terminal_at_set_at_migration"`
	Chunks                   []int     `json:"chunks"`
	TempFile                 *TempFile `json:"temp_file"`
}

// AuditRow is one audit entry; its id must survive so paging cursors still work.
type AuditRow struct {
	ID     int64  `json:"id"`
	At     int64  `json:"at"`
	Action string `json:"action"`
	Actor  string `json:"actor"`
	Target string `json:"target"`
	Detail string `json:"detail"`
}

// NotImported counts rows that exist in the old file but must not be carried over.
type NotImported struct {
	PairingCodes int `json:"pairing_codes"`
	PairRequests int `json:"pair_requests"`
}

// devSpec describes one device with every column of the newest shape; Build
// writes only the columns the requested version has.
type devSpec struct {
	id, label                             string
	created, lastSeen                     int64
	revoked                               bool
	clientID, lastAddress, lastVersion    string
	jailRoot                              string
	readOnly                              bool
	publicKey                             string
	viaLogin                              bool
	username                              string
	viewApps, launchApps                  bool
	browse, download, upload, modify, del bool
	share                                 bool
}

// devSpecs covers: a code-paired phone with narrow grants and a jail, two login
// (admin) devices of one account (empty client_id, so the unique index does not
// merge them), a read-only guest, a revoked device, and a login device whose
// account does not exist.
var devSpecs = []devSpec{
	{id: "dev-guest", label: "Guest tablet", created: epoch + 200, lastSeen: epoch + 9100, clientID: "client-guest",
		lastAddress: "192.0.2.11", lastVersion: "1.0.0", jailRoot: "/srv/share/guest", readOnly: true,
		publicKey: "fixture-pubkey-guest", browse: true, download: true},
	{id: "dev-orphan", label: "Orphaned admin", created: epoch + 500, lastSeen: epoch + 9400,
		lastAddress: "192.0.2.14", lastVersion: "0.9.0", viaLogin: true, username: "ghost",
		browse: true, download: true, upload: true, modify: true, del: true, share: true},
	{id: "dev-owner", label: "Owner phone", created: epoch + 300, lastSeen: epoch + 9200, clientID: "client-owner",
		lastAddress: "192.0.2.12", lastVersion: "1.0.0", publicKey: "fixture-pubkey-owner", viaLogin: true,
		username: "alice", viewApps: true, launchApps: true,
		browse: true, download: true, upload: true, modify: true, del: true, share: true},
	{id: "dev-owner-b", label: "Owner laptop", created: epoch + 400, lastSeen: epoch + 9300,
		lastAddress: "192.0.2.13", lastVersion: "1.0.0", viaLogin: true, username: "alice",
		browse: true, download: true, upload: true, modify: true, del: true, share: true},
	{id: "dev-phone", label: "Phone", created: epoch + 100, lastSeen: epoch + 9000, clientID: "client-phone",
		lastAddress: "192.0.2.10", lastVersion: "1.0.0", jailRoot: "/srv/share/phone",
		publicKey: "fixture-pubkey-phone", viewApps: true,
		browse: true, download: true, upload: true, share: true},
	{id: "dev-revoked", label: "Lost phone", created: epoch + 50, lastSeen: epoch + 8000, revoked: true,
		clientID: "client-revoked", lastAddress: "192.0.2.15", lastVersion: "0.8.0",
		publicKey: "fixture-pubkey-revoked", browse: true, download: true},
}

// uploadSpec describes one upload session; tempBytes < 0 means no temp file.
type uploadSpec struct {
	id, status, deviceID       string
	total                      int64
	totalChunks                int
	updatedAt, terminalAt      int64
	overwrite                  bool
	chunks                     []int
	rawChunkJSON               string // unreadable progress, written only before user_version 2
	tempBytes                  int
	onlyBelowChunkTableVersion bool
}

// uploadChunkSize is small so temp files stay a few bytes long.
const uploadChunkSize = 8

// uploadSpecs covers open sessions with progress, chunk rows beyond the real
// temp file (L1), updated_at = 0 (L2), an empty device id (L3), finished
// sessions, an unknown status (L4) and unreadable chunk progress.
var uploadSpecs = []uploadSpec{
	{id: "up-bad-json", status: "open", deviceID: "dev-phone", total: 16, totalChunks: 2, updatedAt: epoch + 8050,
		rawChunkJSON: "not-json", tempBytes: 8, onlyBelowChunkTableVersion: true},
	{id: "up-done", status: "completed", deviceID: "dev-phone", total: 24, totalChunks: 3, updatedAt: epoch + 8400,
		terminalAt: epoch + 8500, chunks: []int{0, 1, 2}, tempBytes: -1},
	{id: "up-failed", status: "failed", deviceID: "dev-owner", total: 24, totalChunks: 3, updatedAt: epoch + 8500,
		terminalAt: epoch + 8600, chunks: []int{0}, tempBytes: -1},
	{id: "up-legacy-nodevice", status: "open", deviceID: "", total: 16, totalChunks: 2, updatedAt: 0,
		chunks: []int{0}, tempBytes: uploadChunkSize},
	{id: "up-odd-status", status: "paused", deviceID: "dev-phone", total: 16, totalChunks: 2, updatedAt: epoch + 8200,
		tempBytes: 0},
	{id: "up-open-beyond", status: "open", deviceID: "dev-owner", total: 24, totalChunks: 3, updatedAt: epoch + 8100,
		chunks: []int{0, 1, 2}, tempBytes: 2 * uploadChunkSize},
	{id: "up-open-resume", status: "open", deviceID: "dev-phone", total: 24, totalChunks: 3, updatedAt: epoch + 8000,
		overwrite: true, chunks: []int{0, 1}, tempBytes: 2 * uploadChunkSize},
	{id: "up-open-zero-updated", status: "open", deviceID: "dev-phone", total: 16, totalChunks: 2, updatedAt: 0,
		tempBytes: 0},
}

// token returns the fake plaintext bearer token of a device id.
func token(id string) string { return "fixture-token-" + id }

// sha256Hex is the hex SHA-256 the store keeps for tokens (store/devices.go hashToken).
func sha256Hex(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

// terminal reports whether an upload status is one the retention step stamps.
func terminal(status string) bool { return status == "completed" || status == "failed" }

// b2i is the 0/1 SQLite stores for a bool.
func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}

// Build writes a legacy data directory under dir whose agent.db has exactly
// PRAGMA user_version = version (0..LatestVersion), fills it with fixture rows
// and temp files, and returns the paths, plaintext tokens and expected values.
func Build(t testing.TB, dir string, version int) Fixture {
	t.Helper()
	if version < 0 || version > LatestVersion {
		t.Fatalf("legacyfixtures: version %d outside 0..%d", version, LatestVersion)
	}
	f := Fixture{
		Version: version, DataDir: dir, DBPath: filepath.Join(dir, "agent.db"),
		Tokens: map[string]string{}, ShareTokens: map[string]string{}, PairingCode: "FXTCODE1",
	}
	if err := os.MkdirAll(filepath.Join(dir, "transfers"), 0o755); err != nil {
		t.Fatalf("legacyfixtures: mkdir: %v", err)
	}
	db, err := sql.Open("sqlite", f.DBPath)
	if err != nil {
		t.Fatalf("legacyfixtures: open: %v", err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	createSchema(t, db, version)

	exp := Expected{Version: version, MigratedUserVersion: LatestVersion, Config: map[string]string{}}
	users := []UserRow{
		{Username: "alice", PasswordHash: hashAlice, Created: epoch},
		{Username: "bob", PasswordHash: hashBob, Created: epoch + 10},
	}
	known := map[string]bool{}
	for _, u := range users {
		insertRow(t, db, "users", map[string]any{"username": u.Username, "password_hash": u.PasswordHash, "created": u.Created})
		known[u.Username] = true
	}
	exp.Users = users

	for _, d := range devSpecs {
		tok := token(d.id)
		f.Tokens[d.id] = tok
		insertRow(t, db, "devices", map[string]any{
			"id": d.id, "label": d.label, "token_hash": sha256Hex(tok), "created": d.created, "last_seen": d.lastSeen,
			"revoked": b2i(d.revoked), "client_id": d.clientID, "last_address": d.lastAddress, "last_version": d.lastVersion,
			"jail_root": d.jailRoot, "read_only": b2i(d.readOnly), "public_key": d.publicKey, "via_login": b2i(d.viaLogin),
			"username": d.username, "view_apps": b2i(d.viewApps), "launch_apps": b2i(d.launchApps),
			"can_browse": b2i(d.browse), "can_download": b2i(d.download), "can_upload": b2i(d.upload),
			"can_modify": b2i(d.modify), "can_delete": b2i(d.del), "can_share": b2i(d.share),
		})
		exp.Devices = append(exp.Devices, expectedDevice(d, version, known))
	}

	insertShares(t, db, &f, &exp, version)
	insertUploads(t, db, &f, &exp, version)
	insertAuditAndConfig(t, db, &exp, version)
	insertPairing(t, db, &f, &exp, version)

	sort.Slice(exp.Devices, func(i, j int) bool { return exp.Devices[i].ID < exp.Devices[j].ID })
	exp.Counts = map[string]int{
		"users": len(exp.Users), "devices": len(exp.Devices), "share_tokens": len(exp.Shares),
		"share_log": len(exp.ShareLog), "uploads": len(exp.Uploads), "audit_log": len(exp.Audit),
		"settings": len(exp.Config),
	}
	for _, u := range exp.Uploads {
		exp.Counts["upload_chunks"] += len(u.Chunks)
	}
	f.Expected = exp
	return f
}

// expectedDevice computes what a device row looks like once the current
// migrations have run over the fixture of the given version: columns the old
// shape lacked take their defaults, grants are backfilled by step 6, and
// login devices without an account are revoked by step 5.
func expectedDevice(d devSpec, version int, users map[string]bool) DeviceRow {
	tok := token(d.id)
	row := DeviceRow{ID: d.id, Label: d.label, Token: tok, TokenHash: sha256Hex(tok),
		Created: d.created, LastSeen: d.lastSeen, Revoked: d.revoked}
	if version >= 1 {
		row.ClientID, row.LastAddress, row.LastVersion, row.JailRoot = d.clientID, d.lastAddress, d.lastVersion, d.jailRoot
		row.ReadOnly, row.PublicKey, row.ViaLogin, row.Username = d.readOnly, d.publicKey, d.viaLogin, d.username
	}
	if version >= 4 {
		row.ViewApps, row.LaunchApps = d.viewApps, d.launchApps
	}
	if version >= 6 {
		row.CanBrowse, row.CanDownload, row.CanUpload = d.browse, d.download, d.upload
		row.CanModify, row.CanDelete, row.CanShare = d.modify, d.del, d.share
	} else {
		// Step 6 keeps the old full-file access; read-only stays a write guard.
		row.CanBrowse, row.CanDownload, row.CanShare = true, true, true
		row.CanUpload, row.CanModify, row.CanDelete = !row.ReadOnly, !row.ReadOnly, !row.ReadOnly
	}
	orphan := func() bool { return row.ViaLogin && (row.Username == "" || !users[row.Username]) }
	if version >= 1 && version < 5 && orphan() {
		row.Revoked, row.ViaLogin = true, false
	}
	row.Authenticates = !orphan()
	return row
}

// insertShares writes two share tokens (one minted by a device, one legacy
// without an owner) and their log rows, and records the expected values.
func insertShares(t testing.TB, db *sql.DB, f *Fixture, exp *Expected, version int) {
	t.Helper()
	shares := []struct{ path, device string }{
		{"/srv/share/b.txt", ""},
		{"/srv/share/phone/a.txt", "dev-phone"},
	}
	for i, s := range shares {
		tok := "fixture-share-" + string(rune('a'+i))
		f.ShareTokens[s.path] = tok
		hash := sha256Hex(tok)
		created := int64(epoch + 6000 + i)
		insertRow(t, db, "share_tokens", map[string]any{
			"token_hash": hash, "path": s.path, "created": created, "expires": int64(farFuture), "device_id": s.device,
		})
		row := ShareRow{Token: tok, TokenHash: hash, Path: s.path, Created: created, Expires: farFuture}
		if version >= 1 {
			row.DeviceID = s.device
		}
		exp.Shares = append(exp.Shares, row)
	}
	served, ip := int64(epoch+6900), "192.0.2.10"
	logs := []ShareLogRow{
		{ID: 4, TokenHash: sha256Hex("fixture-share-b"), Path: "/srv/share/phone/a.txt", MintedAt: epoch + 6001,
			ExpiresAt: farFuture, ServedAt: &served, RequesterIP: &ip},
		{ID: 9, TokenHash: sha256Hex("fixture-share-a"), Path: "/srv/share/b.txt", MintedAt: epoch + 6000, ExpiresAt: farFuture},
	}
	for _, l := range logs {
		insertRow(t, db, "share_log", map[string]any{
			"id": l.ID, "token_hash": l.TokenHash, "path": l.Path, "minted_at": l.MintedAt, "expires_at": l.ExpiresAt,
			"served_at": l.ServedAt, "requester_ip": l.RequesterIP,
		})
	}
	exp.ShareLog = logs
	sort.Slice(exp.Shares, func(i, j int) bool { return exp.Shares[i].Path < exp.Shares[j].Path })
}

// insertUploads writes the upload sessions, their chunk records (a JSON column
// before user_version 2, rows after) and the temp files, and records the
// expected values.
func insertUploads(t testing.TB, db *sql.DB, f *Fixture, exp *Expected, version int) {
	t.Helper()
	for _, u := range uploadSpecs {
		if u.onlyBelowChunkTableVersion && version >= 2 {
			continue
		}
		rel := "transfers/" + u.id + ".tmp"
		tempPath := filepath.Join(f.DataDir, filepath.FromSlash(rel))
		chunkJSON := u.rawChunkJSON
		if chunkJSON == "" {
			ints := u.chunks
			if ints == nil {
				ints = []int{}
			}
			raw, err := json.Marshal(ints)
			if err != nil {
				t.Fatalf("legacyfixtures: encode chunks: %v", err)
			}
			chunkJSON = string(raw)
		}
		insertRow(t, db, "transfers", map[string]any{
			"id": u.id, "target_path": "/srv/share/uploads/" + u.id + ".bin", "total_size": u.total,
			"chunk_size": uploadChunkSize, "sha256": sha256Hex("content-" + u.id), "received_chunks": chunkJSON,
			"status": u.status, "temp_path": tempPath, "total_chunks": u.totalChunks, "device_id": u.deviceID,
			"updated_at": u.updatedAt, "overwrite": b2i(u.overwrite), "terminal_at": u.terminalAt,
		})
		if version >= 2 {
			for _, n := range u.chunks {
				insertRow(t, db, "transfer_chunks", map[string]any{"transfer_id": u.id, "chunk_no": n})
			}
		}
		row := UploadRow{
			ID: u.id, TargetPath: "/srv/share/uploads/" + u.id + ".bin", TotalSize: u.total, ChunkSize: uploadChunkSize,
			TotalChunks: u.totalChunks, SHA256: sha256Hex("content-" + u.id), Status: u.status, TempPath: rel,
		}
		if u.rawChunkJSON == "" && len(u.chunks) > 0 {
			row.Chunks = append([]int(nil), u.chunks...)
		}
		if version >= 1 {
			row.DeviceID, row.UpdatedAt, row.Overwrite = u.deviceID, u.updatedAt, u.overwrite
		}
		switch {
		case version >= 7:
			row.TerminalAt = u.terminalAt
		case terminal(u.status):
			row.TerminalAtSetAtMigration = true
		}
		if u.tempBytes >= 0 {
			data := make([]byte, u.tempBytes)
			for i := range data {
				data[i] = byte(i % 251)
			}
			if err := os.WriteFile(tempPath, data, 0o600); err != nil {
				t.Fatalf("legacyfixtures: write temp file: %v", err)
			}
			row.TempFile = &TempFile{Name: rel, Size: int64(u.tempBytes)}
		}
		exp.Uploads = append(exp.Uploads, row)
	}
	sort.Slice(exp.Uploads, func(i, j int) bool { return exp.Uploads[i].ID < exp.Uploads[j].ID })
}

// insertAuditAndConfig writes audit rows with gapped ids (the table exists
// from user_version 3; older fixtures get an empty one from migration) and the
// config key/value rows.
func insertAuditAndConfig(t testing.TB, db *sql.DB, exp *Expected, version int) {
	t.Helper()
	if version >= 3 {
		rows := []AuditRow{
			{ID: 10, At: epoch + 1000, Action: "pair", Actor: "dev-phone", Target: "dev-phone", Detail: "paired"},
			{ID: 11, At: epoch + 1100, Action: "login", Actor: "alice", Target: "dev-owner"},
			{ID: 12, At: epoch + 1200, Action: "login_failed", Actor: "alice", Detail: "bad password"},
			{ID: 15, At: epoch + 1500, Action: "device_revoked", Actor: "alice", Target: "dev-revoked"},
			{ID: 16, At: epoch + 1600, Action: "share_created", Actor: "dev-phone", Target: "/srv/share/phone/a.txt"},
		}
		for _, r := range rows {
			insertRow(t, db, "audit_log", map[string]any{
				"id": r.ID, "at": r.At, "action": r.Action, "actor": r.Actor, "target": r.Target, "detail": r.Detail,
			})
		}
		exp.Audit = rows
	}
	cfg := map[string]string{"agentName": "fixture-pc", "roots": "/srv/share", "photoBackupRoot": ""}
	for k, v := range cfg {
		insertRow(t, db, "config", map[string]any{"key": k, "value": v})
		exp.Config[k] = v
	}
}

// insertPairing writes one pairing code and (from user_version 8) one pair
// request. Both expire within an hour or two, so an importer drops them.
func insertPairing(t testing.TB, db *sql.DB, f *Fixture, exp *Expected, version int) {
	t.Helper()
	insertRow(t, db, "pairing_codes", map[string]any{
		"code": f.PairingCode, "expires": int64(epoch + 3600), "jail_root": "/srv/share/guest", "read_only": 1, "can_register": 1,
	})
	exp.NotImported.PairingCodes = 1
	if version >= 8 {
		insertRow(t, db, "pair_requests", map[string]any{
			"id": "pr-fixture-1", "label": "New phone", "client_id": "client-new", "public_key": "fixture-pubkey-new",
			"client_nonce": "fixture-nonce", "nonce_commit": "fixture-commit", "remote_ip": "192.0.2.20",
			"status": "pending", "created": int64(epoch + 7000), "expires": int64(epoch + 7120),
		})
		exp.NotImported.PairRequests = 1
	}
}
