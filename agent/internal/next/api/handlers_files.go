package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/ZaidQamhieh/rfe/agent/internal/next/authz"
)

// Limits and fixed words of the file routes.
const (
	// maxContentBytes caps the body of PUT /content (5 MiB, openapi /content).
	maxContentBytes int64 = 5 << 20
	// maxChecksumPaths caps one POST /fs/checksums request (openapi /fs/checksums).
	maxChecksumPaths = 1000
	// checksumWorkers is the size of the pool that hashes a /fs/checksums batch.
	checksumWorkers = 4

	queryPath         = "path"
	queryCursor       = "cursor"
	queryLimit        = "limit"
	queryAlgo         = "algo"
	queryPermanent    = "permanent"
	queryBaseModified = "baseModified"
	permanentTrue     = "true"

	algoSHA256 = "sha256"
	algoSHA1   = "sha1"
	algoMD5    = "md5"
)

// Fixed client-safe texts of the file routes. None carries request data.
const (
	msgPathRequired     = "path is required"
	msgSrcDstRequired   = "src and dst are required"
	msgSourcesRequired  = "sources and destDir are required"
	msgCompressRequired = "sources and dest are required"
	msgExtractRequired  = "archive and destDir are required"
	msgNoPaths          = "no paths specified"
	msgPathsRequired    = "paths are required"
	msgTooManyPaths     = "too many paths in one request"
	msgBadAlgo          = "unsupported algorithm (use sha256, sha1 or md5)"
	msgBadBaseModified  = "baseModified must be an RFC 3339 date-time"
	msgBodyUnreadable   = "request body could not be read"
)

// FileEntry is the JSON form of one file or folder (openapi schema Entry). Its
// fields match files.Entry one for one so an adapter can convert directly.
type FileEntry struct {
	Name          string       `json:"name"`
	Path          string       `json:"path"`
	IsDir         bool         `json:"isDir"`
	Size          int64        `json:"size"`
	MimeType      string       `json:"mimeType,omitempty"`
	Mode          string       `json:"mode"`
	Modified      time.Time    `json:"modified"`
	Created       time.Time    `json:"created"`
	IsSymlink     bool         `json:"isSymlink"`
	SymlinkTarget string       `json:"symlinkTarget,omitempty"`
	ChildCount    *int         `json:"childCount,omitempty"`
	Snippet       *FileSnippet `json:"snippet,omitempty"`
}

// FileSnippet is a trimmed line of a file that matched a content search. Start
// and Length locate the match inside Text in UTF-16 code units, so a client can
// index a JavaScript string with them directly.
type FileSnippet struct {
	Text   string `json:"text"`
	Start  int    `json:"start"`
	Length int    `json:"length"`
}

// FileListing is one page of a folder listing (openapi schema Listing).
type FileListing struct {
	Path       string      `json:"path"`
	Entries    []FileEntry `json:"entries"`
	NextCursor *string     `json:"nextCursor"`
}

// FileDrive is one mounted volume (openapi schema Drive).
type FileDrive struct {
	Path       string `json:"path"`
	Label      string `json:"label"`
	TotalBytes int64  `json:"totalBytes"`
	FreeBytes  int64  `json:"freeBytes"`
	IsOS       bool   `json:"isOS"`
}

// FileService is the part of the file service the HTTP layer uses, already
// bound to one caller's folders (jail, read-only). Every error it returns
// carries a domain error of this package and a text free of host paths. The
// types above mirror those of package files (which imports this package and so
// cannot be named here); a thin adapter converts between them.
type FileService interface {
	List(path, cursor string, limit int) (*FileListing, error)
	Meta(path string) (*FileEntry, error)
	Checksum(path, algo string) (string, error)
	CreateFolder(path string) (*FileEntry, error)
	CreateFile(path string) (*FileEntry, error)
	WriteContent(path string, data []byte, baseModified *time.Time) (*FileEntry, error)
	Rename(src, dst string) (*FileEntry, error)
	Copy(sources []string, destDir string, duplicate, overwrite bool) []ItemResult
	Move(sources []string, destDir string, duplicate, overwrite bool) []ItemResult
	Delete(paths []string) []ItemResult
	Trash(paths []string) []ItemResult
	Compress(sources []string, dest string) (*FileEntry, error)
	Extract(archive, destDir string, overwrite bool) (*FileEntry, error)
	Drives() ([]FileDrive, error)
	ListTrash() ([]TrashItem, error)
	Restore(ids []string) []ItemResult
	EmptyTrash(ids []string) ([]ItemResult, error)
	ValidTrashID(id string) bool
}

// FilesProvider gives the file service for one caller: its jail and read-only
// state decide which folders the returned service can reach. An error from it
// is an internal error; its text is only logged.
type FilesProvider interface {
	For(p authz.Principal, pol authz.Policy) (FileService, error)
}

// batchResponse is the answer of every batch route (openapi BatchResult).
type batchResponse struct {
	Results []ItemResult `json:"results"`
}

// filesFor returns the file service for the request's caller. A server built
// without a provider answers 501 on every file route.
func (s *Server) filesFor(r *http.Request) (FileService, error) {
	if s.cfg.Files == nil {
		return nil, notYet
	}
	c, err := callOf(r)
	if err != nil {
		return nil, err
	}
	return s.cfg.Files.For(c.principal, c.policy)
}

// addFileRoute registers a file route behind a check that a file service is
// configured, so a server without one answers 501 before reading the request.
func (s *Server) addFileRoute(reg map[string]handlerFunc, key string, h handlerFunc) {
	reg[key] = func(w http.ResponseWriter, r *http.Request) error {
		if s.cfg.Files == nil {
			return notYet
		}
		return h(w, r)
	}
}

// registerFilesHandlers adds the file, trash, drive and content routes to the
// handler registry.
func registerFilesHandlers(s *Server, reg map[string]handlerFunc) {
	add := func(key string, h handlerFunc) { s.addFileRoute(reg, key, h) }
	add(routeKey(get, "/fs"), s.fsList)
	add(routeKey(del, "/fs"), s.fsDelete)
	add(routeKey(post, "/fs/folder"), s.fsCreate(FileService.CreateFolder))
	add(routeKey(post, "/fs/file"), s.fsCreate(FileService.CreateFile))
	add(routeKey(patch, "/fs/rename"), s.fsRename)
	add(routeKey(post, "/fs/copy"), s.fsCopy)
	add(routeKey(post, "/fs/move"), s.fsMove)
	add(routeKey(post, "/fs/compress"), s.fsCompress)
	add(routeKey(post, "/fs/extract"), s.fsExtract)
	add(routeKey(get, "/fs/meta"), s.fsMeta)
	add(routeKey(get, "/fs/checksum"), s.fsChecksum)
	add(routeKey(post, "/fs/checksums"), s.fsChecksums)
	add(routeKey(get, "/system/drives"), s.drives)
	add(routeKey(put, "/content"), s.contentPut)
	registerTrashHandlers(s, reg)
}

// queryPathOf returns the required "path" query value.
func queryPathOf(r *http.Request) (string, error) {
	p := r.URL.Query().Get(queryPath)
	if p == "" {
		return "", badRequest(msgPathRequired)
	}
	return p, nil
}

// readOptionalJSON decodes a body that may be absent: present is false for a
// body with no content at all, and a body that is present but is not one JSON
// value of the right shape is a bad request. A too-large body keeps its own
// kind.
func readOptionalJSON(r *http.Request, dst any) (present bool, err error) {
	if r.Body == nil || r.Body == http.NoBody {
		return false, nil
	}
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		return false, bodyReadError(err)
	}
	if len(bytes.TrimSpace(raw)) == 0 {
		return false, nil
	}
	if err := json.Unmarshal(raw, dst); err != nil { // also rejects data after the value
		return true, badRequest(msgInvalidBody)
	}
	return true, nil
}

// bodyReadError classifies a failure to read a request body: over the size
// limit is 413, anything else a 400 with a fixed text.
func bodyReadError(err error) error {
	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) {
		return &apiError{kind: ErrPayloadTooLarge, msg: msgBodyTooLarge}
	}
	return badRequest(msgBodyUnreadable)
}

// fsList answers GET /fs: one page of a folder listing.
func (s *Server) fsList(w http.ResponseWriter, r *http.Request) error {
	path, err := queryPathOf(r)
	if err != nil {
		return err
	}
	svc, err := s.filesFor(r)
	if err != nil {
		return err
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get(queryLimit)) // a bad or missing limit means the service default
	listing, err := svc.List(path, r.URL.Query().Get(queryCursor), limit)
	if err != nil {
		return err
	}
	if listing.Entries == nil {
		listing.Entries = []FileEntry{}
	}
	writeJSON(w, http.StatusOK, listing)
	return nil
}

// fsDelete answers DELETE /fs: the paths of the "path" query value and of the
// optional body are moved to the trash, or removed for good with
// permanent=true, each with its own result.
func (s *Server) fsDelete(w http.ResponseWriter, r *http.Request) error {
	var paths []string
	if p := r.URL.Query().Get(queryPath); p != "" {
		paths = append(paths, p)
	}
	var body struct {
		Paths []string `json:"paths"`
	}
	if present, err := readOptionalJSON(r, &body); err != nil {
		return err
	} else if present {
		paths = append(paths, body.Paths...)
	}
	if len(paths) == 0 {
		return badRequest(msgNoPaths)
	}
	svc, err := s.filesFor(r)
	if err != nil {
		return err
	}
	var results []ItemResult
	if r.URL.Query().Get(queryPermanent) == permanentTrue {
		results = svc.Delete(paths)
	} else {
		results = svc.Trash(paths)
	}
	writeJSON(w, http.StatusOK, batchResponse{Results: results})
	return nil
}

// fsCreate builds the handler of POST /fs/folder and POST /fs/file: create
// calls the matching service method with the requested path and the entry is
// answered with 201.
func (s *Server) fsCreate(create func(FileService, string) (*FileEntry, error)) handlerFunc {
	return func(w http.ResponseWriter, r *http.Request) error {
		var req struct {
			Path string `json:"path"`
		}
		if err := decodeJSON(r, &req); err != nil {
			return err
		}
		if req.Path == "" {
			return badRequest(msgPathRequired)
		}
		svc, err := s.filesFor(r)
		if err != nil {
			return err
		}
		e, err := create(svc, req.Path)
		if err != nil {
			return err
		}
		writeJSON(w, http.StatusCreated, e)
		return nil
	}
}

// fsRename answers PATCH /fs/rename with the renamed entry.
func (s *Server) fsRename(w http.ResponseWriter, r *http.Request) error {
	var req struct {
		Src string `json:"src"`
		Dst string `json:"dst"`
	}
	if err := decodeJSON(r, &req); err != nil {
		return err
	}
	if req.Src == "" || req.Dst == "" {
		return badRequest(msgSrcDstRequired)
	}
	svc, err := s.filesFor(r)
	if err != nil {
		return err
	}
	e, err := svc.Rename(req.Src, req.Dst)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, e)
	return nil
}

// transferRequest is the body of POST /fs/copy and POST /fs/move.
type transferRequest struct {
	Sources   []string `json:"sources"`
	DestDir   string   `json:"destDir"`
	Duplicate bool     `json:"duplicate"`
	Overwrite bool     `json:"overwrite"`
}

// fsCopy answers POST /fs/copy with one result per source. Replacing the
// destination (overwrite without duplicate) also needs the delete grant, which
// the route table cannot express, so it is decided here.
func (s *Server) fsCopy(w http.ResponseWriter, r *http.Request) error {
	req, err := decodeTransfer(r)
	if err != nil {
		return err
	}
	c, err := callOf(r)
	if err != nil {
		return err
	}
	if req.Overwrite && !req.Duplicate {
		if d := authz.Decide(c.principal, authz.ActionCopyOverwrite, authz.Target{}, c.policy); !d.Allowed {
			return d.Err
		}
	}
	svc, err := s.filesFor(r)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusAccepted, batchResponse{Results: svc.Copy(req.Sources, req.DestDir, req.Duplicate, req.Overwrite)})
	return nil
}

// fsMove answers POST /fs/move with one result per source.
func (s *Server) fsMove(w http.ResponseWriter, r *http.Request) error {
	req, err := decodeTransfer(r)
	if err != nil {
		return err
	}
	svc, err := s.filesFor(r)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusAccepted, batchResponse{Results: svc.Move(req.Sources, req.DestDir, req.Duplicate, req.Overwrite)})
	return nil
}

// decodeTransfer reads and checks the body of a copy or move.
func decodeTransfer(r *http.Request) (transferRequest, error) {
	var req transferRequest
	if err := decodeJSON(r, &req); err != nil {
		return req, err
	}
	if len(req.Sources) == 0 || req.DestDir == "" {
		return req, badRequest(msgSourcesRequired)
	}
	return req, nil
}

// fsCompress answers POST /fs/compress with the entry of the new zip (201).
func (s *Server) fsCompress(w http.ResponseWriter, r *http.Request) error {
	var req struct {
		Sources []string `json:"sources"`
		Dest    string   `json:"dest"`
	}
	if err := decodeJSON(r, &req); err != nil {
		return err
	}
	if len(req.Sources) == 0 || req.Dest == "" {
		return badRequest(msgCompressRequired)
	}
	svc, err := s.filesFor(r)
	if err != nil {
		return err
	}
	e, err := svc.Compress(req.Sources, req.Dest)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusCreated, e)
	return nil
}

// fsExtract answers POST /fs/extract with the entry of the destination folder.
// The contract has no overwrite option, so existing files are kept.
func (s *Server) fsExtract(w http.ResponseWriter, r *http.Request) error {
	var req struct {
		Archive string `json:"archive"`
		DestDir string `json:"destDir"`
	}
	if err := decodeJSON(r, &req); err != nil {
		return err
	}
	if req.Archive == "" || req.DestDir == "" {
		return badRequest(msgExtractRequired)
	}
	svc, err := s.filesFor(r)
	if err != nil {
		return err
	}
	e, err := svc.Extract(req.Archive, req.DestDir, false)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, e)
	return nil
}

// fsMeta answers GET /fs/meta with the entry at the path.
func (s *Server) fsMeta(w http.ResponseWriter, r *http.Request) error {
	path, err := queryPathOf(r)
	if err != nil {
		return err
	}
	svc, err := s.filesFor(r)
	if err != nil {
		return err
	}
	e, err := svc.Meta(path)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, e)
	return nil
}

// checkAlgo returns the algorithm to use ("" means sha256) or a bad request
// when it is not one the contract lists.
func checkAlgo(algo string) (string, error) {
	switch algo {
	case "":
		return algoSHA256, nil
	case algoSHA256, algoSHA1, algoMD5:
		return algo, nil
	}
	return "", badRequest(msgBadAlgo)
}

// fsChecksum answers GET /fs/checksum with the hex digest of one file.
func (s *Server) fsChecksum(w http.ResponseWriter, r *http.Request) error {
	path, err := queryPathOf(r)
	if err != nil {
		return err
	}
	algo, err := checkAlgo(r.URL.Query().Get(queryAlgo))
	if err != nil {
		return err
	}
	svc, err := s.filesFor(r)
	if err != nil {
		return err
	}
	sum, err := svc.Checksum(path, algo)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, map[string]string{"path": path, "algorithm": algo, "checksum": sum})
	return nil
}

// checksumResult is one entry of the POST /fs/checksums answer: a digest or the
// client-safe text of why there is none.
type checksumResult struct {
	Path  string `json:"path"`
	Hash  string `json:"hash,omitempty"`
	Error string `json:"error,omitempty"`
}

// fsChecksums answers POST /fs/checksums: one entry per path in request order,
// hashed by a small fixed pool of workers so a large batch cannot start more
// than checksumWorkers file reads at once.
func (s *Server) fsChecksums(w http.ResponseWriter, r *http.Request) error {
	var req struct {
		Paths []string `json:"paths"`
		Algo  string   `json:"algo"`
	}
	if err := decodeJSON(r, &req); err != nil {
		return err
	}
	switch {
	case len(req.Paths) == 0:
		return badRequest(msgPathsRequired)
	case len(req.Paths) > maxChecksumPaths:
		return badRequest(msgTooManyPaths)
	}
	algo, err := checkAlgo(req.Algo)
	if err != nil {
		return err
	}
	svc, err := s.filesFor(r)
	if err != nil {
		return err
	}
	out := s.hashAll(r.Context(), svc, req.Paths, algo)
	if r.Context().Err() != nil {
		return nil // the client left; the half-filled answer is not sent
	}
	writeJSON(w, http.StatusOK, map[string][]checksumResult{"checksums": out})
	return nil
}

// hashAll computes the digest of each path with at most checksumWorkers running
// at once, stopping early when ctx ends. Slot i of the result belongs to
// paths[i].
func (s *Server) hashAll(ctx context.Context, svc FileService, paths []string, algo string) []checksumResult {
	out := make([]checksumResult, len(paths))
	jobs := make(chan int)
	var wg sync.WaitGroup
	for range min(checksumWorkers, len(paths)) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range jobs {
				out[i] = checksumResult{Path: paths[i]}
				sum, err := svc.Checksum(paths[i], algo)
				if err != nil {
					out[i].Error = s.itemMessage(err)
					continue
				}
				out[i].Hash = sum
			}
		}()
	}
dispatch:
	for i := range paths {
		select {
		case jobs <- i:
		case <-ctx.Done():
			break dispatch
		}
	}
	close(jobs)
	wg.Wait()
	return out
}

// itemMessage is the client-safe text of a per-item failure; an unexpected
// error is logged and shows only the fixed internal text.
func (s *Server) itemMessage(err error) string {
	m := Map(err)
	if m.Internal {
		s.logf("api: file item: internal error: %v", err)
	}
	return m.Message
}

// drives answers GET /system/drives with the volumes the caller can reach.
func (s *Server) drives(w http.ResponseWriter, r *http.Request) error {
	svc, err := s.filesFor(r)
	if err != nil {
		return err
	}
	list, err := svc.Drives()
	if err != nil {
		return err
	}
	if list == nil {
		list = []FileDrive{}
	}
	writeJSON(w, http.StatusOK, list)
	return nil
}

// contentPut answers PUT /content: the body (at most maxContentBytes, held in
// memory because the service replaces the file atomically from a buffer)
// becomes the file's content. baseModified, when given, makes the write fail
// with STALE_WRITE if the file changed since it was read.
func (s *Server) contentPut(w http.ResponseWriter, r *http.Request) error {
	path, err := queryPathOf(r)
	if err != nil {
		return err
	}
	var base *time.Time
	if v := r.URL.Query().Get(queryBaseModified); v != "" {
		t, err := time.Parse(time.RFC3339, v)
		if err != nil {
			return badRequest(msgBadBaseModified)
		}
		base = &t
	}
	svc, err := s.filesFor(r)
	if err != nil {
		return err
	}
	data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxContentBytes))
	if err != nil {
		return bodyReadError(err)
	}
	e, err := svc.WriteContent(path, data, base)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, e)
	return nil
}
