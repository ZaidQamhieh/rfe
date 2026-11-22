// Package share is the share-link service of the rewritten agent. A paired
// device mints a link for one file; anyone holding the link's token can fetch
// that file once, until the link expires or its owner revokes it.
//
// Only the SHA-256 hash of a token is ever stored (the hash format is the old
// agent's, so links carried over by the import keep working). A link is only as
// good as the device that minted it: when the link is fetched, the device must
// still exist, not be revoked and still hold the browse, download and share
// grants, and the file is opened through that device's jail.
//
// The package knows nothing about HTTP routing. It returns domain errors that
// match package api (or authz) kinds, and audit events as plain values for the
// caller to record.
package share

import (
	"crypto/rand"
	"crypto/subtle"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"github.com/ZaidQamhieh/rfe/agent/internal/next/api"
	"github.com/ZaidQamhieh/rfe/agent/internal/next/authz"
	"github.com/ZaidQamhieh/rfe/agent/internal/next/files"
	"github.com/ZaidQamhieh/rfe/agent/internal/next/rootfs"
	"github.com/ZaidQamhieh/rfe/agent/internal/next/store"
)

// Link limits and defaults.
const (
	// TokenBytes is how many random bytes make a token (256 bits, written as hex).
	TokenBytes = 32
	// DefaultExpiry is how long a link lives when the request names no lifetime
	// (the app's default choice, one day).
	DefaultExpiry = 24 * time.Hour
	// MaxExpiry is the longest lifetime a request may ask for.
	MaxExpiry = 7 * 24 * time.Hour
	// MaxFileSize is the largest file a link may be minted for or serve.
	MaxFileSize int64 = 500 << 20
)

// Limits on the unauthenticated fetch route, per source address.
const (
	RateLimitAttempts = 10
	RateLimitWindow   = time.Minute
	RateLimitMaxIPs   = 4096
)

// Audit actions of the events the service returns.
const (
	AuditShareCreated = "share_created"
	AuditShareRevoked = "share_revoked"
)

// Message texts that are safe to show a client.
const (
	msgDisabled     = "share links are disabled on this agent"
	msgNotFound     = "share link not found or expired"
	msgNoSuchShare  = "no such share"
	msgNeedDevice   = "device required"
	msgPathRequired = "path required"
	msgTokenHash    = "tokenHash required"
)

// msgExpiryRange names the longest lifetime in whole days, built from MaxExpiry.
var msgExpiryRange = fmt.Sprintf("expiresInSeconds must be between 0 and the %d day maximum", int(MaxExpiry/(24*time.Hour)))

// ErrDisabled is returned by Mint while the agent's sharing switch is off.
var ErrDisabled error = &authz.Denial{Kind: authz.ErrForbidden, Msg: msgDisabled}

// ErrLinkNotFound is the one answer a fetch gives for a link that never
// existed, expired, was used, was revoked, or whose owner may no longer share:
// the response never says which.
var ErrLinkNotFound error = &kindError{kind: api.ErrNotFound, msg: msgNotFound}

// ErrNoSuchShare is the answer to a revoke of a link that does not exist or
// belongs to another device (a foreign hash is never confirmed).
var ErrNoSuchShare error = &kindError{kind: api.ErrNotFound, msg: msgNoSuchShare}

// kindError pairs a domain error (kind) with an optional safe message and the
// error behind it; errors.Is matches both kind and cause.
type kindError struct {
	kind  error
	msg   string
	cause error
}

func (e *kindError) Error() string {
	if e.msg != "" {
		return e.msg
	}
	return e.kind.Error()
}

func (e *kindError) Unwrap() []error {
	if e.cause == nil {
		return []error{e.kind}
	}
	return []error{e.kind, e.cause}
}

// Config is what New needs. Set and Store are required; the rest have defaults.
type Config struct {
	// Set holds the folders the agent may serve; each device's jail narrows it.
	Set *rootfs.Set
	// Store is the database holding links, history and devices.
	Store *store.DB
	// Sharing reports whether the agent's sharing switch is on; nil means on.
	Sharing func() bool
	// Now is the clock; nil means time.Now.
	Now func() time.Time
	// Rand is the source of token bytes; nil means crypto/rand.
	Rand io.Reader
}

// Service mints, lists, revokes and serves share links.
type Service struct {
	set     *rootfs.Set
	db      *store.DB
	sharing func() bool
	now     func() time.Time
	rand    io.Reader
	limiter *ipLimiter
}

// New returns a Service. It panics if cfg.Set or cfg.Store is nil, which is a
// wiring mistake, not a runtime condition.
func New(cfg Config) *Service {
	if cfg.Set == nil || cfg.Store == nil {
		panic("share: Config needs Set and Store")
	}
	s := &Service{set: cfg.Set, db: cfg.Store, sharing: cfg.Sharing, now: cfg.Now, rand: cfg.Rand}
	if s.now == nil {
		s.now = time.Now
	}
	if s.rand == nil {
		s.rand = rand.Reader
	}
	s.limiter = newIPLimiter(RateLimitAttempts, RateLimitWindow, RateLimitMaxIPs)
	return s
}

// Caller is the authenticated device making a mint, list or revoke request.
type Caller struct {
	authz.Principal
	// Label names the device in audit events; the device id is used when empty.
	Label string
}

// actor is the name an audit event carries for the caller.
func (c Caller) actor() string {
	if c.Label != "" {
		return c.Label
	}
	return c.DeviceID
}

// Event is one audit entry for the caller to record (the service does not
// import the audit package).
type Event struct {
	Action, Actor, Target, Detail string
}

// MintRequest is the body of a mint call. ExpiresInSeconds of 0 means
// DefaultExpiry.
type MintRequest struct {
	Path             string
	ExpiresInSeconds int64
}

// Minted is the result of a successful mint. Token is the only copy of the
// secret: it is returned once and never stored.
type Minted struct {
	Token     string
	TokenHash string
	Path      string
	ExpiresAt int64 // Unix seconds
	Events    []Event
}

// Summary is one active link as listed to its owner.
type Summary struct {
	TokenHash string
	Path      string
	ExpiresAt int64 // Unix seconds
}

// enabled reports whether the sharing switch is on.
func (s *Service) enabled() bool { return s.sharing == nil || s.sharing() }

// Mint creates a one-use link to the regular file at req.Path, seen through the
// caller's jail. The caller needs the share, browse and download grants. The
// link and its history row are written together; the audit event is returned,
// not recorded.
func (s *Service) Mint(c Caller, req MintRequest) (*Minted, error) {
	if !s.enabled() {
		return nil, ErrDisabled
	}
	if req.Path == "" {
		return nil, &kindError{kind: api.ErrBadRequest, msg: msgPathRequired}
	}
	if req.ExpiresInSeconds < 0 || req.ExpiresInSeconds > int64(MaxExpiry/time.Second) {
		return nil, &kindError{kind: api.ErrBadRequest, msg: msgExpiryRange}
	}
	if d := authz.Decide(c.Principal, authz.ActionMintShare, authz.Target{Paths: []string{req.Path}}, authz.Policy{}); !d.Allowed {
		return nil, d.Err
	}
	set, closeSet, err := s.setFor(c.JailRoot)
	if err != nil {
		return nil, classify(err)
	}
	defer closeSet()
	of, err := openRegular(set, req.Path)
	if err != nil {
		return nil, err
	}
	resolved := of.entry.Path
	// Only the check mattered; the link serves the file afresh when it is fetched.
	_ = of.file.Close()

	expiresIn := DefaultExpiry
	if req.ExpiresInSeconds > 0 {
		expiresIn = time.Duration(req.ExpiresInSeconds) * time.Second
	}
	now := s.now()
	token, err := s.newToken()
	if err != nil {
		return nil, fmt.Errorf("share: generate token: %w", err)
	}
	hash := store.HashToken(token)
	link := store.ShareLink{TokenHash: hash, Path: resolved, DeviceID: c.DeviceID, Created: now.Unix(), Expires: now.Add(expiresIn).Unix()}
	ev := store.ShareEvent{TokenHash: hash, Path: resolved, MintedAt: link.Created, ExpiresAt: link.Expires}
	if err := s.db.InsertShareLink(link, ev); err != nil {
		return nil, fmt.Errorf("share: store link: %w", err)
	}
	return &Minted{
		Token: token, TokenHash: hash, Path: resolved, ExpiresAt: link.Expires,
		Events: []Event{{
			Action: AuditShareCreated, Actor: c.actor(), Target: resolved,
			Detail: "expires=" + time.Unix(link.Expires, 0).UTC().Format(time.RFC3339),
		}},
	}, nil
}

// newToken returns TokenBytes random bytes as lower-case hex.
func (s *Service) newToken() (string, error) {
	b := make([]byte, TokenBytes)
	if _, err := io.ReadFull(s.rand, b); err != nil {
		return "", err
	}
	const hexDigits = "0123456789abcdef"
	out := make([]byte, 0, 2*len(b))
	for _, v := range b {
		out = append(out, hexDigits[v>>4], hexDigits[v&0x0f])
	}
	return string(out), nil
}

// owns reports whether c may see or revoke a link owned by ownerID: its own
// links, or any link for an admin device. An owner-less (pre-owner) link is for
// admins only.
func (c Caller) owns(ownerID string) bool {
	if c.DeviceID == "" || c.Revoked {
		return false
	}
	return c.Admin || (ownerID != "" && ownerID == c.DeviceID)
}

// List returns the caller's links that are neither used nor expired, oldest
// first: every link for an admin device, only its own for any other.
func (s *Service) List(c Caller) ([]Summary, error) {
	if c.DeviceID == "" || c.Revoked {
		return nil, &authz.Denial{Kind: authz.ErrForbidden, Msg: msgNeedDevice}
	}
	scope := ""
	if !c.Admin {
		scope = c.DeviceID
	}
	links, err := s.db.ActiveShareLinks(scope, s.now().Unix())
	if err != nil {
		return nil, fmt.Errorf("share: list links: %w", err)
	}
	out := make([]Summary, 0, len(links))
	for _, l := range links {
		out = append(out, Summary{TokenHash: l.TokenHash, Path: l.Path, ExpiresAt: l.Expires})
	}
	return out, nil
}

// Revoke deletes the link with the given token hash. Only its owner or an admin
// may; anyone else gets the same ErrNoSuchShare as for an unknown hash.
func (s *Service) Revoke(c Caller, tokenHash string) ([]Event, error) {
	if tokenHash == "" {
		return nil, &kindError{kind: api.ErrBadRequest, msg: msgTokenHash}
	}
	link, err := s.db.ShareLinkByHash(tokenHash)
	if err != nil {
		return nil, fmt.Errorf("share: find link: %w", err)
	}
	if link == nil || !sameHash(link.TokenHash, tokenHash) || !c.owns(link.DeviceID) {
		return nil, ErrNoSuchShare
	}
	deleted, err := s.db.DeleteShareLink(link.TokenHash)
	if err != nil {
		return nil, fmt.Errorf("share: delete link: %w", err)
	}
	if !deleted {
		return nil, ErrNoSuchShare // someone else revoked it first
	}
	return []Event{{Action: AuditShareRevoked, Actor: c.actor(), Target: link.Path}}, nil
}

// Sweep deletes expired links and returns how many it removed; the caller runs
// it on a timer.
func (s *Service) Sweep() (int, error) {
	return s.db.SweepShareLinks(s.now().Unix())
}

// sameHash compares two token hashes in constant time.
func sameHash(a, b string) bool {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

// Download is a file ready to be sent. Content reads exactly the bytes the file
// held when it was opened. The caller must call Close.
type Download struct {
	Content io.ReadSeeker
	Name    string
	ModTime time.Time
	Size    int64
	// Header carries the response headers that keep a browser from running the
	// file as a page (attachment disposition, nosniff, sandbox policy, no-store).
	Header http.Header
	// RecordErr is non-nil when the serve could not be written to the share
	// history; the file is still delivered, and the caller should log it.
	RecordErr error

	file     *os.File
	closeSet func()
}

// Close releases the open file and the jail view it came through.
func (d *Download) Close() error {
	err := d.file.Close()
	d.closeSet()
	return err
}

// Resolve fetches the file behind token for a request from ip. It is the
// unauthenticated route: after the per-address rate limit, an unknown,
// expired, used or revoked link, a disabled sharing switch, or an owner that
// may no longer share all give ErrLinkNotFound, and none of them uses up a
// live link. Only a request that passes every check consumes the link; if the
// file then cannot be sent (deleted, no longer regular, over MaxFileSize) the
// link stays used.
func (s *Service) Resolve(token, ip string) (*Download, error) {
	now := s.now()
	if !s.limiter.allow(ip, now) {
		return nil, api.ErrRateLimited
	}
	if !s.enabled() {
		return nil, ErrLinkNotFound
	}
	hash := store.HashToken(token)
	link, err := s.db.ShareLinkByHash(hash)
	if err != nil {
		return nil, fmt.Errorf("share: find link: %w", err)
	}
	if link == nil || !sameHash(link.TokenHash, hash) || link.ConsumedAt != nil || link.Expires < now.Unix() {
		return nil, ErrLinkNotFound
	}
	// A link older than owner tracking has no owner to re-check: it is served
	// through the global folders, as the old agent did.
	jail := ""
	if link.DeviceID != "" {
		dev, err := s.db.ShareOwnerDevice(link.DeviceID)
		if err != nil {
			return nil, fmt.Errorf("share: find owner: %w", err)
		}
		if dev == nil {
			return nil, ErrLinkNotFound
		}
		p := principalOf(dev)
		if d := authz.Decide(p, authz.ActionMintShare, authz.Target{Paths: []string{link.Path}}, authz.Policy{}); !d.Allowed {
			return nil, ErrLinkNotFound
		}
		jail = dev.JailRoot
	}
	set, closeSet, err := s.setFor(jail)
	if err != nil {
		return nil, ErrLinkNotFound
	}
	// The atomic update decides a race between two requests for the same link.
	won, err := s.db.ConsumeShareLink(hash, now.Unix())
	if err != nil {
		closeSet()
		return nil, fmt.Errorf("share: use link: %w", err)
	}
	if !won {
		closeSet()
		return nil, ErrLinkNotFound
	}
	of, err := openRegular(set, link.Path)
	if err != nil {
		closeSet()
		if errors.Is(err, api.ErrPayloadTooLarge) {
			return nil, err
		}
		return nil, ErrLinkNotFound
	}
	d := &Download{
		// A section reader pins the length to the size checked above, so a writer
		// appending after the check cannot make the response longer.
		Content: io.NewSectionReader(of.file, 0, of.info.Size()),
		Name:    of.info.Name(), ModTime: of.info.ModTime(), Size: of.info.Size(),
		Header: downloadHeaders(of.info.Name()),
		file:   of.file, closeSet: closeSet,
	}
	d.RecordErr = s.db.RecordShareServed(hash, ip, now.Unix())
	return d, nil
}

// principalOf turns a stored device into the caller description authz decides on.
func principalOf(d *store.Device) authz.Principal {
	return authz.Principal{
		DeviceID: d.ID, Admin: d.IsAdmin(), Revoked: d.Revoked, JailRoot: d.JailRoot, ReadOnly: d.ReadOnly,
		Grants: authz.Grants{
			Browse: d.CanBrowse, Download: d.CanDownload, Upload: d.CanUpload,
			Modify: d.CanModify, Delete: d.CanDelete, Share: d.CanShare,
		},
	}
}

// downloadHeaders are the response headers for a file of unknown, untrusted
// content: force a download, forbid type sniffing, sandbox it, never cache it.
func downloadHeaders(name string) http.Header {
	disposition := mime.FormatMediaType("attachment", map[string]string{"filename": filepath.Base(name)})
	if disposition == "" {
		disposition = "attachment"
	}
	h := http.Header{}
	h.Set("Content-Disposition", disposition)
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Content-Security-Policy", "sandbox; default-src 'none'; base-uri 'none'; form-action 'none'; frame-ancestors 'none'")
	h.Set("Cache-Control", "no-store")
	return h
}

// setFor returns the folders a device with the given jail may reach (the whole
// Set when jail is empty) and a function that releases the view.
func (s *Service) setFor(jail string) (*rootfs.Set, func(), error) {
	if jail == "" {
		return s.set, func() {}, nil
	}
	narrow, err := s.set.Narrow(jail)
	if err != nil {
		return nil, nil, err
	}
	return narrow, func() { _ = narrow.Close() }, nil
}

// openedFile is a regular file opened for reading with what was learned on the way.
type openedFile struct {
	entry *files.Entry
	info  fs.FileInfo
	file  *os.File
}

// openRegular describes the file at path through the files service (which
// classifies missing, outside-the-folders and protected paths) and opens it for
// reading. The opened descriptor, not the earlier description, decides the
// answer: it must be a regular file of at most MaxFileSize. The files service
// has no read operation yet, so the bytes come from the same rootfs view.
func openRegular(set *rootfs.Set, path string) (*openedFile, error) {
	entry, err := files.New(set).Meta(path)
	if err != nil {
		return nil, err
	}
	h, err := set.Open(path)
	if err != nil {
		return nil, classify(err)
	}
	f, err := h.Open()
	if err != nil {
		return nil, classify(err)
	}
	info, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return nil, err
	}
	switch {
	case !info.Mode().IsRegular():
		_ = f.Close()
		return nil, &kindError{kind: api.ErrNotAFile}
	case info.Size() > MaxFileSize:
		_ = f.Close()
		return nil, &kindError{kind: api.ErrPayloadTooLarge}
	}
	return &openedFile{entry: entry, info: info, file: f}, nil
}

// classify turns a rootfs or OS error into one that matches a domain error of
// package api; an error with no domain meaning is returned unchanged (the api
// layer reports it as internal). The text never carries a host path.
func classify(err error) error {
	if err == nil {
		return nil
	}
	for _, k := range []error{api.ErrForbidden, api.ErrBadRequest, api.ErrPathNotFound, api.ErrNotAFile} {
		if errors.Is(err, k) {
			return err
		}
	}
	var kind error
	switch {
	case errors.Is(err, rootfs.ErrOutsideRoots), errors.Is(err, rootfs.ErrProtected), errors.Is(err, rootfs.ErrRootTarget):
		kind = api.ErrForbidden
	case errors.Is(err, rootfs.ErrInvalidPath):
		kind = api.ErrBadRequest
	case errors.Is(err, rootfs.ErrNotRegular):
		kind = api.ErrNotAFile
	case errors.Is(err, fs.ErrNotExist), errors.Is(err, syscall.ENOTDIR):
		kind = api.ErrPathNotFound
	default:
		return err
	}
	return &kindError{kind: kind, cause: err}
}
