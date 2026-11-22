package store

import (
	"errors"
	"fmt"
	"testing"
	"time"
)

func federatedRevoked(t *testing.T, db *DB, id string) bool {
	t.Helper()
	d, err := db.GetDeviceByID(id)
	if err != nil || d == nil {
		t.Fatalf("GetDeviceByID(%s) = %+v, %v", id, d, err)
	}
	return d.Revoked
}

// A federated device the owner revoked must not be revived by a fresh assertion.
func TestFederatedLogin_OwnerRevokedDeviceIsRefused(t *testing.T) {
	db, fp := federatedFixture(t)
	id, err := db.LoginDeviceFederated("c1", "phone", "tok", "pk", "alice", fp)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.RevokeDevice(id); err != nil {
		t.Fatal(err)
	}
	if _, err := db.LoginDeviceFederated("c1", "phone", "tok2", "pk", "alice", fp); !errors.Is(err, ErrDeviceRevoked) {
		t.Fatalf("login after owner revoke = %v, want ErrDeviceRevoked", err)
	}
	if !federatedRevoked(t, db, id) {
		t.Fatal("refused login un-revoked the device")
	}
}

// A device that ended its own session (logout) was not revoked by the owner and can log in again.
func TestFederatedLogin_SessionRevokeIsNotOwnerRevoke(t *testing.T) {
	db, fp := federatedFixture(t)
	id, err := db.LoginDeviceFederated("c1", "phone", "tok", "pk", "alice", fp)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.RevokeDeviceSession(id); err != nil {
		t.Fatal(err)
	}
	if _, err := db.LoginDeviceFederated("c1", "phone", "tok2", "pk", "alice", fp); err != nil {
		t.Fatalf("login after the device's own logout = %v, want success", err)
	}
}

// An expired federated session is stored revoked but renews with a fresh assertion.
func TestFederatedLogin_ExpiryRevokedSessionStillRenews(t *testing.T) {
	db, fp := federatedFixture(t)
	id, err := db.LoginDeviceFederated("c1", "phone", "tok", "pk", "alice", fp)
	if err != nil {
		t.Fatal(err)
	}
	setFederatedLoginAge(t, db, id, FederatedSessionTTL+time.Minute)
	if d, _ := db.DeviceByToken("tok"); d != nil && !d.Revoked {
		t.Fatalf("expired session still usable: %+v", d)
	}
	if !federatedRevoked(t, db, id) {
		t.Fatal("expired session was not stored revoked")
	}
	if _, err := db.LoginDeviceFederated("c1", "phone", "tok2", "pk", "alice", fp); err != nil {
		t.Fatalf("renewal after expiry = %v, want success", err)
	}
	if d, err := db.DeviceByToken("tok2"); err != nil || d == nil || d.Revoked {
		t.Fatalf("renewed session = %+v, %v", d, err)
	}
}

// Owner approval on the PC (a fresh local pairing of the same client id) clears the owner-revoked mark.
func TestFederatedLogin_LocalRepairClearsOwnerRevoke(t *testing.T) {
	db, fp := federatedFixture(t)
	id, err := db.LoginDeviceFederated("c1", "phone", "tok", "pk", "alice", fp)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.RevokeDevice(id); err != nil {
		t.Fatal(err)
	}
	if _, err := db.UpsertDevice("c1", "phone", "tok-local", "pk", false); err != nil {
		t.Fatal(err)
	}
	if _, err := db.LoginDeviceFederated("c1", "phone", "tok2", "pk", "alice", fp); err != nil {
		t.Fatalf("login after owner re-paired the device = %v, want success", err)
	}
}

func TestAccountDeny_RefusesMemberAtLoginUntilAllowed(t *testing.T) {
	db, fp := federatedFixture(t)
	if err := db.DenyAccount(fp, "mallory"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.LoginDeviceFederated("c9", "phone", "tok", "pk", "mallory", fp); !errors.Is(err, ErrAccountDenied) {
		t.Fatalf("denied login = %v, want ErrAccountDenied", err)
	}
	if devs, _ := db.ListDevices(); len(devs) != 0 {
		t.Fatalf("denied login left %d device rows", len(devs))
	}
	if _, err := db.LoginDeviceFederated("c1", "phone", "tok-a", "pk", "alice", fp); err != nil {
		t.Fatalf("another member must still log in: %v", err)
	}
	if err := db.AllowAccount(fp, "mallory"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.LoginDeviceFederated("c9", "phone", "tok", "pk", "mallory", fp); err != nil {
		t.Fatalf("login after allow = %v, want success", err)
	}
}

func TestAccountDeny_EndsExistingSessionsAndLists(t *testing.T) {
	db, fp := federatedFixture(t)
	if _, err := db.LoginDeviceFederated("c1", "phone", "tok", "pk", "alice", fp); err != nil {
		t.Fatal(err)
	}
	if err := db.DenyAccount(fp, "alice"); err != nil {
		t.Fatal(err)
	}
	if err := db.DenyAccount(fp, "alice"); err != nil {
		t.Fatalf("denying twice must be harmless: %v", err)
	}
	if d, _ := db.DeviceByToken("tok"); d != nil && !d.Revoked {
		t.Fatalf("denied member's session still usable: %+v", d)
	}
	list, err := db.DeniedAccounts()
	if err != nil || len(list) != 1 || list[0].Issuer != fp || list[0].Sub != "alice" {
		t.Fatalf("DeniedAccounts = %+v, %v", list, err)
	}
	if err := db.AllowAccount(fp, "nobody"); err != nil {
		t.Fatalf("allowing an unlisted member must be harmless: %v", err)
	}
}

func TestMigrate_AddsOwnerRevokeAndDenyList(t *testing.T) {
	dir := t.TempDir()
	db, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	// Put the file back to the state before this step: no column, no table, previous version.
	for _, q := range []string{
		`ALTER TABLE devices DROP COLUMN revoked_by_owner`,
		`DROP TABLE account_denied`,
	} {
		if _, err := db.db.Exec(q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	if _, err := db.db.Exec(fmt.Sprintf("PRAGMA user_version = %d", len(migrations)-1)); err != nil {
		t.Fatal(err)
	}
	db.Close()
	db, err = Open(dir)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer db.Close()
	var version int
	if err := db.db.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil || version != len(migrations) {
		t.Fatalf("user_version = %d, %v; want %d", version, err, len(migrations))
	}
	var cols int
	if err := db.db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('devices') WHERE name='revoked_by_owner'`).Scan(&cols); err != nil || cols != 1 {
		t.Fatalf("devices.revoked_by_owner columns = %d, %v; want 1", cols, err)
	}
	if err := db.DenyAccount("fp", "alice"); err != nil {
		t.Fatalf("deny list unusable after migration: %v", err)
	}
	var minCompat int
	if err := db.db.QueryRow(`SELECT value FROM schema_meta WHERE key='min_compatible'`).Scan(&minCompat); err != nil || minCompat != 0 {
		t.Fatalf("min_compatible = %d, %v; the step is additive and must leave it 0", minCompat, err)
	}
}
