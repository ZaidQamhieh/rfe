package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"syscall"
	"testing"

	"github.com/ZaidQamhieh/rfe/agent/internal/fsops"
)

// A file system with no atomic no-replace rename and no hard links makes fsops
// refuse with fsops.ErrNoAtomicRename. It must reach the client as 501
// NOT_IMPLEMENTED with fixed text, never as 500 and never with the path.
func TestPass3HandleFsError_UnsupportedRename(t *testing.T) {
	const secret = "/srv/secret-root/dir/file.txt"
	wrapped := fmt.Errorf("rename %s: %w: %w", secret, fsops.ErrNoAtomicRename, errors.New("operation not permitted"))
	for name, err := range map[string]error{
		"sentinel": fsops.ErrNoAtomicRename,
		"wrapped":  wrapped,
	} {
		t.Run(name, func(t *testing.T) {
			if !errors.Is(err, errors.ErrUnsupported) {
				t.Fatalf("sentinel must still match errors.ErrUnsupported")
			}
			rr := httptest.NewRecorder()
			handleFsError(rr, err)
			if rr.Code != http.StatusNotImplemented {
				t.Fatalf("status = %d, want 501: %s", rr.Code, rr.Body.String())
			}
			var got apiError
			if e := json.Unmarshal(rr.Body.Bytes(), &got); e != nil {
				t.Fatalf("decode: %v", e)
			}
			if got.Code != "NOT_IMPLEMENTED" {
				t.Fatalf("code = %q, want NOT_IMPLEMENTED", got.Code)
			}
			if got.Message == "" {
				t.Fatalf("message is empty")
			}
			body := rr.Body.String()
			if strings.Contains(body, "/srv") || strings.Contains(body, secret) || strings.Contains(body, "/") {
				t.Fatalf("body leaks a path: %s", body)
			}
		})
	}
}

// A raw ENOTSUP/EOPNOTSUPP/ENOSYS from some other operation also matches
// errors.ErrUnsupported, but it is not "cannot rename without replacing": it
// stays an unmapped internal error (500, no OS detail).
func TestPass3HandleFsError_RawUnsupportedErrnoIsNot501(t *testing.T) {
	const secret = "/srv/secret-root/dir/file.txt"
	for name, err := range map[string]error{
		"ENOTSUP":             syscall.ENOTSUP,
		"EOPNOTSUPP":          syscall.EOPNOTSUPP,
		"ENOSYS":              syscall.ENOSYS,
		"wrapped ENOTSUP":     fmt.Errorf("setxattr %s: %w", secret, syscall.ENOTSUP),
		"bare ErrUnsupported": errors.ErrUnsupported,
	} {
		t.Run(name, func(t *testing.T) {
			rr := httptest.NewRecorder()
			handleFsError(rr, err)
			if rr.Code == http.StatusNotImplemented {
				t.Fatalf("status = 501, want it left unmapped: %s", rr.Body.String())
			}
			if rr.Code != http.StatusInternalServerError {
				t.Fatalf("status = %d, want 500: %s", rr.Code, rr.Body.String())
			}
			var got apiError
			if e := json.Unmarshal(rr.Body.Bytes(), &got); e != nil {
				t.Fatalf("decode: %v", e)
			}
			if got.Code != "INTERNAL" {
				t.Fatalf("code = %q, want INTERNAL", got.Code)
			}
			if strings.Contains(rr.Body.String(), secret) {
				t.Fatalf("body leaks a path: %s", rr.Body.String())
			}
		})
	}
}

// The archive sentinel is a different error and keeps its own code.
func TestPass3HandleFsError_ArchiveUnsupportedStaysFormat(t *testing.T) {
	for _, err := range []error{
		fsops.ErrUnsupported,
		fmt.Errorf("%w: archive.rar", fsops.ErrUnsupported),
	} {
		rr := httptest.NewRecorder()
		handleFsError(rr, err)
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400: %s", rr.Code, rr.Body.String())
		}
		var got apiError
		if e := json.Unmarshal(rr.Body.Bytes(), &got); e != nil {
			t.Fatalf("decode: %v", e)
		}
		if got.Code != "UNSUPPORTED_FORMAT" {
			t.Fatalf("code = %q, want UNSUPPORTED_FORMAT", got.Code)
		}
	}
}
