package server

import (
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"time"

	"github.com/ZaidQamhieh/rfe/agent/internal/store"
)

// wolRateLimitAttempts is the per-device Wake-on-LAN budget per minute.
const wolRateLimitAttempts = 10

const (
	wolMACLen        = 6
	wolMACRepeats    = 16
	wolSyncByte      = 0xFF
	wolSyncLen       = 6
	wolPacketLen     = wolSyncLen + wolMACRepeats*wolMACLen
	wolBroadcastAddr = "255.255.255.255:9"
	wolSendFailedMsg = "failed to send wake packet"
)

// wolRelayHandler broadcasts a Wake-on-LAN magic packet. Read-only devices
// are refused, and so is any device without the per-device wake grant
// (capWake: off for a newly code-paired device, on for an owner login device
// and for devices that existed before the grant was introduced). It is not
// admin-only because the phone app's ordinary paired token is never "admin".
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
		if !requireDeviceFileCapabilities(w, r, capWake) {
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
		if err != nil || len(mac) != wolMACLen {
			writeError(w, http.StatusBadRequest, "INVALID_MAC", fmt.Sprintf("MAC must be %d-byte colon-separated", wolMACLen))
			return
		}

		// Build magic packet: wolSyncLen×0xFF + wolMACRepeats×MAC.
		var packet [wolPacketLen]byte
		for i := 0; i < wolSyncLen; i++ {
			packet[i] = wolSyncByte
		}
		for i := 0; i < wolMACRepeats; i++ {
			copy(packet[wolSyncLen+i*wolMACLen:], mac)
		}

		conn, err := net.Dial("udp4", wolBroadcastAddr)
		if err != nil {
			// Log the real dial error; the client gets the stable code only,
			// not the host's socket/permission details.
			log.Printf("wol: dial: %v", err)
			writeError(w, http.StatusInternalServerError, "WOL_FAILED", wolSendFailedMsg)
			return
		}
		defer conn.Close()
		if _, err := conn.Write(packet[:]); err != nil {
			log.Printf("wol: write: %v", err)
			writeError(w, http.StatusInternalServerError, "WOL_FAILED", wolSendFailedMsg)
			return
		}
		if db != nil {
			audit(db, r, store.AuditWake, mac.String(), "")
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "sent"})
	}
}
