package main

import (
	"log"

	"github.com/ZaidQamhieh/rfe/agent/internal/server"
	"github.com/ZaidQamhieh/rfe/agent/internal/store"
)

// pairPromptHandler announces an approve-on-PC pairing request to whoever is at
// the computer. The request stays answerable through `rfe pair accept`
// and admin sessions whether or not a desktop notification could be shown.
func pairPromptHandler(db *store.DB) func(server.PairPrompt) {
	return func(p server.PairPrompt) {
		log.Printf("pair request from %q (%s), match code %s — `rfe pair accept %s` or `pair reject %s`", p.Label, p.IP, p.SAS, p.ID[:8], p.ID[:8])
		if p.Replaces != "" {
			log.Printf("pair request %s would replace the paired device %q and reset its access to browse only", p.ID[:8], p.Replaces)
		}
		go notifyPairPrompt(db, p)
	}
}
