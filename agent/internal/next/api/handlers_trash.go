package api

import (
	"errors"
	"net/http"
	"time"
)

// Fixed texts of the trash routes.
const (
	msgIDsRequired = "ids are required"
	msgEmptyIDs    = "ids must not be empty; send no body to empty everything"
	msgBadTrashID  = "invalid trash id"
)

// TrashItem is one item in the trash (openapi schema TrashEntry). Its fields
// match files.TrashEntry one for one.
type TrashItem struct {
	ID           string    `json:"id"`
	Name         string    `json:"name"`
	OriginalPath string    `json:"originalPath"`
	DeletedAt    time.Time `json:"deletedAt"`
	Size         int64     `json:"size"`
	IsDir        bool      `json:"isDir"`
}

// registerTrashHandlers adds the trash routes to the handler registry.
func registerTrashHandlers(s *Server, reg map[string]handlerFunc) {
	s.addFileRoute(reg, routeKey(get, "/trash"), s.trashList)
	s.addFileRoute(reg, routeKey(post, "/trash/restore"), s.trashRestore)
	s.addFileRoute(reg, routeKey(del, "/trash"), s.trashEmpty)
}

// trashList answers GET /trash with the items the caller can see, newest first.
func (s *Server) trashList(w http.ResponseWriter, r *http.Request) error {
	svc, err := s.filesFor(r)
	if err != nil {
		return err
	}
	items, err := svc.ListTrash()
	if err != nil {
		return err
	}
	if items == nil {
		items = []TrashItem{}
	}
	writeJSON(w, http.StatusOK, map[string][]TrashItem{"items": items})
	return nil
}

// trashRestore answers POST /trash/restore with one result per id, in request
// order; each id succeeds or fails on its own.
func (s *Server) trashRestore(w http.ResponseWriter, r *http.Request) error {
	var req struct {
		IDs []string `json:"ids"`
	}
	if err := decodeJSON(r, &req); err != nil {
		return err
	}
	if len(req.IDs) == 0 {
		return badRequest(msgIDsRequired)
	}
	svc, err := s.filesFor(r)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, batchResponse{Results: svc.Restore(req.IDs)})
	return nil
}

// trashEmpty answers DELETE /trash. No body empties everything; a body that is
// present must name ids, and every id is checked before anything is removed so
// a bad request can never wipe the trash. The contract's answer has no
// per-item list, so the first id that could not be removed is reported as the
// error of the request.
func (s *Server) trashEmpty(w http.ResponseWriter, r *http.Request) error {
	var req struct {
		IDs []string `json:"ids"`
	}
	present, err := readOptionalJSON(r, &req)
	if err != nil {
		return err
	}
	if present && len(req.IDs) == 0 {
		return badRequest(msgEmptyIDs)
	}
	svc, err := s.filesFor(r)
	if err != nil {
		return err
	}
	for _, id := range req.IDs {
		if !svc.ValidTrashID(id) {
			return badRequest(msgBadTrashID)
		}
	}
	results, err := svc.EmptyTrash(req.IDs)
	if err != nil {
		return err
	}
	for _, it := range results {
		if !it.OK && it.Error != nil {
			return &apiError{kind: kindOfCode(it.Error.Code), msg: it.Error.Message}
		}
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	return nil
}

// errUnmappedItem stands for a failed item whose code has no row in errorMap
// (the internal code); Map answers it with the fixed internal text.
var errUnmappedItem = errors.New("api: item failed with an unmapped code")

// kindOfCode is the domain error behind an error code of a failed item, so the
// whole-request answer carries the same status and code as the item did.
func kindOfCode(code string) error {
	for _, m := range errorMap {
		if m.code == code {
			return m.kind
		}
	}
	return errUnmappedItem
}
