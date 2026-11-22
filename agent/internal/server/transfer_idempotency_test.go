package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

const (
	idemKeyA = "0123456789abcdef0123456789abcdef"
	idemKeyB = "fedcba9876543210fedcba9876543210"
)

func (s *qa3Stack) openKeyed(token, target, key string, size int64, sha string) *httptest.ResponseRecorder {
	s.t.Helper()
	body, _ := json.Marshal(map[string]any{"path": target, "size": size, "sha256": sha, "chunkSize": qa3ChunkSize, "overwrite": false})
	req := httptest.NewRequest(http.MethodPost, "/v1/transfers", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	if key != "" {
		req.Header.Set("Idempotency-Key", key)
	}
	rr := httptest.NewRecorder()
	s.h.ServeHTTP(rr, req)
	return rr
}

func (s *qa3Stack) openCount() int {
	s.t.Helper()
	open, err := s.db.ListOpenTransfers()
	if err != nil {
		s.t.Fatal(err)
	}
	return len(open)
}

// A repeat POST /transfers with the same key from the same device answers 200 with the first answer's session
// instead of creating another one; the first answer is 201.
func TestIdempotencyKeyRepeatReturnsSameSession(t *testing.T) {
	s := qa3NewStack(t)
	_, tok := s.device("phone", true, true, true, false, false, false)
	target := filepath.Join(s.root, "a.bin")
	sha := qa3SHA(qa3Content32)

	first := s.openKeyed(tok, target, idemKeyA, int64(len(qa3Content32)), sha)
	qa3WantStatus(t, "first open", first, http.StatusCreated, "")
	second := s.openKeyed(tok, target, idemKeyA, int64(len(qa3Content32)), sha)
	qa3WantStatus(t, "repeat open", second, http.StatusOK, "")
	if qa3SessionID(t, first) != qa3SessionID(t, second) {
		t.Fatalf("repeat returned another session: %s vs %s", first.Body.String(), second.Body.String())
	}
	if n := s.openCount(); n != 1 {
		t.Fatalf("open sessions = %d, want 1", n)
	}
}

// Four lost 201s used to fill the device quota (4) and the fifth open got 429; with a key they all collapse.
func TestIdempotencyKeyLostAcksNeverFillQuota(t *testing.T) {
	s := qa3NewStack(t)
	_, tok := s.device("phone", true, true, true, false, false, false)
	sha := qa3SHA(qa3Content32)
	for i := 0; i < 6; i++ {
		rr := s.openKeyed(tok, filepath.Join(s.root, "a.bin"), idemKeyA, int64(len(qa3Content32)), sha)
		if rr.Code != http.StatusCreated && rr.Code != http.StatusOK {
			t.Fatalf("open %d: %d %s", i, rr.Code, rr.Body.String())
		}
	}
	qa3WantStatus(t, "another upload", s.openKeyed(tok, filepath.Join(s.root, "b.bin"), idemKeyB, int64(len(qa3Content32)), sha), http.StatusCreated, "")
	if n := s.openCount(); n != 2 {
		t.Fatalf("open sessions = %d, want 2", n)
	}
}

func TestIdempotencyKeyDifferentKeyOrNoKeyMakesNewSessions(t *testing.T) {
	s := qa3NewStack(t)
	_, tok := s.device("phone", true, true, true, false, false, false)
	sha := qa3SHA(qa3Content32)
	ids := map[string]bool{}
	for _, key := range []string{idemKeyA, idemKeyB, "", ""} {
		rr := s.openKeyed(tok, filepath.Join(s.root, "a.bin"), key, int64(len(qa3Content32)), sha)
		qa3WantStatus(t, "open "+key, rr, http.StatusCreated, "")
		ids[qa3SessionID(t, rr)] = true
	}
	if len(ids) != 4 {
		t.Fatalf("distinct sessions = %d, want 4", len(ids))
	}
}

// The key is scoped to the device: another device using the same key gets its own session and never sees the first.
func TestIdempotencyKeyIsScopedToTheDevice(t *testing.T) {
	s := qa3NewStack(t)
	_, tokA := s.device("phoneA", true, true, true, false, false, false)
	_, tokB := s.device("phoneB", true, true, true, false, false, false)
	sha := qa3SHA(qa3Content32)
	target := filepath.Join(s.root, "a.bin")
	a := s.openKeyed(tokA, target, idemKeyA, int64(len(qa3Content32)), sha)
	b := s.openKeyed(tokB, target, idemKeyA, int64(len(qa3Content32)), sha)
	qa3WantStatus(t, "device A", a, http.StatusCreated, "")
	qa3WantStatus(t, "device B", b, http.StatusCreated, "")
	if qa3SessionID(t, a) == qa3SessionID(t, b) {
		t.Fatal("two devices with the same key shared a session")
	}
}

// A repeat after chunks landed lists them, so the client resumes from the agent's state.
func TestIdempotencyKeyRepeatListsReceivedChunks(t *testing.T) {
	s := qa3NewStack(t)
	_, tok := s.device("phone", true, true, true, false, false, false)
	sha := qa3SHA(qa3Content32)
	target := filepath.Join(s.root, "a.bin")
	first := s.openKeyed(tok, target, idemKeyA, int64(len(qa3Content32)), sha)
	id := qa3SessionID(t, first)
	qa3WantStatus(t, "chunk", s.putChunk(tok, id, 0, qa3Content32[:qa3ChunkSize]), http.StatusNoContent, "")
	again := s.openKeyed(tok, target, idemKeyA, int64(len(qa3Content32)), sha)
	qa3WantStatus(t, "repeat", again, http.StatusOK, "")
	if got := qa3Received(t, again); len(got) != 1 || got[0] != 0 {
		t.Fatalf("receivedChunks = %v, want [0]", got)
	}
}

// A key whose session is no longer open, or that now describes a different upload, is 409 IDEMPOTENCY_KEY_USED.
func TestIdempotencyKeyUsedAndMismatchedAre409(t *testing.T) {
	s := qa3NewStack(t)
	_, tok := s.device("phone", true, true, true, false, false, false)
	sha := qa3SHA(qa3Content32)
	target := filepath.Join(s.root, "a.bin")
	first := s.openKeyed(tok, target, idemKeyA, int64(len(qa3Content32)), sha)
	id := qa3SessionID(t, first)

	qa3WantStatus(t, "other bytes", s.openKeyed(tok, target, idemKeyA, 5, qa3SHA([]byte("hello"))), http.StatusConflict, "IDEMPOTENCY_KEY_USED")

	qa3WantStatus(t, "chunk 0", s.putChunk(tok, id, 0, qa3Content32[:qa3ChunkSize]), http.StatusNoContent, "")
	qa3WantStatus(t, "chunk 1", s.putChunk(tok, id, 1, qa3Content32[qa3ChunkSize:]), http.StatusNoContent, "")
	qa3WantStatus(t, "complete", s.complete(tok, id), http.StatusOK, "")
	qa3WantStatus(t, "after complete", s.openKeyed(tok, target, idemKeyA, int64(len(qa3Content32)), sha), http.StatusConflict, "IDEMPOTENCY_KEY_USED")
}

func TestIdempotencyKeyMalformedIs400(t *testing.T) {
	s := qa3NewStack(t)
	_, tok := s.device("phone", true, true, true, false, false, false)
	sha := qa3SHA(qa3Content32)
	for _, key := range []string{"short", strings.Repeat("a", 65), "0123456789abcdef 123456789abcdef", "0123456789abcdef0123456789abcde!"} {
		qa3WantStatus(t, "key "+key, s.openKeyed(tok, filepath.Join(s.root, "a.bin"), key, int64(len(qa3Content32)), sha), http.StatusBadRequest, "BAD_REQUEST")
	}
	if n := s.openCount(); n != 0 {
		t.Fatalf("a refused key created %d sessions", n)
	}
}
