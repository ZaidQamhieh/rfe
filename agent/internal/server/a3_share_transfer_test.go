package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ZaidQamhieh/rfe/agent/internal/fsops"
)

// writeSparseFile creates a file of the given size without writing its bytes.
func writeSparseFile(t *testing.T, path string, size int64) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create %s: %v", path, err)
	}
	defer f.Close()
	if err := f.Truncate(size); err != nil {
		t.Skipf("filesystem cannot make a %d-byte sparse file: %v", size, err)
	}
}

func decodeErrorCode(t *testing.T, rr *httptest.ResponseRecorder) string {
	t.Helper()
	var got apiError
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode error body %q: %v", rr.Body.String(), err)
	}
	return got.Code
}

// A file over the share size cap is refused at mint time with the contract's
// 413 PAYLOAD_TOO_LARGE (not a share-specific code).
func TestMintShare_OversizeFileIsPayloadTooLarge(t *testing.T) {
	ops, root := newFsFixture(t)
	db, st := newTestDeps(t)
	if err := st.SetAllowSharing(true); err != nil {
		t.Fatalf("enable sharing: %v", err)
	}
	big := filepath.Join(root, "big.bin")
	writeSparseFile(t, big, shareMaxFileSize+1)

	rr := httptest.NewRecorder()
	mintShareHandler(Config{Address: "127.0.0.1:8765", Settings: st}, db, ops)(rr,
		httptest.NewRequest(http.MethodPost, "/v1/share/mint", strings.NewReader(`{"path":"`+big+`"}`)))
	if rr.Code != http.StatusRequestEntityTooLarge || decodeErrorCode(t, rr) != "PAYLOAD_TOO_LARGE" {
		t.Fatalf("want 413 PAYLOAD_TOO_LARGE, got %d: %s", rr.Code, rr.Body.String())
	}
}

// A file that grew past the cap after minting is refused at serve time with
// the same 413 PAYLOAD_TOO_LARGE.
func TestServeShare_OversizeFileIsPayloadTooLarge(t *testing.T) {
	ops, root := newFsFixture(t)
	db, _ := newTestDeps(t)
	big := filepath.Join(root, "big.bin")
	writeSparseFile(t, big, shareMaxFileSize+1)
	if err := db.CreateShareToken(hashShareToken("big-token"), big, "", time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("create token: %v", err)
	}

	rr := httptest.NewRecorder()
	serveShareHandler(db, ops, nil)(rr,
		withURLParam(httptest.NewRequest(http.MethodGet, "/v1/share/big-token", nil), map[string]string{"token": "big-token"}))
	if rr.Code != http.StatusRequestEntityTooLarge || decodeErrorCode(t, rr) != "PAYLOAD_TOO_LARGE" {
		t.Fatalf("want 413 PAYLOAD_TOO_LARGE, got %d: %s", rr.Code, rr.Body.String())
	}
}

// A link whose file lies outside the minting device's current jail is refused
// without burning the one-time token, so it works again if the jail is widened.
func TestServeShare_OutOfJailRefusalKeepsTheLink(t *testing.T) {
	ops, root := newFsFixture(t)
	db, _ := newTestDeps(t)
	if err := db.CreateDevice("jailed", "phone", "jailed-token"); err != nil {
		t.Fatalf("create device: %v", err)
	}
	if err := db.SetDeviceFilePermissions("jailed", true, true, true, true, true, true); err != nil {
		t.Fatalf("grants: %v", err)
	}
	if err := db.SetDeviceJail("jailed", filepath.Join(root, "sub")); err != nil {
		t.Fatalf("jail: %v", err)
	}
	outside := filepath.Join(root, "a.txt") // in the global root, outside the device jail
	if err := db.CreateShareToken(hashShareToken("jail-token"), outside, "jailed", time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("create token: %v", err)
	}
	get := func() *httptest.ResponseRecorder {
		rr := httptest.NewRecorder()
		serveShareHandler(db, ops, nil)(rr,
			withURLParam(httptest.NewRequest(http.MethodGet, "/v1/share/jail-token", nil), map[string]string{"token": "jail-token"}))
		return rr
	}

	if rr := get(); rr.Code != http.StatusNotFound {
		t.Fatalf("out-of-jail fetch: want 404, got %d: %s", rr.Code, rr.Body.String())
	}
	tok, err := db.GetShareToken(hashShareToken("jail-token"))
	if err != nil || tok == nil {
		t.Fatalf("the refused link must stay valid, got token=%v err=%v", tok, err)
	}
	if err := db.SetDeviceJail("jailed", ""); err != nil {
		t.Fatalf("clear jail: %v", err)
	}
	if rr := get(); rr.Code != http.StatusOK {
		t.Fatalf("after the jail is lifted: want 200, got %d: %s", rr.Code, rr.Body.String())
	}
}

// a3Upload runs open, one chunk and complete through the handlers and returns the
// complete response. shaOpen and shaChunk are sent verbatim so a test can use any case.
func a3Upload(t *testing.T, target string, content []byte, shaOpen, shaChunk string, overwrite bool) *httptest.ResponseRecorder {
	t.Helper()
	tm, _ := newTestTransferManager(t)
	ops := fsops.New(nil, false)

	body, _ := json.Marshal(map[string]any{
		"path": target, "size": len(content), "sha256": shaOpen, "chunkSize": 1024, "overwrite": overwrite,
	})
	rr := httptest.NewRecorder()
	openTransferHandler(tm, ops)(rr, asAdmin(httptest.NewRequest(http.MethodPost, "/v1/transfers", bytes.NewReader(body))))
	if rr.Code != http.StatusCreated {
		t.Fatalf("open: want 201, got %d: %s", rr.Code, rr.Body.String())
	}
	var sess struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &sess); err != nil || sess.ID == "" {
		t.Fatalf("open response %q: %v", rr.Body.String(), err)
	}

	rr = httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/v1/transfers/"+sess.ID+"/chunks/0", bytes.NewReader(content))
	req.Header.Set("X-Chunk-Sha256", shaChunk)
	uploadChunkHandler(tm)(rr, asAdmin(withURLParam(req, map[string]string{"id": sess.ID, "n": "0"})))
	if rr.Code != http.StatusNoContent {
		t.Fatalf("chunk: want 204, got %d: %s", rr.Code, rr.Body.String())
	}

	rr = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/v1/transfers/"+sess.ID+"/complete", nil)
	completeTransferHandler(tm, ops)(rr, asAdmin(withURLParam(req, map[string]string{"id": sess.ID})))
	return rr
}

// SHA-256 is hex, and hex has no case: an upload that declares the whole-file
// hash and the chunk hash in upper case must verify like a lower-case one.
func TestUploadAcceptsUppercaseHexHashes(t *testing.T) {
	content := []byte("upper-case hex hashes are still the same hash")
	upper := strings.ToUpper(sha256hex(content))
	target := filepath.Join(t.TempDir(), "out.bin")

	rr := a3Upload(t, target, content, upper, upper, false)
	if rr.Code != http.StatusOK {
		t.Fatalf("complete: want 200, got %d: %s", rr.Code, rr.Body.String())
	}
	got, err := os.ReadFile(target)
	if err != nil || !bytes.Equal(got, content) {
		t.Fatalf("published file = %q, err %v", got, err)
	}
}

// An overwrite upload aimed at an existing folder cannot replace it: it is a
// 409 CONFLICT (the destination is occupied), not a 500.
func TestCompleteOverwriteOntoDirectoryIsConflict(t *testing.T) {
	content := []byte("bytes for a folder")
	target := t.TempDir() // an existing directory
	if err := os.WriteFile(filepath.Join(target, "keep.txt"), []byte("x"), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}

	rr := a3Upload(t, target, content, sha256hex(content), sha256hex(content), true)
	if rr.Code != http.StatusConflict || decodeErrorCode(t, rr) != "CONFLICT" {
		t.Fatalf("want 409 CONFLICT, got %d: %s", rr.Code, rr.Body.String())
	}
	if _, err := os.Stat(filepath.Join(target, "keep.txt")); err != nil {
		t.Fatalf("the folder must be untouched: %v", err)
	}
}
