package api_test

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/url"
	"testing"

	"github.com/ZaidQamhieh/rfe/agent/internal/next/api"
	"github.com/ZaidQamhieh/rfe/agent/internal/next/authz"
	"github.com/ZaidQamhieh/rfe/agent/internal/next/pairing"
)

// clientNonce is the 16-byte secret a phone reveals after its request exists.
const clientNonce = "00112233445566778899aabbccddeeff"

// commitOf is the commitment (hex SHA-256) of a hex nonce.
func commitOf(t *testing.T, nonceHex string) string {
	t.Helper()
	raw, err := hex.DecodeString(nonceHex)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func TestApproveOnPCFlowOverHTTP(t *testing.T) {
	r := newRig(t, rigOptions{})
	adminTok, _ := r.registerAdmin(newPhone("admin-1", 1))
	p := newPhone("phone-1", 2)

	nonce, sig := r.proof(p)
	res := r.do("POST", "/pair/request", "", map[string]any{
		"deviceLabel": "tablet", "deviceId": p.id, "devicePublicKey": p.pub, "nonce": nonce, "signature": sig,
		"clientNonceCommit": commitOf(t, clientNonce),
	})
	var created struct {
		RequestID        string `json:"requestId"`
		ExpiresInSeconds int64  `json:"expiresInSeconds"`
	}
	res.json(t, &created)
	if res.Code != 200 || created.RequestID == "" || created.ExpiresInSeconds <= 0 {
		t.Fatalf("create = %d %s", res.Code, res.Body.String())
	}
	reqPath := "/pair/request/" + created.RequestID
	if len(r.prompts) != 0 {
		t.Fatal("the owner was asked before the reveal")
	}

	// A reveal with the wrong nonce looks like an unknown request.
	r.do("POST", reqPath+"/reveal", "", map[string]any{"clientNonce": "ffffffffffffffffffffffffffffffff"}).wantError(t, http.StatusNotFound, "NOT_FOUND")
	if res := r.do("POST", reqPath+"/reveal", "", map[string]any{"clientNonce": clientNonce}); res.Code != 200 || res.Body.String() != "{\"status\":\"pending\"}\n" {
		t.Fatalf("reveal = %d %s", res.Code, res.Body.String())
	}
	if len(r.prompts) != 1 || r.prompts[0].ID != created.RequestID {
		t.Fatalf("prompts = %+v", r.prompts)
	}

	// Polling needs the nonce: wrong or absent is NOT_FOUND, the header and the
	// query form are both accepted (compat.md C3).
	r.do("GET", reqPath, "", nil).wantError(t, http.StatusNotFound, "NOT_FOUND")
	r.do("GET", reqPath, "", nil, withHeader(pairing.PollNonceHeader, "ffffffffffffffffffffffffffffffff")).wantError(t, http.StatusNotFound, "NOT_FOUND")
	var poll api.PollResponse
	r.do("GET", reqPath, "", nil, withHeader(pairing.PollNonceHeader, clientNonce)).json(t, &poll)
	if poll.Status != pairing.StatusPending || poll.PairResponse != nil {
		t.Fatalf("poll before the decision = %+v", poll)
	}

	// Only an admin sees and answers the waiting request.
	other, _ := r.pairDevice(newPhone("phone-2", 3))
	r.do("GET", "/pair/requests", other, nil).wantError(t, http.StatusForbidden, authz.CodeForbidden)
	r.do("POST", "/pair/requests/"+created.RequestID+"/approve", other, nil).wantError(t, http.StatusForbidden, authz.CodeForbidden)
	var list struct {
		Requests []struct {
			ID, Label, MatchCode, RemoteIP, Replaces, ExpiresAt string
		} `json:"requests"`
	}
	r.do("GET", "/pair/requests", adminTok, nil).json(t, &list)
	if len(list.Requests) != 1 || list.Requests[0].ID != created.RequestID || list.Requests[0].MatchCode != r.prompts[0].SAS || list.Requests[0].ExpiresAt == "" {
		t.Fatalf("pending list = %+v (prompt SAS %q)", list, r.prompts[0].SAS)
	}
	if res := r.do("POST", "/pair/requests/"+created.RequestID+"/approve", adminTok, nil); res.Code != http.StatusNoContent {
		t.Fatalf("approve = %d %s", res.Code, res.Body.String())
	}
	r.do("POST", "/pair/requests/"+created.RequestID+"/reject", adminTok, nil).wantError(t, http.StatusNotFound, "NOT_FOUND")

	// The token is handed out once; this time the nonce comes in the query.
	res = r.do("GET", reqPath+"?nonce="+url.QueryEscape(clientNonce), "", nil)
	res.json(t, &poll)
	if res.Code != 200 || poll.Status != pairing.StatusApproved || poll.PairResponse == nil || poll.DeviceToken == "" ||
		poll.CertFingerprint != testFingerprint || poll.AgentName != testName || poll.Address != testAddress {
		t.Fatalf("approved poll = %d %s", res.Code, res.Body.String())
	}
	r.do("GET", reqPath+"?nonce="+clientNonce, "", nil).wantError(t, http.StatusNotFound, "NOT_FOUND")

	// The new device is browse-only: it can read its own status, not use apps.
	if res := r.do("GET", "/status", poll.DeviceToken, nil); res.Code != 200 {
		t.Fatalf("status with the new token = %d %s", res.Code, res.Body.String())
	}
	r.do("GET", "/apps", poll.DeviceToken, nil).wantError(t, http.StatusForbidden, authz.CodeCapabilityDenied)
}

func TestRejectedRequestReportsRejected(t *testing.T) {
	r := newRig(t, rigOptions{})
	adminTok, _ := r.registerAdmin(newPhone("admin-1", 1))
	p := newPhone("phone-1", 2)
	nonce, sig := r.proof(p)
	var created struct {
		RequestID string `json:"requestId"`
	}
	r.do("POST", "/pair/request", "", map[string]any{
		"deviceLabel": "tablet", "deviceId": p.id, "devicePublicKey": p.pub, "nonce": nonce, "signature": sig,
		"clientNonceCommit": commitOf(t, clientNonce),
	}).json(t, &created)
	r.do("POST", "/pair/request/"+created.RequestID+"/reveal", "", map[string]any{"clientNonce": clientNonce})
	if res := r.do("POST", "/pair/requests/"+created.RequestID+"/reject", adminTok, nil); res.Code != http.StatusNoContent {
		t.Fatalf("reject = %d %s", res.Code, res.Body.String())
	}
	var poll api.PollResponse
	r.do("GET", "/pair/request/"+created.RequestID, "", nil, withHeader(pairing.PollNonceHeader, clientNonce)).json(t, &poll)
	if poll.Status != pairing.StatusRejected || poll.PairResponse != nil {
		t.Fatalf("poll after reject = %+v", poll)
	}
}

func TestPairLoginAndRegisterErrorsKeepTheirCodes(t *testing.T) {
	r := newRig(t, rigOptions{})
	p := newPhone("phone-1", 2)

	// Missing proof, wrong code, a replayed nonce.
	r.do("POST", "/pair", "", map[string]any{"pairingCode": "ABCDEFGH", "deviceLabel": "x"}).wantError(t, http.StatusBadRequest, "DEVICE_KEY_REQUIRED")
	r.do("POST", "/pair", "", r.pairBody(p, "ABCDEFGH")).wantError(t, http.StatusUnauthorized, "INVALID_CODE")
	body := r.pairBody(p, "ABCDEFGH")
	body["signature"] = r.pairBody(newPhone("other", 9), "x")["signature"] // a signature made by another key
	r.do("POST", "/pair", "", body).wantError(t, http.StatusUnauthorized, "INVALID_SIGNATURE")

	// An ordinary code cannot create the account; the owner code can, once.
	m, err := r.svc.Mint(0)
	if err != nil {
		t.Fatal(err)
	}
	reg := r.pairBody(p, m.Code)
	reg["username"], reg["password"] = "owner", testPassword
	r.do("POST", "/register", "", reg).wantError(t, http.StatusForbidden, "CODE_CANNOT_REGISTER")
	adminTok, _ := r.registerAdmin(p)
	if adminTok == "" {
		t.Fatal("no admin token")
	}
	m2, err := r.svc.MintOwner(0)
	if err != nil {
		t.Fatal(err)
	}
	again := r.pairBody(newPhone("phone-9", 9), m2.Code)
	again["username"], again["password"] = "second", testPassword
	r.do("POST", "/register", "", again).wantError(t, http.StatusConflict, "ACCOUNT_ALREADY_EXISTS")

	// Login: wrong password and unknown user look the same; the right one issues a token.
	login := func(user, pass string) result {
		b := r.pairBody(p, "")
		delete(b, "pairingCode")
		b["username"], b["password"] = user, pass
		return r.do("POST", "/login", "", b)
	}
	login("owner", "wrong password").wantError(t, http.StatusUnauthorized, "INVALID_CREDENTIALS")
	login("nobody", testPassword).wantError(t, http.StatusUnauthorized, "INVALID_CREDENTIALS")
	res := login("owner", testPassword)
	var pr api.PairResponse
	res.json(t, &pr)
	if res.Code != 200 || pr.DeviceToken == "" {
		t.Fatalf("login = %d %s", res.Code, res.Body.String())
	}
	if res := r.do("GET", "/metrics", pr.DeviceToken, nil); res.Code != 200 {
		t.Fatalf("a login device must be admin: %d %s", res.Code, res.Body.String())
	}
}
