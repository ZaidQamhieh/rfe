package api

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
)

// Fixed texts of errors the HTTP layer itself raises. None carries request data.
const (
	msgInvalidBody  = "request body is missing or is not valid JSON"
	msgBodyTooLarge = "request body is too large"
)

// errTrailingData marks a body with more than one JSON value.
var errTrailingData = errors.New("trailing data after the JSON value")

// apiError is a domain error with its own client-safe text: Map finds the row
// of kind and sends msg.
type apiError struct {
	kind error
	msg  string
}

// Error returns the client-safe text.
func (e *apiError) Error() string { return e.msg }

// Unwrap gives the kind Map matches against.
func (e *apiError) Unwrap() error { return e.kind }

// badRequest builds a BAD_REQUEST with a fixed text.
func badRequest(msg string) error { return &apiError{kind: ErrBadRequest, msg: msg} }

// writeJSON sends v as the whole response with the given status. The body is
// encoded first so an encoding failure still gets a clean 500.
func writeJSON(w http.ResponseWriter, status int, v any) {
	body, err := json.Marshal(v)
	if err != nil {
		status = http.StatusInternalServerError
		body, _ = json.Marshal(ErrorBody{Code: CodeInternal, Message: InternalMessage})
	}
	w.Header().Set(headerContentType, contentJSON)
	w.WriteHeader(status)
	_, _ = w.Write(append(body, '\n')) // the client may be gone; nothing to do about it
}

// writeNoContent answers 204.
func writeNoContent(w http.ResponseWriter) { w.WriteHeader(http.StatusNoContent) }

// writeError turns err into the contract error body through Map. An unexpected
// error is logged here and the client sees only the fixed internal text.
func (s *Server) writeError(w http.ResponseWriter, r *http.Request, err error) {
	m := Map(err)
	if m.Internal {
		s.logf("api: %s: internal error: %v", r.Pattern, err)
	}
	writeJSON(w, m.Status, ErrorBody{Code: m.Code, Message: m.Message})
}

// decodeJSON reads the request body as one JSON value into dst. An oversized
// body is PAYLOAD_TOO_LARGE; an empty, malformed or trailing-garbage body is
// BAD_REQUEST with a fixed text (the decoder's own text is not sent).
func decodeJSON(r *http.Request, dst any) error {
	dec := json.NewDecoder(r.Body)
	err := dec.Decode(dst)
	if err == nil {
		if _, tokErr := dec.Token(); !errors.Is(tokErr, io.EOF) {
			err = tokErr // a read failure, such as the size limit, keeps its own kind
			if err == nil {
				err = errTrailingData
			}
		}
	}
	if err == nil {
		return nil
	}
	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) {
		return &apiError{kind: ErrPayloadTooLarge, msg: msgBodyTooLarge}
	}
	return badRequest(msgInvalidBody)
}
