package backup

import (
	"bufio"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"golang.org/x/crypto/scrypt"
)

// File layout of an encrypted backup (all fields fixed size, the header doubles as the authenticated data of every
// chunk): magic, format version, log2(N), r, p of scrypt, 16 random salt bytes, 7 random nonce-prefix bytes, then the
// plaintext in 64 KiB chunks each sealed with AES-256-GCM. A chunk's nonce is the prefix, a 32-bit chunk counter and a
// final-chunk flag, so chunks cannot be reordered, dropped from the end or appended to without failing to open.
const (
	encMagic       = "RFEBKENC"
	encVersion     = 1
	encSaltLen     = 16
	encPrefixLen   = 7
	encHeaderLen   = len(encMagic) + 1 + 3 + encSaltLen + encPrefixLen
	encChunkSize   = 64 << 10
	encKeyLen      = 32
	encScryptR     = 8
	encScryptP     = 1
	encMinLog2N    = 12
	encMaxLog2N    = 18 // a header asking for more would make Decrypt allocate more than 256 MiB
	encDefaultLog2 = 17 // 128 MiB, a few tenths of a second
	// EncryptedSuffix is appended to an archive's name for its off-device copy.
	EncryptedSuffix = ".enc"
)

// encryptLog2N is the scrypt cost Encrypt writes; a variable only so tests can use a cheap one.
var encryptLog2N = encDefaultLog2

// ErrDecrypt is returned when an encrypted file cannot be opened: the passphrase is wrong or the file was changed.
// The two cannot be told apart, on purpose.
var ErrDecrypt = errors.New("cannot decrypt: wrong passphrase, or the file is damaged or was altered")

// ErrNotEncrypted means the input does not start with the encrypted-backup header.
var ErrNotEncrypted = errors.New("not an encrypted RFE backup (bad header)")

// deriveKey stretches the passphrase into an AES-256 key with scrypt.
func deriveKey(pass, salt []byte, log2N, r, p int) ([]byte, error) {
	return scrypt.Key(pass, salt, 1<<log2N, r, p, encKeyLen)
}

// newGCM returns an AES-256-GCM AEAD for key.
func newGCM(key []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

// chunkNonce builds the 12-byte nonce for chunk counter ctr; final marks the last chunk of the stream.
func chunkNonce(prefix []byte, ctr uint32, final bool) []byte {
	n := make([]byte, 12)
	copy(n, prefix)
	binary.BigEndian.PutUint32(n[encPrefixLen:], ctr)
	if final {
		n[11] = 1
	}
	return n
}

// Encrypt reads plaintext from r and writes the encrypted form to w, using a key derived from passphrase with scrypt
// and a fresh random salt and nonce prefix. The plaintext is processed in 64 KiB chunks, so memory use does not grow
// with the file. An empty passphrase is refused.
func Encrypt(w io.Writer, r io.Reader, passphrase []byte) error {
	if len(passphrase) == 0 {
		return errors.New("an empty passphrase is not allowed")
	}
	header := make([]byte, 0, encHeaderLen)
	header = append(header, encMagic...)
	header = append(header, encVersion, byte(encryptLog2N), encScryptR, encScryptP)
	random := make([]byte, encSaltLen+encPrefixLen)
	if _, err := rand.Read(random); err != nil {
		return fmt.Errorf("random: %w", err)
	}
	header = append(header, random...)
	salt, prefix := random[:encSaltLen], random[encSaltLen:]
	key, err := deriveKey(passphrase, salt, encryptLog2N, encScryptR, encScryptP)
	if err != nil {
		return err
	}
	aead, err := newGCM(key)
	if err != nil {
		return err
	}
	if _, err := w.Write(header); err != nil {
		return err
	}
	br := bufio.NewReaderSize(r, encChunkSize+1)
	buf := make([]byte, encChunkSize)
	for ctr := uint32(0); ; ctr++ {
		n, err := io.ReadFull(br, buf)
		if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF) {
			return err
		}
		final := n < encChunkSize
		if !final {
			if _, perr := br.Peek(1); errors.Is(perr, io.EOF) {
				final = true
			} else if perr != nil {
				return perr
			}
		}
		if _, err := w.Write(aead.Seal(nil, chunkNonce(prefix, ctr, final), buf[:n], header)); err != nil {
			return err
		}
		if final {
			return nil
		}
		if ctr == math.MaxUint32 {
			return errors.New("input is too large to encrypt")
		}
	}
}

// Decrypt reads an encrypted backup from r and writes the plaintext to w. Each chunk is authenticated before any of
// it is written, and a stream that was cut short, extended or edited fails with ErrDecrypt (output already written
// is then not to be trusted, so callers write to a temporary file). A wrong passphrase also gives ErrDecrypt.
func Decrypt(w io.Writer, r io.Reader, passphrase []byte) error {
	if len(passphrase) == 0 {
		return errors.New("an empty passphrase is not allowed")
	}
	header := make([]byte, encHeaderLen)
	if _, err := io.ReadFull(r, header); err != nil {
		return ErrNotEncrypted
	}
	if string(header[:len(encMagic)]) != encMagic || header[len(encMagic)] != encVersion {
		return ErrNotEncrypted
	}
	log2N, rr, pp := int(header[9]), int(header[10]), int(header[11])
	if log2N < encMinLog2N || log2N > encMaxLog2N || rr != encScryptR || pp != encScryptP {
		return fmt.Errorf("unsupported encryption parameters (log2N=%d r=%d p=%d)", log2N, rr, pp)
	}
	salt := header[12 : 12+encSaltLen]
	prefix := header[12+encSaltLen:]
	key, err := deriveKey(passphrase, salt, log2N, rr, pp)
	if err != nil {
		return err
	}
	aead, err := newGCM(key)
	if err != nil {
		return err
	}
	br := bufio.NewReaderSize(r, encChunkSize+aead.Overhead()+1)
	buf := make([]byte, encChunkSize+aead.Overhead())
	for ctr := uint32(0); ; ctr++ {
		n, err := io.ReadFull(br, buf)
		if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF) {
			return err
		}
		final := n < len(buf)
		if !final {
			if _, perr := br.Peek(1); errors.Is(perr, io.EOF) {
				final = true
			} else if perr != nil {
				return perr
			}
		}
		plain, err := aead.Open(nil, chunkNonce(prefix, ctr, final), buf[:n], header)
		if err != nil {
			return ErrDecrypt
		}
		if _, err := w.Write(plain); err != nil {
			return err
		}
		if final {
			return nil
		}
		if ctr == math.MaxUint32 {
			return ErrDecrypt
		}
	}
}

// writeViaTemp creates dst (mode 0600) by running fill into a temporary file next to it, syncing and renaming, so a
// failure never leaves a partial file under dst. It refuses to replace an existing dst.
func writeViaTemp(dst string, fill func(w io.Writer) error) error {
	if _, err := os.Lstat(dst); err == nil {
		return fmt.Errorf("%s already exists", dst)
	}
	dir := filepath.Dir(dst)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".partial-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	fail := func(err error) error {
		tmp.Close()
		os.Remove(name)
		return err
	}
	if err := os.Chmod(name, 0o600); err != nil {
		return fail(err)
	}
	bw := bufio.NewWriter(tmp)
	if err := fill(bw); err != nil {
		return fail(err)
	}
	if err := bw.Flush(); err != nil {
		return fail(err)
	}
	if err := tmp.Sync(); err != nil {
		return fail(err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(name)
		return err
	}
	if err := os.Rename(name, dst); err != nil {
		os.Remove(name)
		return err
	}
	return syncDir(dir)
}

// EncryptFile encrypts the file src into dst (new file, mode 0600, written atomically).
func EncryptFile(src, dst string, passphrase []byte) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	return writeViaTemp(dst, func(w io.Writer) error { return Encrypt(w, in, passphrase) })
}

// DecryptFile decrypts src into the new file dst (mode 0600, written atomically; nothing is left behind on failure).
func DecryptFile(src, dst string, passphrase []byte) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	return writeViaTemp(dst, func(w io.Writer) error { return Decrypt(w, in, passphrase) })
}

// PruneOffsite deletes the oldest "*.enc" files in dir (by modification time) until keep remain, so the off-device
// folder does not grow without bound. Other files are never touched; a missing dir is not an error.
func PruneOffsite(dir string, keep int) error {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	type file struct {
		path string
		mod  time.Time
	}
	var files []file
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), EncryptedSuffix) {
			continue
		}
		info, err := e.Info()
		if err != nil || !info.Mode().IsRegular() {
			continue
		}
		files = append(files, file{filepath.Join(dir, e.Name()), info.ModTime()})
	}
	sort.Slice(files, func(i, j int) bool { return files[i].mod.After(files[j].mod) })
	var first error
	for i := keep; i < len(files); i++ {
		if err := os.Remove(files[i].path); err != nil && first == nil {
			first = err
		}
	}
	return first
}

// CopyOffsite encrypts archive with passphrase into dir (created 0700 if missing) as <archive name>.enc and returns
// the new path. An existing file of that name is left alone and counts as success: the same backup was already copied.
func CopyOffsite(archive, dir string, passphrase []byte) (string, error) {
	dst := filepath.Join(dir, filepath.Base(archive)+EncryptedSuffix)
	if _, err := os.Lstat(dst); err == nil {
		return dst, nil
	}
	return dst, EncryptFile(archive, dst, passphrase)
}
