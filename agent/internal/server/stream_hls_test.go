package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
)

func streamGet(h http.HandlerFunc, query string) *httptest.ResponseRecorder {
	rr := httptest.NewRecorder()
	h(rr, httptest.NewRequest(http.MethodGet, "/v1/stream?"+query, nil))
	return rr
}

func TestStream_NoFFmpegIs501(t *testing.T) {
	t.Setenv("RFE_FFMPEG", filepath.Join(t.TempDir(), "missing"))
	ops, root := newFsFixture(t)
	rr := streamGet(streamHandler(ops, newHLSManager()), "path="+url.QueryEscape(filepath.Join(root, "a.txt")))
	if rr.Code != http.StatusNotImplemented || !strings.Contains(rr.Body.String(), codeTranscodeNA) {
		t.Fatalf("want 501 %s, got %d %s", codeTranscodeNA, rr.Code, rr.Body)
	}
}

func TestStream_BadRequests(t *testing.T) {
	ops, root := newFsFixture(t)
	h := streamHandler(ops, newHLSManager())
	p := url.QueryEscape(filepath.Join(root, "a.txt"))
	for name, q := range map[string]string{
		"no path":     "",
		"bad quality": "path=" + p + "&quality=8k",
		"bad seg":     "path=" + p + "&seg=-1",
		"seg text":    "path=" + p + "&seg=x",
	} {
		if rr := streamGet(h, q); rr.Code != http.StatusBadRequest {
			t.Errorf("%s: want 400, got %d %s", name, rr.Code, rr.Body)
		}
	}
}

// fakeBins points the handler at stand-in ffmpeg/ffprobe so path checks run.
func fakeBins(t *testing.T) {
	t.Helper()
	d := t.TempDir()
	for _, n := range []string{"ffmpeg", "ffprobe"} {
		p := filepath.Join(d, n)
		if err := os.WriteFile(p, []byte("#!/bin/sh\nexit 1\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("RFE_FFMPEG", filepath.Join(d, "ffmpeg"))
	t.Setenv("RFE_FFPROBE", filepath.Join(d, "ffprobe"))
}

func TestStream_PathErrors(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("needs sh")
	}
	fakeBins(t)
	ops, root := newFsFixture(t)
	h := streamHandler(ops, newHLSManager())
	if rr := streamGet(h, "path="+url.QueryEscape(filepath.Join(root, "nope.mp4"))); rr.Code != http.StatusNotFound {
		t.Errorf("missing file: want 404, got %d %s", rr.Code, rr.Body)
	}
	if rr := streamGet(h, "path="+url.QueryEscape(filepath.Join(root, "sub"))); rr.Code != http.StatusUnprocessableEntity {
		t.Errorf("directory: want 422, got %d %s", rr.Code, rr.Body)
	}
	outside := filepath.Join(t.TempDir(), "x.mp4")
	_ = os.WriteFile(outside, []byte("x"), 0o644)
	if rr := streamGet(h, "path="+url.QueryEscape(outside)); rr.Code != http.StatusForbidden {
		t.Errorf("outside the jail: want 403, got %d %s", rr.Code, rr.Body)
	}
	// An unreadable "video" is a 422, not a 500.
	if rr := streamGet(h, "path="+url.QueryEscape(filepath.Join(root, "a.txt"))); rr.Code != http.StatusUnprocessableEntity {
		t.Errorf("not a video: want 422, got %d %s", rr.Code, rr.Body)
	}
}

func TestHLSPlaylist(t *testing.T) {
	pl := hlsPlaylist("/a b/v.mp4", "720p", 14)
	for _, want := range []string{
		"#EXT-X-PLAYLIST-TYPE:VOD", "#EXT-X-ENDLIST",
		`#EXT-X-MAP:URI="?path=%2Fa+b%2Fv.mp4&quality=720p&init=1"`,
		"#EXTINF:6.000,\n?path=%2Fa+b%2Fv.mp4&quality=720p&seg=0\n",
		"#EXTINF:2.000,\n?path=%2Fa+b%2Fv.mp4&quality=720p&seg=2\n",
	} {
		if !strings.Contains(pl, want) {
			t.Errorf("playlist lacks %q:\n%s", want, pl)
		}
	}
	if strings.Contains(pl, "seg=3") {
		t.Errorf("too many segments:\n%s", pl)
	}
}

func TestHLSManagerLimit(t *testing.T) {
	m := newHLSManager()
	for i := 0; i < hlsMaxSessions; i++ {
		s, err := m.get("k" + strconv.Itoa(i))
		if err != nil {
			t.Fatal(err)
		}
		defer s.close()
	}
	if _, err := m.get("extra"); err != errHLSBusy {
		t.Fatalf("want errHLSBusy, got %v", err)
	}
	if _, err := m.get("k0"); err != nil {
		t.Fatalf("existing session must stay reachable: %v", err)
	}
}

// Real ffmpeg: playlist, init, a first segment and a seek jump, checked with ffprobe.
func TestStream_RealFFmpeg(t *testing.T) {
	ffmpeg, ffprobe, ok := hlsBins()
	if !ok {
		t.Skip("ffmpeg/ffprobe not installed")
	}
	ops, root := newFsFixture(t)
	src := filepath.Join(root, "clip.mp4")
	gen := exec.Command(ffmpeg, "-loglevel", "error", "-f", "lavfi", "-i", "testsrc=size=640x360:rate=24",
		"-f", "lavfi", "-i", "sine=frequency=440", "-t", "14", "-c:v", "libx264", "-c:a", "aac", "-shortest", src)
	if out, err := gen.CombinedOutput(); err != nil {
		t.Fatalf("generate clip: %v %s", err, out)
	}
	mgr := newHLSManager()
	h := streamHandler(ops, mgr)
	base := "path=" + url.QueryEscape(src) + "&quality=480p"

	pl := streamGet(h, base)
	if pl.Code != 200 || strings.Count(pl.Body.String(), "#EXTINF") != 3 {
		t.Fatalf("playlist: %d %s", pl.Code, pl.Body)
	}
	if ct := pl.Header().Get("Content-Type"); ct != "application/vnd.apple.mpegurl" {
		t.Errorf("playlist type %q", ct)
	}
	init := streamGet(h, base+"&init=1")
	seg0 := streamGet(h, base+"&seg=0")
	if init.Code != 200 || seg0.Code != 200 || init.Body.Len() == 0 || seg0.Body.Len() == 0 {
		t.Fatalf("init %d/%d bytes, seg0 %d/%d bytes", init.Code, init.Body.Len(), seg0.Code, seg0.Body.Len())
	}
	// Jump straight to the last segment: must restart ffmpeg there, not wait on 0..1.
	seg2 := streamGet(h, base+"&seg=2")
	if seg2.Code != 200 {
		t.Fatalf("seg2: %d %s", seg2.Code, seg2.Body)
	}
	joined := filepath.Join(t.TempDir(), "s2.mp4")
	_ = os.WriteFile(joined, append(append([]byte{}, init.Body.Bytes()...), seg2.Body.Bytes()...), 0o644)
	out, err := exec.Command(ffprobe, "-v", "error", "-select_streams", "v:0", "-show_entries", "stream=height,start_time", "-of", "csv=p=0", joined).Output()
	if err != nil {
		t.Fatalf("ffprobe seg2: %v", err)
	}
	f := strings.Split(strings.TrimSpace(string(out)), ",")
	st, _ := strconv.ParseFloat(f[1], 64)
	if f[0] != "360" || st < 11.5 || st > 12.5 {
		t.Errorf("seg2 should be 360p starting near 12s, got %q", out)
	}
	if rr := streamGet(h, base+"&seg=9"); rr.Code != http.StatusNotFound {
		t.Errorf("segment past the end: want 404, got %d", rr.Code)
	}
	for _, s := range mgr.sessions {
		s.close()
	}
}

// stubBins installs a stand-in ffprobe (prints a 14 s duration) and ffmpeg (writes
// init.mp4 plus segments from -start_number to 2 next to its output playlist).
func stubBins(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("needs sh")
	}
	d := t.TempDir()
	probe := "#!/bin/sh\necho 14.000\n"
	enc := `#!/bin/sh
start=0; prev=""
for a in "$@"; do
  [ "$prev" = "-start_number" ] && start=$a
  prev=$a; last=$a
done
dir=$(dirname "$last")
printf init > "$dir/init.mp4"
i=$start
while [ "$i" -lt 3 ]; do printf "seg$i" > "$dir/seg$i.m4s"; i=$((i+1)); done
`
	for n, body := range map[string]string{"ffprobe": probe, "ffmpeg": enc} {
		if err := os.WriteFile(filepath.Join(d, n), []byte(body), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("RFE_FFMPEG", filepath.Join(d, "ffmpeg"))
	t.Setenv("RFE_FFPROBE", filepath.Join(d, "ffprobe"))
}

func closeAll(m *hlsManager) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, s := range m.sessions {
		s.close()
	}
}

func TestStream_StubTranscodeFlow(t *testing.T) {
	stubBins(t)
	ops, root := newFsFixture(t)
	mgr := newHLSManager()
	t.Cleanup(func() { closeAll(mgr) })
	h := streamHandler(ops, mgr)
	base := "path=" + url.QueryEscape(filepath.Join(root, "a.txt")) + "&quality=1080p"

	pl := streamGet(h, base)
	if pl.Code != 200 || strings.Count(pl.Body.String(), "#EXTINF") != 3 {
		t.Fatalf("playlist: %d %s", pl.Code, pl.Body)
	}
	if rr := streamGet(h, base+"&init=1"); rr.Code != 200 || rr.Body.String() != "init" {
		t.Fatalf("init: %d %q", rr.Code, rr.Body)
	}
	if rr := streamGet(h, base+"&seg=1"); rr.Code != 200 || rr.Body.String() != "seg1" {
		t.Fatalf("seg1: %d %q", rr.Code, rr.Body)
	}
	if rr := streamGet(h, base+"&seg=3"); rr.Code != http.StatusNotFound {
		t.Fatalf("seg past the end: want 404, got %d", rr.Code)
	}
	if ct := streamGet(h, base+"&seg=0").Header().Get("Content-Type"); ct != "video/mp4" {
		t.Errorf("segment type %q", ct)
	}
}

// A request far from where ffmpeg has produced restarts it at that segment.
func TestStream_StubSeekRestarts(t *testing.T) {
	stubBins(t)
	ops, root := newFsFixture(t)
	mgr := newHLSManager()
	t.Cleanup(func() { closeAll(mgr) })
	h := streamHandler(ops, mgr)
	base := "path=" + url.QueryEscape(filepath.Join(root, "a.txt"))
	if rr := streamGet(h, base+"&seg=2"); rr.Code != 200 || rr.Body.String() != "seg2" {
		t.Fatalf("seg2: %d %q", rr.Code, rr.Body)
	}
	for _, s := range mgr.sessions {
		if s.run == nil || s.run.start != 2 {
			t.Fatalf("ffmpeg should have started at segment 2: %+v", s.run)
		}
	}
}

func TestStream_TooManyTranscodesIs429(t *testing.T) {
	stubBins(t)
	ops, root := newFsFixture(t)
	mgr := newHLSManager()
	t.Cleanup(func() { closeAll(mgr) })
	h := streamHandler(ops, mgr)
	p := url.QueryEscape(filepath.Join(root, "a.txt"))
	for _, q := range []string{"480p", "720p", "1080p"} {
		if rr := streamGet(h, "path="+p+"&quality="+q); rr.Code != 200 {
			t.Fatalf("%s: %d %s", q, rr.Code, rr.Body)
		}
	}
	rr := streamGet(h, "path="+p+"&quality=1440p")
	if rr.Code != http.StatusTooManyRequests || rr.Header().Get("Retry-After") == "" {
		t.Fatalf("want 429 + Retry-After, got %d %v", rr.Code, rr.Header())
	}
}

func TestHLSSessionCloseRemovesFilesAndRefusesNewRuns(t *testing.T) {
	stubBins(t)
	ops, root := newFsFixture(t)
	m := newHLSManager()
	s, err := m.get("k")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(m.root) })
	ffmpeg, _, _ := hlsBins()
	open := func() (hlsInput, error) { return openHLSInput(ops, filepath.Join(root, "a.txt")) }
	build := func(in hlsInput, dir string, start int) *exec.Cmd { return hlsArgs(ffmpeg, in, 480, dir, start) }
	run, err := s.ensureSeg(0, open, build)
	if err != nil {
		t.Fatal(err)
	}
	if err := waitFile(t.Context(), run, "seg0.m4s"); err != nil {
		t.Fatal(err)
	}
	s.close()
	if _, err := os.Stat(s.dir); !os.IsNotExist(err) {
		t.Fatalf("session dir should be gone: %v", err)
	}
	if _, err := s.ensureSeg(1, open, build); err == nil {
		t.Fatal("a closed session must not start ffmpeg again")
	}
	if _, err := os.Stat(s.dir); !os.IsNotExist(err) {
		t.Fatalf("closed session recreated its dir: %v", err)
	}
}

func TestHealthFeatures_HLSOnlyWithFFmpeg(t *testing.T) {
	features := func() []string {
		db, st, token := newAuthTestDeps(t)
		req := httptest.NewRequest(http.MethodGet, "/v1/health", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		rr := httptest.NewRecorder()
		healthHandler(Config{Settings: st}, db)(rr, req)
		var resp struct {
			Features []string `json:"features"`
		}
		if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
			t.Fatal(err)
		}
		return resp.Features
	}
	t.Setenv("RFE_FFMPEG", filepath.Join(t.TempDir(), "missing"))
	if slices.Contains(features(), featureHLS) {
		t.Fatal("hlsTranscode advertised without ffmpeg")
	}
	stubBins(t)
	got := features()
	if !slices.Contains(got, featureHLS) || !slices.IsSorted(got) {
		t.Fatalf("want sorted features with %s, got %v", featureHLS, got)
	}
}
