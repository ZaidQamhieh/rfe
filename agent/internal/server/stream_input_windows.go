//go:build windows

package server

import (
	"os/exec"

	"github.com/ZaidQamhieh/rfe/agent/internal/fsops"
)

// hlsInput names the jail-resolved path: Windows has no descriptor passing.
type hlsInput struct{ path string }

func openHLSInput(ops *fsops.Ops, path string) (hlsInput, error) {
	f, err := ops.Open(path) // jail and type check
	if err != nil {
		return hlsInput{}, err
	}
	f.Close()
	p, err := ops.Resolve(path)
	return hlsInput{p}, err
}

func (i hlsInput) arg() string      { return i.path }
func (i hlsInput) attach(*exec.Cmd) {}
func (i hlsInput) close()           {}
