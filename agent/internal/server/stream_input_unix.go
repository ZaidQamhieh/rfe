//go:build !windows

package server

import (
	"os"
	"os/exec"

	"github.com/ZaidQamhieh/rfe/agent/internal/fsops"
)

// hlsInput hands ffmpeg the already-opened (jail-checked) file as descriptor 3,
// so a symlink swapped after the check cannot redirect the read.
type hlsInput struct{ f *os.File }

func openHLSInput(ops *fsops.Ops, path string) (hlsInput, error) {
	f, err := ops.Open(path)
	return hlsInput{f}, err
}

func (i hlsInput) arg() string        { return "/dev/fd/3" }
func (i hlsInput) attach(c *exec.Cmd) { c.ExtraFiles = []*os.File{i.f} }
func (i hlsInput) close()             { _ = i.f.Close() }
