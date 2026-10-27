package main

import (
	"context"
	"log"
	"os/exec"
	"strings"

	"github.com/ZaidQamhieh/rfe/agent/internal/server"
	"github.com/ZaidQamhieh/rfe/agent/internal/store"
)

// notifyPairPrompt shows a desktop notification with Approve/Reject buttons
// (libnotify's notify-send, when installed) and applies the answer.
func notifyPairPrompt(db *store.DB, p server.PairPrompt) {
	bin, err := exec.LookPath("notify-send")
	if err != nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), p.TTL)
	defer cancel()
	body := "A phone on your network wants access to your files.\nMatch code: " + p.SAS + " (must be the same on the phone)"
	if p.Replaces != "" {
		body += "\nThis replaces the already paired device \"" + p.Replaces + "\" and resets its access to browse only."
	}
	out, err := exec.CommandContext(ctx, bin,
		"--app-name=RFE", "--urgency=critical", "--icon=phone",
		"--action=approve=Approve", "--action=reject=Reject",
		"Pair \""+p.Label+"\"?",
		body,
	).Output()
	if err != nil {
		return // timed out, dismissed by the session, or no notification service
	}
	switch strings.TrimSpace(string(out)) {
	case "approve":
		if err := db.DecidePairRequest(p.ID, true); err != nil {
			log.Printf("pair approve: %v", err)
		}
	case "reject":
		if err := db.DecidePairRequest(p.ID, false); err != nil {
			log.Printf("pair reject: %v", err)
		}
	}
}
