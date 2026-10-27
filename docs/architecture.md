# Architecture

## Overview

Two components plus a shared contract:

- **`mobile/`** — React Native (Expo) Android app. All UI + client orchestration.
- **`agent/`** — Go host service on each Windows/macOS/Linux computer. Owns filesystem access, the
  transfer engine, search, thumbnails, settings, and the device/token store.
- **`agent-rs/`** — Rust sidecars the Go agent starts as child processes: `rfe-indexd` (search index, recents,
  live updates) and `rfe-thumbd` (sandboxed thumbnails). Go stays the shell and falls back to its own code.
- **`protocol/openapi.yaml`** — the REST contract both sides follow (source of truth).

The app talks to the agent over **HTTPS (HTTP/2) + TLS**. On a local network it can connect by
IP or hostname without a VPN. **Tailscale is optional** and provides a private remote route when
installed on both devices; without it, remote access requires another routable path. The agent
advertises `_rfe._tcp` over mDNS/DNS-SD, and Android can browse for IPv4 candidates from the
Add computer screen. QR pairing and manual addresses remain available. Discovery only supplies
an address; the user still verifies the TLS fingerprint independently and enters a pairing code.
The app can save a direct HTTPS hostname or IP as a final connection fallback. The PC owner must
configure DNS, router NAT, and firewall access; the app does not modify network settings. The
project currently has **no cloud relay or cloud database**.

## Key decisions

| Area | Decision |
|------|----------|
| Mobile framework | React Native (Expo, Expo Router, TypeScript) + a Kotlin Expo module |
| Backend | Custom Go host agent — single static binary, runs as a service |
| Connection routes | LAN, optional Tailscale, then a user-configured direct HTTPS address; no built-in relay |
| Agent discovery | Agent-side `_rfe._tcp` advertisement; Android app performs an explicit, bounded mDNS scan and lists IPv4 candidates |
| Transport security | TLS with a self-signed cert; enrollment pins a SHA-256 fingerprint obtained out of band |
| Pin storage | `HostStore` Keychain/Keystore secure storage is authoritative; the SharedPreferences copy is not a trust source |
| Storage | SQLite on the agent; app metadata locally and tokens/pins in Keychain/Keystore secure storage |
| Browser authentication | Embedded web companion uses a Secure, HttpOnly, SameSite=Strict session cookie and same-origin request checks; it does not store its bearer token in Web Storage |
| Host app launch | Current-user registrations: Windows AppsFolder, Linux XDG desktop, and macOS standard `.app` folders; per-device grants default off; launch by opaque ID only |

## Security model (summary)

1. TLS is used for all app-agent API traffic. New pairing requires a fingerprint from a trusted
   independent source (for example, the intended host's local display or console); the app checks
   the pin before sending pairing codes, passwords, or authenticated request data. Tailscale may
   add a private network route, but certificate pinning is independent of it. Direct HTTPS still
   uses the same pin; a TLS-terminating proxy with a different certificate is not supported.
2. Paired-device fingerprints and bearer tokens are stored in Keychain/Keystore secure storage.
   The secure-store fingerprint is authoritative; a missing or invalid value fails closed and is
   never recovered from the legacy SharedPreferences host record.
3. Device pairing (via `rfe pair`, QR or manual entry) issues a revocable bearer token.
   Per-agent authorization includes a root-path jail, optional read-only mode, device
   revocation/removal, and `/pair` rate limiting (10/min). Browse, download, upload, modify,
   delete, and share are independent per-device file-action grants. Existing devices retain
   their prior access during migration; new code-paired devices start browse-only; devices
   authenticated by the account password retain full file access. Owner provenance bypasses
   per-device file grants, while configured roots, global and per-device read-only, per-device
   jail, and the global share switch remain in force. App-catalog viewing and app launching
   remain separate per-device grants, default off; admin provenance does not bypass them.
   A new agent database is restricted to a dedicated `RFE Files` folder under the signed-in
   user's home by default; an empty global root list is an explicit unrestricted-access setting.
   Existing saved root policies are preserved during upgrades.
4. Strict path normalization + rooted filesystem operations enforce configured and per-device
   jails against traversal and symlink escape during file access. This does not fence mount points,
   Linux bind mounts, `/proc` special files, or Unix device files inside an allowed root; see the
   route/security matrix for platform verification limits. The mobile Files tab reads the
   authenticated `/settings` response to show the caller's effective roots. It opens restricted
   hosts at one of those roots and clamps bookmark navigation to the selected root, rather than
   attempting to list `/` outside the jail.
5. The app catalog shows visible user-facing entries from the agent's current-user OS catalogs.
   Each item carries a `launchable` flag; unsupported entries remain visible with Run disabled.
   Launch accepts only an opaque ID, re-resolves it before launch, rejects non-launchable items,
   does not accept client paths/commands/arguments, checks for an interactive desktop session,
   applies rate/concurrency limits, and audits the result. Catalog support is Windows AppsFolder,
   Linux XDG desktop entries, and macOS `.app` bundles in standard application folders; this is not
   an inventory of arbitrary executables or every installed package. macOS aliases and apps outside
   those folders are not included.

The agent has a persistent SQLite audit trail for account, device, share-link, app-launch, and
restart events (including pair/register/login, device changes, share creation/revocation, app
launch outcomes, and agent restart). The audit endpoint is admin-only. File operations are
deliberately not recorded in that trail.

The embedded browser companion receives its device credential in a `/v1`-scoped HttpOnly
session cookie rather than JavaScript-readable storage. It adds a custom request header, and
cookie authentication checks same-origin Fetch Metadata and any supplied `Origin`. Sign-out
clears this browser cookie but leaves the device paired; revocation remains an explicit device
management action. HttpOnly limits credential extraction by page scripts but cannot stop a live
same-origin script compromise from issuing requests, so the CSP and escaped React rendering are
still important controls. The browser's Ed25519 private key is stored as a non-extractable
IndexedDB `CryptoKey`, with an automatic one-time migration from the earlier localStorage format.
API responses use `Cache-Control: no-store` by default; thumbnails retain their explicit private
cache policy.

For the full route-by-route authentication and authorization inventory, including explicit gaps,
see [`security-route-matrix.md`](security-route-matrix.md). The current device model provides
administrator provenance, a per-device path jail, a read-only switch, separate app-view/app-
launch grants, and independent per-device browse, download, upload, modify, delete, and share
grants. New-file transfers require `upload`; small content writes and transfer sessions that
request overwrite require `modify`. Overwrite permission is re-checked for every chunk and at
upload completion.

## Transfers (the core engineering)

- **Upload:** resumable chunked sessions. Per-chunk + whole-file SHA-256; received-chunk bitmap in
  SQLite for resume. Server completion streams the verified open temp file through the current
  request's rooted filesystem operations into the destination. Overwrite uses a same-directory
  atomic rename; overwrite=false uses hard-link publication where available, with a rooted
  `O_EXCL` copy fallback on filesystems that do not support hard links. That fallback can expose
  partial content while copying, but never replaces an existing file. Chunks can upload in parallel.
- **Download:** HTTP Range requests; resume from last offset; optional parallel ranges.

See `../protocol/openapi.yaml` for the full API surface.

---

# Code map (living)

> **Purpose: read this instead of fan-out grepping.** It maps every file to its one
> responsibility so a session — or a dispatched sub-agent — jumps straight to the right file.
> **Keep it current:** when you add/move/split a file, update its row in the same commit.
> Line counts are rough size hints, not exact.

## App: `mobile/`

React Native (Expo, Expo Router, TypeScript), Android only. Routes live in `mobile/src/app/`; everything else is
outside it:

| Area | Where | Responsibility |
|------|-------|----------------|
| core | `src/core/{api,security,storage,models}` | The one pinned agent client (`api/agentClient.ts`), TLS pin and token storage, models, legacy Flutter data import (`storage/legacyImport.ts`). |
| design | `src/design/` | Lumen tokens and shared components. |
| features | `src/features/<name>/` | One folder per screen or domain: hosts, pairing, explorer, transfers, preview, search, settings, photoBackup, update, and so on. |
| state | `src/state/` | App state stores. |
| i18n | `src/i18n/` | Strings (`en.json`). |
| native | `mobile/modules/rfe-transport/` | Kotlin Expo module: pinned-TLS transport, uploads, MediaStore, FileProvider, package installer. |
| config | `mobile/plugins/withRfeAndroid.js`, `app.json` | Config plugin for the generated `android/` project (never committed). |

Port notes and gaps: `docs/rn-migration/ledger.md`. The old Flutter source is in git history at tag `v1.42.5`.

## Agent — `agent/`

| Package / file | Responsibility |
|----------------|----------------|
| `cmd/agent/main.go` | Daemon dispatcher + `runServe`; `defaultDataDir()` (`-data` > `$RFE_DATA_DIR` > `~/.rfe`). |
| `cmd/agent/admin.go` | Admin CLI subcommands: `pair`, `devices`, `revoke`, `remove`, `status`. |
| `cmd/agent/install{,_darwin,_linux,_windows}.go` | `install`/`uninstall` CLI: registers the agent as an auto-start service (systemd --user / launchd / a Windows equivalent) — no root/admin required. |
| `internal/server/server.go` | chi router wiring — where every route is mounted. |
| `internal/server/auth.go` | Bearer-token auth middleware + per-device authorization. |
| `internal/server/login.go` | Username/password login — a second way (besides `/v1/pair`) to obtain a device token. |
| `internal/server/register.go` | Account registration; self-gated since it's reachable before any account exists. |
| `internal/server/challenge.go` | Nonce mint/verify for device-signature proof-of-possession (pairs with `security/device_identity.go`). Accepts the certificate-bound v2 proof (`security.DeviceProofMessageV2`) and, for older clients, a bare-nonce signature. |
| `internal/server/fshandlers.go` | List/read/create/delete/move/rename file endpoints. |
| `internal/server/archive_handler.go` | Compress/extract endpoints (fronts `fsops/archive.go`). |
| `internal/server/chmod_handler.go` | chmod endpoint. |
| `internal/server/dupfinder_handler.go` | Batch-checksum endpoint backing the app's duplicate finder. |
| `internal/server/recent.go` | Recent-files endpoint — jail-scoped live recursive walks with a five-second cache for complete results, not a persistent index. |
| `internal/server/transferhandlers.go` | Upload-session + chunk PUT + download-range endpoints. |
| `internal/server/search.go` | Search endpoint (indexed fast path, bounded recursive fallback before the first build). |
| `internal/server/search_noindex.go`, `search_scope.go` | Search backend when there is no sidecar (live walks) and the root-scope helpers; the in-process index was retired in favour of `rfe-indexd`. |
| `internal/server/thumb.go` | Thumbnail endpoint. |
| `internal/server/settings_handlers.go` | Live-mutable agent settings endpoints. |
| `internal/server/apps_handlers.go` + `apps_{linux,windows,darwin,other}.go` | Per-device app catalog/launch routes and OS-specific current-user app inventory/launch adapters. |
| `internal/server/update_handlers.go` | `/v1/app/latest` + `/v1/app/download` (serves APKs from `updates/`). |
| `internal/server/pair.go` | Pairing endpoint (consumes a DB code, issues a token). |
| `internal/server/ratelimit.go` | Pairing, app-catalog, and app-launch rate limiters. |
| `internal/server/share_handlers.go` | One-time share-link mint/serve/revoke/list — see `docs/r1-share-link-threat-model.md`. |
| `internal/server/sse_handler.go` | Server-sent-events endpoint — server-side half of the PR-59 finding (client parser lives in the frozen `app/lib` tree). |
| `internal/server/status_handlers.go` + `status_disk_{unix,windows}.go` | `/v1/health`/status endpoint, incl. OS-specific disk-space lookup. |
| `internal/server/metrics_{linux,other}.go` | CPU/RAM for `/metrics` — read from `/proc` on Linux, zero on other OSes. |
| `internal/server/agent_control.go` + `agent_control_{linux,other}.go` | `/agent/restart` handler; OS-specific process restart. |
| `internal/server/throughput.go` | Process-lifetime cumulative rx/tx byte counters exposed via metrics. |
| `internal/server/wol_handler.go` | Wake-on-LAN send endpoint. |
| `internal/server/webdata_handlers.go` | List endpoints (+ user removal) backing the web companion's Transfers/Users/Logs pages. |
| `internal/fsops/fsops.go` | Core listing + file ops, implemented through `jail.go`'s request-scoped rooted paths and open handles. |
| `internal/fsops/jail.go` | **Path jail + rooted operations** (normalization, symlink defense, descriptor-relative access) — opens the active root and confines server filesystem operations to it. `Resolve` remains a path validation API, not a race-resistant handle. |
| `internal/fsops/archive.go` | Compress (zip) / Extract (zip, tar.gz), both routed through `Resolve`. |
| `internal/fsops/trash.go` | Move-to-trash / restore, XDG Trash-layout (`files/` + `info/*.trashinfo`). |
| `internal/fsops/{drives_*,birthtime_*}.go` | OS-specific drive enumeration + file birthtime. |
| `internal/transfer/transfer.go` | **Resumable chunked transfer engine** — SHA-256, received-chunk bitmap, and server-supplied publication callback. Its legacy direct API remains for package callers; server writes publish through rooted `fsops`. |
| `internal/transfer/throttle.go` | Rate-limited `io.ReadSeeker` wrapper for bandwidth-capped transfers. |
| `internal/thumbs/thumbs.go` | Thumbnail generation. |
| `internal/pairing/pairing.go` | DB-backed pairing codes (`Mint`/`Consume`). |
| `internal/security/tls.go` | Self-signed cert generation + SHA-256 fingerprint. |
| `internal/security/device_identity.go` | Per-device Ed25519 identity keypair, generated on first use (pairs with `server/challenge.go`). |
| `internal/security/password.go` | bcrypt password hashing for account login (bearer tokens elsewhere use SHA-256 — different threat model). |
| `internal/settings/settings.go` | Agent settings model + load/save. |
| `internal/store/store.go` | SQLite store: devices, tokens, pairing codes, transfer bitmaps. Busy-timeout DSN for daemon+CLI concurrency. |
| `internal/updates/updates.go` | Update-channel management (the `updates/` dir). |
| `internal/netinfo/netinfo.go` | LAN + Tailscale address detection. |
| `internal/mdns/mdns.go` | Agent-side mDNS/DNS-SD advertisement (`_rfe._tcp`) consumed by Android's explicit local-network discovery scan. |
| `internal/webui/` (`webui.go`, `web/`, `dist/`) | Browser-based web companion (control/status/settings/file-browsing), embedded static bundle served at `/`. The Vite + React + TypeScript source lives in `web/`; run `npm run build` there to generate `dist/`, which Go embeds into the agent binary. Edit `web/src/` and rebuild; treat `dist/` as generated output. |

### Rust sidecars — `agent-rs/`

Protocol (frames, ops, sandbox): `protocol/sidecar.md`. Go authorizes everything; a sidecar only does heavy work
and every path it returns is re-checked against the jail.

| Path | Responsibility |
|------|----------------|
| `agent-rs/crates/rfe-proto` | Frame codec, hello, error codes, `release_version` (build-time `RFE_RELEASE_VERSION`). |
| `agent-rs/crates/rfe-indexd` | Rooted parallel walk, chunked index, ordered query scan, recents, notify watcher + debounced applier. |
| `agent-rs/crates/rfe-thumbd` | JPEG/PNG/GIF/WebP decode with limits, EXIF orientation, resize, JPEG encode; seccomp (Linux) / job object (Windows). |
| `agent/internal/sidecar/` | Go client (multiplexed calls, cancel) and supervisor (backoff, breaker, ping). |
| `agent/internal/server/sidecar_index.go` | Index backend over rfe-indexd, recents walker, thumbnail hookup. |
| `agent/internal/server/sidecar_locate.go` | Finds sidecars, verifies `rfe-sidecars.txt` (sha256 + version), `RFE_SIDECARS` switch, status report. |
| `agent/internal/thumbs/remote.go` | `Remote` interface and sidecar renderer with Go fallback. |
| `tools/package-agent.sh` | Builds the agent and archives it with the sidecars and manifest (used by `agent-release.yml`). |

## Test → source map (used by `scripts/test-affected.sh`)

Go tests sit beside their package (`*_test.go`), so a changed Go file maps to `go test` on its
own directory. For `mobile/`, run the affected jest files (`npx jest <path>`); trust CI for the rest. A change to `protocol/openapi.yaml` is
treated as affecting **both** sides.
