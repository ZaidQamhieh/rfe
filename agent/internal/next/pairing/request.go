package pairing

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ZaidQamhieh/rfe/agent/internal/next/audit"
	"github.com/ZaidQamhieh/rfe/agent/internal/next/store"
)

// PollNonceHeader is the request header that carries the poll nonce. During the
// transition (protocol/compat.md C3) the nonce is also accepted as the `nonce`
// query parameter; PollNonce picks between them.
const PollNonceHeader = "X-RFE-Pair-Nonce"

// PollNonce returns the nonce a poll request carries: the header when it is
// set, otherwise the query parameter. Both forms are accepted until older
// clients are gone.
func PollNonce(header, query string) string {
	if header != "" {
		return header
	}
	return query
}

// Prompt is what the owner is asked to approve at the PC.
type Prompt struct {
	ID    string
	Label string
	SAS   string // short match code, "1234 5678"
	IP    string
	TTL   time.Duration
	// Replaces names the already paired device that approving this would take
	// over (new key, new token, access reset to browse-only); empty for a new phone.
	Replaces string
}

// matchCodeDigits and matchCodeGroup shape the match code: 8 decimal digits in
// two groups of 4.
const (
	matchCodeModulus = 100_000_000
	matchCodeGroup   = 10_000
	matchCodeLabel   = "rfe-pair-sas\n"
)

// SAS derives the short match code both sides show during a pair request, so
// the owner can tell the phone reached this agent and not a relay: it is keyed
// by the certificate fingerprint the phone actually saw, and the phone's nonce
// was fixed (committed) before the request id existed. Same output as the old
// agent's pairing.SAS.
func SAS(fingerprintHex, clientNonceHex, requestID string) (string, error) {
	fp, err := hex.DecodeString(fingerprintHex)
	if err != nil || len(fp) != sha256.Size {
		return "", errors.New("bad fingerprint")
	}
	nonce, err := hex.DecodeString(clientNonceHex)
	if err != nil || len(nonce) != clientNonceBytes {
		return "", errors.New("bad client nonce")
	}
	mac := hmac.New(sha256.New, fp)
	mac.Write([]byte(matchCodeLabel))
	mac.Write(nonce)
	mac.Write([]byte(requestID))
	n := binary.BigEndian.Uint64(mac.Sum(nil)[:8]) % matchCodeModulus
	return fmt.Sprintf("%04d %04d", n/matchCodeGroup, n%matchCodeGroup), nil
}

// normHex lowercases a hex string and reports whether it decodes to wantBytes
// bytes. Hex is case-insensitive, so the lowercase form is the one stored and
// compared; a commitment or nonce that differs only in case is the same value.
func normHex(s string, wantBytes int) (string, bool) {
	b, err := hex.DecodeString(s)
	if err != nil || len(b) != wantBytes {
		return "", false
	}
	return hex.EncodeToString(b), true
}

// equalSecret compares two secrets in constant time.
func equalSecret(a, b string) bool {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

// pairRequestLimits are the caps on waiting requests.
func pairRequestLimits() store.PairRequestLimits {
	return store.PairRequestLimits{
		MaxPending: pairRequestMaxPending, MaxCommittedPerIP: pairRequestMaxPerIP, MaxCommitted: pairRequestMaxCommitted,
		RetentionSecs: int64(pairRequestRetention / time.Second),
	}
}

// RequestInput is a phone asking to be paired. NonceCommit is the hex SHA-256
// of the 16-byte nonce the phone will reveal once it knows the request id.
type RequestInput struct {
	Label       string
	DeviceID    string
	PublicKey   string
	Nonce       string
	Signature   string
	NonceCommit string
	RemoteIP    string
}

// RequestCreated is the answer to a new request.
type RequestCreated struct {
	RequestID        string
	ExpiresInSeconds int64
}

// CreateRequest records a pair request after the device proof checked out. The
// owner is not asked yet: the phone must first reveal its nonce.
func (s *Service) CreateRequest(in RequestInput) (RequestCreated, error) {
	if !s.requestLim.Allow(in.RemoteIP) {
		return RequestCreated{}, ErrRateLimited
	}
	commit, ok := normHex(in.NonceCommit, sha256.Size)
	if !ok {
		return RequestCreated{}, badRequest("clientNonceCommit (hex SHA-256 of the client nonce) is required - update the app")
	}
	if err := s.verifyProof(Proof{DeviceID: in.DeviceID, PublicKey: in.PublicKey, Nonce: in.Nonce, Signature: in.Signature}, rePinOnKeyChange); err != nil {
		return RequestCreated{}, err
	}
	id, err := s.randomHex(requestIDBytes)
	if err != nil {
		return RequestCreated{}, err
	}
	now := s.o.Now()
	err = s.o.Store.CreatePairRequest(store.PairRequest{
		ID: id, Label: sanitizeLabel(in.Label), ClientID: in.DeviceID, PublicKey: in.PublicKey,
		NonceCommit: commit, RemoteIP: in.RemoteIP, Created: now.Unix(), Expires: now.Add(PairRequestTTL).Unix(),
	}, now.Unix(), pairRequestLimits())
	if errors.Is(err, store.ErrTooManyPairRequests) {
		return RequestCreated{}, ErrPairBusy
	}
	if err != nil {
		return RequestCreated{}, err
	}
	return RequestCreated{RequestID: id, ExpiresInSeconds: int64(PairRequestTTL / time.Second)}, nil
}

// replaces names the device an approval for clientID with publicKey would take
// over (a different pinned key, or a revoked row), or "" when it is a new phone
// or one re-pairing with its own key.
func (s *Service) replaces(clientID, publicKey string) (string, error) {
	d, err := s.o.Store.DeviceByClientID(clientID)
	if err != nil || d == nil {
		return "", err
	}
	if d.Revoked || (d.PublicKey != "" && d.PublicKey != publicKey) {
		return d.Label, nil
	}
	return "", nil
}

// Reveal takes the phone's nonce, checks it against the commitment sent first,
// derives the match code and asks the owner (Options.OnPairRequest). An unknown
// id, a wrong nonce and a request that is not waiting for its reveal all look
// the same, so ids cannot be probed.
func (s *Service) Reveal(id, clientNonce, remote string) error {
	if !s.revealLim.Allow(remote) {
		return ErrRateLimited
	}
	nonce, ok := normHex(clientNonce, clientNonceBytes)
	if !ok {
		return badRequest("clientNonce must be 16 random bytes, hex encoded")
	}
	pr, found, err := s.o.Store.GetPairRequest(id, s.unixNow())
	if err != nil {
		return err
	}
	raw, _ := hex.DecodeString(nonce)
	sum := sha256.Sum256(raw)
	if !found || pr.Status != store.PairCommitted || !equalSecret(hex.EncodeToString(sum[:]), strings.ToLower(pr.NonceCommit)) {
		return ErrNotFound
	}
	sas, err := SAS(s.o.Fingerprint, nonce, id)
	if err != nil {
		return err
	}
	replaces, err := s.replaces(pr.ClientID, pr.PublicKey)
	if err != nil {
		return err
	}
	pr, err = s.o.Store.RevealPairRequest(id, nonce, s.unixNow(), pairRequestMaxPending)
	switch {
	case errors.Is(err, store.ErrPairRequestGone):
		return ErrNotFound
	case errors.Is(err, store.ErrTooManyPairRequests):
		return ErrPairBusy
	case err != nil:
		return err
	}
	if s.o.OnPairRequest != nil {
		s.o.OnPairRequest(Prompt{ID: id, Label: pr.Label, SAS: sas, IP: remote, TTL: time.Unix(pr.Expires, 0).Sub(s.o.Now()), Replaces: replaces})
	}
	return nil
}

// Poll statuses.
const (
	StatusPending  = store.PairPending
	StatusRejected = store.PairRejected
	StatusApproved = store.PairApproved
)

// PollResult is the answer to a poll. Pair is set only with StatusApproved and
// carries the token, which is handed out once.
type PollResult struct {
	Status string
	Pair   *PairResult
}

// Poll answers the phone's poll for a request. The nonce (see PollNonce) must
// match the one revealed; otherwise, like for an unknown id, the answer is
// NOT_FOUND. When the owner approved, the request is collected exactly once:
// the token is minted and the device written in one transaction.
func (s *Service) Poll(id, nonce, remote string) (PollResult, error) {
	if !s.pollLim.Allow(sourceKey(remote)) {
		return PollResult{}, ErrRateLimited
	}
	pr, found, err := s.o.Store.GetPairRequest(id, s.unixNow())
	if err != nil {
		return PollResult{}, err
	}
	if !found || pr.ClientNonce == "" || !equalSecret(pr.ClientNonce, strings.ToLower(nonce)) {
		return PollResult{}, ErrNotFound
	}
	switch pr.Status {
	case store.PairPending:
		return PollResult{Status: StatusPending}, nil
	case store.PairRejected:
		return PollResult{Status: StatusRejected}, nil
	case store.PairApproved:
		return s.collect(id, remote)
	}
	return PollResult{}, ErrNotFound
}

// collect turns an approved request into a device and its token.
func (s *Service) collect(id, remote string) (PollResult, error) {
	deviceID, token, err := s.newCredentials()
	if err != nil {
		return PollResult{}, err
	}
	pr, saved, ok, err := s.o.Store.CompleteApprovedPairRequest(id, store.DeviceSpec{
		ID: deviceID, TokenHash: store.HashToken(token), Now: s.unixNow(),
	})
	if err != nil {
		return PollResult{}, err
	}
	if !ok {
		return PollResult{}, ErrNotFound
	}
	detail := "approved at the computer, from " + pr.RemoteIP
	if saved.Replaced {
		detail += replacedNote
	}
	s.audit(audit.ActionPair, pr.Label, saved.ID, detail)
	res := s.result(saved.ID, token)
	return PollResult{Status: StatusApproved, Pair: &res}, nil
}

// PendingRequest is one request waiting for the owner, as the admin list shows it.
type PendingRequest struct {
	ID        string
	Label     string
	MatchCode string
	RemoteIP  string
	Replaces  string
	ExpiresAt time.Time
}

// ListPending returns the requests waiting for an answer, oldest first. Only an
// admin may see them.
func (s *Service) ListPending(by Actor) ([]PendingRequest, error) {
	if !by.Admin {
		return nil, ErrForbidden
	}
	list, err := s.o.Store.ListPendingPairRequests(s.unixNow())
	if err != nil {
		return nil, err
	}
	out := make([]PendingRequest, 0, len(list))
	for _, pr := range list {
		sas, err := SAS(s.o.Fingerprint, pr.ClientNonce, pr.ID)
		if err != nil {
			return nil, err
		}
		rep, err := s.replaces(pr.ClientID, pr.PublicKey)
		if err != nil {
			return nil, err
		}
		out = append(out, PendingRequest{ID: pr.ID, Label: pr.Label, MatchCode: sas, RemoteIP: pr.RemoteIP, Replaces: rep, ExpiresAt: time.Unix(pr.Expires, 0).UTC()})
	}
	return out, nil
}

// Decide approves or rejects a waiting request. Only an admin (a login session
// or the PC) may; a request that is gone, expired or already answered is NOT_FOUND.
func (s *Service) Decide(by Actor, id string, approve bool) error {
	if !by.Admin {
		return ErrForbidden
	}
	err := s.o.Store.DecidePairRequest(id, approve, s.unixNow())
	if errors.Is(err, store.ErrPairRequestGone) {
		return &Error{Code: ErrNotFound.Code, Status: ErrNotFound.Status, Message: "pairing request not found, expired or already answered"}
	}
	return err
}
