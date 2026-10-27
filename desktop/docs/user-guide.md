# RFE Desktop user guide

RFE Desktop is the PC-side partner of the RFE agent. It connects to an `rfe` you run on a PC,
checks that it is the agent you mean, signs this computer in, and lists the devices paired with that
agent. This guide covers installing it, trusting an agent, the three ways to sign in, where things are
kept, and what every message means.

## What you need

- The RFE agent (`rfe`) running on the PC you want to reach, and its address (`host:port`).
- Linux with a Secret Service provider (GNOME Keyring, KeePassXC or KDE Wallet) running and unlocked.
  See [keystore.md](keystore.md).
- An agent from `agent-v1.0.0` on: it has the certificate-bound sign-in proof (the app refuses older agents
  before it sends a password) and "Approve on the PC". See [agent-compatibility.md](agent-compatibility.md).

## Install

When a release exists, download the `.deb`, the `.rpm` or the `.AppImage` from the release page, or run the
installer, which works on any distribution, checks the download against `SHA256SUMS` and adds the app to your
launcher: `curl -fsSL https://raw.githubusercontent.com/ZaidQamhieh/rfe/master/tools/install.sh | sh -s desktop`.
On Arch, the AUR package `rfe-desktop-bin` installs it.

- Debian and Ubuntu: `sudo apt install ./RFE-Desktop_1.0.0_amd64.deb` (use the file's real name; a
  release's files have hyphens, a local build's `.deb` has a space in it). The package recommends
  `gnome-keyring` or `keepassxc`.
- Fedora, RHEL and openSUSE: `sudo dnf install ./RFE-Desktop-1.0.0-1.x86_64.rpm` (`zypper install` on openSUSE).
- Any Linux: make the AppImage executable (`chmod +x`) and run it. It does not bring a keystore. It uses the
  system's graphics and GTK libraries, which any desktop install has. On a minimal system add them first
  (Debian and Ubuntu: `sudo apt install libgtk-3-0 libegl1 libgl1 libgles2 libgbm1`); without them it stops at
  once with "error while loading shared libraries". Tested on a clean Ubuntu 24.04 with those packages.
- With no display (an SSH session, a text console) the app says it needs a graphical session and exits.

Check a download before you install it: each release lists `SHA256SUMS` and a software bill of
materials (`rfe-desktop.sbom.json`, CycloneDX) next to the packages. With the sums file and the package
in your downloads folder, `scripts/verify-download.sh SHA256SUMS "<package file>"` (from this repository)
prints `OK` or fails, and `sha256sum -c --ignore-missing SHA256SUMS` does the same without it.

Until then, build it yourself: [README.md](../README.md).

## The window

The window has a rail on the left (**Files**, **Servers**, **Devices**, **Transfers**, **Search**, **Tools**,
**History**, **Settings**, and the **New connection** button above them), a search box and a few buttons
along the top, and the page in the middle. On **Files** the middle holds the folder of the server you are
working on, and a side panel with two tabs, **Details** and **Local files** (this computer); drag items
from one to the other to upload or download. A **Transfers** pill at the bottom right shows what is being transferred: a ring that fills as files move, and
the number waiting. Click the pill, or the transfers button in the top bar, to open the list above it and again to fold it
away; it opens by itself when a transfer starts. The button at the top of the rail shows the names
beside the icons: beside the page from a wide window, over it (closed with `Esc` or a click elsewhere)
in a narrower one. Drag the handle between the list and the side panel to make the panel wider or
narrower; drag it far enough and the panel folds away, and the same handle (or the panel button in the top bar) brings it
back. Arrow keys move either handle, `Enter` folds or opens it, and a double click on the side panel's handle restores
its width. The list always keeps room for its names. The window is at
least 900 by 600 and opens at 1440 by 900; it follows the system's light or dark setting (or the one you
choose in **Settings**) and remembers its size and position. Starting the app a second time does not open
a second copy: the first window is brought forward. Press `/` to search, `Ctrl+K` for the command
palette, and `Esc` to close a dialog or menu. Every control can be reached with Tab, and menus move with
the arrow keys.

The first time the app opens, a short **Welcome** explains the three ideas it is built on (pinned keys,
approval of phones, transfers that resume) and offers the ways to add a computer. **Skip** or `Esc`
closes it; **Show the welcome screen** in the command palette brings it back.

## Connect and trust an agent

1. Press **New connection** (the plug button at the top of the rail, or the button on **Servers**). On
   **Manual**, give the computer a name, type its address (a host name or IP address) and the port, and
   press **Connect**. The app connects without sending any password and reads the agent's certificate.
2. A dialog, **Trust NAME?**, shows the certificate's SHA-256 fingerprint. On the PC run
   `rfe status` and compare the two. Press **They match: trust** only if they match exactly. This is
   the step that protects you from a different machine on the network answering in the agent's place:
   the agent's certificate is self-signed, so the fingerprint is the only identity it has. **Cancel**
   stops, and nothing was sent to the server.
3. After you trust it, the app remembers the fingerprint for that exact `host:port`. Every later
   connection to that address must present the same certificate or the app refuses it.

The other two tabs of **New connection** are shortcuts to the same steps. **Find on network** lists
agents on the same network that advertise themselves (mDNS, `_rfe._tcp`). **Add** only fills in the
address and starts the connection; it does not sign in or trust anything. Anyone on the network can
advertise a service with that name, so a listed agent is exactly as unverified as one you typed: you
still compare the fingerprint with `rfe status`. The search takes a few seconds and happens only
when you press the button. Agents on other networks, or on networks that block multicast (many guest and
corporate networks), are not listed; type their address. **Pairing code** takes the name, the address and
a code, and is described under Sign in.

If the certificate changes (the agent was reinstalled or its data folder was deleted), the server's card
turns red and asks you to compare again; its old files and logins are not used until you do. Only
continue if you know why it changed.

To make the app forget an agent, open **Servers**, press the three dots on its card and choose **Forget
server**, then **Forget** in the dialog. If you were signed in to it, you are signed out on this computer;
the device stays valid on the agent until it is revoked there. Unfinished transfers to that server are
cancelled.

## Sign in

After you trust an agent a **Sign in** dialog follows; a server that is not signed in also shows **Sign
in** on its card. It has three tabs.

| | Account | Pairing code | Ask the PC |
|---|---|---|---|
| You need | a username and password made with `rfe adduser` | an 8-character code from `rfe pair` or from **Devices** on another computer | someone at the PC |
| This computer can | list and manage every device | list and manage only itself | list and manage only itself |
| The password | is sent once over the pinned connection and never stored | not used | not used |

A pairing code is eight letters and digits (without 0, 1, I, O and similar look-alikes); the field
accepts it in lower or upper case, with or without the space.

**Ask the PC.** Press **Ask for approval**. The dialog shows a match code. The PC shows the same code in
a notification, in **Devices** of the app running there, or in `rfe pair requests`. Approve only
if the two codes are identical: the code is computed from the certificate this app saw, so a machine in
the middle shows a different one. Then **Accept** on the PC (or `rfe pair accept`). The dialog
waits for the answer for about five minutes and stops with a message if it is rejected, expires or times
out. **Not now** or `Esc` stops waiting.

In every case this computer is registered with the agent as a device with its own key. **Sign out**
(**Accounts** in the avatar menu) revokes that device on the agent and removes the saved login here. If
the agent cannot be reached, the app signs out on this computer only and says so; the login then still
works until it is revoked on the PC.

<!-- feature:pairing-codes -->
## Pair a phone

When you are signed in with the account, **Devices** shows **Pair a phone**: a pairing code and a QR
code for the RFE phone app, so you do not have to walk to the PC and run `rfe pair`. (The button
with the QR icon at the top of the window opens the same thing in a dialog.)

1. Choose the server at the top of **Devices** if you have several. The app asks the agent for a code
   when the page opens and shows it with the time it has left (2, 5 or 10 minutes, as set in Settings).
2. On the phone, scan the QR code (**RFE for Android → Add computer → Scan QR**) or type the address and
   the code. The phone is paired as an ordinary device with browse access only; give it more under
   **Permissions** in the list below.
3. **Copy code** and **Copy code data** put the code or the whole link on the clipboard; **Save QR** saves
   the picture to a file you choose. These are your choice: the app never copies a code by itself,
   because a code on the clipboard can be read by other programs and clipboard history.

A code works once. **New code** makes another one; it does not cancel an earlier one, which stays valid
until it is used or its time is up. The code leaves the window when you change page or sign out, and it
is never written to `state.json`, the keystore or the log. Only an account sign-in may do this: a computer
paired with a code or approved on the PC sees "This login cannot create pairing codes" and the agent
mints nothing.
<!-- /feature:pairing-codes -->

<!-- feature:pair-inbox -->
## Pairing requests

When a phone or another computer asks to be paired with **Ask the PC**, you can answer it here instead
of at the terminal. **Devices** lists every request the agent is holding, oldest first, each with:

| Column | What it is |
|---|---|
| Device | The name the asking device gave itself. It is whatever that device sent, so do not trust it on its own. If the request would take over a device that is already paired, a line under the name says which one. |
| From | The network address the request came from. |
| Waiting, Expires in | How long ago it was made, and how long you have left. A request lasts 2 minutes. |
| Match code | The code the agent worked out for this request. The device asking shows its own code. |

**Accept only if the code matches the one shown on the device asking.** The code is computed from the
certificate each side saw, so a machine in the middle shows a different one. **Accept…** opens a
confirmation showing the code again; confirm there. The device can then collect its login, and it starts
with browse access only. **Reject** answers at once. Either way the row leaves the list and the window
says what it did. A red dot on **Devices** in the rail counts the requests that are waiting.

The list refreshes by itself every few seconds while the page is open. Only an account sign-in can answer
requests. A computer paired with a code or approved on the PC sees a note instead of the list: sign out
and sign in with the account, or answer on the PC with `rfe pair accept` or `reject`. The agent holds
at most three waiting requests; a fourth device is told the PC is busy until you answer one or one
expires.

<!-- end feature:pair-inbox -->

## Settings

**Settings** lists its sections on the left, in this order, and shows the one you choose on the right
(the window remembers which while it is open). **Appearance** has the theme, row density, **Interface size**
and **Open files and folders**. **Language and accessibility** has the language, reduced motion and high
contrast. **Transfers** holds the number of parallel transfers, the speed limit, what to do when a name
already exists, the checksum check and the download folder (**Change…** opens the system's folder chooser,
**Reset** returns to the default); see **Transfers** below for what each does. **Connection** has automatic
reconnection and the notifications inside the app. **Files** has **Move deleted items to Trash**, how long to
**Keep items in Trash**, the choice to be asked where each download goes and to show hidden files.
**Security** has **Approve new devices here**, the **Pairing code lifetime**, the list of pinned
certificates and **Sign out all phones**. **Desktop** has **Close to the system tray**, **Start RFE when I
sign in**, **Desktop notifications** and a **Test notification** button. **Backup** has **Back up…** and
**Restore…** for the app's own settings (a file you choose; it holds no password, token or key).
**Troubleshooting** has the log detail, **Agent log**, **Check the keystore** and **Device key**. **Help**
shows the keyboard shortcuts. **About** has the version, **Details**, **Report a problem**, **Check for
updates** and the welcome tour.

- **Interface size** (90%, 100%, 115% or 130%) makes the whole window smaller or bigger on top of the scale
  the window already has: everything is drawn in units that grow with the window, from the smallest size
  (900 by 600) up, so a larger or higher-resolution screen shows larger text and controls without any
  setting. The smallest window grows with the size you pick.
- **Open files and folders** decides what a click does. **One click** (the default) opens a folder and
  shows a file that has something to show (a picture, text, or the list inside an archive); any other
  file is only selected, and its details appear in the side panel. The round icon at the left of a
  row, `Ctrl` and `Shift` select without opening. **Two clicks** makes a single click select and a double
  click open, as in most file managers.
- **Approve new devices here** decides whether this app asks you when a new device wants to join. Off, the
  request is not shown here; it still waits on the computer until someone answers it there. The app never
  approves a device by itself.
- **Pairing code lifetime** (2, 5 or 10 minutes) is how long a code made on **Devices** works. A code
  also works only once.
- **Sign out all phones** revokes every device of the connected computer that signed in with a code or
  was approved (not owner logins and not this app), after you confirm.
- **Move deleted items to Trash** off makes a delete on a computer permanent (the agent's trash is
  skipped). **Keep items in Trash for** (7, 30 or 90 days): when you open **Tools → Trash**, items
  deleted longer ago than that are listed in a question ("Delete old items from Trash?"); if you agree they are removed for good and the app says how many, and if you keep them they stay until you empty them.
- **Close to the system tray** hides the window instead of closing it, and transfers go on; the tray
  icon's menu has **Open RFE**, the number of running transfers, **Pause all transfers**, **Pair a
  phone…** and **Quit RFE**, and a click on the icon brings the window back. On a desktop with no tray
  (no status-notifier host) the switch is off and closing the window closes the app.
- **Start RFE when I sign in** adds or removes an autostart entry
  (`~/.config/autostart/rfe-desktop.desktop`) that starts this program hidden in the tray. It is only set
  up on Linux so far.
- **Desktop notifications** shows the finished/failed transfers, new device requests and updates as
  system notifications, only while the app is not the window in front. **Test notification** shows one
  now.
- **Check for updates** (also on **About**) asks GitHub's public list of releases whether a newer
  `desktop-v…` release exists on the chosen channel (**Stable**, or **Beta** which also offers
  pre-releases). **Download** saves the package that fits how the app runs (the AppImage when it runs from
  one or on Arch, the `.rpm` on Fedora, RHEL and openSUSE, else the `.deb`) in your download folder, and keeps it only if its SHA-256 is the one in the
  release's `SHA256SUMS`. The app never installs anything: use your package manager or run the AppImage.
  **Check automatically** (off by default) looks once a day while the app runs. Checking is the only time
  the app talks to GitHub.

The log level controls what the app keeps in memory (the last 500 events); nothing is written to disk.
**Agent log** shows the recent lines of the connected computer's own log, if this login may read it.
**Check the keystore** saves, reads back and removes a test secret in the system keystore. **New key…**
makes this computer a new device the next time you sign in; the saved logins are removed, and the old
device stays on each server until it is removed there.

**Report a problem** builds the diagnostics report for a bug report: versions, the servers' addresses and
pinned fingerprints, the recent errors and the log. It holds no password, token or key (secrets the app
handles are masked out of the log even if one slipped in). It does name the servers' addresses, and the
log names the hosts you tried, so read it before you post it in public. **Copy** puts it on the
clipboard.

<!-- feature:file-browser -->
## Files

**Files** shows the folder of the server you are working on (choose it from the **Browse on** menu in the
top bar, or open a server from **Servers**) and, in the side panel, a **Local files** tab for this
computer. A server that is not connected shows its state and the button that
fixes it (Connect, Retry, Trust, Sign in); a login that may not open any folder shows "No folder is open
to this login".

1. **Locations.** The first level lists where you may start: the folders the agent was confined to
   (`-roots`, or a per-device folder), or, when the agent has no folder limit, its drives. Choose one to
   open it. A note at the bottom of the list says when the agent is read-only, when this login may look
   but not change, and when the agent lists more items than are shown (the list shows the first 60
   pages of 500).
2. **A folder** shows each entry's name, size, modified time and permissions; a folder shows how many
   items it holds when the agent says. The path above the list is a trail: each earlier step is a
   button that goes there, and `Ctrl+L` lets you type a path. **Up**, **Back** and
   **Forward** move around; hidden files are shown only when **Settings → Files → Show hidden files** is
   on. **Refresh** reads the folder again. The chips (**All**, **Folders**, **Packages**, **Media**,
   **Archives**, **Over 100 MB**) narrow what is shown, and the **Name**, **Size** and **Modified**
   headings sort it; a folder always stays above the files. Switch between list and grid with the buttons
   at the top right of the list. An empty folder says "This folder is empty."
3. **A file** opens in **Preview** when it is an image or text the app can show; for anything else the
   preview says "No preview" and offers **Download**. A link's target is looked up first: the agent
   decides whether it may be followed, and a link that leaves the folders the agent allows is refused with
   the agent's own words (below).

**Keyboard.** Tab reaches the list once; Up and Down move between rows (with Shift or Ctrl they extend
or toggle the selection); Enter opens a folder or previews a file; `F2` renames; `Delete` deletes;
`Ctrl+A` selects all; `Ctrl+Shift+N` makes a new folder; `Ctrl+D` downloads and `Ctrl+U` uploads;
`Ctrl+L` lets you type a path; `F5` refreshes; `Alt+Left`, `Alt+Right` and `Alt+Up` (or `Backspace`) go
back, forward and up; `/` jumps to the search box and `Ctrl+K` opens the command palette. The menu of
the right mouse button (or of the three dots on a row) holds the same actions and moves with the arrow
keys. You can also drag items from one list to the other.

**Changes.** **New folder**, **Rename**, **Delete**, **Copy to…**, **Move to…**, **Compress to .zip**
and **Extract here** are offered only when the agent allows this computer to change files and is not
read-only; otherwise a note says so. A computer paired with a code starts with looking only; the owner
can allow more on the PC. **Delete** moves the entry to the agent's trash after a confirmation and
shows "Moved to Trash: NAME"; **Tools → Trash** lists what is there, and **Restore** puts an item back.
There is no permanent delete except **Discard** in the trash. If the agent refuses a change anyway, you
see its refusal and nothing changes. A new name is one plain name (no `/` or `\`, no control
characters, not `.` or `..`); it cannot move an entry to another folder. Copy, move, compress and
extract run on the agent; the window shows them as work in progress and refreshes the folder when
they finish.

**Properties** shows an item's size, times, permissions and, for a folder, how many items it holds. When
this login may change files, **Permissions** lets you set the mode (three or four octal digits, for
example `644`); the agent refuses a mode it does not accept and says so. **Checksum…** asks the agent to
compute a file's SHA-256 (or another algorithm it offers) so you can compare it with a published value.
**Share link…** makes a link that opens the file in a browser without a sign-in; it lasts from one minute
to seven days, you pick how long, and **Tools → Share links** lists the live ones and turns them off.

Every path is checked before it is sent (it must be absolute and have no `..` step), and the agent
checks it again against the folders it allows, including links: nothing outside them is listed.

<!-- end feature:file-browser -->

<!-- feature:multi-hosts -->
## Servers

You can keep several agents and use them at the same time. **Servers** shows one card for each agent you
have trusted: its name and address, the state (Connected, Connecting, Sign in, Lost, Offline, Key
changed), the route, system, agent version, response time and disk use, and the buttons that fit the
state.

- **Open** (on a connected server) shows its files. **Disconnect** stops using it without forgetting
  anything; **Connect** starts again, with no password, because the login is saved.
- **Sign in** (on a server that has none) opens the sign-in dialog with the account name filled in.
- The three dots hold **Browse files**, **Edit connection** (the name only: the address belongs to the
  trusted key, so a different address is a new connection), **Status…**, **Run diagnostics** and **Forget
  server**. **Forget server** deletes that server's saved login from the keystore, forgets its trusted
  certificate (you compare its fingerprint again if you ever add it back) and takes it off the list,
  after a confirmation. The device stays registered on the agent until it is revoked there: sign out
  first if you want that.
- **Add a server** (or **New connection**) is described under Connect and trust an agent. The login you
  had on the other servers stays saved.

Each server has its own certificate pin and its own login token. A token is only ever sent to the agent
it came from. A server that stops answering turns to **Lost**: its transfers are paused and resume when
it is back, and the app retries by itself unless **Reconnect automatically** is off in Settings.
**Status…** asks the agent for its health and, for an account sign-in, its load (see Status of a
server). A host that was saved by an older version shows up automatically; nothing is lost.
<!-- /feature:multi-hosts --><!-- /feature:multi-hosts -->

## What is kept, and where

| What | Where |
|---|---|
| Device signing key, login token | the system keystore |
| Agent address, its fingerprint, account name, device id, log level | `state.json` in the app's data folder (`~/.local/share/app.rfe.desktop/` on Linux; Settings shows the exact path), readable only by you |
| Password | nowhere |
| The saved hosts: name, address, account name, device id | `hosts.json` next to `state.json`, readable only by you. No secret is in it. A host's login token is in the keystore under its own entry |

If `state.json` is damaged the app says so and does not guess; delete the file to start over (you will
compare fingerprints again).

<!-- feature:device-actions -->
## Manage devices

**Devices** lists the phones and computers paired with the server you choose at the top, with when each
was last seen and what it may do. When you signed in with an **account**, each row has actions (a
computer paired with a code or approved on the PC sees only itself and gets none), under **Permissions**
and the three dots:

- **Permissions** opens the device's access: which file actions it may use (browse, download, upload,
  change, delete, make share links), whether it may see or start apps, a read-only switch, and a folder
  limit (an absolute path inside the agent's folders; empty means no limit). Only the settings you change
  are sent. A device that itself signed in with an account ignores the file permissions; the read-only
  switch and the folder limit still apply to it.
- **Revoke access** blocks the device: the agent refuses its login from then on, and the row stays in
  the list marked Revoked. It can only get back in by being paired or signed in again.
- **Remove from list** deletes the device's row for good (an active device is blocked at the same time).
- **Activity** opens **History** on the security events.

**Revoke access** and **Remove from list** ask in a dialog first. After every action the list is loaded
again from the agent.

The agent has no way to rename a device from another device: a device's name is the one it gave when it
signed in. (The PC-side `rfe` shows the same names.)

**This computer's own row** is marked **This app**. Revoking or removing it signs this window out, and
changing its own access can lock it out. The dialog then says "This is the sign-in this app uses" and the
app itself refuses the action unless that dialog confirms it. After it, the server asks you to sign in
again.

Messages of these actions:

| The window says | What it means and what to do |
|---|---|
| This login is not an admin session, so the agent will not change other devices. | The agent answered 403. Only a sign-in with the account (not a pairing code, not an approval on the PC) may revoke, remove or change other devices. Sign out and sign in with the account, or use `rfe revoke`, `remove` and `jail` on the PC. |
| That device is no longer on the agent | The agent answered 404: the device was removed (here, on the PC or from another computer) before the action arrived. The list has been loaded again; nothing else is needed. |
| This agent does not support that device action | The agent is too old for it. Update the agent, or use `rfe` on the PC. |
| This is the computer you are using | You tried to revoke, remove or change the access of this computer's own device without the second, confirming press. Press the button again to confirm, or leave it. |
| Could not tell which device is this computer | The saved login has no device id and the agent did not say which listed device is this one, so nothing was changed. Refresh the list and try again. |
| This agent is too old to sign in from this app | The agent only knows the older sign-in proof, which is not tied to its own certificate, so the app refuses it before any password is sent. Update the agent on the PC (the release that adds `proof: v2` to `/auth/challenge`), then try again. |
| This is the sign-in this app uses | The warning shown when you revoke or remove this computer's own row. Nothing has happened yet; confirm in the dialog to do it, or cancel. |
| Allowing a device to launch apps needs it to be allowed to view apps as well. | Turn on "See the apps on this PC" together with "Start approved apps". |
| No setting was changed | Saving needs at least one changed setting. |
| The folder limit must be one path, up to 4096 characters, or empty. | Type one absolute path, or clear the box for no limit. |
| The agent refused that change: ... | The agent said no, and the text after the colon is its reason, for example a folder limit that is not inside the agent's folders. Nothing was changed. |
| Revoked, Removed, Saved the access of | The action worked. "This computer is signed out; sign in again to continue." follows when it was this computer's own row. |
| The list could not be refreshed | The action worked, but loading the list again failed (the text after the colon says why). Press Refresh. |

## Troubleshooting

Find the message the window shows. Messages from the agent:

| The window says | Code | What to do |
|---|---|---|
| Wrong username or password. | `INVALID_CREDENTIALS` | Check the username and password (the password is case-sensitive). Accounts are made on the PC with `rfe adduser`. |
| That pairing code is wrong, expired or already used. Generate a new one on the PC. | `INVALID_CODE` | Pairing codes work once and expire. Run `rfe pair` on the PC for a new one. |
| The agent no longer accepts this sign-in attempt (it expired or was already used). Try again. | `INVALID_NONCE` | Press the button again. Each attempt asks the agent for a fresh challenge. |
| The agent could not verify this computer's device key. Try again; if it keeps failing, report it. | `INVALID_SIGNATURE` | Try again. If it keeps happening, open Settings, set the log level to Detailed, and report it. |
| The agent requires a device key proof that this app did not send. This is a bug in the app. | `DEVICE_KEY_REQUIRED` | The app and the agent disagree about the sign-in protocol. Update the app (see agent-compatibility.md); if both are current, report it. |
| The agent already knows a different key for this computer. Remove this computer from the agent's device list (rfe remove <id> on the PC), then sign in again. | `DEVICE_KEY_MISMATCH` | The agent has a different key on file for this computer's device id. Remove the old entry on the PC (`rfe devices`, then `rfe remove <id>`) and sign in again, or use "Create a new device key for this computer" on the sign-in screen, which registers this computer as a new device. |
| Too many attempts. The agent allows only a few sign-in and pairing attempts per minute. Wait a minute, then try again. | `RATE_LIMITED` | Wait a minute. The agent limits sign-in and pairing attempts. |
| The agent already has pairing requests waiting for approval on the PC. Answer them or wait for them to expire, then try again. | `PAIR_BUSY` | On the PC, answer the waiting requests (`rfe pair requests`, then `rfe pair accept` or `reject`), or wait for them to expire. |
| The agent no longer accepts this login. Sign in again. | `UNAUTHORIZED` | The agent revoked or forgot this computer's login. The app returns to the sign-in screen; sign in again. |
| This login is not allowed to do that. Sign in with the account, not a pairing code, to manage devices. | `FORBIDDEN` | A pairing-code login can manage only itself. Sign out and sign in with an account to manage every device. |
| The agent has no such item. It may have expired. | `NOT_FOUND` | The item expired or was already collected. Start the action again. |
| The agent refused the request as malformed. This is a bug in the app. | `BAD_REQUEST` | This is a bug in the app. Report it with the log level set to Detailed. |
| The agent had an internal error. Check its log on the PC. | `INTERNAL` | Look at the agent's log on the PC. |

Messages from this app:

| The window says | What it means and what to do |
|---|---|
| agent address must be host:port (put an IPv6 address in brackets, like [::1]:8765) | The address has a scheme, path or typo. Use `host:port`, for example `192.168.1.20:8765`. |
| cannot reach the agent securely | Nothing answered, or the connection failed. Check that the agent is running, the address and port are right, and no firewall is in the way. The text after the colon is the reason. |
| This agent's certificate is not the one you trusted | The agent presented a different certificate than the one you pinned. Do not sign in. If you did replace or reinstall the agent, open Servers and choose Forget server on its card, then add it again and compare the new fingerprint. If you did not, something else is answering at that address. |
| the agent presented no certificate | The address answers but not with TLS. It is not an RFE agent. |
| the agent reports a different fingerprint than the pinned one | The agent's own report disagrees with the certificate it presented. Do not continue; compare fingerprints again. |
| fingerprint must be 64 hex characters | Internal check; reconnect from the first screen. |
| unexpected response | The address answered with something that is not an RFE agent reply. Check the address and the agent version. |
| unexpected request id, unexpected device id, unknown pairing status | The agent sent a value the app refuses to use. Update the agent and the app; if both are current, report it. |
| enter the pairing code | The pairing code field is empty. |
| This agent is too old to approve a new computer from the PC | Approval needs `agent-v1.0.0` or newer. Pair with a code or sign in with an account, or update the agent. |
| The request was rejected on the PC. | The owner pressed reject. Ask again if it was a mistake. |
| The request expired or was already used. Ask again. | The request ran out of time, or its answer was already collected. Press the button again. |
| The request timed out. Ask again. | Nobody answered on the PC in time. |
| The PC approved this computer, but saving the login failed | The agent hands out the approval only once and the keystore refused to keep it. Fix the keystore ([keystore.md](keystore.md)), then ask again. |
| not signed in | Sign in first. |
| is no longer a trusted agent; connect and compare its fingerprint again | The pin was forgotten while a login for it existed. Connect to the agent again from the first screen. |
| Sign out first; the saved login belongs to the current device key. | A new device key can only be made while signed out. Making one also removes the logins saved for your other agents (their devices stay registered on those agents until removed there), so you sign in to each again. |
| Signed out on this computer only | The agent could not be told. Revoke this computer on the PC (`rfe revoke <id>`) so its old login stops working. |
| This transfer belongs to <agent> | The transfer was started on another agent and you have since switched to a different one. It does nothing to the wrong computer. Switch back to that agent (Servers) and press Retry, or cancel it. |
| is damaged (...); delete it to create a new device identity | An `identity.json` left by an early version cannot be read. Delete that file; the app makes a new device key at the next sign-in. |
| cannot start network discovery, network discovery stopped | The app could not listen for mDNS answers (no network interface, or the system refused). Type the agent's address instead. |
| unknown log level | Choose one of the levels in the list. |
| is damaged (...); delete it to start over | `state.json` cannot be read. Delete it (Settings shows where) and set the app up again. |
| read, create, open, write or rename a path failed | The app's data folder cannot be used: check that it exists and that you own it. |
| tls config, http client, random | The system could not set up a secure connection or random numbers. Report it. |
| this server is not in the saved list | The server was removed (perhaps from another window) before the action ran. Open Servers again. |
| enter a name for the host | The new name is empty. Type a name, or press Cancel. |
| the name can be at most 64 characters | Use a shorter name. |
| the name cannot contain control characters | The name has a line break or another control character. Type it again on one line. |
| `hosts.json` is damaged (...); delete it to start over | The list of saved hosts cannot be read. Delete `hosts.json` (Settings shows the folder): the list is rebuilt from the agents you trusted, and every login stays where it is. Until then signing in to a second agent is refused so the list is not overwritten. |
| `hosts.json` was written by a newer version of this app | A newer app wrote the list. Update this app, or delete `hosts.json` to start over as above. |
| Signed out on this server. Sign in to continue. | You switched to a server that has no saved login (you signed out of it, or only trusted it). Sign in; the account name is filled in. |
| Type the new server's address. Your current login stays saved. | Shown when you press **Add a server**. Nothing was changed on the server you were using. |
| This login cannot create pairing codes. Only a session signed in with the account can; a computer paired with a code or approved on the PC cannot. | The agent refused (`FORBIDDEN`) because this computer is not an account session. Sign out and sign in with the account, or run `rfe pair` on the PC. |
| This pairing code has expired. Generate a new one. | The code's lifetime (2, 5 or 10 minutes, set in Settings) is up and it was removed from the window. Press **New code**. |
| unexpected response: the agent sent no pairing code | The agent answered without a code. Update the agent and the app; if both are current, report it. |
| This login cannot answer pairing requests. | This computer was paired with a code or approved on the PC, and the agent lets only an account answer. Sign out and sign in with the account, or use `rfe pair accept` or `reject` on the PC. |
| This agent is too old to list pairing requests | Answering from the window needs `agent-v1.0.0` or newer. Update the agent, or answer on the PC. |
| The agent holds at most 3 waiting requests. | Shown when three are waiting: a fourth is refused (the device asking sees that the PC is busy) until you answer one or one expires. |
| Accepted ..., Rejected ... | Your answer reached the agent. An accepted device collects its login on its own. |
| That request already expired or was answered on the PC. | The request ran out of time, or someone answered it (at the PC, or from another window) a moment before you did. Nothing was changed by your press. Ask the device to try again if it was a good one. |

<!-- feature:file-browser -->
Messages from the file browser:

| The window says | What it means and what to do |
|---|---|
| The agent says: ... (CODE) | The agent refused or could not do what you asked, and this is its own wording and code. Common ones: `FORBIDDEN` (the path, or a link, is outside the folders the agent allows; nothing was listed or changed), `CAPABILITY_DENIED` (this computer is not allowed that action; the owner can allow it on the PC), `READ_ONLY` (the agent is in read-only mode), `CONFLICT` (that name already exists), `PATH_NOT_FOUND` (it was moved or deleted; press Refresh). |
| a path must be absolute (start with / or a drive letter) | The app refused to send a path that is not absolute. Open it from the list instead of typing it. |
| a path cannot be empty, hold a NUL character or a .. step, or be longer than 4096 bytes | The app refused to send that path. A `..` step is never sent; the agent would clean it, but the app does not rely on that. |
| a name must be 1 to 255 bytes, with no / or \, no control characters, no space at either end, and not . or .. | The name you typed for a new folder or a rename is not one plain name. Choose another. |
| the agent did not say whether the delete worked | The agent answered without a result for the entry. Press Refresh to see whether it is still there. |
| unexpected route | Internal check on the address the app asked for. Report it. |
| No folder is open to this login. | The agent shows this computer nothing to browse. The sentence after it says why: the agent confined this computer to a folder it does not allow (change it on the PC), this computer is not allowed to browse (allow it on the PC), or the agent has not shared a folder with it. |
| This folder is empty. | There is nothing in it. |
| The agent lists more items than are shown. | The folder has more entries than the window reads (60 pages of 1000). Use search to find the rest. |
| The agent is read-only, so nothing here can be created, renamed or deleted. | The agent was started with `-read-only`. |
| This computer may look but not change files on this agent. | This login has no modify or delete permission. The owner can allow it on the PC. |
| Moved to Trash: NAME | Done. Restore it from the Trash view. |
| New folder name | The label of the box for a new folder (a rename says "New name for NAME"). |

<!-- end feature:file-browser -->
Messages about the system keystore (`the OS keystore ...`, `no OS keystore answered`) are explained
in [keystore.md](keystore.md).

## Messages of the tools and dialogs

| The window says | What it means and what to do |
|---|---|
| Connect to a server first. | The action needs a connected server and none is selected or connected. Connect one from **Servers**. |
| Open a connected server first to upload into it. | Uploading goes to the server shown on the left of **Files**; open one that is connected. |
| Open a connected server in Files first. | Adding a favorite or a folder rule needs a connected server open in **Files**. |
| Select one file on a connected server in Files first. | The action works on exactly one file of a server (for example **Checksum…**). |
| Pick a server first: switch the main view to a server. | The left list shows **This computer**; choose a server in the **Browse on** menu first. |
| Copying between two servers isn’t supported. | Download to this computer first, then upload to the other server. |
| Nothing to transfer | Nothing is selected, or the selection holds no files. |
| Open the folder in Files to extract. | **Extract here** works on an archive you opened in **Files**. |
| Trash is already empty | There is nothing in the agent's trash to restore or discard. |
| Removed N items older than D days from Trash | You opened Trash, agreed to "Delete old items from Trash?" and **Keep items in Trash for** removed the items deleted longer ago than that. |
| RFE starts hidden in the tray when you sign in | **Start RFE when I sign in** was switched on. |
| RFE no longer starts when you sign in | It was switched off. |
| The notification was not shown | **Test notification** was refused: notifications are not available in this session. |
| the system did not show the notification | The desktop has no notification service (or it refused). Start one (for example the one of your desktop environment) and try again. |
| that is not a kind of notification | The window asked for a kind of notification the app does not have. Report it. |
| a notification needs a title | The window sent an empty notification. Report it. |
| Starting with the session is only set up on Linux so far. | **Start RFE when I sign in** works only on Linux in this version. |
| cannot remove the autostart entry | The file in `~/.config/autostart` could not be removed. The text after it says why. |
| cannot find where this program is installed | The app could not tell where its own program file is, so it cannot write the autostart entry. |
| Cannot reach GitHub. Check the connection and try again. | **Check for updates** needs the internet. |
| GitHub did not answer in time. Check the connection and try again. | GitHub was too slow; try again. |
| GitHub answered N when asked for the releases. | GitHub refused the request (a 403 or 429 means too many requests from your address: wait a while). |
| GitHub's list of releases could not be read | GitHub's answer was not a list of releases. Try again later. |
| GitHub sent more than was expected | The answer was larger than the app accepts, so it was dropped. |
| that release is no longer listed | The release was removed or replaced; check for updates again. |
| that release has no such package | The release has no file of that name. |
| that is not a package this app fetches | Only a `.deb`, an `.rpm` or an `.AppImage` is fetched. |
| that package has a size this app will not fetch | The package is empty or larger than 400 MiB. |
| that release has no SHA256SUMS file, so the package cannot be checked | The release has no checksum list, so nothing was downloaded. |
| the release's checksums do not list that package | The checksum list does not name the package, so nothing was downloaded. |
| the checksum file of that release could not be fetched | GitHub did not give the `SHA256SUMS` file. Try again. |
| The package does not match the release's checksum. It was deleted. | The downloaded bytes differ from the release's SHA-256, so the file was deleted. Download again; if it repeats, do not install it. |
| the package ended early; try again | The connection stopped before the whole package arrived. |
| the package could not be put in place | The package could not be given its name in the download folder. Check its permissions. |
| that is not a version | The window sent something that is not a version. Report it. |
| Deleted forever: | The item was discarded from the trash and cannot come back. |
| That folder can’t be opened: | The folder is not allowed to this login, or no longer exists; the text after the colon is the agent's reason. |
| Could not start: | The window could not read its saved settings or hosts when it opened. The text after the colon says why; restart the app, and report it if it stays. |
| This sign-in cannot change what a device may do. | Only an account sign-in may change a device's permissions. |
| The MAC address of | **Wake computer** needs the agent's MAC address, which it reports when it is on; without it nothing can be sent. |
| Wake signal sent to | A wake-on-LAN packet was sent. It does not say whether the computer woke; give it a minute and press **Retry**. |
| The keystore works: a test secret was saved, read back and removed. | **Check the keystore** succeeded. |
| The keystore is not working. | **Check the keystore** failed; the text after it says why (see keystore.md). |
| A new device key will be created the next time you sign in. | **New key…** worked. Sign in to each server again; the old device stays on each server until it is removed there. |
| Tick the box to confirm you compared the fingerprints. | Trusting a server needs the box ticked, so a fingerprint is never accepted by accident. |
| Describe the problem first | **Report a problem** needs a few words before it makes the report. |
| The clipboard is not available here | The system refused to copy. Select the text and press Ctrl+C. |
| a search needs 1 to 256 characters | The search box is empty or longer than 256 characters. |
| choose between 1 and 1000 items | A copy, move, compress or delete takes 1 to 1000 items at a time. |
| the checksum must be sha256, sha1 or md5 | The algorithm the agent computes is one of these three. |
| a permission mode is 3 or 4 octal digits, for example 0755 | Type the mode as octal digits, for example `644` or `0755`. |
| this file is not text, or is too large to open here | The preview shows text files up to a size limit; download the file to open it. |
| a share link lasts from 1 minute to 24 hours | Choose a duration within that range (the window offers 15 minutes, 1 hour or 24 hours). |
| that is not a share link id | The link was already removed or never existed. |
| a MAC address looks like aa:bb:cc:dd:ee:ff | Type the MAC address with colons. |
| a path must be absolute, with no .. step | A path on this computer starts with `/` and has no `..` step. |
| this item cannot be renamed | Drives and the root have no name to change. |
| a drive or the root cannot be deleted | Choose a folder or a file. |
| This is not a file. | A preview or a read was asked for something that is not a regular file. |
| This file is too large to show as text (over 1 MB). | Download it to read it. |
| This file is not text. | The preview only shows text; download the file. |
| This picture is too large to preview (over 8 MB). | Download it to see it. |
| This file would start a program, so it is not opened from here. | The app never runs programs from a list; open it in your file manager if you mean to. |
| That folder does not exist on this computer. | The download folder you chose is gone; choose another in **Settings → Transfers**. |
| Too many files with that name in the folder. | The folder already has `name (1)` to `name (999)`; clean it up. |
| pick a folder or a file, not the root | Upload a folder or a file, not the whole disk. |
| that folder holds too many files to upload at once | Upload it in smaller parts. |
| The agent did not confirm that the change was made. | The agent answered a copy, move or restore without saying whether it worked. Refresh the folder and check; try again if nothing changed. |
| items failed: | A copy, move or restore of several items had refusals. The text after the colon is the agent's reason for the first one (for example a name that already exists); the others may have worked. |

<!-- feature:app-catalog -->
## Apps on the PC

**Tools → Apps** lists the apps the PC running the agent offers (on Linux, the applications in its
menu); the chips above the list choose the server. Each card shows the app's name, description and
category. **Launch** opens a dialog that names the app and the PC; **Launch** in the dialog opens the
app on the PC's own screen. Nothing runs on this computer. This app sends the agent only the app's
catalog id, never a command or a path, and the agent decides what that id means; the launch is recorded
in the agent's security log.

What it takes:

- The owner must give this computer two rights on the agent: *view apps* (to see the list) and *launch
  apps* (to start one). Neither comes with an account sign-in or a pairing code. Without *launch apps*
  the list is shown and the Launch buttons are disabled, with a note.
- Someone has to be signed in to a graphical desktop on the PC, and the PC needs `gio` for the agent to
  start anything.
- A card that is dimmed is listed but has no way to start.
- "The PC lists no apps." means the catalog is empty, not that an error happened.

Success means the PC's launcher accepted the request, not that the app finished starting.

Messages from the agent for these two screens:

| The window says | Code | What to do |
|---|---|---|
| This computer is not allowed to see the PC's apps. On the PC's agent, turn on app viewing for this computer, then refresh. | `APP_VIEW_FORBIDDEN` | The owner has not given this computer the right to see the PC's apps. An account sign-in does not include it. Turn on "view apps" for this computer's device on the agent (an account session can change it), then press Refresh. |
| This computer may see the PC's apps but not start them. On the PC's agent, turn on app launching for this computer. | `APP_LAUNCH_FORBIDDEN` | This computer may list apps but not start them. Turn on "launch apps" for it on the agent (it needs "view apps" too). The Launch buttons stay hidden until then. |
| That app is no longer in the PC's catalog. It may have been uninstalled. Refresh the list. | `APP_NOT_FOUND` | The app was removed from the PC after the list was loaded. Press Refresh. |
| The PC has no way to start that app. It is listed but cannot be launched from here. | `APP_NOT_LAUNCHABLE` | The PC knows the app but has no way to start it (for example its desktop entry has no command, or the program it names is missing). Nothing can be done from here. |
| The PC is already starting another app. Wait a moment, then try again. | `APP_LAUNCH_BUSY` | The agent starts one app at a time. Wait a few seconds and press Launch again. |
| Nobody is signed in to a graphical desktop on the host, so there is nowhere to open the app. Sign in at the PC, then try again. | `NO_INTERACTIVE_SESSION` | The agent only opens apps on a screen someone is signed in to. Sign in to the PC's desktop, then try again. |
| The PC's app launcher is not available (on Linux it needs the gio tool). Install it on the PC, then try again. | `APP_LAUNCH_UNAVAILABLE` | The agent's launcher is missing. On Linux the PC needs `gio` (the `glib2` or `libglib2.0-bin` package) on the agent's PATH. |
| The PC tried to start the app and could not. Check that it still works on the PC. | `APP_LAUNCH_FAILED` | The launcher accepted the request but could not start the program. Try it on the PC itself. |
| Too many requests for the app list. Wait a minute, then refresh. | `APP_CATALOG_RATE_LIMITED` | The agent allows 60 list requests a minute per computer. Wait a minute. |
| Too many app launches. The agent allows only a few per minute. Wait a minute, then try again. | `APP_LAUNCH_RATE_LIMITED` | The agent allows 5 launches a minute per computer. Wait a minute. |
| The PC's operating system does not support the app catalog yet. | `APP_CATALOG_UNSUPPORTED` | The agent runs on a system with no app catalog. Nothing can be done. |
| The agent refused the app id as malformed. This is a bug in the app. | `BAD_APP_ID` | The app sent an id the agent does not accept. This is a bug in the app. Report it with the log level set to Detailed. |

Messages from this app:

| The window says | What it means and what to do |
|---|---|
| unexpected app id | The app refused to put an id that does not look like `app_` and 64 hex digits into a request. Refresh the list; if it keeps happening, report it. |
| unknown launch status | The agent answered a launch with something other than "started". Update the agent and the app; if both are current, report it. |
| This agent has no app catalog. Update the agent on the PC, then try again. | The agent is older than the app catalog and answers `/apps` with a plain 404. Update it. |
| Asked the PC to open (app name). It can take a moment to appear on its screen. | Not an error: the PC's launcher accepted the request. If nothing appears, check the PC's screen and that the app starts there. |
| Cannot be launched, Launching not allowed | A row without a Launch button: the PC has no way to start that app, or this computer lacks the *launch apps* right (see above). |
<!-- feature:health-metrics -->
## Status of a server

**Status…** (in the three dots of a server's card, or **Status of NAME** in the command palette) shows
what the agent reports about itself and, for an administrator, how busy the PC is.

- **Agent**, **System**, **Platform**: the agent's name and version, its operating system, and whether
  it is read-only. **Reachable at** lists the addresses it reports (LAN, Tailscale, MAC).
- **Uptime** and **Disk**: how long the agent has been running, and the used and total space of its data
  folder.
- **Load** (administrators only): processor and memory in use (percent, at the moment of reading) and
  the bytes received and sent since the agent started (shown in decimal units: 1 KB is 1000 bytes).
- **Refresh** reads again. The window keeps only the last reading, and the agent keeps no history.

A value the agent did not send reads "not reported" (an older agent, or one that withheld it) instead
of a made-up zero. The agent does not report the state of its helper programs, so none is shown.

| The window says | What it means and what to do |
|---|---|
| Metrics are for administrators. | This computer signed in with a pairing code or was approved on the PC, so the agent answers the metrics request with 403 (`FORBIDDEN`). The rest of the screen still works. Sign out and sign in with the account (`rfe adduser`) to see the metrics. |
| No metrics were reported. | The agent answered the health request but not the metrics one; the text after it says why (for example it timed out). Press Refresh to try again. |
| Disk and uptime were not reported | The agent's status request failed; the reason follows. The health part is still current. |
| The agent did not answer within 5 seconds. | The agent did not reply in time. It may be asleep, busy, or on another network. Check that it is running and the address is right, then press Refresh. Each of the three requests has its own five second limit and they run together, so the screen never waits longer. |
| unexpected response: the agent's health answer does not say ok | The address answered with something that is not an RFE agent's health reply. Check the address and the agent version. |
| not reported | That value was missing from the agent's answer. |



<!-- feature:audit-logs -->
## Audit and logs (admin)

**History** lists what this app and the servers have done: transfers, connections and, with the
**Security** filter, what the agent has recorded. The audit part needs an account sign-in: the agent
refuses it for a computer paired with a code or approved on the PC, because the trail describes every
device, not just this one.

- **Audit log**: pairings, registrations, sign-ins (failed ones included), device revokes, removals and
  changes, share links, agent restarts and app launches, newest first. File operations are deliberately
  not recorded. The agent keeps the most recent 5000 events. **Devices → Security events** shows the latest few; **View all** opens them in History.
- **Agent log** (**Settings → Troubleshooting → Agent log**): the last ~200 lines of the agent's own log,
  which it reads from the systemd journal (`journalctl --user -u rfe.service`). It is empty when
  the agent does not run as that service. The box above it narrows what is already loaded; it does not
  ask the agent again.
- The filters of History narrow what is already loaded; to look further back, widen the filters
  first.
- Times are shown in this computer's local time. Point at a time to see the exact timestamp the agent
  recorded.
- The text of an event (the name of a device or of an account somebody tried to sign in with) comes from
  whoever sent it, so the app shows it as plain text, replaces control characters, and cuts long text with
  an ellipsis.

| The window says | What it means and what to do |
|---|---|
| This login cannot read the audit log or the agent log. | The agent refused (403 FORBIDDEN). Sign out and sign in with an account (`rfe adduser`), not a pairing code or an approval on the PC. |
| The audit log is empty. | The agent has recorded nothing yet. |
| No loaded event matches these filters. | Nothing already loaded fits the Event and Time choices. Widen them. |
| No log line matches these filters. | Nothing in the agent's last lines fits the text and Time. Clear them. |
| The agent's log is empty. | The agent could not read a journal. Run it as the `rfe` service (`rfe setup`), or read its output where you started it. |
| unexpected audit cursor, the page size must be | A bug in the app: it asked for a page the agent does not have. Report it with the log level set to Detailed. |


Any other message here (the agent is unreachable, the login was revoked) is one of the messages in the
tables above.
<!-- /feature:audit-logs -->

<!-- feature:transfers -->
## Transfers

**Transfers** shows every upload and download as a card with a progress bar, grouped as **Needs
attention**, **Active**, **Queued** and **Completed**, with the current speed and totals above. The
**Transfers** pill on the other pages shows the same cards while you browse. Everything is done by the app's core
over the same pinned connection as the rest; the window only shows the progress.

- **Download:** select files in a server's list and press **Download** (or `Ctrl+D`), or drag them onto
  **Local files**. They are saved in your download folder under `RFE Desktop` (`~/Downloads/RFE
  Desktop` on most systems; **Settings → Transfers** shows the exact folder and can change it), or the
  app asks for a folder each time if **Ask where to save downloads** is on. Only the file's name is
  used: folders in the path, dots at the start, control characters and a very long name are removed, so
  a download can never be written outside that folder or hidden. A name that is already taken is never
  replaced; the new file is saved as `name (1)`, `name (2)` and so on. The file only gets its real name
  when it is whole, and the app then asks the computer for the file's SHA-256 and compares it with the
  saved copy; a match is shown as "Checksum verified", a difference discards the copy.
- **Upload:** select files in **Local files** and press **Upload** (or `Ctrl+U`), drag them onto a
  server's list, or press **Upload files** to choose files with the system's file chooser. The path must
  be a regular file. A path that is itself a symbolic link is refused; the app does not follow it. The file
  keeps its name, and a file of that name on the computer is only replaced when you choose Replace (see "When a name already exists" below). The computer checks every
  piece and the whole file before the file appears under its name.
- **Pause** keeps what has arrived (a download's unfinished file, an upload's session on the computer)
  and frees the slot; **Resume** continues from there. **Pause all** and **Resume all** do the same for
  every transfer. **Cancel** stops a waiting or running transfer: a download's unfinished file is deleted
  and an upload's unfinished copy is removed from the computer; on a paused transfer it discards what was
  kept. **Retry** (after a failure or a cancel) starts again: a download continues from what it already
  has, an upload sends only the pieces the computer is missing. **Retry failed** retries every failed
  transfer and **Clear completed** removes finished and cancelled transfers from the list.
- **Settings → Transfers** change how transfers run, and they count for transfers already running:
  *Parallel transfers* (1 to 4, two by default) is how many move at once, the others say "Waiting for a
  free slot"; *Speed limit* (none, 10, 25 or 50 MB/s) is shared by all transfers together;
  *Verify checksums* off skips the SHA-256 comparison of a finished download (an upload is always
  checked by the computer); *When a name already exists* decides what happens when a file of that name is
  already in the download folder or on the computer: **Ask** (the transfer waits on a card with
  *Replace*, *Keep both* and *Skip*, and *Apply to all* answers every waiting transfer), **Replace**,
  **Keep both** (the new file is called `name (1)`, `name (2)` and so on) or **Skip**. A transfer that
  waits for your answer does not hold up the others. The list is kept only while the
  app runs. A download that was running when the app was killed leaves a hidden file starting with
  `.rfe-` and ending `.part` in the download folder; delete it.
- A download of a very large file ends with "Checking the file" while the computer works out its
  SHA-256; an upload ends the same way while the computer checks the whole file.
- If a server is lost, its transfers pause by themselves and continue when it is back.

Messages about transfers from the agent (the window shows this wording instead of the generic one for
these codes):

| The window says | Code | What to do |
|---|---|---|
| The computer has no file at that path. It may have been moved or deleted. | `PATH_NOT_FOUND` | Check the path. File names are case-sensitive on Linux. |
| This login may not use that path on the computer. It is outside the folders it can reach. | `FORBIDDEN` | The agent limits this device to certain folders. Use a path inside them, or change the limit on the PC (`rfe jail`). |
| The agent or this device is read-only, so it cannot accept uploads. | `READ_ONLY` | Turn read-only mode off on the PC. |
| This device has not been allowed to do that. Allow it on the computer, in the device's access settings. | `CAPABILITY_DENIED` | A pairing-code or approved device may be limited to browsing. Allow downloads or uploads for it on the PC. |
| A file with this name already exists on the computer. | `CONFLICT` | Settings → Transfers → "When a name already exists" decides: ask, replace, keep both or skip. Without an answer an upload never replaces a file. |
| The file changed or was damaged while it was uploading. The upload was discarded; try again. | `HASH_MISMATCH` | Do not change the file while it uploads, then retry. |
| A piece of the file was damaged in transit three times in a row. Try again. | `CHUNK_HASH_MISMATCH` | The network is corrupting data. Retry; if it repeats, check the connection. |
| The file is too large for the computer to accept. | `PAYLOAD_TOO_LARGE` | The agent accepts at most 64 GiB per upload. |
| The computer has too many uploads open. Wait for one to finish, then retry. | `RESOURCE_LIMIT` | The agent allows a few open uploads per device and per computer. Wait, then retry. |
| The computer is still finishing the previous attempt. Retry in a moment. | `TRANSFER_ACTIVE` | The agent is still busy with the last attempt. Retry after a few seconds. |
| The computer closed this upload. Retry starts it again from the beginning. | `TRANSFER_NOT_OPEN` | The agent no longer holds the partly uploaded file. Retry. |

Messages about transfers from this app:

| The window says | What it means and what to do |
|---|---|
| enter the path of the file on the computer | The download path is empty. |
| enter the full path of the file on this computer | The upload path is empty. |
| enter the folder on the computer to upload into | The destination folder is empty. |
| that path on the computer is not usable | The path holds a character that cannot be sent or is longer than 4096 bytes. |
| cannot use the downloads folder | The app could not create `RFE Desktop` in your download folder. The text after it says why; check that you own the folder. |
| cannot find your Downloads folder | The system does not say where your download folder is. Create `~/Downloads` or set `XDG_DOWNLOAD_DIR`. |
| give the full path of the file, starting with / | The upload path is relative. Type the whole path. |
| that path is a symbolic link; type the real path of the file | The app does not follow a link. Type the path of the file the link points to. |
| that is not a regular file | The path is a folder, a device or something else that is not a plain file. |
| cannot read that file | The file is missing or you may not read it. The text after it says which. |
| the file changed while it was being opened; try again | The path was replaced by another file at that moment. Try again. |
| That file changed while it was being sent. Send it again. | The file was edited after the upload started, so it was stopped before the computer checked it. Do not change the file while it uploads. |
| cannot write | The app could not write the unfinished download. Check free space and permissions of the download folder. |
| The downloaded copy does not match the file on the computer | The saved bytes differ from the file's SHA-256 on the computer, so the copy was deleted. Start the download again. If it repeats, the file may be changing. |
| The download could not be checked against the computer | You have verification turned on, but the computer did not answer the check (network trouble, a busy agent or an unexpected reply). The file was not saved as finished. Retry the download, or turn verification off in Settings to accept it unchecked. |
| The file is no longer on the computer, so it could not be checked | You have verification turned on, and the file was removed or renamed on the computer after the download finished. The download was not saved as finished. Download it again if it still exists. |
| the downloaded file could not be put in place | The finished file could not be given its name. Check the download folder's permissions. |
| the agent ended the download early | The connection ended before all the bytes arrived. Press Retry; the download continues. |
| no such transfer | The transfer is no longer in the list. |
| Skipped: a file with this name already exists | You (or the setting "When a name already exists") chose Skip, so the old file was left and nothing was transferred. |
| that transfer is not waiting for an answer | The question was already answered or the transfer ended. |
| that is not a way to settle a taken name | The window sent something other than ask, replace, keep or skip. Report it. |
| a folder has that name | You chose Replace but a folder, not a file, has that name in the download folder. Rename or remove the folder. |
| every numbered name on the computer is taken | Keep both found `name (1)` to `name (1000)` all taken on the computer. Clean up the folder. |
| that transfer cannot be retried now | Retry works only on a failed or cancelled transfer. |
| a background task failed | Reading or checking a file stopped unexpectedly. Report it with the log level set to Detailed. |

In the list: "Waiting for a free slot" is a transfer queued behind the running ones, "Checking the file"
is a finished transfer being verified, and "Checksum verified" means the server confirmed the
file's SHA-256. A download from an older agent may finish without that line because it could not answer
the check.
