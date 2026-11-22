# agent-next: intentional differences from the old agent

agent-next is not bent to match the old agent; where it behaves better it stays. The old agent is the oracle only
for: pairing flow and approve-on-PC, TOFU/fingerprint pinning, token authentication and revocation, root-jail and
symlink-escape refusal, and transfer integrity (chunked upload, resume, hash check, Range download, atomic rename).
The conformance harness (`agent/internal/conformance`) fails only on those groups. Bracketed IDs are the `improved`
entries in `agent/internal/conformance/diffs_test.go`.

| Route | Old | New | Why |
|---|---|---|---|
| `DELETE /fs`, `POST /fs/copy`, `POST /fs/move`, `POST /trash/restore`, `DELETE /trash` on a read-only agent or device [R1] | 200/202, a `READ_ONLY` result per path | one `403 READ_ONLY`, nothing touched | one clear error, the operation never starts |
| `DELETE /devices/{id}`, unknown device [D1] | 204 and an audit entry | `404 NOT_FOUND`, no audit entry | caller can tell; audit records real changes only |
| apps list/launch without the grant [A1] | `APP_VIEW_FORBIDDEN` / `APP_LAUNCH_FORBIDDEN` | `403 CAPABILITY_DENIED` | one code for a missing capability |
| refused `POST /apps/{id}/launch` | no audit entry | `app_launch` entry, `outcome=permission_denied` | security-relevant event |
| `POST /wol`, bad address [W1] | `INVALID_MAC` / `INVALID_BODY` | `400 BAD_REQUEST` | generic validation code |
| missing path: delete (permanent), trash delete, copy, move, compress, share mint [C1, C3, F1, S2] | `ok:true`, `COPY_FAILED`, `CONFLICT`, `NOT_FOUND`, `500 INTERNAL` | `PATH_NOT_FOUND` | one code for "not there" |
| move folder into itself [F2] | `MOVE_FAILED` | `CONFLICT` | it is a conflict |
| `POST /share/mint` of a directory [S2] | `400 BAD_REQUEST` | `422 NOT_A_FILE` | same as `GET /content` |
| `POST /share/mint` without `expiresInSeconds` [S1] | 15 minutes | 1 day | |
| negative bandwidth limit [B1] | accepted, stored 0 | `400 BAD_REQUEST` | reject bad input |
| content search [Q1] | names only | also file contents | feature |
| thumbnails [T1] | exact size | bucketed sizes | cache reuse |
| `GET /health`, `GET /status` [C8, H1] | `apiVersion` 2 | `apiVersion` 3; status adds addresses, MAC, fingerprint, name, counts, sidecar state | fewer calls |
| `GET /logs`, `GET /metrics` [L1, L2, M1] | limit ignored | `?limit` honoured | |
| approved pair-request poll [P1] | `tailscaleAddress: ""` | omitted when unset | optional field |
| error message text, per-item FORBIDDEN outside root [G1, E1] | may contain host paths | plain wording | no host paths leaked |
| every response | no `Referrer-Policy` | `Referrer-Policy: no-referrer` | |
| files the agent creates | umask mode | `0600` | private by default |
| pairing code generation | no PNG | adds `qrPngBase64` | desktop shows the QR |
| `GET /transfers/list` | `activeNow` = open sessions with a recent chunk | same definition | kept |

Not yet documented (shown as `improved` without an entry in the last harness run): share-revoke, audit-owner,
audit-limit1, audit-bad-limit.

## Client impact of D1 (404 on a missing device)
- Desktop `device_actions.rs`: `explain` maps `404 NOT_FOUND` to "That device is no longer on the agent" and
  refreshes the list. No error screen.
- Mobile `agentClient.deleteDevice`: a 404 throws like any non-2xx; the "missing path counts as done" rule is only for
  delete batches. `PhonesWithAccessScreen` and `HostSettingsScreen` would show an error toast (only if the row
  vanished between listing and the tap).
