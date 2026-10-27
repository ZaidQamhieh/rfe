# Live E2E

`run.sh` builds the agent from this checkout (or tests `E2E_AGENT`), runs the fixture self-checks,
then the 41 cases in `cases.md` against real agents over HTTPS on 127.0.0.1, and writes one
verdict per case to `matrix.md` in a new `/tmp/rfe-e2e-run.*` directory. It exits non-zero if any
case is FAIL or NOT RUN. Needs bash, curl, jq, rg, sqlite3, python3 and go.

| File | Contents |
|---|---|
| `cases.md` | The case manifest: REQ-1..REQ-41, one per requirement, with its Accept line |
| `lib.sh` | Fixture layer: instances, devices, session hygiene, process cleanup |
| `selfcheck.sh` | Checks of `lib.sh` itself, one per false result seen in pass 4 |
| `cases-a.sh` | REQ-1..20 except 11, on one instance |
| `cases-b.sh` | REQ-21..41 except 34, 37, 39, on one instance |
| `cases-iso.sh` | REQ-11, 34, 37, 39, each on its own instance with preconditions and a control |
| `client/` | Pairs a device with a test agent and prints its token |
| `mkarch.py` | Builds the crafted zip and tar archives |

## Fixture rules

- Each instance gets a new data dir, port, root, trash (`XDG_DATA_HOME` inside the run dir) and
  owner. `-roots` and `-read-only` only seed a new `agent.db`, so a configuration is never
  changed by restarting with other flags; `e2e_restart` takes none, and a case that needs a
  different configuration starts a new instance.
- Devices are paired with unique labels (`e2e-owner`, `e2e-dev-NAME`) and looked up by label.
  A lookup that matches zero or several rows, or the owner, is a harness error.
- `e2e_case` checks that no upload session is open before a case and removes any the case
  left, printing a `HARNESS` note. The quota case (REQ-34) runs on its own instance.
- Only agent processes this run started, checked by executable path, are stopped. Instance
  files are removed after each instance stops (`E2E_KEEP=1` keeps them); logs stay.

## Reading results

| Status | Meaning |
|---|---|
| PASS | Every check held; for the isolated cases, the preconditions held and the control behaved as expected |
| FAIL[product?] | A check failed; look at the evidence before calling it a product defect |
| FAIL[fixture] | Setup or a precondition failed, so the case says nothing about the product |
| FAIL[harness] | The runner saw no single verdict (for example two) |
| N-A | Does not apply; the evidence says why (REQ-1 is a meta requirement) |
| NOT RUN | No verdict was printed |

A precondition (`[pre ...]` in the evidence) is what makes a verdict mean what it says: for
REQ-11, the configured roots contain the data dir and paths beside it are reachable; for REQ-37,
the same upload succeeds while the modify grant is held; for REQ-39 global, the agent reports
`readOnly: true`. A FAIL from this harness is a claim about the harness until the evidence
shows its precondition held.
