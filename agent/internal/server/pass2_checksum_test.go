package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"

	"github.com/ZaidQamhieh/rfe/agent/internal/fsops"
)

func getChecksum(ops *fsops.Ops, path, algo string) *httptest.ResponseRecorder {
	q := url.Values{}
	q.Set("path", path)
	if algo != "" {
		q.Set("algo", algo)
	}
	req := httptest.NewRequest(http.MethodGet, "/v1/fs/checksum?"+q.Encode(), nil)
	rr := httptest.NewRecorder()
	checksumHandler(ops)(rr, req)
	return rr
}

// P2-6: unsupported algorithm and a directory are 400 BAD_REQUEST, a file is 200.
func TestPass2ChecksumHandlerStatuses(t *testing.T) {
	root := t.TempDir()
	ops := fsops.New([]string{root}, false)
	file := filepath.Join(root, "f.txt")
	if err := os.WriteFile(file, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name, path, algo string
		want             int
	}{
		{"file default algo", file, "", http.StatusOK},
		{"file md5", file, "md5", http.StatusOK},
		{"unknown algo", file, "foo", http.StatusBadRequest},
		{"directory", root, "sha256", http.StatusBadRequest},
		{"missing file", filepath.Join(root, "nope"), "sha256", http.StatusNotFound},
	}
	for _, c := range cases {
		rr := getChecksum(ops, c.path, c.algo)
		if rr.Code != c.want {
			t.Errorf("%s: status = %d, want %d (body %s)", c.name, rr.Code, c.want, rr.Body.String())
			continue
		}
		if c.want == http.StatusBadRequest {
			var e struct{ Code string }
			if err := json.Unmarshal(rr.Body.Bytes(), &e); err != nil || e.Code != "BAD_REQUEST" {
				t.Errorf("%s: code = %q (%v), want BAD_REQUEST", c.name, e.Code, err)
			}
		}
	}
}
