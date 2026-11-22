// Command rfe-sign writes the signed release manifest the host updater checks (internal/hostupdate): a JSON
// manifest of the release's bundles and a detached Ed25519 signature over its exact bytes.
//
//	rfe-sign -key primary.key -version 1.2.0 -base-url https://example/releases/1.2.0 -out dir \
//	    linux/amd64=rfe-linux-amd64.tar.gz windows/arm64=rfe-windows-arm64.zip ...
//
// The key file holds the base64 of a 32-byte Ed25519 seed (as written by "rfe-sign -genkey <file>"). Each
// bundle argument is goos/goarch=path; the URL is base-url plus the file name. Run it on the owner's machine:
// the private key must never reach CI.
package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ZaidQamhieh/rfe/agent/internal/hostupdate"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "rfe-sign:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	fs := flag.NewFlagSet("rfe-sign", flag.ContinueOnError)
	keyFile := fs.String("key", "", "private key file (base64 of the 32-byte seed)")
	genkey := fs.String("genkey", "", "write a new key to this file (mode 0600), print its public key, and exit")
	version := fs.String("version", "", "release version, e.g. 1.2.0")
	baseURL := fs.String("base-url", "", "URL directory the bundle files are published under")
	out := fs.String("out", ".", "directory to write manifest.json and manifest.json.sig to")
	minInstalled := fs.String("min-installed", "", "oldest installed version allowed to update straight to this release")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *genkey != "" {
		return generate(*genkey)
	}
	if *keyFile == "" || *version == "" || *baseURL == "" || fs.NArg() == 0 {
		return fmt.Errorf("usage: rfe-sign -key <file> -version <v> -base-url <url> [-out dir] goos/goarch=path ...")
	}
	priv, err := loadKey(*keyFile)
	if err != nil {
		return err
	}
	rel := hostupdate.Release{Version: *version, Released: time.Now().UTC().Truncate(time.Second), MinInstalled: *minInstalled}
	for _, arg := range fs.Args() {
		platform, path, ok := strings.Cut(arg, "=")
		goos, goarch, ok2 := strings.Cut(platform, "/")
		if !ok || !ok2 {
			return fmt.Errorf("bundle %q: want goos/goarch=path", arg)
		}
		size, sum, err := hashFile(path)
		if err != nil {
			return err
		}
		rel.Bundles = append(rel.Bundles, hostupdate.Bundle{
			GOOS: goos, GOARCH: goarch, URL: strings.TrimRight(*baseURL, "/") + "/" + filepath.Base(path), Size: size, SHA256: sum,
		})
	}
	data, err := json.MarshalIndent(rel, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if _, err := hostupdate.ParseRelease(data); err != nil { // refuse to sign what the agent would refuse to read
		return err
	}
	sig := base64.StdEncoding.EncodeToString(ed25519.Sign(priv, data)) + "\n"
	if err := os.MkdirAll(*out, 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(*out, "manifest.json"), data, 0o644); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(*out, "manifest.json.sig"), []byte(sig), 0o644)
}

func loadKey(path string) (ed25519.PrivateKey, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	seed, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(raw)))
	if err != nil || len(seed) != ed25519.SeedSize {
		return nil, fmt.Errorf("%s: not the base64 of a %d-byte seed", path, ed25519.SeedSize)
	}
	return ed25519.NewKeyFromSeed(seed), nil
}

func generate(path string) error {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600) // never overwrites a key
	if err != nil {
		return err
	}
	defer f.Close()
	if _, err := fmt.Fprintln(f, base64.StdEncoding.EncodeToString(priv.Seed())); err != nil {
		return err
	}
	fmt.Println(base64.StdEncoding.EncodeToString(pub))
	return nil
}

func hashFile(path string) (int64, string, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, "", err
	}
	defer f.Close()
	h := sha256.New()
	n, err := io.Copy(h, f)
	return n, hex.EncodeToString(h.Sum(nil)), err
}
