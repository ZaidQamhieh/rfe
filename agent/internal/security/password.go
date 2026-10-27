package security

import (
	"errors"
	"sync"

	"golang.org/x/crypto/bcrypt"
)

// MaxPasswordBytes is bcrypt's input limit. Longer passwords are refused rather
// than truncated (two passwords sharing a 72-byte prefix must not match) or
// turned into an opaque server error.
const MaxPasswordBytes = 72

// ErrPasswordTooLong means the password exceeds MaxPasswordBytes.
var ErrPasswordTooLong = errors.New("password is longer than 72 bytes")

// HashPassword salts and hashes a user-chosen password with bcrypt — unlike
// the SHA-256 used for high-entropy bearer tokens elsewhere in this package,
// a human password needs a deliberately slow, salted hash to resist offline
// brute force.
func HashPassword(password string) (string, error) {
	if len(password) > MaxPasswordBytes {
		return "", ErrPasswordTooLong
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	return string(hash), err
}

// VerifyPassword reports whether password matches hash (as produced by
// HashPassword).
func VerifyPassword(hash, password string) bool {
	if len(password) > MaxPasswordBytes {
		return false
	}
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
}

var decoyHash = sync.OnceValue(func() string {
	h, err := HashPassword("decoy-password-for-timing")
	if err != nil {
		return ""
	}
	return h
})

// BurnPasswordCheck spends the same bcrypt time as a real VerifyPassword. Call
// it when the account does not exist so response time does not reveal which
// usernames are real.
func BurnPasswordCheck(password string) {
	_ = VerifyPassword(decoyHash(), password)
}
