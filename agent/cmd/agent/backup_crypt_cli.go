package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/term"

	"github.com/ZaidQamhieh/rfe/agent/internal/backup"
)

// errNoPassphrase is returned when no passphrase was given by the environment, a terminal or stdin.
var errNoPassphrase = errors.New("no passphrase: set " + envBackupPassphrase + ", or run on a terminal, or pipe it on stdin")

// readBackupPassphrase gets the passphrase for an encrypted backup copy, never from a command-line argument (that
// would show in the process list). It uses RFE_BACKUP_PASSPHRASE when set; otherwise a hidden prompt on a terminal
// (typed twice when confirm is set); otherwise the first line of stdin.
func readBackupPassphrase(confirm bool, stdin *os.File, stderr io.Writer) ([]byte, error) {
	if p := os.Getenv(envBackupPassphrase); p != "" {
		return []byte(p), nil
	}
	if term.IsTerminal(int(stdin.Fd())) {
		fmt.Fprint(stderr, "Backup passphrase: ")
		first, err := term.ReadPassword(int(stdin.Fd()))
		fmt.Fprintln(stderr)
		if err != nil {
			return nil, err
		}
		if len(first) == 0 {
			return nil, errNoPassphrase
		}
		if confirm {
			fmt.Fprint(stderr, "Repeat passphrase: ")
			again, err := term.ReadPassword(int(stdin.Fd()))
			fmt.Fprintln(stderr)
			if err != nil {
				return nil, err
			}
			if string(again) != string(first) {
				return nil, errors.New("the two passphrases differ")
			}
		}
		return first, nil
	}
	line, err := bufio.NewReader(stdin).ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, err
	}
	line = strings.TrimRight(line, "\r\n")
	if line == "" {
		return nil, errNoPassphrase
	}
	return []byte(line), nil
}

// cmdBackupDecrypt implements "rfe backup decrypt <file> <out>": it turns an encrypted off-device copy (.enc) back
// into the plain archive, which "rfe backup verify" and "rfe restore" accept. It refuses to overwrite <out>.
func cmdBackupDecrypt(args []string) error {
	fs := newAdminFlags("backup decrypt", "backup decrypt <file.enc> <out>   (passphrase from "+envBackupPassphrase+", a prompt, or stdin)")
	pos, err := fs.parse(args, 2, 2)
	if err != nil {
		return err
	}
	pass, err := readBackupPassphrase(false, os.Stdin, os.Stderr)
	if err != nil {
		return err
	}
	if err := backup.DecryptFile(pos[0], pos[1], pass); err != nil {
		return fmt.Errorf("decrypt %s: %w", filepath.Base(pos[0]), err)
	}
	fmt.Printf("Decrypted %s to %s\n", filepath.Base(pos[0]), pos[1])
	return nil
}

// newestArchive returns the most recently written backup archive in dir, whatever its label.
func newestArchive(dir string) (string, error) {
	matches, err := filepath.Glob(filepath.Join(dir, "rfe-backup-*.tar.gz"))
	if err != nil {
		return "", err
	}
	best, bestTime := "", time.Time{}
	for _, m := range matches {
		if st, err := os.Stat(m); err == nil && (best == "" || st.ModTime().After(bestTime)) {
			best, bestTime = m, st.ModTime()
		}
	}
	if best == "" {
		return "", fmt.Errorf("no backup archive in %s (run `rfe backup` first)", dir)
	}
	return best, nil
}

// cmdBackupOffsite implements "rfe backup offsite -dir D [-archive A]": it verifies the newest archive (or A),
// encrypts it with a passphrase and writes <name>.enc into D, then records the result in the status file.
// The archive itself never leaves the data dir unencrypted.
func cmdBackupOffsite(args []string) error {
	fs := newAdminFlags("backup offsite", "backup offsite -dir <folder> [-archive <file>] [-data <dir>]   (passphrase from "+envBackupPassphrase+", a prompt, or stdin)")
	data := fs.String("data", "", "agent data dir")
	dest := fs.String("dir", "", "folder (for example a USB drive or a synced folder) to write the encrypted copy to")
	archive := fs.String("archive", "", "archive to copy (default: the newest in <data dir>/backups)")
	if _, err := fs.parse(args, 0, 0); err != nil {
		return err
	}
	if *dest == "" {
		return errors.New("backup offsite: -dir is required")
	}
	dir := adminDataDir(*data)
	src := *archive
	if src == "" {
		var err error
		if src, err = newestArchive(filepath.Join(dir, backup.SubdirName)); err != nil {
			return err
		}
	}
	pass, err := readBackupPassphrase(true, os.Stdin, os.Stderr)
	if err != nil {
		return err
	}
	_, err = backup.Verify(src)
	var copied string
	if err == nil {
		copied, err = backup.CopyOffsite(src, *dest, pass)
	}
	if rerr := backup.RecordOffsite(dir, time.Now(), err, *dest); rerr != nil {
		fmt.Fprintf(os.Stderr, "warning: could not record the status: %v\n", rerr)
	}
	if err != nil {
		return fmt.Errorf("backup offsite: %w", err)
	}
	fmt.Printf("Encrypted copy written: %s\n", copied)
	fmt.Printf("Keep the passphrase safe: without it the copy cannot be read, and it is not stored anywhere.\n")
	return nil
}
