# Version compatibility

Phones, desktops and agents upgrade independently. Every combination must work; newer features simply switch off.

- The agent's authorized `GET /health` returns `apiVersion` (int) and `features` (sorted string ids).
- Missing `apiVersion` means 1 (v1.42.x agent). Missing `features` means none.
- Clients gate UI on feature ids (`hasFeature`), never on version strings, and keep baseline features always on.
- A route an older agent lacks answers 404/405; clients show "this PC's agent is too old for X, update it", not a generic failure.
- Everyone ignores unknown JSON fields and defaults missing ones.
- Changes are additive: new fields and headers are optional, a feature id is never reused or removed, an old client keeps working against a new agent. The one exception is the two behaviour changes of apiVersion 3 that break clients, C1 and C2, listed below.
- Add a feature id in `agent/internal/server/server.go` (`Features`) and list it here.

| id | adds |
|---|---|
| `pairRequest` | `/pair/request*`: phone-initiated pairing approved on the PC |

## apiVersion 3

An agent that reports `apiVersion` 3 makes the changes below. `openapi.yaml` describes the apiVersion 2 behaviour in its main text where a change breaks clients, and marks the operation with an `x-rfe-v3` note; the other changes are written straight into the contract.

| # | Change | Breaks an apiVersion 2 client? |
|---|---|---|
| C1 | `DELETE /fs` on a path that does not exist: that item gets `ok=false`, `code=PATH_NOT_FOUND` | yes |
| C2 | `GET /transfers/{id}`, `PUT /transfers/{id}/chunks/{n}` and `POST /transfers/{id}/complete` answer only the device that opened the session and admin devices; anyone else gets 404 `NOT_FOUND` | no: the v2 agent already does this for devices with upload rights (conformance harness); only legacy rows with an empty device id are new |
| C3 | Pair-poll nonce (`GET /pair/request/{id}`) is sent in the `X-RFE-Pair-Nonce` header (preferred); the `nonce` query parameter is still read when the header is absent | no: the agent still reads the query form |
| C4 | `INVALID_NONCE` is in the error enum; the `/share/{token}` GET and `/share/{tokenHash}` DELETE parameter names are documented | no |
| C5 | Contract matches the code: `/fs/move` needs `modify` and `delete`; `/share/mint` needs `share`, `browse` and `download`; `PUT /content` needs `modify`; archive listing reads tar; a denied app view or launch is `CAPABILITY_DENIED` | no |
| C6 | `POST /share/mint` on a non-regular file: 422 `NOT_A_FILE`; `expiresInSeconds` above 7 days: 400 `BAD_REQUEST` | no |
| C7 | Archive extraction over its limit: 413 `PAYLOAD_TOO_LARGE` (was 500); renaming an entry onto itself: 200 no-op; uploads and archives are published with mode 0644 filtered by umask (was 0600) | no (fixes) |

### What a client does about C1 to C3

- C1: an apiVersion 3 agent reports a missing path as a failed item inside the 200 result list. A client that treated "no error entry" as "gone" must read `ok` on every item (it already must, see `BatchResult`). Against an older agent, a missing path was not reported this way.
- C2: a client that resumes or completes an upload it did not open (another device's session id) now gets 404. Resuming from the device that opened the session is unchanged.
- C3: the pair-poll request carries the nonce in `X-RFE-Pair-Nonce`; the agent reads the header when set and otherwise the `nonce` query parameter (agent/internal/next/pairing/request.go PollNonce; agent/internal/server/pair_request.go pollNonce); the mobile and desktop clients send both forms on every poll (agentClient.ts pairRequestStatus; desktop agent_client.rs pair-poll builder), so a client does not choose between them. Whether an older agent ignores the header is not verified here.

## Supported range

| Agent | Phone / desktop built for apiVersion 2 | Phone / desktop built for apiVersion 3 |
|---|---|---|
| apiVersion 3 (current) | everything except the behaviours of C1 and C2: missing-path deletes show as failed items; uploads resumed from another device get 404 | everything |
| apiVersion 2 (the releases before the apiVersion 3 one, newer than v1.43.0) | everything | everything, using the apiVersion 2 forms of C1 to C3 |
| agent-v1.43.0 | everything except `pairRequest` (older nonce protocol): the app says "agent too old for asking to pair", offers QR pairing | same |
| older than v1.43.0 (v1.42.x) | unsupported, the user must update to v1.43.0 or later. Sign-in is refused: the agent only checks the bare-nonce proof, which a relay can replay, and the app never downgrades to it. The app says "agent too old to sign in, update it" | same |

Older apps against a newer agent keep working except for the apiVersion 3 behaviours above: the agent otherwise only adds fields and routes.
