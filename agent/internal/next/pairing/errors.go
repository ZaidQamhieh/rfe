package pairing

import (
	"errors"
	"net/http"
)

// Error is a failure the HTTP layer reports to the caller as-is: Code is the
// OpenAPI Error enum value and Status the HTTP status that goes with it.
// Anything that is not an *Error is an internal failure (report 500).
type Error struct {
	Code    string
	Status  int
	Message string
}

// Error returns the message.
func (e *Error) Error() string { return e.Message }

// The errors the service returns. Messages are the old agent's, word for word
// where it had one.
var (
	ErrRateLimited        = &Error{"RATE_LIMITED", http.StatusTooManyRequests, "too many attempts, try again later"}
	ErrInvalidCode        = &Error{"INVALID_CODE", http.StatusUnauthorized, "invalid or expired pairing code"}
	ErrCodeCannotRegister = &Error{"CODE_CANNOT_REGISTER", http.StatusForbidden, "this code can't create an account - run `rfe pair --owner` on the computer"}
	ErrAccountExists      = &Error{"ACCOUNT_ALREADY_EXISTS", http.StatusConflict, "this computer already has an account - log in instead, or ask the owner to add a device"}
	ErrUsernameTaken      = &Error{"USERNAME_TAKEN", http.StatusConflict, "that username is already registered on this computer"}
	ErrInvalidCredentials = &Error{"INVALID_CREDENTIALS", http.StatusUnauthorized, "invalid username or password"}
	ErrDeviceKeyRequired  = &Error{"DEVICE_KEY_REQUIRED", http.StatusBadRequest, "devicePublicKey, nonce, and signature are required"}
	ErrInvalidNonce       = &Error{"INVALID_NONCE", http.StatusUnauthorized, "nonce missing, already used, or expired - fetch a fresh one from /v1/auth/challenge"}
	ErrInvalidSignature   = &Error{"INVALID_SIGNATURE", http.StatusUnauthorized, "signature does not match devicePublicKey"}
	ErrDeviceKeyMismatch  = &Error{"DEVICE_KEY_MISMATCH", http.StatusUnauthorized, "this device id is already bound to a different key - re-pair via QR code to reset trust"}
	ErrPairBusy           = &Error{"PAIR_BUSY", http.StatusTooManyRequests, "other pairing requests are waiting for approval on the computer"}
	ErrNotFound           = &Error{"NOT_FOUND", http.StatusNotFound, "pairing request not found or expired"}
	ErrForbidden          = &Error{"FORBIDDEN", http.StatusForbidden, "this needs an admin (login) session or the PC"}
	ErrUnauthorized       = &Error{"UNAUTHORIZED", http.StatusUnauthorized, "invalid or revoked token"}
	ErrDeviceNotFound     = &Error{"NOT_FOUND", http.StatusNotFound, "device not found"}
	ErrDeviceRevoked      = &Error{"CONFLICT", http.StatusConflict, "device is revoked; its settings can no longer be changed"}
)

// badRequest returns a BAD_REQUEST error with the given message.
func badRequest(msg string) *Error {
	return &Error{"BAD_REQUEST", http.StatusBadRequest, msg}
}

// CodeOf returns the OpenAPI error code of err and true, or "" and false when
// err is not one the service reports to the caller.
func CodeOf(err error) (string, bool) {
	var e *Error
	if errors.As(err, &e) {
		return e.Code, true
	}
	return "", false
}
