# Which agents the desktop app works with

> Versions restarted at `agent-v1.0.0` when the agent was renamed from `rfe-agent` to `rfe`; every `agent-v1.x` from
> 1.0.0 on has all the features below. The `agent-v1.42`/`1.43` rows are the old line.

Measured 2026-10-04 by building each release from its tag and running the whole desktop test suite
(`cargo test --locked`, `RFE_AGENT_BIN` pointing at that build) against it.

| Agent release | Sign in with an account | Pair with a code | Approve on the PC | Result |
|---|---|---|---|---|
| `agent-v1.42.6`, `agent-v1.42.7` | refused: no bound proof | refused | no, endpoint missing | the app says "This agent is too old to sign in from this app" before it sends a password; measured: 81 of the suite's tests need a sign-in and fail with that message, the rest pass |
| `agent-v1.43.0-rc.1` | refused: no bound proof | refused | yes | same refusal |
| `agent-v1.43.0` (stable), `agent-v1.43.0-rc.2` and the checkout (first releases with `proof: v2`) | yes | yes | yes | all tests pass (the main CI job) |

**Minimum agent:** the first release that includes the certificate-bound sign-in proof (`proof: "v2"` in the
`POST /auth/challenge` answer, `security.DeviceProofMessageV2`). `agent-v1.43.0-rc.2` is the first tag that has it, and `agent-v1.43.0` the first stable one. The desktop app signs only that proof. A bare-nonce signature is good at every agent, so
an agent that relays a nonce from a second agent to this app could replay the answer there; refusing agents
without the field keeps such an agent from hiding its support to get one. The phone app and the browser
companion still sign the bare nonce, and the agent still accepts it from them.

What the app does with an agent that is too old for approval: the agent answers the endpoint with a bare
`404 page not found`, and the app says "This agent is too old to approve a new computer from the PC (it
needs agent-v1.43.0-rc.1 or newer). Pair with a code or sign in with an account instead." instead of
showing `HTTP_404`.

## How CI keeps this true

`desktop.yml` runs the suite twice: against an agent built from the same checkout (a missing feature
fails), and against the newest stable `agent-v*` tag, which is not a `-rc`. While that release lacks the
bound proof, the second run only proves the refusal (`tests/bound_proof.rs`); once a stable release has it,
the second run is the whole suite again. Neither run has happened on GitHub yet.

Pick the agent tag to test by hand with `RFE_AGENT_BIN`, building it from `git archive <tag> agent`.
