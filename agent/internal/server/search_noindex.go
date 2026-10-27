package server

import "github.com/ZaidQamhieh/rfe/agent/internal/fsops"

// noIndex is the search backend of an agent that runs without rfe-indexd: it never has a snapshot, so searches
// (and recents) walk the roots live, the same path an indexed agent takes while its sidecar is down or still
// building.
type noIndex struct{}

func (noIndex) query(*searchFilters, []string, int) ([]fsops.Entry, bool, bool) {
	return nil, false, false
}
