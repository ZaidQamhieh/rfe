package server

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ZaidQamhieh/rfe/agent/internal/fsops"
	"github.com/ZaidQamhieh/rfe/agent/internal/thumbs"
)

func TestThumbHandlerRejectsOutsideFileSymlink(t *testing.T) {
	jail := t.TempDir()
	outside := t.TempDir()
	secret := filepath.Join(outside, "secret.png")
	if err := os.WriteFile(secret, []byte("outside-jail-content"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(jail, "escape.png")
	if err := os.Symlink(secret, link); err != nil {
		t.Skipf("cannot create symlink on this platform: %v", err)
	}
	renderer, err := thumbs.New(filepath.Join(t.TempDir(), "thumb-cache"))
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/v1/thumb?path="+url.QueryEscape(link), nil)
	rr := httptest.NewRecorder()
	thumbHandler(fsops.New([]string{jail}, false), renderer)(rr, req)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403; body: %s", rr.Code, rr.Body.String())
	}
	if strings.Contains(rr.Body.String(), "outside-jail-content") {
		t.Fatal("thumbnail response exposed outside file contents")
	}
}
