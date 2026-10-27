# The OS keystore

RFE Desktop keeps two secrets in the operating system's keystore and nowhere else: this computer's
device signing key and the login token the agent gave it. There is no file fallback. If the
keystore cannot be used, sign-in stops with a message and nothing secret is written to disk.
`state.json` holds only the agent's address, its pinned certificate fingerprint, the account name
and the device id.

| System | Keystore |
|---|---|
| Linux | Secret Service over D-Bus |
| macOS | Keychain |
| Windows | Credential Manager |

## Linux: what has to be running

A Secret Service provider on the **session** bus, with a default keyring or wallet that is unlocked:

- **gnome-keyring** (GNOME, and most desktops that ship it). Unlocked at login when your login
  password is the keyring's password.
- **KeePassXC**: Settings, Secret Service Integration, enable it and expose the database. The
  database must be open.
- **KDE Wallet**: recent Plasma versions answer the Secret Service API; the wallet must exist and be
  open.

The `.deb` recommends `gnome-keyring | keepassxc`. The AppImage brings neither.

The entries are named service `rfe-desktop`, with the secret kind and the state directory in the
account, for example `token:/home/you/.local/share/app.rfe.desktop`. Look them up in Seahorse,
KeePassXC or with `secret-tool search service rfe-desktop`.

## Messages and what to do

Settings has a **Test the keystore** button: it saves a throwaway secret, reads it back and removes it,
and shows the same message the sign-in would.

| The window says | Cause | Do this |
|---|---|---|
| "no OS keystore answered ... DBus error ... Failed to connect to socket" | No session bus, for example over SSH, in a container, or under `sudo`; or a bus with no provider on it | Run the app inside a desktop session with gnome-keyring, KeePassXC or KDE Wallet running. On a server: `dbus-run-session` plus a started and unlocked provider |
| "the OS keystore refused access ... unlock prompt was dismissed" | A provider is running but locked, and the unlock prompt was closed or could not be shown | Unlock the keyring or wallet (log in again, or open it in Seahorse, KWalletManager or KeePassXC), then retry |
| "the OS keystore refused access ... no result found" | The provider has no default keyring | Create one (Seahorse: new password keyring, set as default) |
| "the OS keystore refused access ... Cannot create an item in a locked collection" | Locked, as above | Unlock it |
| "the OS keystore did not keep the login token" or "... the device key" | The keystore accepted a write but returned something else when read back | Try again; if it repeats, the provider is faulty or full. Run **Test the keystore** in Settings |
| "the OS keystore did not return what was saved" | Same, from the Settings test | Same |
| "the OS keystore call failed to run" | The app's own worker stopped | Restart the app; report it if it repeats |
| "the keystore identity is damaged" | The stored device key cannot be read | Use "Create a new device key for this computer" on the sign-in screen while signed out |
| "... holds a different key than the keystore; delete the one you do not want" | A leftover `identity.json` from an early version differs from the key in the keystore | Delete whichever of the two you do not want to keep, then retry |
| "the OS keystore did not answer within ..." | An unlock prompt is open and nobody answered | Answer it. The app stops waiting after the timeout; try again |

Every one of these appears before the agent is contacted, so a failed attempt never reaches the PC.

## Good to know

- A keyring with a **blank password** is stored as a readable file under
  `~/.local/share/keyrings/`. The keystore is only as strong as the keyring behind it: anything that can
  read your home directory can read the token. Give the keyring a password, which is gnome-keyring's
  default when you log in with a password.
- Reading an entry that does not exist works even while the keyring is locked (the answer is "nothing
  there"); reading or writing one that does exist needs the unlock. A locked keyring therefore shows
  up at sign-in or when the saved login is first used, not before.
- Signing out removes the token from the keystore. The device key stays, so signing in again is the
  same device. "Create a new device key" on the sign-in screen replaces it, and removes the logins parked for
  your other saved agents, since they were made with the old key.

## Testing against a real provider

Neither of these touches the keyring or the keyring daemon of whoever runs them: each starts its own
D-Bus session and gnome-keyring with a temporary data and runtime directory, and no display, so no
prompt can open on your desktop. They need `dbus-run-session`, `gnome-keyring-daemon` and
`secret-tool` (package `libsecret-tools`).

```
desktop/scripts/with-keyring.sh cargo test --locked --test keystore -- --ignored
desktop/scripts/with-keyring.sh --locked cargo test --locked --test keystore_locked -- --ignored
```

The first runs with an unlocked keyring that has no password. The second runs with a keyring that
exists and is locked. The headless case (no bus at all) runs in every plain `cargo test`.

Do not probe the keystore with a bare `dbus-run-session`: D-Bus activation starts the system's
gnome-keyring against your own data directory, and anything the probe stores lands in your real
keyring.
