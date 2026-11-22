package backup

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ZaidQamhieh/rfe/agent/internal/security"
	"github.com/ZaidQamhieh/rfe/agent/internal/store"
)

// newDataDir builds a data dir the way the daemon does: identity first, then the database, plus an
// account-server key file. The returned database stays open (and in WAL mode) until the test ends.
func newDataDir(t *testing.T) (string, *store.DB) {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "data")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := security.LoadOrCreateCert(dir); err != nil {
		t.Fatal(err)
	}
	db, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	for i, id := range []string{"dev-a", "dev-b", "dev-c"} {
		if err := db.CreateDevice(id, "phone "+id, "token-"+id+strings.Repeat("x", i)); err != nil {
			t.Fatal(err)
		}
	}
	key := bytes.Repeat([]byte("signing-key-bytes-"), 8)
	if err := os.WriteFile(filepath.Join(dir, store.AccountKeyFile), key, 0o600); err != nil {
		t.Fatal(err)
	}
	return dir, db
}

func readFile(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func tokenHashes(t *testing.T, dir string) map[string]string {
	t.Helper()
	db, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	devs, err := db.ListDevices()
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]string{}
	for _, d := range devs {
		out[d.ID] = d.TokenHash
	}
	return out
}

func memberNames(t *testing.T, archive string) []string {
	t.Helper()
	f, err := os.Open(archive)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	tr := tar.NewReader(gz)
	var names []string
	for {
		h, err := tr.Next()
		if err == io.EOF {
			return names
		}
		if err != nil {
			t.Fatal(err)
		}
		names = append(names, h.Name)
	}
}

// rewrite copies an archive to dst, letting mutate change each member's bytes; a nil result drops it.
// extra members are appended unchanged.
func rewrite(t *testing.T, src, dst string, mutate func(name string, data []byte) []byte, extra map[string][]byte) {
	t.Helper()
	in, err := os.Open(src)
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	gz, err := gzip.NewReader(in)
	if err != nil {
		t.Fatal(err)
	}
	tr := tar.NewReader(gz)
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)
	put := func(name string, data []byte) {
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o600, Size: int64(len(data)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write(data); err != nil {
			t.Fatal(err)
		}
	}
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		data, err := io.ReadAll(tr)
		if err != nil {
			t.Fatal(err)
		}
		if data = mutate(h.Name, data); data != nil {
			put(h.Name, data)
		}
	}
	for name, data := range extra {
		put(name, data)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dst, buf.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
}

func identity(_ string, data []byte) []byte { return data }

func TestSnapshotVerifyRestoreRoundTrip(t *testing.T) {
	dir, db := newDataDir(t)
	// A write after Open sits in the -wal file; the snapshot must still contain it.
	if err := db.CreateDevice("dev-late", "late", "token-late"); err != nil {
		t.Fatal(err)
	}
	backups := filepath.Join(t.TempDir(), "backups")
	archive, m, err := Create(dir, backups, Options{Label: LabelManual, AgentVersion: "test-1.2.3"})
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(archive) != ArchiveName(LabelManual, m.Created) {
		t.Fatalf("archive name %q does not follow the scheme", archive)
	}
	for _, n := range memberNames(t, archive) {
		if strings.HasSuffix(n, "-wal") || strings.HasSuffix(n, "-shm") {
			t.Fatalf("archive contains %s", n)
		}
	}
	if entries, _ := os.ReadDir(backups); len(entries) != 1 {
		t.Fatalf("backup dir has %d entries after Create, want only the archive", len(entries))
	}
	vm, err := Verify(archive)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if vm.AgentVersion != "test-1.2.3" || vm.CertFingerprint == "" || vm.Format != FormatVersion {
		t.Fatalf("manifest = %+v", vm)
	}

	target := filepath.Join(t.TempDir(), "restored")
	if _, err := Restore(archive, target); err != nil {
		t.Fatalf("Restore: %v", err)
	}
	for _, name := range []string{CertMember, KeyMember, AccountKey} {
		if !bytes.Equal(readFile(t, filepath.Join(dir, name)), readFile(t, filepath.Join(target, name))) {
			t.Fatalf("%s differs after restore", name)
		}
	}
	for _, name := range []string{DBMember + "-wal", DBMember + "-shm"} {
		if _, err := os.Stat(filepath.Join(target, name)); err == nil {
			t.Fatalf("restored dir contains %s", name)
		}
	}
	want := tokenHashes(t, dir)
	got := tokenHashes(t, target)
	if len(want) != 4 || !reflect.DeepEqual(want, got) {
		t.Fatalf("token hashes after restore = %v, want %v", got, want)
	}
	// The restored identity is the one phones pin.
	cert, err := security.LoadCert(target)
	if err != nil {
		t.Fatal(err)
	}
	if security.Fingerprint(cert) != m.CertFingerprint {
		t.Fatal("restored certificate fingerprint differs from the manifest")
	}
	// The original dir was not touched.
	if _, err := os.Stat(filepath.Join(dir, DBMember)); err != nil {
		t.Fatal(err)
	}
}

func TestCreateWithoutAccountKeyOmitsIt(t *testing.T) {
	dir, _ := newDataDir(t)
	if err := os.Remove(filepath.Join(dir, store.AccountKeyFile)); err != nil {
		t.Fatal(err)
	}
	archive, _, err := Create(dir, t.TempDir(), Options{})
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range memberNames(t, archive) {
		if n == AccountKey {
			t.Fatal("archive has an account key the data dir does not")
		}
	}
	if _, err := Verify(archive); err != nil {
		t.Fatal(err)
	}
}

func TestRestoreRefusesNonEmptyDir(t *testing.T) {
	dir, _ := newDataDir(t)
	archive, _, err := Create(dir, t.TempDir(), Options{})
	if err != nil {
		t.Fatal(err)
	}
	target := t.TempDir()
	existing := filepath.Join(target, "precious.txt")
	if err := os.WriteFile(existing, []byte("keep me"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err = Restore(archive, target)
	if !errors.Is(err, ErrRestoreTargetNotEmpty) {
		t.Fatalf("Restore into a non-empty dir = %v, want ErrRestoreTargetNotEmpty", err)
	}
	if string(readFile(t, existing)) != "keep me" {
		t.Fatal("existing file was changed")
	}
	entries, _ := os.ReadDir(target)
	if len(entries) != 1 {
		t.Fatalf("target has %d entries after a refused restore, want 1", len(entries))
	}
	// A file at the target path is refused too.
	file := filepath.Join(t.TempDir(), "afile")
	os.WriteFile(file, []byte("x"), 0o600)
	if _, err := Restore(archive, file); !errors.Is(err, ErrRestoreTargetNotEmpty) {
		t.Fatalf("Restore onto a file = %v", err)
	}
	// An existing empty directory is fine, and no staging dir is left beside it.
	empty := filepath.Join(t.TempDir(), "empty")
	if err := os.Mkdir(empty, 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := Restore(archive, empty); err != nil {
		t.Fatalf("Restore into an empty dir: %v", err)
	}
	if siblings, _ := os.ReadDir(filepath.Dir(empty)); len(siblings) != 1 {
		t.Fatalf("parent has %d entries after restore, want only the target", len(siblings))
	}
}

func TestVerifyDetectsTampering(t *testing.T) {
	dir, _ := newDataDir(t)
	archive, _, err := Create(dir, t.TempDir(), Options{})
	if err != nil {
		t.Fatal(err)
	}
	work := t.TempDir()

	// One flipped byte inside a member (archive otherwise well formed).
	for _, name := range []string{DBMember, CertMember, KeyMember, AccountKey} {
		bad := filepath.Join(work, "bad-"+name+".tar.gz")
		rewrite(t, archive, bad, func(n string, data []byte) []byte {
			if n != name {
				return data
			}
			c := append([]byte(nil), data...)
			c[len(c)/2] ^= 0x01
			return c
		}, nil)
		if _, err := Verify(bad); err == nil {
			t.Fatalf("Verify accepted an archive with a changed byte in %s", name)
		}
		if _, err := Restore(bad, filepath.Join(work, "r-"+name)); err == nil {
			t.Fatalf("Restore accepted an archive with a changed byte in %s", name)
		}
		if _, err := os.Stat(filepath.Join(work, "r-"+name)); err == nil {
			t.Fatalf("failed restore left r-%s behind", name)
		}
	}

	// One flipped byte in the compressed file itself.
	raw := readFile(t, archive)
	raw[len(raw)/2] ^= 0x01
	flipped := filepath.Join(work, "flipped.tar.gz")
	if err := os.WriteFile(flipped, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Verify(flipped); err == nil {
		t.Fatal("Verify accepted a flipped byte in the compressed archive")
	}

	// A missing member, and a member the manifest does not list.
	missing := filepath.Join(work, "missing.tar.gz")
	rewrite(t, archive, missing, func(n string, data []byte) []byte {
		if n == KeyMember {
			return nil
		}
		return data
	}, nil)
	if _, err := Verify(missing); err == nil {
		t.Fatal("Verify accepted an archive without the key")
	}
	extra := filepath.Join(work, "extra.tar.gz")
	rewrite(t, archive, extra, identity, map[string][]byte{"agent.db-wal": []byte("x")})
	if _, err := Verify(extra); err == nil {
		t.Fatal("Verify accepted an unexpected member")
	}
}

func TestExtractRefusesPathsAndLinks(t *testing.T) {
	dir, _ := newDataDir(t)
	archive, _, err := Create(dir, t.TempDir(), Options{})
	if err != nil {
		t.Fatal(err)
	}
	work := t.TempDir()
	for _, name := range []string{"../escape", "/abs/path", "sub/agent.db", "agent-cert.pem/../x"} {
		bad := filepath.Join(work, "p.tar.gz")
		rewrite(t, archive, bad, identity, map[string][]byte{name: []byte("x")})
		if _, err := Verify(bad); err == nil {
			t.Fatalf("Verify accepted member %q", name)
		}
	}
	if _, err := os.Stat(filepath.Join(work, "escape")); err == nil {
		t.Fatal("a member escaped the extraction dir")
	}

	// A symlink entry named like a real member.
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)
	tw.WriteHeader(&tar.Header{Name: CertMember, Typeflag: tar.TypeSymlink, Linkname: "/etc/passwd"})
	tw.Close()
	gw.Close()
	link := filepath.Join(work, "link.tar.gz")
	os.WriteFile(link, buf.Bytes(), 0o600)
	if _, err := Verify(link); err == nil {
		t.Fatal("Verify accepted a symlink member")
	}
	if _, err := Verify(filepath.Join(work, "nonexistent.tar.gz")); err == nil {
		t.Fatal("Verify accepted a missing file")
	}
}

func TestCreateRefusesWithoutIdentity(t *testing.T) {
	dir, db := newDataDir(t)
	_ = db
	if err := os.Remove(filepath.Join(dir, security.KeyFile)); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(t.TempDir(), "backups")
	_, _, err := Create(dir, dest, Options{})
	if !errors.Is(err, security.ErrIdentityMissing) {
		t.Fatalf("Create without a key = %v, want ErrIdentityMissing", err)
	}
	if _, err := os.Stat(dest); err == nil {
		t.Fatal("Create made the destination dir before checking the identity")
	}
}

func TestCreateRejectsBadLabel(t *testing.T) {
	dir, _ := newDataDir(t)
	for _, label := range []string{"../x", "Daily", "a b", "a-b"} {
		if _, _, err := Create(dir, t.TempDir(), Options{Label: label}); err == nil {
			t.Fatalf("label %q accepted", label)
		}
	}
}

func TestSnapshotLeavesNoWALAndIsConsistent(t *testing.T) {
	dir, db := newDataDir(t)
	if err := db.RevokeDevice("dev-a"); err != nil {
		t.Fatal(err)
	}
	dest := t.TempDir()
	path, err := Snapshot(dir, dest)
	if err != nil {
		t.Fatal(err)
	}
	entries, _ := os.ReadDir(dest)
	if len(entries) != 1 || entries[0].Name() != DBMember {
		t.Fatalf("snapshot dir = %v, want only %s", entries, DBMember)
	}
	if _, err := inspectDB(path); err != nil {
		t.Fatal(err)
	}
	if _, err := Snapshot(dir, dest); err == nil {
		t.Fatal("Snapshot overwrote an existing target")
	}
	if _, err := Snapshot(t.TempDir(), t.TempDir()); err == nil {
		t.Fatal("Snapshot of a dir with no database succeeded")
	}
}

func TestRevocationReplayAfterRestore(t *testing.T) {
	dir, db := newDataDir(t)
	archive, m, err := Create(dir, t.TempDir(), Options{})
	if err != nil {
		t.Fatal(err)
	}
	// After the backup: revoke dev-a and remove dev-b on the live host.
	time.Sleep(10 * time.Millisecond)
	if err := db.RevokeDevice("dev-a"); err != nil {
		t.Fatal(err)
	}
	if err := db.DeleteDevice("dev-b"); err != nil {
		t.Fatal(err)
	}
	// An entry from before the backup (dev-c was re-paired since) must not be replayed.
	if err := store.AppendRevocation(dir, "dev-c", store.RevocationRevoke, m.Created.Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	logPath := filepath.Join(dir, store.RevocationsLogFile)

	target := filepath.Join(t.TempDir(), "restored")
	rm, n, err := RestoreWithRevocations(archive, target, logPath)
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 || !rm.Created.Equal(m.Created) {
		t.Fatalf("applied %d revocations (created %v), want 2", n, rm.Created)
	}
	rdb, err := store.Open(target)
	if err != nil {
		t.Fatal(err)
	}
	defer rdb.Close()
	devs, err := rdb.ListDevices()
	if err != nil {
		t.Fatal(err)
	}
	state := map[string]bool{}
	for _, d := range devs {
		state[d.ID] = d.Revoked
	}
	if revoked, ok := state["dev-a"]; !ok || !revoked {
		t.Fatalf("dev-a state = %v (present %v), want revoked", revoked, ok)
	}
	if _, ok := state["dev-b"]; ok {
		t.Fatal("dev-b still exists after replayed removal")
	}
	if revoked, ok := state["dev-c"]; !ok || revoked {
		t.Fatalf("dev-c state = %v (present %v), want present and not revoked", revoked, ok)
	}
	if !bytes.Equal(readFile(t, logPath), readFile(t, filepath.Join(target, store.RevocationsLogFile))) {
		t.Fatal("restored dir did not receive the revocation log")
	}
	// Replaying again changes nothing.
	again, err := ReplayRevocations(rdb, logPath, m.Created)
	if err != nil || again != 2 {
		t.Fatalf("second replay = %d, %v", again, err)
	}
}

func TestReplayWithMissingLogIsNoOp(t *testing.T) {
	dir, _ := newDataDir(t)
	archive, _, err := Create(dir, t.TempDir(), Options{})
	if err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "restored")
	_, n, err := RestoreWithRevocations(archive, target, filepath.Join(t.TempDir(), "none.log"))
	if err != nil || n != 0 {
		t.Fatalf("RestoreWithRevocations = %d, %v", n, err)
	}
	if _, err := os.Stat(filepath.Join(target, store.RevocationsLogFile)); err == nil {
		t.Fatal("a log appeared from nothing")
	}
}

func fakeArchives(t *testing.T, dir string, label string, days int, size int) {
	t.Helper()
	base := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	for i := 0; i < days; i++ {
		name := ArchiveName(label, base.AddDate(0, 0, i))
		if err := os.WriteFile(filepath.Join(dir, name), bytes.Repeat([]byte("z"), size), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

func count(t *testing.T, dir, label string) int {
	t.Helper()
	files, err := listArchives(dir)
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, f := range files {
		if f.label == label {
			n++
		}
	}
	return n
}

func TestPruneRetentionCounts(t *testing.T) {
	dir := t.TempDir()
	fakeArchives(t, dir, LabelPremigration, 6, 10)
	fakeArchives(t, dir, LabelDaily, 12, 10)
	fakeArchives(t, dir, LabelWeekly, 9, 10)
	fakeArchives(t, dir, LabelManual, 5, 10)
	os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("mine"), 0o600)
	os.WriteFile(filepath.Join(dir, "rfe-backup-daily-notadate.tar.gz"), []byte("mine"), 0o600)
	os.Mkdir(filepath.Join(dir, ArchiveName(LabelDaily, time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC))), 0o700)

	removed, err := Prune(dir, DefaultPolicy())
	if err != nil {
		t.Fatal(err)
	}
	if len(removed) != 3+5+5 {
		t.Fatalf("removed %d, want 13: %v", len(removed), removed)
	}
	for label, want := range map[string]int{LabelPremigration: 3, LabelDaily: 7, LabelWeekly: 4, LabelManual: 5} {
		if got := count(t, dir, label); got != want {
			t.Errorf("%s kept %d, want %d", label, got, want)
		}
	}
	// The newest of each limited label survived, the oldest did not.
	base := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	if _, err := os.Stat(filepath.Join(dir, ArchiveName(LabelDaily, base.AddDate(0, 0, 11)))); err != nil {
		t.Fatal("newest daily was removed")
	}
	if _, err := os.Stat(filepath.Join(dir, ArchiveName(LabelDaily, base))); err == nil {
		t.Fatal("oldest daily was kept")
	}
	for _, keepName := range []string{"notes.txt", "rfe-backup-daily-notadate.tar.gz"} {
		if _, err := os.Stat(filepath.Join(dir, keepName)); err != nil {
			t.Fatalf("Prune touched %s", keepName)
		}
	}
	if st, err := os.Stat(filepath.Join(dir, ArchiveName(LabelDaily, time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)))); err != nil || !st.IsDir() {
		t.Fatal("Prune touched a directory named like an archive")
	}
	// A second run removes nothing.
	if removed, err := Prune(dir, DefaultPolicy()); err != nil || len(removed) != 0 {
		t.Fatalf("second Prune = %v, %v", removed, err)
	}
}

func TestPruneSizeCapDropsOldestButNeverTheNewest(t *testing.T) {
	dir := t.TempDir()
	fakeArchives(t, dir, LabelDaily, 5, 100)
	p := Policy{KeepPremigration: 3, KeepDaily: 7, KeepWeekly: 4, MaxTotalBytes: 250}
	removed, err := Prune(dir, p)
	if err != nil {
		t.Fatal(err)
	}
	if len(removed) != 3 || count(t, dir, LabelDaily) != 2 {
		t.Fatalf("removed %d, kept %d; want 3 removed, 2 kept", len(removed), count(t, dir, LabelDaily))
	}
	base := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	for _, d := range []int{3, 4} {
		if _, err := os.Stat(filepath.Join(dir, ArchiveName(LabelDaily, base.AddDate(0, 0, d)))); err != nil {
			t.Fatalf("day %d should have been kept", d)
		}
	}
	// A cap smaller than one archive still keeps the newest.
	if _, err := Prune(dir, Policy{KeepDaily: 7, MaxTotalBytes: 1}); err != nil {
		t.Fatal(err)
	}
	if count(t, dir, LabelDaily) != 1 {
		t.Fatalf("kept %d, want exactly the newest", count(t, dir, LabelDaily))
	}
	if _, err := os.Stat(filepath.Join(dir, ArchiveName(LabelDaily, base.AddDate(0, 0, 4)))); err != nil {
		t.Fatal("the newest archive was removed")
	}
	if _, err := Prune(filepath.Join(dir, "missing"), DefaultPolicy()); err == nil {
		t.Fatal("Prune of a missing dir succeeded")
	}
}
