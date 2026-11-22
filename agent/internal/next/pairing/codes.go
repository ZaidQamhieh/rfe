package pairing

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"time"

	"github.com/ZaidQamhieh/rfe/agent/internal/next/store"
)

// QRPayload is the JSON the QR code carries for the phone: where to connect,
// the certificate fingerprint to pin and the one-time code.
type QRPayload struct {
	Address          string `json:"address"`
	TailscaleAddress string `json:"tailscaleAddress,omitempty"`
	CertFingerprint  string `json:"certFingerprint"`
	PairingCode      string `json:"pairingCode"`
}

// MintedCode is a freshly minted one-time code. ExpiresInSeconds is the TTL
// actually applied (after the default and the cap), never the requested one.
type MintedCode struct {
	Code             string
	ExpiresInSeconds int64
	ExpiresAt        time.Time
	QR               QRPayload
}

// Mint makes an ordinary single-use code: it pairs a device, browse-only.
func (s *Service) Mint(ttl time.Duration) (MintedCode, error) {
	return s.mint(ttl, store.PairingCode{})
}

// MintOwner makes a single-use code that can also create the owner account.
func (s *Service) MintOwner(ttl time.Duration) (MintedCode, error) {
	return s.mint(ttl, store.PairingCode{CanRegister: true})
}

// MintGuest makes a single-use code whose device is read-only and jailed to
// jailRoot. An empty jailRoot is refused: a guest code must never be wider
// than asked for. The caller has validated jailRoot against the allowed roots.
func (s *Service) MintGuest(ttl time.Duration, jailRoot string) (MintedCode, error) {
	if jailRoot == "" {
		return MintedCode{}, badRequest("guest pairing codes require a jailRoot")
	}
	return s.mint(ttl, store.PairingCode{JailRoot: jailRoot, ReadOnly: true})
}

// clampTTL applies the default to a zero or negative TTL and the cap to a long one.
func clampTTL(ttl time.Duration) time.Duration {
	switch {
	case ttl <= 0:
		return DefaultCodeTTL
	case ttl > MaxCodeTTL:
		return MaxCodeTTL
	}
	return ttl
}

// randomCode draws CodeLength characters from the alphabet. The alphabet has
// 32 letters, a divisor of 256, so masking one random byte is unbiased.
func (s *Service) randomCode() (string, error) {
	b, err := s.randomBytes(CodeLength)
	if err != nil {
		return "", err
	}
	mask := byte(len(CodeAlphabet) - 1)
	for i := range b {
		b[i] = CodeAlphabet[b[i]&mask]
	}
	return string(b), nil
}

// mint stores a new code (as a hash) with the given limits. A hash collision
// with a live code is retried with a new code.
func (s *Service) mint(ttl time.Duration, tmpl store.PairingCode) (MintedCode, error) {
	ttl = clampTTL(ttl)
	now := s.o.Now()
	for i := 0; i < maxCodeMintAttempts; i++ {
		code, err := s.randomCode()
		if err != nil {
			return MintedCode{}, err
		}
		c := tmpl
		c.Hash, c.Expires = hashCode(code), now.Add(ttl).Unix()
		err = s.o.Store.AddPairingCode(c, now.Unix())
		if errors.Is(err, store.ErrCodeExists) {
			continue
		}
		if err != nil {
			return MintedCode{}, err
		}
		return MintedCode{
			Code: code, ExpiresInSeconds: int64(ttl / time.Second), ExpiresAt: time.Unix(c.Expires, 0),
			QR: QRPayload{Address: s.o.Address, TailscaleAddress: s.o.TailscaleAddress, CertFingerprint: s.o.Fingerprint, PairingCode: code},
		}, nil
	}
	return MintedCode{}, errors.New("pairing: could not mint a unique code")
}

// GenerateRequest is the optional body of POST /pairing/generate.
type GenerateRequest struct {
	TTLSeconds int64  `json:"ttlSeconds"`
	Guest      bool   `json:"guest"`
	JailRoot   string `json:"jailRoot"`
}

// ParseGenerateRequest reads the optional body. An empty body means the
// defaults; a body that is too large or does not decode is a BAD_REQUEST, so a
// request for a jailed guest code that is malformed can never fall back to an
// ordinary, unrestricted code.
func ParseGenerateRequest(r io.Reader) (GenerateRequest, error) {
	var req GenerateRequest
	raw, err := io.ReadAll(io.LimitReader(r, MaxGenerateBodyBytes+1))
	if err != nil {
		return req, badRequest("could not read the request body")
	}
	if len(raw) > MaxGenerateBodyBytes {
		return req, badRequest("request body too large")
	}
	if len(bytes.TrimSpace(raw)) == 0 {
		return req, nil
	}
	if err := json.Unmarshal(raw, &req); err != nil {
		return GenerateRequest{}, badRequest("invalid request body")
	}
	return req, nil
}

// Generate mints a code for an admin caller. validateJail checks and
// normalises a guest jail root against the allowed roots; a guest request with
// no validator, a refused root or an empty root is a BAD_REQUEST and mints
// nothing.
func (s *Service) Generate(by Actor, req GenerateRequest, validateJail func(root string) (string, error)) (MintedCode, error) {
	if !by.Admin {
		return MintedCode{}, ErrForbidden
	}
	var ttl time.Duration
	if req.TTLSeconds > int64(MaxCodeTTL/time.Second) {
		ttl = MaxCodeTTL // clamp before multiplying: no overflow for any int64
	} else if req.TTLSeconds > 0 {
		ttl = time.Duration(req.TTLSeconds) * time.Second
	}
	if !req.Guest {
		return s.Mint(ttl)
	}
	if validateJail == nil {
		return MintedCode{}, badRequest("guest pairing codes cannot be validated")
	}
	jail, err := validateJail(req.JailRoot)
	if err != nil {
		return MintedCode{}, badRequest(err.Error())
	}
	return s.MintGuest(ttl, jail)
}
