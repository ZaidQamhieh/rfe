package settings

import "testing"

func TestParseBool(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want bool
	}{
		{"stored true", "true", true},
		{"stored false", "false", false},
		{"empty (unset) reads as false", "", false},
		{"case differs reads as false", "TRUE", false},
		{"padded true reads as false", " true", false},
		{"numeric one reads as false", "1", false},
		{"corrupt value reads as false", "yes", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := parseBool(tt.in); got != tt.want {
				t.Fatalf("parseBool(%q) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}

func TestParseBool_RoundTripsBoolToStr(t *testing.T) {
	for _, b := range []bool{true, false} {
		if got := parseBool(boolToStr(b)); got != b {
			t.Fatalf("parseBool(boolToStr(%v)) = %v", b, got)
		}
	}
}
