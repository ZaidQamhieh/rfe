package api

import (
	"errors"
	"io/fs"
	"net/http"
	"sort"

	"github.com/ZaidQamhieh/rfe/agent/internal/next/apps"
	"github.com/ZaidQamhieh/rfe/agent/internal/next/authz"
	"github.com/ZaidQamhieh/rfe/agent/internal/next/pairing"
)

// Error codes of the openapi Error enum (protocol/openapi.yaml, schema Error).
// The four authorization codes live in authz.
const (
	CodeBadRequest       = "BAD_REQUEST"
	CodePathNotFound     = "PATH_NOT_FOUND"
	CodeNotFound         = "NOT_FOUND"
	CodeNotAFile         = "NOT_A_FILE"
	CodeConflict         = "CONFLICT"
	CodeStaleWrite       = "STALE_WRITE"
	CodeUnsupported      = "UNSUPPORTED_FORMAT"
	CodeNotImplemented   = "NOT_IMPLEMENTED"
	CodeChunkHash        = "CHUNK_HASH_MISMATCH"
	CodeHashMismatch     = "HASH_MISMATCH"
	CodeUploadIncomplete = "UPLOAD_INCOMPLETE"
	CodeTransferNotOpen  = "TRANSFER_NOT_OPEN"
	CodeTransferActive   = "TRANSFER_ACTIVE"
	CodeResourceLimit    = "RESOURCE_LIMIT"
	CodePayloadTooLarge  = "PAYLOAD_TOO_LARGE"
	CodeRateLimited      = "RATE_LIMITED"
	CodeInvalidNonce     = "INVALID_NONCE"
	CodeInvalidCode      = "INVALID_CODE"
	CodeNotAvailable     = "NOT_AVAILABLE"
	CodeThumbBusy        = "THUMB_BUSY"
	CodeChunkBusy        = "CHUNK_BUSY"
	CodeRecentBusy       = "RECENT_BUSY"
	CodeUsageBusy        = "USAGE_BUSY"
	CodeInternal         = "INTERNAL"
)

// InternalMessage is the only text a client sees for an unexpected error
// (server.go:285-288); the real error is logged by the caller.
const InternalMessage = "internal error"

// messageDestinationExists is the fixed text for a raw "already exists" error,
// whose own text carries paths (fshandlers.go:413-416).
const messageDestinationExists = "destination already exists"

// Domain errors. Services return these (wrapped with %w for context); Map turns
// them into a response. Their text is safe to show a client.
var (
	ErrForbidden         = authz.ErrForbidden // path outside the roots or jail: fsops.ErrForbidden, fshandlers.go:392
	ErrBadRequest        = errors.New("bad request")
	ErrPathNotFound      = errors.New("path not found")
	ErrNotFound          = errors.New("not found")
	ErrNotAFile          = errors.New("not a regular file")
	ErrConflict          = errors.New(messageDestinationExists)
	ErrStale             = errors.New("file changed since it was read")
	ErrUnsupportedFormat = errors.New("unsupported format")
	ErrNotImplemented    = errors.New("not implemented on this file system")
	ErrChunkHashMismatch = errors.New("chunk sha256 mismatch")
	ErrHashMismatch      = errors.New("whole-file sha256 mismatch")
	ErrUploadIncomplete  = errors.New("upload is missing chunks")
	ErrTransferNotOpen   = errors.New("transfer is no longer open")
	ErrTransferActive    = errors.New("transfer has another active operation; retry after it finishes")
	ErrResourceLimit     = errors.New("upload capacity is full")
	ErrPayloadTooLarge   = errors.New("payload too large")
	ErrRateLimited       = errors.New("too many requests")
	ErrInvalidNonce      = errors.New("nonce is missing, expired or already used")
	ErrInvalidCode       = errors.New("pairing code invalid or expired")
	ErrNotAvailable      = errors.New("not available")
	ErrThumbBusy         = errors.New("thumbnail capacity is full")
	ErrChunkBusy         = errors.New("chunk capacity is full")
	ErrRecentBusy        = errors.New("recent-file scans are busy")
	ErrUsageBusy         = errors.New("storage scans are busy")
)

// Mapped is the HTTP answer for an error.
type Mapped struct {
	Status  int
	Code    string
	Message string
	// Internal is true for an unexpected error: the caller must log the original
	// error, and Message is the fixed InternalMessage.
	Internal bool
}

// mapping is one row of the error map. message, when set, replaces the error's
// own text.
type mapping struct {
	kind    error
	status  int
	code    string
	message string
}

// errorMap is checked top to bottom with errors.Is; the status and code of each
// row are the old agent's, with the sending line in the comment.
var errorMap = []mapping{
	{authz.ErrUnauthorized, http.StatusUnauthorized, authz.CodeUnauthorized, ""},      // auth.go:81,101
	{authz.ErrCapabilityDenied, http.StatusForbidden, authz.CodeCapabilityDenied, ""}, // auth.go:186
	{authz.ErrReadOnly, http.StatusForbidden, authz.CodeReadOnly, ""},                 // fshandlers.go:396, server.go:257
	{authz.ErrForbidden, http.StatusForbidden, authz.CodeForbidden, ""},               // fshandlers.go:392, settings_handlers.go:38
	{ErrBadRequest, http.StatusBadRequest, CodeBadRequest, ""},                        // fshandlers.go:400, transferhandlers.go:442
	{ErrPathNotFound, http.StatusNotFound, CodePathNotFound, ""},                      // fshandlers.go:394
	{ErrNotFound, http.StatusNotFound, CodeNotFound, ""},                              // transferhandlers.go:300
	{ErrNotAFile, http.StatusUnprocessableEntity, CodeNotAFile, ""},                   // fshandlers.go:398; compat.md C6
	{ErrConflict, http.StatusConflict, CodeConflict, ""},                              // fshandlers.go:411
	{fs.ErrExist, http.StatusConflict, CodeConflict, messageDestinationExists},        // fshandlers.go:413
	{ErrStale, http.StatusConflict, CodeStaleWrite, ""},                               // fshandlers.go:417
	{ErrUnsupportedFormat, http.StatusBadRequest, CodeUnsupported, ""},                // fshandlers.go:402
	{ErrNotImplemented, http.StatusNotImplemented, CodeNotImplemented, ""},            // fshandlers.go:409
	{ErrChunkHashMismatch, http.StatusConflict, CodeChunkHash, ""},                    // transferhandlers.go:437
	{ErrHashMismatch, http.StatusUnprocessableEntity, CodeHashMismatch, ""},           // transferhandlers.go:514
	{ErrUploadIncomplete, http.StatusConflict, CodeUploadIncomplete, ""},              // transferhandlers.go:510
	{ErrTransferNotOpen, http.StatusConflict, CodeTransferNotOpen, ""},                // transferhandlers.go:376
	{ErrTransferActive, http.StatusConflict, CodeTransferActive, ""},                  // transferhandlers.go:529
	{ErrResourceLimit, http.StatusTooManyRequests, CodeResourceLimit, ""},             // transferhandlers.go:261
	{ErrPayloadTooLarge, http.StatusRequestEntityTooLarge, CodePayloadTooLarge, ""},   // transferhandlers.go:257; compat.md C7
	{ErrRateLimited, http.StatusTooManyRequests, CodeRateLimited, ""},                 // pair.go, wol_handler.go
	{ErrInvalidNonce, http.StatusUnauthorized, CodeInvalidNonce, ""},                  // challenge.go
	{ErrInvalidCode, http.StatusUnauthorized, CodeInvalidCode, ""},                    // pair.go
	{ErrNotAvailable, http.StatusNotFound, CodeNotAvailable, ""},                      // thumb.go
	{ErrThumbBusy, http.StatusTooManyRequests, CodeThumbBusy, ""},                     // thumb.go
	{ErrChunkBusy, http.StatusTooManyRequests, CodeChunkBusy, ""},                     // transferhandlers.go
	{ErrRecentBusy, http.StatusTooManyRequests, CodeRecentBusy, ""},                   // recent.go
	{ErrUsageBusy, http.StatusTooManyRequests, CodeUsageBusy, ""},                     // usage.go
}

// Map turns err into the status and code a client gets. Anything not in
// errorMap is an internal error: 500 INTERNAL with a fixed message, so paths
// and driver text never leave the agent. A nil error maps to 200 with no code.
func Map(err error) Mapped {
	if err == nil {
		return Mapped{Status: http.StatusOK}
	}
	if m, ok := typedError(err); ok {
		return m
	}
	for _, m := range errorMap {
		if errors.Is(err, m.kind) {
			msg := m.message
			if msg == "" {
				msg = err.Error()
			}
			return Mapped{Status: m.status, Code: m.code, Message: msg}
		}
	}
	return Mapped{Status: http.StatusInternalServerError, Code: CodeInternal, Message: InternalMessage, Internal: true}
}

// typedError answers for a service error that carries its own code and HTTP
// status (*pairing.Error, *apps.Error). Their texts are fixed and hold no host
// data. An error with an empty code or a status outside 400-599 is not trusted
// and falls through to the table and then to INTERNAL.
func typedError(err error) (Mapped, bool) {
	var code, msg string
	var status int
	var pe *pairing.Error
	var ae *apps.Error
	switch {
	case errors.As(err, &pe):
		code, status, msg = pe.Code, pe.Status, pe.Message
	case errors.As(err, &ae):
		code, status, msg = ae.Code, ae.Status, ae.Message
	default:
		return Mapped{}, false
	}
	if code == "" || status < http.StatusBadRequest || status > maxHTTPStatus {
		return Mapped{}, false
	}
	return Mapped{Status: status, Code: code, Message: msg}, true
}

// maxHTTPStatus is the highest status a typed error may carry.
const maxHTTPStatus = 599

// TypedCodes lists the codes the typed service errors can carry: every sentinel
// the pairing and apps services export. Several are not in the openapi Error
// enum yet (see the change report); AllCodes leaves them out until they are.
func TypedCodes() []string {
	var out []string
	for _, e := range []*pairing.Error{
		pairing.ErrRateLimited, pairing.ErrInvalidCode, pairing.ErrCodeCannotRegister, pairing.ErrAccountExists,
		pairing.ErrUsernameTaken, pairing.ErrInvalidCredentials, pairing.ErrDeviceKeyRequired,
		pairing.ErrInvalidNonce, pairing.ErrInvalidSignature, pairing.ErrDeviceKeyMismatch, pairing.ErrPairBusy,
		pairing.ErrNotFound, pairing.ErrForbidden, pairing.ErrUnauthorized, pairing.ErrDeviceNotFound,
		pairing.ErrDeviceRevoked,
	} {
		out = append(out, e.Code)
	}
	for _, e := range []*apps.Error{
		apps.ErrCatalogRateLimited, apps.ErrLaunchRateLimited, apps.ErrCatalogUnsupported,
		apps.ErrCatalogUnavailable, apps.ErrBadAppID, apps.ErrAppNotFound, apps.ErrAppNotLaunchable,
		apps.ErrLaunchBusy, apps.ErrNoInteractiveUser, apps.ErrLauncherUnavailable, apps.ErrLaunchFailed,
	} {
		out = append(out, e.Code)
	}
	return out
}

// AllCodes lists every code Map can produce, sorted. A test checks each one is
// in the openapi enum.
func AllCodes() []string {
	seen := map[string]bool{CodeInternal: true}
	for _, m := range errorMap {
		seen[m.code] = true
	}
	for _, c := range TypedCodes() {
		seen[c] = true
	}
	out := make([]string, 0, len(seen))
	for c := range seen {
		out = append(out, c)
	}
	sort.Strings(out)
	return out
}

// ErrorBody is the JSON error envelope (openapi schema Error).
type ErrorBody struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// ItemResult is one entry of a batch answer (openapi BatchResult).
type ItemResult struct {
	Path  string     `json:"path"`
	OK    bool       `json:"ok"`
	Error *ErrorBody `json:"error,omitempty"`
}

// Item builds a batch entry from err with the same map as a whole-request error,
// so a failed item and a failed request carry the same code.
func Item(path string, err error) ItemResult {
	if err == nil {
		return ItemResult{Path: path, OK: true}
	}
	m := Map(err)
	return ItemResult{Path: path, Error: &ErrorBody{Code: m.Code, Message: m.Message}}
}
