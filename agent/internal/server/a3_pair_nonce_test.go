package server

import (
	"crypto/ed25519"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
)

// pollWith polls request id with the nonce given as a query value and/or as the X-RFE-Pair-Nonce header.
func (e *pairReqEnv) pollWith(id, queryNonce, headerNonce string) (int, map[string]any) {
	e.t.Helper()
	target := "/v1/pair/request/" + id
	if queryNonce != "" {
		target += "?nonce=" + queryNonce
	}
	req := httptest.NewRequest(http.MethodGet, target, nil)
	if headerNonce != "" {
		req.Header.Set("X-RFE-Pair-Nonce", headerNonce)
	}
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", id)
	req = req.WithContext(withChiCtx(req, rctx))
	rr := httptest.NewRecorder()
	e.poll(rr, req)
	var out map[string]any
	_ = json.Unmarshal(rr.Body.Bytes(), &out)
	return rr.Code, out
}

func TestPairRequest_PollAcceptsNonceHeaderAndPrefersIt(t *testing.T) {
	e := newPairReqEnv(t)
	pub, priv, _ := ed25519.GenerateKey(nil)
	id := e.requestWith("phone-h", pub, priv)
	const wrong = "ffffffffffffffffffffffffffffffff"
	if code, out := e.pollWith(id, "", testCNonc); code != 200 || out["status"] != "pending" {
		t.Fatalf("header-only poll: %d %v", code, out)
	}
	if code, out := e.pollWith(id, testCNonc, ""); code != 200 || out["status"] != "pending" {
		t.Fatalf("query-only poll must keep working: %d %v", code, out)
	}
	// The header wins over a wrong query value, and a wrong header is not rescued by a right query value.
	if code, _ := e.pollWith(id, wrong, testCNonc); code != 200 {
		t.Fatalf("header must be preferred over the query, got %d", code)
	}
	if code, _ := e.pollWith(id, testCNonc, wrong); code != http.StatusNotFound {
		t.Fatalf("a wrong header must be refused even with a right query, got %d", code)
	}
}
