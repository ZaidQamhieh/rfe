# Running the agent on Windows

The agent is a single self-contained `.exe` — no install, no runtime, no terminal knowledge needed
to run the daemon. Pairing a phone now uses a small admin command from a terminal (see step 4).

> **Note:** the bundled `rfe-windows-amd64.exe` in this folder may be stale — it is not
> rebuilt automatically when the agent source changes. If `rfe-windows-amd64.exe pair`
> doesn't work (e.g. "unknown command"), rebuild it from source (cross-compiled from Linux/macOS):
>
> ```sh
> cd agent
> GOOS=windows GOARCH=amd64 go build -o ../dist/rfe-windows-amd64.exe ./cmd/agent
> ```

## 1. Get the file onto the Windows PC
Copy `rfe-windows-amd64.exe` to the Windows machine (USB stick, Tailscale file send,
shared folder, cloud drive — whatever you use). Put it anywhere, e.g. your Desktop.

## 2. Run the daemon
Double-click `rfe-windows-amd64.exe` (or run it with no arguments from a terminal).

- A **console window** opens showing the agent's name and cert fingerprint, and it starts
  listening for connections.
- **Windows SmartScreen** may say "Windows protected your PC" (the .exe isn't code-signed yet) →
  click **More info → Run anyway**.
- **Windows Firewall** will pop up asking to allow network access → tick **Private networks** and
  click **Allow access**. (This is what lets your phone reach it.)

Keep the console window open while you use the app. Closing it stops the agent.

## 3. Find the PC's address
Open Command Prompt and run `ipconfig` — note the **IPv4 Address** (e.g. `192.168.1.50`).
If both devices are on Tailscale, you can use the PC's Tailscale name/IP instead.

## 4. Pair the phone with `pair`
Pairing codes are no longer printed by the running daemon. Open a **second** Command Prompt
window (the daemon from step 2 keeps running) and run:

```
rfe-windows-amd64.exe pair
```

This prints a one-time pairing code, the LAN/Tailscale addresses, and a scannable QR code in the
terminal. In the app: **Add computer → Scan QR**, or use the **Manual tab** and enter:
- **Address:** `<that-ip>:8765`  (e.g. `192.168.1.50:8765`)
- **Pairing code:** the code printed by `pair`

That's it — the app supports multiple computers, so your Windows PC shows up alongside any others.
You'll see `C:\`, `D:\`, etc. as drives.

Other admin commands (run the same way, in a separate terminal from the daemon):

```
rfe-windows-amd64.exe devices    # list paired devices
rfe-windows-amd64.exe revoke <id>
rfe-windows-amd64.exe remove <id>
rfe-windows-amd64.exe status
```

## Notes
- Default port is `8765`. To change it: run from a terminal as
  `rfe-windows-amd64.exe -addr 0.0.0.0:9000`. Pass the same `-addr` to `pair`/`status` if
  you change it, so the printed addresses are correct.
- Data (TLS cert, device DB, transfers, thumbnails, update cache) is stored under
  `%USERPROFILE%\.rfe` by default. Override with `-data <dir>` or the `RFE_DATA_DIR`
  environment variable; the daemon and the admin commands must agree on this directory (the
  default is shared between them).
- Running always-on as a proper **Windows Service** (auto-start, no console window) is on the
  roadmap — for now, running the .exe directly is the way to test.
