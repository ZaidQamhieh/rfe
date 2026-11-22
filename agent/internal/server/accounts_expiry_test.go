package server

import (
	"net/http"
	"testing"
	"time"

	"github.com/ZaidQamhieh/rfe/agent/internal/store"
)

func TestFederatedSession_ExpiredTokenGets401AuditedOnceAndLoginRenews(t *testing.T) {
	f := newFederation(t)
	tok := fedSession(t, f)
	if rr := accCall(f.member, http.MethodGet, "/v1/transfers/list", tok, ""); rr.Code != http.StatusOK {
		t.Fatalf("live federated token = %d", rr.Code)
	}
	f.memberDB.SetFederatedSessionTTL(-time.Hour)
	for i := 0; i < 2; i++ {
		if rr := accCall(f.member, http.MethodGet, "/v1/transfers/list", tok, ""); rr.Code != http.StatusUnauthorized {
			t.Fatalf("expired federated token = %d, want 401: %s", rr.Code, rr.Body.String())
		}
	}
	f.memberDB.SetFederatedSessionTTL(store.FederatedSessionTTL)
	var n int
	for _, e := range auditEntries(t, f.memberDB) {
		if e.Action == store.AuditFederatedSessionExpired {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("%d expiry audit rows, want 1", n)
	}
	fresh := fedSession(t, f)
	if rr := accCall(f.member, http.MethodGet, "/v1/transfers/list", fresh, ""); rr.Code != http.StatusOK {
		t.Fatalf("token after a fresh assertion = %d", rr.Code)
	}
}
