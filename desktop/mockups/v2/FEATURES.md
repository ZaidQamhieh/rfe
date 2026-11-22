# RFE Desktop: feature inventory for the v2 mockups

Collected from `desktop/docs/user-guide.md` and the window code in `desktop/ui/` (pages.js, features.js,
fpages.js, fx2.js to fx6.js). Every v2 mockup reaches every line below; each design's feature map (button
in the top strip) says where.

## Shell
- Navigation to Files, Servers, Devices, Transfers, Search, Tools, History and Settings, plus **New connection**
- Search box (`/`), **Browse on** server switcher with state dot, response time and route
- Top actions: Refresh, Pair a phone (QR dialog), Theme, Command palette (`Ctrl+K`), Notifications, Account menu (Accounts, Sign out)
- Transfers dock: progress ring, waiting count, list grouped Active / Needs attention / Queued / Done, Pause all
- Snackbar messages with an action, first-run **Welcome** (pinned keys, approval of phones, transfers that resume)
- Window: light/dark/system theme, side panel that resizes and folds, every control reachable with Tab

## Connect, trust, sign in
- New connection: **Manual** (name, address, port), **Find on network** (mDNS, Scan again), **Pairing code**
- **Trust NAME?**: SHA-256 fingerprint, compare with `rfe status`, tick to confirm, **They match: trust**
- Sign in: **Account**, **Pairing code**, **Ask the PC** (match code, waits about 5 minutes), **Create a new device key**
- Key changed: red card, **Review key…**, **Trust new key**, **Keep it blocked**

## Servers
- Card per server: name, address, state (Connected, Connecting, Sign in, Lost, Offline, Key changed), route, system, agent version, response time, disk use
- **Open**, **Disconnect**, **Connect**, **Sign in**, **Wake computer** (wake-on-LAN)
- Menu: Browse files, Edit connection (name), Status…, Run diagnostics, Allowed roots…, Forget server
- **Status of NAME**: agent, system, platform, read-only, reachable at (LAN, Tailscale, MAC), uptime, disk, load (CPU, memory, bytes in and out; admin only), Refresh

## Files
- Locations (roots or drives) with notes (read-only agent, look-only login, more items than shown)
- Back, Forward, Up, path trail, type a path (`Ctrl+L`), Refresh, favorite star
- Filter chips All, Folders, Packages, Media, Archives, Over 100 MB; filter this folder; sort Name, Size, Modified; list or grid
- Columns: name, size, modified, permissions, item count; hidden files when the setting is on
- Selection (round box, Ctrl, Shift, Ctrl+A), bulk actions bar
- New folder, Rename (`F2`), Batch rename, Delete to Trash, Copy to…, Move to…, Compress to .zip, Extract here
- Properties with Permissions (octal mode), Checksum… (sha256, sha1, md5), Share link… (15 minutes, 1 hour, 24 hours), Copy path
- Make available offline, Add to favorites
- Preview: images (zoom, previous, next), text, archive listing, "No preview" with Download
- Text editor: Save, Save as copy, "This file changed on the server" (Overwrite, Save as copy)
- Side panel: **Details** and **Local files** (this computer); drag between them to upload or download
- Upload (`Ctrl+U`, Upload files chooser, drop from the desktop), Download (`Ctrl+D`)
- Keyboard: arrows, Enter, F2, Delete, Ctrl+Shift+N, F5, Alt+Left, Alt+Right, Alt+Up, Backspace

## Devices
- Server chooser
- **Pair a phone**: QR, 8-character code, time left, Copy code, Copy code data, Save QR, New code
- **Pairing requests**: device, takes over (if any), from, waiting, expires, match code `1234 5678`, **Accept…** with confirmation, **Reject**; red count on the navigation
- Device list: name, This app, sign-in method, last seen, what it may do, folder limit, Revoked
- **Permissions…**: browse, download, upload, change, delete, share links, see apps, start apps, read-only, folder limit
- **Revoke access**, **Remove from list**, **Activity**; Security events with **View all**

## Transfers
- Totals: current speed, active, queued, need attention, completed, moved this session
- Groups Needs attention, Active, Queued, Completed
- Per card: Pause, Resume, Cancel, Retry, Show in folder; states Waiting for a free slot, Checking the file, Checksum verified, paused because the server is lost, belongs to another agent
- Name conflict: Replace, Keep both, Skip, Apply to all
- Pause all, Resume all, Retry failed, Clear completed
- Transfer journal: unfinished transfers from the last session, Queue again

## Search
- Scope: This server, All connected servers, This computer; type chips; progress (items scanned, skipped servers)
- Results with server, folder, modified, size; saved searches (save, run, remove)

## Tools
- **Favorites & offline**: favorites (Add current folder, Open, Remove), recent files, available offline (progress, Stop keeping offline)
- **Storage**: per-server stats, roots usage, largest files (Scan again), duplicates
- **Trash**: select, Restore, Delete forever, Empty Trash, "Delete old items from Trash?"
- **Share links**: live links, expiry, Copy link, Turn off
- **Backup & sync**: one-way sync rules (New rule, Run now, Delete rule, last run)
- **Apps**: server chips, filter, app cards (name, description, category), Launch with confirmation, disabled cards and rights notes

## History
- Filters All, Transfers, Connections, Security, Errors; time and event filters; Clear history
- Audit log of the agent (admin): pairings, sign-ins incl. failed, revokes, share links, restarts, app launches
- Agent log (last ~200 lines, filter)

## Settings (in this order)
- Appearance: theme, row density, interface size 90/100/115/130%, open files with one or two clicks
- Language and accessibility: English, Deutsch, Español, العربية (right to left), reduce motion, high contrast
- Transfers: parallel 1 to 4, speed limit, when a name exists (Ask, Replace, Keep both, Skip), verify checksums, download folder (Change…, Reset)
- Connection: reconnect automatically, notifications inside the app
- Files: move deleted items to Trash, keep items in Trash 7/30/90 days, ask where to save downloads, show hidden files
- Security: approve new devices here, pairing code lifetime 2/5/10 minutes, pinned certificates, sign out all phones
- Desktop: close to the tray, start RFE when I sign in, desktop notifications, Test notification
- Backup: Back up… and Restore… (password-encrypted file)
- Troubleshooting: log detail, Agent log, Check the keystore, Device key (New key…)
- Help: keyboard shortcuts
- About: version, Details, Report a problem, Check for updates (Stable or Beta, Check automatically, Download with checksum), welcome tour

## Outside the window
- Tray menu: Open RFE, running transfers, Pause all transfers, Pair a phone…, Quit RFE
- Desktop notifications: finished or failed transfers, new device requests, updates

## Design rules (v2.1, after studying ObsidianUI)
- Emoji (Microsoft Fluent Emoji Flat, MIT, `emoji/LICENSE.txt`) mark *things*: file kinds, computers and their OS,
  phones, tool and settings sections, history kinds. Line icons mark *actions*. No emoji as decoration.
- Icons are duotone (closed shapes carry a 14% fill of their own color).
- Controls lift with a 1px ring plus a 1px drop; the primary button is raised; a press scales to .97; destructive
  confirmations are filled red; menus are frosted and settle from a slight squash; scrollbars show only on hover.
- Every icon-only button has a tooltip naming the action and its shortcut.
- Fewer controls: top bar is search-or-command field, Pair a phone, notifications, account (theme lives there);
  Refresh is automatic (F5); Forward is Alt+Right; the path trail is the path field; one action per server state;
  secondary actions sit in a ⋯ menu next to the primary one; group actions sit on their group; rows open on click.
