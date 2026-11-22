package conformance

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// Actors name whose credentials a step sends.
const (
	actorOwner    = "owner"    // registered owner device: every file grant
	actorGuest    = "guest"    // code-paired device: browse only
	actorUploader = "uploader" // code-paired device the owner later grants upload; not the session's opener
	actorRevoked  = "revoked"  // code-paired device the owner revokes mid-table
	actorJailed   = "jailed"   // code-paired device the owner confines to <root>/docs
	actorLogged   = "logged"   // device that signs in with the owner's password mid-table
	actorAnon     = "anon"     // no Authorization header
	actorBadToken = "badtoken" // a bearer value the agent never issued

	// Identities that hold no token at the start: the table pairs or logs them in.
	identityLogger    = "logger"
	identityRequester = "requester"

	// notAToken is sent by actorBadToken; it is obviously not a credential.
	notAToken = "not-a-real-token"
	// missingTransferID is a well-formed id that no session has.
	missingTransferID = "00000000-0000-0000-0000-000000000000"
	// uploadPayload is the bytes the chunked-upload cases send, in chunks of uploadChunk.
	uploadPayload = "0123456789"
	uploadChunk   = 4
	// pairClientNonce is the 16-byte (hex) nonce the approve-on-PC pairing cases commit to and reveal.
	pairClientNonce = "00112233445566778899aabbccddeeff"
	// trashOnlyFile is the fixture the trash cases move to the bin and restore.
	trashOnlyFile = "delete-me.txt"
)

// step is one request in the table. path, header values, json and raw may use
// {{name}} placeholders (see expand); capture copies keys of a JSON reply (a
// dotted path such as items.0.id reaches into arrays) into variables for later
// steps.
//
//   - proofFor merges a fresh device proof (id, label, public key, nonce,
//     signature) of the named identity into the JSON body.
//   - tokenFor stores the reply's deviceToken as the named actor's token.
//   - relExpiry reports expiresAt as seconds from now (rounded) instead of masking it.
type step struct {
	name      string
	as        string
	method    string
	path      string
	header    map[string]string
	json      string
	raw       string
	capture   map[string]string
	proofFor  string
	tokenFor  string
	relExpiry bool
	sortList  string // JSON array to sort before comparing (listKeyTop: the body itself); for lists whose tie order is unstable
}

// listKeyTop names the whole body in step.sortList.
const listKeyTop = "."

// sortResultList sorts the JSON array named by key (listKeyTop: the body) by
// its elements' JSON text, so entries the agent orders arbitrarily compare equal.
func sortResultList(r *result, key string) {
	sortAny := func(list []any) {
		sort.Slice(list, func(i, j int) bool {
			a, _ := json.Marshal(list[i])
			b, _ := json.Marshal(list[j])
			return string(a) < string(b)
		})
	}
	if key == listKeyTop {
		if list, ok := r.JSON.([]any); ok {
			sortAny(list)
		}
		return
	}
	if m, ok := r.JSON.(map[string]any); ok {
		if list, ok := m[key].([]any); ok {
			sortAny(list)
		}
	}
}

var placeholderRe = regexp.MustCompile(`\{\{(\w+)\}\}`)

// expand substitutes {{name}} placeholders in tmpl with vars, passing each
// value through esc. An unknown name is an error so a typo cannot go unnoticed.
func expand(tmpl string, vars map[string]string, esc func(string) string) (string, error) {
	var missing []string
	out := placeholderRe.ReplaceAllStringFunc(tmpl, func(m string) string {
		name := placeholderRe.FindStringSubmatch(m)[1]
		v, ok := vars[name]
		if !ok {
			missing = append(missing, name)
		}
		return esc(v)
	})
	if len(missing) > 0 {
		return "", fmt.Errorf("unknown placeholder(s) %v in %q", missing, tmpl)
	}
	return out, nil
}

// jsonEscape escapes s for use inside a JSON string literal.
func jsonEscape(s string) string {
	b, _ := json.Marshal(s)
	return string(b[1 : len(b)-1])
}

// hexSHA256 is the lowercase hex SHA-256 of s.
func hexSHA256(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

// staticVars are the placeholder values that do not depend on a running agent.
func staticVars() map[string]string {
	nonce, _ := hex.DecodeString(pairClientNonce)
	return map[string]string{
		"payloadSHA":  hexSHA256(uploadPayload),
		"c0SHA":       hexSHA256(uploadPayload[0:4]),
		"c1SHA":       hexSHA256(uploadPayload[4:8]),
		"c2SHA":       hexSHA256(uploadPayload[8:]),
		"wrongSHA":    hexSHA256("not the chunk"),
		"missingID":   missingTransferID,
		"unknownLink": "no-such-share-link",
		"clientNonce": pairClientNonce,
		"nonceCommit": hexSHA256(string(nonce)),
		"ownerUser":   ownerUser,
		"ownerPass":   ownerPassword,
	}
}

// jsonPath reads a dotted path (object keys and array indexes) out of a decoded JSON value.
func jsonPath(v any, path string) (any, bool) {
	for _, part := range strings.Split(path, ".") {
		switch x := v.(type) {
		case map[string]any:
			child, ok := x[part]
			if !ok {
				return nil, false
			}
			v = child
		case []any:
			i, err := strconv.Atoi(part)
			if err != nil || i < 0 || i >= len(x) {
				return nil, false
			}
			v = x[i]
		default:
			return nil, false
		}
	}
	return v, v != nil
}

// buildCases returns the request table in execution order. Later steps depend
// on state left by earlier ones (created files, an open upload session, a
// minted share link, a revoked device), so the order is part of the contract.
func buildCases() []step {
	const jsonType = "application/json"
	js := map[string]string{"Content-Type": jsonType}
	get := http.MethodGet
	post := http.MethodPost
	put := http.MethodPut
	patch := http.MethodPatch
	del := http.MethodDelete
	chunk := func(sum string) map[string]string { return map[string]string{"X-Chunk-Sha256": sum} }
	var cases []step
	add := func(s ...step) { cases = append(cases, s...) }

	// Health and authentication.
	add(
		step{name: "health-anon", as: actorAnon, method: get, path: "/v1/health"},
		step{name: "health-owner", as: actorOwner, method: get, path: "/v1/health"},
		step{name: "health-bad-token", as: actorBadToken, method: get, path: "/v1/health"},
		step{name: "unauth-fs-list", as: actorAnon, method: get, path: "/v1/fs?path={{root}}"},
		step{name: "bad-token-fs-list", as: actorBadToken, method: get, path: "/v1/fs?path={{root}}"},

		// Filesystem: list and stat.
		step{name: "fs-list-root", as: actorOwner, method: get, path: "/v1/fs?path={{root}}"},
		step{name: "fs-list-docs-limit1", as: actorOwner, method: get, path: "/v1/fs?path={{root}}/docs&limit=1"},
		step{name: "fs-list-missing", as: actorOwner, method: get, path: "/v1/fs?path={{root}}/nope"},
		step{name: "fs-list-outside-root", as: actorOwner, method: get, path: "/v1/fs?path={{root}}/.."},
		step{name: "fs-list-no-path", as: actorOwner, method: get, path: "/v1/fs"},
		step{name: "fs-stat-file", as: actorOwner, method: get, path: "/v1/fs/meta?path={{root}}/docs/hello.txt"},
		step{name: "fs-stat-dir", as: actorOwner, method: get, path: "/v1/fs/meta?path={{root}}/docs"},
		step{name: "fs-stat-missing", as: actorOwner, method: get, path: "/v1/fs/meta?path={{root}}/nope"},

		// Search runs early: the index is built from the fixture tree before any
		// request changes it, so the results do not depend on indexing delay.
		step{name: "search-name", as: actorOwner, method: get, path: "/v1/search?q=needle"},
		step{name: "search-content", as: actorOwner, method: get, path: "/v1/search?q=findme-content-token"},
		step{name: "search-ext", as: actorOwner, method: get, path: "/v1/search?q=beta&ext=log"},
		step{name: "search-limit1", as: actorOwner, method: get, path: "/v1/search?q=txt&limit=1"},
		step{name: "search-no-query", as: actorOwner, method: get, path: "/v1/search"},
		step{name: "search-guest", as: actorGuest, method: get, path: "/v1/search?q=needle"},
		step{name: "search-anon", as: actorAnon, method: get, path: "/v1/search?q=needle"},

		// Thumbnails.
		step{name: "thumb-png", as: actorOwner, method: get, path: "/v1/thumb?path={{root}}/data/pic.png&size=128"},
		step{name: "thumb-png-large", as: actorOwner, method: get, path: "/v1/thumb?path={{root}}/data/pic.png&size=512"},
		step{name: "thumb-png-odd-size", as: actorOwner, method: get, path: "/v1/thumb?path={{root}}/data/pic.png&size=77"},
		step{name: "thumb-text-file", as: actorOwner, method: get, path: "/v1/thumb?path={{root}}/docs/hello.txt&size=128"},
		step{name: "thumb-missing", as: actorOwner, method: get, path: "/v1/thumb?path={{root}}/nope.png&size=128"},
		step{name: "thumb-no-path", as: actorOwner, method: get, path: "/v1/thumb"},
		step{name: "thumb-anon", as: actorAnon, method: get, path: "/v1/thumb?path={{root}}/data/pic.png&size=128"},

		// Status, metrics and drives.
		step{name: "status-owner", as: actorOwner, method: get, path: "/v1/status"},
		step{name: "status-anon", as: actorAnon, method: get, path: "/v1/status"},
		step{name: "metrics-owner", as: actorOwner, method: get, path: "/v1/metrics"},
		step{name: "metrics-guest", as: actorGuest, method: get, path: "/v1/metrics"},
		step{name: "drives-owner", as: actorOwner, method: get, path: "/v1/system/drives"},
		step{name: "drives-anon", as: actorAnon, method: get, path: "/v1/system/drives"},

		// Filesystem: create, rename.
		step{name: "fs-mkdir", as: actorOwner, method: post, path: "/v1/fs/folder", header: js, json: `{"path":"{{root}}/made"}`},
		step{name: "fs-mkdir-exists", as: actorOwner, method: post, path: "/v1/fs/folder", header: js, json: `{"path":"{{root}}/made"}`},
		step{name: "fs-mkdir-no-path", as: actorOwner, method: post, path: "/v1/fs/folder", header: js, json: `{}`},
		step{name: "fs-create-file", as: actorOwner, method: post, path: "/v1/fs/file", header: js, json: `{"path":"{{root}}/made/new.txt"}`},
		step{name: "fs-rename", as: actorOwner, method: patch, path: "/v1/fs/rename", header: js, json: `{"src":"{{root}}/made/new.txt","dst":"{{root}}/made/renamed.txt"}`},
		step{name: "fs-rename-missing", as: actorOwner, method: patch, path: "/v1/fs/rename", header: js, json: `{"src":"{{root}}/made/gone.txt","dst":"{{root}}/made/other.txt"}`},
		step{name: "fs-list-made", as: actorOwner, method: get, path: "/v1/fs?path={{root}}/made"},

		// Content: whole-file read and write.
		step{name: "content-get", as: actorOwner, method: get, path: "/v1/content?path={{root}}/docs/hello.txt"},
		step{name: "content-get-missing", as: actorOwner, method: get, path: "/v1/content?path={{root}}/nope.txt"},
		step{name: "content-get-dir", as: actorOwner, method: get, path: "/v1/content?path={{root}}/docs"},
		step{name: "content-get-no-path", as: actorOwner, method: get, path: "/v1/content"},
		step{name: "content-put", as: actorOwner, method: put, path: "/v1/content?path={{root}}/made/renamed.txt", raw: "put body\n"},
		step{name: "content-put-readback", as: actorOwner, method: get, path: "/v1/content?path={{root}}/made/renamed.txt"},
		step{name: "content-put-missing", as: actorOwner, method: put, path: "/v1/content?path={{root}}/made/never-created.txt", raw: "x"},

		// Download with Range.
		step{name: "download-range-middle", as: actorOwner, method: get, path: "/v1/content?path={{root}}/data/range.txt", header: map[string]string{"Range": "bytes=10-19"}},
		step{name: "download-range-suffix", as: actorOwner, method: get, path: "/v1/content?path={{root}}/data/range.txt", header: map[string]string{"Range": "bytes=-5"}},
		step{name: "download-range-open-end", as: actorOwner, method: get, path: "/v1/content?path={{root}}/data/range.txt", header: map[string]string{"Range": "bytes=95-"}},
		step{name: "download-range-unsatisfiable", as: actorOwner, method: get, path: "/v1/content?path={{root}}/data/range.txt", header: map[string]string{"Range": "bytes=500-"}},
		step{name: "download-range-multi", as: actorOwner, method: get, path: "/v1/content?path={{root}}/data/range.txt", header: map[string]string{"Range": "bytes=0-1,5-6"}},
		step{name: "download-full", as: actorOwner, method: get, path: "/v1/content?path={{root}}/data/range.txt"},

		// Chunked upload with SHA-256, plus the other-device checks.
		step{name: "upload-create", as: actorOwner, method: post, path: "/v1/transfers", header: js,
			json:    `{"path":"{{root}}/made/up.txt","size":10,"sha256":"{{payloadSHA}}","chunkSize":4}`,
			capture: map[string]string{"sid": "id"}},
		step{name: "upload-create-bad-body", as: actorOwner, method: post, path: "/v1/transfers", header: js, json: `{"path":"{{root}}/made/bad.txt"}`},
		step{name: "upload-status-open", as: actorOwner, method: get, path: "/v1/transfers/{{sid}}"},
		step{name: "upload-chunk-0", as: actorOwner, method: put, path: "/v1/transfers/{{sid}}/chunks/0", header: chunk("{{c0SHA}}"), raw: uploadPayload[0:4]},
		step{name: "upload-chunk-wrong-hash", as: actorOwner, method: put, path: "/v1/transfers/{{sid}}/chunks/1", header: chunk("{{wrongSHA}}"), raw: uploadPayload[4:8]},
		step{name: "upload-chunk-no-hash", as: actorOwner, method: put, path: "/v1/transfers/{{sid}}/chunks/1", raw: uploadPayload[4:8]},
		step{name: "upload-chunk-out-of-range", as: actorOwner, method: put, path: "/v1/transfers/{{sid}}/chunks/9", header: chunk("{{c1SHA}}"), raw: uploadPayload[4:8]},
		step{name: "upload-chunk-1", as: actorOwner, method: put, path: "/v1/transfers/{{sid}}/chunks/1", header: chunk("{{c1SHA}}"), raw: uploadPayload[4:8]},
		step{name: "upload-status-partial", as: actorOwner, method: get, path: "/v1/transfers/{{sid}}"},
		step{name: "upload-status-other-device", as: actorGuest, method: get, path: "/v1/transfers/{{sid}}"},
		step{name: "grant-upload", as: actorOwner, method: patch, path: "/v1/devices/{{uploaderID}}", header: js, json: `{"upload":true}`},
		step{name: "upload-status-other-uploader", as: actorUploader, method: get, path: "/v1/transfers/{{sid}}"},
		step{name: "upload-chunk-other-uploader", as: actorUploader, method: put, path: "/v1/transfers/{{sid}}/chunks/2", header: chunk("{{c2SHA}}"), raw: uploadPayload[8:]},
		step{name: "upload-complete-other-uploader", as: actorUploader, method: post, path: "/v1/transfers/{{sid}}/complete"},
		step{name: "upload-chunk-other-device", as: actorGuest, method: put, path: "/v1/transfers/{{sid}}/chunks/2", header: chunk("{{c2SHA}}"), raw: uploadPayload[8:]},
		step{name: "upload-complete-other-device", as: actorGuest, method: post, path: "/v1/transfers/{{sid}}/complete"},
		step{name: "upload-complete-incomplete", as: actorOwner, method: post, path: "/v1/transfers/{{sid}}/complete"},
		step{name: "upload-chunk-2", as: actorOwner, method: put, path: "/v1/transfers/{{sid}}/chunks/2", header: chunk("{{c2SHA}}"), raw: uploadPayload[8:]},
		step{name: "upload-complete", as: actorOwner, method: post, path: "/v1/transfers/{{sid}}/complete"},
		step{name: "upload-status-done", as: actorOwner, method: get, path: "/v1/transfers/{{sid}}"},
		step{name: "upload-readback", as: actorOwner, method: get, path: "/v1/content?path={{root}}/made/up.txt"},
		step{name: "upload-create-conflict", as: actorOwner, method: post, path: "/v1/transfers", header: js,
			json: `{"path":"{{root}}/docs/hello.txt","size":10,"sha256":"{{payloadSHA}}","chunkSize":4}`},
		step{name: "upload-status-missing", as: actorOwner, method: get, path: "/v1/transfers/{{missingID}}"},

		// Upload resume: reopen a half-sent session by creating it again, then finish it.
		step{name: "upload-resume-create", as: actorOwner, method: post, path: "/v1/transfers", header: js,
			json:    `{"path":"{{root}}/made/resume.txt","size":10,"sha256":"{{payloadSHA}}","chunkSize":4}`,
			capture: map[string]string{"rid": "id"}},
		step{name: "upload-resume-chunk-0", as: actorOwner, method: put, path: "/v1/transfers/{{rid}}/chunks/0", header: chunk("{{c0SHA}}"), raw: uploadPayload[0:4]},
		step{name: "upload-resume-recreate", as: actorOwner, method: post, path: "/v1/transfers", header: js,
			json: `{"path":"{{root}}/made/resume.txt","size":10,"sha256":"{{payloadSHA}}","chunkSize":4}`},
		step{name: "upload-resume-status", as: actorOwner, method: get, path: "/v1/transfers/{{rid}}"},
		step{name: "upload-resume-chunk-0-again", as: actorOwner, method: put, path: "/v1/transfers/{{rid}}/chunks/0", header: chunk("{{c0SHA}}"), raw: uploadPayload[0:4]},
		step{name: "upload-resume-chunk-1", as: actorOwner, method: put, path: "/v1/transfers/{{rid}}/chunks/1", header: chunk("{{c1SHA}}"), raw: uploadPayload[4:8]},
		step{name: "upload-resume-chunk-2", as: actorOwner, method: put, path: "/v1/transfers/{{rid}}/chunks/2", header: chunk("{{c2SHA}}"), raw: uploadPayload[8:]},
		step{name: "upload-resume-complete", as: actorOwner, method: post, path: "/v1/transfers/{{rid}}/complete"},
		step{name: "upload-resume-complete-again", as: actorOwner, method: post, path: "/v1/transfers/{{rid}}/complete"},

		// Upload whose whole-file hash is wrong: every chunk is accepted, completion is refused.
		step{name: "upload-mismatch-create", as: actorOwner, method: post, path: "/v1/transfers", header: js,
			json:    `{"path":"{{root}}/made/mismatch.txt","size":10,"sha256":"{{wrongSHA}}","chunkSize":4}`,
			capture: map[string]string{"mid": "id"}},
		step{name: "upload-mismatch-chunk-0", as: actorOwner, method: put, path: "/v1/transfers/{{mid}}/chunks/0", header: chunk("{{c0SHA}}"), raw: uploadPayload[0:4]},
		step{name: "upload-mismatch-chunk-1", as: actorOwner, method: put, path: "/v1/transfers/{{mid}}/chunks/1", header: chunk("{{c1SHA}}"), raw: uploadPayload[4:8]},
		step{name: "upload-mismatch-chunk-2", as: actorOwner, method: put, path: "/v1/transfers/{{mid}}/chunks/2", header: chunk("{{c2SHA}}"), raw: uploadPayload[8:]},
		step{name: "upload-mismatch-complete", as: actorOwner, method: post, path: "/v1/transfers/{{mid}}/complete"},
		step{name: "upload-mismatch-status", as: actorOwner, method: get, path: "/v1/transfers/{{mid}}"},
		step{name: "upload-mismatch-target-absent", as: actorOwner, method: get, path: "/v1/fs/meta?path={{root}}/made/mismatch.txt"},
		step{name: "upload-abort", as: actorOwner, method: del, path: "/v1/transfers/{{mid}}"},
		step{name: "upload-abort-again", as: actorOwner, method: del, path: "/v1/transfers/{{mid}}"},
		step{name: "upload-abort-other-device", as: actorGuest, method: del, path: "/v1/transfers/{{rid}}"},
		step{name: "transfers-list-owner", as: actorOwner, method: get, path: "/v1/transfers/list", sortList: "transfers"},
		step{name: "transfers-list-anon", as: actorAnon, method: get, path: "/v1/transfers/list"},

		// Share link: refused while sharing is off (the default), then mint, fetch once, fetch again.
		step{name: "share-mint-disabled", as: actorOwner, method: post, path: "/v1/share/mint", header: js, json: `{"path":"{{root}}/docs/hello.txt"}`},
		step{name: "settings-enable-sharing", as: actorOwner, method: patch, path: "/v1/settings", header: js, json: `{"allowSharing":true}`},
		step{name: "share-mint", as: actorOwner, method: post, path: "/v1/share/mint", header: js,
			json:    `{"path":"{{root}}/docs/hello.txt","expiresInSeconds":600}`,
			capture: map[string]string{"shareToken": "token"}},
		step{name: "share-mint-missing", as: actorOwner, method: post, path: "/v1/share/mint", header: js, json: `{"path":"{{root}}/nope.txt"}`},
		step{name: "share-fetch", as: actorAnon, method: get, path: "/v1/share/{{shareToken}}"},
		step{name: "share-fetch-again", as: actorAnon, method: get, path: "/v1/share/{{shareToken}}"},
		step{name: "share-fetch-unknown", as: actorAnon, method: get, path: "/v1/share/{{unknownLink}}"},
		step{name: "share-mint-default-expiry", as: actorOwner, method: post, path: "/v1/share/mint", header: js, relExpiry: true,
			json:    `{"path":"{{root}}/data/range.txt"}`,
			capture: map[string]string{"shareToken2": "token", "shareHash2": "tokenHash"}},
		step{name: "share-mint-too-long", as: actorOwner, method: post, path: "/v1/share/mint", header: js, json: `{"path":"{{root}}/data/range.txt","expiresInSeconds":99999999}`},
		step{name: "share-mint-directory", as: actorOwner, method: post, path: "/v1/share/mint", header: js, json: `{"path":"{{root}}/docs"}`},
		step{name: "share-fetch-range", as: actorAnon, method: get, path: "/v1/share/{{shareToken2}}", header: map[string]string{"Range": "bytes=10-19"}},
		step{name: "share-list-owner", as: actorOwner, method: get, path: "/v1/share"},
		step{name: "share-list-guest", as: actorGuest, method: get, path: "/v1/share"},
		step{name: "share-revoke-other-device", as: actorGuest, method: del, path: "/v1/share/{{shareHash2}}"},
		step{name: "share-revoke", as: actorOwner, method: del, path: "/v1/share/{{shareHash2}}"},
		step{name: "share-revoke-again", as: actorOwner, method: del, path: "/v1/share/{{shareHash2}}"},
		step{name: "share-fetch-revoked", as: actorAnon, method: get, path: "/v1/share/{{shareToken2}}"},

		// A browse-only device is refused everything that writes, reads bytes, or shares.
		step{name: "guest-fs-list", as: actorGuest, method: get, path: "/v1/fs?path={{root}}/docs"},
		step{name: "guest-mkdir", as: actorGuest, method: post, path: "/v1/fs/folder", header: js, json: `{"path":"{{root}}/guest-made"}`},
		step{name: "guest-content-get", as: actorGuest, method: get, path: "/v1/content?path={{root}}/docs/hello.txt"},
		step{name: "guest-delete", as: actorGuest, method: del, path: "/v1/fs?path={{root}}/docs/notes.md"},
		step{name: "guest-share-mint", as: actorGuest, method: post, path: "/v1/share/mint", header: js, json: `{"path":"{{root}}/docs/hello.txt"}`},
		step{name: "guest-content-put", as: actorGuest, method: put, path: "/v1/content?path={{root}}/docs/hello.txt", raw: "x"},
		step{name: "guest-rename", as: actorGuest, method: patch, path: "/v1/fs/rename", header: js, json: `{"src":"{{root}}/docs/hello.txt","dst":"{{root}}/docs/h2.txt"}`},
		step{name: "guest-copy", as: actorGuest, method: post, path: "/v1/fs/copy", header: js, json: `{"sources":["{{root}}/docs/hello.txt"],"destDir":"{{root}}/data"}`},
		step{name: "guest-upload-create", as: actorGuest, method: post, path: "/v1/transfers", header: js, json: `{"path":"{{root}}/data/g.txt","size":10,"sha256":"{{payloadSHA}}","chunkSize":4}`},
		step{name: "guest-checksum", as: actorGuest, method: get, path: "/v1/fs/checksum?path={{root}}/docs/hello.txt"},
		step{name: "guest-archive", as: actorGuest, method: get, path: "/v1/fs/archive?path={{root}}/data/docs.zip"},

		// Delete: to trash, a missing path, permanently, and no path at all.
		step{name: "fs-delete-trash", as: actorOwner, method: del, path: "/v1/fs?path={{root}}/delete-me.txt"},
		step{name: "fs-delete-missing", as: actorOwner, method: del, path: "/v1/fs?path={{root}}/does-not-exist.txt"},
		step{name: "fs-delete-permanent", as: actorOwner, method: del, path: "/v1/fs?path={{root}}/delete-perm.txt&permanent=true"},
		step{name: "fs-delete-missing-permanent", as: actorOwner, method: del, path: "/v1/fs?path={{root}}/does-not-exist.txt&permanent=true"},
		step{name: "fs-delete-batch-body", as: actorOwner, method: del, path: "/v1/fs?permanent=true", header: js,
			json: `{"paths":["{{root}}/docs/sub/nested.txt","{{root}}/missing-in-batch.txt"]}`},
		step{name: "fs-delete-no-path", as: actorOwner, method: del, path: "/v1/fs"},
		step{name: "fs-list-after-delete", as: actorOwner, method: get, path: "/v1/fs?path={{root}}"},

		// Trash: list, restore, restore errors, empty.
		step{name: "trash-list", as: actorOwner, method: get, path: "/v1/trash", capture: map[string]string{"trashID": "items.0.id"}},
		step{name: "trash-list-guest", as: actorGuest, method: get, path: "/v1/trash"},
		step{name: "trash-restore", as: actorOwner, method: post, path: "/v1/trash/restore", header: js, json: `{"ids":["{{trashID}}"]}`},
		step{name: "trash-restore-missing", as: actorOwner, method: post, path: "/v1/trash/restore", header: js, json: `{"ids":["{{missingID}}"]}`},
		step{name: "trash-restore-no-ids", as: actorOwner, method: post, path: "/v1/trash/restore", header: js, json: `{}`},
		step{name: "trash-restore-guest", as: actorGuest, method: post, path: "/v1/trash/restore", header: js, json: `{"ids":["{{missingID}}"]}`},
		step{name: "trash-restored-file", as: actorOwner, method: get, path: "/v1/fs/meta?path={{root}}/" + trashOnlyFile},
		step{name: "trash-fill-again", as: actorOwner, method: del, path: "/v1/fs?path={{root}}/" + trashOnlyFile},
		step{name: "trash-delete-bad-body", as: actorOwner, method: del, path: "/v1/trash", header: js, json: `{"ids":`},
		step{name: "trash-delete-one-missing", as: actorOwner, method: del, path: "/v1/trash", header: js, json: `{"ids":["{{missingID}}"]}`},
		step{name: "trash-empty-guest", as: actorGuest, method: del, path: "/v1/trash"},
		step{name: "trash-empty", as: actorOwner, method: del, path: "/v1/trash"},
		step{name: "trash-list-empty", as: actorOwner, method: get, path: "/v1/trash"},

		// Copy, move, checksum, recent, media info, chmod, archive.
		step{name: "fs-copy", as: actorOwner, method: post, path: "/v1/fs/copy", header: js, json: `{"sources":["{{root}}/data/needle-alpha.txt"],"destDir":"{{root}}/made"}`},
		step{name: "fs-copy-conflict", as: actorOwner, method: post, path: "/v1/fs/copy", header: js, json: `{"sources":["{{root}}/data/needle-alpha.txt"],"destDir":"{{root}}/made"}`},
		step{name: "fs-copy-duplicate", as: actorOwner, method: post, path: "/v1/fs/copy", header: js, json: `{"sources":["{{root}}/data/needle-alpha.txt"],"destDir":"{{root}}/made","duplicate":true}`},
		step{name: "fs-copy-overwrite", as: actorOwner, method: post, path: "/v1/fs/copy", header: js, json: `{"sources":["{{root}}/data/needle-alpha.txt"],"destDir":"{{root}}/made","overwrite":true}`},
		step{name: "fs-copy-missing-source", as: actorOwner, method: post, path: "/v1/fs/copy", header: js, json: `{"sources":["{{root}}/data/not-there.txt"],"destDir":"{{root}}/made"}`},
		step{name: "fs-copy-no-body", as: actorOwner, method: post, path: "/v1/fs/copy", header: js, json: `{}`},
		step{name: "fs-move", as: actorOwner, method: post, path: "/v1/fs/move", header: js, json: `{"sources":["{{root}}/made/needle-alpha.txt"],"destDir":"{{root}}/docs/sub"}`},
		step{name: "fs-move-missing-source", as: actorOwner, method: post, path: "/v1/fs/move", header: js, json: `{"sources":["{{root}}/made/needle-alpha.txt"],"destDir":"{{root}}/docs/sub"}`},
		step{name: "fs-move-into-itself", as: actorOwner, method: post, path: "/v1/fs/move", header: js, json: `{"sources":["{{root}}/docs"],"destDir":"{{root}}/docs/sub"}`},
		step{name: "fs-list-after-move", as: actorOwner, method: get, path: "/v1/fs?path={{root}}/docs/sub"},
		step{name: "fs-checksum", as: actorOwner, method: get, path: "/v1/fs/checksum?path={{root}}/data/range.txt"},
		step{name: "fs-checksum-sha256", as: actorOwner, method: get, path: "/v1/fs/checksum?path={{root}}/data/range.txt&algo=sha256"},
		step{name: "fs-checksum-bad-algo", as: actorOwner, method: get, path: "/v1/fs/checksum?path={{root}}/data/range.txt&algo=rot13"},
		step{name: "fs-checksum-dir", as: actorOwner, method: get, path: "/v1/fs/checksum?path={{root}}/docs"},
		step{name: "fs-checksum-missing", as: actorOwner, method: get, path: "/v1/fs/checksum?path={{root}}/nope"},
		step{name: "fs-checksums-batch", as: actorOwner, method: post, path: "/v1/fs/checksums", header: js, json: `{"paths":["{{root}}/data/range.txt","{{root}}/nope"],"algo":"sha256"}`},
		step{name: "fs-checksums-no-paths", as: actorOwner, method: post, path: "/v1/fs/checksums", header: js, json: `{"paths":[]}`},
		step{name: "fs-recent", as: actorOwner, method: get, path: "/v1/fs/recent?limit=3", sortList: listKeyTop},
		step{name: "fs-recent-bad-root", as: actorOwner, method: get, path: "/v1/fs/recent?root={{root}}/../.."},
		step{name: "fs-recent-anon", as: actorAnon, method: get, path: "/v1/fs/recent"},
		step{name: "fs-media-info-png", as: actorOwner, method: get, path: "/v1/fs/media-info?path={{root}}/data/pic.png"},
		step{name: "fs-media-info-text", as: actorOwner, method: get, path: "/v1/fs/media-info?path={{root}}/docs/hello.txt"},
		step{name: "fs-media-info-missing", as: actorOwner, method: get, path: "/v1/fs/media-info?path={{root}}/nope.png"},
		step{name: "fs-chmod", as: actorOwner, method: post, path: "/v1/fs/chmod", header: js, json: `{"path":"{{root}}/data/run.sh","mode":"755"}`},
		step{name: "fs-chmod-readback", as: actorOwner, method: get, path: "/v1/fs/meta?path={{root}}/data/run.sh"},
		step{name: "fs-chmod-bad-mode", as: actorOwner, method: post, path: "/v1/fs/chmod", header: js, json: `{"path":"{{root}}/data/run.sh","mode":"999"}`},
		step{name: "fs-chmod-setuid", as: actorOwner, method: post, path: "/v1/fs/chmod", header: js, json: `{"path":"{{root}}/data/run.sh","mode":"4755"}`},
		step{name: "fs-chmod-missing", as: actorOwner, method: post, path: "/v1/fs/chmod", header: js, json: `{"path":"{{root}}/data/nope.sh","mode":"644"}`},
		step{name: "fs-chmod-guest", as: actorGuest, method: post, path: "/v1/fs/chmod", header: js, json: `{"path":"{{root}}/data/run.sh","mode":"644"}`},
		step{name: "fs-compress", as: actorOwner, method: post, path: "/v1/fs/compress", header: js, json: `{"sources":["{{root}}/docs"],"dest":"{{root}}/data/docs.zip"}`},
		step{name: "fs-compress-missing-source", as: actorOwner, method: post, path: "/v1/fs/compress", header: js, json: `{"sources":["{{root}}/nope"],"dest":"{{root}}/data/nope.zip"}`},
		// The entry order of an archive listing is not stable in the current
		// agent, so the list is sorted; a limited listing is left out for the same reason.
		step{name: "fs-archive-list", as: actorOwner, method: get, path: "/v1/fs/archive?path={{root}}/data/docs.zip", sortList: "entries"},
		step{name: "fs-archive-not-archive", as: actorOwner, method: get, path: "/v1/fs/archive?path={{root}}/docs/hello.txt"},
		step{name: "fs-archive-missing", as: actorOwner, method: get, path: "/v1/fs/archive?path={{root}}/nope.zip"},
		step{name: "fs-extract", as: actorOwner, method: post, path: "/v1/fs/extract", header: js, json: `{"archive":"{{root}}/data/docs.zip","destDir":"{{root}}/data/extracted"}`},
		step{name: "fs-extract-list", as: actorOwner, method: get, path: "/v1/fs?path={{root}}/data/extracted"},
		step{name: "fs-extract-not-archive", as: actorOwner, method: post, path: "/v1/fs/extract", header: js, json: `{"archive":"{{root}}/docs/hello.txt","destDir":"{{root}}/data/extracted2"}`},

		// Jail escape attempts by an unconfined device (the root list is the boundary) and a jailed one.
		step{name: "escape-list-dotdot", as: actorOwner, method: get, path: "/v1/fs?path={{root}}/docs/../../"},
		step{name: "escape-meta-absolute", as: actorOwner, method: get, path: "/v1/fs/meta?path={{outside}}/secret.txt"},
		step{name: "escape-content-sibling", as: actorOwner, method: get, path: "/v1/content?path={{outside}}/secret.txt"},
		step{name: "escape-mkdir-sibling", as: actorOwner, method: post, path: "/v1/fs/folder", header: js, json: `{"path":"{{root}}/../escaped"}`},
		step{name: "escape-rename-out", as: actorOwner, method: patch, path: "/v1/fs/rename", header: js, json: `{"src":"{{root}}/docs/hello.txt","dst":"{{outside}}/hello.txt"}`},
		step{name: "escape-copy-out", as: actorOwner, method: post, path: "/v1/fs/copy", header: js, json: `{"sources":["{{root}}/docs/hello.txt"],"destDir":"{{outside}}"}`},
		step{name: "escape-symlink-list", as: actorOwner, method: get, path: "/v1/fs?path={{root}}/data/escape-link"},
		step{name: "escape-symlink-content", as: actorOwner, method: get, path: "/v1/content?path={{root}}/data/escape-link/secret.txt"},
		step{name: "escape-symlink-put", as: actorOwner, method: put, path: "/v1/content?path={{root}}/data/escape-link/secret.txt", raw: "overwritten"},
		step{name: "grant-jailed", as: actorOwner, method: patch, path: "/v1/devices/{{jailedID}}", header: js, json: `{"jailRoot":"{{root}}/docs","download":true}`},
		step{name: "jailed-list-inside", as: actorJailed, method: get, path: "/v1/fs?path={{root}}/docs"},
		step{name: "jailed-list-outside", as: actorJailed, method: get, path: "/v1/fs?path={{root}}/data"},
		step{name: "jailed-list-dotdot", as: actorJailed, method: get, path: "/v1/fs?path={{root}}/docs/../data"},
		step{name: "jailed-content-inside", as: actorJailed, method: get, path: "/v1/content?path={{root}}/docs/hello.txt"},
		step{name: "jailed-content-outside", as: actorJailed, method: get, path: "/v1/content?path={{root}}/data/range.txt"},
		step{name: "jailed-search", as: actorJailed, method: get, path: "/v1/search?q=hello"},
		step{name: "jailed-recent", as: actorJailed, method: get, path: "/v1/fs/recent", sortList: listKeyTop},

		// Settings, read-only mode and bandwidth.
		step{name: "settings-get-owner", as: actorOwner, method: get, path: "/v1/settings"},
		step{name: "settings-get-guest", as: actorGuest, method: get, path: "/v1/settings"},
		step{name: "settings-get-anon", as: actorAnon, method: get, path: "/v1/settings"},
		step{name: "settings-patch-guest", as: actorGuest, method: patch, path: "/v1/settings", header: js, json: `{"allowSharing":false}`},
		step{name: "settings-patch-bad-body", as: actorOwner, method: patch, path: "/v1/settings", header: js, json: `{"roots":"not-a-list"}`},
		step{name: "settings-patch-read-only", as: actorOwner, method: patch, path: "/v1/settings", header: js, json: `{"readOnly":true}`},
		step{name: "read-only-mkdir", as: actorOwner, method: post, path: "/v1/fs/folder", header: js, json: `{"path":"{{root}}/ro-made"}`},
		step{name: "read-only-content-put", as: actorOwner, method: put, path: "/v1/content?path={{root}}/made/renamed.txt", raw: "ro"},
		step{name: "read-only-delete", as: actorOwner, method: del, path: "/v1/fs?path={{root}}/made/renamed.txt"},
		step{name: "read-only-rename", as: actorOwner, method: patch, path: "/v1/fs/rename", header: js, json: `{"src":"{{root}}/made/renamed.txt","dst":"{{root}}/made/ro.txt"}`},
		step{name: "read-only-upload-create", as: actorOwner, method: post, path: "/v1/transfers", header: js, json: `{"path":"{{root}}/made/ro-up.txt","size":10,"sha256":"{{payloadSHA}}","chunkSize":4}`},
		step{name: "read-only-chmod", as: actorOwner, method: post, path: "/v1/fs/chmod", header: js, json: `{"path":"{{root}}/data/run.sh","mode":"644"}`},
		step{name: "read-only-trash-empty", as: actorOwner, method: del, path: "/v1/trash"},
		step{name: "read-only-read-still-works", as: actorOwner, method: get, path: "/v1/content?path={{root}}/made/renamed.txt"},
		step{name: "read-only-health", as: actorOwner, method: get, path: "/v1/health"},
		step{name: "settings-patch-writable", as: actorOwner, method: patch, path: "/v1/settings", header: js, json: `{"readOnly":false}`},
		step{name: "bandwidth-get", as: actorOwner, method: get, path: "/v1/settings/bandwidth"},
		step{name: "bandwidth-put", as: actorOwner, method: put, path: "/v1/settings/bandwidth", header: js, json: `{"maxUploadBytesPerSec":1048576,"maxDownloadBytesPerSec":0}`},
		step{name: "bandwidth-get-after", as: actorOwner, method: get, path: "/v1/settings/bandwidth"},
		step{name: "bandwidth-put-negative", as: actorOwner, method: put, path: "/v1/settings/bandwidth", header: js, json: `{"maxUploadBytesPerSec":-1,"maxDownloadBytesPerSec":0}`},
		step{name: "bandwidth-put-reset", as: actorOwner, method: put, path: "/v1/settings/bandwidth", header: js, json: `{"maxUploadBytesPerSec":0,"maxDownloadBytesPerSec":0}`},
		step{name: "bandwidth-get-guest", as: actorGuest, method: get, path: "/v1/settings/bandwidth"},

		// Apps and Wake-on-LAN (invalid input only: a valid address would broadcast on the LAN).
		step{name: "apps-owner", as: actorOwner, method: get, path: "/v1/apps"},
		step{name: "apps-guest", as: actorGuest, method: get, path: "/v1/apps"},
		step{name: "apps-anon", as: actorAnon, method: get, path: "/v1/apps"},
		step{name: "apps-launch-unknown", as: actorOwner, method: post, path: "/v1/apps/no-such-app/launch"},
		step{name: "apps-launch-guest", as: actorGuest, method: post, path: "/v1/apps/no-such-app/launch"},
		step{name: "wol-bad-mac", as: actorOwner, method: post, path: "/v1/wol", header: js, json: `{"mac":"not-a-mac"}`},
		step{name: "wol-empty-body", as: actorOwner, method: post, path: "/v1/wol", header: js, json: `{}`},
		step{name: "wol-guest", as: actorGuest, method: post, path: "/v1/wol", header: js, json: `{"mac":"not-a-mac"}`},

		// Pairing, registration and login.
		step{name: "auth-challenge", as: actorAnon, method: post, path: "/v1/auth/challenge", header: js, json: `{}`},
		step{name: "pair-no-proof", as: actorAnon, method: post, path: "/v1/pair", header: js, json: `{"pairingCode":"BAD-CODE"}`},
		step{name: "pair-bad-code", as: actorAnon, method: post, path: "/v1/pair", header: js, json: `{"pairingCode":"BAD-CODE"}`, proofFor: identityRequester},
		step{name: "register-bad-code", as: actorAnon, method: post, path: "/v1/register", header: js, json: `{"pairingCode":"BAD-CODE","username":"second-owner","password":"conformance-fake-pw-2"}`, proofFor: identityRequester},
		step{name: "login-wrong-password", as: actorAnon, method: post, path: "/v1/login", header: js, json: `{"username":"{{ownerUser}}","password":"wrong-password"}`, proofFor: identityLogger},
		step{name: "login-unknown-user", as: actorAnon, method: post, path: "/v1/login", header: js, json: `{"username":"nobody","password":"wrong-password"}`, proofFor: identityLogger},
		step{name: "login-no-proof", as: actorAnon, method: post, path: "/v1/login", header: js, json: `{"username":"{{ownerUser}}","password":"{{ownerPass}}"}`},
		step{name: "login-ok", as: actorAnon, method: post, path: "/v1/login", header: js, json: `{"username":"{{ownerUser}}","password":"{{ownerPass}}"}`, proofFor: identityLogger, tokenFor: actorLogged},
		step{name: "logged-devices", as: actorLogged, method: get, path: "/v1/devices", sortList: listKeyTop},
		step{name: "logged-upload-grant-bypass", as: actorLogged, method: post, path: "/v1/fs/folder", header: js, json: `{"path":"{{root}}/data/logged-made"}`},
		step{name: "logout", as: actorLogged, method: post, path: "/v1/auth/logout"},
		step{name: "logged-after-logout", as: actorLogged, method: get, path: "/v1/fs?path={{root}}/docs"},
		step{name: "pairing-generate-owner", as: actorOwner, method: post, path: "/v1/pairing/generate", header: js, json: `{}`},
		step{name: "pairing-generate-guest", as: actorGuest, method: post, path: "/v1/pairing/generate", header: js, json: `{}`},
		step{name: "pair-request-create", as: actorAnon, method: post, path: "/v1/pair/request", header: js, json: `{"clientNonceCommit":"{{nonceCommit}}"}`, proofFor: identityRequester, capture: map[string]string{"reqID": "requestId"}},
		step{name: "pair-request-bad-commit", as: actorAnon, method: post, path: "/v1/pair/request", header: js, json: `{"clientNonceCommit":"zz"}`, proofFor: identityRequester},
		step{name: "pair-request-poll-unrevealed", as: actorAnon, method: get, path: "/v1/pair/request/{{reqID}}?nonce={{clientNonce}}"},
		step{name: "pair-request-reveal-bad-nonce", as: actorAnon, method: post, path: "/v1/pair/request/{{reqID}}/reveal", header: js, json: `{"clientNonce":"short"}`},
		step{name: "pair-request-reveal", as: actorAnon, method: post, path: "/v1/pair/request/{{reqID}}/reveal", header: js, json: `{"clientNonce":"{{clientNonce}}"}`},
		step{name: "pair-request-poll-pending", as: actorAnon, method: get, path: "/v1/pair/request/{{reqID}}?nonce={{clientNonce}}"},
		step{name: "pair-request-poll-wrong-nonce", as: actorAnon, method: get, path: "/v1/pair/request/{{reqID}}?nonce=00000000000000000000000000000000"},
		step{name: "pair-requests-list-owner", as: actorOwner, method: get, path: "/v1/pair/requests"},
		step{name: "pair-requests-list-guest", as: actorGuest, method: get, path: "/v1/pair/requests"},
		step{name: "pair-request-approve-guest", as: actorGuest, method: post, path: "/v1/pair/requests/{{reqID}}/approve"},
		step{name: "pair-request-approve", as: actorOwner, method: post, path: "/v1/pair/requests/{{reqID}}/approve"},
		step{name: "pair-request-approve-again", as: actorOwner, method: post, path: "/v1/pair/requests/{{reqID}}/approve"},
		step{name: "pair-request-poll-approved", as: actorAnon, method: get, path: "/v1/pair/request/{{reqID}}?nonce={{clientNonce}}"},
		step{name: "pair-request-reject-missing", as: actorOwner, method: post, path: "/v1/pair/requests/{{missingID}}/reject"},

		// Devices, users, audit and logs.
		step{name: "devices-owner", as: actorOwner, method: get, path: "/v1/devices", sortList: listKeyTop},
		step{name: "devices-guest", as: actorGuest, method: get, path: "/v1/devices"},
		step{name: "devices-anon", as: actorAnon, method: get, path: "/v1/devices"},
		step{name: "device-patch-missing", as: actorOwner, method: patch, path: "/v1/devices/{{missingID}}", header: js, json: `{"browse":true}`},
		step{name: "device-patch-guest", as: actorGuest, method: patch, path: "/v1/devices/{{uploaderID}}", header: js, json: `{"upload":false}`},
		step{name: "device-patch-bad-body", as: actorOwner, method: patch, path: "/v1/devices/{{uploaderID}}", header: js, json: `{"upload":"yes"}`},
		step{name: "device-patch-jail-outside-roots", as: actorOwner, method: patch, path: "/v1/devices/{{uploaderID}}", header: js, json: `{"jailRoot":"{{outside}}"}`},
		step{name: "device-delete-missing", as: actorOwner, method: del, path: "/v1/devices/{{missingID}}"},
		step{name: "device-delete-other-device", as: actorGuest, method: del, path: "/v1/devices/{{uploaderID}}"},
		step{name: "users-owner", as: actorOwner, method: get, path: "/v1/users"},
		step{name: "users-guest", as: actorGuest, method: get, path: "/v1/users"},
		step{name: "users-delete-missing", as: actorOwner, method: del, path: "/v1/users/nobody"},
		step{name: "users-delete-guest", as: actorGuest, method: del, path: "/v1/users/nobody"},
		step{name: "audit-owner", as: actorOwner, method: get, path: "/v1/audit?limit=200"},
		step{name: "audit-limit1", as: actorOwner, method: get, path: "/v1/audit?limit=1"},
		step{name: "audit-bad-limit", as: actorOwner, method: get, path: "/v1/audit?limit=abc"},
		step{name: "audit-guest", as: actorGuest, method: get, path: "/v1/audit"},
		step{name: "logs-owner", as: actorOwner, method: get, path: "/v1/logs"},
		step{name: "logs-limit", as: actorOwner, method: get, path: "/v1/logs?limit=3"},
		step{name: "logs-guest", as: actorGuest, method: get, path: "/v1/logs"},

		// Revocation: works, the owner revokes it, then it is refused.
		step{name: "revoked-before-revoke", as: actorRevoked, method: get, path: "/v1/fs?path={{root}}/docs"},
		step{name: "revoke-device", as: actorOwner, method: del, path: "/v1/devices/{{revokedID}}"},
		step{name: "revoked-fs-list", as: actorRevoked, method: get, path: "/v1/fs?path={{root}}/docs"},
		step{name: "revoked-health", as: actorRevoked, method: get, path: "/v1/health"},
	)
	return cases
}

// caseNames lists the table's step names in order.
func caseNames() []string {
	var names []string
	for _, s := range buildCases() {
		names = append(names, s.name)
	}
	return names
}

// reportedAPIVersion reads apiVersion from an authenticated /v1/health reply.
func reportedAPIVersion(t *testing.T, a *agentUnderTest, token string) int {
	t.Helper()
	r, err := a.do(token, http.MethodGet, "/v1/health", nil, nil)
	if err != nil || r.status != http.StatusOK {
		t.Fatalf("health: %v (%+v)", err, r)
	}
	var h struct {
		APIVersion int `json:"apiVersion"`
	}
	if err := json.Unmarshal(r.body, &h); err != nil {
		t.Fatalf("health body: %v", err)
	}
	return h.APIVersion
}

// session is one running agent with its paired devices, the tokens the table
// sends and the variables captured so far. A differential run holds two.
type session struct {
	a       *agentUnderTest
	devices map[string]*testDevice
	tokens  map[string]string
	vars    map[string]string
	norm    *normalizer
	version int
}

// newSession starts the binary, registers the owner and pairs the other
// devices, so the table can start from a known state.
func newSession(t *testing.T, bin string) *session {
	t.Helper()
	a := startAgent(t, bin)
	s := &session{a: a, devices: map[string]*testDevice{}, vars: staticVars()}
	for _, d := range []struct{ actor, id, label string }{
		{actorOwner, "conf-owner-device", "owner phone"},
		{actorGuest, "conf-guest-device", "guest phone"},
		{actorUploader, "conf-uploader-device", "uploader phone"},
		{actorRevoked, "conf-revoked-device", "revoked phone"},
		{actorJailed, "conf-jailed-device", "jailed phone"},
		{identityLogger, "conf-logger-device", "logger phone"},
		{identityRequester, "conf-requester-device", "requester phone"},
	} {
		s.devices[d.actor] = newDevice(t, d.id, d.label)
	}
	a.registerOwner(t, s.devices[actorOwner])
	for _, actor := range []string{actorGuest, actorUploader, actorRevoked, actorJailed} {
		a.pairGuest(t, s.devices[actor])
	}
	s.version = reportedAPIVersion(t, a, s.devices[actorOwner].token)
	s.tokens = map[string]string{
		actorOwner: s.devices[actorOwner].token, actorGuest: s.devices[actorGuest].token,
		actorUploader: s.devices[actorUploader].token, actorRevoked: s.devices[actorRevoked].token,
		actorJailed: s.devices[actorJailed].token, actorAnon: "", actorBadToken: notAToken,
	}
	s.vars["root"] = a.root
	s.vars["outside"] = a.outside
	s.vars["uploaderID"] = s.devices[actorUploader].id
	s.vars["revokedID"] = s.devices[actorRevoked].id
	s.vars["jailedID"] = s.devices[actorJailed].id
	s.norm = newNormalizer(a)
	return s
}

// bodyWithProof returns body (a JSON object, or empty) with a fresh device
// proof of the named identity merged in.
func (s *session) bodyWithProof(t *testing.T, body []byte, identity string) []byte {
	t.Helper()
	d := s.devices[identity]
	if d == nil {
		t.Fatalf("unknown identity %q", identity)
	}
	m := map[string]any{}
	if len(body) > 0 {
		if err := json.Unmarshal(body, &m); err != nil {
			t.Fatalf("body for proof is not a JSON object: %v", err)
		}
	}
	for k, v := range s.a.proof(t, d) {
		m[k] = v
	}
	out, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// exec sends one table step and returns the normalized response. A capture
// that finds nothing is logged and leaves the variable at a marker, so the
// later steps still run and the difference shows up in their responses.
func (s *session) exec(t *testing.T, st step) result {
	t.Helper()
	token, ok := s.tokens[st.as]
	if !ok {
		t.Fatalf("unknown actor %q", st.as)
	}
	path, err := expand(st.path, s.vars, url.QueryEscape)
	if err != nil {
		t.Fatal(err)
	}
	header := map[string]string{}
	for k, v := range st.header {
		if header[k], err = expand(v, s.vars, func(x string) string { return x }); err != nil {
			t.Fatal(err)
		}
	}
	var body []byte
	switch {
	case st.json != "":
		j, err := expand(st.json, s.vars, jsonEscape)
		if err != nil {
			t.Fatal(err)
		}
		body = []byte(j)
	case st.raw != "":
		body = []byte(st.raw)
	}
	if st.proofFor != "" {
		body = s.bodyWithProof(t, body, st.proofFor)
	}
	resp, err := s.a.do(token, st.method, path, header, body)
	if err != nil {
		t.Fatalf("%s %s: %v", st.method, strings.SplitN(path, "?", 2)[0], err)
	}
	var decoded any
	if v, err := decodeJSON(resp.body); err == nil {
		decoded = v
	}
	for name, key := range st.capture {
		v, found := jsonPath(decoded, key)
		if !found {
			t.Logf("capture %q: no %q in status %d reply %.200q", name, key, resp.status, resp.body)
			s.vars[name] = uncaptured
			continue
		}
		s.vars[name] = fmt.Sprint(v)
	}
	if st.tokenFor != "" {
		if v, found := jsonPath(decoded, "deviceToken"); found {
			s.tokens[st.tokenFor] = fmt.Sprint(v)
		} else {
			t.Logf("tokenFor %q: no deviceToken in status %d reply %.200q", st.tokenFor, resp.status, resp.body)
		}
	}
	s.norm.relExpiry = st.relExpiry
	res := s.norm.normalize(resp)
	if st.sortList != "" {
		sortResultList(&res, st.sortList)
	}
	return res
}

// uncaptured stands in for a variable whose capture found nothing.
const uncaptured = "uncaptured-value"

// TestConformance starts the agent, pairs the devices the table needs, replays
// the request table and compares each normalized response with its golden file.
func TestConformance(t *testing.T) {
	pinUmask(t)
	s := newSession(t, agentBinary(t))
	v3 := s.version >= 3
	t.Logf("%s; apiVersion=%d (comparing against %s)", s.a.describe(), s.version,
		map[bool]string{false: "goldens", true: "goldens + expectedV3Diffs"}[v3])
	for _, st := range buildCases() {
		t.Run(st.name, func(t *testing.T) {
			checkGolden(t, st.name, st.sortList, s.exec(t, st), v3)
		})
	}
}
