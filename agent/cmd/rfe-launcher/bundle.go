package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"

	"github.com/ZaidQamhieh/rfe/agent/internal/bundle"
	"github.com/ZaidQamhieh/rfe/agent/internal/launcher"
)

// sidecarManifest is the file the existing release packaging puts next to the helpers; it is copied unchanged
// so the agent's own helper check keeps working inside a bundle.
const sidecarManifest = "rfe-sidecars.txt"

// bundleMain implements "rfe-launcher bundle -version V [-goos G] [-goarch A] [-pending] <srcdir> <outdir>".
func bundleMain(args []string) error {
	flags := flag.NewFlagSet("bundle", flag.ContinueOnError)
	version := flags.String("version", "", "bundle version (required)")
	goos := flags.String("goos", runtime.GOOS, "target operating system")
	goarch := flags.String("goarch", runtime.GOARCH, "target architecture")
	pending := flags.Bool("pending", false, "stage the new bundle as pending when the root already has a state")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 2 || *version == "" {
		return errors.New("usage: rfe-launcher bundle -version V [-goos G] [-goarch A] [-pending] <srcdir> <outdir>")
	}
	return assemble(flags.Arg(0), flags.Arg(1), *version, *goos, *goarch, *pending)
}

// assemble copies the agent (rfe or rfe.exe, required), the helpers rfe-indexd and rfe-thumbd and the helper
// manifest rfe-sidecars.txt (each when present) from src into out/staging/<version>, writes manifest.json, verifies
// it, then renames the directory to out/versions/<version>. A fresh root also gets a state.json naming this
// version current; an existing root keeps its state unless pending is set.
func assemble(src, out, version, goos, goarch string, pending bool) error {
	if !launcher.ValidVersion(version) {
		return fmt.Errorf("invalid version %q", version)
	}
	store := launcher.NewStore(out)
	final, _ := store.VersionDir(version)
	if _, err := os.Lstat(final); err == nil {
		return fmt.Errorf("%s already exists", final)
	}
	ext := ""
	if goos == "windows" {
		ext = ".exe"
	}
	stage := filepath.Join(store.StagingPath(), version)
	if err := os.RemoveAll(stage); err != nil {
		return err
	}
	if err := os.MkdirAll(stage, 0o755); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(final), 0o755); err != nil {
		return err
	}
	for _, f := range []struct {
		name     string
		required bool
	}{{"rfe" + ext, true}, {"rfe-indexd" + ext, false}, {"rfe-thumbd" + ext, false}, {sidecarManifest, false}} {
		mode := fs.FileMode(0o755)
		if f.name == sidecarManifest {
			mode = 0o644
		}
		err := copyFile(filepath.Join(src, f.name), filepath.Join(stage, f.name), mode)
		if errors.Is(err, fs.ErrNotExist) && !f.required {
			continue
		}
		if err != nil {
			os.RemoveAll(stage)
			return err
		}
	}
	if _, err := bundle.Build(stage, version, goos, goarch); err != nil {
		os.RemoveAll(stage)
		return err
	}
	if _, err := bundle.Verify(stage); err != nil {
		os.RemoveAll(stage)
		return err
	}
	if err := os.Rename(stage, final); err != nil {
		return err
	}
	st, err := store.Read()
	if err != nil {
		return err
	}
	switch {
	case st.Current == "":
		_, err = store.Apply(launcher.EvSetCurrent, version)
	case pending:
		_, err = store.Apply(launcher.EvSetPending, version)
	}
	if err != nil {
		return err
	}
	fmt.Println(final)
	return nil
}

// copyFile copies a regular file (not a symlink) to dst, which must not exist, and syncs it.
func copyFile(src, dst string, mode fs.FileMode) error {
	if st, err := os.Lstat(src); err != nil {
		return err
	} else if !st.Mode().IsRegular() {
		return fmt.Errorf("%s is not a regular file", src)
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
	if err != nil {
		return err
	}
	_, err = io.Copy(out, in)
	if err == nil {
		err = out.Sync()
	}
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	return err
}
