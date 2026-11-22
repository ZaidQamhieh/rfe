package daemon

import (
	"context"
	"io/fs"
	"net/url"
	"time"

	"github.com/ZaidQamhieh/rfe/agent/internal/next/api"
	"github.com/ZaidQamhieh/rfe/agent/internal/next/audit"
	"github.com/ZaidQamhieh/rfe/agent/internal/next/authz"
	"github.com/ZaidQamhieh/rfe/agent/internal/next/files"
	"github.com/ZaidQamhieh/rfe/agent/internal/next/rootfs"
	"github.com/ZaidQamhieh/rfe/agent/internal/next/search"
	"github.com/ZaidQamhieh/rfe/agent/internal/next/settings"
	"github.com/ZaidQamhieh/rfe/agent/internal/next/share"
	"github.com/ZaidQamhieh/rfe/agent/internal/next/store"
	"github.com/ZaidQamhieh/rfe/agent/internal/next/transfer"
	"github.com/ZaidQamhieh/rfe/agent/internal/sidecar"
)

// The adapters must satisfy every interface the HTTP layer looks for. The two
// extra ones matter most: the HTTP layer answers 501 for a file service that
// lacks them.
var (
	_ api.FileService    = (*fileAdapter)(nil)
	_ api.FsExtraService = (*fileAdapter)(nil)
	_ api.ContentService = (*fileAdapter)(nil)
	_ api.FilesProvider  = (*filesProvider)(nil)
	_ api.Transfers      = (*transferAdapter)(nil)
	_ api.SearchService  = (*searchAdapter)(nil)
	_ api.ShareService   = (*shareAdapter)(nil)
)

// fileAdapter presents one caller's *files.Service as the api file interfaces.
// The service and the api have separate (field-identical) types because the
// files package imports the api for its error kinds, so the api cannot name it.
type fileAdapter struct{ svc *files.Service }

// entryOf converts a file service entry to its api twin; nil stays nil.
func entryOf(e *files.Entry) *api.FileEntry {
	if e == nil {
		return nil
	}
	v := api.FileEntry(*e)
	return &v
}

// List returns one page of a folder.
func (a *fileAdapter) List(path, cursor string, limit int) (*api.FileListing, error) {
	l, err := a.svc.List(path, cursor, limit)
	if err != nil {
		return nil, err
	}
	out := &api.FileListing{Path: l.Path, NextCursor: l.NextCursor, Entries: make([]api.FileEntry, len(l.Entries))}
	for i, e := range l.Entries {
		out.Entries[i] = api.FileEntry(e)
	}
	return out, nil
}

// Meta describes one file or folder.
func (a *fileAdapter) Meta(path string) (*api.FileEntry, error) {
	e, err := a.svc.Meta(path)
	return entryOf(e), err
}

// Checksum hashes a file with the named algorithm.
func (a *fileAdapter) Checksum(path, algo string) (string, error) { return a.svc.Checksum(path, algo) }

// CreateFolder makes a folder.
func (a *fileAdapter) CreateFolder(path string) (*api.FileEntry, error) {
	e, err := a.svc.CreateFolder(path)
	return entryOf(e), err
}

// CreateFile makes an empty file.
func (a *fileAdapter) CreateFile(path string) (*api.FileEntry, error) {
	e, err := a.svc.CreateFile(path)
	return entryOf(e), err
}

// WriteContent replaces a file's content, refusing when it changed since baseModified.
func (a *fileAdapter) WriteContent(path string, data []byte, baseModified *time.Time) (*api.FileEntry, error) {
	e, err := a.svc.WriteContent(path, data, baseModified)
	return entryOf(e), err
}

// Rename moves a file or folder to a new name.
func (a *fileAdapter) Rename(src, dst string) (*api.FileEntry, error) {
	e, err := a.svc.Rename(src, dst)
	return entryOf(e), err
}

// Copy copies each source into destDir.
func (a *fileAdapter) Copy(sources []string, destDir string, duplicate, overwrite bool) []api.ItemResult {
	return a.svc.Copy(sources, destDir, duplicate, overwrite)
}

// Move moves each source into destDir.
func (a *fileAdapter) Move(sources []string, destDir string, duplicate, overwrite bool) []api.ItemResult {
	return a.svc.Move(sources, destDir, duplicate, overwrite)
}

// Delete permanently removes each path.
func (a *fileAdapter) Delete(paths []string) []api.ItemResult { return a.svc.Delete(paths) }

// Trash moves each path to the trash.
func (a *fileAdapter) Trash(paths []string) []api.ItemResult { return a.svc.Trash(paths) }

// Compress packs sources into a zip at dest.
func (a *fileAdapter) Compress(sources []string, dest string) (*api.FileEntry, error) {
	e, err := a.svc.Compress(sources, dest)
	return entryOf(e), err
}

// Extract unpacks an archive into destDir.
func (a *fileAdapter) Extract(archive, destDir string, overwrite bool) (*api.FileEntry, error) {
	e, err := a.svc.Extract(archive, destDir, overwrite)
	return entryOf(e), err
}

// Drives lists the volumes the caller can reach.
func (a *fileAdapter) Drives() ([]api.FileDrive, error) {
	ds, err := a.svc.Drives()
	if err != nil {
		return nil, err
	}
	out := make([]api.FileDrive, len(ds))
	for i, d := range ds {
		out[i] = api.FileDrive(d)
	}
	return out, nil
}

// ListTrash lists what is in the trash.
func (a *fileAdapter) ListTrash() ([]api.TrashItem, error) {
	ts, err := a.svc.ListTrash()
	if err != nil {
		return nil, err
	}
	out := make([]api.TrashItem, len(ts))
	for i, t := range ts {
		out[i] = api.TrashItem(t)
	}
	return out, nil
}

// Restore puts trash items back where they came from.
func (a *fileAdapter) Restore(ids []string) []api.ItemResult { return a.svc.Restore(ids) }

// EmptyTrash permanently removes the named trash items, or all of them.
func (a *fileAdapter) EmptyTrash(ids []string) ([]api.ItemResult, error) {
	return a.svc.EmptyTrash(ids)
}

// ValidTrashID reports whether id can name a trash item.
func (a *fileAdapter) ValidTrashID(id string) bool { return files.ValidTrashID(id) }

// Recent lists the newest files under the requested folders.
func (a *fileAdapter) Recent(ctx context.Context, req api.RecentRequest) (*api.RecentPage, error) {
	res, err := a.svc.Recent(ctx, files.RecentRequest{Root: req.Root, Roots: req.Roots, Limit: req.Limit})
	if err != nil {
		return nil, err
	}
	page := &api.RecentPage{Partial: res.Partial, Entries: make([]api.FileEntry, len(res.Entries))}
	for i, e := range res.Entries {
		page.Entries[i] = api.FileEntry(e)
	}
	return page, nil
}

// Usage adds up the sizes under a folder, per immediate child.
func (a *fileAdapter) Usage(ctx context.Context, path string) (*api.UsagePage, error) {
	res, err := a.svc.Usage(ctx, path)
	if err != nil {
		return nil, err
	}
	page := &api.UsagePage{Path: res.Path, TotalBytes: res.TotalBytes, Partial: res.Partial, Entries: make([]api.UsageEntryJSON, len(res.Entries))}
	for i, e := range res.Entries {
		page.Entries[i] = api.UsageEntryJSON(e)
	}
	return page, nil
}

// MediaInfo reads the size, capture date and duration from a photo or video header.
func (a *fileAdapter) MediaInfo(path string) (*api.FileMediaInfo, error) {
	m, err := a.svc.MediaInfo(path)
	if err != nil || m == nil {
		return nil, err
	}
	v := api.FileMediaInfo(*m)
	return &v, nil
}

// Chmod sets the permission bits of a file.
func (a *fileAdapter) Chmod(path string, mode fs.FileMode) (*api.FileEntry, error) {
	e, err := a.svc.Chmod(path, mode)
	return entryOf(e), err
}

// OpenContent opens a regular file for streaming.
func (a *fileAdapter) OpenContent(path string) (*api.ContentFile, error) {
	return a.svc.OpenContent(path)
}

// ArchiveEntries lists the first limit entries of an archive.
func (a *fileAdapter) ArchiveEntries(path string, limit int) ([]api.ArchiveEntry, error) {
	return a.svc.ArchiveEntries(path, limit)
}

// filesProvider gives each caller the file service of its own view of the disk.
type filesProvider struct{ hub *hub }

// For returns the file service confined to the caller's jail, read-only when
// either the device or the whole agent is.
func (p *filesProvider) For(pr authz.Principal, pol authz.Policy) (api.FileService, error) {
	svc, err := p.hub.filesFor(pr.JailRoot, pr.ReadOnly || pol.ReadOnly)
	if err != nil {
		return nil, err
	}
	return &fileAdapter{svc: svc}, nil
}

// transferAdapter binds the upload engine to the caller's view of the disk.
type transferAdapter struct {
	mgr *transfer.Manager
	hub *hub
}

// publisher returns the file service the engine publishes finished uploads through.
func (t *transferAdapter) publisher(p authz.Principal, pol authz.Policy) (*files.Service, error) {
	return t.hub.filesFor(p.JailRoot, p.ReadOnly || pol.ReadOnly)
}

// Open starts an upload session.
func (t *transferAdapter) Open(p authz.Principal, pol authz.Policy, req api.TransferOpen) (*store.Upload, error) {
	pub, err := t.publisher(p, pol)
	if err != nil {
		return nil, err
	}
	return t.mgr.Open(p, pub, transfer.OpenRequest(req))
}

// Lookup returns the session row without touching it.
func (t *transferAdapter) Lookup(p authz.Principal, id string) (*store.Upload, error) {
	return t.mgr.Lookup(p, id)
}

// Status returns the session with its received chunk numbers.
func (t *transferAdapter) Status(p authz.Principal, id string) (*store.Upload, error) {
	return t.mgr.Status(p, id)
}

// Delete removes an inactive session.
func (t *transferAdapter) Delete(p authz.Principal, id string) error { return t.mgr.Delete(p, id) }

// WriteChunk stores one chunk after checking its length and SHA-256.
func (t *transferAdapter) WriteChunk(p authz.Principal, id string, n int, data []byte, chunkSHA256 string) error {
	return t.mgr.WriteChunk(p, id, n, data, chunkSHA256)
}

// Complete verifies the whole file and publishes it. A nil entry stays a nil
// interface so the caller can tell "no entry" from an entry.
func (t *transferAdapter) Complete(p authz.Principal, pol authz.Policy, id string) (any, error) {
	pub, err := t.publisher(p, pol)
	if err != nil {
		return nil, err
	}
	e, err := t.mgr.Complete(p, pub, id)
	if e == nil {
		return nil, err
	}
	return e, err
}

// searchAdapter runs searches on behalf of a device, inside its jail.
type searchAdapter struct {
	hub      *hub
	settings *settings.Service
	indexer  search.Indexer // nil: walk the folders
}

// rootsFor returns the folders a search starts from: only the jail for a
// jailed device, else the owner's allowed folders (empty means the home folder).
func (s *searchAdapter) rootsFor(jail string) []string {
	if jail != "" {
		return []string{jail}
	}
	return s.settings.Get().Roots
}

// Search parses the query and searches the caller's folders.
func (s *searchAdapter) Search(ctx context.Context, jailRoot string, query url.Values) (api.SearchPage, error) {
	req, err := search.ParseQuery(query)
	if err != nil {
		return api.SearchPage{}, err
	}
	set, err := s.hub.setFor(jailRoot)
	if err != nil {
		return api.SearchPage{}, err
	}
	return s.run(ctx, set, jailRoot, req)
}

// run executes req over set, starting from the roots for jail.
func (s *searchAdapter) run(ctx context.Context, set *rootfs.Set, jail string, req search.Request) (api.SearchPage, error) {
	cfg := search.Config{Roots: s.rootsFor(jail), Indexer: s.indexer, Note: sidecar.Note}
	res, err := search.New(set, cfg).Search(ctx, req)
	if err != nil {
		return api.SearchPage{}, err
	}
	return api.SearchPage{Entries: res.Entries, NextCursor: res.NextCursor, Truncated: res.Truncated, TimeBudget: res.TimeBudget}, nil
}

// shareAdapter presents the live share service to the HTTP layer.
type shareAdapter struct{ hub *hub }

// service returns the share service of the live folders.
func (s *shareAdapter) service() (*share.Service, error) {
	g, err := s.hub.current()
	if err != nil {
		return nil, err
	}
	return g.share, nil
}

// caller converts the HTTP layer's caller to the share service's.
func caller(c api.ShareCaller) share.Caller {
	return share.Caller{Principal: c.Principal, Label: c.Label}
}

// auditEvents converts the share service's audit events.
func auditEvents(in []share.Event) []audit.Event {
	out := make([]audit.Event, len(in))
	for i, e := range in {
		out[i] = audit.Event(e)
	}
	return out
}

// Mint creates a one-use link.
func (s *shareAdapter) Mint(c api.ShareCaller, req api.ShareMintRequest) (api.ShareMinted, error) {
	svc, err := s.service()
	if err != nil {
		return api.ShareMinted{}, err
	}
	m, err := svc.Mint(caller(c), share.MintRequest(req))
	if err != nil {
		return api.ShareMinted{}, err
	}
	return api.ShareMinted{Token: m.Token, TokenHash: m.TokenHash, ExpiresAt: m.ExpiresAt, Events: auditEvents(m.Events)}, nil
}

// List returns the caller's active links.
func (s *shareAdapter) List(c api.ShareCaller) ([]api.ShareSummary, error) {
	svc, err := s.service()
	if err != nil {
		return nil, err
	}
	rows, err := svc.List(caller(c))
	if err != nil {
		return nil, err
	}
	out := make([]api.ShareSummary, len(rows))
	for i, r := range rows {
		out[i] = api.ShareSummary(r)
	}
	return out, nil
}

// Revoke deletes a link of the caller.
func (s *shareAdapter) Revoke(c api.ShareCaller, tokenHash string) ([]audit.Event, error) {
	svc, err := s.service()
	if err != nil {
		return nil, err
	}
	ev, err := svc.Revoke(caller(c), tokenHash)
	if err != nil {
		return nil, err
	}
	return auditEvents(ev), nil
}

// Resolve uses up a link and opens its file for the public fetch.
func (s *shareAdapter) Resolve(token, ip string) (*api.ShareDownload, error) {
	svc, err := s.service()
	if err != nil {
		return nil, err
	}
	d, err := svc.Resolve(token, ip)
	if err != nil {
		return nil, err
	}
	return &api.ShareDownload{
		Content: d.Content, Name: d.Name, ModTime: d.ModTime,
		Header: d.Header, RecordErr: d.RecordErr, Close: d.Close,
	}, nil
}

// DropDevice forwards to the engine so the API can clear a revoked device's unfinished uploads.
func (t *transferAdapter) DropDevice(deviceID string) { t.mgr.DropDevice(deviceID) }

// The v3 database is the transfer engine's store in production.
var _ transfer.Store = (*store.DB)(nil)
