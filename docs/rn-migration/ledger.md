# RN migration parity ledger (frozen 2026-09-29, base b49d7044, Flutter 1.42.5+80)

Status: port | replace | gap | obsolete(owner-approved). Post-parity items are appended below, never added to the ledger.

## Release identity
- applicationId `com.zqamhieh.remote_file_explorer`; versionCode 80; release signing via `app/android/key.properties` (absent here; `~/.rfe-keystore` missing) -> blocker rfe-6do.

## Screens (app/lib/features; Dart lines)
explorer 9735 · settings 5564 · preview 3888 · hosts 3633 · search 2339 · transfers 2258 · pairing 1837 · photo_backup 1078 · share 653 · home 569 · sync 556 · handoff 520 · onboarding 277 · bookmarks 227. All: port.

## API (protocol/openapi.yaml, 51 paths)
health,status,metrics,transfers/list,users(+/{u}),logs,audit,agent/restart,auth/{challenge,logout},pair,register,login,wol,share/{mint,token,list},system/drives,fs/{,folder,file,rename,copy,move,...}. All: port via generated typed client. Agent-admin endpoints (users/logs/restart) stay owner-gated.

## Native bridges (MainActivity.kt 475 L, TransferService.kt 86 L)
| channel | methods | RN plan |
|---|---|---|
| intent | getInitialHostId | Expo module + intent filter plugin |
| discovery | scan, stop | Kotlin NsdManager/mDNS module |
| transfers | start, stop, complete | Kotlin foreground service module (spike 1C) |
| files | saveToDownloads, getDeviceId, installApk, openFile | Kotlin module (MediaStore, FileProvider, package installer) |

## Persisted state
- SharedPreferences (`FlutterSharedPreferences`, keys prefixed `flutter.`): `rfe_hosts_v1` (JSON list), `rfe_last_seen_*`, favorites, bookmarks, pins, sync rules, saved/recent searches, view/visibility prefs, settings (`app.`, `host.`).
- Secure storage (FlutterSecureStorage 10.3.1): `rfe_token_<hostId>`, `rfe_fp_<hostId>`, `rfe_device_identity_private_v1` (32-byte Ed25519 seed, base64), `..._public_v1`, offline body cache key.
- Backup (`rfe-backup` v1) EXCLUDES the device private key by design (PR-17): backup cannot migrate identity. Migration must be in-place Kotlin read or verified re-pair.

## Pin semantics (agent_client.dart)
Empty trust store; exact leaf-cert SHA-256 (lowercase hex) compared in cert callback before any header/body; missing pin with token -> `MissingCertPin`; mismatch -> `CertPinMismatch`. One pin per host across routes. RN transport must replicate: custom Kotlin TrustManager, no system roots.

## Tests to port/replace: 100 Dart test files (app/test).

## Deliberate deviations (owner review)
- Explorer row long-press starts selection; Bookmark moved to the selection app bar (1 item). Flutter bound row long-press to bookmark, which left multi-select unreachable by touch.
- Preview kind ignores MIME parameters: the agent sends `text/markdown; charset=utf-8`, so Flutter always showed Markdown/CSV as plain text.
- PDF pages scroll vertically (Flutter paged horizontally inside the horizontal file pager).
- Pinned HTTPS reuses TLS connections per pin (Flutter's HttpClient pooled too); never across pins.
- Details sheet (folder/file actions, permissions, checksum): opened from the folder info button, from the selection bar when one item is selected, from the preview menu, and by tapping a file that has no previewer. Flutter reached it only from the folder info button and only showed a toast for unpreviewable files, so file actions such as Extract here and Share link were unreachable.
- Offline bodies are AES-256-GCM (Tink streaming AEAD, 64 KiB segments) with the key in secure storage; Flutter used ChaCha20-Poly1305 with its own envelope. Existing Flutter offline_cache files are not migrated (they are reconstructible). File names are hashes, so the directory no longer reveals host ids or paths.
- The Files tab opens a host's cached listings while it is offline by remembering the roots it last allowed (Flutter showed a connection error for "Browse cache").
- Forgetting a computer also drops its cached listings and offline bodies (the Flutter behaviour, which the first RN port skipped).
- Host settings: Storage insights and Sync rules rows are left out until those screens are ported (rfe-bvw.5, rfe-bvw.4) instead of shipping dead links.
- Connection diagnostics tells DNS failure from an unreachable host by reading the transport error behind the connection failure (AgentApiError.cause); Flutter read Dio exception types.
- Cross-host search (Flutter had the screen but no way to reach it; only its widget tests used it): reachable from the host search screen's "Search every paired device instead" button, and its rows open the result's folder in Files (Flutter's rows did nothing).
- Duplicate finder asks before moving the marked copies to Trash (Flutter trashed on one tap). The kept copy is the first in walk order; Flutter's was whatever order the checksum response had.

## Post-parity list
(none yet)

## Onboarding pager
- The welcome and ready pages use a static gradient circle, not the animated two-blob hero (no Rive asset exists for that slot either way).
- The pager is skipped for any install that already has a paired computer, so upgraders from the Flutter app never see it. The flag is stored as `onboarding_complete` in the kv table.

## Upload engine
- Uploads run in the same native journaled engine as downloads (one foreground service, one journal). The record carries `direction`, the agent `sessionId` and the whole-file SHA-256, so a killed app resumes the same agent session and sends only the chunks the agent's bitmap lacks.
- Chunks are sent one at a time at a fixed 4 MiB (the Flutter app's default). Parallel chunk sends are not ported yet.
- Picked files are moved from the picker's cache into `files/uploads/<id>/` and deleted with their journal entry outcome (done or cancelled), so a cleared cache cannot break a resume. A failed upload keeps its copy so Retry works.
- "Keep both" numbers the new copy against the names already listed in the folder plus the other picks. A concurrent writer on the host can still cause a CONFLICT, which surfaces as a failed transfer with a plain-language reason.
- The Transfers tab groups Active, Failed and Finished with a progress bar per row and Clear for finished ones. Speed and ETA, and per-host grouping, are not ported yet.

## Public Downloads
- Finished downloads are copied into the shared `Download/RFE` folder through MediaStore (Android 10 and newer, no storage permission) and the app-private copy is dropped. The Flutter app kept downloads in its app-specific external folder, invisible to other apps. On Android 9 and older, or if publishing fails, the file stays in app storage and the row says so.
- A finished download opens from its Transfers row. Removing a row never deletes the published file.

## App settings
- The Settings tab is a hub with Preferences (Appearance, File visibility, Transfers), Data (Storage & Security) and Support (About) sub-screens. Flutter mixed these with per-computer settings on one long screen; per-computer settings stay under each host.
- Accent color uses the same eight presets and the Material 3 tonal-spot derivation as `ColorScheme.fromSeed` (`@material/material-color-utilities`); the default keeps the hand-picked brand scheme. The app's violet stays the secondary role, as in Flutter.
- "Use wallpaper colors" is not offered: React Native has no Material You source. The stored `app.dynamicColor` value is kept so a round trip does not lose it.
- App lock uses `expo-local-authentication` (biometric with device PIN/pattern fallback). It locks on cold start and after 2 s in the background, and fails open when no screen lock is enrolled (a lock nobody can satisfy would brick the app). Turning it on requires a screen lock and one successful prompt first.
- Trusted certificates lists each paired computer's pinned fingerprint with Forget (the same un-pairing as host settings, including its cached data). Flutter showed the same list without a way to forget from there.
- Cache shows the cached folder-listing count and Clear also empties the image cache. The "Downloaded files" line is gone: downloads now live in the shared Downloads folder and are not app cache.
- Not ported because nothing in the RN app consumes them yet: notification toggles, low-disk alerts, weekly digest, watched folders and compress-on-cellular. The stored values are read and written unchanged. The update tile, diagnostics export and open-source licenses are under About & Support (see Updater and Licenses below).
- Transfers settings only shows the fixed 4 MiB chunk size and links to the Transfers tab.

## Storage insights
- Host settings gets a Storage Insights row (Flutter had it too, reached from the same screen). The ring, per-drive list and free-space row use the same aggregation as Flutter; the agent reports drives that sit at or under the device's allowed folders, so a jailed device with roots on a non-mount folder sees an empty state, as in Flutter.
- The by-type map scans the host recursively from `/` (Flutter's start point), one page of 200 at a time, and stops when the screen closes. The route also accepts a `path` parameter to scan a single folder; nothing in the UI passes one yet.
- Sizes use the shared formatter (binary units), so a 10.2 MB file set shows as 9.7 MB, as in the Flutter map.

## Explorer command palette and transfer pace
- Command palette (overflow menu) filters the same eleven actions as Flutter; Navigate to Path asks for a path and jumps there. Storage by type in the overflow menu now opens the map for the current folder (Flutter did too).
- Active transfers show the smoothed speed and time left, worked out in JS from the engine's progress events (an average that ignores samples closer than 0.5 s). Flutter showed the same two numbers; there is no native rate.
- Each download is staged in its own folder (`downloads/<transfer id>/<name>`). Flutter and the first RN port used `downloads/<name>`, so two downloads of one file at once shared a partial file and corrupted each other ("size mismatch"). The folder is removed once the file is published, cancelled or forgotten.

## Offline pin sync
- Once per launch, the first time a host's Files tab loads successfully, its pinned folders are re-listed (updating the offline listing cache) and files that are not yet stored offline are downloaded. Flutter only fetched a folder's files when it was pinned and never refreshed afterwards. Bodies already stored are left as they are; they refresh when the file is next opened online. An offline start does not count, so it retries on the next visit.

## Hand-off and share intake
- Hand-off QR: a file's More sheet has "Scan to receive". The QR carries the host's certificate fingerprint and the file path (no token, no address). The other phone's Receive screen scans it, finds the paired computer whose pin matches, and downloads through the normal engine. A phone that has not paired that computer is told so rather than pairing it. Flutter had no phone-to-phone hand-off.
- Share intake: sharing files from another app to RFE opens a screen to pick a paired computer (skipped with one), then a folder with the same picker as Move/Copy, and uploads with the usual name-clash prompt. Text and links are ignored with a toast. Android only (`expo-share-intent`, iOS disabled). Flutter's share intake did not exist.

## Not ported yet
- No in-app update-ready notification (Flutter posted one from its background check); the RN background job downloads the APK quietly and About shows "Update available".
- `expo-share-intent` throws on a share whose content URI returns no rows (an unreadable or revoked grant) and takes the app down; normal senders grant access, so this is left as is. Verified on the emulator with a MediaStore image: cold start, folder pick, upload, and the keep-both prompt. The camera scan in Receive was not exercised (no camera on the emulator); the QR sheet and the permission screen render.

## Photo backup
- Settings > Photo backup keeps the Flutter behavior: one-way copy of photos (not videos) to the folder the computer's owner set in its web companion, in `<folder>/<phone nickname>/<year>/<year-month>/<asset id>.<ext>`, optional album selection, Wi-Fi-only and charging-only switches, and a "Back up now" action. The settings and the backed-up record use the Flutter keys and encoding, so the one-time import carries them over.
- Differences: Flutter never had a scheduled photo backup (its WorkManager job only checked for app updates). RN adds "Back up automatically" (see Scheduled background work). A photo is recorded as backed up when its upload finishes, found by the transfer id `pb-<asset id>`; the record is updated while the app is open, so a photo whose upload finishes while the app is dead is recorded at the next launch (unless its finished row was cleared first, in which case it uploads again and the agent answers CONFLICT, which is treated as already backed up: the path is device, capture date and MediaStore id, so the file there is the same photo). The media library reports assets as content URIs, so the file name, transfer id and record use the trailing MediaStore row id, which is also what Flutter recorded.
- Emulator-verified against a scratch agent: permission prompt, PC picker, album picker (root-level files with no bucket name show as "Other photos"), files landing at `<folder>/<device id>/<year>/<year-month>/<id>.jpg`, live backed-up count, reset record, already up to date, Wi-Fi-only and charging-only messages, and the "not set on the PC" message. The PC must list its backup folder inside its roots (a relative `-roots` value made the destination unavailable to the paired device).
- Each photo is copied into app storage before queueing (the engine deletes the copy when done), so a large first backup briefly needs the same amount of free space as the queued photos.

## Parallel chunk uploads
- An upload sends up to three chunks at once (Flutter sent one at a time per file). The agent already allows concurrent chunk requests per session; completion waits for all of them. Progress and resume are unchanged: the agent's bitmap decides what is left, so a failed run may leave later chunks already stored. JVM-tested against the fake agent (concurrency asserted). Emulator wall clock, loopback, 100 MB: about 12 s unthrottled (roughly 8 MB/s or more; the emulator's own network throttle does not apply to the host loopback). There is no switch to force one chunk at a time, so no sequential baseline was measured, and no real Wi-Fi timing exists (needs a phone).

## Support and About
- About & Support has What's new (a short 2.0 list, not Flutter's per-release history from v1.18), Privacy policy (the same facts as Flutter, plus that diagnostics carry no passwords, tokens or file names) and Export diagnostics, which copies a plain-text summary to the clipboard as Flutter did. The RN summary adds app lock and drops the Dart version.
- The in-app updater and the licenses page are ported (see below). Host setup guidance is the existing pairing, login and register hints, which were ported with their Flutter wording.

## Sync rules
- Settings > Sync Rules mirrors one remote folder into a folder picked with the system folder picker (Storage Access Framework tree; the app keeps access across restarts). Rules keep the Flutter keys and encoding, so the import carries them over; a rule whose local path is a plain path from Flutter still runs.
- On demand only, downloads only, top level only (no subfolders, no deletions), as in Flutter. A file is fetched when it is missing, a different size, or older than the remote copy; each download goes to a staging file first and is copied in on completion, replacing any existing document. There is no background sync.
- Emulator-verified: add rule, Documents picker with the "Allow access" prompt, first run downloads (names and sizes right), second run reports 0 downloaded, an edited remote file replaces the local copy with no `name (1)` duplicate, delete rule. The remote path must be absolute on the computer's filesystem (a relative `/docs` was refused by the agent as outside its roots). A failed run names the first file's reason in the message.
- The picker refuses the storage root, Downloads and similar folders (Android's own rule).

## Encrypted backup
- Settings > Backup & restore exports hosts, tokens, favorites, bookmarks, pins and settings to an `.rfebackup` file and shares it, and imports one. The envelope is Flutter's (`rfe-backup` v1, PBKDF2-HMAC-SHA256 200k iterations, AES-256-GCM), so files move both ways. A JVM test decrypts a Flutter-made vector. Passphrase minimum is 12 characters. Import replaces everything owned by the app after a confirm, and asks for an app restart.
- Only the known secure keys (`rfe_token_*`, `rfe_fp_*`, device identity) are exported; unknown secure entries are never read into a backup.
- Emulator-verified: export writes the file and opens the share sheet; import with a wrong passphrase reports "Incorrect passphrase, or this backup file has been corrupted"; import with the right one restores; the RN-side envelope round trips. Importing the Flutter vector through the UI was not done (adb cannot type the non-ASCII passphrase); the JVM test covers it.

## Scheduled background work
- One periodic WorkManager job (`expo-background-task`, about every 6 hours, network required) runs the automatic photo backup (only when both "Enable photo backup" and "Back up automatically" are on) and the update check. Photo permission is read without prompting; no permission means no run. The job waits up to 8 minutes for the queued uploads before the process may be stopped.
- The task is defined in `mobile/index.js` (the app's entry), because the headless run evaluates the entry but never mounts the router; defining it in the root layout failed with "No task registered" on the emulator.
- Emulator-verified with the process dead: a new photo reached the computer (`148.jpg`) from a forced job run; the job survives a reboot (WorkManager re-registers it without opening the app); force-stop cancels it until the app opens again (Android behavior). Each run logs a one-line report (`[rfe-background]`).
- Not verified: OEM battery savers that kill the job or delay it past the interval (needs the owner's phone), and whether a foreground-service start is refused when the job begins on a locked, idle phone (the upload finished on the emulator; a stricter Android 14 or 15 device may queue it until the app opens).

## Updater
- Release channel: the same GitHub repo and `releases/latest/download/latest.json` the Flutter app reads, with the APK host allow-list; size and SHA-256 are checked before the installer opens. About & Support > Updates checks on demand; the Devices tab shows a banner. The APK is fetched in the background on Wi-Fi only. The installer needs the "Install unknown apps" permission, which the app sends the person to.
- Found while testing: the repo's Latest release is currently the agent (`agent-v1.42.7`), which has no `latest.json`, so the manifest URL returns 404 and neither app can find an update until an app release is Latest. The RN About screen now shows an "Update failed: HTTP 404" message instead of "Up to date" in that case (the manual check used to swallow the error).
- Not verified: download, verification and the installer prompt (no newer app release exists to serve, and the host allow-list blocks a local one).

## Licenses
- About & Support > Open-source licenses lists the built-in native code plus every JavaScript dependency with its license text (expandable), generated by `scripts/gen-licenses.mjs` into `src/features/support/licenses.json`. Emulator-verified: renders, scrolls, expands.

## Edge cases checked on the emulator (rfe-2mi.9)
- Revoked token: the host opens to "invalid or revoked token" with Retry; un-revoking and Retry recovers. Flutter behaves the same (no re-pair prompt).
- Network drop mid-upload (Wi-Fi and data off for 20 s, through a throttling TCP proxy so the transfer is slow enough to catch): the transfer fails as "Could not reach the computer"; it does not resume by itself. Back up now (or Retry) resumes on the same session and finishes byte-exact. Killing the app mid-upload leaves the transfer Paused; Back up now resumes it, also byte-exact.
- Bugs this found and fixed: concurrent chunk requests failed with "Cipher not initialized" (secure-store reads are now serialised); a failed photo upload was removed and re-queued, opening a new session each time, and the agent allows only four open sessions per device (a stalled phone could be locked out for a week: the agent has no way to close a session and sweeps stale ones after seven days), so failed and paused photo uploads now resume their own session; after a dropped connection the agent answers 409 TRANSFER_ACTIVE until it times out the old request, so the uploader waits and repeats (up to 8 times, 5 s apart).
- Storage full: a backup with no room to copy the photo said "Already up to date"; it now says how many photos could not be prepared and tries them again next run. No partial staging file is left behind.
- Reboot: the periodic job is back after boot. Transfers found running become Paused, as before.
- Device matrix (emulator only): 720x1280 at 280 dpi, font scale 1.6, dark mode, and a 1600x2560 tablet at 320 dpi all render without overflow or crashes. At 1.6x the bottom-tab labels nearly touch and long status lines truncate; the tablet gets the phone layout stretched, with no two-pane layout.

## Pairing without codes or fingerprints (owner request)
- Add computer > Find on LAN / Enter address: pick a computer and it asks the owner to approve at the PC. The PC shows a desktop notification with Approve/Reject (`notify-send` actions on Linux) or the owner runs `rfe pair requests|accept|reject`; nothing is typed on the phone, and the certificate fingerprint is never shown, typed or scanned in the app (it is learned on first contact and pinned).
- Both sides show an 8-digit match code, HMAC-SHA256 keyed by the pinned certificate, so a machine relaying the connection shows a different code than the PC does. The TS and Go implementations share a test vector, and the live contract test checks the phone's code equals the one `rfe pair requests` prints.
- Agent: `POST /v1/pair/request`, `GET /v1/pair/request/{id}?nonce=` (token handed out once), admin list/approve/reject; requests expire after 2 minutes, 3 wait at most, 6 asks per minute per address (in `protocol/openapi.yaml`). Approved devices start browse-only, like code-paired ones.
- Verified on the owner's phone with the real agent: request, matching codes (8863 5563 on both), approve from the PC, device listed, computer opens and lists its shared folder. Not verified: clicking the desktop notification button (approved through the CLI, which takes the same path); the web companion does not list requests yet.
- Login and Register no longer ask for a fingerprint (learned on first contact instead). Register still sends a one-time code, so on a hostile network that code could be handed to a relay; QR scan (code and fingerprint inside the QR) is unchanged and not re-tested here.

## Owner's phone walkthrough (USB, real agent)
- Verified on the phone: browse; download (28.6 MB, saved to Downloads, SHA-256 equal to the source); text view and edit; image view; upload through the system picker (landed on the PC); Move to Trash; photo backup of one album (one photo landed in `Phone Backup/<phone>/2026/2026-09/`). Permission denials show the agent's reason: "device lacks modify permission", "Delete failed: device lacks delete permission".
- Bug found and fixed: a refused download (403 CAPABILITY_DENIED) said "Could not reach the computer". The engine now fails with the agent's code and the app explains the missing permission.
- New devices are browse-only by design, and the app does not know its own permissions (the Download and Delete buttons show anyway). `rfe allow <id> browse,download,upload,modify,delete,share|all|none` sets them from the PC.
- Setting the photo backup folder needs the web companion (or a config row plus agent restart, as done here).
- Also verified on the phone: search; Sync Rules (first sync and an incremental one with a new and a changed file); host settings screen; encrypted config export (file produced, passphrase rules enforced).
- Found and fixed: the host settings header, the Trusted certificates list and the connection diagnostics still showed a fingerprint prefix. They now show the address or a plain "pinned" state; the fingerprint appears nowhere in the app.
- Verified on the phone (second pass): share link create and revoke (link fetch is covered by the agent's tests; I could not read the full token from the screen); config export then import (file is an encrypted envelope with no plaintext host data; a restore keeps the host and token across a full restart); Login (audit shows it, the device becomes an owner device); Register on a scratch agent with no account (it refuses on an agent that already has one, with a clear message); connection diagnostics sheet (no fingerprint shown; Tailscale "No response" because the phone has no Tailscale).
- Performance pass on the phone (S23 Ultra, release build, 600 JPEGs of 1600x1200 in one folder, 20 flings): 1255 frames, 1.83% janky, 50th/90th/99th percentile 12/14/20 ms, app PSS 488 MB (graphics 131 MB, native 146 MB). Thumbnails rendered. No Flutter build on the phone, so there is no side-by-side number. Listings are cached: a new folder on the PC shows after pull-to-refresh.
- The desktop notification is sent with Approve and Reject actions (seen on the session D-Bus); clicking it is still unverified.
- Actions the device may not use are now hidden (download, delete, rename, copy/move, create, upload, share link), driven by `fileCapabilities` on GET /settings; the agent still enforces. Verified: an upload-only device sees only "Upload file" in the Create menu and no Download or Delete in the file sheet.
- `rfe backup-dir [path|none]` sets the photo backup folder (restart the agent to apply).
- Accepted risk, for the security review: Register and Login trust the certificate on first contact, so a machine in the middle during that first request could read the one-time code or password. QR and approve-on-PC pairing do not have this gap.

## Scope decision
- The web companion (`agent/internal/webui`) is out of scope for this migration: the owner is taking it in a new direction. No work is planned on it here, and the code is left untouched. Consequences: pair requests are answered with the desktop notification or `rfe pair accept|reject`; the photo backup folder and device permissions are set with the CLI (`rfe allow`) or the agent's config until the new direction lands.

## Still needs a physical phone (recorded, not verified)
- Camera QR scan on a real camera and LAN discovery (mDNS): the emulator's NAT drops multicast. QR decoding itself is verified on the emulator's virtual-scene camera (see below).
- Secure-storage migration on a production Flutter install (needs the owner's signed Flutter build and its data; the reader is the same plugin code, JVM- and emulator-tested on fresh data only).
- Scroll, thumbnail-memory and throughput comparison against Flutter, and per-OEM device walkthroughs: emulator numbers are not comparable to a phone, and no Flutter build was installed for a side by side.
- Real Wi-Fi to cellular handover and OEM background-kill behavior.

## QR scan and Approve click (emulator / stub)
- QR scan: emulator `rfe_test` with `hw.camera.back = virtualscene`, the pairing QR (address 10.0.2.2, scratch agent) as the scene's wall poster, camera moved with the emulator gRPC `setPhysicalModel` (POSITION/ROTATION). The scanner decoded it and the app paired ("Paired with qr-scratch", host online). Real-camera scan on the phone is the owner's to do.
- Approve click: `contract/fake_notification_server.py` stands in for the notification service on a private D-Bus and answers the agent's Approve action, so the agent's click handling and pairing completion are tested (`RFE_NOTIFY_STUB=1`). A real click on the Plasma notification is still unverified.

## Second phone: Galaxy A53 (SM-A536E, Android 15, 1080x2400, serial R5CTB12ZLQH)
- Had an old Flutter 1.12.0 (versionCode 20) signed with a different key; `adb install -r` of the RN release (versionCode 81, production signer) failed with INSTALL_FAILED_UPDATE_INCOMPATIBLE. Uninstalled it (owner-approved, secondary phone) and installed RN: onboarding, Add computer > Enter address, pairing, Devices shows <host> Online v1.3.0 LAN", Files lists "RFE Files". This is not the iib.7 upgrade test (wrong key, no production data).
- Approve: the first request (code 2197 1184) expired after about 2.5 min with nobody clicking ("Nobody approved in time"). The retry (code 0414 2043, matching the phone) was accepted with `rfe pair accept`, not a click. A real click on the Plasma notification and a real-camera QR scan are still unverified.
- Device `<id>` is this A53; `<id>` is the earlier pairing. Neither is removed.
- Approve on the real desktop notification: verified on the A53 (request <id>, code 8261 2207, started by the owner on the phone, approved 4 s later; audit_log id 7 "approved at the computer"; no `pair accept` was run by the agent for it). The phone re-paired into the existing device row <id> (same client id), so no extra device exists. Audit text is identical for CLI and click, so this rests on no CLI accept having been run. Still unverified: the real-camera QR scan.
- Real-camera QR scan: verified on the A53 (camera permission prompt, live viewfinder, pairing QR from `rfe pair` shown on the desktop terminal). audit_log id 8 "pair ... from <lan-ip>" (code path, no "approved at the computer"); the phone re-paired into device <id> and stayed <host> Online". LAN discovery (mDNS) on a real phone is still unverified.

## Scope decision: iOS dropped
- Owner dropped iOS on 2026-10-01 (no Apple developer license). rfe-oz9 closed. The migration is Android only.

## Lumen design pass (2026-10-01)
- Mockup: `design/mobile-redesign/lumen-variants.html` (found; rfe-4kh.8 said it was missing). Reviewed by a defender, a critic (two rounds) and a judge; the judge's 9-change spec was applied (commits bce05ae4, 69e4508e, 214b68b2): role colours with a contrast test (AA on all three schemes, dark `onSurfaceVariant` raised to #A1A1AA), category-coloured file and root icons, a flat 4-tab dock without the FAB, a root-named breadcrumb with an AA current crumb, a Files action footer (Upload/Paste/New gated by the device's grants; Paste needs modify), a host card with a route strip, Open and three tiles, a filled Add computer button, Transfers tones and an "Open Files" empty-state action, 48 dp touch targets.
- Rejected on purpose: graphite palette, Lato, Trust tab and screen, floating dock, hero ring, static status chips, fabricated data (Ask first, device key, item counts). Buttons stay stadium-shaped (UI rule).
- Manual test, two parallel testers: emulator (scratch agent; all four grant sets, themes, font 1.3; no crashes) and the A53 (real agent, read-only). Defects found and fixed: dock pill lost its radius after a tab change, Paste offered without modify, neon-green secondary button in dark, selection bar with no allowed action, batch rename without modify, deep breadcrumb cut off, offline card dimmed as a whole, failure reason clipped, toast swallowing dock taps, legacy search chip contrast. Not fixed (pre-existing): menu anchor position, light theme + AMOLED toggle shows black, stale offline banner, "Read-only mode" row shows the agent-wide flag.
- Not yet verified after the fixes: the dock pill on the A53 (locked during the recheck). The A53 is browse-only, so the footer was only seen on the emulator.
- Dock pill fix verified on both the emulator and the A53 (commit dbd6cb6f): a colour swap or late mount of the pill background dropped its radius on Android, so all four pill backgrounds are mounted up front and only their opacity changes. After a relaunch the Files tab shows "Select a server" until a host is opened from Devices (existing behaviour).

## Full Lumen pass (2026-10-01)
- Foundation 78a14f56, screens 89111491: Home (host hero, route strip, Files/Apps/Activity/Trust tiles, recent), Workspaces, Files (collections, role-tinted rows, 60/40 footer, selection), Apps, Activity, Add workspace, Connect, Trust, Settings hub, flat sheets and dialogs. Two-row top bar (context + label/state pill) as in the mockup.
- Real data only: Trust lists the device's eight grants, pin state and paired date (fingerprint never shown) and recent audit; Connect lists probed routes with latency. Not reproduced because the agent returns no data for them: folder item counts, "Home office" host note, Ask first, per-app running state, relative transfer time.
- Fixed a foundation bug: `LumenSize` values are dp, but Button, dock, Card and screens multiplied them by 2.
- Checks: tsc clean, lint 0 warnings, jest 430 pass, gradle :rfe-transport:testReleaseUnitTest ok, go vet and go test (agent/) 490 pass. Built arm64 release (versionCode 81), verified on A53 in white and dark.
- Not yet verified: emulator run, S23 Ultra, light theme with AMOLED toggle, Apps list with a device that has view_apps (A53 is browse-only, so the Apps list itself was not seen).
- Second pass (05bbeeb3 and after): emulator with the scratch agent and all grants (x86_64 build) confirmed Home with Recent, Apps list (89 apps, Run buttons, Can launch), Files root and folder with Upload 60 / Select 40, selection mode with the five-tile bar, Trust with all grants, Activity with a finished download. Fixed there: outlined search/text fields now flat; Trust asks the apps endpoint for View/Launch apps when the device record is unreadable (A53 shows Not allowed, emulator Allowed); Activity sections read In progress / Recent / Failed; permission titles wrap to two lines.
- Still differs from the mockup, by design (no such data from the agent): folder item counts and "updated today", the host note under the name, "Ask first", per-app running state and tinted app icons (the catalog gives no icon hint for Linux apps), relative transfer time, a Workspaces dock item, second footer button. Not verified: S23 Ultra, light theme with the AMOLED toggle.
- Mistake to note: `rfe pair` without `-data` created a pairing code on the real agent (and printed its fingerprint into a session transcript). The code was deleted from the real agent.db at once; the fingerprint is public to anything that connects, but rotate nothing unless wanted.

## Real data for the remaining Lumen content (ab2f19da, owner: "do what you see fit and add what you see fit")
- Agent: `Entry.childCount` (folders in a listing; capped at 1000, first 60 dirs per page, read through the jailed path) and `HostApp.category` (freedesktop Categories mapped to Development, Internet, Office, Media, Graphics, Games, System, Utilities); OpenAPI updated; Go tests added. Contract suite 20/20 against a fresh agent build; `go vet` and `go test ./...` pass (492).
- Native journal: `updatedAt` on every write (finish time for a done transfer); new JVM test; `:rfe-transport:testReleaseUnitTest` 14/14 in TransferEngineTest.
- App: folder rows show "Folder · N items", app rows show "Category · ready" with a category-based icon, Activity shows "... · 3 minutes ago", a local workspace note (Workspaces row menu: Add note) shows under the host name, and the AMOLED switch no longer blackens the light theme (`effectiveMode`). jest 437 pass, tsc and lint clean.
- Seen on the emulator with the new agent: folder counts (0 items, 1 item, 6 items), Apps rows "System · ready", "Development · ready" with a code icon, "Internet · ready" with a globe.
- Not seen on a device: the note dialog and note text under the host name, the "N minutes ago" text in Activity, the AMOLED fix, the S23 Ultra. The real desktop agent is still the old build (no counts or categories until it is reinstalled; the app copes with their absence).
- Seen on devices afterwards (emulator with the new agent build, then A53): the workspace note dialog and "Home office · connected now" under the name (aac8985c), "Saved to Downloads · just now" in Activity, and the AMOLED fix (Light stays light with AMOLED on; Dark with AMOLED is black) on the emulator and the A53. The A53's theme settings were put back and its test note removed. Still open: the S23 Ultra, and reinstalling the real desktop agent so it sends folder counts and app categories.

## Live agent upgraded (2026-10-01)

The owner's systemd user service now runs the `rn/migration` agent build (was the earlier build). Backup of the old binary and a SQLite copy of agent.db: `<home>/.rfe/<backup>`. Rollback: copy `rfe.old` back to `~/.local/bin/rfe`, restore `agent.db` if the schema moved, `systemctl --user restart rfe`. Verified: `/v1/health` ok, A53 reconnects, folder rows show live counts ("Folder · 600 items", "1 item"). The A53 is browse-only so app categories are only verified on the scratch agent. S23 Ultra still unchecked (not connected).

Live Apps check (2026-10-01): with `view_apps` briefly granted to the A53 (owner-approved, restored to browse-only afterwards), the Apps tab on the live agent showed 89 apps with real categories (System, Development, Utilities) and the "View only" state.

S23 Ultra stand-in (2026-10-01): the S23 Ultra was not attached, so the x86_64 release build ran on the emulator at 1440x3088 / 450 dpi (the S23 Ultra's size). Home, Files and Apps showed no overflow, truncation or dock problems. This is not a check on the real S23 Ultra; that and the Flutter-upgrade test (rfe-iib.7) still need the phone.

S23 Ultra check (2026-10-01, SM-S918B, 1440x3088 @ 600 dpi, graphite dark, live agent): Home, Activity and the gated Apps state render to the Lumen layout with no overflow or truncation, and the floating dock clears the three-button nav bar. The S23 already ran the RN build (versionCode 81, installed 01:50), so the upgrade-over-Flutter test (rfe-iib.7) cannot be run on it without a Flutter build to install first.

Upgrade over a real Flutter install on the S23 Ultra (2026-10-01, rfe-iib.7): the RN app (vc81) was uninstalled, the published Flutter v1.42.5 (build 80, signer 40b84489...e878e9) installed and paired to the live agent with a `rfe pair` code (Enter Code tab); it listed `RFE Files` with its folders. `adb install -r` of the arm64 RN release APK (versionCode 81, identity check ok) then upgraded in place: firstInstallTime unchanged, no re-pair, Home showed <host> "Connected securely" with recent files, and Files listed the same four folders (authenticated, with item counts). Not covered: rollback to Flutter; Flutter-era app data beyond the host record (favorites, pins, sync rules) was not created before the upgrade, so only host, token and pin import were exercised on the phone.

Real-phone edge cases, S23 Ultra, 2026-10-01 (rfe-2mi.9):
- Wi-Fi to cellular mid-download (agent port throttled to 24 Mbit with tc, 400 MB file): the transfer failed with "Could not reach the computer" at 208 MB and showed Retry. Retry while on cellular (3G "H", Tailscale up) showed "Receiving" but did not move in 15 s. After Wi-Fi came back, Resume finished the file byte-exact (SHA-256 matches the PC). So a network switch does not corrupt anything and recovers on Wi-Fi; it does not hand over to cellular by itself. Mobile data and Wi-Fi were put back as found.
- Low storage (the phone filled to about 20 MB free with `fallocate` files, removed straight after): the download stopped at 185.8 MB and the app kept showing "Receiving" with a stale speed and never failed. Cause: the failed write was caught, but saving the FAILED state to the journal threw as well (no space), so the UI was never told. Fixed in `TransferEngine.kt`: the journal write is best effort, and a no-space error now has its own code (`ERR_STORAGE_FULL`, "This phone is out of storage. Free some space, then tap Retry.") instead of "Could not reach the computer". Two JVM tests added (a symlink to /dev/full, and an unwritable journal); both fail without the fix. Not yet re-run on the phone: the S23 locked itself (PIN) before the fixed build could be tested.
- Not done: OEM battery kill on a real Samsung (the sleeping-apps policy needs days of disuse; only app-ops and force-stop can be emulated), reboot on the real phone.

More S23 Ultra results (2026-10-01):
- Fixed build (98d8c7c) re-run on the phone: with the disk full the transfer now ends as Failed with "This phone is out of storage. Free some space, then tap Retry."; Retry then finished the 400 MB file byte-exact. The filler files were removed at once (free space back to 122 GB).
- Battery kill, emulated (a real Samsung "sleeping apps" policy needs days of disuse): `RUN_ANY_IN_BACKGROUND deny`, standby bucket restricted, then `am force-stop` during a throttled 150 MB download. On reopening, the transfer was Paused at 27.4 MB with Resume; Resume finished byte-exact (SHA-256 matches). App-ops and bucket were restored afterwards.
- Reboot: the periodic WorkManager job (network constraint, about 5.5 h delay) was present before and after `adb reboot`.
- LAN discovery: Find on local network lists the PC (<lan-ip>:8765, instance name "<instance>") within 10 s on the real phone. Pair was not tapped.
- Test artifacts removed: the large test files on the PC and phone, the tc throttle on the PC (qdiscs back to fq_codel); the temporary download grant on the S23 was restored to browse-only.

Minimum Android (2026-10-01, rfe-iib.2): the x86_64 release build (production key) on an API 24 (Android 7.0) google_apis emulator at 1080x1920: installs and launches, the camera-permission and pairing screens render, "Ask to pair" to a scratch agent works over the pinned TLS connection (match code identical on both sides), and the shared folder lists. Not covered: both real phones are Samsung (Android 14/15); no other brand, and no API 24 hardware.

A53 (R5CTB12ZLQH), 2026-10-01: RN screenshots for the owner's before/after check were taken (Home, Files hosts, Files root, Big Folder, Activity; graphite dark) and saved in `docs/rn-migration/screens-a53/` (not committed). The matching Flutter shots were not taken: the owner said not to use the old app any more. The A53 was uninstalled and reinstalled once during this attempt, so it was re-paired with Find on local network (match code checked on both sides, approved at the computer); it reused its device row and is browse-only again.

## Security review (rfe-iib.5)

Scope: agent pairing/auth (`pair_request.go`, `challenge.go`, `store/devices.go`, `store/pair_requests.go`, `pairing/sas.go`), the RN app's native layer (`PinnedHttp`, `MediaProxy`, `FetchFile`, `PublicHttp`, `ExternalFiles`, legacy secure-store/prefs readers), manifest and config plugin, deep links, updater, dependency audits (npm, govulncheck). No critical findings. Fixed in this branch: R1-R6. R7 and R8 accepted.

| # | Severity | Finding | Status |
|---|----------|---------|--------|
| R1 | Medium | `rfe://pair/request?address=<host>` (any app or web page can fire it) started pairing with an attacker-chosen address with no tap: the phone connected, pinned that host's certificate and sent its device id and public key. Reproduced on the A53. | Fixed: `src/app/+native-intent.tsx` + `core/externalLink.ts` allow-list; external links reach only the Add-workspace screen, address prefilled, and everything else lands on home. Tests in `externalLink.test.ts`. |
| R2 | Medium | `allowBackup="true"` in the RN manifest (Flutter had `false`): device private key, tokens and pins would be in cloud/adb backups. The Keystore wrapping key does not travel, but the data should not either. | Fixed: plugin sets `allowBackup=false`. |
| R3 | Low | Permissions beyond the Flutter app: SYSTEM_ALERT_WINDOW, RECORD_AUDIO, MODIFY_AUDIO_SETTINGS (template/library defaults, unused). | Fixed: `android.blockedPermissions` in app.json. |
| R4 | Low | Approve-on-PC with a known client id reuses that device's row: new key and token, `revoked` cleared, grants kept (only if the old row was not login-minted). The prompt does not say it replaces an existing device. The client id (ANDROID_ID) is never exposed by the agent, so an attacker must already know it. Login already rejects a key change; the request path re-pins. | Fixed: a request whose key differs from the pinned one, or whose device was revoked, is flagged as replacing that device (desktop notification, `pair requests`, admin list `replaces` field in openapi); approval resets that row to browse-only with no app access or share links, and the audit entry names the replaced device. A same-key re-pair is unchanged. Tests in `pair_request_test.go` (fail without the reset). |
| R5 | Low | Go 1.26.5 standard library: GO-2026-6090 (crypto/tls post-handshake messages), -6089 (net/http h2c ReadHeaderTimeout), -5972 (encoding/asn1 depth). All unauthenticated-DoS class. | Fixed: `toolchain go1.26.6` in go.mod (CI already used it); govulncheck now reports 0. The running systemd agent keeps 1.26.5 until it is redeployed. |
| R6 | Low | Update manifest `sha256` is optional (`updateService.ts`); a manifest without it skips the hash check. Android's installer still refuses an APK signed with another key, and the URL host is limited to GitHub. | Fixed: `parseRelease` refuses a manifest without a valid sha256 (the release workflow always writes one) and the download is always hashed. |
| R7 | Low | `MediaProxy` path token is reusable for the life of the player (its comment says one-use) and the thread pool is unbounded. A local app would need the 192-bit path. | Accepted. |
| R8 | Info | npm audit (prod): 15 moderate (decode-uri-component, uuid, expo config-plugin chain), 0 high or critical; build-time or unreachable paths. | Accepted. |

Accepted by design (unchanged from Flutter): Register/Login TOFU. The first TLS connection to an address pins whatever certificate it presents, so a LAN attacker who answers before the real PC wins that pairing. Mitigations in place: the QR and approve-on-PC flows carry the fingerprint or an 8-digit match code keyed by it, and a later mismatch is refused.

Checked and fine: pinned TLS (empty trust store, leaf SHA-256 compared inside the handshake, no cleartext, no redirects, unpinned calls limited to health and challenge and never with credentials); network security config (cleartext only to 127.0.0.1); FileProvider limited to cache/share, open, updates; only MainActivity is exported; pairing polling needs the client nonce; tokens are one-shot per approved request; pair requests rate limited and capped.

## Two-network test (agent and app on separate routed networks)

Lab: three Linux network namespaces, A `10.10.1.0/24` (scratch agent on `10.10.1.2:8765`), B `10.10.2.0/24` (API 36 emulator running the release APK, headless, own adb server), joined by a forwarding router namespace. Torn down afterwards.

| Check | Result |
|-------|--------|
| HTTPS health from B to A across the router | 200 |
| Ask-to-pair from the app in B; match code on phone and PC | Same code (3592 9433); agent saw the client as 10.10.2.2 |
| Approve on the PC, app finishes pairing, host shown "Connected securely", file list | Works; file content read |
| Router forwarding cut while a preview loads | Spinner while the link is down, no crash; the request completed once forwarding returned |
| Different agent (new certificate) at the same address | Refused: "Error: Certificate fingerprint mismatch"; app shows Offline |
| mDNS discovery across the router | Not tested (multicast does not cross routers, expected) |

Findings (UX, not security): a phone paired through approve-on-PC is browse-only, so opening a file shows a raw "HTTP 403" (native downloader error) instead of saying the computer has not allowed downloads for this phone; the pinning refusal shows "Error: Certificate fingerprint mismatch" with the workspace pill still saying "Connected". Fixed afterwards and re-verified in the same lab: `humanizeError` now explains a bare 403 and a certificate mismatch (the "Error:" prefix is gone), the Files header shows "Not trusted" and Home/Workspaces say "Computer identity changed · not connected" instead of "Connected"/"Offline"; recovery to "Linked" once the real agent is back. Tests in `pairingService.test.ts`.

## Flutter-era data import on a device (rfe-iib.4, favorites/settings)

The upgrade over real Flutter on the S23 only carried a host. To exercise the rest without running Flutter, the emulator (API 36, `adb root`) got a fresh install of the release APK plus a hand-written `FlutterSharedPreferences.xml` in the shared_preferences_android format (list marker + JSON array; boolean, long and string values): one host (no secure-store entries), a favorite, a bookmark with tag and colour, an offline pin, `app.themeMode=dark`, `app.amoledDark=true`, `app.density=compact`, `app.gridView=true`, `app.lowDiskThresholdBytes`.

Result on first launch: the host appears ("Old Desktop") and, having no stored pin, refuses to connect and says to re-pair (no unpinned fallback); the theme is dark with a pure black background; the bookmark shows under its host with the "work" tag. Favorites and pins use the same list reader as bookmarks and were not opened on screen (they need a reachable host). Found and fixed on the way: an install with settings but no saved computers skipped the whole import (`legacyImport.ts`, test added).
