//go:build linux

package fsops

import "testing"

// TestUnescapeMountField checks that the octal escapes /proc/mounts uses for
// space, tab, newline and backslash are decoded to the real characters.
func TestUnescapeMountField(t *testing.T) {
	cases := []struct{ in, want string }{
		{"/media/user/My\\040Drive", "/media/user/My Drive"},
		{"/mnt/a\\011b", "/mnt/a\tb"},
		{"/mnt/a\\012b", "/mnt/a\nb"},
		{"/mnt/a\\134b", "/mnt/a\\b"},
		{"/mnt/plain", "/mnt/plain"},
		// A backslash that does not start a known escape is kept as is.
		{"/mnt/a\\zb", "/mnt/a\\zb"},
		{"/mnt/trail\\04", "/mnt/trail\\04"},
		// Decoding is single pass: an escaped backslash followed by digits stays literal.
		{"/mnt/a\\134040", "/mnt/a\\040"},
	}
	for _, c := range cases {
		if got := unescapeMountField(c.in); got != c.want {
			t.Errorf("unescapeMountField(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
