package server

import "github.com/ZaidQamhieh/rfe/agent/internal/transfer"

// The quota tests in this package declare uploads of tens of GiB, far more than the test disk holds. Opening a
// session now reserves its declared size, so they would be refused with 507; sizing the temp file sparsely keeps
// them about quota accounting. Tests of the reservation itself call transfer.SetSparseReservations(false).
func init() { transfer.SetSparseReservations(true) }
