package server

// Break tests (qa3) for the HTTP transfer routes. They run through a chi router
// that wires the real authMiddleware, deviceJailMiddleware, the DELETE route and
// registerTransferRoutes (server.go:78-85 and 237-246), so a revoked token or a
// withdrawn grant is refused by the code production uses. Written by qa-break;
// not run by it.
//
// Run from /home/zaid/Projects/rfe-rn/agent:
//   go test ./internal/server/ -run TestQA3Break -count=1 -v -timeout 300s
//
// Every test uses its own t.TempDir() store, jail root and temp directory and
// opens no port. A restart is simulated in-process: the store is closed and
// reopened on the same directory with a new Manager and router.

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/ZaidQamhieh/rfe/agent/internal/fsops"
	"github.com/ZaidQamhieh/rfe/agent/internal/store"
	"github.com/ZaidQamhieh/rfe/agent/internal/transfer"
)

// qa3Env is one agent surface over one store.
type qa3Env struct {
	db     *store.DB
	tm     *transfer.Manager
	root   string
	temp   string
	router http.Handler
}

// qa3OpenStore opens the store on dir and closes it when the test ends. A second
// Close is harmless because database/sql's Close is idempotent.
func qa3OpenStore(t *testing.T, dir string) *store.DB {
	t.Helper()
	db, err := store.Open(dir)
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// qa3Build wires the transfer routes the way server.New does: behind the bearer
// auth middleware and the per-device jail, with DELETE /transfers/{id} beside them.
func qa3Build(t *testing.T, db *store.DB, dataDir string) *qa3Env {
	t.Helper()
	root := filepath.Join(dataDir, "root")
	temp := filepath.Join(dataDir, "tmp")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("mkdir root: %v", err)
	}
	tm, err := transfer.New(db, temp)
	if err != nil {
		t.Fatalf("transfer.New: %v", err)
	}
	ops := fsops.New([]string{root}, false)
	r := chi.NewRouter()
	r.Route("/v1", func(r chi.Router) {
		r.Group(func(r chi.Router) {
			r.Use(authMiddleware(db))
			r.Use(deviceJailMiddleware(ops))
			r.Delete("/transfers/{id}", deleteTransferHandler(db, tm))
			registerTransferRoutes(r, tm, Config{}, ops)
		})
	})
	return &qa3Env{db: db, tm: tm, root: root, temp: temp, router: r}
}

// qa3Device creates a paired device with the given bearer token. With grants it
// also holds every file capability.
func qa3Device(t *testing.T, db *store.DB, id, token string, grants bool) {
	t.Helper()
	if err := db.CreateDevice(id, id, token); err != nil {
		t.Fatalf("CreateDevice(%s): %v", id, err)
	}
	if grants {
		if err := db.SetDeviceFilePermissions(id, true, true, true, true, true, true); err != nil {
			t.Fatalf("SetDeviceFilePermissions(%s): %v", id, err)
		}
	}
}

func qa3Sum(b []byte) string {
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}

// qa3Part returns chunk n of content split into chunkSize pieces.
func qa3Part(content []byte, chunkSize, n int) []byte {
	start := n * chunkSize
	end := start + chunkSize
	if end > len(content) {
		end = len(content)
	}
	return content[start:end]
}

// do sends one request through the router, with an optional bearer token.
func (e *qa3Env) do(method, path, token string, body []byte, hdr map[string]string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, bytes.NewReader(body))
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	rr := httptest.NewRecorder()
	e.router.ServeHTTP(rr, req)
	return rr
}

// open opens a session through POST /v1/transfers and returns its id.
func (e *qa3Env) open(t *testing.T, token, target string, size int64, chunkSize int, sum string, overwrite bool) string {
	t.Helper()
	body, err := json.Marshal(map[string]any{
		"path": target, "size": size, "sha256": sum, "chunkSize": chunkSize, "overwrite": overwrite,
	})
	if err != nil {
		t.Fatalf("marshal open body: %v", err)
	}
	rr := e.do(http.MethodPost, "/v1/transfers", token, body, nil)
	if rr.Code != http.StatusCreated {
		t.Fatalf("open session: status %d: %s", rr.Code, rr.Body.String())
	}
	var sess struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &sess); err != nil || sess.ID == "" {
		t.Fatalf("open session: no id in %s (%v)", rr.Body.String(), err)
	}
	return sess.ID
}

// put sends chunk idx (kept as the raw path segment) with its X-Chunk-Sha256.
func (e *qa3Env) put(token, id, idx string, data []byte, sum string) *httptest.ResponseRecorder {
	return e.do(http.MethodPut, "/v1/transfers/"+id+"/chunks/"+idx, token, data, map[string]string{"X-Chunk-Sha256": sum})
}

// mustPut sends chunk n of data with a correct hash and requires 204.
func (e *qa3Env) mustPut(t *testing.T, token, id string, n int, data []byte) {
	t.Helper()
	rr := e.put(token, id, strconv.Itoa(n), data, qa3Sum(data))
	if rr.Code != http.StatusNoContent {
		t.Fatalf("chunk %d: status %d: %s", n, rr.Code, rr.Body.String())
	}
}

func (e *qa3Env) complete(token, id string) *httptest.ResponseRecorder {
	return e.do(http.MethodPost, "/v1/transfers/"+id+"/complete", token, nil, nil)
}

// qa3WantError requires the status and the error-envelope code.
func qa3WantError(t *testing.T, what string, rr *httptest.ResponseRecorder, status int, code string) {
	t.Helper()
	if rr.Code != status {
		t.Errorf("%s: status %d, want %d: %s", what, rr.Code, status, rr.Body.String())
		return
	}
	var ae apiError
	if err := json.Unmarshal(rr.Body.Bytes(), &ae); err != nil || ae.Code != code {
		t.Errorf("%s: code %q (err %v), want %q", what, ae.Code, err, code)
	}
}

func qa3Recorded(t *testing.T, db *store.DB, id string) []int {
	t.Helper()
	got, err := db.ChunkNumbers(id)
	if err != nil {
		t.Fatalf("ChunkNumbers: %v", err)
	}
	return got
}

// qa3Absent requires that nothing exists at path (Lstat, so a dangling symlink counts).
func qa3Absent(t *testing.T, path, what string) {
	t.Helper()
	if _, err := os.Lstat(path); !os.IsNotExist(err) {
		t.Errorf("%s still exists at %s (lstat err=%v)", what, path, err)
	}
}

// TestQA3Break_ForeignDeviceWithFullGrantsCannotUseSession: a paired device that
// holds every file grant, but did not open the session, sends chunk 1 and then a
// complete for the owner's session id. Expected: both are 404 NOT_FOUND, the
// foreign chunk is not recorded, and nothing is published. The same holds for an
// overwrite=true session with an existing destination, which must keep its bytes.
// The owner's own complete then succeeds, so the refusals are ownership refusals
// and not a broken session. Label: code failure. Basis: transferhandlers.go:271-274
// (PR-11: a non-owner is treated as if the transfer does not exist) and openapi.yaml
// /transfers/{id}/chunks/{n} and /transfers/{id}/complete (404). Confidence: high.
func TestQA3Break_ForeignDeviceWithFullGrantsCannotUseSession(t *testing.T) {
	dataDir := t.TempDir()
	db := qa3OpenStore(t, dataDir)
	env := qa3Build(t, db, dataDir)
	const ownerTok, foreignTok = "qa3-owner-token", "qa3-foreign-token"
	qa3Device(t, db, "dev-owner", ownerTok, true)
	qa3Device(t, db, "dev-foreign", foreignTok, true)

	content := []byte("ABCDEFGH")
	c0, c1 := qa3Part(content, 4, 0), qa3Part(content, 4, 1)
	target := filepath.Join(env.root, "owned.bin")
	id := env.open(t, ownerTok, target, int64(len(content)), 4, qa3Sum(content), false)
	env.mustPut(t, ownerTok, id, 0, c0)

	qa3WantError(t, "foreign chunk 1", env.put(foreignTok, id, "1", c1, qa3Sum(c1)), http.StatusNotFound, "NOT_FOUND")
	if got := qa3Recorded(t, db, id); len(got) != 1 {
		t.Errorf("foreign chunk changed the received set to %v, want [0]", got)
	}

	env.mustPut(t, ownerTok, id, 1, c1)
	qa3WantError(t, "foreign complete", env.complete(foreignTok, id), http.StatusNotFound, "NOT_FOUND")
	qa3Absent(t, target, "destination after a foreign complete")
	if rr := env.complete(ownerTok, id); rr.Code != http.StatusOK {
		t.Fatalf("owner complete (positive control): status %d: %s", rr.Code, rr.Body.String())
	}
	if got, err := os.ReadFile(target); err != nil || string(got) != string(content) {
		t.Errorf("published bytes = %q (err %v), want %q", got, err, content)
	}

	t.Run("overwrite session keeps existing destination", func(t *testing.T) {
		old := []byte("old-contents")
		target2 := filepath.Join(env.root, "owned-overwrite.bin")
		if err := os.WriteFile(target2, old, 0o644); err != nil {
			t.Fatal(err)
		}
		id2 := env.open(t, ownerTok, target2, int64(len(content)), 4, qa3Sum(content), true)
		env.mustPut(t, ownerTok, id2, 0, c0)
		env.mustPut(t, ownerTok, id2, 1, c1)
		qa3WantError(t, "foreign complete on overwrite session", env.complete(foreignTok, id2), http.StatusNotFound, "NOT_FOUND")
		if got, err := os.ReadFile(target2); err != nil || string(got) != string(old) {
			t.Errorf("foreign complete changed the existing destination to %q (err %v)", got, err)
		}
	})
}

// TestQA3Break_RevokedDeviceCannotUseTransferMidUpload: a device uploads into two
// sessions, is revoked, and then sends the missing chunk of one session and a
// complete for the other, which had every chunk before the revocation. Expected:
// both requests are 401 UNAUTHORIZED, the missing chunk is not recorded, the
// destination is not published, the complete-ready session stays open, and a
// status GET is refused too. Label: code failure. Basis: auth.go:100-101 (a revoked
// device's token is refused, "invalid or revoked token") and openapi.yaml
// /transfers/{id}/chunks/{n}, /transfers/{id}/complete and /transfers/{id} (401).
// Confidence: high.
func TestQA3Break_RevokedDeviceCannotUseTransferMidUpload(t *testing.T) {
	dataDir := t.TempDir()
	db := qa3OpenStore(t, dataDir)
	env := qa3Build(t, db, dataDir)
	const tok = "qa3-revoke-token"
	qa3Device(t, db, "dev-revoked", tok, true)

	content := []byte("ABCDEFGH")
	c0, c1 := qa3Part(content, 4, 0), qa3Part(content, 4, 1)
	missing := filepath.Join(env.root, "revoke-missing.bin")
	idMissing := env.open(t, tok, missing, int64(len(content)), 4, qa3Sum(content), false)
	whole := filepath.Join(env.root, "revoke-whole.bin")
	idWhole := env.open(t, tok, whole, int64(len(content)), 4, qa3Sum(content), false)
	env.mustPut(t, tok, idMissing, 0, c0)
	env.mustPut(t, tok, idWhole, 0, c0)
	env.mustPut(t, tok, idWhole, 1, c1)

	if err := db.RevokeDevice("dev-revoked"); err != nil {
		t.Fatalf("RevokeDevice: %v", err)
	}
	qa3WantError(t, "revoked chunk PUT", env.put(tok, idMissing, "1", c1, qa3Sum(c1)), http.StatusUnauthorized, "UNAUTHORIZED")
	if got := qa3Recorded(t, db, idMissing); len(got) != 1 {
		t.Errorf("revoked device's chunk changed the received set to %v, want [0]", got)
	}
	qa3WantError(t, "revoked complete", env.complete(tok, idWhole), http.StatusUnauthorized, "UNAUTHORIZED")
	qa3Absent(t, whole, "destination after a revoked complete")
	if row, err := db.GetTransfer(idWhole); err != nil || row == nil || row.Status != "open" {
		t.Errorf("session after a revoked complete = %+v (err %v), want status open", row, err)
	}
	qa3WantError(t, "revoked status GET", env.do(http.MethodGet, "/v1/transfers/"+idWhole, tok, nil, nil), http.StatusUnauthorized, "UNAUTHORIZED")
}

// TestQA3Break_UploadGrantWithdrawnMidTransferStopsChunksAndCompletion: the owner
// holds the upload grant, opens two sessions, and has every chunk of one session
// in place. The grant is then withdrawn. Expected: a further chunk on the first
// session and the complete of the second are both 403 CAPABILITY_DENIED, the
// refused chunk is not recorded, the destination is not published, and the second
// session stays open. Label: code failure. Basis: CLAUDE.md "Security / trust
// model" (per-device upload grant) and server.go:240 (requireFileCapabilities(capUpload)
// on the chunk and complete routes, enforced per request by auth.go:176-190).
// Confidence: high.
func TestQA3Break_UploadGrantWithdrawnMidTransferStopsChunksAndCompletion(t *testing.T) {
	dataDir := t.TempDir()
	db := qa3OpenStore(t, dataDir)
	env := qa3Build(t, db, dataDir)
	const tok = "qa3-grant-token"
	qa3Device(t, db, "dev-a", tok, true)

	content := []byte("ABCDEFGH")
	c0, c1 := qa3Part(content, 4, 0), qa3Part(content, 4, 1)
	t1 := filepath.Join(env.root, "grant-missing.bin")
	id1 := env.open(t, tok, t1, int64(len(content)), 4, qa3Sum(content), false)
	t2 := filepath.Join(env.root, "grant-whole.bin")
	id2 := env.open(t, tok, t2, int64(len(content)), 4, qa3Sum(content), false)
	env.mustPut(t, tok, id1, 0, c0)
	env.mustPut(t, tok, id2, 0, c0)
	env.mustPut(t, tok, id2, 1, c1)

	if err := db.SetDeviceFilePermissions("dev-a", true, true, false, true, true, true); err != nil {
		t.Fatalf("withdraw upload grant: %v", err)
	}
	qa3WantError(t, "chunk after upload grant withdrawn", env.put(tok, id1, "1", c1, qa3Sum(c1)), http.StatusForbidden, "CAPABILITY_DENIED")
	if got := qa3Recorded(t, db, id1); len(got) != 1 {
		t.Errorf("refused chunk changed the received set to %v, want [0]", got)
	}
	qa3WantError(t, "complete after upload grant withdrawn", env.complete(tok, id2), http.StatusForbidden, "CAPABILITY_DENIED")
	qa3Absent(t, t2, "destination after a refused complete")
	if row, err := db.GetTransfer(id2); err != nil || row == nil || row.Status != "open" {
		t.Errorf("session after a refused complete = %+v (err %v), want status open", row, err)
	}
}

// TestQA3Break_ConcurrentForeignChunksDuringOwnerUploadAreRefused: while the owner
// uploads all chunks of a session, three foreign workers with full grants send
// every chunk, with correct hashes, twice over. Expected: every foreign request is
// 404 NOT_FOUND, every owner request is 204, the owner's complete then succeeds, and
// the published file equals the source. Label: code failure. Confidence: medium.
// Unforced race: the goroutines are released together but their interleaving is
// not forced, so a pass shows only that no foreign request was accepted in this
// run. Basis: transferhandlers.go:271-274 (ownership is checked before any state
// change, so a foreign request must never change the received set) and PR-11.
func TestQA3Break_ConcurrentForeignChunksDuringOwnerUploadAreRefused(t *testing.T) {
	dataDir := t.TempDir()
	db := qa3OpenStore(t, dataDir)
	env := qa3Build(t, db, dataDir)
	const ownerTok, foreignTok = "qa3-race-owner", "qa3-race-foreign"
	qa3Device(t, db, "dev-owner", ownerTok, true)
	qa3Device(t, db, "dev-foreign", foreignTok, true)

	const chunkSize, chunks, foreignWorkers, foreignPasses = 8, 8, 3, 2
	content := bytes.Repeat([]byte("abcdefgh"), chunks)
	target := filepath.Join(env.root, "contended.bin")
	id := env.open(t, ownerTok, target, int64(len(content)), chunkSize, qa3Sum(content), false)

	start := make(chan struct{})
	ownerCodes := make(chan int, chunks)
	foreignCodes := make(chan int, foreignWorkers*chunks*foreignPasses)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		<-start
		for n := 0; n < chunks; n++ {
			p := qa3Part(content, chunkSize, n)
			ownerCodes <- env.put(ownerTok, id, strconv.Itoa(n), p, qa3Sum(p)).Code
		}
	}()
	for w := 0; w < foreignWorkers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			for pass := 0; pass < foreignPasses; pass++ {
				for n := 0; n < chunks; n++ {
					p := qa3Part(content, chunkSize, n)
					foreignCodes <- env.put(foreignTok, id, strconv.Itoa(n), p, qa3Sum(p)).Code
				}
			}
		}()
	}
	close(start)
	wg.Wait()
	close(ownerCodes)
	close(foreignCodes)
	for code := range ownerCodes {
		if code != http.StatusNoContent {
			t.Errorf("owner chunk status %d, want 204", code)
		}
	}
	for code := range foreignCodes {
		if code != http.StatusNotFound {
			t.Errorf("foreign chunk status %d, want 404", code)
		}
	}
	if rr := env.complete(ownerTok, id); rr.Code != http.StatusOK {
		t.Fatalf("owner complete: status %d: %s", rr.Code, rr.Body.String())
	}
	if got, err := os.ReadFile(target); err != nil || string(got) != string(content) {
		t.Errorf("published bytes differ from the source (err %v)", err)
	}
}

// TestQA3Break_CancelledSessionRefusesLateChunksAndCompleteWithoutRecreatingTemp:
// the owner cancels a half-uploaded session with DELETE /transfers/{id}, then sends
// the missing chunk, a resend of a received chunk and a complete. Expected: the
// DELETE is 204; each later request is 404 NOT_FOUND; the destination is never
// created; the temp file is not recreated by a late chunk; and the session row is
// gone. Label: code failure. Basis: openapi.yaml DELETE /transfers/{id} ("Deletes
// the session record and its temporary upload file"), the 404 of the chunk and
// complete routes, and transferhandlers.go:341-344 (an unknown session is 404).
// Confidence: high.
func TestQA3Break_CancelledSessionRefusesLateChunksAndCompleteWithoutRecreatingTemp(t *testing.T) {
	dataDir := t.TempDir()
	db := qa3OpenStore(t, dataDir)
	env := qa3Build(t, db, dataDir)
	const tok = "qa3-cancel-token"
	qa3Device(t, db, "dev-a", tok, true)

	content := []byte("ABCDEFGH")
	c0, c1 := qa3Part(content, 4, 0), qa3Part(content, 4, 1)
	target := filepath.Join(env.root, "cancelled.bin")
	id := env.open(t, tok, target, int64(len(content)), 4, qa3Sum(content), false)
	env.mustPut(t, tok, id, 0, c0)

	if rr := env.do(http.MethodDelete, "/v1/transfers/"+id, tok, nil, nil); rr.Code != http.StatusNoContent {
		t.Fatalf("cancel (DELETE): status %d: %s", rr.Code, rr.Body.String())
	}
	qa3WantError(t, "late chunk 1 after cancel", env.put(tok, id, "1", c1, qa3Sum(c1)), http.StatusNotFound, "NOT_FOUND")
	qa3WantError(t, "late resend of chunk 0 after cancel", env.put(tok, id, "0", c0, qa3Sum(c0)), http.StatusNotFound, "NOT_FOUND")
	qa3WantError(t, "late complete after cancel", env.complete(tok, id), http.StatusNotFound, "NOT_FOUND")
	qa3Absent(t, target, "destination after cancel")
	qa3Absent(t, filepath.Join(env.temp, id+".tmp"), "temp file after cancel and late requests")
	if row, err := db.GetTransfer(id); err != nil || row != nil {
		t.Errorf("cancelled session row remains: %+v (err %v)", row, err)
	}
}

// TestQA3Break_FinishTwiceRefusedAndLaterFileUntouched: the owner completes a
// session (200), then someone else writes the destination, then the owner completes
// again. Subtests cover overwrite=false and overwrite=true. Expected: the second
// complete is 409 TRANSFER_NOT_OPEN and the destination keeps the other writer's
// bytes. Label: code failure. Basis: openapi.yaml /transfers/{id}/complete 409
// ("the session is no longer open (TRANSFER_NOT_OPEN)") and transferhandlers.go:519-521,
// with transfer.go:639-641 refusing a non-open session before any publish.
// Confidence: high.
func TestQA3Break_FinishTwiceRefusedAndLaterFileUntouched(t *testing.T) {
	for _, overwrite := range []bool{false, true} {
		t.Run(fmt.Sprintf("overwrite=%t", overwrite), func(t *testing.T) {
			dataDir := t.TempDir()
			db := qa3OpenStore(t, dataDir)
			env := qa3Build(t, db, dataDir)
			const tok = "qa3-twice-token"
			qa3Device(t, db, "dev-a", tok, true)

			content := []byte("ABCDEFGH")
			target := filepath.Join(env.root, "twice.bin")
			if overwrite {
				if err := os.WriteFile(target, []byte("old"), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			id := env.open(t, tok, target, int64(len(content)), 4, qa3Sum(content), overwrite)
			env.mustPut(t, tok, id, 0, qa3Part(content, 4, 0))
			env.mustPut(t, tok, id, 1, qa3Part(content, 4, 1))
			if rr := env.complete(tok, id); rr.Code != http.StatusOK {
				t.Fatalf("first complete: status %d: %s", rr.Code, rr.Body.String())
			}

			if err := os.WriteFile(target, []byte("user-replaced"), 0o644); err != nil {
				t.Fatal(err)
			}
			qa3WantError(t, "second complete", env.complete(tok, id), http.StatusConflict, "TRANSFER_NOT_OPEN")
			if got, err := os.ReadFile(target); err != nil || string(got) != "user-replaced" {
				t.Errorf("second complete changed the destination to %q (err %v)", got, err)
			}
		})
	}
}

// TestQA3Break_ChunkIndexOutsideSessionIsRefusedAndWritesNothing: a two-chunk
// session receives out-of-range indexes: -1, 2 (the off-by-one total), 3, a value
// that overflows Atoi, and MaxInt64. Each is refused. Expected: 400 BAD_REQUEST for
// each, no chunk recorded, and the temp file still holds only zero bytes (no write
// at a bad offset). The valid last index is then accepted (positive control).
// Label: code failure. Basis: transferhandlers.go:330-333 (a non-numeric index is
// 400), transferhandlers.go:435-438 and transfer.go:568-573 (an index outside the
// session's chunk count is a client error, PR-12), openapi.yaml chunk PUT 400.
// Confidence: high.
func TestQA3Break_ChunkIndexOutsideSessionIsRefusedAndWritesNothing(t *testing.T) {
	dataDir := t.TempDir()
	db := qa3OpenStore(t, dataDir)
	env := qa3Build(t, db, dataDir)
	const tok = "qa3-index-token"
	qa3Device(t, db, "dev-a", tok, true)

	content := []byte("ABCDEFGH") // two chunks of 4
	c1 := qa3Part(content, 4, 1)
	id := env.open(t, tok, filepath.Join(env.root, "index.bin"), int64(len(content)), 4, qa3Sum(content), false)
	temp := filepath.Join(env.temp, id+".tmp")

	for _, idx := range []string{"-1", "2", "3", "99999999999999999999", "9223372036854775807"} {
		t.Run("index "+idx, func(t *testing.T) {
			qa3WantError(t, "out-of-range chunk "+idx, env.put(tok, id, idx, c1, qa3Sum(c1)), http.StatusBadRequest, "BAD_REQUEST")
		})
	}
	if got := qa3Recorded(t, db, id); len(got) != 0 {
		t.Errorf("refused indexes recorded chunks %v", got)
	}
	raw, err := os.ReadFile(temp)
	if err != nil || !bytes.Equal(raw, make([]byte, len(content))) {
		t.Errorf("temp file after refused indexes = %v (err %v), want all zero bytes", raw, err)
	}
	env.mustPut(t, tok, id, 1, c1)
}

// TestQA3Break_FinishChecksumMismatchOverHTTPPublishesNothingAndStaysClosed: every
// chunk hash is right, but the session declares the hash of other bytes. Expected:
// complete is 422 HASH_MISMATCH, no destination is created, the temp file is
// removed, and afterwards a chunk PUT and a second complete are both 409
// TRANSFER_NOT_OPEN. Label: code failure. Basis: openapi.yaml /transfers/{id}/complete
// 422 (HASH_MISMATCH) and 409 (TRANSFER_NOT_OPEN); transfer.go:674-678 (the temp file
// of a failed session is removed, since the transfer cannot be resumed).
// Confidence: high.
func TestQA3Break_FinishChecksumMismatchOverHTTPPublishesNothingAndStaysClosed(t *testing.T) {
	dataDir := t.TempDir()
	db := qa3OpenStore(t, dataDir)
	env := qa3Build(t, db, dataDir)
	const tok = "qa3-finish-hash-token"
	qa3Device(t, db, "dev-a", tok, true)

	content := []byte("ABCDEFGH")
	c0, c1 := qa3Part(content, 4, 0), qa3Part(content, 4, 1)
	target := filepath.Join(env.root, "finish-hash.bin")
	id := env.open(t, tok, target, int64(len(content)), 4, qa3Sum([]byte("ZZZZZZZZ")), false)
	env.mustPut(t, tok, id, 0, c0)
	env.mustPut(t, tok, id, 1, c1)

	qa3WantError(t, "complete with wrong whole-file hash", env.complete(tok, id), http.StatusUnprocessableEntity, "HASH_MISMATCH")
	qa3Absent(t, target, "destination after a hash mismatch")
	qa3Absent(t, filepath.Join(env.temp, id+".tmp"), "temp file after a hash mismatch")
	qa3WantError(t, "chunk after failed session", env.put(tok, id, "0", c0, qa3Sum(c0)), http.StatusConflict, "TRANSFER_NOT_OPEN")
	qa3WantError(t, "second complete after failed session", env.complete(tok, id), http.StatusConflict, "TRANSFER_NOT_OPEN")
	qa3Absent(t, target, "destination after the failed session was retried")
}

// TestQA3Break_ChunkChecksumMismatchOverHTTPWritesNoBytes: a chunk body does not
// match its X-Chunk-Sha256. Expected: 409 CHUNK_HASH_MISMATCH, nothing recorded, and
// the bytes at that chunk's offset are still zero in the temp file. The correct
// chunk and the rest are then accepted and the file completes with the source bytes.
// Label: code failure. Basis: openapi.yaml chunk PUT 409 (CHUNK_HASH_MISMATCH),
// transferhandlers.go:431-434, and transfer.go:582-586 (the hash is verified before
// any write or record). Confidence: high.
func TestQA3Break_ChunkChecksumMismatchOverHTTPWritesNoBytes(t *testing.T) {
	dataDir := t.TempDir()
	db := qa3OpenStore(t, dataDir)
	env := qa3Build(t, db, dataDir)
	const tok = "qa3-chunk-hash-token"
	qa3Device(t, db, "dev-a", tok, true)

	content := []byte("ABCDEFGH")
	c0, c1 := qa3Part(content, 4, 0), qa3Part(content, 4, 1)
	target := filepath.Join(env.root, "chunk-hash.bin")
	id := env.open(t, tok, target, int64(len(content)), 4, qa3Sum(content), false)
	temp := filepath.Join(env.temp, id+".tmp")

	qa3WantError(t, "chunk with a wrong X-Chunk-Sha256", env.put(tok, id, "0", []byte("AAAA"), qa3Sum([]byte("XXXX"))), http.StatusConflict, "CHUNK_HASH_MISMATCH")
	if got := qa3Recorded(t, db, id); len(got) != 0 {
		t.Errorf("rejected chunk recorded %v", got)
	}
	raw, err := os.ReadFile(temp)
	if err != nil || len(raw) < 4 || !bytes.Equal(raw[:4], make([]byte, 4)) {
		t.Errorf("rejected chunk left bytes in the temp file: %q (err %v)", raw, err)
	}
	env.mustPut(t, tok, id, 0, c0)
	env.mustPut(t, tok, id, 1, c1)
	if rr := env.complete(tok, id); rr.Code != http.StatusOK {
		t.Fatalf("complete after the rejected chunk: status %d: %s", rr.Code, rr.Body.String())
	}
	if got, err := os.ReadFile(target); err != nil || string(got) != string(content) {
		t.Errorf("published bytes = %q (err %v), want %q", got, err, content)
	}
}

// TestQA3Break_RestartOverSameStateKeepsOwnerAndResumes: the owner sends chunk 0,
// the store is closed, and the store and router are rebuilt on the same directory.
// Expected: the owner's status shows chunk 0 received, a foreign device is still
// 404 for that session, and the owner can send chunk 1 and complete, which publishes
// the exact source. Label: code failure. Basis: openapi.yaml GET /transfers/{id}
// ("session status + missing chunks, for resume"), transfer.go:1-7 (the resume
// flow), and transferhandlers.go:271-274 (ownership is the device ID persisted on
// the session row, so it must survive the restart). Confidence: high.
func TestQA3Break_RestartOverSameStateKeepsOwnerAndResumes(t *testing.T) {
	dataDir := t.TempDir()
	const ownerTok, foreignTok = "qa3-restart-owner", "qa3-restart-foreign"
	content := []byte("ABCDEFGH")
	c0, c1 := qa3Part(content, 4, 0), qa3Part(content, 4, 1)

	db1 := qa3OpenStore(t, dataDir)
	env1 := qa3Build(t, db1, dataDir)
	qa3Device(t, db1, "dev-owner", ownerTok, true)
	qa3Device(t, db1, "dev-foreign", foreignTok, true)
	target := filepath.Join(env1.root, "restart.bin")
	id := env1.open(t, ownerTok, target, int64(len(content)), 4, qa3Sum(content), false)
	env1.mustPut(t, ownerTok, id, 0, c0)
	if err := db1.Close(); err != nil {
		t.Fatalf("close store before restart: %v", err)
	}

	db2 := qa3OpenStore(t, dataDir)
	env2 := qa3Build(t, db2, dataDir)
	rr := env2.do(http.MethodGet, "/v1/transfers/"+id, ownerTok, nil, nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("owner status after restart: %d: %s", rr.Code, rr.Body.String())
	}
	var st struct {
		ReceivedChunks []int `json:"receivedChunks"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &st); err != nil || len(st.ReceivedChunks) != 1 || st.ReceivedChunks[0] != 0 {
		t.Errorf("status after restart lists received chunks %v (err %v), want [0]", st.ReceivedChunks, err)
	}
	qa3WantError(t, "foreign status after restart", env2.do(http.MethodGet, "/v1/transfers/"+id, foreignTok, nil, nil), http.StatusNotFound, "NOT_FOUND")
	qa3WantError(t, "foreign chunk after restart", env2.put(foreignTok, id, "1", c1, qa3Sum(c1)), http.StatusNotFound, "NOT_FOUND")
	env2.mustPut(t, ownerTok, id, 1, c1)
	if rr := env2.complete(ownerTok, id); rr.Code != http.StatusOK {
		t.Fatalf("owner complete after restart: status %d: %s", rr.Code, rr.Body.String())
	}
	if got, err := os.ReadFile(target); err != nil || string(got) != string(content) {
		t.Errorf("published bytes after restart = %q (err %v), want %q", got, err, content)
	}
}
