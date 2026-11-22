# Choosing how your phone reaches your PC

RFE has no cloud server. Your phone talks to the agent on your PC directly, over HTTPS, and pins the PC's certificate fingerprint when you pair. The only choice is the network path. Facts below are from the vendors' own documentation (read 2026-11-14).

| Option | How it connects | Setup | Works behind NAT | Main caveat |
|---|---|---|---|---|
| Same Wi-Fi (LAN IP) | Direct to the PC's local address | None; the app finds PCs on the network | Same network only | Does not work away from home |
| Tailscale | Direct UDP when possible; otherwise a relay (DERP, TCP 443) | Install on both devices and sign in | Yes; hard NAT forces the slower relay | Needs the Tailscale app on the phone and an account |
| NetBird | Peers connect outbound; falls back to a relay on UDP/443 or TCP/443 | Install on both devices | Yes, no inbound port needed | Account and management service |
| WireGuard (manual) | Keys and peer endpoints you configure | Manual | Needs at least one peer with a reachable address | You manage keys and addresses yourself |
| ZeroTier | Virtual network joined with a network ID | Install and join | Not verified here | Not verified here |
| Reverse proxy or Cloudflare Tunnel | Outbound tunnel or a proxy with a public name | cloudflared or Caddy/nginx plus a name | Tunnel: yes, no inbound ports | Cloudflare's terms prohibit streaming video through its free, Pro and Business plans; a large-file or video app may be restricted |
| Direct HTTPS with port forward | Router forwards a port to the agent | Router settings and a stable address | Needs a forwarded port | The PC is exposed to the internet; keep the pairing code and device grants tight |

Which to pick: stay on LAN at home; add Tailscale or NetBird for everywhere else (no router changes, encrypted). Use a reverse proxy only when you already run one and accept the exposure. Not verified: ZeroTier relay behaviour, Caddy/nginx specifics.

Video quality streaming (HLS transcode) sends many video segments, so do not route it through Cloudflare on the Free, Pro or Business plans: Cloudflare forbids serving video there.
