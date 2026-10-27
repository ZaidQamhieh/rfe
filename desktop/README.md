# RFE Desktop

A Tauri app for a PC that talks to one or several `rfe`s: it pins each agent's certificate, signs
this computer in (account, pairing code, or approval on the PC), browses and transfers files between this
computer and the servers, and manages the devices paired with them. It keeps its secrets in the system
keystore.

- Using it: [docs/user-guide.md](docs/user-guide.md) (install, trust, sign in, every message explained)
- The keystore, per provider, and its messages: [docs/keystore.md](docs/keystore.md)
- Which agent releases it works with: [docs/agent-compatibility.md](docs/agent-compatibility.md)

## Layout

| Path | What |
|---|---|
| `src-tauri/` | The Rust core. It owns every network call and every secret; the window has no network access. |
| `ui/` | The window: plain HTML, CSS and scripts, no build step. `engine.js` is the layer between the pages and the core (folder cache, servers, transfers); the pages (`pages.js`, `fpages.js`, `features.js`, `fx*.js`) only draw. |
| `ui-dev/` | A browser harness for the window (`ui-dev/harness.html`): a stand-in for the core, so a page can be opened and screenshotted without building the app. Not shipped. |
| `e2e/` | End-to-end tests in the real window (`e2e/run.sh`): WebDriver through `tauri-driver`, a real keyring and a throwaway agent, in a private virtual desktop that never touches yours. Also measures the app's memory. |
| `ui-tests/` | Node tests: the engine against a fake core, the QR encoder, the content-security rules, the colour contrast, that the window and the Rust core agree on every command, and the whole window run in jsdom (every screen, every dialog, a pause and resume). |
| `scripts/with-keyring.sh` | Runs a command against a private, real gnome-keyring. |
| `docs/` | The guides above. |

## Build and test

You need Rust (the toolchain is pinned in `src-tauri/rust-toolchain.toml`), Go (the tests build the
agent), Node, and on Linux the WebKitGTK development packages listed in
`.github/workflows/desktop.yml`.

```sh
cd desktop/src-tauri
cargo fmt --all --check
cargo clippy --all-targets --locked -- -D warnings
RFE_AGENT_BIN=/path/to/rfe cargo test --locked    # go build -o /path/to/rfe ./cmd/agent, in agent/
npm ci --prefix ../ui-tests && node --test ../ui-tests/*.test.mjs    # jsdom is the only dependency
RFE_AGENT_BIN=<built agent> e2e/run.sh       # real window; needs kwin_wayland, tauri-driver, a built debug app; SHOTS=1 refreshes the pictures in docs/
```

The tests start throwaway agents on random ports with temporary data folders and no desktop
notification or session bus, so nothing reaches your screen or your keyring. The tests that need a real
Secret Service run through `scripts/with-keyring.sh` (see [docs/keystore.md](docs/keystore.md)).

Run the app: `cargo run` in `desktop/src-tauri`. Package it: `cargo tauri build --bundles deb,appimage`
(`cargo install tauri-cli --version 2.12.1 --locked` once). Versioning and tags: the "Desktop app"
section of the repository's `CLAUDE.md`.
