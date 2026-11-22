// Package events is the agent's push channel: a hub that tells connected
// clients "something of this kind changed" so they refetch instead of polling.
//
// An event carries a kind and a counter, never a path, name or token: the
// client re-reads through the normal authorized endpoints, so the channel can
// not leak what those endpoints would refuse. Memory is bounded by design:
// each subscriber holds at most one pending flag per kind (a burst of 10 000
// chunk uploads is one "transfers" event), and the number of subscribers is
// capped.
package events

import (
	"sync"
	"sync/atomic"
)

// Kinds a client can receive.
const (
	KindHello     = "hello"     // sent once on connect: refresh everything
	KindAudit     = "audit"     // an audit entry was recorded (admin only)
	KindDevices   = "devices"   // a device or pairing request changed (admin only)
	KindTransfers = "transfers" // a transfer session changed
	KindFiles     = "files"     // files were created, changed, moved or deleted
	KindSettings  = "settings"  // agent settings changed
)

// MaxSubscribers caps concurrent streams; a further Subscribe returns nil.
const MaxSubscribers = 64

// MaxPerOwner caps the streams one device may hold, so a single paired device
// can not take every slot.
const MaxPerOwner = 8

// kinds lists the publishable kinds in delivery order.
var kinds = []string{KindAudit, KindDevices, KindTransfers, KindFiles, KindSettings}

// AdminOnly reports whether a kind is delivered to administrators only.
func AdminOnly(kind string) bool { return kind == KindAudit || kind == KindDevices }

// Hub fans events out to subscribers.
type Hub struct {
	mu   sync.Mutex
	subs map[*Sub]struct{}
	seq  atomic.Uint64
	// done is closed by Close so every stream can end on shutdown.
	done      chan struct{}
	closeOnce sync.Once
}

// NewHub returns an empty hub.
func NewHub() *Hub { return &Hub{subs: map[*Sub]struct{}{}, done: make(chan struct{})} }

// Close tells every subscriber to finish (see Done) and refuses new ones. It is
// safe to call more than once.
func (h *Hub) Close() { h.closeOnce.Do(func() { close(h.done) }) }

// Done is closed once the hub is closed.
func (h *Hub) Done() <-chan struct{} { return h.done }

// Sub is one subscriber. Its pending set is a bitmask, so it can not grow.
type Sub struct {
	admin   bool
	owner   string
	mu      sync.Mutex
	pending uint32
	wake    chan struct{}
}

// Subscribe registers a subscriber, or returns nil when the cap is reached or
// the hub is closed.
// admin selects whether admin-only kinds are delivered.
func (h *Hub) Subscribe(admin bool) *Sub { return h.SubscribeAs(admin, "") }

// SubscribeAs is Subscribe for a named owner (a device id); an owner holds at
// most MaxPerOwner streams. An empty owner is not limited.
func (h *Hub) SubscribeAs(admin bool, owner string) *Sub {
	select {
	case <-h.done:
		return nil
	default:
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.subs) >= MaxSubscribers {
		return nil
	}
	if owner != "" {
		n := 0
		for o := range h.subs {
			if o.owner == owner {
				n++
			}
		}
		if n >= MaxPerOwner {
			return nil
		}
	}
	s := &Sub{admin: admin, owner: owner, wake: make(chan struct{}, 1)}
	h.subs[s] = struct{}{}
	return s
}

// Unsubscribe removes s.
func (h *Hub) Unsubscribe(s *Sub) {
	h.mu.Lock()
	delete(h.subs, s)
	h.mu.Unlock()
}

// Count is the number of subscribers.
func (h *Hub) Count() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.subs)
}

// Publish marks kind pending on every eligible subscriber. It never blocks.
// Unknown kinds are ignored. A nil hub is a no-op.
func (h *Hub) Publish(kind string) {
	if h == nil {
		return
	}
	bit := bitOf(kind)
	if bit == 0 {
		return
	}
	h.seq.Add(1)
	h.mu.Lock()
	defer h.mu.Unlock()
	for s := range h.subs {
		if AdminOnly(kind) && !s.admin {
			continue
		}
		s.mu.Lock()
		s.pending |= bit
		s.mu.Unlock()
		select {
		case s.wake <- struct{}{}:
		default:
		}
	}
}

// Wake signals that Take has something to return.
func (s *Sub) Wake() <-chan struct{} { return s.wake }

// Take returns and clears the pending kinds, in a fixed order.
func (s *Sub) Take() []string {
	s.mu.Lock()
	p := s.pending
	s.pending = 0
	s.mu.Unlock()
	var out []string
	for _, k := range kinds {
		if p&bitOf(k) != 0 {
			out = append(out, k)
		}
	}
	return out
}

// Seq is the number of events published so far (the SSE id counter).
func (h *Hub) Seq() uint64 { return h.seq.Load() }

func bitOf(kind string) uint32 {
	for i, k := range kinds {
		if k == kind {
			return 1 << uint(i)
		}
	}
	return 0
}
