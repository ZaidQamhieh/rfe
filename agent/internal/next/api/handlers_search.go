package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
)

// Response headers of GET /search that report a partial result, and the one that
// carries the cursor for the next page (the body stays a bare Entry array).
const (
	headerSearchTruncated  = "X-Search-Truncated"
	headerSearchTimeBudget = "X-Search-Time-Budget"
	headerSearchNextCursor = "X-Search-Next-Cursor"
	flagOn                 = "1"
)

// maxSearchRawQueryBytes bounds the whole query string of a search before it is
// parsed. The search service bounds q and the cursor precisely; this only stops
// an oversized request from being parsed at all.
const maxSearchRawQueryBytes = 8192

// msgSearchQueryTooLong is the BAD_REQUEST text for an oversized query string.
const msgSearchQueryTooLong = "search query is too long"

// SearchPage is one page of search hits. Entries is a slice that marshals to the
// Entry array of the contract; it may be nil, which is sent as an empty array.
type SearchPage struct {
	Entries    any
	NextCursor *string // nil when the search finished
	Truncated  bool    // the page is full and more matches exist
	TimeBudget bool    // the time budget ended the search early
}

// SearchService runs a search on behalf of a device. The implementation turns
// query (the raw URL values of GET /search) into a request with the search
// package's ParseQuery, and limits the search to jailRoot when it is not empty.
// Its errors must be domain errors (ErrBadRequest, ErrForbidden, ...) with
// client-safe text; anything else is reported as an internal error.
type SearchService interface {
	Search(ctx context.Context, jailRoot string, query url.Values) (SearchPage, error)
}

// clientGone reports whether err only says the caller gave up: its context is
// done, so there is nobody to answer.
func clientGone(r *http.Request, err error) bool {
	return r.Context().Err() != nil &&
		(errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded))
}

// search answers GET /search: a bare JSON array of entries, with headers for
// "more results follow" (cursor), "page was full" and "time budget ran out".
func (s *Server) search(w http.ResponseWriter, r *http.Request) error {
	svc := s.cfg.Misc.Search
	if svc == nil {
		return notYet
	}
	c, err := callOf(r)
	if err != nil {
		return err
	}
	if len(r.URL.RawQuery) > maxSearchRawQueryBytes {
		return badRequest(msgSearchQueryTooLong)
	}
	page, err := svc.Search(r.Context(), c.principal.JailRoot, r.URL.Query())
	if err != nil {
		if clientGone(r, err) {
			return nil
		}
		return err
	}
	body, err := json.Marshal(page.Entries)
	if err != nil {
		return err
	}
	if string(body) == "null" {
		body = []byte("[]")
	}
	h := w.Header()
	if page.Truncated {
		h.Set(headerSearchTruncated, flagOn)
	}
	if page.TimeBudget {
		h.Set(headerSearchTimeBudget, flagOn)
	}
	if page.NextCursor != nil {
		h.Set(headerSearchNextCursor, *page.NextCursor)
	}
	writeJSON(w, http.StatusOK, json.RawMessage(body))
	return nil
}
