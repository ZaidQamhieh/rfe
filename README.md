# RFE

Remote File Explorer: a mobile app that turns your phone into a full graphical file explorer for your Windows, macOS, and
Linux computers — browse, manage, and transfer files over a Finder/Explorer-style GUI, with no SSH
or terminal required.

- **`mobile/`** — React Native (Expo) Android app; it upgrades the old Flutter v1.42 app in place (see `docs/upgrade-to-react-native.md`)
- **`agent/`** — Go host service that runs on each Windows/macOS/Linux computer
- **`protocol/`** — OpenAPI 3 contract shared by both sides (source of truth)
- **`docs/`** — architecture and setup guides

The app connects to the agent over HTTPS/TLS. On a local network, connect by IP address or
hostname without a VPN. **Tailscale is optional** for remote access; the app can also save a
direct HTTPS hostname or IP as a fallback route. That route works only after the PC owner configures
public DNS (if needed), router NAT, and firewall access to the agent. The app never opens ports or
changes router settings, and its pinned certificate means a TLS-terminating proxy with a different
certificate is not compatible. The agent advertises itself over mDNS/DNS-SD; the Android app can
list local IPv4 candidates from Add computer → Find on LAN, then still requires the normal
certificate-pin and pairing-code checks. mDNS does not authenticate a host. QR pairing and manual
address entry remain available. The project has no cloud relay or cloud database. See `docs/` for
the full architecture.

## Status

The agent serves the v1 API for directory browsing, resumable transfers, search, previews, settings,
paired-device management, and in-app Android updates. Windows, Linux, and macOS hosts also expose a
permission-controlled app catalog and launch action. The Android app covers
these features with a Finder/Explorer-style UI and self-updates over the air.

## Pairing

Pairing is done from the host side with the agent's admin CLI:

```sh
rfe pair         # prints a one-time pairing code, QR, and readable certificate fingerprint
                       # (first time? `rfe pair --owner` also lets the app create your account)
```

Scan the QR displayed locally on the intended host, or obtain the pairing details through a
trusted independent channel. For manual pairing, compare the fingerprint with `rfe status`
on the host console, then enter the address, code, and fingerprint in the app. The fingerprint
must be independently verified: a QR and fingerprint received together over the same untrusted
connection do not establish the host's identity. The app checks the pin before sending pairing
codes or account credentials. Successful pairing stores a per-device bearer token; no cloud
account is required.

### Approve on the computer

A phone that can reach the agent but has no code can ask to be paired: in the app choose Add
workspace → Enter address → Ask to pair. The phone and the computer each show the same 8-digit
match code; compare them, then approve at the computer (desktop notification, or
`rfe pair requests` and `rfe pair accept <id>`; `pair reject <id>` declines). The code
is derived from the certificate the phone actually connected to, so a machine relaying the request
shows a different code. A new phone starts browse-only. If the request is for a device that is
already paired under a different key, or one that was revoked, the prompt says which device it
replaces and approval resets that device to browse-only.

## Host app access

Each paired device starts with app-list and app-launch access turned off. An admin device can
grant either permission in that computer's Settings under Devices → App access. Launch permission
requires list permission. The Apps button shows user-facing entries discovered in the host's
supported catalog: Windows AppsFolder entries, Linux XDG desktop entries, or macOS apps in
standard application folders. Entries without a supported launch action remain visible with Run
disabled. The list is not an inventory of every executable or installed package; macOS aliases
and apps outside those folders are not included.

Run requests contain only a host-issued opaque app ID. The agent resolves it against the current
catalog and starts the app in the interactive desktop session; it does not accept a client command,
path, or arguments. Launch attempts are rate-limited and recorded in the host audit log. If the
agent is running without an active desktop session, launching is unavailable until that user signs
in to the desktop. This applies to all three host platforms.

## Device file access

New devices paired with a one-time code can browse files, but start without download, upload,
modify, delete, share-link, or app-catalog access. An owner device authenticated with the host
account can grant these permissions separately in **Settings → Devices**. Existing devices keep
their prior file access when upgraded. Upload permission also allows replacing an existing file;
the host's configured roots, per-device jail, read-only mode, and global share-link switch continue
to apply.

The Files tab starts from the roots available to the current device instead of assuming that the
host's `/` path is accessible. Restricted devices see only their effective shared folders;
unrestricted devices use the host's drive list or filesystem root. Bookmarks and search results
open from a matching allowed root, and stale locations stay clamped to that root.

## Install the host agent

On Linux, any distribution, one command downloads the agent (and with `all`, the desktop app), checks it against the
release's `SHA256SUMS`, runs `rfe setup` and adds the desktop app to your launcher:

```sh
curl -fsSL https://raw.githubusercontent.com/ZaidQamhieh/rfe/master/tools/install.sh | sh -s all
```

Packages are also published per release: `.deb` and `.rpm` for the agent (`agent-v*`) and the desktop app
(`desktop-v*`), and the AUR packages `rfe-bin` and `rfe-desktop-bin` (recipes in `packaging/aur/`).

Download the matching host binary, verify its SHA-256 checksum, and run `rfe setup`. New
agent databases default to a dedicated `RFE Files` folder under the signed-in user's home,
including when the daemon is started directly. Setup installs a user-level start-at-login service
and prints a pairing QR. Use `rfe setup --root <path>` to choose another existing folder.
The [host setup guide](docs/host-setup.md) has exact steps for Windows, macOS, and Linux, including
service controls and uninstall. Connections use HTTPS only; setup does not configure a firewall,
router, or public DNS. The native app pins the agent certificate, while
public web-companion access needs an owner-managed browser-trusted certificate/proxy path and is not
ready out of the box.

For development, run the agent with `cd agent && go run ./cmd/agent`; see
[`docs/development.md`](docs/development.md) for the developer workflow. The same agent process
serves the web companion at `https://<host>:8765/`.

## Contributing and license

Pull requests are welcome; see [`CONTRIBUTING.md`](CONTRIBUTING.md). The project is licensed under the
GNU General Public License v3.0 ([`LICENSE`](LICENSE)). The Rust sidecars in `agent-rs/` are MIT.
