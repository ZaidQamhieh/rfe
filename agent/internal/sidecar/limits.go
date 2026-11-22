package sidecar

// Name and limits of the rfe-indexd sidecar, shared by the server's search index and the next daemon's index
// supervisor so the two cannot drift.
const (
	// IndexdName is the sidecar's name: its RFE_SIDECARS word and the prefix of its log lines and notes.
	IndexdName = "indexd"

	// IndexMaxEntries caps the entries one index.build snapshot may hold.
	IndexMaxEntries = 2_000_000
	// IndexMaxEstimatedBytes caps the estimated bytes of one snapshot. It stops unusually long names and paths
	// from defeating the entry cap.
	IndexMaxEstimatedBytes = 128 << 20
)
