package main

import (
	"crypto/tls"
	"net/http"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"
)

// TestWebListenBindAddr is the PR-61 regression: an operator restricting the
// primary listener to a specific host must have the best-effort port-443
// listener inherit that restriction, not always bind all interfaces.
func TestWebListenBindAddr(t *testing.T) {
	cases := []struct {
		name    string
		primary string
		want    string
	}{
		{"no host: all interfaces", ":8765", webListenAddr},
		{"explicit loopback carries over", "127.0.0.1:8765", "127.0.0.1:443"},
		{"explicit LAN IP carries over", "192.168.1.5:8765", "192.168.1.5:443"},
		{"unparsable falls back to all interfaces", "not-an-addr", webListenAddr},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := webListenBindAddr(tc.primary); got != tc.want {
				t.Fatalf("webListenBindAddr(%q) = %q, want %q", tc.primary, got, tc.want)
			}
		})
	}
}

func TestServeConfigurationHelpers(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("RFE_DATA_DIR", "/tmp/from-env")
	flags := parseServeFlags([]string{"-addr", "127.0.0.1:8765", "-name", "desk", "-data", "/tmp/from-flag", "-read-only", "-roots", " /srv/a, ,/srv/b "})
	if flags.addr != "127.0.0.1:8765" || flags.name != "desk" || flags.dataDir != "/tmp/from-flag" || !flags.readOnly || flags.roots != " /srv/a, ,/srv/b " {
		t.Fatalf("unexpected parsed flags: %+v", flags)
	}
	if got, want := parseSeedRoots(" /srv/a, ,/srv/b,, "), []string{"/srv/a", "/srv/b"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("parseSeedRoots = %#v, want %#v", got, want)
	}
	if got := parseSeedRoots(""); len(got) != 0 {
		t.Fatalf("empty roots = %#v, want empty", got)
	}
	if flags := parseServeFlags(nil); filepath.Clean(flags.roots) != filepath.Join(home, "RFE Files") || flags.rootsExplicit {
		t.Fatalf("default roots = (%q, explicit=%v), want RFE Files under home and implicit", flags.roots, flags.rootsExplicit)
	}
	if flags := parseServeFlags([]string{"-roots", ""}); flags.roots != "" || !flags.rootsExplicit {
		t.Fatalf("explicit empty roots = (%q, explicit=%v), want unrestricted empty value", flags.roots, flags.rootsExplicit)
	}
}

func TestDataDirTrashAndDisplayHelpers(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("RFE_DATA_DIR", "")
	if got, want := defaultDataDir(), filepath.Join(home, ".rfe"); got != want {
		t.Fatalf("defaultDataDir = %q, want %q", got, want)
	}
	t.Setenv("RFE_DATA_DIR", "/custom/rfe")
	if got := defaultDataDir(); got != "/custom/rfe" {
		t.Fatalf("environment data dir = %q", got)
	}
	t.Setenv("XDG_DATA_HOME", "/custom/xdg")
	wantTrash := filepath.Join("/data", "trash")
	if runtime.GOOS == "linux" {
		wantTrash = filepath.Join("/custom/xdg", "Trash")
	}
	if got, want := defaultTrashDir("/data"), wantTrash; got != want {
		t.Fatalf("defaultTrashDir from XDG = %q, want %q", got, want)
	}
	t.Setenv("XDG_DATA_HOME", "")
	wantTrash = filepath.Join("/data", "trash")
	if runtime.GOOS == "linux" {
		wantTrash = filepath.Join(home, ".local", "share", "Trash")
	}
	if got, want := defaultTrashDir("/data"), wantTrash; got != want {
		t.Fatalf("defaultTrashDir from home = %q, want %q", got, want)
	}
	if got := orNone(""); got != "(none detected)" {
		t.Fatalf("orNone empty = %q", got)
	}
	if got := orNone("192.0.2.4"); got != "192.0.2.4" {
		t.Fatalf("orNone value = %q", got)
	}
}

func TestHTTPAndAddressHelpers(t *testing.T) {
	server := newHTTPServer(":8765", http.NotFoundHandler(), tls.Certificate{})
	if server.Addr != ":8765" || server.TLSConfig.MinVersion != tls.VersionTLS12 || len(server.TLSConfig.Certificates) != 1 || server.ReadHeaderTimeout == 0 || server.ReadTimeout == 0 || server.WriteTimeout == 0 || server.IdleTimeout == 0 {
		t.Fatalf("unexpected server settings: %+v", server)
	}
	for _, tc := range []struct{ input, want string }{
		{"127.0.0.1:8765", "127.0.0.1:443"},
		{"[::1]:8765", "[::1]:443"},
		{":8765", ":443"},
		{"0.0.0.0:8765", "0.0.0.0:443"},
		{"not-an-address", ":443"},
	} {
		if got := webListenBindAddr(tc.input); got != tc.want {
			t.Errorf("webListenBindAddr(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
	if lan, ts, mac := reachableAddresses("malformed"); lan != "" || ts != "" || mac != "" {
		t.Fatalf("malformed address produced (%q,%q,%q)", lan, ts, mac)
	}
	if lan, ts, mac := reachableAddresses("192.0.2.12:8765"); lan != "192.0.2.12:8765" || ts != "" || mac != "" {
		t.Fatalf("explicit bind produced (%q,%q,%q)", lan, ts, mac)
	}
	if stop := startMDNS("bad", version); stop != nil {
		t.Fatal("invalid mDNS listen address unexpectedly started")
	}
	if stop := startMDNS(":not-a-port", version); stop != nil {
		t.Fatal("invalid mDNS port unexpectedly started")
	}
	if stop := startWebAlias("bad", version); stop != nil {
		t.Fatal("invalid web alias address unexpectedly started")
	}
	if stop := startWebAlias("127.0.0.1:not-a-port", version); stop != nil {
		t.Fatal("invalid web alias port unexpectedly started")
	}
	for _, addr := range []string{"127.0.0.1:18765", "[::1]:18765", "localhost:18765"} {
		if stop := startMDNS(addr, version); stop != nil {
			stop()
			t.Fatalf("mDNS advertised a loopback-only listener %s", addr)
		}
		if stop := startWebAlias(addr, version); stop != nil {
			stop()
			t.Fatalf("web alias advertised a loopback-only listener %s", addr)
		}
	}
}
