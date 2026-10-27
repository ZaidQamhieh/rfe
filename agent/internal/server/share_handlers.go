// Package server — R1 one-time share link handlers.
//
// GET /v1/share/{token} is the only unauthenticated route in the agent
// besides /health and /pair — see docs/r1-share-link-threat-model.md. Every
// other route in this file is authenticated and mounted in server.go's
// authenticated route group.
package server

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"os"
	"sync"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/ZaidQamhieh/rfe/agent/internal/fsops"
	"github.com/ZaidQamhieh/rfe/agent/internal/store"
)

const (
	shareDefaultExpiry = 15 * time.Minute
	shareMaxExpiry     = 24 * time.Hour
	shareMaxFileSize   = 500 << 20 // 500 MiB per the R1 threat model.

	// T2: the token is 32 bytes of crypto/rand (2^256 space) so brute force is
	// already infeasible, but the unauthenticated /share/{token} route is
	// rate-limited anyway, matching /pair's defense-in-depth posture.
	shareRateLimitAttempts = 10
	shareRateLimitWindow   = time.Minute
	shareRateLimitMaxIPs   = 4096

	// shareSweepInterval is how often StartShareSweeper deletes expired
	// share tokens (T6).
	shareSweepInterval = 5 * time.Minute
)

func hashShareToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// --------- POST /v1/share/mint (authenticated) ---------

type mintShareRequest struct {
	Path             string `json:"path"`
	ExpiresInSeconds int64  `json:"expiresInSeconds"`
}

type mintShareResponse struct {
	Token     string `json:"token"`
	TokenHash string `json:"tokenHash"`
	ExpiresAt int64  `json:"expiresAt"`
	URL       string `json:"url"`
}

func mintShareHandler(cfg Config, db *store.DB, ops *fsops.Ops) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !cfg.Settings.IsAllowSharing() {
			writeError(w, http.StatusForbidden, "FORBIDDEN", "share links are disabled on this agent")
			return
		}

		var req mintShareRequest
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxJSONBody)).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid JSON body")
			return
		}
		if req.Path == "" {
			writeError(w, http.StatusBadRequest, "BAD_REQUEST", "path required")
			return
		}
		if req.ExpiresInSeconds < 0 || req.ExpiresInSeconds > int64(shareMaxExpiry/time.Second) {
			writeError(w, http.StatusBadRequest, "BAD_REQUEST", "expiresInSeconds exceeds the 24h maximum")
			return
		}

		reqOps := opsFromContext(r.Context(), ops)
		resolved, err := reqOps.Resolve(req.Path)
		if err != nil {
			handleFsError(w, err)
			return
		}
		info, err := reqOps.Stat(req.Path)
		if err != nil {
			if os.IsNotExist(err) {
				writeError(w, http.StatusNotFound, "NOT_FOUND", "file not found")
			} else {
				writeInternal(w, "mint share", err)
			}
			return
		}
		if !info.Mode().IsRegular() {
			writeError(w, http.StatusBadRequest, "BAD_REQUEST", "only regular files can be shared")
			return
		}
		if info.Size() > shareMaxFileSize {
			writeError(w, http.StatusRequestEntityTooLarge, "FILE_TOO_LARGE", "shared files may not exceed 500 MiB")
			return
		}

		expiresIn := shareDefaultExpiry
		if req.ExpiresInSeconds > 0 {
			expiresIn = time.Duration(req.ExpiresInSeconds) * time.Second
		}
		expiresAt := time.Now().Add(expiresIn)

		token, err := randomToken(32)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "INTERNAL", "failed to generate token")
			return
		}
		hash := hashShareToken(token)

		// Stamp the minting device so list/revoke can be scoped to it (PR-03).
		var ownerID string
		if d := deviceFromContext(r); d != nil {
			ownerID = d.ID
		}
		if err := db.CreateShareToken(hash, resolved, ownerID, expiresAt); err != nil {
			writeInternal(w, "create share token", err)
			return
		}
		if err := db.LogShareMint(hash, resolved, expiresAt); err != nil {
			if cleanupErr := db.DeleteShareToken(hash); cleanupErr != nil {
				log.Printf("share audit: remove unaudited token %s: %v", hash[:8], cleanupErr)
			}
			writeInternal(w, "record share mint", err)
			return
		}
		audit(db, r, store.AuditShareCreated, resolved,
			"expires="+expiresAt.UTC().Format(time.RFC3339))

		writeJSON(w, http.StatusOK, mintShareResponse{
			Token:     token,
			TokenHash: hash,
			ExpiresAt: expiresAt.Unix(),
			URL:       shareURL(cfg, token),
		})
	}
}

// shareURL builds the fully-qualified share link for token, preferring the
// Tailscale address (reachable from anywhere) over the LAN address.
func shareURL(cfg Config, token string) string {
	addr := cfg.Address
	if cfg.TailscaleAddress != "" {
		addr = cfg.TailscaleAddress
	}
	return "https://" + addr + "/v1/share/" + token
}

// --------- GET /v1/share/{token} (UNAUTHENTICATED — see package doc) ---------

func serveShareHandler(db *store.DB, ops *fsops.Ops, sharingEnabled func() bool) http.HandlerFunc {
	limiter := newShareIPLimiter(shareRateLimitAttempts, shareRateLimitWindow, shareRateLimitMaxIPs)
	return func(w http.ResponseWriter, r *http.Request) {
		if !limiter.allow(clientIP(r)) {
			writeError(w, http.StatusTooManyRequests, "RATE_LIMITED", "too many share requests, try again later")
			return
		}

		token := chi.URLParam(r, "token")
		hash := hashShareToken(token)

		// Look first, consume last: a link that is denied now (sharing off, minter revoked or stripped of the
		// right) must stay valid for when that changes, not be burned by the refusal.
		tok, err := db.GetShareToken(hash)
		if err != nil {
			writeInternal(w, "serve share", err)
			return
		}
		ok := tok != nil && time.Now().Before(tok.Expires.Add(time.Second))
		ownerID := ""
		if tok != nil {
			ownerID = tok.DeviceID
		}
		if ok && sharingEnabled != nil && !sharingEnabled() {
			ok = false
		}
		// The link is only as good as its minting device: re-check that
		// device still exists, is not revoked, may still share, and serve
		// through its current jail rather than the global one.
		serveOps := ops
		if ok && ownerID != "" {
			dev, derr := db.GetDeviceByID(ownerID)
			switch {
			case derr != nil:
				writeInternal(w, "serve share", derr)
				return
			case dev == nil || dev.Revoked || (!dev.ViaLogin && !(dev.CanShare && dev.CanDownload)):
				ok = false
			case dev.JailRoot != "":
				serveOps = ops.Jailed(dev.JailRoot)
			}
		}
		var path string
		if ok {
			// Single use: the atomic delete decides a race between two requests.
			var cok bool
			path, _, cok, err = db.ConsumeShareTokenFor(hash)
			if err != nil {
				writeInternal(w, "serve share", err)
				return
			}
			ok = cok
		}
		if !ok {
			// Don't distinguish "expired" from "never existed" (T1/T6).
			writeError(w, http.StatusNotFound, "NOT_FOUND", "share link not found or expired")
			return
		}

		// OpenFile re-validates the minted path against the agent's CURRENT
		// jail config and opens it through the rooted filesystem boundary.
		// The file may have been deleted/moved since mint (T6-adjacent).
		// O_NONBLOCK prevents a path swapped to a FIFO from pinning an HTTP
		// handler while Open waits for a writer. It has no effect for regular
		// files; validate the opened descriptor before sending any bytes.
		f, err := serveOps.OpenFile(path, os.O_RDONLY|syscall.O_NONBLOCK, 0)
		if err != nil {
			writeError(w, http.StatusNotFound, "NOT_FOUND", "share link not found or expired")
			return
		}
		defer f.Close()
		info, err := f.Stat()
		if err != nil || !info.Mode().IsRegular() {
			writeError(w, http.StatusNotFound, "NOT_FOUND", "share link not found or expired")
			return
		}
		if info.Size() > shareMaxFileSize {
			writeError(w, http.StatusRequestEntityTooLarge, "FILE_TOO_LARGE", "shared files may not exceed 500 MiB")
			return
		}
		setUntrustedFileResponseHeaders(w, info.Name())

		if err := db.LogShareServed(hash, clientIP(r)); err != nil {
			// ConsumeShareToken has already committed the one-time deletion.
			// Keep serving the file rather than making an audit outage consume
			// the link without delivering it; report the audit failure locally.
			log.Printf("share audit: record serve %s: %v", hash[:8], err)
		}

		// Bound ServeContent to the descriptor size observed above. A writer
		// may append to the same inode after Stat; a section reader prevents
		// that race from streaming more than the checked size/cap.
		content := io.NewSectionReader(f, 0, info.Size())
		http.ServeContent(w, r, info.Name(), info.ModTime(), content)
	}
}

type shareIPRateEntry struct {
	windowStart time.Time
	hits        int
}

// shareIPLimiter keeps a fixed-size, per-source-IP budget for the public
// single-use share endpoint. When the map is full, expired entries are
// removed; if it is still full, new source IPs are denied until a slot ages
// out. Existing IPs retain independent budgets and cannot be starved by one
// noisy peer, and attacker-controlled source churn cannot grow memory without
// bound.
type shareIPLimiter struct {
	mu          sync.Mutex
	maxAttempts int
	window      time.Duration
	maxIPs      int
	entries     map[string]shareIPRateEntry
}

func newShareIPLimiter(maxAttempts int, window time.Duration, maxIPs int) *shareIPLimiter {
	if maxIPs < 1 {
		maxIPs = 1
	}
	return &shareIPLimiter{
		maxAttempts: maxAttempts,
		window:      window,
		maxIPs:      maxIPs,
		entries:     make(map[string]shareIPRateEntry),
	}
}

func (l *shareIPLimiter) allow(ip string) bool {
	return l.allowAt(ip, time.Now())
}

func (l *shareIPLimiter) allowAt(ip string, now time.Time) bool {
	if ip == "" {
		ip = "<unknown>"
	}
	l.mu.Lock()
	defer l.mu.Unlock()

	entry, exists := l.entries[ip]
	if exists && now.Sub(entry.windowStart) < l.window {
		if entry.hits >= l.maxAttempts {
			return false
		}
		entry.hits++
		l.entries[ip] = entry
		return true
	}

	if len(l.entries) >= l.maxIPs {
		cutoff := now.Add(-l.window)
		for key, stale := range l.entries {
			if !stale.windowStart.After(cutoff) {
				delete(l.entries, key)
			}
		}
		if _, exists = l.entries[ip]; !exists && len(l.entries) >= l.maxIPs {
			return false
		}
	}
	l.entries[ip] = shareIPRateEntry{windowStart: now, hits: 1}
	return true
}

// --------- DELETE /v1/share/{tokenHash} (authenticated) ---------

func revokeShareHandler(db *store.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		hash := chi.URLParam(r, "tokenHash")
		if hash == "" {
			writeError(w, http.StatusBadRequest, "BAD_REQUEST", "tokenHash required")
			return
		}
		// Only the device that minted the link (or an admin) may revoke it —
		// otherwise any paired device can kill every other device's shares.
		// Non-owners get 404, so a foreign hash isn't confirmed (PR-03).
		t, err := db.GetShareToken(hash)
		if err != nil {
			writeInternal(w, "get share token", err)
			return
		}
		if t == nil || !callerOwnsShare(r, t) {
			writeError(w, http.StatusNotFound, "NOT_FOUND", "no such share")
			return
		}
		if err := db.DeleteShareToken(hash); err != nil {
			writeInternal(w, "delete share token", err)
			return
		}
		audit(db, r, store.AuditShareRevoked, t.Path, "")
		w.WriteHeader(http.StatusNoContent)
	}
}

// callerOwnsShare reports whether the authenticated device minted this share
// token, or is an admin device. Mirrors callerOwnsTransfer's rule: tokens with
// no recorded owner (minted before the device_id column) are admin-only.
func callerOwnsShare(r *http.Request, t *store.ShareToken) bool {
	d := deviceFromContext(r)
	if d == nil {
		return false
	}
	if t.DeviceID != "" && t.DeviceID == d.ID {
		return true
	}
	return isAdminDevice(d)
}

// --------- GET /v1/share (authenticated) ---------

func listSharesHandler(db *store.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// A non-admin sees only the links it minted; the full list names other
		// devices' shared paths (PR-03).
		caller := deviceFromContext(r)
		scope := ""
		if !isAdminDevice(caller) {
			// scope "" means "every token" — never let a non-admin reach that,
			// including the ID-less case.
			if caller == nil || caller.ID == "" {
				writeError(w, http.StatusForbidden, "FORBIDDEN", "device required")
				return
			}
			scope = caller.ID
		}
		tokens, err := db.ListShareTokens(scope)
		if err != nil {
			writeInternal(w, "list share tokens", err)
			return
		}
		out := make([]map[string]any, 0, len(tokens))
		for _, t := range tokens {
			out = append(out, map[string]any{
				"tokenHash": t.TokenHash,
				"path":      t.Path,
				"expiresAt": t.Expires.Unix(),
			})
		}
		writeJSON(w, http.StatusOK, out)
	}
}

// StartShareSweeper launches a background goroutine that deletes expired
// share tokens every shareSweepInterval (T6). It never stops — the agent
// process owns its lifetime, same as the mDNS advertisement in main.go.
func StartShareSweeper(db *store.DB) {
	go func() {
		ticker := time.NewTicker(shareSweepInterval)
		defer ticker.Stop()
		for range ticker.C {
			_, _ = db.SweepExpiredShareTokens()
		}
	}()
}
