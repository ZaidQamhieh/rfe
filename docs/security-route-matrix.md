# Agent security route matrix

Reviewed against `agent/internal/server/server.go` and its handlers on 2026-09-28.
This is a code review map, not a claim that the host is ready for public exposure.

## Authentication classes

| Class | Meaning |
| --- | --- |
| Public bootstrap | No bearer token. Health probing, identity challenge, pairing/account bootstrap, browser-session logout, and the one-time share fetch are mounted this way. Challenge/pair/login have per-source rate limits with a 4,096-key cap and shared overflow window; registration has a global rate limit. Pair/register/login bodies are capped at 1 MiB. |
| Browser session | Embedded web companion uses a `Secure`, `HttpOnly`, `SameSite=Strict` cookie scoped to `/v1`, plus a custom same-origin request header. The token is omitted from the browser's auth JSON response. The browser relies on normal HTTPS certificate validation; JavaScript cannot pin the server leaf certificate. |
| Paired device | Valid, non-revoked bearer token. The request gets the device's jail and read-only state in middleware; file routes also check required per-device capabilities. |
| Owner device | A paired device minted through password login/registration (`via_login`). Owner-only handlers use `adminOnly` or a handler-level `isAdminDevice` check. File capability gates are bypassed for owner devices; global policy and effective path roots remain in force. |
| File capability | One of `browse`, `download`, `upload`, `modify`, `delete`, or `share`. Missing grants return 403 `CAPABILITY_DENIED`; grants never confer owner/admin privileges. |
| App grant | Paired device plus the stored `viewApps` and, for launch, `launchApps` flags. Owner provenance does not implicitly grant these flags. |
| Transfer/share owner | The device that created the session/link, or an owner device. Other devices receive the same 404 as an unknown ID. |

## Routes and gates

| Route(s) | Gate and scope | Sensitive behavior / review note |
| --- | --- | --- |
| `GET /v1/health` | Public minimal response; valid bearer or same-origin browser session may receive additional host details | Does not expose topology details to an unauthenticated caller. |
| `POST /v1/auth/challenge`, `/pair`, `/register`, `/login` | Public bootstrap; nonce proof is single-use; all four are rate-limited; per-source limiter state is capped at 4,096 keys, and excess unseen source addresses share an overflow window. Pair/register/login bodies are capped at 1 MiB. | The native app pins a fingerprint obtained independently before sending a code or credentials. The embedded browser has no leaf-pin check: browser login/pair/register must use a browser-trusted HTTPS certificate. Do not bypass a self-signed-certificate warning on an untrusted network. Register requires the one-time code and creates the sole login account; login/register devices carry owner privileges. |
| `POST /v1/auth/logout` | Public cookie clearing endpoint | Clears the embedded browser's session cookie and ends the session server-side (login session deleted, paired browser revoked). Native clients continue using bearer tokens. |
| `GET /v1/share/{token}` | Public, single-use, expiring random token, per-IP rate-limited | The handler rechecks the current global path jail, opens and validates a regular file, limits the response to the checked size, and sets download-safe browser headers. This is the only intentionally public content route. |
| `GET /v1/status` | Any paired device | Returns agent version, uptime, platform, and data-volume capacity. |
| `GET /v1/app/latest`, `GET /v1/app/download` | Any paired device | Authenticated agent-update metadata and APK; these routes do not read user-selected filesystem paths and do not require file-download capability. |
| `GET /v1/transfers/list`, `GET/DELETE /v1/transfers/{id}` | Paired device; list and individual rows are scoped to the creator, with owner access | Foreign IDs are hidden as 404. Upload creation/chunk/completion additionally require `upload` and writable effective policy. |
| `GET /v1/apps` | `viewApps` grant | Catalog is separate from launch permission. |
| `POST /v1/apps/{id}/launch` | Both `viewApps` and `launchApps`; opaque ID re-resolved from the current native catalog; rate and concurrency limits; audit event | No client command, executable path, or arguments are accepted. GUI session and platform checks apply. |
| `GET /v1/metrics`, `/users`, `/logs`, `/audit`; `DELETE /v1/users/{username}`; `POST /v1/agent/restart` | Owner device (`ViaLogin`) via `adminOnly` | Host-wide telemetry, account, audit, log, and process-control data/actions are not available to ordinary paired devices. |
| `GET /v1/settings` | Any paired device; owner sees global settings, other devices receive effective scoped settings | Non-owner response omits unrelated global paths and hides the photo backup path unless it falls within the device's effective scope. |
| `PATCH /v1/settings`, `/settings/bandwidth` read/write, `POST /v1/pairing/generate` | Owner device (handler-level checks) | Changes to global policy and creation of pairing codes are owner-only. |
| `GET /v1/devices` | Owner sees all devices; other paired devices see only a reduced self entry | The reduced response omits access-control fields and other devices. |
| `PATCH /v1/devices/{id}` | Owner device | Sets another device's jail/read-only, app, and file-action grants; checks target jail against global roots. Disabling `share` deletes active tokens minted by that device. |
| `DELETE /v1/devices/{id}` | Device can revoke/remove itself; owner can manage another device | Revocation is checked by auth middleware on the next request. `?purge=true` removes the database row. |
| `POST /v1/wol` | Paired, non-read-only device | Read-only devices cannot send Wake-on-LAN. |
| `POST /v1/share/mint` | `share` + `browse`, global sharing enabled, and an in-jail regular file; owner bypasses the grants | Share tokens are hashed at rest and audited. Disabling a device's share grant deletes its active links. Public tokens remain one-use bearer credentials until used or revoked. |
| `GET /v1/share`, `DELETE /v1/share/{tokenHash}` | Paired device; list/revoke are owner-scoped | Devices can still inspect/revoke existing links after the share-mint grant is removed. |
| `GET /v1/system/drives`, `/search`, `/fs`, `/fs/meta`, `/fs/archive`, `/fs/recent`, `GET /v1/trash` | `browse`; filesystem paths are constrained by configured and per-device roots when present | Directory, metadata, search, archive-entry and trash discovery. Without configured roots or a per-device jail, the agent account's OS-level accessible paths remain in scope. `/fs/recent` still walks the full tree (15s budget), with at most two scans active per process; excess requests receive `429 RECENT_BUSY`. |
| `GET /v1/thumb`, `/fs/checksum`, `POST /v1/fs/checksums`, `GET /v1/content` | `download`; path reads constrained by the effective jail | Thumbnails/checksums are byte-derived reads. `/app/latest` and `/app/download` are authenticated agent-update routes, not user-file downloads. |
| `PUT /v1/content` | `modify`, writable effective policy, and effective jail | Small text writes can create or replace content, so they use the same grant as other file modifications. |
| `POST /v1/transfers`, `PUT /v1/transfers/{id}/chunks/{n}`, `POST /v1/transfers/{id}/complete` | `upload`, writable effective policy, and effective jail; `modify` is also required throughout sessions whose overwrite flag is true | Overwrite permission is checked when opening the session, for each chunk, and again at completion, so a later grant removal blocks publication. |
| `POST /v1/fs/folder`, `/fs/file`, `/fs/rename`, `/fs/copy`, `/fs/compress`, `/fs/extract`, `/fs/chmod`, `POST /v1/trash/restore` | `modify`, writable effective policy, and effective jail | Copy is modify-only; restore is classified as modify. Batch handlers can return per-item errors after route authorization. |
| `POST /v1/fs/move` | `modify` + `delete`, writable effective policy, and effective jail | Moving changes the destination and removes the source. |
| `DELETE /v1/fs`, `/v1/trash` | `delete`, writable effective policy, and effective jail for user paths | Includes filesystem deletion and empty-trash. With a device jail, empty-trash filters entries by recorded origin; an unjailed device can empty the shared trash store. |

## Authorization coverage and residual limits

Tests include route-level read-only and per-device file-capability matrices, migration/default
tests, owner-vs-device settings/device-management tests, app grant checks, and transfer/share
ownership checks. This route map is hand-maintained alongside route registration.

The migration preserves prior access for existing devices: browse, download, and share remain
enabled; upload, modify, and delete remain enabled only for devices that were not read-only.
Newly paired devices start browse-only. Login/register owner devices retain full file access and
bypass per-device file grants; configured roots, global/per-device read-only, and per-device jail
remain effective. Share links are public one-use bearer credentials; disabling a device's share
grant deletes its outstanding tokens, but changing its jail does not retroactively change links
already minted.

Configured and per-device jails use rooted filesystem handles for the server's file operations,
recursive walks, thumbnail reads, and upload publication; upload completion rechecks the current
request's effective jail before publishing. `os.Root` prevents symlink traversal outside the opened
root, but it does not block mount-point or Linux bind-mount traversal, `/proc` special files, or
Unix device files. Linux adversarial symlink-race tests run locally; Windows and macOS server tests
were cross-compiled but not runtime-tested in this environment. When the target filesystem does
not support hard links, overwrite=false upload publication uses rooted `O_EXCL` copying: it never
replaces an existing destination, but the destination can be visible before the full copy finishes.

`GET /v1/fs/recent` scans its selected tree for up to 15 seconds on a cache miss. A complete
result is reused for up to five seconds for the same effective roots and limit; partial results
are never cached. The handler admits at most two walks concurrently per process; additional
uncached requests receive `429 RECENT_BUSY` with `Retry-After: 1`. The endpoint still needs a
full-tree refresh after cache expiry.

## Transport and reachability

The agent serves HTTPS only; the main listener defaults to port 8765 and a best-effort HTTPS
listener may also bind port 443. There is no plaintext HTTP mode. The mobile app pins the agent
certificate independently of whether the address is LAN, Tailscale, or owner-configured direct
HTTPS. The application does not configure router forwarding, DNS, firewall rules, or a relay.
Direct internet reachability therefore remains an explicit host-owner network setup.

The embedded web companion does not persist its bearer token in Web Storage. Login, pairing,
and registration set a session cookie with `Secure`, `HttpOnly`, `SameSite=Strict`, and a `/v1`
path; cookie-authenticated requests also require the companion's custom header and same-origin
Fetch Metadata / `Origin` checks. The cookie is a browser convenience for the existing device
token and is cleared on sign-out; it does not revoke the device. `HttpOnly` prevents page scripts
from reading the token, but an active same-origin script compromise could still issue requests
while the session is open. The companion's CSP and React's escaped text rendering remain part of
that defense-in-depth boundary. Browser device private keys are migrated from legacy Web Storage
to non-extractable Ed25519 `CryptoKey` objects in IndexedDB. API responses default to `no-store`;
the thumbnail handler keeps its explicit private cache policy.

Browser authentication still depends on the browser validating the HTTPS certificate: unlike
the native app, page JavaScript cannot inspect and pin the peer certificate. The default agent
certificate is self-signed, so browser access over an untrusted network requires an owner-managed
browser-trusted certificate and a correct TLS proxy configuration.

The shared JSON decoder enforces a 1 MiB request limit and requires a single JSON document. The
public `/pair`, `/register`, and `/login` handlers use it before parsing request bodies. This review
did not run tests; regression coverage should assert oversized and concatenated JSON bodies are
rejected for each of those routes.

For the status of findings carried over from the July production-readiness audit, see
[`audit-reconciliation.md`](audit-reconciliation.md). The original aggregate count is not treated
as closed because its itemized report was unavailable for this review.
