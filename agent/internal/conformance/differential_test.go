package conformance

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"slices"
	"strings"
	"testing"
)

// Verdicts of one differential case.
const (
	verdictIdentical  = "identical"
	verdictExplained  = "explained"   // differs, and the difference is named in expectedV3Diffs
	verdictNotSeen    = "not-seen"    // a named difference exists but the two agents answered identically
	verdictBeyond     = "UNEXPLAINED" // an invariant group differs, beyond what (if anything) expectedV3Diffs names: fails the run
	verdictImproved   = "improved"    // a group that is not an invariant differs beyond its named entry: an expected improvement, never a failure
	diffContextLines  = 2
	reportFilePerm    = 0o644
	reportDiffIndent  = "    "
	reportNoDiffLabel = "-"
)

// errorWordingID is the ID of the global rule that error message text may differ
// (see the G1 entry in the expectedV3Diffs comment).
const errorWordingID = "G1"

// isErrorReply reports whether r is a 4xx or 5xx reply with a JSON error body.
func isErrorReply(r result) bool {
	_, isObject := r.JSON.(map[string]any)
	return r.Status >= http.StatusBadRequest && isObject
}

// errorWordingOnly reports whether two replies differ only in error message
// text: both are 4xx or 5xx error replies, or both carry per-item errors
// (results[].error) with the same status. It masks the messages in both replies
// in place, so the caller uses them only when this returns true.
func errorWordingOnly(o, g *result) bool {
	switch {
	case o.Status != g.Status:
		return false
	case isErrorReply(*o) && isErrorReply(*g):
		maskMessages(o.JSON)
		maskMessages(g.JSON)
	case hasItemErrors(o.JSON) && hasItemErrors(g.JSON):
		maskItemErrorMessages(o.JSON)
		maskItemErrorMessages(g.JSON)
	default:
		return false
	}
	return bytes.Equal(marshalResult(*o), marshalResult(*g))
}

// outcome is the comparison of one case's responses from the two agents.
type outcome struct {
	name    string
	group   string // the case's group (see groupOf)
	verdict string
	diffID  string // the named difference applied, if any
	diff    string // readable diff of the normalized responses (UNEXPLAINED and improved only)
}

// decodeResult parses the JSON form marshalResult writes, keeping numbers exact.
func decodeResult(b []byte) result {
	var r result
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.UseNumber()
	if err := dec.Decode(&r); err != nil {
		panic(err) // marshalResult output always decodes
	}
	return r
}

// compareResults compares the oracle's response with the new agent's for one
// case. Both are copied first, so a named difference never changes the caller's
// values. A case in an invariant group with no named difference must match
// exactly; any other case may differ (verdictImproved) and is only reported.
func compareResults(name string, oracle, got result) outcome {
	out := outcome{name: name, group: groupOf(name)}
	o, g := decodeResult(marshalResult(oracle)), decodeResult(marshalResult(got))
	rawSame := bytes.Equal(marshalResult(oracle), marshalResult(got))
	d := v3DiffFor(name)
	if d != nil {
		out.diffID = d.ID
	}
	applyDiff(d, &o, &g)
	wb, gb := marshalResult(o), marshalResult(g)
	// G1: only the wording of error messages may differ; status, code and
	// every other field must still match.
	if !bytes.Equal(wb, gb) && errorWordingOnly(&o, &g) {
		out.verdict, out.diffID = verdictExplained, errorWordingID
		return out
	}
	switch {
	case !bytes.Equal(wb, gb) && isInvariantGroup(out.group):
		out.verdict = verdictBeyond
		out.diff = lineDiff(string(wb), string(gb))
	case !bytes.Equal(wb, gb):
		out.verdict = verdictImproved
		out.diff = lineDiff(string(wb), string(gb))
	case d == nil:
		out.verdict = verdictIdentical
	case rawSame && !d.AlreadyOnOld:
		out.verdict = verdictNotSeen
	case rawSame:
		out.verdict = verdictIdentical
	default:
		out.verdict = verdictExplained
	}
	return out
}

// lineDiff renders a line-by-line difference of two texts: "-" lines only in
// old, "+" lines only in new, with a little unchanged context around each change.
func lineDiff(oldText, newText string) string {
	a, b := strings.Split(strings.TrimRight(oldText, "\n"), "\n"), strings.Split(strings.TrimRight(newText, "\n"), "\n")
	// lcs[i][j] is the longest common subsequence length of a[i:] and b[j:].
	lcs := make([][]int, len(a)+1)
	for i := range lcs {
		lcs[i] = make([]int, len(b)+1)
	}
	for i := len(a) - 1; i >= 0; i-- {
		for j := len(b) - 1; j >= 0; j-- {
			if a[i] == b[j] {
				lcs[i][j] = lcs[i+1][j+1] + 1
			} else {
				lcs[i][j] = max(lcs[i+1][j], lcs[i][j+1])
			}
		}
	}
	type line struct {
		mark string
		text string
	}
	var lines []line
	i, j := 0, 0
	for i < len(a) || j < len(b) {
		switch {
		case i < len(a) && j < len(b) && a[i] == b[j]:
			lines = append(lines, line{" ", a[i]})
			i++
			j++
		case j >= len(b) || (i < len(a) && lcs[i+1][j] >= lcs[i][j+1]):
			lines = append(lines, line{"-", a[i]})
			i++
		default:
			lines = append(lines, line{"+", b[j]})
			j++
		}
	}
	keep := make([]bool, len(lines))
	for k, l := range lines {
		if l.mark == " " {
			continue
		}
		for c := max(0, k-diffContextLines); c <= min(len(lines)-1, k+diffContextLines); c++ {
			keep[c] = true
		}
	}
	var sb strings.Builder
	skipped := false
	for k, l := range lines {
		if !keep[k] {
			skipped = true
			continue
		}
		if skipped {
			sb.WriteString("  ...\n")
			skipped = false
		}
		sb.WriteString(l.mark + " " + l.text + "\n")
	}
	return sb.String()
}

// verdictOrder is the column order of the per-group table in the report.
var verdictOrder = []string{verdictIdentical, verdictExplained, verdictNotSeen, verdictImproved, verdictBeyond}

// writeReport writes a per-group table of verdict counts, then every case's
// verdict, and the diff of each unexplained or improved one, to path.
func writeReport(path, oldBin, newBin string, oldVersion, newVersion int, outs []outcome) error {
	counts := map[string]int{}
	perGroup := map[string]map[string]int{}
	for _, o := range outs {
		counts[o.verdict]++
		if perGroup[o.group] == nil {
			perGroup[o.group] = map[string]int{}
		}
		perGroup[o.group][o.verdict]++
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "old: %s (apiVersion %d)\nnew: %s (apiVersion %d)\n", oldBin, oldVersion, newBin, newVersion)
	fmt.Fprintf(&sb, "cases: %d  %s: %d  %s: %d  %s: %d  %s: %d  %s: %d\n\n", len(outs),
		verdictIdentical, counts[verdictIdentical], verdictExplained, counts[verdictExplained],
		verdictNotSeen, counts[verdictNotSeen], verdictImproved, counts[verdictImproved],
		verdictBeyond, counts[verdictBeyond])
	fmt.Fprintf(&sb, "%-10s %-9s", "group", "oracle")
	for _, v := range verdictOrder {
		fmt.Fprintf(&sb, " %12s", v)
	}
	sb.WriteString("\n")
	for _, g := range append(slices.Clone(invariantGroups), groupOther) {
		oracle := "no"
		if isInvariantGroup(g) {
			oracle = "yes"
		}
		fmt.Fprintf(&sb, "%-10s %-9s", g, oracle)
		for _, v := range verdictOrder {
			fmt.Fprintf(&sb, " %12d", perGroup[g][v])
		}
		sb.WriteString("\n")
	}
	sb.WriteString("\n")
	for _, o := range outs {
		id := o.diffID
		if id == "" {
			id = reportNoDiffLabel
		}
		fmt.Fprintf(&sb, "%-12s %-10s %-6s %s\n", o.verdict, o.group, id, o.name)
		if o.diff != "" {
			sb.WriteString(reportDiffIndent + strings.ReplaceAll(strings.TrimRight(o.diff, "\n"), "\n", "\n"+reportDiffIndent) + "\n")
		}
	}
	return os.WriteFile(path, []byte(sb.String()), reportFilePerm)
}

// requireBinary returns the executable named by an environment variable, or
// skips the test when it is unset.
func requireBinary(t *testing.T, env string) string {
	t.Helper()
	p := os.Getenv(env)
	if p == "" {
		t.Skipf("%s is not set", env)
	}
	return p
}

// TestDifferential runs the request table against the current agent
// (RFE_CONF_OLD_BIN) and the agent under test (RFE_CONF_BIN) side by side, each
// on its own temp data dir and loopback port, and fails every case whose
// normalized responses differ, in an invariant group (see invariantGroups), in
// a way expectedV3Diffs does not name. A difference in any other group is an
// improvement: logged and reported, never a failure. Set RFE_CONF_REPORT to a
// file to also get every verdict written there.
func TestDifferential(t *testing.T) {
	oldBin := requireBinary(t, oldBinEnv)
	newBin := requireBinary(t, binEnv)
	pinUmask(t)
	oldBin, newBin = absBinary(t, oldBinEnv, oldBin), absBinary(t, binEnv, newBin)
	oldS, newS := newSession(t, oldBin), newSession(t, newBin)
	t.Logf("old: %s apiVersion=%d; new: %s apiVersion=%d", oldS.a.describe(), oldS.version, newS.a.describe(), newS.version)

	var outs []outcome
	var ignored []string
	for _, h := range strings.Split(os.Getenv(ignoreHeadersEnv), ",") {
		if h = strings.TrimSpace(h); h != "" {
			ignored = append(ignored, http.CanonicalHeaderKey(h))
		}
	}
	for _, st := range buildCases() {
		t.Run(st.name, func(t *testing.T) {
			ro, rn := oldS.exec(t, st), newS.exec(t, st)
			for _, h := range ignored {
				delete(ro.Headers, h)
				delete(rn.Headers, h)
			}
			o := compareResults(st.name, ro, rn)
			outs = append(outs, o)
			switch o.verdict {
			case verdictBeyond:
				t.Errorf("new agent differs from the old one in the %s invariant (difference %q applied)\n--- old\n+++ new\n%s", o.group, o.diffID, o.diff)
			case verdictImproved:
				t.Logf("improvement, group %s (difference %q applied)\n--- old\n+++ new\n%s", o.group, o.diffID, o.diff)
			case verdictNotSeen:
				t.Logf("difference %s is named for this case but both agents answered the same", o.diffID)
			}
		})
	}
	if path := os.Getenv(reportEnv); path != "" {
		if err := writeReport(path, oldBin, newBin, oldS.version, newS.version, outs); err != nil {
			t.Errorf("write %s: %v", path, err)
		}
	}
}
