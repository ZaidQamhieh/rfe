package server

import (
	"encoding/json"
	"log"
	"net"
	"net/http"
	"time"

	"github.com/ZaidQamhieh/rfe/agent/internal/store"
)

// wolRateLimitAttempts is the per-device Wake-on-LAN budget per minute.
const wolRateLimitAttempts = 10

// wolRelayHandler broadcasts a Wake-on-LAN magic packet. Guest/read-only
// devices are refused (PR-61): the phone app's normal paired token is never
// "admin" (that's reserved for the separate web-login session), so gating
// this to admin-only would disable WoL for every ordinary pairing — the
// read-only flag is what actually distinguishes a lower-trust guest pairing
// (MintGuest forces it) from a normal full-access paired phone, which is
// the real, intended caller here and keeps working unchanged.
func wolRelayHandler(db *store.DB) http.HandlerFunc {
	// Each device may send a handful of packets a minute: enough to wake a
	// machine, not enough to use the agent as a LAN broadcast amplifier.
	limiter := newKeyedLimiter(wolRateLimitAttempts, time.Minute)
	return func(w http.ResponseWriter, r *http.Request) {
		cur := deviceFromContext(r)
		if cur == nil || cur.ReadOnly {
			writeError(w, http.StatusForbidden, "FORBIDDEN", "this device is not permitted to send Wake-on-LAN")
			return
		}
		if !limiter.Allow(cur.ID) {
			writeError(w, http.StatusTooManyRequests, "RATE_LIMITED", "too many Wake-on-LAN requests, try again in a minute")
			return
		}
		var body struct {
			MAC string `json:"mac"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxJSONBody)).Decode(&body); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_BODY", "expected {\"mac\": \"aa:bb:cc:dd:ee:ff\"}")
			return
		}
		mac, err := net.ParseMAC(body.MAC)
		if err != nil || len(mac) != 6 {
			writeError(w, http.StatusBadRequest, "INVALID_MAC", "MAC must be 6-byte colon-separated")
			return
		}

		// Build magic packet: 6×0xFF + 16×MAC.
		var packet [102]byte
		for i := 0; i < 6; i++ {
			packet[i] = 0xFF
		}
		for i := 0; i < 16; i++ {
			copy(packet[6+i*6:], mac)
		}

		conn, err := net.Dial("udp4", "255.255.255.255:9")
		if err != nil {
			// Log the real dial error; the client gets the stable code only,
			// not the host's socket/permission details (PR-53).
			log.Printf("wol: dial: %v", err)
			writeError(w, http.StatusInternalServerError, "WOL_FAILED", "failed to send wake packet")
			return
		}
		defer conn.Close()
		if _, err := conn.Write(packet[:]); err != nil {
			log.Printf("wol: write: %v", err)
			writeError(w, http.StatusInternalServerError, "WOL_FAILED", "failed to send wake packet")
			return
		}
		if db != nil {
			audit(db, r, store.AuditWake, mac.String(), "")
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "sent"})
	}
}
