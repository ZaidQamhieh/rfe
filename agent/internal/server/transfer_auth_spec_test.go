package server

// Target: agent/internal/server, transfer and content routes with their file-action grants.
// Every test builds the real router with New(...), so authMiddleware, deviceJailMiddleware,
// requireFileCapabilities and requireWritable all run. Devices are real rows in the store and
// send real bearer tokens.
//
// Requirements covered (the brief's bullets were not numbered, so they are numbered here):
//   B1  A device without the `upload` grant cannot open, chunk or complete an upload.
//       A device with the grant can open, chunk, resume and complete one.
//   B2  A device without the `download` grant cannot start, or resume with a Range request, a
//       GET /content download. A device with the grant can.
//   B3  Grants, the per-device read-only flag and revocation are checked on resume and on every
//       chunk request, not only at start. A revoked or downgraded client cannot continue an old
//       transfer id.
//   B4  Another client cannot use a transfer id. It gets the same 404 NOT_FOUND an unknown id
//       gets and writes nothing. The owner, and an owner (admin) device, still reach the transfer.
//
// Sources read (line numbers in protocol/openapi.yaml):
//   25-31   "File-action permissions are independent per device ... A denied file action returns
//           HTTP 403 with code `CAPABILITY_DENIED`."
//   27-29   "Owner devices bypass these per-device file grants ..."
//   1208-1226  GET /content (download, Range); no 403 is listed here, the 25-31 rule covers it.
//   1262-1337  POST /transfers: 403 "Every mutating transfer route honours read-only"; 1302-1306
//           "When true, the caller must also hold `modify` when opening the session, uploading
//           chunks, and completing it."
//   1339-1382  GET and DELETE /transfers/{id}: 1366-1369 "Anyone else gets 404 - the same response an
//           unknown ID gets, so a foreign session ID is never confirmed."
//   1384-1421  PUT /transfers/{id}/chunks/{n}: 401, 403 (READ_ONLY or CAPABILITY_DENIED), 404, 409.
//   1423-1475  POST /transfers/{id}/complete: 401, 403, 404, 409.
//   2188-2196  device readOnly: "every filesystem write ... is rejected with 403 READ_ONLY".
//   2215-2228  device browse/download/upload/modify grants.
//   2549       "`UNAUTHORIZED` (401): missing/invalid/revoked bearer token."
//   2590-2592  "`CAPABILITY_DENIED` (403): the calling device lacks the per-device permission ...".
//   docs/security-route-matrix.md 16 (transfer owner: "Other devices receive the same 404 as an
//           unknown ID"), 36 (revocation checked on the next request), 41 (GET /content needs
//           download), 43-44 (upload, chunk and completion need upload; overwrite also needs modify,
//           "checked when opening the session, for each chunk, and again at completion").
//
// Run (from agent/): go test ./internal/server -run 'TestQA3_B' -count=1 -v

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/ZaidQamhieh/rfe/agent/internal/pairing"
	"github.com/ZaidQamhieh/rfe/agent/internal/store"
	"github.com/ZaidQamhieh/rfe/agent/internal/transfer"
)

// qa3ChunkSize is the chunk size every session in this file uses.
const qa3ChunkSize = 16

// qa3Content32 is two chunks of qa3ChunkSize bytes each.
var qa3Content32 = []byte("0123456789abcdefghijklmnopqrstuv")

func qa3SHA(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

type qa3Stack struct {
	t    *testing.T
	h    http.Handler
	db   *store.DB
	tm   *transfer.Manager
	root string
}

// qa3NewStack builds the real router over a fresh store. Uploads and downloads must stay under root.
func qa3NewStack(t *testing.T) *qa3Stack {
	t.Helper()
	root := t.TempDir()
	db, st := newTestDepsWithRoots(t, []string{root})
	tm, err := transfer.New(db, t.TempDir())
	if err != nil {
		t.Fatalf("transfer.New: %v", err)
	}
	h, err := New(Config{
		Name:          "qa3-transfer",
		Settings:      st,
		TrashDir:      t.TempDir(),
		ThumbCacheDir: t.TempDir(),
		UpdatesDir:    t.TempDir(),
		StartTime:     time.Now(),
		DataDir:       t.TempDir(),
	}, db, pairing.New(db, "127.0.0.1", "", "fp"), tm)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return &qa3Stack{t: t, h: h, db: db, tm: tm, root: root}
}

// device mints a paired, non-owner device with exactly the given file grants.
// It returns the device id and its bearer token.
func (s *qa3Stack) device(name string, browse, download, upload, modify, del, share bool) (string, string) {
	s.t.Helper()
	token := "tok-" + name
	id, err := s.db.UpsertDevice(name, name, token, "", false)
	if err != nil {
		s.t.Fatalf("UpsertDevice(%s): %v", name, err)
	}
	if err := s.db.SetDeviceFilePermissions(id, browse, download, upload, modify, del, share); err != nil {
		s.t.Fatalf("SetDeviceFilePermissions(%s): %v", name, err)
	}
	return id, token
}

// owner registers the login account. Its device has owner provenance (viaLogin=true).
func (s *qa3Stack) owner() (string, string) {
	s.t.Helper()
	token := "tok-qa3-owner"
	id, err := s.db.RegisterAccount("qa3-owner", "qa3-owner", token, "", "qa3owner", "hash")
	if err != nil {
		s.t.Fatalf("RegisterAccount: %v", err)
	}
	return id, token
}

func (s *qa3Stack) do(method, path, token string, body []byte, headers map[string]string) *httptest.ResponseRecorder {
	s.t.Helper()
	req := httptest.NewRequest(method, path, bytes.NewReader(body))
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rr := httptest.NewRecorder()
	s.h.ServeHTTP(rr, req)
	return rr
}

// openSession posts POST /v1/transfers through the router.
func (s *qa3Stack) openSession(token, target string, size int64, sha string, overwrite bool) *httptest.ResponseRecorder {
	s.t.Helper()
	body, err := json.Marshal(map[string]any{
		"path":      target,
		"size":      size,
		"sha256":    sha,
		"chunkSize": qa3ChunkSize,
		"overwrite": overwrite,
	})
	if err != nil {
		s.t.Fatalf("marshal open body: %v", err)
	}
	return s.do(http.MethodPost, "/v1/transfers", token, body, nil)
}

// putChunk sends PUT /v1/transfers/{id}/chunks/{n} with a matching X-Chunk-Sha256.
func (s *qa3Stack) putChunk(token, id string, n int, data []byte) *httptest.ResponseRecorder {
	s.t.Helper()
	return s.do(http.MethodPut, fmt.Sprintf("/v1/transfers/%s/chunks/%d", id, n), token, data,
		map[string]string{"X-Chunk-Sha256": qa3SHA(data)})
}

func (s *qa3Stack) complete(token, id string) *httptest.ResponseRecorder {
	s.t.Helper()
	return s.do(http.MethodPost, "/v1/transfers/"+id+"/complete", token, nil, nil)
}

func (s *qa3Stack) status(token, id string) *httptest.ResponseRecorder {
	s.t.Helper()
	return s.do(http.MethodGet, "/v1/transfers/"+id, token, nil, nil)
}

// qa3WantStatus fails unless rr has the wanted HTTP status and, when wantErr is set, the wanted error code.
func qa3WantStatus(t *testing.T, what string, rr *httptest.ResponseRecorder, wantCode int, wantErr string) {
	t.Helper()
	if rr.Code != wantCode {
		t.Fatalf("%s: status = %d, want %d: %s", what, rr.Code, wantCode, rr.Body.String())
	}
	if wantErr == "" {
		return
	}
	var e apiError
	if err := json.Unmarshal(rr.Body.Bytes(), &e); err != nil {
		t.Fatalf("%s: decode error body %q: %v", what, rr.Body.String(), err)
	}
	if e.Code != wantErr {
		t.Fatalf("%s: error code = %q, want %q", what, e.Code, wantErr)
	}
}

func qa3SessionID(t *testing.T, rr *httptest.ResponseRecorder) string {
	t.Helper()
	var got struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil || got.ID == "" {
		t.Fatalf("no session id in %d response %q (decode err %v)", rr.Code, rr.Body.String(), err)
	}
	return got.ID
}

func qa3Received(t *testing.T, rr *httptest.ResponseRecorder) []int {
	t.Helper()
	var got struct {
		ReceivedChunks []int `json:"receivedChunks"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode status body %q: %v", rr.Body.String(), err)
	}
	return got.ReceivedChunks
}

// TestQA3_B1_UploadStartChunkCompleteNeedUploadGrant covers B1 (the denial half).
// Requirement B1: a device without the `upload` grant cannot open, chunk or complete an upload.
// Source: openapi.yaml 30-31 "A denied file action returns HTTP 403 with code `CAPABILITY_DENIED`."
//
//	and 1317-1323 (POST /transfers 403), 1406-1411 (PUT chunk 403), 1458-1463 (complete 403),
//	2225-2228 (upload: "Allows content writes and resumable transfers"); security-route-matrix.md 43.
//
// Expected: start -> 403 CAPABILITY_DENIED; chunk -> 403 CAPABILITY_DENIED and nothing recorded;
//
//	complete -> 403 CAPABILITY_DENIED. The browse-only device is the owner of the seeded session,
//	so the refusal comes from the grant and not from ownership.
//
// catches: a browse-only device opens an upload, records a chunk, or publishes a file.
func TestQA3_B1_UploadStartChunkCompleteNeedUploadGrant(t *testing.T) {
	s := qa3NewStack(t)
	// Browse-only: browse on, every other grant off (openapi 2215-2228 defaults for new devices).
	id, token := s.device("qa3-browse", true, false, false, false, false, false)
	target := filepath.Join(s.root, "b1-denied.bin")
	size := int64(len(qa3Content32))
	sha := qa3SHA(qa3Content32)

	qa3WantStatus(t, "start by a browse-only device", s.openSession(token, target, size, sha, false),
		http.StatusForbidden, "CAPABILITY_DENIED")

	// The start was refused, so seed the session directly with this device as its owner.
	sid := uuid.NewString()
	if _, err := s.tm.OpenSession(sid, target, size, qa3ChunkSize, sha, false, id); err != nil {
		t.Fatalf("seed session: %v", err)
	}
	qa3WantStatus(t, "chunk by a browse-only device", s.putChunk(token, sid, 0, qa3Content32[:qa3ChunkSize]),
		http.StatusForbidden, "CAPABILITY_DENIED")
	qa3WantStatus(t, "complete by a browse-only device", s.complete(token, sid),
		http.StatusForbidden, "CAPABILITY_DENIED")

	st, err := s.tm.Status(sid)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if len(st.ReceivedChunks) != 0 {
		t.Fatalf("refused chunk was recorded: %v", st.ReceivedChunks)
	}
}

// TestQA3_B1_UploadGrantLetsClientStartResumeChunkAndComplete covers B1 (the permitted half).
// Requirement B1: with the `upload` grant, the device can open, chunk, resume and complete an upload.
// Source: openapi.yaml 1262-1310 (open, 201), 1384-1415 (chunk, 204), 1339-1351 (status/resume, 200,
//
//	"A successful status/resume request by the session owner refreshes the session's activity time"),
//	1423-1456 (complete, 200, verified=true, sha256).
//
// Expected: start 201; chunk 0 204; status shows receivedChunks [0]; chunk 1 204; complete 200 with
//
//	verified=true and sha256 == SHA-256 of the content; the destination holds the content.
//
// catches: an upload-granted device refused at any step, or a resume that drops recorded chunks.
func TestQA3_B1_UploadGrantLetsClientStartResumeChunkAndComplete(t *testing.T) {
	s := qa3NewStack(t)
	_, token := s.device("qa3-upload", true, false, true, false, false, false)
	target := filepath.Join(s.root, "b1-ok.bin")
	sha := qa3SHA(qa3Content32)

	start := s.openSession(token, target, int64(len(qa3Content32)), sha, false)
	qa3WantStatus(t, "start with upload grant", start, http.StatusCreated, "")
	id := qa3SessionID(t, start)

	qa3WantStatus(t, "chunk 0", s.putChunk(token, id, 0, qa3Content32[:qa3ChunkSize]), http.StatusNoContent, "")

	resume := s.status(token, id)
	qa3WantStatus(t, "resume status by owner", resume, http.StatusOK, "")
	if got := qa3Received(t, resume); len(got) != 1 || got[0] != 0 {
		t.Fatalf("receivedChunks after chunk 0 = %v, want [0]", got)
	}

	qa3WantStatus(t, "chunk 1", s.putChunk(token, id, 1, qa3Content32[qa3ChunkSize:]), http.StatusNoContent, "")

	done := s.complete(token, id)
	qa3WantStatus(t, "complete", done, http.StatusOK, "")
	var got struct {
		Verified bool   `json:"verified"`
		SHA256   string `json:"sha256"`
	}
	if err := json.Unmarshal(done.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode complete body: %v", err)
	}
	if !got.Verified || got.SHA256 != sha {
		t.Fatalf("complete: verified=%v sha256=%q, want verified=true sha256=%q", got.Verified, got.SHA256, sha)
	}
	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("read destination: %v", err)
	}
	if !bytes.Equal(data, qa3Content32) {
		t.Fatalf("destination content = %q, want %q", data, qa3Content32)
	}
}

// TestQA3_B2_DownloadNeedsDownloadGrantForStartAndRangeResume covers B2.
// Requirement B2: a device without `download` cannot start or resume (Range) a GET /content download;
//
//	with `download` it can.
//
// Source: openapi.yaml 25-31 (CAPABILITY_DENIED, 403), 1208-1226 (GET /content, 200 and 206 with Range),
//
//	2221-2224 (download: "Allows file-content reads"); security-route-matrix.md 41 (GET /v1/content needs download).
//
// Expected: browse-only device: GET -> 403 CAPABILITY_DENIED; GET with Range "bytes=6-10" -> 403 CAPABILITY_DENIED.
//
//	download device: GET -> 200 with the whole file; GET with Range "bytes=6-10" -> 206 with bytes 6..10
//	inclusive of the file (the slice body[6:11]).
//
// catches: a browse-only device reads file bytes at start or on a Range resume; or a download-granted
//
//	device is refused.
func TestQA3_B2_DownloadNeedsDownloadGrantForStartAndRangeResume(t *testing.T) {
	s := qa3NewStack(t)
	file := filepath.Join(s.root, "b2.txt")
	body := []byte("hello world")
	if err := os.WriteFile(file, body, 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	q := "/v1/content?path=" + url.QueryEscape(file)
	_, browseTok := s.device("qa3-b2-browse", true, false, false, false, false, false)
	_, dlTok := s.device("qa3-b2-download", true, true, false, false, false, false)

	qa3WantStatus(t, "start download without download grant",
		s.do(http.MethodGet, q, browseTok, nil, nil), http.StatusForbidden, "CAPABILITY_DENIED")
	qa3WantStatus(t, "range resume without download grant",
		s.do(http.MethodGet, q, browseTok, nil, map[string]string{"Range": "bytes=6-10"}),
		http.StatusForbidden, "CAPABILITY_DENIED")

	full := s.do(http.MethodGet, q, dlTok, nil, nil)
	qa3WantStatus(t, "download with download grant", full, http.StatusOK, "")
	if !bytes.Equal(full.Body.Bytes(), body) {
		t.Fatalf("download body = %q, want %q", full.Body.Bytes(), body)
	}

	part := s.do(http.MethodGet, q, dlTok, nil, map[string]string{"Range": "bytes=6-10"})
	qa3WantStatus(t, "range resume with download grant", part, http.StatusPartialContent, "")
	if !bytes.Equal(part.Body.Bytes(), body[6:11]) {
		t.Fatalf("range body = %q, want %q", part.Body.Bytes(), body[6:11])
	}
}

// TestQA3_B1_B2_OwnerDeviceBypassesFileGrants covers the owner exception in B1 and B2.
// Requirement B1/B2 (owner exception): owner devices bypass the per-device file grants.
// Source: openapi.yaml 27-29 "Owner devices bypass these per-device file grants, while the agent's global
//
//	read-only policy, configured roots, per-device jail/read-only controls, and global share-link switch
//	remain in force."; 1747-1748 "login/register owner devices retain full file access and bypass per-device
//	file grants."; security-route-matrix.md 13 (file capability gates bypassed for owner devices).
//
// Expected: owner device with every grant off: POST /transfers -> 201; GET /content -> 200.
// catches: owner devices refused by empty per-device grants, which would lock the owner out.
func TestQA3_B1_B2_OwnerDeviceBypassesFileGrants(t *testing.T) {
	s := qa3NewStack(t)
	ownerID, ownerTok := s.owner()
	if err := s.db.SetDeviceFilePermissions(ownerID, false, false, false, false, false, false); err != nil {
		t.Fatalf("clear owner grants: %v", err)
	}
	file := filepath.Join(s.root, "b2-owner.txt")
	if err := os.WriteFile(file, []byte("owner view"), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	target := filepath.Join(s.root, "b1-owner.bin")
	sha := qa3SHA(qa3Content32)

	qa3WantStatus(t, "owner upload start with no upload grant",
		s.openSession(ownerTok, target, int64(len(qa3Content32)), sha, false), http.StatusCreated, "")
	qa3WantStatus(t, "owner download with no download grant",
		s.do(http.MethodGet, "/v1/content?path="+url.QueryEscape(file), ownerTok, nil, nil), http.StatusOK, "")
}

// TestQA3_B3_RevokedClientCannotContinueItsOldTransferId covers B3 (revocation).
// Requirement B3: a client revoked after starting a transfer cannot continue it with the old transfer id.
// Source: openapi.yaml 2549 "`UNAUTHORIZED` (401): missing/invalid/revoked bearer token."; 1384-1421
//
//	(chunk 401) and 1423-1475 (complete 401); security-route-matrix.md 36 ("Revocation is checked by
//	auth middleware on the next request").
//
// Expected: after RevokeDevice, chunk 1 -> 401 UNAUTHORIZED, complete -> 401 UNAUTHORIZED, status -> 401
//
//	UNAUTHORIZED. The owner-level status still shows receivedChunks [0] (chunk 0, the one sent before revocation).
//
// catches: a revoked device keeps writing chunks or publishing with its old transfer id.
func TestQA3_B3_RevokedClientCannotContinueItsOldTransferId(t *testing.T) {
	s := qa3NewStack(t)
	devID, tok := s.device("qa3-revoked", true, false, true, false, false, false)
	_, adminTok := s.owner()
	target := filepath.Join(s.root, "b3-revoked.bin")
	sha := qa3SHA(qa3Content32)

	start := s.openSession(tok, target, int64(len(qa3Content32)), sha, false)
	qa3WantStatus(t, "start before revocation", start, http.StatusCreated, "")
	id := qa3SessionID(t, start)
	qa3WantStatus(t, "chunk 0 before revocation", s.putChunk(tok, id, 0, qa3Content32[:qa3ChunkSize]), http.StatusNoContent, "")

	if err := s.db.RevokeDevice(devID); err != nil {
		t.Fatalf("RevokeDevice: %v", err)
	}
	qa3WantStatus(t, "chunk 1 after revocation", s.putChunk(tok, id, 1, qa3Content32[qa3ChunkSize:]),
		http.StatusUnauthorized, "UNAUTHORIZED")
	qa3WantStatus(t, "complete after revocation", s.complete(tok, id), http.StatusUnauthorized, "UNAUTHORIZED")
	qa3WantStatus(t, "status after revocation", s.status(tok, id), http.StatusUnauthorized, "UNAUTHORIZED")

	owner := s.status(adminTok, id)
	qa3WantStatus(t, "owner-level status", owner, http.StatusOK, "")
	if got := qa3Received(t, owner); len(got) != 1 || got[0] != 0 {
		t.Fatalf("receivedChunks after revoked attempts = %v, want [0]", got)
	}
}

// TestQA3_B3_DowngradedUploadGrantStopsChunkAndCompleteUntilRestored covers B3 (grant removed mid-transfer).
// Requirement B3: the upload grant is checked on every chunk request and on complete, not only at start.
// Source: openapi.yaml 1406-1411 (chunk 403 CAPABILITY_DENIED), 1458-1463 (complete 403 CAPABILITY_DENIED);
//
//	security-route-matrix.md 28 ("Upload creation/chunk/completion additionally require `upload` and
//	writable effective policy.").
//
// Expected: after upload is removed, chunk 1 -> 403 CAPABILITY_DENIED and complete -> 403 CAPABILITY_DENIED,
//
//	and the owner-level status still shows [0]. After the grant is restored, chunk 1 -> 204 and complete -> 200.
//
// catches: a grant removed after start still lets the device write or publish; or a restored grant is ignored.
func TestQA3_B3_DowngradedUploadGrantStopsChunkAndCompleteUntilRestored(t *testing.T) {
	s := qa3NewStack(t)
	devID, tok := s.device("qa3-downgrade", true, false, true, false, false, false)
	_, adminTok := s.owner()
	target := filepath.Join(s.root, "b3-downgrade.bin")
	sha := qa3SHA(qa3Content32)

	start := s.openSession(tok, target, int64(len(qa3Content32)), sha, false)
	qa3WantStatus(t, "start before downgrade", start, http.StatusCreated, "")
	id := qa3SessionID(t, start)
	qa3WantStatus(t, "chunk 0 before downgrade", s.putChunk(tok, id, 0, qa3Content32[:qa3ChunkSize]), http.StatusNoContent, "")

	if err := s.db.SetDeviceFilePermissions(devID, true, false, false, false, false, false); err != nil {
		t.Fatalf("remove upload grant: %v", err)
	}
	qa3WantStatus(t, "chunk 1 after upload grant removed", s.putChunk(tok, id, 1, qa3Content32[qa3ChunkSize:]),
		http.StatusForbidden, "CAPABILITY_DENIED")
	qa3WantStatus(t, "complete after upload grant removed", s.complete(tok, id),
		http.StatusForbidden, "CAPABILITY_DENIED")
	owner := s.status(adminTok, id)
	qa3WantStatus(t, "owner-level status after refused chunk", owner, http.StatusOK, "")
	if got := qa3Received(t, owner); len(got) != 1 || got[0] != 0 {
		t.Fatalf("receivedChunks after refused chunk = %v, want [0]", got)
	}

	if err := s.db.SetDeviceFilePermissions(devID, true, false, true, false, false, false); err != nil {
		t.Fatalf("restore upload grant: %v", err)
	}
	qa3WantStatus(t, "chunk 1 after upload grant restored", s.putChunk(tok, id, 1, qa3Content32[qa3ChunkSize:]),
		http.StatusNoContent, "")
	qa3WantStatus(t, "complete after upload grant restored", s.complete(tok, id), http.StatusOK, "")
}

// TestQA3_B3_ReadOnlyDowngradeRefusesChunkAndComplete covers B3 (per-device read-only flag set mid-transfer).
// Requirement B3: the per-device read-only flag is checked on each chunk and on complete.
// Source: openapi.yaml 2188-2196 (device readOnly: "every filesystem write ... is rejected with 403 READ_ONLY");
//
//	1406-1411 (chunk 403 "the calling device is read-only ... code=READ_ONLY or CAPABILITY_DENIED");
//	1458-1463 (complete 403 READ_ONLY).
//
// Expected: after readOnly is set, chunk 1 -> 403 READ_ONLY and complete -> 403 READ_ONLY.
// catches: a read-only device keeps writing or publishing into an open upload.
func TestQA3_B3_ReadOnlyDowngradeRefusesChunkAndComplete(t *testing.T) {
	s := qa3NewStack(t)
	devID, tok := s.device("qa3-ro", true, false, true, false, false, false)
	target := filepath.Join(s.root, "b3-ro.bin")
	sha := qa3SHA(qa3Content32)

	start := s.openSession(tok, target, int64(len(qa3Content32)), sha, false)
	qa3WantStatus(t, "start before read-only", start, http.StatusCreated, "")
	id := qa3SessionID(t, start)
	qa3WantStatus(t, "chunk 0 before read-only", s.putChunk(tok, id, 0, qa3Content32[:qa3ChunkSize]), http.StatusNoContent, "")

	if err := s.db.SetDeviceReadOnly(devID, true); err != nil {
		t.Fatalf("SetDeviceReadOnly: %v", err)
	}
	qa3WantStatus(t, "chunk 1 after device set read-only", s.putChunk(tok, id, 1, qa3Content32[qa3ChunkSize:]),
		http.StatusForbidden, "READ_ONLY")
	qa3WantStatus(t, "complete after device set read-only", s.complete(tok, id),
		http.StatusForbidden, "READ_ONLY")
}

// TestQA3_B3_OverwriteNeedsModifyAtOpenChunkAndComplete covers B3 (overwrite sessions need modify each step).
// Requirement B3: with overwrite=true the caller must hold `modify` when opening, uploading each chunk,
//
//	and completing.
//
// Source: openapi.yaml 1297-1306 "When true, the caller must also hold `modify` when opening the session,
//
//	uploading chunks, and completing it."; 1317-1323 (POST /transfers 403 CAPABILITY_DENIED for
//	overwrite without modify); security-route-matrix.md 43 ("checked when opening the session, for each
//	chunk, and again at completion").
//
// Expected: upload-only device, overwrite=true -> 403 CAPABILITY_DENIED at open. Modify holder opens 201.
//
//	Modify removed mid-session: chunk 0 -> 403 CAPABILITY_DENIED, complete -> 403 CAPABILITY_DENIED, and the
//	existing file still reads "old". Modify restored: chunk 0 -> 204, complete -> 200, file holds the content.
//
// catches: an overwrite opens, uploads or publishes without modify, or a file is replaced after the grant is gone.
func TestQA3_B3_OverwriteNeedsModifyAtOpenChunkAndComplete(t *testing.T) {
	s := qa3NewStack(t)
	_, upTok := s.device("qa3-ow-upload", true, false, true, false, false, false)
	modID, modTok := s.device("qa3-ow-modify", true, false, true, true, false, false)
	target := filepath.Join(s.root, "replace.bin")
	if err := os.WriteFile(target, []byte("old"), 0o600); err != nil {
		t.Fatalf("write existing file: %v", err)
	}
	content := qa3Content32[:qa3ChunkSize]
	sha := qa3SHA(content)

	qa3WantStatus(t, "overwrite open without modify grant",
		s.openSession(upTok, target, int64(len(content)), sha, true), http.StatusForbidden, "CAPABILITY_DENIED")

	start := s.openSession(modTok, target, int64(len(content)), sha, true)
	qa3WantStatus(t, "overwrite open with modify grant", start, http.StatusCreated, "")
	id := qa3SessionID(t, start)

	if err := s.db.SetDeviceFilePermissions(modID, true, false, true, false, false, false); err != nil {
		t.Fatalf("remove modify grant: %v", err)
	}
	qa3WantStatus(t, "chunk after modify removed", s.putChunk(modTok, id, 0, content),
		http.StatusForbidden, "CAPABILITY_DENIED")
	qa3WantStatus(t, "complete after modify removed", s.complete(modTok, id),
		http.StatusForbidden, "CAPABILITY_DENIED")
	if got, err := os.ReadFile(target); err != nil || string(got) != "old" {
		t.Fatalf("existing file after refused overwrite = (%q, %v), want \"old\"", got, err)
	}

	if err := s.db.SetDeviceFilePermissions(modID, true, false, true, true, false, false); err != nil {
		t.Fatalf("restore modify grant: %v", err)
	}
	qa3WantStatus(t, "chunk after modify restored", s.putChunk(modTok, id, 0, content), http.StatusNoContent, "")
	qa3WantStatus(t, "complete after modify restored", s.complete(modTok, id), http.StatusOK, "")
	if got, err := os.ReadFile(target); err != nil || !bytes.Equal(got, content) {
		t.Fatalf("destination after overwrite = (%q, %v), want %q", got, err, content)
	}
}

// TestQA3_B4_ForeignClientGetsUnknownIdResponseAndCannotWrite covers B4.
// Requirement B4: another client cannot use a transfer id. The response is the same 404 NOT_FOUND an
//
//	unknown id gets, and nothing is written. The owner, and an owner (admin) device, still reach it.
//
// Source: openapi.yaml 1366-1369 "Anyone else gets 404 - the same response an unknown ID gets, so a foreign
//
//	session ID is never confirmed."; 1379-1382 (404 NOT_FOUND, "or it belongs to another device");
//	security-route-matrix.md 16 ("Other devices receive the same 404 as an unknown ID.").
//
// Expected: foreign client, with full upload grants: GET -> 404 NOT_FOUND; DELETE -> 404 NOT_FOUND;
//
//	chunk 0 -> 404 NOT_FOUND; complete -> 404 NOT_FOUND. Unknown-id GET by the foreign client -> 404 NOT_FOUND.
//	Owner-level status -> 200 with receivedChunks [] (nothing written, the session still exists).
//	The owner's own chunk 0 afterwards -> 204.
//
// catches: a foreign client reads, deletes, writes to, or publishes the session; or the foreign reply differs
//
//	from an unknown-id reply (confirms the id exists).
func TestQA3_B4_ForeignClientGetsUnknownIdResponseAndCannotWrite(t *testing.T) {
	s := qa3NewStack(t)
	_, ownerTok := s.device("qa3-b4-owner", true, false, true, false, false, false)
	_, foreignTok := s.device("qa3-b4-foreign", true, false, true, false, false, false)
	_, adminTok := s.owner()
	target := filepath.Join(s.root, "b4.bin")
	content := qa3Content32[:qa3ChunkSize]
	sha := qa3SHA(content)

	start := s.openSession(ownerTok, target, int64(len(content)), sha, false)
	qa3WantStatus(t, "owner start", start, http.StatusCreated, "")
	id := qa3SessionID(t, start)
	const unknown = "00000000-0000-4000-8000-000000000000"

	qa3WantStatus(t, "foreign client, unknown id", s.status(foreignTok, unknown), http.StatusNotFound, "NOT_FOUND")
	qa3WantStatus(t, "foreign client, status of owner's id", s.status(foreignTok, id), http.StatusNotFound, "NOT_FOUND")
	qa3WantStatus(t, "foreign client, delete of owner's id",
		s.do(http.MethodDelete, "/v1/transfers/"+id, foreignTok, nil, nil), http.StatusNotFound, "NOT_FOUND")
	qa3WantStatus(t, "foreign client, chunk into owner's id", s.putChunk(foreignTok, id, 0, content),
		http.StatusNotFound, "NOT_FOUND")
	qa3WantStatus(t, "foreign client, complete of owner's id", s.complete(foreignTok, id),
		http.StatusNotFound, "NOT_FOUND")

	owner := s.status(adminTok, id)
	qa3WantStatus(t, "owner-level status after foreign attempts", owner, http.StatusOK, "")
	if got := qa3Received(t, owner); len(got) != 0 {
		t.Fatalf("foreign attempts recorded chunks: %v", got)
	}

	qa3WantStatus(t, "owner chunk 0 after foreign attempts", s.putChunk(ownerTok, id, 0, content),
		http.StatusNoContent, "")
}
