package hostupdate

import (
	"fmt"
	"strconv"
	"strings"
)

// semver is a parsed semantic version (https://semver.org): numbers plus dot-separated pre-release identifiers.
// Build metadata (+...) is accepted and ignored, as the specification says for ordering.
type semver struct {
	major, minor, patch uint64
	pre                 []string
}

// parseSemver parses MAJOR.MINOR.PATCH[-pre][+build]; no leading "v", no leading zeros in numbers.
func parseSemver(s string) (semver, error) {
	bad := func(why string) (semver, error) {
		return semver{}, fmt.Errorf("%w: %q (%s)", ErrBadVersion, s, why)
	}
	core, _, _ := strings.Cut(s, "+")
	core, pre, hasPre := strings.Cut(core, "-")
	parts := strings.Split(core, ".")
	if len(parts) != 3 {
		return bad("want MAJOR.MINOR.PATCH")
	}
	var nums [3]uint64
	for i, p := range parts {
		n, err := parseNumeric(p)
		if err != nil {
			return bad(err.Error())
		}
		nums[i] = n
	}
	v := semver{major: nums[0], minor: nums[1], patch: nums[2]}
	if hasPre {
		v.pre = strings.Split(pre, ".")
		for _, id := range v.pre {
			if id == "" {
				return bad("empty pre-release identifier")
			}
			for _, r := range id {
				if !(r >= '0' && r <= '9' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r == '-') {
					return bad("bad pre-release character")
				}
			}
			if isNumeric(id) && len(id) > 1 && id[0] == '0' {
				return bad("leading zero in pre-release number")
			}
		}
	}
	return v, nil
}

// isNumeric reports whether s is a non-empty string of ASCII digits.
func isNumeric(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// parseNumeric parses a version number field: digits only, no leading zero.
func parseNumeric(p string) (uint64, error) {
	if !isNumeric(p) {
		return 0, fmt.Errorf("%q is not a number", p)
	}
	if len(p) > 1 && p[0] == '0' {
		return 0, fmt.Errorf("%q has a leading zero", p)
	}
	n, err := strconv.ParseUint(p, 10, 63)
	if err != nil {
		return 0, fmt.Errorf("%q is out of range", p)
	}
	return n, nil
}

// compare orders two parsed versions: -1, 0 or 1. A version with a pre-release (for example 1.2.0-rc.1) is lower
// than the same version without one; pre-release identifiers compare numerically when both are numbers, otherwise
// as text, and a numeric identifier is lower than a textual one.
func (a semver) compare(b semver) int {
	for _, d := range [][2]uint64{{a.major, b.major}, {a.minor, b.minor}, {a.patch, b.patch}} {
		if d[0] != d[1] {
			if d[0] < d[1] {
				return -1
			}
			return 1
		}
	}
	switch {
	case len(a.pre) == 0 && len(b.pre) == 0:
		return 0
	case len(a.pre) == 0:
		return 1
	case len(b.pre) == 0:
		return -1
	}
	for i := 0; i < len(a.pre) && i < len(b.pre); i++ {
		x, y := a.pre[i], b.pre[i]
		if x == y {
			continue
		}
		xn, yn := isNumeric(x), isNumeric(y)
		switch {
		case xn && yn:
			xv, _ := strconv.ParseUint(x, 10, 64)
			yv, _ := strconv.ParseUint(y, 10, 64)
			if xv != yv {
				if xv < yv {
					return -1
				}
				return 1
			}
			continue // equal numbers: the identifiers are equal strings too (no leading zeros), so unreachable
		case xn:
			return -1
		case yn:
			return 1
		default:
			if x < y {
				return -1
			}
			return 1
		}
	}
	switch {
	case len(a.pre) < len(b.pre):
		return -1
	case len(a.pre) > len(b.pre):
		return 1
	}
	return 0
}

// CompareVersions orders two semantic version strings: -1 if a is lower than b, 0 if equal, 1 if higher.
func CompareVersions(a, b string) (int, error) {
	va, err := parseSemver(a)
	if err != nil {
		return 0, err
	}
	vb, err := parseSemver(b)
	if err != nil {
		return 0, err
	}
	return va.compare(vb), nil
}
