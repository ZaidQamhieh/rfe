package api_test

import (
	"bufio"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/ZaidQamhieh/rfe/agent/internal/next/api"
	"github.com/ZaidQamhieh/rfe/agent/internal/next/authz"
)

// responseEntryMarker starts the body of components.responses.Error, which
// shares its name with the Error schema whose enum the test reads.
const responseEntryMarker = "      content"

// Each case is one old agent mapping; src names the code line that sends it.
var errCases = []struct {
	name   string
	err    error
	status int
	code   string
	src    string
}{
	{"fs forbidden", api.ErrForbidden, http.StatusForbidden, "FORBIDDEN", "fshandlers.go:392"},
	{"fs not found", api.ErrPathNotFound, http.StatusNotFound, "PATH_NOT_FOUND", "fshandlers.go:394"},
	{"fs read only", authz.ErrReadOnly, http.StatusForbidden, "READ_ONLY", "fshandlers.go:396"},
	{"not a regular file", api.ErrNotAFile, http.StatusUnprocessableEntity, "NOT_A_FILE", "fshandlers.go:398"},
	{"is a directory / not a directory", api.ErrBadRequest, http.StatusBadRequest, "BAD_REQUEST", "fshandlers.go:400"},
	{"unsupported archive", api.ErrUnsupportedFormat, http.StatusBadRequest, "UNSUPPORTED_FORMAT", "fshandlers.go:402"},
	{"no atomic rename", api.ErrNotImplemented, http.StatusNotImplemented, "NOT_IMPLEMENTED", "fshandlers.go:409"},
	{"conflict", api.ErrConflict, http.StatusConflict, "CONFLICT", "fshandlers.go:411"},
	{"raw exists from a lost race", fs.ErrExist, http.StatusConflict, "CONFLICT", "fshandlers.go:413"},
	{"stale write", api.ErrStale, http.StatusConflict, "STALE_WRITE", "fshandlers.go:417"},
	{"transfer not found", api.ErrNotFound, http.StatusNotFound, "NOT_FOUND", "transferhandlers.go:300"},
	{"chunk hash mismatch", api.ErrChunkHashMismatch, http.StatusConflict, "CHUNK_HASH_MISMATCH", "transferhandlers.go:437"},
	{"whole-file hash mismatch", api.ErrHashMismatch, http.StatusUnprocessableEntity, "HASH_MISMATCH", "transferhandlers.go:514"},
	{"upload incomplete", api.ErrUploadIncomplete, http.StatusConflict, "UPLOAD_INCOMPLETE", "transferhandlers.go:510"},
	{"transfer not open", api.ErrTransferNotOpen, http.StatusConflict, "TRANSFER_NOT_OPEN", "transferhandlers.go:376"},
	{"transfer busy", api.ErrTransferActive, http.StatusConflict, "TRANSFER_ACTIVE", "transferhandlers.go:529"},
	{"upload quota", api.ErrResourceLimit, http.StatusTooManyRequests, "RESOURCE_LIMIT", "transferhandlers.go:261"},
	{"declared size too large", api.ErrPayloadTooLarge, http.StatusRequestEntityTooLarge, "PAYLOAD_TOO_LARGE", "transferhandlers.go:257; compat.md C7"},
	{"rate limited", api.ErrRateLimited, http.StatusTooManyRequests, "RATE_LIMITED", "server.go (10 sites)"},
	{"bad nonce", api.ErrInvalidNonce, http.StatusUnauthorized, "INVALID_NONCE", "challenge.go"},
	{"bad pairing code", api.ErrInvalidCode, http.StatusUnauthorized, "INVALID_CODE", "pair.go"},
	{"no thumbnail", api.ErrNotAvailable, http.StatusNotFound, "NOT_AVAILABLE", "thumb.go"},
	{"thumbnail capacity", api.ErrThumbBusy, http.StatusTooManyRequests, "THUMB_BUSY", "thumb.go"},
	{"chunk capacity", api.ErrChunkBusy, http.StatusTooManyRequests, "CHUNK_BUSY", "transferhandlers.go"},
	{"recent scan capacity", api.ErrRecentBusy, http.StatusTooManyRequests, "RECENT_BUSY", "recent.go"},
	{"storage scan capacity", api.ErrUsageBusy, http.StatusTooManyRequests, "USAGE_BUSY", "usage.go"},
	{"authz unauthorized", authz.ErrUnauthorized, http.StatusUnauthorized, "UNAUTHORIZED", "auth.go:100"},
	{"authz forbidden", authz.ErrForbidden, http.StatusForbidden, "FORBIDDEN", "settings_handlers.go:38"},
	{"authz capability", authz.ErrCapabilityDenied, http.StatusForbidden, "CAPABILITY_DENIED", "auth.go:186"},
}

func TestMapMatchesTheOldAgentsStatusAndCode(t *testing.T) {
	for _, c := range errCases {
		got := api.Map(c.err)
		if got.Status != c.status || got.Code != c.code {
			t.Errorf("%s (%s): got %d %s, want %d %s", c.name, c.src, got.Status, got.Code, c.status, c.code)
		}
		if got.Internal {
			t.Errorf("%s: a known domain error must not be reported as internal", c.name)
		}
	}
}

func TestMapSeesThroughWrapping(t *testing.T) {
	wrapped := fmt.Errorf("copy %q: %w", "a.txt", api.ErrConflict)
	if got := api.Map(wrapped); got.Code != "CONFLICT" || got.Status != http.StatusConflict {
		t.Fatalf("wrapped conflict: %+v", got)
	}
	d := authz.Decide(authz.Principal{DeviceID: "d"}, authz.ActionBrowse, authz.Target{}, authz.Policy{})
	got := api.Map(d.Err)
	if got.Code != authz.CodeCapabilityDenied || got.Message != "device lacks browse permission" {
		t.Fatalf("authz denial: %+v", got)
	}
}

// An unexpected error is a 500 whose text never reaches the client
// (server.go:285-288): absolute paths and driver messages stay in the log.
func TestUnexpectedErrorBecomesInternalWithoutLeakingDetail(t *testing.T) {
	secret := "open /home/someone/private/file: permission denied"
	got := api.Map(errors.New(secret))
	if got.Status != http.StatusInternalServerError || got.Code != "INTERNAL" || !got.Internal {
		t.Fatalf("got %+v", got)
	}
	if strings.Contains(got.Message, "private") || got.Message != api.InternalMessage {
		t.Fatalf("message leaks detail or is not the fixed text: %q", got.Message)
	}
	if got := api.Map(nil); got.Status != http.StatusOK || got.Code != "" {
		t.Fatalf("nil error: %+v", got)
	}
}

func TestBatchItemUsesTheSameMap(t *testing.T) {
	item := api.Item("/a", api.ErrPathNotFound)
	if item.OK || item.Path != "/a" || item.Error == nil || item.Error.Code != "PATH_NOT_FOUND" {
		t.Fatalf("failed item: %+v", item)
	}
	if ok := api.Item("/b", nil); !ok.OK || ok.Error != nil {
		t.Fatalf("ok item: %+v", ok)
	}
}

// specErrorCodes reads the enum of the Error schema in protocol/openapi.yaml.
func specErrorCodes(t *testing.T) map[string]bool {
	t.Helper()
	f, err := os.Open(specPath)
	if err != nil {
		t.Fatalf("open spec: %v", err)
	}
	defer f.Close()
	schemaStart := regexp.MustCompile(`^    Error:\s*$`)
	enumLine := regexp.MustCompile(`^\s+enum:\s*$`)
	item := regexp.MustCompile(`^\s+-\s+([A-Z_]+)\s*$`)
	codes := map[string]bool{}
	inSchema, inEnum := false, false
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		switch {
		case !inSchema:
			inSchema = schemaStart.MatchString(line)
		case !inEnum:
			if enumLine.MatchString(line) {
				inEnum = true
			} else if strings.HasPrefix(line, responseEntryMarker) {
				inSchema = false // this is the responses.Error entry, not the schema
			}
		default:
			if m := item.FindStringSubmatch(line); m != nil {
				codes[m[1]] = true
			} else {
				return codes
			}
		}
	}
	if err := sc.Err(); err != nil {
		t.Fatalf("read spec: %v", err)
	}
	return codes
}

func TestEveryEmittedCodeIsInTheOpenAPIEnum(t *testing.T) {
	enum := specErrorCodes(t)
	if len(enum) < 20 {
		t.Fatalf("parsed only %d codes from the Error enum; the parser needs updating", len(enum))
	}
	for _, code := range api.AllCodes() {
		if !enum[code] {
			t.Errorf("code %q is not in the Error enum of protocol/openapi.yaml", code)
		}
	}
	for _, c := range errCases {
		if !enum[c.code] {
			t.Errorf("%s: code %q not in the spec enum", c.name, c.code)
		}
	}
}
