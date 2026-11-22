package conformance

import (
	"slices"
	"strings"
	"testing"
)

// Groups of cases. The old agent is an oracle only for the invariant groups:
// the properties the owner named as not to be changed by the rewrite. A
// difference in any other group is an expected improvement.
const (
	groupPairing  = "pairing"  // pairing flow, registration, login, approve-on-PC
	groupTOFU     = "tofu"     // certificate fingerprint handed out at pairing, for pinning
	groupAuth     = "auth"     // bearer-token authentication and revocation
	groupJail     = "jail"     // root jail and symlink-escape refusal
	groupTransfer = "transfer" // chunked upload, resume, hash check, Range download, atomic rename
	groupOther    = "other"    // everything else: differences are improvements
)

// invariantGroups are the groups where the old agent is the oracle: an
// unexplained difference there fails the differential run.
var invariantGroups = []string{groupPairing, groupTOFU, groupAuth, groupJail, groupTransfer}

// groupRule assigns cases to a group by exact name or by name prefix or suffix.
type groupRule struct {
	group    string
	names    []string
	prefixes []string
	suffixes []string
}

// groupRules are tried in order; the first rule that matches a case wins, and
// a case no rule matches is in groupOther.
var groupRules = []groupRule{
	{group: groupTOFU, names: []string{"auth-challenge", "pair-no-proof", "login-no-proof"}},
	{group: groupPairing, names: []string{"logout", "register-bad-code"},
		prefixes: []string{"pair-", "pairing-", "login-", "logged-"}},
	{group: groupAuth, names: []string{"revoke-device"},
		prefixes: []string{"unauth-", "bad-token-", "revoked-"}, suffixes: []string{"-anon", "-bad-token"}},
	{group: groupJail, names: []string{"grant-jailed", "fs-list-outside-root", "device-patch-jail-outside-roots"},
		prefixes: []string{"escape-", "jailed-"}},
	{group: groupTransfer, names: []string{"grant-upload"},
		prefixes: []string{"upload-", "download-"}},
}

// groupOf returns the group of a case: the first rule that names it.
func groupOf(caseName string) string {
	for _, r := range groupRules {
		if slices.Contains(r.names, caseName) {
			return r.group
		}
		for _, p := range r.prefixes {
			if strings.HasPrefix(caseName, p) {
				return r.group
			}
		}
		for _, s := range r.suffixes {
			if strings.HasSuffix(caseName, s) {
				return r.group
			}
		}
	}
	return groupOther
}

// isInvariantGroup reports whether the old agent is the oracle for group g.
func isInvariantGroup(g string) bool { return slices.Contains(invariantGroups, g) }

// TestEveryInvariantGroupHasCases needs no agent: a group with no case would
// make the invariant it names unchecked without anyone noticing.
func TestEveryInvariantGroupHasCases(t *testing.T) {
	seen := map[string]int{}
	for _, n := range caseNames() {
		seen[groupOf(n)]++
	}
	for _, g := range invariantGroups {
		if seen[g] == 0 {
			t.Errorf("invariant group %q has no case in the request table", g)
		}
	}
	if seen[groupOther] == 0 {
		t.Errorf("no case is outside the invariant groups; the group rules are too wide")
	}
}

// TestGroupOfClassifiesTheNamedInvariants spot-checks one case of each
// invariant group and one improvement-only case.
func TestGroupOfClassifiesTheNamedInvariants(t *testing.T) {
	for name, want := range map[string]string{
		"pair-request-approve":    groupPairing,
		"auth-challenge":          groupTOFU,
		"bad-token-fs-list":       groupAuth,
		"revoked-fs-list":         groupAuth,
		"escape-symlink-list":     groupJail,
		"upload-mismatch-chunk-0": groupTransfer,
		"download-range-middle":   groupTransfer,
		"read-only-delete":        groupOther,
		"device-delete-missing":   groupOther,
		"fs-copy":                 groupOther,
	} {
		if got := groupOf(name); got != want {
			t.Errorf("groupOf(%q) = %q, want %q", name, got, want)
		}
	}
}
