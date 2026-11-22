package api

import (
	"encoding/base64"
	"net/http"
	"time"

	"github.com/ZaidQamhieh/rfe/agent/internal/next/pairing"
)

// proofVersion is the device-proof version this agent accepts (openapi
// /auth/challenge: only the certificate-bound v2 proof).
const proofVersion = "v2"

// statusPending is what the reveal call answers once the owner has been asked.
const statusPending = "pending"

// ChallengeResponse is the body of POST /auth/challenge.
type ChallengeResponse struct {
	Nonce           string `json:"nonce"`
	Proof           string `json:"proof"`
	CertFingerprint string `json:"certFingerprint"`
}

// PairResponse is the body of a successful pair, register or login, and the
// approved answer of a poll (openapi PairResponse). The token is sent once.
type PairResponse struct {
	DeviceToken      string `json:"deviceToken,omitempty"`
	DeviceID         string `json:"deviceId"`
	AgentName        string `json:"agentName"`
	CertFingerprint  string `json:"certFingerprint"`
	Address          string `json:"address,omitempty"`
	TailscaleAddress string `json:"tailscaleAddress,omitempty"`
}

// PollResponse is the body of GET /pair/request/{id}: the state, plus the
// pairing details once approved.
type PollResponse struct {
	Status string `json:"status"`
	*PairResponse
}

// pairRequestBody is the JSON of POST /pair.
type pairRequestBody struct {
	PairingCode     string `json:"pairingCode"`
	DeviceLabel     string `json:"deviceLabel"`
	DeviceID        string `json:"deviceId"`
	DevicePublicKey string `json:"devicePublicKey"`
	Nonce           string `json:"nonce"`
	Signature       string `json:"signature"`
}

// registerBody is the JSON of POST /register.
type registerBody struct {
	pairRequestBody
	Username string `json:"username"`
	Password string `json:"password"`
}

// loginBody is the JSON of POST /login.
type loginBody struct {
	DeviceLabel     string `json:"deviceLabel"`
	DeviceID        string `json:"deviceId"`
	DevicePublicKey string `json:"devicePublicKey"`
	Nonce           string `json:"nonce"`
	Signature       string `json:"signature"`
	Username        string `json:"username"`
	Password        string `json:"password"`
}

// pairingRequestBody is the JSON of POST /pair/request.
type pairingRequestBody struct {
	DeviceLabel       string `json:"deviceLabel"`
	DeviceID          string `json:"deviceId"`
	DevicePublicKey   string `json:"devicePublicKey"`
	Nonce             string `json:"nonce"`
	Signature         string `json:"signature"`
	ClientNonceCommit string `json:"clientNonceCommit"`
}

// pairResponseOf converts the service result to the wire shape.
func pairResponseOf(res pairing.PairResult) PairResponse {
	return PairResponse{
		DeviceToken: res.DeviceToken, DeviceID: res.DeviceID, AgentName: res.AgentName,
		CertFingerprint: res.CertFingerprint, Address: res.Address, TailscaleAddress: res.TailscaleAddress,
	}
}

// challenge mints the single-use nonce a device signs before pairing or login.
func (s *Server) challenge(w http.ResponseWriter, r *http.Request) error {
	ch, err := s.cfg.Pairing.Challenge(remoteHost(r))
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, ChallengeResponse{Nonce: ch.Nonce, Proof: proofVersion, CertFingerprint: ch.CertFingerprint})
	return nil
}

// logout is the browser-session sign-out. Browser sessions are not part of this
// slice, and the route has no effect on bearer-token clients, so it only answers
// 204 as the contract says for a caller without a session.
func (s *Server) logout(w http.ResponseWriter, _ *http.Request) error {
	writeNoContent(w)
	return nil
}

// pair pairs a device with a one-time code and returns its token.
func (s *Server) pair(w http.ResponseWriter, r *http.Request) error {
	var b pairRequestBody
	if err := decodeJSON(r, &b); err != nil {
		return err
	}
	res, err := s.cfg.Pairing.PairWithCode(pairing.PairInput{
		Code: b.PairingCode, Label: b.DeviceLabel, DeviceID: b.DeviceID, PublicKey: b.DevicePublicKey,
		Nonce: b.Nonce, Signature: b.Signature, RemoteIP: remoteHost(r),
	})
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, pairResponseOf(res))
	return nil
}

// register creates the account and pairs the first device with an owner code.
func (s *Server) register(w http.ResponseWriter, r *http.Request) error {
	var b registerBody
	if err := decodeJSON(r, &b); err != nil {
		return err
	}
	res, err := s.cfg.Pairing.Register(pairing.RegisterInput{
		Code: b.PairingCode, Username: b.Username, Password: b.Password, Label: b.DeviceLabel,
		DeviceID: b.DeviceID, PublicKey: b.DevicePublicKey, Nonce: b.Nonce, Signature: b.Signature,
		RemoteIP: remoteHost(r),
	})
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, pairResponseOf(res))
	return nil
}

// login signs in with the account password and returns a device token.
func (s *Server) login(w http.ResponseWriter, r *http.Request) error {
	var b loginBody
	if err := decodeJSON(r, &b); err != nil {
		return err
	}
	res, err := s.cfg.Pairing.Login(pairing.LoginInput{
		Username: b.Username, Password: b.Password, Label: b.DeviceLabel, DeviceID: b.DeviceID,
		PublicKey: b.DevicePublicKey, Nonce: b.Nonce, Signature: b.Signature, RemoteIP: remoteHost(r),
	})
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, pairResponseOf(res))
	return nil
}

// pairRequest records an approve-on-PC pairing request and returns its id.
func (s *Server) pairRequest(w http.ResponseWriter, r *http.Request) error {
	var b pairingRequestBody
	if err := decodeJSON(r, &b); err != nil {
		return err
	}
	created, err := s.cfg.Pairing.CreateRequest(pairing.RequestInput{
		Label: b.DeviceLabel, DeviceID: b.DeviceID, PublicKey: b.DevicePublicKey, Nonce: b.Nonce,
		Signature: b.Signature, NonceCommit: b.ClientNonceCommit, RemoteIP: remoteHost(r),
	})
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, struct {
		RequestID        string `json:"requestId"`
		ExpiresInSeconds int64  `json:"expiresInSeconds"`
	}{created.RequestID, created.ExpiresInSeconds})
	return nil
}

// pairReveal takes the phone's secret nonce and asks the owner to approve.
func (s *Server) pairReveal(w http.ResponseWriter, r *http.Request) error {
	var b struct {
		ClientNonce string `json:"clientNonce"`
	}
	if err := decodeJSON(r, &b); err != nil {
		return err
	}
	if err := s.cfg.Pairing.Reveal(r.PathValue("id"), b.ClientNonce, remoteHost(r)); err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, struct {
		Status string `json:"status"`
	}{statusPending})
	return nil
}

// pairPoll reports the owner's answer to a request. The nonce proving the poller
// is the phone that asked comes from the X-RFE-Pair-Nonce header or, for older
// clients, the nonce query parameter (protocol/compat.md C3).
func (s *Server) pairPoll(w http.ResponseWriter, r *http.Request) error {
	nonce := pairing.PollNonce(r.Header.Get(pairing.PollNonceHeader), r.URL.Query().Get(queryNonce))
	res, err := s.cfg.Pairing.Poll(r.PathValue("id"), nonce, remoteHost(r))
	if err != nil {
		return err
	}
	out := PollResponse{Status: res.Status}
	if res.Pair != nil {
		pr := pairResponseOf(*res.Pair)
		out.PairResponse = &pr
	}
	writeJSON(w, http.StatusOK, out)
	return nil
}

// pendingJSON is one waiting request in the admin list.
type pendingJSON struct {
	ID        string `json:"id"`
	Label     string `json:"label"`
	MatchCode string `json:"matchCode"`
	RemoteIP  string `json:"remoteIp"`
	Replaces  string `json:"replaces"`
	ExpiresAt string `json:"expiresAt"`
}

// pairPending lists the requests waiting for the owner (admin only).
func (s *Server) pairPending(w http.ResponseWriter, r *http.Request) error {
	c, err := callOf(r)
	if err != nil {
		return err
	}
	list, err := s.cfg.Pairing.ListPending(pairing.ActorOf(c.device))
	if err != nil {
		return err
	}
	out := make([]pendingJSON, 0, len(list))
	for _, p := range list {
		out = append(out, pendingJSON{
			ID: p.ID, Label: p.Label, MatchCode: p.MatchCode, RemoteIP: p.RemoteIP,
			Replaces: p.Replaces, ExpiresAt: p.ExpiresAt.UTC().Format(time.RFC3339),
		})
	}
	writeJSON(w, http.StatusOK, struct {
		Requests []pendingJSON `json:"requests"`
	}{out})
	return nil
}

// pairApprove approves a waiting request.
func (s *Server) pairApprove(w http.ResponseWriter, r *http.Request) error {
	return s.decide(w, r, true)
}

// pairReject rejects a waiting request.
func (s *Server) pairReject(w http.ResponseWriter, r *http.Request) error {
	return s.decide(w, r, false)
}

// decide records the admin's answer to a waiting request and replies 204.
func (s *Server) decide(w http.ResponseWriter, r *http.Request, approve bool) error {
	c, err := callOf(r)
	if err != nil {
		return err
	}
	if err := s.cfg.Pairing.Decide(pairing.ActorOf(c.device), r.PathValue("id"), approve); err != nil {
		return err
	}
	writeNoContent(w)
	return nil
}

// pairingGenerate mints a one-time code for an admin and returns it with its QR
// payload and a base64 PNG of that payload's QR code. A guest code's jail folder
// is checked by ValidateJail; its error text is replaced by a fixed one so no
// host path reaches the client.
func (s *Server) pairingGenerate(w http.ResponseWriter, r *http.Request) error {
	c, err := callOf(r)
	if err != nil {
		return err
	}
	req, err := pairing.ParseGenerateRequest(r.Body)
	if err != nil {
		return err
	}
	minted, err := s.cfg.Pairing.Generate(pairing.ActorOf(c.device), req, s.validateJail)
	if err != nil {
		return err
	}
	// A QR that cannot be drawn is not an error: the text code still pairs.
	var qrPNG string
	if png, qrErr := pairing.QRPNG(minted.QR); qrErr == nil {
		qrPNG = base64.StdEncoding.EncodeToString(png)
	} else {
		s.logf("api: pairing: QR encode failed (the code is still valid): %v", qrErr)
	}
	writeJSON(w, http.StatusOK, struct {
		PairingCode      string            `json:"pairingCode"`
		ExpiresInSeconds int64             `json:"expiresInSeconds"`
		QRPayload        pairing.QRPayload `json:"qrPayload"`
		QRPngBase64      string            `json:"qrPngBase64,omitempty"`
	}{minted.Code, minted.ExpiresInSeconds, minted.QR, qrPNG})
	return nil
}
