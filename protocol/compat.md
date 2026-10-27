# Version compatibility

Phones, desktops and agents upgrade independently. Every combination must work; newer features simply switch off.

- The agent's authorized `GET /health` returns `apiVersion` (int) and `features` (sorted string ids).
- Missing `apiVersion` means 1 (v1.42.x agent). Missing `features` means none.
- Clients gate UI on feature ids (`hasFeature`), never on version strings, and keep baseline features always on.
- A route an older agent lacks answers 404/405; clients show "this PC's agent is too old for X, update it", not a generic failure.
- Everyone ignores unknown JSON fields and defaults missing ones.
- Changes are additive: new fields and headers are optional, a feature id is never reused or removed, an old client keeps working against a new agent.
- Add a feature id in `agent/internal/server/server.go` (`Features`) and list it here.

| id | adds |
|---|---|
| `pairRequest` | `/pair/request*`: phone-initiated pairing approved on the PC |

## Supported range

| Agent | Phone / desktop 2.x |
|---|---|
| current (apiVersion 2) | everything |
| agent-v1.43.0 | everything except `pairRequest` (older nonce protocol): the app says "agent too old for asking to pair", offers QR pairing |
| older than v1.43.0 (v1.42.x) | unsupported, the user must update to v1.43.0 or later. Sign-in is refused: the agent only checks the bare-nonce proof, which a relay can replay, and the app never downgrades to it. The app says "agent too old to sign in, update it" |

Older apps against a newer agent keep working: the agent only adds fields and routes.
