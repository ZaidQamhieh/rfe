package search

import (
	"context"
	"errors"
)

// Operation name of the rfe-indexd sidecar that answers a name query
// (protocol/sidecar.md).
const opIndexQuery = "index.query"

// ErrIndexNotReady is what an Indexer returns when it has no answer yet (no
// snapshot, or the roots are not covered by it). The service then walks the
// folders itself.
var ErrIndexNotReady = errors.New("search: index is not ready")

// IndexFilters is the wire form of the validated filters handed to the
// indexer: a lowercase glob or substring plus the optional bounds. A nil Types
// or Exts means no filter.
type IndexFilters struct {
	Glob        string   `json:"glob"`
	Needle      string   `json:"needle"`
	Types       []string `json:"types"`
	Exts        []string `json:"exts"`
	MinSize     *int64   `json:"minSize"`
	MaxSize     *int64   `json:"maxSize"`
	ModAfterNs  *int64   `json:"modAfterNs"`
	ModBeforeNs *int64   `json:"modBeforeNs"`
}

// IndexQuery asks an Indexer for the matches below one root.
type IndexQuery struct {
	Filters IndexFilters `json:"filters"`
	Roots   []string     `json:"roots"`
	Limit   int          `json:"limit"`
}

// IndexEntry is one index hit. Only the path is used: the service re-reads the
// entry itself through its own jailed file layer, so a wrong or stale index
// can never put data into a result.
type IndexEntry struct {
	Path string `json:"path"`
}

// IndexResult is an Indexer's answer. Entries must come in the order of a
// depth-first walk with names sorted (the order of the live walk), which is
// what makes a cursor from an index page valid for the walk. Truncated is true
// when Limit was reached or the index itself was cut by its own budget.
type IndexResult struct {
	Ready     bool         `json:"ready"`
	Entries   []IndexEntry `json:"entries"`
	Truncated bool         `json:"truncated"`
}

// Indexer answers name queries from a prebuilt index (the rfe-indexd sidecar).
// It is an optimisation only: the service falls back to its own walk whenever
// Query fails, is not ready, or returns an incomplete answer, and the walk is
// the oracle for the result.
type Indexer interface {
	Query(ctx context.Context, q IndexQuery) (IndexResult, error)
}

// Caller is the part of the sidecar client (agent/internal/sidecar Client and
// Supervisor) that SidecarIndexer needs: one request and its decoded answer.
type Caller interface {
	Call(ctx context.Context, op string, req, resp any) error
}

// SidecarIndexer is an Indexer backed by rfe-indexd.
type SidecarIndexer struct {
	Caller Caller
}

// Query sends index.query to the sidecar. A snapshot that is not ready is
// reported as ErrIndexNotReady.
func (s SidecarIndexer) Query(ctx context.Context, q IndexQuery) (IndexResult, error) {
	var res IndexResult
	if err := s.Caller.Call(ctx, opIndexQuery, q, &res); err != nil {
		return IndexResult{}, err
	}
	if !res.Ready {
		return IndexResult{}, ErrIndexNotReady
	}
	return res, nil
}
