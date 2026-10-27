# R1 — One-Time Share Link: Threat Model

**Status: IMPLEMENTED — R1 shipped; this doc now describes the built system.**
Written 2026-06-30 as the pre-build gate required by
`docs/next-waves-addendum.md`; updated 2026-07-15 once the feature landed.
The mitigations below are the ones in `agent/internal/server/share_handlers.go`
and `agent/internal/store/store.go` — keep them in step with the code, in the
same change (PR-72).

**Since the original draft:** share links are additionally scoped to their
minting device — only that device or an admin may list or revoke a link, and
links minted before the agent recorded owners are admin-only (PR-03). This
narrows T5/T9: one paired device can no longer enumerate or revoke another's
shares.

---

## What it does

The agent serves a single file via a short-lived, tokenized HTTPS URL.
The recipient fetches it once; the token then expires and is deleted.

---

## Trust boundary

Every other RFE feature stays inside Tailscale (WireGuard encrypted, device-authenticated).
R1 **breaks out of that boundary**: the share URL is reachable by anyone who has it,
over the public internet (or LAN, depending on how the agent is bound).

This is intentional and useful ("grab this file from my PC"), but it is the **only
RFE surface that is not Tailscale-gated**. Everything else below exists to make that
safe.

---

## Threat inventory

| # | Threat | Likelihood | Impact | Mitigation |
|---|--------|-----------|--------|------------|
| T1 | Link forwarded to unintended recipient | Medium | High | Token is single-use + expiring; once fetched it's gone |
| T2 | Brute-force token guessing | Low | High | Token = 32 bytes crypto/rand → 2^256 space; allow 10 requests/min per source IP. Keep at most 4096 IP buckets, pruning expired entries and returning 429 to new IPs while the table is full. |
| T3 | Path traversal via crafted share request | Low | Critical | Share token is bound to an absolute, jail-checked path at mint time; `/share/:token` never accepts a path parameter |
| T4 | Token leaked in server logs | Medium | Medium | Agent logs the token as `sha256(token)[:8]` only, never the raw value |
| T5 | Share feature enabled without user knowledge | Low | High | Host-level `"allowSharing": false` default; must be explicitly enabled in agent settings |
| T6 | Expired token still served (clock drift) | Low | Low | Tokens are deleted from DB on first serve OR on expiry sweep — whichever comes first |
| T7 | Serving a directory or special file instead of a regular file | Low | High | Reject non-regular files at mint; open with nonblocking semantics and verify the opened descriptor is still a regular file before serving |
| T8 | Large file exhausting agent bandwidth | Medium | Medium | Hard limit of 500 MiB per file at mint and serve; there is no setting to raise or remove the limit. If a file grows past the cap after mint, fetch returns 413 and consumes the one-time token. |
| T9 | No audit trail | Medium | Medium | Mint is returned only after the `share_log` row is written; serving writes the requester IP to SQLite. If the serve audit write fails after token consumption, the failure is written to the local process log and delivery proceeds so an audit outage does not silently consume the link without serving it. |

---

## Design decisions

### Token format
`crypto/rand` 32-byte random → hex-encoded 64-char string.
Stored in SQLite as SHA-256 hash; raw token only in the response to the minting caller.

### Expiry
Default: **15 minutes**. Max: 24 hours (agent setting). The app UI shows a countdown.
Expired tokens are deleted by a background goroutine sweeping every 5 minutes.

### Revoke
`DELETE /v1/share/{tokenHash}` (authenticated, bearer token required).
The app shows a "Revoke" button while the share is active.

### Host-level gate
`agent_settings.go` gets a new `AllowSharing bool` field (default `false`).
`GET /v1/share/mint` returns 403 if `allowSharing == false`.
The Flutter settings screen exposes a "Enable share links" toggle per host.

### Endpoint surface (OpenAPI changes required)
```
POST /v1/share/mint          → {token, expiresAt, url}        (authenticated)
GET  /v1/share/{token}       → file bytes                      (public, single-use)
DELETE /v1/share/{tokenHash} → 204                             (authenticated, revoke)
GET  /v1/share               → [{tokenHash, path, expiresAt}]  (authenticated, list active)
```
`GET /v1/share/:token` is the **only unauthenticated endpoint** in the entire agent.

### Audit log
New `share_log` table in `store.go`:
```sql
CREATE TABLE share_log (
  id          INTEGER PRIMARY KEY,
  token_hash  TEXT NOT NULL,
  path        TEXT NOT NULL,
  minted_at   INTEGER NOT NULL,   -- Unix timestamp
  expires_at  INTEGER NOT NULL,
  served_at   INTEGER,            -- NULL until fetched
  requester_ip TEXT               -- NULL for mint row
);
```

### Flutter UX
- Explorer: "Share link" in the file context menu (only if host has `allowSharing: true`)
- Shows: generated URL + copy button + expiry countdown + Revoke button
- Settings: per-host "Enable share links" toggle

---

## What is explicitly OUT OF SCOPE

- Password-protecting the link (adds complexity; expiry + single-use is the safety model)
- Link re-use (would require removing the single-use guarantee)  
- Folder shares (directories only — not a regular file; T7 blocks it)
- Analytics on who fetched the link (IP is logged, nothing more)

---

## Implementation pre-conditions (all met — kept as the build record)

1. This doc reviewed and approved by owner ✅
2. Agent `store.go` extended with `share_tokens` and `share_log` tables ✅
3. `agent_settings.go` has `AllowSharing bool` ✅
4. Rate-limit middleware confirmed reusable for `/share/` path ✅
5. OpenAPI updated in same commit as implementation ✅

---

## R1 was safe to build given the mitigations above, and shipped with them.
The critical invariant: **the only unauthenticated endpoint serves one file once, for at most 24 hours, only when the host owner has explicitly opted in.**
