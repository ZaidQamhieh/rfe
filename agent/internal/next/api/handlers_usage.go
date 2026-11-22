package api

import (
	"context"
	"net/http"
	"time"
)

const (
	// usageMaxScans is how many storage scans may run at once; more get 429 USAGE_BUSY.
	usageMaxScans = 2
	// usageTimeBudget ends a scan; the answer is then marked partial.
	usageTimeBudget = 15 * time.Second
	// usageRetryAfterSeconds is the Retry-After of a 429 USAGE_BUSY.
	usageRetryAfterSeconds = "1"
	msgUsageBusy           = "storage scan capacity is full; retry shortly"
)

// UsageEntryJSON is one child of the scanned folder (openapi GET /fs/usage).
type UsageEntryJSON struct {
	Name  string `json:"name"`
	IsDir bool   `json:"isDir"`
	Bytes int64  `json:"bytes"`
}

// UsagePage is the answer of a storage scan.
type UsagePage struct {
	Path       string           `json:"path"`
	TotalBytes int64            `json:"totalBytes"`
	Partial    bool             `json:"partial"`
	Entries    []UsageEntryJSON `json:"entries"`
}

// UsageService is the part of the file service GET /fs/usage uses. When the FileService does not implement it the
// route answers 501. Errors must be domain errors with client-safe text.
type UsageService interface {
	Usage(ctx context.Context, path string) (*UsagePage, error)
}

// registerUsageHandlers adds the handler of GET /fs/usage.
func registerUsageHandlers(s *Server, m map[string]handlerFunc) {
	scans := make(chan struct{}, usageMaxScans)
	s.addFileRoute(m, routeKey(get, "/fs/usage"), func(w http.ResponseWriter, r *http.Request) error {
		path, err := queryPathOf(r)
		if err != nil {
			return err
		}
		fsvc, err := s.filesFor(r)
		if err != nil {
			return err
		}
		svc, ok := fsvc.(UsageService)
		if !ok {
			return notYet
		}
		select {
		case scans <- struct{}{}:
			defer func() { <-scans }()
		default:
			w.Header().Set(headerRetryAfter, usageRetryAfterSeconds)
			return &apiError{kind: ErrUsageBusy, msg: msgUsageBusy}
		}
		ctx, cancel := context.WithTimeout(r.Context(), usageTimeBudget)
		defer cancel()
		page, err := svc.Usage(ctx, path)
		if err != nil {
			return err
		}
		if page.Entries == nil {
			page.Entries = []UsageEntryJSON{}
		}
		if page.Partial {
			w.Header().Set(headerSearchTimeBudget, flagOn)
		}
		writeJSON(w, http.StatusOK, page)
		return nil
	})
}
