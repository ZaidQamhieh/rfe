package hostupdate

import (
	"fmt"
	"math"
)

// requiredSpace is the free space a download needs: the archive and its extracted copy (2x the bundle size) plus
// room for three copies of the database (backup, migration copy, trial copy). The second result is false when the
// sum would overflow, which callers treat as "not enough".
func requiredSpace(bundleSize, databaseBytes int64) (uint64, bool) {
	if bundleSize < 0 || databaseBytes < 0 {
		return 0, false
	}
	b, d := uint64(bundleSize), uint64(databaseBytes)
	if b > math.MaxUint64/2 || d > math.MaxUint64/3 {
		return 0, false
	}
	need := 2*b + 3*d
	if need < 2*b {
		return 0, false
	}
	return need, true
}

// checkDisk fails with ErrInsufficientSpace unless dir has at least requiredSpace free.
func checkDisk(free func(string) (uint64, error), dir string, bundleSize, databaseBytes int64) error {
	need, ok := requiredSpace(bundleSize, databaseBytes)
	if !ok {
		return fmt.Errorf("%w: requirement overflows", ErrInsufficientSpace)
	}
	have, err := free(dir)
	if err != nil {
		return fmt.Errorf("cannot read free space of %s: %w", dir, err)
	}
	if have < need {
		return fmt.Errorf("%w: %d bytes free in %s, %d needed (2x bundle %d + 3x database %d)",
			ErrInsufficientSpace, have, dir, need, bundleSize, databaseBytes)
	}
	return nil
}
