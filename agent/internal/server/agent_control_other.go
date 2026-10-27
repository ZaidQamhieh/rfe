//go:build !linux

package server

func restartSupported() bool { return false }

func restartAgent() error { return nil }
