package files

import (
	"errors"
	"fmt"
	"io/fs"

	"github.com/ZaidQamhieh/rfe/agent/internal/fsops"
	"github.com/ZaidQamhieh/rfe/agent/internal/next/api"
	"github.com/ZaidQamhieh/rfe/agent/internal/next/authz"
)

// classifyAccess is classify plus one more rule: an operating-system refusal
// (the agent's own account may not open or change the item) is api.ErrForbidden,
// with the OS error kept behind it for logging.
func classifyAccess(err error) error {
	err = classify(err)
	var oe *opError
	if err != nil && !errors.As(err, &oe) && errors.Is(err, fs.ErrPermission) {
		return &opError{kind: api.ErrForbidden, cause: err}
	}
	return err
}

// MediaInfo is what could be read from a photo or video header (openapi schema
// MediaInfo). A field is zero, and left out of the JSON, when the file does not
// say, so a folder or an unsupported file gives an empty value.
type MediaInfo struct {
	Width      int    `json:"width,omitempty"`
	Height     int    `json:"height,omitempty"`
	TakenAt    string `json:"takenAt,omitempty"` // RFC 3339, from the EXIF capture date
	DurationMs int64  `json:"durationMs,omitempty"`
}

// MediaInfo reads the pixel size, capture date and duration of the photo or
// video at path from its header only. The header parser is fsops.ProbeMedia,
// fed from a file opened through rootfs, so a symlink cannot lead outside the
// allowed folders. A folder or a file of an unknown type gives an empty
// MediaInfo; a missing path is api.ErrPathNotFound and a special file (named
// pipe, socket, device) is api.ErrNotAFile.
func (s *Service) MediaInfo(path string) (*MediaInfo, error) {
	h, err := s.open(path)
	if err != nil {
		return nil, err
	}
	f, err := h.Open()
	if err != nil {
		return nil, classifyAccess(err)
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, classifyAccess(err)
	}
	if info.IsDir() {
		return &MediaInfo{}, nil
	}
	m := fsops.ProbeMedia(f, info.Size())
	return &MediaInfo{Width: m.Width, Height: m.Height, TakenAt: m.TakenAt, DurationMs: m.DurationMs}, nil
}

// Chmod sets the nine permission bits (rwx for owner, group and others) of the
// file or folder at path and returns its new description. A symlink is
// followed inside the allowed folders, never out of them. Setuid, setgid,
// sticky and any other bit above the nine are api.ErrBadRequest, and a
// read-only Service refuses with authz.ErrReadOnly before touching anything.
// The target is opened for reading through rootfs (which refuses a protected
// folder and a special file such as a named pipe, so the call cannot block), so
// an item the agent itself cannot open (mode 000) cannot be changed here.
func (s *Service) Chmod(path string, mode fs.FileMode) (*Entry, error) {
	if mode&^fs.ModePerm != 0 {
		return nil, fmt.Errorf("%w: only the nine permission bits can be set", api.ErrBadRequest)
	}
	h, err := s.open(path)
	if err != nil {
		return nil, err
	}
	if s.set.IsReadOnly() {
		return nil, authz.ErrReadOnly
	}
	f, err := h.Open()
	if err != nil {
		return nil, classifyAccess(err)
	}
	err = f.Chmod(mode)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return nil, classifyAccess(err)
	}
	return s.Meta(h.Path())
}
