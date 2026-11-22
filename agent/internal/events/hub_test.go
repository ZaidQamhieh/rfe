package events

import (
	"reflect"
	"testing"
)

func TestPublishCoalescesAndOrders(t *testing.T) {
	h := NewHub()
	s := h.Subscribe(true)
	for i := 0; i < 10000; i++ {
		h.Publish(KindTransfers)
	}
	h.Publish(KindFiles)
	h.Publish(KindAudit)
	h.Publish("bogus")
	<-s.Wake()
	if got := s.Take(); !reflect.DeepEqual(got, []string{KindAudit, KindTransfers, KindFiles}) {
		t.Fatalf("Take = %v", got)
	}
	if got := s.Take(); got != nil {
		t.Fatalf("second Take = %v, want nothing", got)
	}
}

func TestAdminOnlyKindsSkipOrdinarySubscribers(t *testing.T) {
	h := NewHub()
	s := h.Subscribe(false)
	h.Publish(KindAudit)
	h.Publish(KindDevices)
	select {
	case <-s.Wake():
		t.Fatal("ordinary subscriber was woken by an admin-only kind")
	default:
	}
	h.Publish(KindFiles)
	<-s.Wake()
	if got := s.Take(); !reflect.DeepEqual(got, []string{KindFiles}) {
		t.Fatalf("Take = %v", got)
	}
}

func TestSubscriberCapAndUnsubscribe(t *testing.T) {
	h := NewHub()
	var subs []*Sub
	for i := 0; i < MaxSubscribers; i++ {
		s := h.Subscribe(false)
		if s == nil {
			t.Fatalf("subscriber %d refused", i)
		}
		subs = append(subs, s)
	}
	if h.Subscribe(false) != nil {
		t.Fatal("subscriber past the cap accepted")
	}
	h.Unsubscribe(subs[0])
	if h.Subscribe(false) == nil || h.Count() != MaxSubscribers {
		t.Fatal("slot not freed")
	}
}

func TestCloseEndsDoneAndRefusesSubscribers(t *testing.T) {
	h := NewHub()
	select {
	case <-h.Done():
		t.Fatal("Done closed before Close")
	default:
	}
	h.Close()
	h.Close() // idempotent
	select {
	case <-h.Done():
	default:
		t.Fatal("Done not closed after Close")
	}
	if h.Subscribe(true) != nil {
		t.Fatal("closed hub accepted a subscriber")
	}
}

func TestNilHubPublishIsSafe(t *testing.T) {
	var h *Hub
	h.Publish(KindFiles)
}
