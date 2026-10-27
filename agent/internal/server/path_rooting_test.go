package server

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ZaidQamhieh/rfe/agent/internal/fsops"
)

// These tests pin the server boundary as well as the lower-level fsops
// behavior: handler reads and recursive scans must not follow a jail entry
// outside the configured root.
func TestPathRootedHandlersRejectOutsideFileSymlink(t *testing.T) {
	jail := t.TempDir()
	outside := t.TempDir()
	secret := filepath.Join(outside, "secret.zip")
	const secretContent = "outside-jail-secret"
	if err := os.WriteFile(secret, []byte(secretContent), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(jail, "escape.zip")
	if err := os.Symlink(secret, link); err != nil {
		t.Skipf("cannot create symlink on this platform: %v", err)
	}
	ops := fsops.New([]string{jail}, false)

	t.Run("download", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/v1/content?path="+url.QueryEscape(link), nil)
		rr := httptest.NewRecorder()
		downloadHandler(ops)(rr, req)
		if rr.Code != http.StatusForbidden {
			t.Fatalf("status = %d, want 403; body: %s", rr.Code, rr.Body.String())
		}
		if strings.Contains(rr.Body.String(), secretContent) {
			t.Fatal("download response exposed outside file contents")
		}
	})

	t.Run("archive peek", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/v1/fs/archive?path="+url.QueryEscape(link), nil)
		rr := httptest.NewRecorder()
		archivePeekHandler(ops)(rr, req)
		if rr.Code != http.StatusForbidden {
			t.Fatalf("status = %d, want 403; body: %s", rr.Code, rr.Body.String())
		}
		if strings.Contains(rr.Body.String(), secretContent) {
			t.Fatal("archive response exposed outside file contents")
		}
	})

	t.Run("public share", func(t *testing.T) {
		db, _ := newTestDeps(t)
		if err := db.CreateShareToken(hashShareToken("rooted-path-test"), link, "", time.Now().Add(time.Hour)); err != nil {
			t.Fatal(err)
		}
		req := httptest.NewRequest(http.MethodGet, "/v1/share/rooted-path-test", nil)
		req = withURLParam(req, map[string]string{"token": "rooted-path-test"})
		rr := httptest.NewRecorder()
		serveShareHandler(db, ops, nil)(rr, req)
		if rr.Code != http.StatusNotFound {
			t.Fatalf("status = %d, want 404; body: %s", rr.Code, rr.Body.String())
		}
		if strings.Contains(rr.Body.String(), secretContent) {
			t.Fatal("share response exposed outside file contents")
		}
	})
}

func TestPathRootedSearchAndRecentDoNotWalkOutsideDirectorySymlink(t *testing.T) {
	jail := t.TempDir()
	outside := t.TempDir()
	const outsideName = "private-needle-secret.txt"
	if err := os.WriteFile(filepath.Join(outside, outsideName), []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(filepath.Join(outside, outsideName), time.Now(), time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(jail, "linked-outside")); err != nil {
		t.Skipf("cannot create directory symlink on this platform: %v", err)
	}
	ops := fsops.New([]string{jail}, false)

	t.Run("search", func(t *testing.T) {
		rr, entries := doSearch(t, ops, "q=needle-secret")
		if rr.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200; body: %s", rr.Code, rr.Body.String())
		}
		if containsName(names(entries), outsideName) {
			t.Fatalf("search followed an outside directory symlink: %v", names(entries))
		}
	})

	t.Run("recent", func(t *testing.T) {
		rr, entries := doRecent(t, ops, "limit=100")
		if rr.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200; body: %s", rr.Code, rr.Body.String())
		}
		if containsName(names(entries), outsideName) {
			t.Fatalf("recent followed an outside directory symlink: %v", names(entries))
		}
	})
}
