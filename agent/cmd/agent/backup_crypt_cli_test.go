package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ZaidQamhieh/rfe/agent/internal/backup"
)

func TestBackupOffsiteThenDecryptRoundTripAndStatus(t *testing.T) {
	dir := initializeAdminData(t)
	if _, err := captureStdout(t, func() error { return runAdmin("backup", []string{"-data", dir}) }); err != nil {
		t.Fatal(err)
	}
	const pass = "correct horse battery"
	t.Setenv(envBackupPassphrase, pass)
	usb := filepath.Join(t.TempDir(), "usb")
	out, err := captureStdout(t, func() error { return runAdmin("backup", []string{"offsite", "-data", dir, "-dir", usb}) })
	if err != nil {
		t.Fatalf("offsite: %v\n%s", err, out)
	}
	if strings.Contains(out, pass) {
		t.Error("the passphrase was printed")
	}
	copies, _ := filepath.Glob(filepath.Join(usb, "*"+backup.EncryptedSuffix))
	if len(copies) != 1 {
		t.Fatalf("encrypted copies = %v", copies)
	}
	raw, _ := os.ReadFile(copies[0])
	if !bytes.HasPrefix(raw, []byte("RFEBKENC")) {
		t.Errorf("the copy is not in the encrypted format")
	}
	rep, err := backup.Status(dir)
	if err != nil || !rep.Offsite.Configured || !rep.Offsite.OK {
		t.Fatalf("offsite status = %+v, %v", rep.Offsite, err)
	}
	if rep.LastBackup == nil || !rep.LastBackup.OK || rep.LastBackup.Label != backup.LabelManual {
		t.Errorf("manual backup was not recorded: %+v", rep.LastBackup)
	}

	plain := filepath.Join(t.TempDir(), "restored.tar.gz")
	t.Setenv(envBackupPassphrase, "wrong")
	if _, err := captureStdout(t, func() error { return runAdmin("backup", []string{"decrypt", copies[0], plain}) }); err == nil {
		t.Fatal("a wrong passphrase decrypted the copy")
	}
	if _, err := os.Stat(plain); err == nil {
		t.Error("a failed decrypt left an output file")
	}
	t.Setenv(envBackupPassphrase, pass)
	if out, err := captureStdout(t, func() error { return runAdmin("backup", []string{"decrypt", copies[0], plain}) }); err != nil {
		t.Fatalf("decrypt: %v\n%s", err, out)
	}
	if _, err := backup.Verify(plain); err != nil {
		t.Errorf("decrypted archive does not verify: %v", err)
	}
}

func TestBackupOffsiteFailureIsRecordedAndDirIsRequired(t *testing.T) {
	dir := initializeAdminData(t)
	t.Setenv(envBackupPassphrase, "pw")
	if _, err := captureStdout(t, func() error { return runAdmin("backup", []string{"offsite", "-data", dir, "-dir", t.TempDir()}) }); err == nil {
		t.Fatal("offsite with no archive must fail")
	}
	if _, err := captureStdout(t, func() error { return runAdmin("backup", []string{"offsite", "-data", dir}) }); err == nil {
		t.Fatal("-dir is required")
	}
}
