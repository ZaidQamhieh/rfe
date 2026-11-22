package daemon

import (
	"errors"
	"slices"
	"sync"

	"github.com/ZaidQamhieh/rfe/agent/internal/next/files"
	"github.com/ZaidQamhieh/rfe/agent/internal/next/rootfs"
	"github.com/ZaidQamhieh/rfe/agent/internal/next/settings"
	"github.com/ZaidQamhieh/rfe/agent/internal/next/share"
	"github.com/ZaidQamhieh/rfe/agent/internal/next/store"
)

// errHubClosed is returned for a request that arrives after shutdown began.
var errHubClosed = errors.New("daemon: the file layer is shut down")

// viewKey names one caller view of the disk: the folder it is confined to ("" for
// none) and whether it may only read.
type viewKey struct {
	jail     string
	readOnly bool
}

// generation is everything built on one set of allowed folders. A change of the
// folders in the settings makes a new generation; the old one stays open until
// shutdown because requests may still be using it.
type generation struct {
	base  *rootfs.Set
	roots []string
	share *share.Service
	sets  map[string]*rootfs.Set // jail folder -> Set narrowed to it
	views map[viewKey]*files.Service
}

// close releases every root and trash folder the generation opened.
func (g *generation) close() {
	for _, v := range g.views {
		_ = v.Close()
	}
	for _, s := range g.sets {
		_ = s.Close()
	}
	_ = g.base.Close()
}

// hub owns the folder sets and the per-caller file services built on them, and
// rebuilds them when the owner changes the allowed folders in the settings.
// Narrowed sets and file services are cached per jail so a request never opens
// a new file handle of its own.
type hub struct {
	db        *store.DB
	settings  *settings.Service
	protected []string
	trashDir  string
	logf      func(format string, args ...any)

	mu      sync.Mutex
	cur     *generation
	retired []*generation
	closed  bool
}

// newHub opens the first generation from the current settings.
func newHub(db *store.DB, st *settings.Service, protected []string, trashDir string, logf func(string, ...any)) (*hub, error) {
	h := &hub{db: db, settings: st, protected: protected, trashDir: trashDir, logf: logf}
	g, err := h.open(st.Get().Roots)
	if err != nil {
		return nil, err
	}
	h.cur = g
	return h, nil
}

// open builds a generation for roots; an empty list is unrestricted access,
// which the settings only produce on an explicit owner choice.
func (h *hub) open(roots []string) (*generation, error) {
	opts := rootfs.Options{Protected: h.protected}
	var base *rootfs.Set
	var err error
	if len(roots) == 0 {
		base, err = rootfs.Unrestricted(opts)
	} else {
		base, err = rootfs.New(roots, opts)
	}
	if err != nil {
		return nil, err
	}
	return &generation{
		base:  base,
		roots: slices.Clone(roots),
		share: share.New(share.Config{
			Set:     base,
			Store:   h.db,
			Sharing: func() bool { return h.settings.Get().AllowSharing },
		}),
		sets:  map[string]*rootfs.Set{},
		views: map[viewKey]*files.Service{},
	}, nil
}

// onSettings is the settings subscriber: when the allowed folders changed it
// switches to a new generation, and keeps the old one when the new folders
// cannot be opened.
func (h *hub) onSettings(ch settings.Change) {
	if slices.Equal(ch.Old.Roots, ch.New.Roots) {
		return
	}
	g, err := h.open(ch.New.Roots)
	if err != nil {
		h.logf("settings: the new folders cannot be opened, keeping the old ones: %v", err)
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		g.close()
		return
	}
	h.retired = append(h.retired, h.cur)
	h.cur = g
}

// current returns the live generation.
func (h *hub) current() (*generation, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return nil, errHubClosed
	}
	return h.cur, nil
}

// setLocked returns the Set for jail (the base Set when jail is empty).
func (h *hub) setLocked(g *generation, jail string) (*rootfs.Set, error) {
	if jail == "" {
		return g.base, nil
	}
	if s, ok := g.sets[jail]; ok {
		return s, nil
	}
	s, err := g.base.Narrow(jail)
	if err != nil {
		return nil, err
	}
	g.sets[jail] = s
	return s, nil
}

// setFor returns the folders a caller confined to jail may reach.
func (h *hub) setFor(jail string) (*rootfs.Set, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return nil, errHubClosed
	}
	return h.setLocked(h.cur, jail)
}

// filesFor returns the file service for a caller confined to jail, read-only or
// not, with the trash configured. The service is cached per view.
func (h *hub) filesFor(jail string, readOnly bool) (*files.Service, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return nil, errHubClosed
	}
	g := h.cur
	key := viewKey{jail: jail, readOnly: readOnly}
	if s, ok := g.views[key]; ok {
		return s, nil
	}
	set, err := h.setLocked(g, jail)
	if err != nil {
		return nil, err
	}
	if readOnly {
		set = set.ReadOnly()
	}
	svc := files.New(set)
	if err := svc.WithTrash(h.trashDir); err != nil {
		return nil, err
	}
	g.views[key] = svc
	return svc, nil
}

// validateJail checks that root is a folder inside the allowed folders and
// returns its normalised path.
func (h *hub) validateJail(root string) (string, error) {
	g, err := h.current()
	if err != nil {
		return "", err
	}
	handle, err := g.base.Open(root)
	if err != nil {
		return "", err
	}
	narrowed, err := g.base.Narrow(root)
	if err != nil {
		return "", err
	}
	_ = narrowed.Close()
	return handle.Path(), nil
}

// close releases every generation; later calls fail with errHubClosed.
func (h *hub) close() {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return
	}
	h.closed = true
	for _, g := range h.retired {
		g.close()
	}
	h.cur.close()
}
