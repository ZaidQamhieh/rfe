// Package server — batch checksums handler.
package server

import (
	"encoding/json"
	"net/http"
	"sync"

	"github.com/ZaidQamhieh/rfe/agent/internal/fsops"
)

const (
	batchChecksumMaxPaths = 1000
	batchChecksumWorkers  = 4
)

type checksumResult struct {
	Path  string `json:"path"`
	Hash  string `json:"hash,omitempty"`
	Error string `json:"error,omitempty"`
}

func batchChecksumHandler(ops *fsops.Ops) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ops := opsFromContext(r.Context(), ops)

		var req struct {
			Paths []string `json:"paths"`
			Algo  string   `json:"algo"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxJSONBody)).Decode(&req); err != nil || len(req.Paths) == 0 {
			writeError(w, http.StatusBadRequest, "BAD_REQUEST", "paths required")
			return
		}
		if len(req.Paths) > batchChecksumMaxPaths {
			writeError(w, http.StatusBadRequest, "BAD_REQUEST", "too many paths (max 1000)")
			return
		}
		if req.Algo == "" {
			req.Algo = "sha256"
		}

		results := make([]checksumResult, len(req.Paths))

		// Bounded goroutine pool.
		var wg sync.WaitGroup
		work := make(chan int, len(req.Paths))
		for i := range req.Paths {
			work <- i
		}
		close(work)

		for w := 0; w < batchChecksumWorkers; w++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for i := range work {
					p := req.Paths[i]
					sum, err := ops.Checksum(p, req.Algo)
					if err != nil {
						results[i] = checksumResult{Path: p, Error: err.Error()}
					} else {
						results[i] = checksumResult{Path: p, Hash: sum}
					}
				}
			}()
		}
		wg.Wait()

		writeJSON(w, http.StatusOK, map[string]any{"checksums": results})
	}
}
