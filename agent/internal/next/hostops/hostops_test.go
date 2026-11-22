package hostops

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

const (
	testMAC       = "aa:bb:cc:dd:ee:ff"
	testDevice    = "device-1"
	testAPKName   = "rfe-1.2.0-12.apk"
	testAPKBytes  = "apk-bytes-0123456789"
	journalSample = "2030-01-02T03:04:05+00:00 host rfe[42]: 2030/01/02 03:04:05 listening\nplain"
)

func TestJournalTailParsesLinesAndPassesLimit(t *testing.T) {
	var gotArgs []string
	j := Journal{Run: func(_ context.Context, name string, args ...string) ([]byte, error) {
		gotArgs = append([]string{name}, args...)
		return []byte(journalSample + "\n"), nil
	}}
	lines, err := j.Tail(context.Background(), 5)
	if err != nil {
		t.Fatal(err)
	}
	want := []LogLine{
		{TS: "2030-01-02T03:04:05+00:00", Message: "listening"},
		{TS: "", Message: "plain"},
	}
	if !reflect.DeepEqual(lines, want) {
		t.Fatalf("lines = %+v, want %+v", lines, want)
	}
	joined := strings.Join(gotArgs, " ")
	if !strings.Contains(joined, "-n 5 ") || !strings.Contains(joined, "-u "+AgentUnit) {
		t.Fatalf("command = %q, want the limit 5 and the agent unit", joined)
	}
}

func TestJournalTailLimitOutOfRangeFallsBackToDefault(t *testing.T) {
	for _, n := range []int{0, -3, MaxLogLines + 1} {
		if got := NormalizeLimit(n); got != DefaultLogLines {
			t.Errorf("NormalizeLimit(%d) = %d, want %d", n, got, DefaultLogLines)
		}
	}
	if got := NormalizeLimit(MaxLogLines); got != MaxLogLines {
		t.Errorf("NormalizeLimit(max) = %d", got)
	}
}

func TestJournalTailKeepsOnlyTheNewestLimitLines(t *testing.T) {
	j := Journal{Run: func(context.Context, string, ...string) ([]byte, error) {
		return []byte("t1 h p: one\nt2 h p: two\nt3 h p: three\n"), nil
	}}
	lines, err := j.Tail(context.Background(), 2)
	if err != nil || len(lines) != 2 || lines[0].Message != "two" || lines[1].Message != "three" {
		t.Fatalf("lines = %+v, %v", lines, err)
	}
}

func TestJournalTailFailureIsUnavailableAndEmptyOutputIsEmptyList(t *testing.T) {
	boom := errors.New("no journald")
	_, err := Journal{Run: func(context.Context, string, ...string) ([]byte, error) { return nil, boom }}.Tail(context.Background(), 0)
	if !errors.Is(err, ErrLogsUnavailable) {
		t.Fatalf("err = %v, want ErrLogsUnavailable", err)
	}
	lines, err := Journal{Run: func(context.Context, string, ...string) ([]byte, error) { return nil, nil }}.Tail(context.Background(), 0)
	if err != nil || lines == nil || len(lines) != 0 {
		t.Fatalf("empty output = %+v, %v; want a non-nil empty list", lines, err)
	}
}

func TestRestarterOnlyLinuxAndRunsUnitCommandAfterDelay(t *testing.T) {
	var slept time.Duration
	var ran []string
	r := &Restarter{
		OS: "linux", Unit: "u.service", Delay: 7 * time.Second,
		Sleep: func(d time.Duration) { slept = d },
		Run:   func(name string, args ...string) error { ran = append([]string{name}, args...); return nil },
	}
	if !r.Supported() {
		t.Fatal("linux must be supported")
	}
	if err := r.Restart(); err != nil {
		t.Fatal(err)
	}
	if slept != 7*time.Second || !reflect.DeepEqual(ran, []string{"systemctl", "--user", "restart", "u.service"}) {
		t.Fatalf("slept %v, ran %v", slept, ran)
	}
	if (&Restarter{OS: "windows"}).Supported() {
		t.Fatal("windows must not be supported")
	}
}

// fakeSender records packets.
type fakeSender struct {
	packets [][]byte
	err     error
}

func (f *fakeSender) Send(p []byte) error {
	f.packets = append(f.packets, append([]byte(nil), p...))
	return f.err
}

func TestWakerSendsMagicPacketAndReturnsCanonicalMAC(t *testing.T) {
	s := &fakeSender{}
	w := NewWaker(s, WakerOptions{})
	got, err := w.Wake(testDevice, "AA-BB-CC-DD-EE-FF")
	if err != nil || got != testMAC {
		t.Fatalf("Wake = %q, %v", got, err)
	}
	if len(s.packets) != 1 {
		t.Fatalf("%d packets sent", len(s.packets))
	}
	p := s.packets[0]
	if len(p) != macBytes*(1+magicRepeats) || !bytes.Equal(p[:macBytes], bytes.Repeat([]byte{magicHeaderByte}, macBytes)) {
		t.Fatalf("bad packet header, len %d", len(p))
	}
	if !bytes.Equal(p[len(p)-macBytes:], []byte{0xaa, 0xbb, 0xcc, 0xdd, 0xee, 0xff}) {
		t.Fatalf("packet does not end with the MAC: %x", p)
	}
}

func TestWakerRefusesBadMACWithoutSending(t *testing.T) {
	s := &fakeSender{}
	w := NewWaker(s, WakerOptions{})
	for _, bad := range []string{"", "not-a-mac", "aa:bb:cc:dd:ee", "00:00:5e:00:53:01:02:03"} {
		if _, err := w.Wake(testDevice, bad); !errors.Is(err, ErrInvalidMAC) {
			t.Errorf("Wake(%q) = %v, want ErrInvalidMAC", bad, err)
		}
	}
	if len(s.packets) != 0 {
		t.Fatal("a packet was sent for a bad MAC")
	}
}

func TestWakerRateLimitsPerDeviceAndRecoversAfterWindow(t *testing.T) {
	now := time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)
	s := &fakeSender{}
	w := NewWaker(s, WakerOptions{Budget: 2, Window: time.Minute, Now: func() time.Time { return now }})
	for i := 0; i < 2; i++ {
		if _, err := w.Wake(testDevice, testMAC); err != nil {
			t.Fatalf("call %d: %v", i, err)
		}
	}
	if _, err := w.Wake(testDevice, testMAC); !errors.Is(err, ErrWakeRateLimited) {
		t.Fatalf("third call = %v, want ErrWakeRateLimited", err)
	}
	if _, err := w.Wake("other", testMAC); err != nil {
		t.Fatalf("another device was limited: %v", err)
	}
	now = now.Add(time.Minute)
	if _, err := w.Wake(testDevice, testMAC); err != nil {
		t.Fatalf("after the window: %v", err)
	}
}

func TestWakerReturnsSenderError(t *testing.T) {
	boom := errors.New("socket closed")
	w := NewWaker(&fakeSender{err: boom}, WakerOptions{})
	if _, err := w.Wake(testDevice, testMAC); !errors.Is(err, boom) {
		t.Fatalf("err = %v, want the sender's error", err)
	}
}

// writeAPK writes an APK file with content and returns its digest.
func writeAPK(t *testing.T, dir, name, content string) string {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte(content))
	return hex.EncodeToString(sum[:])
}

func TestUpdatesLatestReportsNewestWithDigestAndSize(t *testing.T) {
	dir := t.TempDir()
	writeAPK(t, dir, "rfe-1.0.0-1.apk", "old")
	want := writeAPK(t, dir, testAPKName, testAPKBytes)
	rel, err := NewUpdates(dir).Latest()
	if err != nil || rel == nil {
		t.Fatalf("Latest = %+v, %v", rel, err)
	}
	if rel.VersionCode != 12 || rel.VersionName != "1.2.0" || rel.Size != int64(len(testAPKBytes)) || rel.SHA256 != want {
		t.Fatalf("release = %+v, want code 12, size %d, sha %s", rel, len(testAPKBytes), want)
	}
}

func TestUpdatesDigestTracksAReplacedFile(t *testing.T) {
	dir := t.TempDir()
	u := NewUpdates(dir)
	writeAPK(t, dir, testAPKName, "first")
	first, _ := u.Latest()
	want := writeAPK(t, dir, testAPKName, "second, longer")
	second, err := u.Latest()
	if err != nil || second.SHA256 != want || second.SHA256 == first.SHA256 {
		t.Fatalf("digest after replace = %+v, %v; want %s", second, err, want)
	}
}

func TestUpdatesNoneIsNilNil(t *testing.T) {
	for _, dir := range []string{t.TempDir(), filepath.Join(t.TempDir(), "missing")} {
		u := NewUpdates(dir)
		if rel, err := u.Latest(); rel != nil || err != nil {
			t.Fatalf("Latest(%s) = %+v, %v", dir, rel, err)
		}
		if d, err := u.Open(); d != nil || err != nil {
			t.Fatalf("Open(%s) = %+v, %v", dir, d, err)
		}
	}
}

func TestUpdatesOpenGivesTheFileAndItsSize(t *testing.T) {
	dir := t.TempDir()
	writeAPK(t, dir, testAPKName, testAPKBytes)
	d, err := NewUpdates(dir).Open()
	if err != nil || d == nil {
		t.Fatalf("Open = %+v, %v", d, err)
	}
	defer d.Close()
	body, _ := io.ReadAll(d.Content)
	if string(body) != testAPKBytes || d.Size != int64(len(testAPKBytes)) || d.Name != testAPKName {
		t.Fatalf("download = %q size %d name %q", body, d.Size, d.Name)
	}
}
