//go:build !linux

package main

import (
	"github.com/ZaidQamhieh/rfe/agent/internal/server"
	"github.com/ZaidQamhieh/rfe/agent/internal/store"
)

// notifyPairPrompt has no desktop notification on this platform; the request is
// answered with `rfe pair accept|reject` or an admin session.
func notifyPairPrompt(*store.DB, server.PairPrompt) {}
