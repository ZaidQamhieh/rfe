package audit

import "github.com/ZaidQamhieh/rfe/agent/internal/next/store"

// The v3 database is the log's store in production.
var _ Store = (*store.DB)(nil)
