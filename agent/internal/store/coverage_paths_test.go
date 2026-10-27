package store

import (
	"database/sql"
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"
)

func openCoverageDB(t *testing.T) *DB {
	t.Helper()
	db, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func coverageTransfer(id, deviceID string, size int64) *Transfer {
	return &Transfer{
		ID:         id,
		TargetPath: "/uploads/" + id,
		TotalSize:  size,
		ChunkSize:  4,
		SHA256:     "digest-" + id,
		TempPath:   "/tmp/" + id,
		TotalChunks: func() int {
			if size == 0 {
				return 1
			}
			return int((size + 3) / 4)
		}(),
		DeviceID:  deviceID,
		Overwrite: true,
	}
}

func TestExpectedChunkLenBoundaries(t *testing.T) {
	transfer := &Transfer{TotalSize: 10, ChunkSize: 4, TotalChunks: 3}
	for _, tc := range []struct {
		n    int
		want int
	}{{-1, -1}, {0, 4}, {1, 4}, {2, 2}, {3, -1}} {
		if got := transfer.ExpectedChunkLen(tc.n); got != tc.want {
			t.Errorf("ExpectedChunkLen(%d) = %d, want %d", tc.n, got, tc.want)
		}
	}
	if got := (&Transfer{TotalSize: 1, ChunkSize: 0, TotalChunks: 1}).ExpectedChunkLen(0); got != -1 {
		t.Errorf("zero chunk size result = %d, want -1", got)
	}
	if got := (&Transfer{TotalSize: 9, ChunkSize: 4, TotalChunks: 3}).ExpectedChunkLen(2); got != 1 {
		t.Errorf("short final chunk result = %d, want 1", got)
	}
	if got := (&Transfer{TotalSize: 0, ChunkSize: 4, TotalChunks: 1}).ExpectedChunkLen(0); got != 0 {
		t.Errorf("empty transfer chunk result = %d, want 0", got)
	}
	if got := (&Transfer{TotalSize: 0, ChunkSize: 4, TotalChunks: 2}).ExpectedChunkLen(1); got != -1 {
		t.Errorf("inconsistent transfer metadata result = %d, want -1", got)
	}
}

func TestCreateTransferWithinLimitsEnforcesHostAndDeviceReservations(t *testing.T) {
	db := openCoverageDB(t)
	first := coverageTransfer("first", "phone-a", 6)
	created, err := db.CreateTransferWithinLimits(first, TransferLimits{
		MaxHostSessions: 2, MaxHostBytes: 10, MaxDeviceSessions: 1, MaxDeviceBytes: 8,
	})
	if err != nil || !created {
		t.Fatalf("first reservation = (%v, %v), want true/nil", created, err)
	}
	for _, tc := range []struct {
		name   string
		limits TransferLimits
		want   bool
	}{
		{name: "same device session cap", limits: TransferLimits{MaxHostSessions: 3, MaxHostBytes: 20, MaxDeviceSessions: 1, MaxDeviceBytes: 20}},
		{name: "same device byte cap", limits: TransferLimits{MaxHostSessions: 3, MaxHostBytes: 20, MaxDeviceSessions: 3, MaxDeviceBytes: 5}},
		{name: "host session cap", limits: TransferLimits{MaxHostSessions: 1, MaxHostBytes: 20, MaxDeviceSessions: 3, MaxDeviceBytes: 20}},
		{name: "host byte cap", limits: TransferLimits{MaxHostSessions: 3, MaxHostBytes: 5, MaxDeviceSessions: 3, MaxDeviceBytes: 20}},
		{name: "independent device within host cap", limits: TransferLimits{MaxHostSessions: 3, MaxHostBytes: 20, MaxDeviceSessions: 1, MaxDeviceBytes: 10}, want: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			id, device := "blocked-"+tc.name, "phone-a"
			if tc.want {
				device = "phone-b"
			}
			transfer := coverageTransfer(id, device, 6)
			got, err := db.CreateTransferWithinLimits(transfer, tc.limits)
			if err != nil || got != tc.want {
				t.Fatalf("reservation = (%v, %v), want (%v, nil)", got, err, tc.want)
			}
		})
	}
}

func TestTransferOpenRowsTouchListsAndDeleteStaleSnapshot(t *testing.T) {
	db := openCoverageDB(t)
	open := coverageTransfer("open-session", "phone", 9)
	if err := db.CreateTransfer(open); err != nil {
		t.Fatalf("create open transfer: %v", err)
	}
	if err := db.MarkChunkReceived(open.ID, 2); err != nil {
		t.Fatalf("record chunk: %v", err)
	}
	if err := db.TouchOpenTransfer(open.ID, 1234567890); err != nil {
		t.Fatalf("touch open transfer: %v", err)
	}
	emptyTemp := coverageTransfer("empty-temp", "phone", 0)
	emptyTemp.TempPath = ""
	if err := db.CreateTransfer(emptyTemp); err != nil {
		t.Fatalf("create second transfer: %v", err)
	}
	if err := db.CreateTransfer(coverageTransfer("done", "other", 4)); err != nil {
		t.Fatalf("create completed transfer: %v", err)
	}
	if err := db.SetTransferStatus("done", "completed"); err != nil {
		t.Fatalf("complete transfer: %v", err)
	}

	openRows, err := db.ListOpenTransfers()
	if err != nil || len(openRows) != 2 {
		t.Fatalf("ListOpenTransfers = (%+v, %v), want two open rows", openRows, err)
	}
	if openRows[0].ID != open.ID || openRows[0].UpdatedAt != 1234567890 || openRows[0].ReceivedCount != 1 || openRows[0].Overwrite != open.Overwrite {
		t.Fatalf("open row omitted persisted metadata: %+v", openRows[0])
	}
	tempPaths, err := db.ListTransferTempPaths()
	if err != nil || !reflect.DeepEqual(tempPaths, []string{open.TempPath}) {
		t.Fatalf("ListTransferTempPaths = (%v, %v), want only %q", tempPaths, err, open.TempPath)
	}

	snapshot := openRows[0]
	if deleted, err := db.DeleteStaleOpenTransfer(&Transfer{ID: snapshot.ID, UpdatedAt: snapshot.UpdatedAt, ReceivedCount: 0}); err != nil || deleted {
		t.Fatalf("stale snapshot with wrong chunk count = (%v, %v), want false/nil", deleted, err)
	}
	if deleted, err := db.DeleteStaleOpenTransfer(&snapshot); err != nil || !deleted {
		t.Fatalf("matching stale snapshot = (%v, %v), want true/nil", deleted, err)
	}
	if present, err := db.HasChunk(snapshot.ID, 2); err != nil || present {
		t.Fatalf("chunk after stale row deletion = (%v, %v), want false/nil", present, err)
	}
	if deleted, err := db.DeleteStaleOpenTransfer(&snapshot); err != nil || deleted {
		t.Fatalf("deleting removed snapshot = (%v, %v), want false/nil", deleted, err)
	}
}

func TestPruneTerminalTransfersBeforeKeepsRecentAndOpenRows(t *testing.T) {
	db := openCoverageDB(t)
	cutoff := time.Now().Unix()
	rows := []*Transfer{
		coverageTransfer("expired-completed", "phone", 4),
		coverageTransfer("expired-failed", "phone", 4),
		coverageTransfer("recent-completed", "phone", 4),
		coverageTransfer("at-cutoff", "phone", 4),
		coverageTransfer("open-old", "phone", 4),
		coverageTransfer("legacy-terminal", "phone", 4),
	}
	for _, row := range rows {
		if err := db.CreateTransfer(row); err != nil {
			t.Fatalf("create %s: %v", row.ID, err)
		}
	}
	for _, id := range []string{"expired-completed", "recent-completed", "at-cutoff"} {
		if err := db.SetTransferStatus(id, "completed"); err != nil {
			t.Fatalf("mark %s completed: %v", id, err)
		}
	}
	if err := db.SetTransferStatus("expired-failed", "failed"); err != nil {
		t.Fatalf("mark failed: %v", err)
	}
	if err := db.MarkChunkReceived("expired-completed", 0); err != nil {
		t.Fatalf("add expired completed chunk: %v", err)
	}
	if err := db.MarkChunkReceived("expired-failed", 0); err != nil {
		t.Fatalf("add expired failed chunk: %v", err)
	}
	if _, err := db.db.Exec(`UPDATE transfers SET terminal_at=CASE id
WHEN 'expired-completed' THEN ?
WHEN 'expired-failed' THEN ?
WHEN 'at-cutoff' THEN ?
WHEN 'recent-completed' THEN ?
ELSE terminal_at END`, cutoff-1, cutoff-1, cutoff, cutoff+1); err != nil {
		t.Fatalf("seed terminal timestamps: %v", err)
	}
	// An old last-activity time must not make an open upload eligible for
	// terminal-history pruning. Zero terminal_at also preserves legacy rows.
	if _, err := db.db.Exec(`UPDATE transfers SET updated_at=1 WHERE id='open-old'`); err != nil {
		t.Fatal(err)
	}

	deleted, err := db.PruneTerminalTransfersBefore(cutoff)
	if err != nil || deleted != 2 {
		t.Fatalf("pruned %d terminal rows, err=%v; want two expired rows", deleted, err)
	}
	for _, id := range []string{"expired-completed", "expired-failed"} {
		var count int
		if err := db.db.QueryRow(`SELECT COUNT(*) FROM transfers WHERE id=?`, id).Scan(&count); err != nil || count != 0 {
			t.Errorf("expired row %q remains: count=%d err=%v", id, count, err)
		}
		if found, err := db.HasChunk(id, 0); err != nil || found {
			t.Errorf("expired chunk for %q remains: found=%v err=%v", id, found, err)
		}
	}
	for _, id := range []string{"recent-completed", "at-cutoff", "open-old", "legacy-terminal"} {
		var count int
		if err := db.db.QueryRow(`SELECT COUNT(*) FROM transfers WHERE id=?`, id).Scan(&count); err != nil || count != 1 {
			t.Errorf("protected row %q missing: count=%d err=%v", id, count, err)
		}
	}
}

// retentionMigrationBase is the schema version just before migrateTransferHistoryRetention.
var retentionMigrationBase = len(migrations) - 3

func TestTransferHistoryMigrationBackfillsOnce(t *testing.T) {
	db := openCoverageDB(t)
	if err := db.CreateTransfer(coverageTransfer("old-completed", "phone", 4)); err != nil {
		t.Fatal(err)
	}
	if err := db.SetTransferStatus("old-completed", "completed"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.db.Exec(`UPDATE transfers SET terminal_at=0 WHERE id='old-completed'`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.db.Exec(fmt.Sprintf(`PRAGMA user_version=%d`, retentionMigrationBase)); err != nil {
		t.Fatal(err)
	}
	if err := db.migrate(); err != nil {
		t.Fatalf("run retention migration: %v", err)
	}
	var first int64
	if err := db.db.QueryRow(`SELECT terminal_at FROM transfers WHERE id='old-completed'`).Scan(&first); err != nil || first <= 0 {
		t.Fatalf("legacy terminal history was not backfilled: terminal_at=%d err=%v", first, err)
	}
	if _, err := db.db.Exec(`UPDATE transfers SET terminal_at=1234 WHERE id='old-completed'`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.db.Exec(fmt.Sprintf(`PRAGMA user_version=%d`, retentionMigrationBase)); err != nil {
		t.Fatal(err)
	}
	if err := db.migrate(); err != nil {
		t.Fatalf("replay retention migration: %v", err)
	}
	var second int64
	if err := db.db.QueryRow(`SELECT terminal_at FROM transfers WHERE id='old-completed'`).Scan(&second); err != nil || second != 1234 {
		t.Fatalf("migration replay reset established terminal_at: got %d err=%v", second, err)
	}
}

func TestTransferHistoryFiltersCountsAndDeviceLabels(t *testing.T) {
	db := openCoverageDB(t)
	if err := db.CreateUser("owner", "pw-hash"); err != nil {
		t.Fatalf("create owner: %v", err)
	}
	loginID, err := db.LoginDevice("client-login", "browser", "login-token", "pub", "owner", "pw-hash")
	if err != nil {
		t.Fatalf("login device: %v", err)
	}
	if _, err := db.LoginDevice("client-rejected", "bad", "bad-token", "", "owner", "wrong-hash"); !errors.Is(err, ErrLoginAccountChanged) {
		t.Fatalf("mismatched account login error = %v, want ErrLoginAccountChanged", err)
	}
	if err := db.CreateDevice("paired", "phone", "paired-token"); err != nil {
		t.Fatalf("create paired device: %v", err)
	}
	for _, tr := range []*Transfer{
		coverageTransfer("login-open", loginID, 4),
		coverageTransfer("login-done", loginID, 8),
		coverageTransfer("paired-open", "paired", 2),
		coverageTransfer("legacy-open", "", 1),
	} {
		if err := db.CreateTransfer(tr); err != nil {
			t.Fatalf("create %s: %v", tr.ID, err)
		}
	}
	if err := db.SetTransferStatus("login-done", "completed"); err != nil {
		t.Fatal(err)
	}
	if err := db.TouchOpenTransfer("login-open", time.Now().Unix()); err != nil {
		t.Fatal(err)
	}

	all, err := db.ListTransfers(10, "", "")
	if err != nil || len(all) != 4 {
		t.Fatalf("all transfer history = %d rows, %v", len(all), err)
	}
	byDevice, err := db.ListTransfers(10, "paired", "")
	if err != nil || len(byDevice) != 1 || byDevice[0].ID != "paired-open" {
		t.Fatalf("device-scoped history = %+v, %v", byDevice, err)
	}
	byUser, err := db.ListTransfers(10, "", "owner")
	if err != nil || len(byUser) != 2 {
		t.Fatalf("user-scoped history = %+v, %v", byUser, err)
	}
	limited, err := db.ListTransfers(1, "", "")
	if err != nil || len(limited) != 1 || limited[0].ID != "legacy-open" {
		t.Fatalf("limited newest-first history = %+v, %v", limited, err)
	}
	counts, err := db.CountTransfersByStatus()
	if err != nil || counts["open"] != 3 || counts["completed"] != 1 {
		t.Fatalf("transfer counts = (%v, %v)", counts, err)
	}
	active, err := db.CountActiveTransfers()
	if err != nil || active != 1 {
		t.Fatalf("active transfer count = (%d, %v), want one freshly touched open session", active, err)
	}
	devices, err := db.ListTransferDevices()
	if err != nil || len(devices) != 2 {
		t.Fatalf("transfer devices = (%+v, %v), want two labeled devices", devices, err)
	}
	if devices[0].ID != loginID || devices[0].Label != "browser" || devices[0].Username != "owner" || devices[1].ID != "paired" || devices[1].Username != "" {
		t.Fatalf("unexpected transfer device labels: %+v", devices)
	}
	if err := db.DeleteDevice("paired"); err != nil {
		t.Fatalf("remove paired device: %v", err)
	}
	devices, err = db.ListTransferDevices()
	if err != nil || len(devices) != 2 || devices[1].Label != "paired" {
		t.Fatalf("missing device fallback label = (%+v, %v)", devices, err)
	}
}

func TestLoginUsersAndConfigPersistence(t *testing.T) {
	db := openCoverageDB(t)
	if has, err := db.HasAnyUser(); err != nil || has {
		t.Fatalf("empty HasAnyUser = (%v, %v)", has, err)
	}
	if err := db.CreateUser("first", "hash-a"); err != nil {
		t.Fatal(err)
	}
	if has, err := db.HasAnyUser(); err != nil || !has {
		t.Fatalf("populated HasAnyUser = (%v, %v)", has, err)
	}
	if err := db.CreateUser("second", "hash-b"); err != nil {
		t.Fatal(err)
	}
	users, err := db.ListUsers()
	if err != nil || len(users) != 2 {
		t.Fatalf("ListUsers = (%+v, %v)", users, err)
	}
	for _, user := range users {
		if user.PasswordHash != "" {
			t.Fatalf("password hash leaked in ListUsers: %+v", user)
		}
	}
	if err := db.DeleteUser("missing"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("deleting unknown user = %v, want sql.ErrNoRows", err)
	}
	if err := db.DeleteUser("first"); err != nil {
		t.Fatalf("delete one of two users: %v", err)
	}
	if err := db.DeleteUser("second"); !errors.Is(err, ErrLastUser) {
		t.Fatalf("deleting final user = %v, want ErrLastUser", err)
	}
	if value, err := db.GetConfig("missing"); err != nil || value != "" {
		t.Fatalf("missing config = (%q, %v)", value, err)
	}
	if exists, err := db.HasConfig("missing"); err != nil || exists {
		t.Fatalf("missing config exists = (%v, %v), want false", exists, err)
	}
	if err := db.SetConfig("theme", "dark"); err != nil {
		t.Fatal(err)
	}
	if exists, err := db.HasConfig("theme"); err != nil || !exists {
		t.Fatalf("theme config exists = (%v, %v), want true", exists, err)
	}
	if err := db.SetConfig("theme", "light"); err != nil {
		t.Fatal(err)
	}
	if value, err := db.GetConfig("theme"); err != nil || value != "light" {
		t.Fatalf("updated config = (%q, %v), want light", value, err)
	}
}

func TestDeviceClientKeyAndAppPermissionRoundTrip(t *testing.T) {
	db := openCoverageDB(t)
	if key, err := db.DevicePublicKeyByClientID(""); err != nil || key != "" {
		t.Fatalf("empty client key lookup = (%q, %v)", key, err)
	}
	if key, err := db.DevicePublicKeyByClientID("missing-client"); err != nil || key != "" {
		t.Fatalf("missing client key lookup = (%q, %v)", key, err)
	}
	id, err := db.UpsertDevice("stable-client", "desktop", "token", "public-key-1", false)
	if err != nil {
		t.Fatal(err)
	}
	if key, err := db.DevicePublicKeyByClientID("stable-client"); err != nil || key != "public-key-1" {
		t.Fatalf("pinned client key lookup = (%q, %v)", key, err)
	}
	if err := db.SetDeviceAppPermissions(id, true, false); err != nil {
		t.Fatal(err)
	}
	device, err := db.GetDeviceByID(id)
	if err != nil || device == nil || !device.ViewApps || device.LaunchApps {
		t.Fatalf("catalog-only grant = (%+v, %v)", device, err)
	}
	if err := db.SetDeviceAppPermissions(id, true, true); err != nil {
		t.Fatal(err)
	}
	device, err = db.DeviceByToken("token")
	if err != nil || device == nil || !device.ViewApps || !device.LaunchApps {
		t.Fatalf("catalog and launch grant = (%+v, %v)", device, err)
	}
	if err := db.SetDeviceAppPermissions(id, false, false); err != nil {
		t.Fatal(err)
	}
	devices, err := db.ListDevices()
	if err != nil || len(devices) != 1 || devices[0].ViewApps || devices[0].LaunchApps {
		t.Fatalf("revoked app grants = (%+v, %v)", devices, err)
	}
}

func TestShareTokenMetadataAndAuditLifecycle(t *testing.T) {
	db := openCoverageDB(t)
	expires := time.Now().Add(time.Hour).Truncate(time.Second)
	if err := db.CreateShareToken("token-hash", "/srv/report.txt", "device-1", expires); err != nil {
		t.Fatal(err)
	}
	token, err := db.GetShareToken("token-hash")
	if err != nil || token == nil || token.Path != "/srv/report.txt" || token.DeviceID != "device-1" || !token.Expires.Equal(expires) {
		t.Fatalf("share token = (%+v, %v)", token, err)
	}
	if token, err := db.GetShareToken("missing"); err != nil || token != nil {
		t.Fatalf("missing share token = (%+v, %v)", token, err)
	}
	if err := db.LogShareMint("token-hash", "/srv/report.txt", expires); err != nil {
		t.Fatal(err)
	}
	if err := db.LogShareServed("token-hash", "192.0.2.10"); err != nil {
		t.Fatal(err)
	}
	var requester string
	var served sql.NullInt64
	if err := db.db.QueryRow(`SELECT requester_ip, served_at FROM share_log WHERE token_hash=?`, "token-hash").Scan(&requester, &served); err != nil {
		t.Fatal(err)
	}
	if requester != "192.0.2.10" || !served.Valid || served.Int64 == 0 {
		t.Fatalf("share serve audit = requester %q, served %+v", requester, served)
	}
	if err := db.DeleteShareToken("token-hash"); err != nil {
		t.Fatal(err)
	}
	if token, err := db.GetShareToken("token-hash"); err != nil || token != nil {
		t.Fatalf("share token after revoke = (%+v, %v)", token, err)
	}
}
