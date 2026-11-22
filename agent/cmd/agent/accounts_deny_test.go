//go:build linux

package main

import (
	"errors"
	"strings"
	"testing"

	"github.com/ZaidQamhieh/rfe/agent/internal/store"
)

func TestAccountsDenyAllowRoundTrip(t *testing.T) {
	dir := accountsDataDir(t)
	const issuer = "AB:CD:ef"
	if out, _, err := pass3Run(t, "accounts", "denied", "-data", dir); err != nil || !strings.Contains(out, "No members are denied") {
		t.Fatalf("empty list: %q, %v", out, err)
	}
	if _, _, err := pass3Run(t, "accounts", "deny", issuer, "mallory", "-data", dir); err != nil {
		t.Fatalf("deny: %v", err)
	}
	out, _, err := pass3Run(t, "accounts", "denied", "-data", dir)
	if err != nil || !strings.Contains(out, "abcdef") || !strings.Contains(out, "mallory") {
		t.Fatalf("list after deny: %q, %v", out, err)
	}
	db, _ := store.Open(dir)
	if ok, err := db.AccountDenied("abcdef", "mallory"); err != nil || !ok {
		t.Fatalf("AccountDenied = %v, %v", ok, err)
	}
	db.Close()
	if _, _, err := pass3Run(t, "accounts", "allow", issuer, "mallory", "-data", dir); err != nil {
		t.Fatalf("allow: %v", err)
	}
	if out, _, err := pass3Run(t, "accounts", "denied", "-data", dir); err != nil || !strings.Contains(out, "No members are denied") {
		t.Fatalf("list after allow: %q, %v", out, err)
	}
	if _, _, err := pass3Run(t, "accounts", "deny", "onlyone", "-data", dir); err == nil {
		t.Fatal("deny accepted a missing subject")
	}
}

// "rfe remove <id> --deny" removes a federated device and denies its member; on a device that is not
// federated it refuses and removes nothing.
func TestRemoveDeny_DeniesFederatedMemberAndRefusesLocalDevice(t *testing.T) {
	dir := accountsDataDir(t)
	_, key, fp := fakeAccountServer(t, 200)
	db, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.SetTrustedAccountServer("lab", key); err != nil {
		t.Fatal(err)
	}
	fedID, err := db.LoginDeviceFederated("c1", "phone", "tok", "pk", "alice", fp)
	if err != nil {
		t.Fatal(err)
	}
	localID, err := db.UpsertDevice("c2", "laptop", "tok2", "pk2", false)
	if err != nil {
		t.Fatal(err)
	}
	db.Close()

	if _, _, err := pass3Run(t, "remove", localID, "--deny", "-data", dir); err == nil {
		t.Fatal("remove --deny accepted a device that is not federated")
	}
	if _, _, err := pass3Run(t, "remove", fedID, "--deny", "-data", dir); err != nil {
		t.Fatalf("remove --deny: %v", err)
	}
	db, err = store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if d, _ := db.GetDeviceByID(fedID); d != nil {
		t.Fatalf("federated device still present: %+v", d)
	}
	if d, _ := db.GetDeviceByID(localID); d == nil {
		t.Fatal("the refused removal deleted the local device")
	}
	if ok, err := db.AccountDenied(fp, "alice"); err != nil || !ok {
		t.Fatalf("member not denied after remove --deny: %v, %v", ok, err)
	}
	if _, err := db.LoginDeviceFederated("c1", "phone", "tok3", "pk", "alice", fp); !errors.Is(err, store.ErrAccountDenied) {
		t.Fatalf("login after remove --deny = %v, want ErrAccountDenied", err)
	}
}
