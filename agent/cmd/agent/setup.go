package main

import (
	"bufio"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/term"

	"github.com/ZaidQamhieh/rfe/agent/internal/security"
	"github.com/ZaidQamhieh/rfe/agent/internal/settings"
	"github.com/ZaidQamhieh/rfe/agent/internal/store"
)

// cmdSetup provisions a new per-user agent with a conservative folder jail,
// installs its login service, and mints the first pairing code. It refuses to
// reuse an existing database so setup cannot silently replace a live policy.
func cmdSetup(args []string) error {
	fs := flag.NewFlagSet("setup", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	rootFlag := fs.String("root", "", "folder the agent may expose (default: ~/RFE Files)")
	noInstall := fs.Bool("no-install", false, "save the initial folder policy without installing or starting a service")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("usage: rfe setup [-root <path>] [-no-install]")
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("resolve signed-in user's home folder: %w", err)
	}
	defaultRoot := defaultSetupRoot(home)
	rootInput := *rootFlag
	if strings.TrimSpace(rootInput) == "" {
		if !term.IsTerminal(int(os.Stdin.Fd())) {
			return fmt.Errorf("setup needs a folder path in non-interactive use; pass -root <path> or run it in a terminal to use the default %q", defaultRoot)
		}
		rootInput, err = promptSetupRoot(os.Stdin, os.Stdout, defaultRoot)
		if err != nil {
			return err
		}
	}
	rootCandidate, err := setupRootCandidate(rootInput, home)
	if err != nil {
		return err
	}

	dataDir := defaultDataDir()
	standardDataDir := filepath.Join(home, ".rfe")
	if filepath.Clean(dataDir) != filepath.Clean(standardDataDir) {
		return fmt.Errorf("the automatic service setup uses the standard data folder %q; clear RFE_DATA_DIR or use the manual daemon/service commands", standardDataDir)
	}
	dbPath := filepath.Join(dataDir, "agent.db")
	if _, err := os.Stat(dbPath); err == nil {
		return fmt.Errorf("agent data already exists at %q; setup only initializes new installs and did not change it", dataDir)
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("check agent data at %q: %w", dataDir, err)
	}
	if filepath.Clean(rootCandidate) == filepath.Clean(defaultRoot) {
		if err := ensureDefaultShareRoot(rootCandidate); err != nil {
			return fmt.Errorf("prepare default RFE Files folder: %w", err)
		}
	}
	root, err := normalizeSetupRoot(rootCandidate, home)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return fmt.Errorf("create agent data folder: %w", err)
	}

	db, err := store.Open(dataDir)
	if err != nil {
		return fmt.Errorf("initialize agent database: %w", err)
	}
	configured, err := settings.Load(db, false, []string{root}, hostName())
	if err != nil {
		_ = db.Close()
		return fmt.Errorf("initialize agent settings: %w", err)
	}
	if err := configured.SetRoots([]string{root}); err != nil {
		_ = db.Close()
		return fmt.Errorf("save initial folder access: %w", err)
	}
	if err := db.Close(); err != nil {
		return fmt.Errorf("close initial agent database: %w", err)
	}
	cert, err := security.LoadOrCreateCert(dataDir)
	if err != nil {
		return fmt.Errorf("create host identity: %w", err)
	}

	fmt.Printf("Initial folder access: %s\n", root)
	fmt.Printf("Host identity fingerprint: %s\n", security.Fingerprint(cert))
	if *noInstall {
		fmt.Println("Setup saved. Run `rfe install` to start the user service, then `rfe pair` to create a pairing QR.")
		return nil
	}
	if err := cmdInstall(nil); err != nil {
		return fmt.Errorf("folder policy is saved, but service installation failed: %w (fix the cause, then run `rfe install`; setup cannot be run again)", err)
	}
	return cmdPair(nil)
}

func promptSetupRoot(input io.Reader, output io.Writer, defaultRoot string) (string, error) {
	fmt.Fprintf(output, "Folder the agent may expose [%s]: ", defaultRoot)
	line, err := bufio.NewReader(input).ReadString('\n')
	if err != nil && err != io.EOF {
		return "", fmt.Errorf("read folder path: %w", err)
	}
	if strings.TrimSpace(line) == "" {
		return defaultRoot, nil
	}
	return strings.TrimSpace(line), nil
}

func normalizeSetupRoot(value, home string) (string, error) {
	absolute, err := setupRootCandidate(value, home)
	if err != nil {
		return "", err
	}
	canonical, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return "", fmt.Errorf("folder %q does not exist or cannot be resolved: %w", value, err)
	}
	info, err := os.Stat(canonical)
	if err != nil {
		return "", fmt.Errorf("inspect folder %q: %w", canonical, err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("folder access root %q is not a directory", canonical)
	}
	return filepath.Clean(canonical), nil
}

func setupRootCandidate(value, home string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		value = defaultSetupRoot(home)
	} else if value == "~" {
		value = home
	} else if strings.HasPrefix(value, "~"+string(filepath.Separator)) {
		value = filepath.Join(home, value[2:])
	}
	absolute, err := filepath.Abs(value)
	if err != nil {
		return "", fmt.Errorf("resolve folder path %q: %w", value, err)
	}
	return filepath.Clean(absolute), nil
}

func defaultSetupRoot(home string) string {
	return filepath.Join(home, "RFE Files")
}

func ensureDefaultShareRoot(root string) error {
	info, err := os.Lstat(root)
	if os.IsNotExist(err) {
		if err := os.Mkdir(root, 0o700); err != nil && !os.IsExist(err) {
			return err
		}
		info, err = os.Lstat(root)
	}
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("%q is a symbolic link; choose another folder", root)
	}
	if !info.IsDir() {
		return fmt.Errorf("%q is not a directory; choose another folder", root)
	}
	return nil
}
