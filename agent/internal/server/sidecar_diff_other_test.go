//go:build !unix

package server

import "time"

func setLinkTime(string, time.Time) error { return nil }
