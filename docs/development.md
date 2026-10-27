# Development setup

## Toolchain

- **Go (agent):** `agent/go.mod` requires Go 1.26.0 or newer. CI and host releases use Go 1.26.6
  (`.github/workflows/ci.yml` and `.github/workflows/agent-release.yml`) so builds use the patched
  standard library and can install the current `govulncheck` release.
- **Node (app):** Node 22 and npm; JDK 21 for Android builds (CI uses both). The app is `mobile/`.

If Go is installed at `~/.local/go`, add it to your PATH (e.g. in `~/.bashrc`):

  ```sh
  export PATH="$HOME/.local/go/bin:$PATH"
  ```

## Agent

```sh
cd agent
go vet ./...
go build -o bin/agent ./cmd/agent   # no sidecars: search/thumbnails run in-process; tools/install-agent-local.sh installs the full set
go run ./cmd/agent -addr 127.0.0.1:8765 -name "my-pc"
```

First run writes `agent-cert.pem` / `agent-key.pem`, the SQLite DB, and other state into the
**data dir**, and reports the certificate fingerprint used to establish phone trust. The data dir is resolved with this
precedence: `-data <dir>` flag > `$RFE_DATA_DIR` env var > `~/.rfe` (default). The admin CLI
(below) resolves the data dir the same way, so `rfe devices` (no flags) talks to the same DB
as a no-args `rfe` daemon.

Smoke test:

```sh
curl -sk https://127.0.0.1:8765/v1/health
```

### Pairing a device

Pairing codes are minted by the admin CLI, not printed by the running daemon:

```sh
go run ./cmd/agent pair         # mints a one-time code + prints a QR in the terminal
go run ./cmd/agent devices       # list paired devices
go run ./cmd/agent revoke <id>   # block a device
go run ./cmd/agent remove <id>   # permanently delete a device row
go run ./cmd/agent status        # name, addresses, fingerprint, device counts
```

The pair command prints a one-time code, QR, and readable certificate fingerprint. Scan the QR
displayed on the intended host's local screen, or obtain the QR and fingerprint through a trusted
independent channel. For manual pairing, compare the fingerprint with `rfe status` on the
host console before entering the address, code, and fingerprint in the app. Do not trust a QR and
fingerprint delivered together over the same untrusted network connection. These subcommands work
whether or not the daemon is running, since they open the same on-disk DB directly (with a
busy-timeout so concurrent daemon + CLI writes are safe).

## App

```sh
cd mobile
npm ci
npx tsc --noEmit && npx eslint . && npx jest
npx expo run:android     # dev build; enter the agent's host:port on the connect screen
```

Note: the app verifies the agent's self-signed certificate against a SHA-256 fingerprint before
it sends pairing codes, passwords, or authenticated requests. New enrollment requires the
fingerprint from a trusted independent source; the app does not trust a fingerprint learned from
the same untrusted connection. For paired hosts, the fingerprint in Keychain/Keystore secure
storage is authoritative. If that pin is missing, the app fails closed and does not fall back to
the legacy copy in SharedPreferences; re-pair the host to store a secure pin again.

## Local CI (act)

```sh
docker build -t rfe-act:24.04 tools/act      # once: the runner image .actrc names
tools/act/run.sh -W .github/workflows/ci.yml -j web
```

The job container runs as root, so `.actrc` does not use `--bind`: act copies the checkout
(without gitignored files) into the container, and nothing a job writes lands in this tree.
`tools/act/run.sh` mounts the git dir read-only at its own path so git steps also work from a
linked worktree. Each run starts without host build caches (`node_modules`, `target`), so it is
slower than a bind mount. A plain `act --bind` brings back root-owned `node_modules`, `dist`
and `target` files in the checkout.

## Live E2E

`tools/e2e/run.sh` builds the agent from this checkout and runs the 41 cases in
`tools/e2e/cases.md` against isolated agents on 127.0.0.1; see `tools/e2e/README.md`.

## Layout

```
mobile/src/core/      api client, models, storage
mobile/src/features/  hosts, explorer, transfers, preview, pairing, settings
agent/cmd/agent/   main (daemon) + admin.go (pair/devices/revoke/remove/status CLI)
agent/internal/    server, fsops, transfer, search, thumbs, pairing, store, security, settings, updates, mdns
protocol/          openapi.yaml (shared contract)
```

The agent advertises `_rfe._tcp` over mDNS/DNS-SD. On Android, Add computer → Find on LAN runs a
bounded, user-started system DNS-SD scan and lists IPv4 address candidates. Selecting one only
prefills the ordinary code-pairing form; the TLS certificate fingerprint must still be obtained
through a trusted independent channel before the pairing code is sent. QR and manual address
entry remain available. iOS is out of scope for this Android-first app.

A brand-new agent database defaults its allowed root to a dedicated `RFE Files` folder under the
signed-in user's home. Pass `-roots <path>` to select another root; unrestricted access requires an
explicit empty `-roots` value or owner settings. Existing saved root policies are retained.
`rfe setup` provides the interactive first-run folder, service, and pairing flow.
