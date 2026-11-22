package store

import "testing"

func idemTransfer(id, device, key string) *Transfer {
	return &Transfer{
		ID: id, TargetPath: "/t/" + id, TotalSize: 4, ChunkSize: 4, SHA256: "ab",
		TempPath: "/tmp/" + id + ".tmp", TotalChunks: 1, DeviceID: device, IdempotencyKey: key,
	}
}

// A key is unique per device: the same key on the same device is refused by the index, on another device or
// without a key (NULL) it is not.
func TestTransferIdempotencyKeyIsUniquePerDevice(t *testing.T) {
	db, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	const key = "0123456789abcdef0123456789abcdef"

	if err := db.CreateTransfer(idemTransfer("a", "dev1", key)); err != nil {
		t.Fatalf("first create: %v", err)
	}
	if err := db.CreateTransfer(idemTransfer("b", "dev1", key)); err == nil {
		t.Fatal("same device and key created a second row; the unique index is missing")
	}
	if err := db.CreateTransfer(idemTransfer("c", "dev2", key)); err != nil {
		t.Fatalf("same key on another device must be independent: %v", err)
	}
	for _, id := range []string{"d", "e"} {
		if err := db.CreateTransfer(idemTransfer(id, "dev1", "")); err != nil {
			t.Fatalf("rows without a key must never collide: %v", err)
		}
	}
	got, err := db.GetTransferByKey("dev1", key)
	if err != nil || got == nil || got.ID != "a" {
		t.Fatalf("GetTransferByKey(dev1) = %+v, %v; want row a", got, err)
	}
	if got, err := db.GetTransferByKey("dev3", key); err != nil || got != nil {
		t.Fatalf("GetTransferByKey(dev3) = %+v, %v; want none", got, err)
	}
	if got, err := db.GetTransferByKey("dev1", ""); err != nil || got != nil {
		t.Fatalf("an empty key must match nothing, got %+v, %v", got, err)
	}
}

// The quota insert stores the key too, so a repeat open cannot slip past the index through the limited path.
func TestCreateTransferWithinLimitsStoresKey(t *testing.T) {
	db, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	lim := TransferLimits{MaxHostSessions: 8, MaxHostBytes: 1 << 20, MaxDeviceSessions: 4, MaxDeviceBytes: 1 << 20}
	const key = "fedcba9876543210fedcba9876543210"
	if ok, err := db.CreateTransferWithinLimits(idemTransfer("a", "dev1", key), lim); err != nil || !ok {
		t.Fatalf("create: ok=%v err=%v", ok, err)
	}
	got, err := db.GetTransferByKey("dev1", key)
	if err != nil || got == nil || got.ID != "a" {
		t.Fatalf("GetTransferByKey = %+v, %v; want row a", got, err)
	}
	if _, err := db.CreateTransferWithinLimits(idemTransfer("b", "dev1", key), lim); err == nil {
		t.Fatal("a duplicate key through the limited insert must be refused by the index")
	}
}
