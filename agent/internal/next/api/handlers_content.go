package api

import (
	"encoding/json"
	"errors"
	"io"
	"math"
	"mime"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Fixed words of the content and archive routes.
const (
	dispositionAttach = "attachment"

	headerDisposition = "Content-Disposition"
	headerCSP         = "Content-Security-Policy"

	// downloadCSP sandboxes a downloaded file that a browser might render:
	// nothing in it may run, load or embed anything.
	downloadCSP = "sandbox; default-src 'none'; base-uri 'none'; form-action 'none'; frame-ancestors 'none'"

	msgFileServiceNoRead = "reading file content is not available"
)

// ArchiveEntry is one entry of an archive listing (openapi schema ArchiveEntry).
// Modified is the zero time (or the Unix epoch, which is what a tar header with
// no mtime reads back as) when the archive records none; the JSON then leaves
// "modified" out instead of sending a made-up date.
type ArchiveEntry struct {
	Path     string    `json:"path"`
	Size     int64     `json:"size"`
	Modified time.Time `json:"modified"`
	IsDir    bool      `json:"isDir"`
}

// MarshalJSON writes the entry with the openapi field names, leaving out
// "modified" when the entry has no recorded time.
func (e ArchiveEntry) MarshalJSON() ([]byte, error) {
	out := struct {
		Path     string     `json:"path"`
		Size     int64      `json:"size"`
		Modified *time.Time `json:"modified,omitempty"`
		IsDir    bool       `json:"isDir"`
	}{Path: e.Path, Size: e.Size, IsDir: e.IsDir}
	if !e.Modified.IsZero() && e.Modified.Unix() != 0 {
		out.Modified = &e.Modified
	}
	return json.Marshal(out)
}

// ContentFile is a regular file opened for reading. Content supports Seek, so a
// Range request is answered from it. The holder must call Close exactly once.
type ContentFile struct {
	Name    string
	ModTime time.Time
	Size    int64
	Content io.ReadSeeker
	Close   func() error
}

// ContentService is the read side of the file service: what GET /content and
// GET /fs/archive need beyond FileService. It is a separate interface so the
// provider's service may add it without changing FileService; a service that
// lacks it answers 501. Its errors follow the rules of FileService.
type ContentService interface {
	OpenContent(path string) (*ContentFile, error)
	// ArchiveEntries lists the first limit entries of an archive; a limit of 0
	// or less means the default, and one above the maximum is lowered to it.
	ArchiveEntries(path string, limit int) ([]ArchiveEntry, error)
}

// registerContentHandlers adds the handlers of GET /fs/archive and GET /content.
func registerContentHandlers(s *Server, m map[string]handlerFunc) {
	s.addFileRoute(m, routeKey(get, "/fs/archive"), s.fsArchive)
	s.addFileRoute(m, routeKey(get, "/content"), s.contentGet)
}

// contentServiceFor returns the read side of the caller's file service.
func (s *Server) contentServiceFor(r *http.Request) (ContentService, error) {
	svc, err := s.filesFor(r)
	if err != nil {
		return nil, err
	}
	cs, ok := svc.(ContentService)
	if !ok {
		return nil, &apiError{kind: ErrNotImplemented, msg: msgFileServiceNoRead}
	}
	return cs, nil
}

// contentGet answers GET /content: it streams the file at path, honouring the
// Range header (206, or 416 when the range lies outside the file). The file is
// sent as an untrusted download (forced attachment, no type sniffing, sandbox
// policy, never cached) and is never read into memory as a whole.
func (s *Server) contentGet(w http.ResponseWriter, r *http.Request) error {
	cs, err := s.contentServiceFor(r)
	if err != nil {
		return err
	}
	path, err := queryPathOf(r)
	if err != nil {
		return err
	}
	f, err := cs.OpenContent(path)
	if err != nil {
		return err
	}
	defer func() {
		if cerr := f.Close(); cerr != nil {
			s.logf("api: content: close file: %v", cerr)
		}
	}()
	setDownloadHeaders(w.Header(), f.Name)
	http.ServeContent(w, r, f.Name, f.ModTime, paceReadSeekerShared(f.Content, s.downloadRate(), r.Context(), &s.downPacer))
	return nil
}

// setDownloadHeaders marks a response as a download of an untrusted file.
func setDownloadHeaders(h http.Header, name string) {
	disposition := mime.FormatMediaType(dispositionAttach, map[string]string{"filename": filepath.Base(name)})
	if disposition == "" {
		disposition = dispositionAttach
	}
	h.Set(headerDisposition, disposition)
	h.Set(headerNoSniff, noSniffValue)
	h.Set(headerCSP, downloadCSP)
	h.Set(headerCacheControl, cacheNoStore)
}

// archiveLimitOf reads the optional limit query value. Empty, not a number, zero
// or negative gives 0 (the default); a number too large for an int gives the
// largest int, which the service lowers to its maximum.
func archiveLimitOf(raw string) int {
	if raw == "" {
		return 0
	}
	n, err := strconv.Atoi(raw)
	switch {
	case err == nil && n > 0:
		return n
	case errors.Is(err, strconv.ErrRange) && !strings.HasPrefix(raw, "-"):
		return math.MaxInt
	}
	return 0
}

// fsArchive answers GET /fs/archive: the entries of a zip, tar or tar.gz file,
// read from its headers without extracting anything.
func (s *Server) fsArchive(w http.ResponseWriter, r *http.Request) error {
	cs, err := s.contentServiceFor(r)
	if err != nil {
		return err
	}
	path, err := queryPathOf(r)
	if err != nil {
		return err
	}
	entries, err := cs.ArchiveEntries(path, archiveLimitOf(r.URL.Query().Get(queryLimit)))
	if err != nil {
		return err
	}
	if entries == nil {
		entries = []ArchiveEntry{}
	}
	writeJSON(w, http.StatusOK, struct {
		Entries []ArchiveEntry `json:"entries"`
	}{entries})
	return nil
}
