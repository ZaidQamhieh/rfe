// Package security handles the agent's TLS identity and token hashing.
package security

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// ErrIdentityMissing means the data directory already holds an agent database but not the TLS
// certificate and key that identify it. A fresh identity would make every paired phone reject the host,
// so it is never generated silently; the files must be restored (for example from a backup).
var ErrIdentityMissing = errors.New("host identity missing")

// Names of the identity files and of the database whose presence marks a dir as already set up.
const (
	CertFile = "agent-cert.pem"
	KeyFile  = "agent-key.pem"
	dbFile   = "agent.db"
)

// LoadCert reads the existing certificate and key under dir and never creates or changes a file.
// It returns an error wrapping ErrIdentityMissing when either file is absent.
func LoadCert(dir string) (tls.Certificate, error) {
	certPath := filepath.Join(dir, CertFile)
	keyPath := filepath.Join(dir, KeyFile)
	if missing := missingIdentityFiles(certPath, keyPath); len(missing) > 0 {
		return tls.Certificate{}, fmt.Errorf("%w: %s not found in %s", ErrIdentityMissing, strings.Join(missing, " and "), dir)
	}
	return tls.LoadX509KeyPair(certPath, keyPath)
}

// missingIdentityFiles lists the base names of those of the given files that do not exist.
func missingIdentityFiles(paths ...string) []string {
	var missing []string
	for _, p := range paths {
		if !fileExists(p) {
			missing = append(missing, filepath.Base(p))
		}
	}
	return missing
}

// LoadOrCreateCert returns the agent's TLS certificate, generating a self-signed
// one on first run and persisting it under dir. The certificate's SHA-256
// fingerprint is what the phone pins at pairing time (trust on first use).
// When agent.db already exists in dir but the certificate or key is missing, it refuses with
// ErrIdentityMissing instead of generating a new identity.
func LoadOrCreateCert(dir string) (tls.Certificate, error) {
	certPath := filepath.Join(dir, CertFile)
	keyPath := filepath.Join(dir, KeyFile)

	if fileExists(certPath) && fileExists(keyPath) {
		return tls.LoadX509KeyPair(certPath, keyPath)
	}
	if _, err := os.Stat(filepath.Join(dir, dbFile)); err == nil {
		return tls.Certificate{}, fmt.Errorf("%w: %s not found in %s although %s exists; refusing to create a new identity because paired phones would reject it. Restore the files from a backup (rfe restore) or remove the data dir to start over",
			ErrIdentityMissing, strings.Join(missingIdentityFiles(certPath, keyPath), " and "), dir, dbFile)
	} else if !errors.Is(err, fs.ErrNotExist) {
		return tls.Certificate{}, fmt.Errorf("check %s in %s: %w", dbFile, dir, err)
	}

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("generate key: %w", err)
	}

	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return tls.Certificate{}, err
	}

	tmpl := x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: "remote-file-explorer-agent"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().AddDate(10, 0, 0),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		// The cert is pinned by fingerprint (trust-on-first-use), so hostname
		// verification is bypassed and the SAN list is only a courtesy: it
		// covers localhost plus every non-loopback interface IP — which on a
		// Tailscale host includes the 100.x Tailscale IP. The Tailscale DNS
		// name is intentionally not listed; pinning makes it unnecessary.
		DNSNames:    []string{"localhost"},
		IPAddresses: localIPs(),
	}

	der, err := x509.CreateCertificate(rand.Reader, &tmpl, &tmpl, &key.PublicKey, key)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("create cert: %w", err)
	}

	if err := os.MkdirAll(dir, 0o700); err != nil {
		return tls.Certificate{}, err
	}
	// The key goes first and the certificate last: the certificate is what phones pin and what marks
	// the pair as complete, so a crash between the two writes leaves only a key, which is replaced on
	// the next start before anything was ever pinned (audit F-92).
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return tls.Certificate{}, err
	}
	// A certificate left over from a key that went missing is not usable with the new key; drop it first so a
	// crash after the key write leaves a lone key, which the next start replaces, not a mismatched pair.
	if err := os.Remove(certPath); err != nil && !os.IsNotExist(err) {
		return tls.Certificate{}, err
	}
	if err := writePEM(keyPath, "EC PRIVATE KEY", keyDER, 0o600); err != nil {
		return tls.Certificate{}, err
	}
	if err := writePEM(certPath, "CERTIFICATE", der, 0o644); err != nil {
		return tls.Certificate{}, err
	}

	return tls.LoadX509KeyPair(certPath, keyPath)
}

// Fingerprint returns the lowercase hex SHA-256 of the leaf certificate's DER bytes.
func Fingerprint(cert tls.Certificate) string {
	sum := sha256.Sum256(cert.Certificate[0])
	return hex.EncodeToString(sum[:])
}

// writePEM writes through a temporary file and renames it into place, so a crash never leaves a
// truncated file behind.
func writePEM(path, blockType string, der []byte, mode os.FileMode) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".tmp*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	fail := func(err error) error {
		tmp.Close()
		os.Remove(name)
		return err
	}
	// A refused chmod must fail: it would leave the private key with a looser mode than asked for.
	// Windows has no POSIX modes, so Chmod there is not meaningful.
	if err := tmp.Chmod(mode); err != nil && runtime.GOOS != "windows" {
		return fail(err)
	}
	if err := pem.Encode(tmp, &pem.Block{Type: blockType, Bytes: der}); err != nil {
		return fail(err)
	}
	if err := tmp.Sync(); err != nil {
		return fail(err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(name)
		return err
	}
	if err := os.Rename(name, path); err != nil {
		os.Remove(name)
		return err
	}
	return nil
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

func localIPs() []net.IP {
	ips := []net.IP{net.IPv4(127, 0, 0, 1), net.IPv6loopback}
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		log.Printf("tls: listing interface addresses failed, certificate covers loopback only: %v", err)
		return ips
	}
	for _, a := range addrs {
		if ipnet, ok := a.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
			ips = append(ips, ipnet.IP)
		}
	}
	return ips
}
