package daemon

import (
	"context"
	"os"
	"strings"
	"sync/atomic"
	"time"

	"github.com/ZaidQamhieh/rfe/agent/internal/next/search"
	"github.com/ZaidQamhieh/rfe/agent/internal/next/thumbs"
	"github.com/ZaidQamhieh/rfe/agent/internal/sidecar"
)

// Sidecar settings: the environment names, the manifest a release package
// writes next to the executables, and the short names of the two sidecars.
const (
	envSidecars = "RFE_SIDECARS"

	sidecarFilePrefix = "rfe-"

	sidecarIndexd = sidecar.IndexdName
	sidecarThumbd = "thumbd"

	opIndexBuild      = "index.build"
	indexMaxEntries   = sidecar.IndexMaxEntries
	indexMaxBytes     = sidecar.IndexMaxEstimatedBytes
	indexBuildTimeout = 30 * time.Minute
)

// Words of RFE_SIDECARS: the values that switch every sidecar off, and those
// that switch every sidecar on.
var (
	sidecarOffWords = []string{"off", "none", "0", "false"}
	sidecarAllWords = []string{"all", "1", "true"}
)

// locateSidecar is sidecar.Locate; see there.
func locateSidecar(short string) (sidecar.Located, bool, error) { return sidecar.Locate(short) }

// containsWord reports whether any comma-separated item of raw is in words.
func containsWord(raw string, words ...string) bool {
	for _, v := range strings.Split(raw, ",") {
		v = strings.TrimSpace(v)
		for _, w := range words {
			if v == w {
				return true
			}
		}
	}
	return false
}

// sidecarEnabled decides whether to use the sidecar "short". RFE_SIDECARS lists
// short names (or "all") to turn on and "off" to turn everything off. Left unset,
// a sidecar is used only when a release package installed it, verified.
func sidecarEnabled(short string) bool {
	raw := strings.ToLower(strings.TrimSpace(os.Getenv(envSidecars)))
	if raw == "" {
		loc, ok, _ := locateSidecar(short)
		return ok && loc.Verified
	}
	if containsWord(raw, sidecarOffWords...) {
		return false
	}
	return containsWord(raw, append([]string{short}, sidecarAllWords...)...)
}

// sidecarConfig is the supervisor configuration for "short" when it is
// installed and, if a manifest vouches for it, intact. Without a manifest it is
// allowed only as a development build (see sidecar.LocateToRun). The config carries
// the verified hash so every restart re-checks the file. A refusal is logged.
func sidecarConfig(short string, logf func(string, ...any)) (sidecar.Config, bool) {
	name := sidecarFilePrefix + short
	explicit := strings.TrimSpace(os.Getenv(envSidecars)) != ""
	loc, ok, err := sidecar.LocateToRun(short, explicit)
	if err != nil {
		logf("sidecar %s refused: %v", name, err)
		sidecar.Note(name + ".refused")
		return sidecar.Config{}, false
	}
	if !ok {
		return sidecar.Config{}, false
	}
	cfg := sidecar.Config{Name: name, Path: loc.Path, Logf: logf}
	if loc.Verified {
		cfg.ExpectVersion = loc.Version
		cfg.SHA256 = loc.SHA256
	}
	return cfg, true
}

// sidecarSet supervises the optional rfe-indexd and rfe-thumbd processes. A
// sidecar that is off or missing leaves its field nil and the Go code serves.
type sidecarSet struct {
	indexSup *sidecar.Supervisor
	thumbSup *sidecar.Supervisor
	roots    func() []string
	logf     func(string, ...any)

	building atomic.Bool
	again    atomic.Bool
}

// startSidecars starts the sidecars that are enabled. roots gives the folders
// the search index covers.
func startSidecars(roots func() []string, logf func(string, ...any)) *sidecarSet {
	s := &sidecarSet{roots: roots, logf: logf}
	if sidecarEnabled(sidecarIndexd) {
		if cfg, ok := sidecarConfig(sidecarIndexd, logf); ok {
			s.indexSup = sidecar.NewSupervisor(cfg, func(*sidecar.Client) { s.rebuildIndex() })
			s.indexSup.Run()
		}
	}
	if sidecarEnabled(sidecarThumbd) {
		if cfg, ok := sidecarConfig(sidecarThumbd, logf); ok {
			s.thumbSup = sidecar.NewSupervisor(cfg, nil)
			s.thumbSup.Run()
		}
	}
	return s
}

// renderer returns the thumbnail renderer backed by rfe-thumbd, or nil.
func (s *sidecarSet) renderer() thumbs.Renderer {
	if s.thumbSup == nil {
		return nil
	}
	return thumbs.SidecarRenderer{Caller: s.thumbSup}
}

// indexer returns the name-search index backed by rfe-indexd, or nil.
func (s *sidecarSet) indexer() search.Indexer {
	if s.indexSup == nil {
		return nil
	}
	return search.SidecarIndexer{Caller: s.indexSup}
}

// buildRoots are the folders the index covers: the allowed folders, else the home folder.
func (s *sidecarSet) buildRoots() []string {
	if r := s.roots(); len(r) > 0 {
		return r
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		return []string{home}
	}
	return nil
}

// rebuildIndex asks the indexer to rebuild over the current folders in the
// background; a request that arrives during a build runs one more build after it.
func (s *sidecarSet) rebuildIndex() {
	if s.indexSup == nil {
		return
	}
	if !s.building.CompareAndSwap(false, true) {
		s.again.Store(true)
		return
	}
	go func() {
		defer s.building.Store(false)
		for {
			s.again.Store(false)
			s.buildIndexOnce()
			if !s.again.Load() {
				return
			}
		}
	}()
}

// buildIndexOnce runs one index.build call.
func (s *sidecarSet) buildIndexOnce() {
	type buildReq struct {
		Roots      []string `json:"roots"`
		MaxEntries int      `json:"maxEntries"`
		MaxBytes   int64    `json:"maxBytes"`
	}
	var resp struct {
		Entries   int  `json:"entries"`
		Truncated bool `json:"truncated"`
	}
	ctx, cancel := context.WithTimeout(context.Background(), indexBuildTimeout)
	defer cancel()
	err := s.indexSup.Call(ctx, opIndexBuild, buildReq{Roots: s.buildRoots(), MaxEntries: indexMaxEntries, MaxBytes: indexMaxBytes}, &resp)
	switch {
	case err != nil:
		s.logf("search index: build failed: %v", err)
	case resp.Truncated:
		s.logf("search index: truncated at %d entries; the rest is not indexed", resp.Entries)
	}
}

// stop ends both supervisors and their processes.
func (s *sidecarSet) stop() {
	if s.indexSup != nil {
		s.indexSup.Stop()
	}
	if s.thumbSup != nil {
		s.thumbSup.Stop()
	}
}
