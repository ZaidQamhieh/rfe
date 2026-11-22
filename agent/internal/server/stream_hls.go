package server

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ZaidQamhieh/rfe/agent/internal/fsops"
)

// Adaptive-quality video: GET /v1/stream transcodes a video with ffmpeg into
// HLS (fragmented MP4) at a chosen height. One URL serves three things, told
// apart by query: the playlist (no seg/init), the init segment (init=1) and
// media segment N (seg=N). The playlist is synthesised from the probed
// duration so the player gets a seekable VOD timeline at once; a segment
// request waits for ffmpeg to produce it, and a jump far from what ffmpeg has
// produced restarts it at the requested segment.

const (
	hlsSegSeconds       = 6
	hlsMaxSessions      = 3
	hlsIdleTimeout      = 3 * time.Minute
	hlsSegWait          = 90 * time.Second
	hlsAheadLimit       = 20 // segments ffmpeg may run past the last one requested
	hlsRestartAhead     = 3  // a request this far past the newest segment restarts ffmpeg there
	hlsDefaultQual      = "720p"
	codeTranscodeNA     = "TRANSCODE_UNAVAILABLE"
	codeTranscodeBusy   = "TRANSCODE_BUSY"
	codeTranscodeFailed = "TRANSCODE_FAILED"
	codeNotAVideo       = "NOT_A_VIDEO"
	// featureHLS is the /v1/health feature id advertised while ffmpeg is installed.
	featureHLS = "hlsTranscode"
)

// hlsQualities maps a quality name to its target height in pixels.
var hlsQualities = map[string]int{"480p": 480, "720p": 720, "1080p": 1080, "1440p": 1440, "2160p": 2160}

// hlsBins finds ffmpeg and ffprobe, honouring RFE_FFMPEG / RFE_FFPROBE. Looked
// up per request so installing ffmpeg takes effect without a restart.
func hlsBins() (ffmpeg, ffprobe string, ok bool) {
	find := func(env, name string) string {
		if p := os.Getenv(env); p != "" {
			if _, err := os.Stat(p); err == nil {
				return p
			}
			return ""
		}
		p, err := exec.LookPath(name)
		if err != nil {
			return ""
		}
		return p
	}
	ffmpeg, ffprobe = find("RFE_FFMPEG", "ffmpeg"), find("RFE_FFPROBE", "ffprobe")
	return ffmpeg, ffprobe, ffmpeg != "" && ffprobe != ""
}

type hlsManager struct {
	mu       sync.Mutex
	root     string // created on first use
	sessions map[string]*hlsSession
	seq      int
}

// liveHLS is the manager the running server uses, so shutdown can stop ffmpeg.
var liveHLS atomic.Pointer[hlsManager]

// CloseStreams stops every running transcode and removes its temp files. The
// daemon calls it when the HTTP server shuts down.
func CloseStreams() {
	if m := liveHLS.Load(); m != nil {
		m.closeAll()
	}
}

// closeAll closes every session and the work dir.
func (m *hlsManager) closeAll() {
	m.mu.Lock()
	ss := make([]*hlsSession, 0, len(m.sessions))
	for k, s := range m.sessions {
		ss = append(ss, s)
		delete(m.sessions, k)
	}
	root := m.root
	m.mu.Unlock()
	for _, s := range ss {
		s.close()
	}
	if root != "" {
		_ = os.RemoveAll(root)
	}
}

func newHLSManager() *hlsManager { return &hlsManager{sessions: map[string]*hlsSession{}} }

var (
	errHLSBusy   = errors.New("too many transcodes running")
	errHLSClosed = errors.New("transcode session ended")
)

// get returns the session for key, creating it (and the work dir) when new.
func (m *hlsManager) get(key string) (*hlsSession, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if s := m.sessions[key]; s != nil {
		s.touch()
		return s, nil
	}
	if len(m.sessions) >= hlsMaxSessions {
		return nil, errHLSBusy
	}
	if m.root == "" {
		dir, err := os.MkdirTemp("", "rfe-hls-")
		if err != nil {
			return nil, err
		}
		m.root = dir
	}
	m.seq++
	dir := filepath.Join(m.root, strconv.Itoa(m.seq))
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	s := &hlsSession{dir: dir, last: time.Now(), done: make(chan struct{})}
	m.sessions[key] = s
	go m.reap(key, s)
	return s, nil
}

// reap closes a session after it has been idle for hlsIdleTimeout and, while
// it lives, stops ffmpeg once it runs too far ahead of the player.
func (m *hlsManager) reap(key string, s *hlsSession) {
	t := time.NewTicker(2 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-s.done:
			return
		case <-t.C:
		}
		if s.idleFor() > hlsIdleTimeout {
			m.mu.Lock()
			delete(m.sessions, key)
			m.mu.Unlock()
			s.close()
			return
		}
		s.parkIfAhead()
	}
}

type hlsRun struct {
	dir   string
	start int // first segment this run produces
	cmd   *exec.Cmd
	done  chan struct{} // closed when ffmpeg exits
	err   error         // set before done closes
	kill  atomic.Bool   // we stopped it ourselves
}

type hlsSession struct {
	dir    string
	mu     sync.Mutex
	last   time.Time
	req    int // newest segment index requested
	runs   int
	run    *hlsRun
	dur    float64       // probed duration, seconds; 0 until known
	done   chan struct{} // closed by close; ends the reaper
	closed bool          // set by close: no new ffmpeg may start
}

func (s *hlsSession) touch() {
	s.mu.Lock()
	s.last = time.Now()
	s.mu.Unlock()
}

func (s *hlsSession) idleFor() time.Duration {
	s.mu.Lock()
	defer s.mu.Unlock()
	return time.Since(s.last)
}

// close stops ffmpeg, deletes the session's segment files and marks it closed so
// a request that raced the close cannot start ffmpeg again. Safe to call twice.
func (s *hlsSession) close() {
	s.mu.Lock()
	r := s.run
	s.run = nil
	if !s.closed {
		s.closed = true
		close(s.done)
	}
	s.mu.Unlock()
	if r != nil {
		stopRun(r)
	}
	_ = os.RemoveAll(s.dir)
}

func stopRun(r *hlsRun) {
	r.kill.Store(true)
	if r.cmd.Process != nil {
		_ = r.cmd.Process.Kill()
	}
	<-r.done
}

// parkIfAhead stops ffmpeg when it has produced far more than the player asked
// for; the next request past the end restarts it. This bounds disk and CPU.
func (s *hlsSession) parkIfAhead() {
	s.mu.Lock()
	r, req := s.run, s.req
	s.mu.Unlock()
	if r == nil {
		return
	}
	select {
	case <-r.done:
		return
	default:
	}
	if hi := highestSeg(r.dir); hi > req+hlsAheadLimit {
		stopRun(r)
	}
}

func segFile(dir string, n int) string { return filepath.Join(dir, "seg"+strconv.Itoa(n)+".m4s") }

func segExists(dir string, n int) bool {
	_, err := os.Stat(segFile(dir, n))
	return err == nil
}

// highestSeg is the newest finished segment in dir, or -1.
func highestSeg(dir string) int {
	ents, _ := os.ReadDir(dir)
	hi := -1
	for _, e := range ents {
		n := e.Name()
		if !strings.HasPrefix(n, "seg") || !strings.HasSuffix(n, ".m4s") {
			continue
		}
		if v, err := strconv.Atoi(n[3 : len(n)-4]); err == nil && v > hi {
			hi = v
		}
	}
	return hi
}

// ensureSeg makes sure ffmpeg is (or was) producing segment n, starting or
// restarting it as needed. open supplies a fresh handle on the source file.
func (s *hlsSession) ensureSeg(n int, open func() (hlsInput, error), build func(in hlsInput, outDir string, startSeg int) *exec.Cmd) (*hlsRun, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil, errHLSClosed
	}
	if n > s.req {
		s.req = n
	}
	r := s.run
	if r != nil {
		alive := true
		select {
		case <-r.done:
			alive = false
		default:
		}
		have := segExists(r.dir, n)
		within := n >= r.start && n <= highestSeg(r.dir)+hlsRestartAhead
		if have || (alive && within) {
			return r, nil
		}
		stopRun(r)
		s.req = n // a seek: measure the run-ahead bound from here, not from the furthest segment ever asked
	}
	in, err := open()
	if err != nil {
		return nil, err
	}
	s.runs++
	dir := filepath.Join(s.dir, "r"+strconv.Itoa(s.runs))
	if err := os.MkdirAll(dir, 0o700); err != nil {
		in.close()
		return nil, err
	}
	nr := &hlsRun{dir: dir, start: n, done: make(chan struct{})}
	nr.cmd = build(in, dir, n)
	var stderr bytes.Buffer
	nr.cmd.Stderr = &limitedBuf{b: &stderr, max: 4096}
	if err := nr.cmd.Start(); err != nil {
		in.close()
		return nil, err
	}
	in.close() // the child holds its own descriptor
	go func() {
		err := nr.cmd.Wait()
		if err != nil && !nr.kill.Load() {
			nr.err = fmt.Errorf("ffmpeg: %w: %s", err, strings.TrimSpace(stderr.String()))
		}
		close(nr.done)
	}()
	s.run = nr
	return nr, nil
}

type limitedBuf struct {
	b   *bytes.Buffer
	max int
}

func (l *limitedBuf) Write(p []byte) (int, error) {
	if room := l.max - l.b.Len(); room > 0 {
		l.b.Write(p[:min(len(p), room)])
	}
	return len(p), nil
}

// waitFile waits until name exists in the run's dir, ffmpeg dies without
// producing it, ctx ends, or the wait times out.
func waitFile(ctx context.Context, r *hlsRun, name string) error {
	path := filepath.Join(r.dir, name)
	deadline := time.NewTimer(hlsSegWait)
	defer deadline.Stop()
	tick := time.NewTicker(100 * time.Millisecond)
	defer tick.Stop()
	for {
		if _, err := os.Stat(path); err == nil {
			return nil
		}
		select {
		case <-r.done:
			if _, err := os.Stat(path); err == nil {
				return nil
			}
			if r.err != nil {
				return r.err
			}
			return os.ErrNotExist // ffmpeg finished: no such segment
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline.C:
			return errors.New("timed out waiting for the transcoder")
		case <-tick.C:
		}
	}
}

// hlsArgs builds the ffmpeg command for one run starting at segment startSeg.
func hlsArgs(ffmpeg string, in hlsInput, height int, outDir string, startSeg int) *exec.Cmd {
	ss := float64(startSeg * hlsSegSeconds)
	args := []string{"-hide_banner", "-nostdin", "-loglevel", "error"}
	if startSeg > 0 {
		args = append(args, "-ss", strconv.FormatFloat(ss, 'f', 3, 64))
	}
	args = append(args, "-i", in.arg(),
		"-map", "0:v:0", "-map", "0:a:0?", "-sn", "-dn",
		"-vf", fmt.Sprintf("scale=-2:min(%d\\,ih),format=yuv420p", height),
		"-c:v", "libx264", "-preset", "veryfast", "-crf", "23",
		"-force_key_frames", fmt.Sprintf("expr:gte(t,n_forced*%d)", hlsSegSeconds),
		"-sc_threshold", "0",
		"-c:a", "aac", "-b:a", "128k", "-ac", "2",
	)
	if startSeg > 0 {
		args = append(args, "-output_ts_offset", strconv.FormatFloat(ss, 'f', 3, 64))
	}
	args = append(args,
		"-f", "hls", "-hls_time", strconv.Itoa(hlsSegSeconds),
		"-hls_segment_type", "fmp4", "-hls_fmp4_init_filename", "init.mp4",
		"-hls_flags", "temp_file+independent_segments",
		"-hls_playlist_type", "vod",
		"-start_number", strconv.Itoa(startSeg),
		"-hls_segment_filename", filepath.Join(outDir, "seg%d.m4s"),
		filepath.Join(outDir, "index.m3u8"),
	)
	cmd := exec.Command(ffmpeg, args...)
	in.attach(cmd)
	return cmd
}

// probeDuration returns the media duration in seconds via ffprobe.
func probeDuration(ctx context.Context, ffprobe string, in hlsInput) (float64, error) {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, ffprobe, "-v", "error", "-show_entries", "format=duration", "-of", "csv=p=0", in.arg())
	in.attach(cmd)
	out, err := cmd.Output()
	if err != nil {
		return 0, err
	}
	d, err := strconv.ParseFloat(strings.TrimSpace(string(out)), 64)
	if err != nil || d <= 0 {
		return 0, errors.New("no duration")
	}
	return d, nil
}

func hlsPlaylist(path, quality string, dur float64) string {
	base := "path=" + url.QueryEscape(path) + "&quality=" + quality
	var b strings.Builder
	b.WriteString("#EXTM3U\n#EXT-X-VERSION:7\n")
	fmt.Fprintf(&b, "#EXT-X-TARGETDURATION:%d\n#EXT-X-PLAYLIST-TYPE:VOD\n#EXT-X-INDEPENDENT-SEGMENTS\n#EXT-X-MEDIA-SEQUENCE:0\n", hlsSegSeconds)
	fmt.Fprintf(&b, "#EXT-X-MAP:URI=\"?%s&init=1\"\n", base)
	n := int(dur / hlsSegSeconds)
	if float64(n*hlsSegSeconds) < dur {
		n++
	}
	for i := 0; i < n; i++ {
		d := float64(hlsSegSeconds)
		if i == n-1 {
			d = dur - float64(i*hlsSegSeconds)
		}
		fmt.Fprintf(&b, "#EXTINF:%.3f,\n?%s&seg=%d\n", d, base, i)
	}
	b.WriteString("#EXT-X-ENDLIST\n")
	return b.String()
}

// streamHandler serves GET /v1/stream. 501 TRANSCODE_UNAVAILABLE when ffmpeg or
// ffprobe is not installed on the PC.
func streamHandler(baseOps *fsops.Ops, mgr *hlsManager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ops := opsFromContext(r.Context(), baseOps)
		q := r.URL.Query()
		path := q.Get("path")
		if path == "" {
			writeError(w, http.StatusBadRequest, codeBadRequest, pathRequiredMsg)
			return
		}
		quality := q.Get("quality")
		if quality == "" {
			quality = hlsDefaultQual
		}
		height, okQ := hlsQualities[quality]
		if !okQ {
			writeError(w, http.StatusBadRequest, codeBadRequest, "quality must be one of 480p, 720p, 1080p, 1440p, 2160p")
			return
		}
		seg, isSeg := -1, q.Has("seg")
		if isSeg {
			v, err := strconv.Atoi(q.Get("seg"))
			if err != nil || v < 0 || v > 100000 {
				writeError(w, http.StatusBadRequest, codeBadRequest, "seg must be a non-negative integer")
				return
			}
			seg = v
		}
		ffmpeg, ffprobe, ok := hlsBins()
		if !ok {
			writeError(w, http.StatusNotImplemented, codeTranscodeNA, "video transcoding needs ffmpeg and ffprobe installed on this PC")
			return
		}
		// Open first: it enforces the jail and answers 404/422 for a bad path.
		f, err := ops.Open(path)
		if err != nil {
			if errors.Is(err, fsops.ErrForbidden) || errors.Is(err, fsops.ErrNotRegular) {
				handleFsError(w, err)
			} else if os.IsNotExist(err) {
				writeError(w, http.StatusNotFound, "PATH_NOT_FOUND", "file not found")
			} else {
				writeInternal(w, "stream", err)
			}
			return
		}
		info, err := f.Stat()
		if err != nil || info.IsDir() {
			f.Close()
			writeError(w, http.StatusUnprocessableEntity, "NOT_A_FILE", "path is not a regular file")
			return
		}
		f.Close()

		key := fmt.Sprintf("%s|%d|%d|%s", path, info.Size(), info.ModTime().UnixNano(), quality)
		sess, err := mgr.get(key)
		if err != nil {
			if errors.Is(err, errHLSBusy) {
				w.Header().Set("Retry-After", "5")
				writeError(w, http.StatusTooManyRequests, codeTranscodeBusy, err.Error())
				return
			}
			writeInternal(w, "stream", err)
			return
		}
		open := func() (hlsInput, error) { return openHLSInput(ops, path) }
		build := func(in hlsInput, dir string, start int) *exec.Cmd { return hlsArgs(ffmpeg, in, height, dir, start) }

		isInit := q.Get("init") == "1"
		sess.mu.Lock()
		dur := sess.dur
		sess.mu.Unlock()
		if dur == 0 {
			in, err := open()
			if err != nil {
				writeInternal(w, "stream", err)
				return
			}
			dur, err = probeDuration(r.Context(), ffprobe, in)
			in.close()
			if err != nil {
				writeError(w, http.StatusUnprocessableEntity, codeNotAVideo, "the file could not be read as a video")
				return
			}
			sess.mu.Lock()
			sess.dur = dur
			sess.mu.Unlock()
		}
		if !isSeg && !isInit {
			w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
			w.Header().Set("Cache-Control", "no-store")
			_, _ = io.WriteString(w, hlsPlaylist(path, quality, dur))
			return
		}

		if isSeg && float64(seg*hlsSegSeconds) >= dur {
			writeError(w, http.StatusNotFound, "PATH_NOT_FOUND", "no such segment")
			return
		}
		want, name := seg, ""
		if isInit {
			want, name = 0, "init.mp4"
			sess.mu.Lock()
			if sess.run != nil {
				want = sess.run.start
			}
			sess.mu.Unlock()
		} else {
			name = "seg" + strconv.Itoa(seg) + ".m4s"
		}
		var run *hlsRun
		// A request that seeks elsewhere kills the run this one waits on: ask
		// again once instead of answering 404 for a valid segment.
		for attempt := 0; ; attempt++ {
			run, err = sess.ensureSeg(want, open, build)
			if err != nil {
				break
			}
			// ffmpeg creates init.mp4 before it is complete; the first segment's
			// arrival means the init segment is final.
			gate := name
			if isInit {
				gate = "seg" + strconv.Itoa(run.start) + ".m4s"
			}
			err = waitFile(r.Context(), run, gate)
			if err == nil && isInit {
				err = waitFile(r.Context(), run, name)
			}
			if errors.Is(err, os.ErrNotExist) && run.kill.Load() && attempt < 2 {
				continue
			}
			break
		}
		if errors.Is(err, errHLSClosed) {
			w.Header().Set("Retry-After", "1")
			writeError(w, http.StatusTooManyRequests, codeTranscodeBusy, err.Error())
			return
		}
		if err != nil {
			writeInternal(w, "stream", err)
			return
		}
		if err != nil {
			switch {
			case errors.Is(err, os.ErrNotExist):
				writeError(w, http.StatusNotFound, "PATH_NOT_FOUND", "no such segment")
			case errors.Is(err, context.Canceled):
			default:
				log.Printf("stream: %v", err)
				writeError(w, http.StatusBadGateway, codeTranscodeFailed, "the transcoder failed")
			}
			return
		}
		sf, err := os.Open(filepath.Join(run.dir, name))
		if err != nil {
			writeInternal(w, "stream", err)
			return
		}
		defer sf.Close()
		w.Header().Set("Content-Type", "video/mp4")
		w.Header().Set("Cache-Control", "no-store")
		http.ServeContent(w, r, name, time.Time{}, sf)
	}
}
