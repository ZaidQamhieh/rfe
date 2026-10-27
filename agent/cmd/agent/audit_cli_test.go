//go:build linux

package main

import (
	"testing"

	"github.com/ZaidQamhieh/rfe/agent/internal/settings"
	"github.com/ZaidQamhieh/rfe/agent/internal/store"
)

// Audit F-118: device changes made from the CLI must reach the audit trail.
func TestCliAuditWritesAnEntry(t *testing.T) {
	db, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	cliAudit(db, store.AuditDeviceRevoked, "dev-1", "")
	entries, err := db.AuditEntries(10, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Action != store.AuditDeviceRevoked || entries[0].Actor != cliActor || entries[0].Target != "dev-1" {
		t.Fatalf("audit entries = %+v", entries)
	}
}

// F-122: the jail command on a database the daemon never started must not seed unrestricted settings.
func TestJailRefusesAnUninitialisedDatabase(t *testing.T) {
	data := initializeAdminData(t)
	db, err := store.Open(data)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.CreateDevice("d1", "phone", "tok"); err != nil {
		t.Fatal(err)
	}
	db.Close()
	if _, err := captureStdout(t, func() error { return cmdJail([]string{"-data", data, "d1", t.TempDir()}) }); err == nil {
		t.Fatal("jail on a never-started database should be refused")
	}
	db, err = store.Open(data)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if ok, _ := settings.Initialised(db); ok {
		t.Fatal("the command wrote settings it should not have")
	}
}

// F-91: install must not silently drop a data directory the service would not use.
func TestInstallRefusesOptionsAndForeignDataDir(t *testing.T) {
	if err := cmdInstall([]string{"-data", t.TempDir()}); err == nil {
		t.Fatal("install with options should be refused")
	}
	t.Setenv("RFE_DATA_DIR", t.TempDir())
	if err := cmdInstall(nil); err == nil {
		t.Fatal("install with a non-standard RFE_DATA_DIR should be refused")
	}
}

// F-125: an empty request id must not approve the one waiting request by prefix-matching everything.
func TestPairAcceptRefusesAnEmptyID(t *testing.T) {
	data := initializeAdminData(t)
	if err := cmdPairDecide([]string{"-data", data, ""}, true); err == nil {
		t.Fatal("an empty id should be refused")
	}
}
