package pairing

import (
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/ZaidQamhieh/rfe/agent/internal/next/audit"
	"github.com/ZaidQamhieh/rfe/agent/internal/next/store"
)

// PairInput is a pairing by one-time code.
type PairInput struct {
	Code      string
	Label     string
	DeviceID  string // the phone's hardware-stable client id; may be empty
	PublicKey string
	Nonce     string
	Signature string
	RemoteIP  string
}

// proof is the device-identity proof carried by the input.
func (in PairInput) proof() Proof {
	return Proof{DeviceID: in.DeviceID, PublicKey: in.PublicKey, Nonce: in.Nonce, Signature: in.Signature}
}

// replacedNote is the audit detail added when a pairing took over an existing
// device row, which then starts again from browse-only.
const replacedNote = "; replaced an existing device (access reset to browse-only)"

// PairWithCode pairs a device with a one-time code. The code is spent, the
// device written and its guest limits applied in one transaction, and only
// after the device proof checked out; a failure at any step leaves the code
// usable. The new device is browse-only. The token in the result is the only
// copy.
func (s *Service) PairWithCode(in PairInput) (PairResult, error) {
	if !s.pairLim.Allow(in.RemoteIP) {
		return PairResult{}, ErrRateLimited
	}
	if err := s.verifyProof(in.proof(), rePinOnKeyChange); err != nil {
		return PairResult{}, err
	}
	if in.Code == "" || len(in.Code) > maxCodeInputBytes {
		return PairResult{}, ErrInvalidCode
	}
	id, token, err := s.newCredentials()
	if err != nil {
		return PairResult{}, err
	}
	label := sanitizeLabel(in.Label)
	_, saved, err := s.o.Store.PairWithCode(hashCode(in.Code), store.DeviceSpec{
		ID: id, ClientID: in.DeviceID, Label: label, TokenHash: store.HashToken(token),
		PublicKey: in.PublicKey, Origin: store.OriginCode, Now: s.unixNow(),
	})
	if errors.Is(err, store.ErrCodeInvalid) {
		return PairResult{}, ErrInvalidCode
	}
	if err != nil {
		return PairResult{}, err
	}
	detail := "from " + in.RemoteIP
	if saved.Replaced {
		detail += replacedNote
	}
	s.audit(audit.ActionPair, label, saved.ID, detail)
	return s.result(saved.ID, token), nil
}

// RegisterInput is the creation of the owner account together with its first
// device, authorised by an owner code.
type RegisterInput struct {
	Code      string
	Username  string
	Password  string
	Label     string
	DeviceID  string
	PublicKey string
	Nonce     string
	Signature string
	RemoteIP  string
}

// Register creates the computer's single account and the registering device
// with an owner code. Cheap checks and the device proof come first and an
// ordinary code is refused before anything is spent; the account, device and
// code spend then happen in one transaction, so no failure burns the code. The
// device is an account device: it bypasses the file grants.
func (s *Service) Register(in RegisterInput) (PairResult, error) {
	if !s.registerLim.Allow(in.RemoteIP) {
		return PairResult{}, ErrRateLimited
	}
	if in.Username == "" || len(in.Username) > maxUsernameBytes ||
		utf8.RuneCountInString(in.Password) < MinPasswordRunes || len(in.Password) > s.o.Passwords.MaxBytes {
		return PairResult{}, badRequest(fmt.Sprintf("username required and password must be %d characters to %d bytes", MinPasswordRunes, s.o.Passwords.MaxBytes))
	}
	has, err := s.o.Store.HasAnyUser()
	if err != nil {
		return PairResult{}, err
	}
	if has {
		return PairResult{}, ErrAccountExists
	}
	if err := s.verifyProof(Proof{DeviceID: in.DeviceID, PublicKey: in.PublicKey, Nonce: in.Nonce, Signature: in.Signature}, rePinOnKeyChange); err != nil {
		return PairResult{}, err
	}
	if in.Code == "" || len(in.Code) > maxCodeInputBytes {
		return PairResult{}, ErrInvalidCode
	}
	codeHash := hashCode(in.Code)
	// Peek first: an ordinary code must not be spent by an attempt it can never
	// satisfy, and the costly password hash waits for a code that can.
	peek, err := s.o.Store.PeekPairingCode(codeHash, s.unixNow())
	if errors.Is(err, store.ErrCodeInvalid) {
		return PairResult{}, ErrInvalidCode
	}
	if err != nil {
		return PairResult{}, err
	}
	if !peek.CanRegister {
		return PairResult{}, ErrCodeCannotRegister
	}
	hash, err := s.o.Passwords.Hash(in.Password)
	if err != nil {
		return PairResult{}, err
	}
	id, token, err := s.newCredentials()
	if err != nil {
		return PairResult{}, err
	}
	label := sanitizeLabel(in.Label)
	saved, err := s.o.Store.RegisterWithCode(codeHash, store.DeviceSpec{
		ID: id, ClientID: in.DeviceID, Label: label, TokenHash: store.HashToken(token),
		PublicKey: in.PublicKey, Origin: store.OriginRegister, Now: s.unixNow(),
	}, store.User{Username: in.Username, PasswordHash: hash})
	switch {
	case errors.Is(err, store.ErrCodeInvalid):
		return PairResult{}, ErrInvalidCode
	case errors.Is(err, store.ErrCodeCannotRegister):
		return PairResult{}, ErrCodeCannotRegister
	case errors.Is(err, store.ErrAccountExists):
		return PairResult{}, ErrAccountExists
	case errors.Is(err, store.ErrUsernameTaken):
		return PairResult{}, ErrUsernameTaken
	case err != nil:
		return PairResult{}, err
	}
	s.audit(audit.ActionRegister, in.Username, label, "from "+in.RemoteIP)
	return s.result(saved.ID, token), nil
}

// LoginInput is a sign-in with the account password.
type LoginInput struct {
	Username  string
	Password  string
	Label     string
	DeviceID  string
	PublicKey string
	Nonce     string
	Signature string
	RemoteIP  string
}

// Login signs in with the account password and returns a device token for an
// account device (file grants bypassed). Per source and per account (counting
// failures from any address) it is rate limited; a missing account and a wrong
// password look the same and cost the same. A changed key under a known device
// id is refused (re-pair instead).
func (s *Service) Login(in LoginInput) (PairResult, error) {
	if !s.loginLim.Allow(sourceKey(in.RemoteIP)) {
		return PairResult{}, ErrRateLimited
	}
	if in.Username == "" || in.Password == "" || len(in.Username) > maxUsernameBytes {
		return PairResult{}, badRequest("username and password required")
	}
	account := strings.ToLower(in.Username)
	if s.accountFails.Exhausted(account) {
		return PairResult{}, ErrRateLimited
	}
	user, err := s.o.Store.UserByUsername(in.Username)
	if err != nil {
		return PairResult{}, err
	}
	ok := false
	switch {
	case user == nil:
		s.o.Passwords.Burn(in.Password)
	case len(in.Password) <= s.o.Passwords.MaxBytes:
		ok = s.o.Passwords.Verify(user.PasswordHash, in.Password)
	}
	if !ok {
		s.accountFails.Allow(account)
		s.audit(audit.ActionLoginFailed, in.Username, "", "from "+in.RemoteIP)
		return PairResult{}, ErrInvalidCredentials
	}
	if err := s.verifyProof(Proof{DeviceID: in.DeviceID, PublicKey: in.PublicKey, Nonce: in.Nonce, Signature: in.Signature}, rejectOnKeyChange); err != nil {
		return PairResult{}, err
	}
	id, token, err := s.newCredentials()
	if err != nil {
		return PairResult{}, err
	}
	label := sanitizeLabel(in.Label)
	saved, err := s.o.Store.LoginDevice(store.DeviceSpec{
		ID: id, ClientID: in.DeviceID, Label: label, TokenHash: store.HashToken(token),
		PublicKey: in.PublicKey, Origin: store.OriginLogin, Username: user.Username, Now: s.unixNow(),
	}, user.PasswordHash)
	if errors.Is(err, store.ErrLoginAccountChanged) {
		s.audit(audit.ActionLoginFailed, in.Username, label, "account changed during login")
		return PairResult{}, ErrInvalidCredentials
	}
	if err != nil {
		return PairResult{}, err
	}
	s.audit(audit.ActionLogin, in.Username, label, "from "+in.RemoteIP)
	return s.result(saved.ID, token), nil
}
