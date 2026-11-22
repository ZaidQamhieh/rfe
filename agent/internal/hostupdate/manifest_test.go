package hostupdate

import (
	"errors"
	"testing"
)

func TestPickBundle(t *testing.T) {
	rel := func(plats ...string) Release {
		var r Release
		r.Version = "1.0.0"
		for _, p := range plats {
			var os, arch string
			for i := range p {
				if p[i] == '/' {
					os, arch = p[:i], p[i+1:]
				}
			}
			r.Bundles = append(r.Bundles, Bundle{GOOS: os, GOARCH: arch, URL: "https://x/" + p})
		}
		return r
	}
	cases := []struct {
		name                   string
		r                      Release
		goos, goarch, emulated string
		want                   string // "" means ErrWrongArch
	}{
		{"exact linux", rel("linux/amd64", "linux/arm64"), "linux", "arm64", "", "linux/arm64"},
		{"exact windows amd64", rel("windows/amd64", "windows/arm64"), "windows", "amd64", "", "windows/amd64"},
		{"x64 emulated on arm64 prefers arm64", rel("windows/amd64", "windows/arm64"), "windows", "amd64", "arm64", "windows/arm64"},
		{"x64 emulated, no arm64 bundle falls back to amd64", rel("windows/amd64"), "windows", "amd64", "arm64", "windows/amd64"},
		{"native arm64 ignores emulation hint", rel("windows/amd64", "windows/arm64"), "windows", "arm64", "arm64", "windows/arm64"},
		{"emulation hint is windows only", rel("linux/amd64", "linux/arm64"), "linux", "amd64", "arm64", "linux/amd64"},
		{"os must match", rel("linux/amd64"), "windows", "amd64", "", ""},
		{"arch missing", rel("linux/amd64"), "linux", "arm64", "", ""},
		{"arm64 only, amd64 host", rel("windows/arm64"), "windows", "amd64", "", ""},
		{"empty", rel(), "linux", "amd64", "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			b, err := PickBundle(tc.r, tc.goos, tc.goarch, tc.emulated)
			if tc.want == "" {
				if !errors.Is(err, ErrWrongArch) {
					t.Fatalf("err = %v, want ErrWrongArch", err)
				}
				return
			}
			if err != nil || b.GOOS+"/"+b.GOARCH != tc.want {
				t.Fatalf("got %+v, %v; want %s", b, err, tc.want)
			}
		})
	}
}

func TestCompareVersions(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"1.0.0", "1.0.0", 0},
		{"1.0.1", "1.0.0", 1},
		{"1.9.0", "1.10.0", -1},
		{"2.0.0", "1.99.99", 1},
		{"1.0.0-rc.1", "1.0.0", -1},
		{"1.0.0", "1.0.0-rc.1", 1},
		{"1.0.0-rc.1", "1.0.0-rc.2", -1},
		{"1.0.0-rc.2", "1.0.0-rc.10", -1},
		{"1.0.0-alpha", "1.0.0-alpha.1", -1},
		{"1.0.0-alpha.1", "1.0.0-alpha.beta", -1},
		{"1.0.0-beta", "1.0.0-rc.1", -1},
		{"1.0.0+build5", "1.0.0+build9", 0},
		{"1.1.0-rc.1", "1.0.9", 1},
	}
	for _, tc := range cases {
		got, err := CompareVersions(tc.a, tc.b)
		if err != nil || got != tc.want {
			t.Errorf("CompareVersions(%q,%q) = %d, %v; want %d", tc.a, tc.b, got, err, tc.want)
		}
		if rev, _ := CompareVersions(tc.b, tc.a); rev != -tc.want {
			t.Errorf("CompareVersions(%q,%q) not antisymmetric: %d", tc.b, tc.a, rev)
		}
	}
	for _, bad := range []string{"", "1", "1.2", "v1.2.3", "01.2.3", "1.2.3-", "1.2.3-01", "1.2.x", "1.2.3.4", "dev"} {
		if _, err := CompareVersions(bad, "1.0.0"); !errors.Is(err, ErrBadVersion) {
			t.Errorf("%q: err = %v, want ErrBadVersion", bad, err)
		}
	}
}
