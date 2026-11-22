package conformance

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

// update rewrites the golden files from the agent under test instead of comparing.
var update = flag.Bool("update", false, "rewrite testdata/*.golden.json from the agent under test")

const (
	goldenDir     = "testdata"
	goldenSuffix  = ".golden.json"
	maxInlineBody = 2048
	// expiryRounding is the step expiry times are rounded to under relExpiry.
	expiryRounding = time.Minute
)

// keptHeaders is the whitelist of response headers that are stable and part of
// the contract. Everything else (Date, ETag, Last-Modified, request ids) is dropped.
var keptHeaders = []string{
	"Content-Type", "Content-Range", "Accept-Ranges", "Content-Disposition",
	"Cache-Control", "X-Content-Type-Options", "Referrer-Policy", "Content-Security-Policy",
}

// volatileKeys are JSON object keys whose scalar values differ on every run
// (times, random ids and tokens) and are replaced by "<key>".
var volatileKeys = map[string]bool{
	"modified": true, "modifiedAt": true, "created": true, "createdAt": true,
	"updatedAt": true, "expiresAt": true, "lastSeen": true, "lastSeenAt": true,
	"token": true, "tokenHash": true, "deviceToken": true, "nonce": true,
	"id": true, "deviceId": true, "url": true, "time": true, "ts": true,
	"certFingerprint": true, "fingerprint": true, "pairingCode": true, "qrPngBase64": true, "requestId": true, "matchCode": true,
	// Live host readings: disk space, uptime and the metrics sample.
	"freeBytes": true, "totalBytes": true, "uptimeSeconds": true, "cpuPercent": true,
	"ramPercent": true, "rxBytes": true, "txBytes": true, "tsMs": true,
}

// The type of a shell script comes from the host's MIME database (Go reads /etc/mime.types and the shared-mime-info
// files), so two machines disagree on it. Every spelling seen is compared as one placeholder.
const (
	mimeTypeKey         = "mimeType"
	hostMimePlaceholder = "<shellscript mime>"
)

var hostDependentMime = map[string]bool{
	"text/x-shellscript; charset=utf-8": true,
	"text/x-shellscript":                true,
	"application/x-shellscript":         true,
	"application/x-sh":                  true,
	"text/x-sh":                         true,
}

var (
	uuidRe = regexp.MustCompile(`[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}`)
	// boundaryRe matches a multipart/byteranges boundary (a long random hex string).
	boundaryRe = regexp.MustCompile(`(boundary=|--)[0-9a-f]{40,}`)
	timeRe     = regexp.MustCompile(`\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(\.\d+)?(Z|[+-]\d{2}:\d{2})`)
)

// result is a response reduced to what a client can rely on: status, a few
// headers, and the body. JSON bodies are kept as values; other bodies as text
// (when short and printable) or a length and SHA-256.
type result struct {
	Status     int               `json:"status"`
	Headers    map[string]string `json:"headers,omitempty"`
	JSON       any               `json:"json,omitempty"`
	Text       string            `json:"text,omitempty"`
	BodyLen    int               `json:"bodyLen,omitempty"`
	BodySHA256 string            `json:"bodySha256,omitempty"`
}

// normalizer masks values that legitimately differ between runs or machines:
// temp paths, the listen address, timestamps and random identifiers.
type normalizer struct {
	paths *strings.Replacer
	// relExpiry reports a unix-seconds expiresAt as the time left from now
	// (rounded to expiryRounding) instead of masking it, so a differing
	// default lifetime shows.
	relExpiry bool
	now       func() time.Time
}

// newNormalizer builds a normalizer that masks the given agent's directories and address.
func newNormalizer(a *agentUnderTest) *normalizer {
	return &normalizer{now: time.Now, paths: strings.NewReplacer(
		a.dataDir, "<DATA>", a.root, "<ROOT>", a.home, "<HOME>", a.addr, "<ADDR>", a.outside, "<OUTSIDE>",
	)}
}

// str masks paths, the address, UUIDs and RFC 3339 times inside one string.
func (n *normalizer) str(s string) string {
	s = n.paths.Replace(s)
	s = uuidRe.ReplaceAllString(s, "<uuid>")
	s = timeRe.ReplaceAllString(s, "<time>")
	return boundaryRe.ReplaceAllString(s, "${1}<boundary>")
}

// walk normalizes a decoded JSON value in place; key is the object key it sits under.
func (n *normalizer) walk(key string, v any) any {
	switch x := v.(type) {
	case map[string]any:
		// A directory's reported size is whatever the host filesystem says it is.
		if dir, _ := x["isDir"].(bool); dir {
			if _, ok := x["size"]; ok {
				x["size"] = "<dirSize>"
			}
		}
		for k, child := range x {
			x[k] = n.walk(k, child)
		}
		return x
	case []any:
		for i, child := range x {
			x[i] = n.walk(key, child)
		}
		return x
	case string:
		if volatileKeys[key] {
			return "<" + key + ">"
		}
		if key == mimeTypeKey && hostDependentMime[x] {
			return hostMimePlaceholder
		}
		return n.str(x)
	case json.Number:
		if key == "expiresAt" && n.relExpiry {
			if secs, err := x.Int64(); err == nil {
				left := time.Unix(secs, 0).Sub(n.now()).Round(expiryRounding)
				return "<expires in " + strconv.FormatInt(int64(left/time.Second), 10) + "s>"
			}
		}
		if volatileKeys[key] {
			return "<" + key + ">"
		}
	}
	return v
}

// decodeJSON parses b keeping numbers exact (json.Number) so goldens do not lose precision.
func decodeJSON(b []byte) (any, error) {
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return nil, err
	}
	return v, nil
}

// normalize converts a raw response into its comparable form.
func (n *normalizer) normalize(r *rawResponse) result {
	out := result{Status: r.status}
	for _, h := range keptHeaders {
		if v := r.header.Get(h); v != "" {
			if out.Headers == nil {
				out.Headers = map[string]string{}
			}
			out.Headers[h] = n.str(v)
		}
	}
	if len(r.body) == 0 {
		return out
	}
	if strings.Contains(r.header.Get("Content-Type"), "json") {
		if v, err := decodeJSON(r.body); err == nil {
			out.JSON = n.walk("", v)
			return out
		}
	}
	out.BodyLen = len(r.body)
	if len(r.body) <= maxInlineBody && utf8.Valid(r.body) {
		out.Text = n.str(string(r.body))
		return out
	}
	sum := sha256.Sum256(r.body)
	out.BodySHA256 = hex.EncodeToString(sum[:])
	return out
}

// marshalResult renders a result as stable, indented JSON without HTML escaping.
func marshalResult(r result) []byte {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(r); err != nil {
		panic(err) // result only holds JSON-safe values
	}
	return buf.Bytes()
}

// goldenPath is where the expected response for a named case lives.
func goldenPath(name string) string {
	return filepath.Join(goldenDir, name+goldenSuffix)
}

// loadGolden reads and decodes a case's golden file.
func loadGolden(name string) (result, error) {
	b, err := os.ReadFile(goldenPath(name))
	if err != nil {
		return result{}, err
	}
	var r result
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.UseNumber()
	if err := dec.Decode(&r); err != nil {
		return result{}, err
	}
	return r, nil
}

// checkGolden compares got with the case's golden file, or rewrites the file
// under -update. When v3 is true the golden is first transformed by the case's
// entry in expectedV3Diffs; a rewritten agent is then held to the old agent's
// answers only in the invariant groups (see groups_test.go), and a difference
// in any other group is logged as an improvement; -update is refused for v3 so the goldens
// stay a record of the old agent.
func checkGolden(t *testing.T, name, sortList string, got result, v3 bool) {
	t.Helper()
	if *update {
		if v3 {
			t.Fatalf("-update would record a v3 agent as the oracle; regenerate goldens from the old agent")
		}
		if err := os.MkdirAll(goldenDir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(goldenPath(name), marshalResult(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := loadGolden(name)
	if errors.Is(err, os.ErrNotExist) {
		t.Fatalf("no golden file %s; run: go test ./internal/conformance/ -update", goldenPath(name))
	}
	if err != nil {
		t.Fatalf("golden %s: %v", goldenPath(name), err)
	}
	if sortList != "" {
		// The response is sorted the same way (see step.sortList), so the
		// golden, recorded before the sort existed, is sorted too.
		sortResultList(&want, sortList)
	}
	label := "golden"
	if v3 {
		if d := v3DiffFor(name); d != nil {
			label = "golden + " + d.ID
			applyDiff(d, &want, &got)
		}
	}
	if w, g := marshalResult(want), marshalResult(got); !bytes.Equal(w, g) {
		if v3 && errorWordingOnly(&want, &got) { // G1: message wording is not part of the contract
			return
		}
		// Only the invariant groups are held to the old agent; elsewhere a
		// rewritten agent may behave better, which is reported, not failed.
		if v3 && !isInvariantGroup(groupOf(name)) {
			t.Logf("improvement over %s (group %s)\n--- want\n%s--- got\n%s", label, groupOf(name), w, g)
			return
		}
		t.Errorf("response differs from %s\n--- want\n%s--- got\n%s", label, w, g)
	}
}
