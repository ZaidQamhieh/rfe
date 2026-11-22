package files

import (
	"errors"
	"io"
	"io/fs"
	"mime"
	"net/http"
	"path/filepath"
	"time"

	"github.com/ZaidQamhieh/rfe/agent/internal/next/api"
	"github.com/ZaidQamhieh/rfe/agent/internal/next/rootfs"
)

// defaultMime is the type of a file whose extension and content tell nothing.
const defaultMime = "application/octet-stream"

// sniffBytes is how much of a file without a known extension is read to guess
// its type.
const sniffBytes = 512

// Entry is the JSON form of one file or folder (openapi schema Entry).
type Entry struct {
	Name          string    `json:"name"`
	Path          string    `json:"path"`
	IsDir         bool      `json:"isDir"`
	Size          int64     `json:"size"`
	MimeType      string    `json:"mimeType,omitempty"`
	Mode          string    `json:"mode"`
	Modified      time.Time `json:"modified"`
	Created       time.Time `json:"created"`
	IsSymlink     bool      `json:"isSymlink"`
	SymlinkTarget string    `json:"symlinkTarget,omitempty"`
	// ChildCount is the number of items directly inside a folder, capped at
	// MaxChildCount (a capped count means "at least"). Only List fills it, and
	// only for the first maxCountedDirs folders of a page.
	ChildCount *int `json:"childCount,omitempty"`
	// Snippet is the first matching line of a content search hit; nil elsewhere.
	Snippet *Snippet `json:"snippet,omitempty"`
}

// Snippet is a trimmed line of a file that matched a content search (see api.FileSnippet).
type Snippet = api.FileSnippet

// Listing is one page of a folder listing (openapi schema Listing).
type Listing struct {
	Path       string  `json:"path"`
	Entries    []Entry `json:"entries"`
	NextCursor *string `json:"nextCursor"`
}

// entryFor describes the entry at h from info (taken without following a final
// symlink). A symlink reports the type of its target, so a link to a folder
// stays navigable; a broken link keeps its own type, and a link whose target or
// text cannot be read simply has no target or no link text.
func entryFor(h *rootfs.Handle, info fs.FileInfo) Entry {
	isSymlink := info.Mode()&fs.ModeSymlink != 0
	isDir := info.IsDir()
	if isSymlink {
		if target, err := h.Stat(); err == nil {
			isDir = target.IsDir()
		}
	}
	e := Entry{
		Name:      filepath.Base(h.Path()),
		Path:      h.Path(),
		IsDir:     isDir,
		Size:      info.Size(),
		Mode:      info.Mode().String(),
		Modified:  info.ModTime(),
		Created:   birthTime(info),
		IsSymlink: isSymlink,
	}
	if !isDir {
		e.MimeType = mimeFor(h, info)
	}
	if isSymlink {
		if target, err := h.Readlink(); err == nil {
			e.SymlinkTarget = target
		}
	}
	return e
}

// mimeFor guesses a file's type from its extension, then from its first bytes
// (only through rootfs, so a link cannot lead outside the allowed folders), and
// falls back to a generic binary type. A file that cannot be opened or read
// gets the generic type.
func mimeFor(h *rootfs.Handle, info fs.FileInfo) string {
	if ext := filepath.Ext(h.Path()); ext != "" {
		if m := mime.TypeByExtension(ext); m != "" {
			return m
		}
	}
	if info.Size() > 0 && (info.Mode().IsRegular() || info.Mode()&fs.ModeSymlink != 0) {
		if f, err := h.Open(); err == nil {
			defer f.Close()
			buf := make([]byte, sniffBytes)
			// A short read or EOF is normal here; only n matters.
			if n, _ := f.Read(buf); n > 0 {
				return http.DetectContentType(buf[:n])
			}
		}
	}
	return defaultMime
}

// countChildren counts the items directly inside the folder at h, stopping
// after MaxChildCount+1 names. It returns nil when the folder cannot be read.
// The count may include a protected entry (it is a count, not a listing).
func countChildren(h *rootfs.Handle) *int {
	f, err := h.Open()
	if err != nil {
		return nil
	}
	defer f.Close()
	names, err := f.Readdirnames(MaxChildCount + 1)
	if err != nil && !errors.Is(err, io.EOF) {
		return nil
	}
	n := min(len(names), MaxChildCount)
	return &n
}
