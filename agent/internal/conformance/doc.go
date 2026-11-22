// Package conformance holds a black-box test harness for the agent's REST
// contract. It starts an agent binary (RFE_CONF_BIN, or one built from
// ./cmd/agent) on loopback with a throwaway data directory and fixture tree,
// pairs devices through the real admin CLI and pairing endpoints, replays a
// fixed table of requests, and compares the normalized responses with golden
// files in testdata/ (recorded from the old agent) or, in TestDifferential,
// with the live old agent (RFE_CONF_OLD_BIN).
//
// The rewrite (agent-next) is not bent to match the old agent: it has more
// features and is more efficient, and where it behaves better it stays. The old
// agent is the oracle only for the invariants the owner named, each a group of
// cases (groups_test.go): the pairing flow and approve-on-PC, TOFU/fingerprint
// pinning, token authentication and revocation, root-jail and symlink-escape
// refusal, and transfer integrity (chunked upload, resume, hash check, Range
// download, atomic rename). Only a difference in one of those groups that
// expectedV3Diffs does not name fails a run. Every other group is expected to
// improve: its differences are reported as "improved", ideally with a named
// entry (reason and status "improved") in expectedV3Diffs and a row in
// docs/agent-next-differences.md, and never fail the run.
//
// Regenerate the goldens from the old agent (never from the rewrite) with:
//
//	go test ./internal/conformance/ -update
package conformance
