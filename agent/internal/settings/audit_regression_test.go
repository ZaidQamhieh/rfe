package settings

import "testing"

// Audit 2026-10-20 (F-93): a corrupt bandwidth limit is read as 0 (unlimited) without an error.
func TestAuditRepro_CorruptThrottleIsNotSilentlyUnlimited(t *testing.T) {
	db := newDB(t)
	if err := db.SetConfig(keyMaxUploadBPS, "abc"); err != nil {
		t.Fatal(err)
	}
	if err := db.SetConfig(keyMaxDownloadBPS, "-5"); err != nil {
		t.Fatal(err)
	}
	s, err := Load(db, false, nil, "pc")
	if err == nil && (s.MaxUploadBytesPerSec() <= 0 || s.MaxDownloadBytesPerSec() <= 0) {
		t.Fatalf("corrupt limits loaded without error as upload=%d download=%d (<=0 means unlimited)",
			s.MaxUploadBytesPerSec(), s.MaxDownloadBytesPerSec())
	}
}
