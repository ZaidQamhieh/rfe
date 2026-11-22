package backup

import (
	"bytes"
	"crypto/rand"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

// cheapKDF lowers the scrypt cost for the duration of a test.
func cheapKDF(t *testing.T) {
	t.Helper()
	old := encryptLog2N
	encryptLog2N = encMinLog2N
	t.Cleanup(func() { encryptLog2N = old })
}

func seal(t *testing.T, plain, pass []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := Encrypt(&buf, bytes.NewReader(plain), pass); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestEncryptDecryptRoundTripAcrossChunkBoundaries(t *testing.T) {
	cheapKDF(t)
	pass := []byte("correct horse")
	for _, n := range []int{0, 1, encChunkSize - 1, encChunkSize, encChunkSize + 1, 3*encChunkSize + 17} {
		plain := make([]byte, n)
		rand.Read(plain)
		enc := seal(t, plain, pass)
		if n >= 16 && bytes.Contains(enc, plain[:min(n, 32)]) {
			t.Fatalf("size %d: plaintext visible in the ciphertext", n)
		}
		var out bytes.Buffer
		if err := Decrypt(&out, bytes.NewReader(enc), pass); err != nil {
			t.Fatalf("size %d: %v", n, err)
		}
		if !bytes.Equal(out.Bytes(), plain) {
			t.Fatalf("size %d: round trip differs", n)
		}
	}
}

func TestPruneOffsiteKeepsNewestEncFilesOnly(t *testing.T) {
	dir := t.TempDir()
	base := time.Now().Add(-time.Hour)
	for i, name := range []string{"a.enc", "b.enc", "c.enc", "d.enc", "keep.txt"} {
		p := filepath.Join(dir, name)
		os.WriteFile(p, []byte("x"), 0o600)
		os.Chtimes(p, base.Add(time.Duration(i)*time.Minute), base.Add(time.Duration(i)*time.Minute))
	}
	if err := PruneOffsite(dir, 2); err != nil {
		t.Fatal(err)
	}
	for name, want := range map[string]bool{"a.enc": false, "b.enc": false, "c.enc": true, "d.enc": true, "keep.txt": true} {
		if _, err := os.Stat(filepath.Join(dir, name)); (err == nil) != want {
			t.Errorf("%s present=%v, want %v", name, err == nil, want)
		}
	}
	if err := PruneOffsite(filepath.Join(dir, "missing"), 2); err != nil {
		t.Fatalf("missing dir: %v", err)
	}
}

func TestDecryptWrongPassphraseFails(t *testing.T) {
	cheapKDF(t)
	enc := seal(t, []byte("secret archive"), []byte("right"))
	var out bytes.Buffer
	if err := Decrypt(&out, bytes.NewReader(enc), []byte("wrong")); !errors.Is(err, ErrDecrypt) {
		t.Fatalf("want ErrDecrypt, got %v", err)
	}
	if out.Len() != 0 {
		t.Errorf("wrote %d bytes before failing", out.Len())
	}
	if err := Decrypt(&out, bytes.NewReader(enc), nil); err == nil {
		t.Error("empty passphrase must be refused")
	}
}

func TestDecryptDetectsTamperTruncationAndExtension(t *testing.T) {
	cheapKDF(t)
	pass := []byte("pw")
	plain := bytes.Repeat([]byte("0123456789abcdef"), encChunkSize/16*2+5) // three chunks
	enc := seal(t, plain, pass)
	flip := func(i int) []byte { c := append([]byte{}, enc...); c[i] ^= 1; return c }
	chunk := encChunkSize + 16
	cases := map[string][]byte{
		"flipped header salt":    flip(14),
		"flipped first chunk":    flip(encHeaderLen + 10),
		"flipped last byte":      flip(len(enc) - 1),
		"truncated at boundary":  enc[:encHeaderLen+chunk],
		"truncated mid chunk":    enc[:len(enc)-5],
		"extra bytes at the end": append(append([]byte{}, enc...), 'x'),
		"header only":            enc[:encHeaderLen],
		"chunks swapped": append(append(append(append([]byte{}, enc[:encHeaderLen]...), enc[encHeaderLen+chunk:encHeaderLen+2*chunk]...),
			enc[encHeaderLen:encHeaderLen+chunk]...), enc[encHeaderLen+2*chunk:]...),
	}
	for name, bad := range cases {
		if err := Decrypt(&bytes.Buffer{}, bytes.NewReader(bad), pass); err == nil {
			t.Errorf("%s: tampered input was accepted", name)
		}
	}
}

func TestDecryptRejectsForeignAndAbusiveHeaders(t *testing.T) {
	cheapKDF(t)
	if err := Decrypt(&bytes.Buffer{}, bytes.NewReader([]byte("plain text that is not a backup, long enough to hold a header")), []byte("pw")); !errors.Is(err, ErrNotEncrypted) {
		t.Errorf("foreign file: %v", err)
	}
	enc := seal(t, []byte("x"), []byte("pw"))
	enc[9] = 30 // log2N far above the cap: must be refused before any key derivation
	if err := Decrypt(&bytes.Buffer{}, bytes.NewReader(enc), []byte("pw")); err == nil || errors.Is(err, ErrDecrypt) {
		t.Errorf("abusive cost parameter: %v", err)
	}
}

func TestEncryptFileDecryptFileAreAtomicAndPrivate(t *testing.T) {
	cheapKDF(t)
	dir := t.TempDir()
	src := filepath.Join(dir, "a.tar.gz")
	if err := os.WriteFile(src, []byte("archive bytes"), 0o600); err != nil {
		t.Fatal(err)
	}
	enc := filepath.Join(dir, "off", "a.tar.gz.enc")
	if err := EncryptFile(src, enc, []byte("pw")); err != nil {
		t.Fatal(err)
	}
	if st, err := os.Stat(enc); err != nil || runtime.GOOS != "windows" && st.Mode().Perm() != 0o600 {
		t.Fatalf("encrypted file: %v %v", st, err)
	}
	out := filepath.Join(dir, "back.tar.gz")
	if err := DecryptFile(enc, out, []byte("nope")); err == nil {
		t.Fatal("wrong passphrase accepted")
	}
	if _, err := os.Stat(out); err == nil {
		t.Error("failed decrypt left an output file")
	}
	if left, _ := filepath.Glob(filepath.Join(dir, ".partial-*")); len(left) != 0 {
		t.Errorf("partial files left: %v", left)
	}
	if err := DecryptFile(enc, out, []byte("pw")); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(out); string(got) != "archive bytes" {
		t.Errorf("decrypted %q", got)
	}
	if err := DecryptFile(enc, out, []byte("pw")); err == nil {
		t.Error("an existing output file must not be replaced")
	}
}

func TestCopyOffsiteWritesEncryptedCopyOnceAndRoundTrips(t *testing.T) {
	cheapKDF(t)
	dir := newDirWithArchive(t)
	off := filepath.Join(t.TempDir(), "usb")
	dst, err := CopyOffsite(dir, off, []byte("pw"))
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(dst) != filepath.Base(dir)+EncryptedSuffix {
		t.Errorf("name %s", dst)
	}
	first, _ := os.ReadFile(dst)
	if _, err := CopyOffsite(dir, off, []byte("other")); err != nil {
		t.Fatal(err)
	}
	if again, _ := os.ReadFile(dst); !bytes.Equal(first, again) {
		t.Error("an existing copy was overwritten")
	}
	plain := filepath.Join(t.TempDir(), "restored.tar.gz")
	if err := DecryptFile(dst, plain, []byte("pw")); err != nil {
		t.Fatal(err)
	}
	if _, err := Verify(plain); err != nil { // the file name is irrelevant to Verify; the content must still verify
		t.Errorf("decrypted archive does not verify: %v", err)
	}
}

// newDirWithArchive makes a real backup archive and returns its path.
func newDirWithArchive(t *testing.T) string {
	t.Helper()
	return makeArchive(t)
}
