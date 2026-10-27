# Next-Waves Addendum — RFE

**Status: PLANNED, NOT STARTED.** Written 2026-06-13. Companion to
`feature-roadmap.md`, `ui-redesign-plan.md`, and `FUTURE_FEATURES.md` (the
latter lives in `~/Desktop/NEXT WAVE/`). This doc captures **net-new** features
discovered by gap analysis — none of them duplicate the existing roadmap.

Effort: S (≤1 agent dispatch) · M (2–3) · L (multi-session) · XL (own plan doc).
Impact: ★ to ★★★ for a personal phone↔PC tool over Tailscale.

These slot **after** the committed work. Do not reorder the existing plan:
finish UI **D → E → F**, then the CD + auto-update bundle (`FUTURE_FEATURES.md`),
then `feature-roadmap.md` Tier 1. The waves below come after Tier 1.

---

## Already spoken for (NOT repeated here)

File visibility, share-to-PC upload, zip/unzip, storage insights, trash,
recents, streaming previews, biometric lock, per-device read-only, background
transfers, SSE events, photo backup, mDNS, offline pins, PC↔PC copy,
widgets/QS tile, app shortcuts, audit log, dual-pane, QR pairing, Windows
drives, CD/auto-update, dynamic color, a11y, swipe-between previews.

---

## Wave 0 — Settings architecture: app defaults + per-device overrides (DO FIRST)

**Effort M · Impact ★★★ · owner-requested · foundational (precedes any wave
that adds a new setting).**

**The problem (owner-flagged):** settings are scattered and mostly per-PC.
File-visibility is global, but view mode / grid density / sort are persisted
*per host*, and there is no single "app settings" surface. Result: you
configure the same preference once for every PC. That's backwards — there
should be **one general app setting**, and per-device settings should only be
deliberate exceptions.

### The model — two tiers with explicit inheritance
- **App Defaults**: one source of truth, the "general settings" screen.
- **Device Overrides**: each overridable setting on a device is either
  **"Use app default"** (the default state) or **"Override"** with its own
  value. Absence of an override = inherit. No silent per-host divergence.
- A typed **`SettingsResolver`** returns the effective value:
  `deviceOverride ?? appDefault ?? hardcodedFallback`.

### Settings catalog, classified
- **App-global only** (no per-device override makes sense): theme/accent,
  language + RTL (O1), biometric lock (Tier 1 #7), notification prefs (L1),
  update channel.
- **Global default + optional per-device override:** view mode (list/grid),
  grid density, default sort + direction, file-visibility rules, transfer
  policy (Wi-Fi-only / throttle, J2), confirm-before-delete.
- **Per-device only** (inherently device-scoped): active address (LAN/Tailscale),
  read-only flag (Tier 1 #8), favorites/pins, per-host accent/tag (Q3),
  agent-specific bits.

### Storage & migration
- Keep SharedPreferences. App defaults under `app.*`; device overrides under
  `host.<id>.<key>` **only when explicitly set** (key absent = inherit).
- **One-time migration on upgrade:** existing per-host values that equal the old
  implicit default collapse into the app default; genuine divergences become
  explicit overrides. Never lose a user's current behavior silently.

### UI
- **Settings screen = App Defaults** — this is the "general" surface the owner
  is asking for; everything that can be global lives here.
- **Per-device settings** show each overridable row with a `Use app default (X)`
  vs `Override` control, plus a **"Reset to app defaults"** button that clears
  all overrides for that host.

### Tests
Resolver precedence (override > default > fallback), absence = inherit,
migration (match-default collapses, divergence → override), reset clears
overrides, persistence across restart (mock SharedPreferences).

**Why first:** later waves (Q theming, J2 bandwidth, L notifications, file
visibility tweaks) all add settings — build them on the two-tier model from the
start instead of retrofitting. This is the settings analogue of UI Wave A.

---

## Wave G — Core file-manager ops the redesign still lacks

The UI waves restyle the explorer but leave real file-manager *capabilities*
on the table.

| # | Feature | Effort | Impact | Why it's a gap |
|---|---|---|---|---|
| G1 | **In-app text/code editor** — edit small text files, save back via `PUT /v1/content` (size-capped, optimistic-lock on mtime) | M | ★★★ | Wave E keeps text preview read-only and rules out highlighting. Editing a config / `.env` / note on the PC from the phone is the standout "away from my desk" use case. **Lead feature of this wave.** |
| G2 | **Persistent cut/copy/paste clipboard** — survives navigation, multi-folder, multi-select | M | ★★ | Today move/copy is a one-shot destination dialog. A clipboard you fill in folder A and paste in folder B is the real file-manager mental model. |
| G3 | **Write-conflict resolution** — overwrite / skip / keep-both on upload & copy collisions | S–M | ★★★ | No defined collision behavior today → a correctness + data-loss gap, not a nicety. |
| G4 | **New empty file + duplicate-in-place** | S | ★ | `create_menu` only does New Folder. |
| G5 | **Batch / pattern rename** | S–M | ★ | Power feature; pairs with selection mode. |

**Agent notes:** G1 needs an agent endpoint (`PUT /v1/content`, jailed,
size-capped) + OpenAPI contract in the same commit. G2–G5 are client-side over
existing move/copy/create APIs. G3's collision check can be a pre-flight `stat`.

## Wave H — Trust & access control

The June 2026 code review (`project-rfe-code-review-2026-06`) flagged
token/pairing weaknesses. These productize the fixes.

| # | Feature | Effort | Impact | Why it's a gap |
|---|---|---|---|---|
| H1 | **Active-sessions view + remote revocation** — list paired tokens (last-seen, address, version), "revoke now" | M | ★★★ | Per-device read-only is planned, but there's no way to *kill* a lost phone's access. A token grants full PC read/write — this is the missing safety valve. |
| H2 | **Per-token path jail** — scope a device to a subtree (e.g. only `~/Shared`) | M | ★★ | Server already jails to a root; make the root per-token. Lets you hand a device limited access instead of the whole filesystem. |
| H3 | **Post-transfer integrity check** — compare agent hash after copy, show verified check | S | ★★ | Agent already hashes for dedupe; surfacing it closes the "did it arrive intact" loop, esp. over cellular. |

**Agent notes:** H1/H2 are agent-led (token store gains `revoked`, `jailRoot`,
`lastSeen` fields; middleware enforces). Additive, backward-compatible, bump
agent minor version.

## Wave I — Multi-host & connectivity

The app assumes one host at a time and an always-live Tailscale path.

| # | Feature | Effort | Impact | Why it's a gap |
|---|---|---|---|---|
| I1 | **Cross-host unified search** — fan search to all paired PCs, merged results grouped by host | M–L | ★★ | Search is single-host. "Is that file on the laptop or the desktop?" is a natural multi-PC question. |
| I2 | **Wake-on-LAN** — send a magic packet to power on a sleeping host before browsing | S | ★★ | An asleep PC is just "offline" today. Pairs with the dashboard online/offline dot. |
| I3 | **Connection diagnostics + auto LAN↔Tailscale switch** | S–M | ★★ | Code review: *"address-fallback never returns to LAN."* Turn that bug into a visible, self-healing connection-quality indicator. |
| I4 | **Agent install / auto-start helper + self-report** (uptime, version, free space) | M | ★ | Lowers host-setup friction; feeds the dashboard gauges. |

## Wave J — Automation & organization (horizon)

| # | Feature | Effort | Impact | Notes |
|---|---|---|---|---|
| J1 | **Scheduled / rule-based folder sync** — "auto-pull new files in PC folder X" | L | ★★ | Generalizes the photo-backup engine to arbitrary folders. Hard-depends on background transfers (Tier 2 #9). |
| J2 | **Global bandwidth controls** — Wi-Fi-only, throttle, cellular guard | S | ★★ | Photo backup has Wi-Fi/charging rules — promote them to an app-wide transfer policy. |
| J3 | **File bookmarks + client-side tags/labels** + tag-filter view | M | ★ | Favorites only pin folders; tagging individual files adds organization, no server change. |
| J4 | **Duplicate finder** — hash-based "reclaim space" | M | ★ | Reuses the agent's existing hashing; natural companion to Storage Insights (Tier 1 #3). |
| J5 | **Archive peek** — browse inside a zip without extracting | M | ★ | Direct extension of the planned zip/unzip feature. |

## Wave K — Media & document experience

Wave E only handles image / pdf / text / video viewers and explicitly punts
highlighting. Real consumption of media and documents is still thin.

| # | Feature | Effort | Impact | Why it's a gap |
|---|---|---|---|---|
| K1 | **Real audio player** — folder-as-queue, playlist, album art, scrubbing, background-safe playback | M | ★★ | Tier 1 #6 only promises a mini-player *bar*. A proper player turns the phone into a remote jukebox for the PC's music library. |
| K2 | **Image gallery mode** — swipe gallery, pinch-zoom, EXIF panel, geotag → map | S–M | ★★ | Wave E adds swipe-between for previews generically; a dedicated photo experience (metadata, map, slideshow) is a different surface and pairs with photo backup. |
| K3 | **Office/Markdown rendering** — render `.md`, and `.docx/.xlsx/.pptx` (or thumbnail-via-agent) read-only | M | ★ | Office files are opaque today. Even a server-rendered thumbnail/preview beats "download to open elsewhere." |
| K4 | **Video player polish** — playback speed, external subtitle track, resume position | S | ★ | Builds on the streaming-preview work (Tier 1 #6). |
| K5 | **"Open with / share to app"** — hand a downloaded file to any Android app | S | ★★ | The escape hatch for any file type the app can't render itself; pairs with share-to-PC as the reverse direction. |

## Wave L — Notifications & ambient awareness

The app is pull-based and silent. Make it tell you things.

| # | Feature | Effort | Impact | Why it's a gap |
|---|---|---|---|---|
| L1 | **Notification preferences center** — one settings surface for transfer, host, and sync notifications | S | ★★ | Background transfers (Tier 2 #9) ship notifications but there's no place to govern them. |
| L2 | **Host low-disk alerts** — push when a paired PC's free space crosses a threshold | S–M | ★★ | Storage insights (Tier 1 #3) is pull-only; a proactive warning is the payoff. |
| L3 | **New-files-on-host notification** — opt-in per watched folder (rides the SSE events channel) | M | ★ | Turns the events channel (Tier 2 #10) into something you feel, not just refresh-elimination. |
| L4 | **Weekly storage digest** — optional summary notification of space trends per host | S | ★ | Cheap engagement; reuses insights aggregation. |

## Wave M — Search & discovery power

Search v2 redesigned the screen; its *capabilities* are still basic.

| # | Feature | Effort | Impact | Why it's a gap |
|---|---|---|---|---|
| M1 | **Advanced filters** — date-range, size-range, type facets in the filters sheet | S–M | ★★ | The sheet currently only does include-hidden + categories. Faceting is the natural next layer. |
| M2 | **Saved searches + search history** | S | ★ | Re-running "big videos older than a year" shouldn't mean re-typing it. |
| M3 | **Glob / regex name matching** (opt-in toggle) | S | ★ | Power-user precision over substring match. |
| M4 | **Command palette / quick-jump** — fuzzy global launcher (host, recent path, action) | M | ★★ | Pairs with the tablet/keyboard story in UI Wave F; fastest path to anywhere. |

## Wave N — Data safety & app-state portability

If the phone is lost or reinstalled, all pairing/config is gone today.

| # | Feature | Effort | Impact | Why it's a gap |
|---|---|---|---|---|
| N1 | **Export / import app config** — hosts, tokens (encrypted), favorites, visibility rules to an encrypted blob | M | ★★★ | Re-pairing every PC after a reinstall is painful. There's no backup of the app's own state — a real continuity gap. |
| N2 | **Download cache management** — view cache size, clear, pin items against eviction | S | ★★ | Streaming/offline pins will accrete cache with no UI to govern it. |
| N3 | **Diagnostics / log export** — capture recent client logs + a redacted connection report to share when something breaks | S | ★ | Makes "it didn't work" debuggable without a wired session. |
| N4 | **Transfer journal viewer** — history of completed/failed transfers with details | S | ★ | The transfer engine is resumable/journaled; expose the journal as an auditable history. |

## Wave O — Onboarding, help & localization

| # | Feature | Effort | Impact | Why it's a gap |
|---|---|---|---|---|
| O1 | **Arabic localization + RTL** — i18n framework, Arabic strings, right-to-left layout audit | M | ★★ | The owner is Arabic-first (Birzeit). RTL is a genuine reach + correctness item, not cosmetic, and forces a layout-direction audit that benefits a11y too. |
| O2 | **First-run onboarding** — guided pairing (QR), permission rationale, a "what is this" tour | S–M | ★★ | Pairing/permissions are dropped on the user cold today. |
| O3 | **In-app help + changelog viewer** — contextual tips, "what's new" after OTA updates | S | ★ | Updates land via OTA with no surfaced release notes; a changelog closes that loop. |

## Wave P — Desktop reach & power-user (horizon)

| # | Feature | Effort | Impact | Notes |
|---|---|---|---|---|
| P1 | **Web companion** — browse a paired agent from a browser (the agent is already an HTTP server) | XL | ★★ | Genuinely novel: the same TOFU-pinned API, read-only first. Its own plan doc. |
| P2 | **External-keyboard shortcuts** — copy/paste/delete/search/navigate via hardware keys (tablet) | S | ★ | Pairs with UI Wave F's tablet two-pane. |
| P3 | **Android intents / Tasker hooks** — expose "upload to PC", "open host" as system intents for automation | M | ★ | Lets power users script the app without an in-app rules engine. |

## Wave Q — Theming & personalization

Distinct from UI Wave F (which only does dynamic color + light/dark). This is
user-driven customization.

| # | Feature | Effort | Impact | Why it's a gap |
|---|---|---|---|---|
| Q1 | **Custom accent picker + theme presets** — choose accent / preset palettes when not using Material You | S–M | ★ | Wave F derives color from the wallpaper; an explicit picker covers users who want a fixed brand look. |
| Q2 | **AMOLED / true-black theme** | S | ★ | Cheap battery + aesthetic win on the owner's Samsung OLED. |
| Q3 | **Per-host & per-folder color/icon tags** | M | ★ | Builds on per-host accent (roadmap UX idea) and J3 tags; makes multiple PCs and key folders instantly recognizable. |
| Q4 | **Reorderable dashboard** — drag host cards, pin a default host | S | ★ | The host list is fixed-order today. |
| Q5 | **Alternate app icons** | S | ★ | Low-effort personalization. |

## Wave R — Outbound sharing (horizon, security-gated)

Sending files *out* of the trust boundary. Powerful but adds attack surface —
every item here is **opt-in, off by default, and audited** (ties to H1/H-audit).

| # | Feature | Effort | Impact | Why it's a gap / caveat |
|---|---|---|---|---|
| R1 | **One-time share link** — agent serves a single file via a short-lived, tokenized URL | M | ★★ | Genuinely useful ("grab this from the PC"), but it exposes a path outside Tailscale. Must be: explicit per-share, expiring, revocable, logged. Gate behind a host-level "allow sharing" switch. |
| R2 | **Device-to-device send** — phone→phone brokered by the shared PC (extends PC↔PC copy) | M | ★ | Reuses the chunk protocol; both devices already trust the host. Stays inside the trust boundary, unlike R1. |
| R3 | **QR hand-off** — show a QR another paired device scans to fetch a file | S | ★ | LAN/Tailscale-internal, no public exposure. |

**Note:** R1 is the only item in the whole addendum that touches the network
boundary — spec its threat model explicitly before building. R2/R3 are safe
because they stay device↔host↔device.

## Wave S — Performance & scale

The app assumes modest folders and a fast link. Make it hold up otherwise.

| # | Feature | Effort | Impact | Why it's a gap |
|---|---|---|---|---|
| S1 | **Server-side thumbnails** — agent generates image/video thumbnails on demand (`GET /v1/thumb`), cached | M | ★★ | Today previews pull full files to thumbnail client-side — wasteful over cellular. Biggest bandwidth win for media-heavy folders. |
| S2 | **Huge-folder virtualization + incremental listing** — handle 10k+ entries without jank | M | ★★ | Pagination exists but the UI isn't proven at scale; the file-visibility footer already hints at large junk folders. |
| S3 | **Transfer compression-in-transit** — opt-in gzip for compressible types over cellular | S–M | ★ | Pairs with J2 bandwidth controls; cheap for logs/text/source trees. |
| S4 | **Thumbnail/preview prefetch policy** — bounded, network-aware prefetch of neighbors | S | ★ | Wave E preloads neighbors; make it a governed policy, not unbounded. |

## Wave T — Advanced file properties & permissions

A file tool that can't see or set permissions is incomplete for a Linux host.

| # | Feature | Effort | Impact | Why it's a gap |
|---|---|---|---|---|
| T1 | **Permissions view + chmod** (Linux) / read-only/attrib (Windows) | M | ★ | The meta-sheet shows size/date but not mode/owner. View-first, edit behind the per-token write check (H2). |
| T2 | **Checksums on demand** — show md5/sha256 in the meta-sheet (agent already hashes) | S | ★ | Verify a file matches without a transfer; cheap given existing hashing. |
| T3 | **Space-by-type treemap** — richer visualization than the Storage Insights bar list | S–M | ★ | Turns insights (Tier 1 #3) into a scannable map of what's eating the disk. |
| T4 | **Symlink awareness** — show link targets, don't silently traverse/loop | S | ★ | Correctness: jailed traversal must handle symlinks deliberately. |

> **Explicit non-goals reminder (from `feature-roadmap.md`):** remote shell /
> command execution, Chromecast, iOS port, full-text content search, two-way
> sync. None of the waves above cross those lines — **R1 (share link) is the one
> item that touches the network boundary; treat it as security-gated, not core.**

---

## Priority

By leverage, after `feature-roadmap.md` Tier 1:

0. **Wave 0 (settings architecture)** — **do first.** Owner-requested, and it's
   the foundation every later setting hangs off. Retrofitting the two-tier model
   after Q/L/J2 land would mean reworking them.
1. **Wave G** — highest daily value, mostly client-side; the editor is the standout.
2. **Wave H** — security debt the code review already surfaced; cheap, high-impact.
3. **Wave N** (N1 config backup) — small, self-contained, removes the biggest
   reinstall pain; can land any time.
4. **Wave O** (O1 Arabic/RTL) — owner-relevant and forces a layout audit; do it
   before the surface area grows further.
5. **Waves K, L, M** — quality-of-life layers; L and L3 want the SSE events
   channel (Tier 2 #10), K1/K2 ride the media + photo-backup work.
6. **Wave S** (S1 server-side thumbnails) — slot alongside the media waves; it's
   the biggest bandwidth win for anyone browsing photo/video folders remotely.
7. **Waves I, J, P, Q, T** — bigger lifts / personalization / horizon; several
   depend on background transfers (Tier 2 #9). P1 web companion and **R1 share
   link** each get their own plan doc (R1 needs a threat model) when reached.

**Top 3 if only three ship:** Wave 0 settings architecture (fixes the
configure-every-PC annoyance and unblocks the rest), G1 in-app text editor
(biggest *capability* gap), H1 remote device revocation (biggest *security*
gap). Honorable mentions: G3 write-conflict resolution and N1 config export.

**Every agent-side feature:** contract goes into `protocol/openapi.yaml` in the
same commit as the implementation — the spec drifted once; don't repeat it.
