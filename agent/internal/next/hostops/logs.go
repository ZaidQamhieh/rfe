// Package hostops holds the host-side operations behind the admin and utility
// routes of the new agent: reading the agent's own log, restarting its service,
// sending a Wake-on-LAN packet and serving the newest Android APK. Each
// operation talks to the machine through a small injectable seam (a command
// runner, a packet sender, a directory), so tests never touch the real host.
package hostops

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Journal settings. AgentUnit is the systemd user unit the agent runs as.
const (
	AgentUnit       = "rfe.service"
	DefaultLogLines = 200 // the old agent's fixed tail, and the default of ?limit
	MaxLogLines     = 1000
	journalCommand  = "journalctl"
	journalTimeout  = 10 * time.Second
	journalFormat   = "short-iso"
	// minLogFields is how many parts a journal line has when it carries a
	// timestamp and a rest.
	minLogFields = 2
)

// ErrLogsUnavailable means the log source could not be read (no journald, or
// the agent is not running as a unit). The route answers an empty list.
var ErrLogsUnavailable = errors.New("agent log is not available on this host")

// goLogPrefix matches the "2006/01/02 15:04:05 " stamp Go's log package puts in
// front of each message; the journal already carries its own timestamp.
var goLogPrefix = regexp.MustCompile(`^\d{4}/\d{2}/\d{2} \d{2}:\d{2}:\d{2} `)

// LogLine is one line of the agent's log (openapi LogLine).
type LogLine struct {
	TS      string `json:"ts"`
	Message string `json:"message"`
}

// Runner runs a command and returns its standard output.
type Runner func(ctx context.Context, name string, args ...string) ([]byte, error)

// execRunner runs the command for real.
func execRunner(ctx context.Context, name string, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, name, args...).Output()
}

// Journal reads the agent's own output from the systemd user journal.
type Journal struct {
	// Run executes journalctl; nil runs the real command.
	Run Runner
	// Unit is the unit to read; empty means AgentUnit.
	Unit string
}

// NormalizeLimit returns n when it is a usable line count and DefaultLogLines
// otherwise (zero, negative or above MaxLogLines), the way GET /audit treats an
// out-of-range page size.
func NormalizeLimit(n int) int {
	if n < 1 || n > MaxLogLines {
		return DefaultLogLines
	}
	return n
}

// Tail returns the newest limit lines of the unit's journal, oldest first. The
// command gets a deadline, so a wedged journald cannot pin the caller. Any
// failure to read is ErrLogsUnavailable (wrapping the cause for the log).
func (j Journal) Tail(ctx context.Context, limit int) ([]LogLine, error) {
	limit = NormalizeLimit(limit)
	run := j.Run
	if run == nil {
		run = execRunner
	}
	unit := j.Unit
	if unit == "" {
		unit = AgentUnit
	}
	ctx, cancel := context.WithTimeout(ctx, journalTimeout)
	defer cancel()
	raw, err := run(ctx, journalCommand, "--user", "-u", unit, "-n", strconv.Itoa(limit), "--no-pager", "-o", journalFormat)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrLogsUnavailable, err)
	}
	text := strings.TrimRight(string(raw), "\n")
	if text == "" {
		return []LogLine{}, nil
	}
	lines := strings.Split(text, "\n")
	if len(lines) > limit {
		lines = lines[len(lines)-limit:]
	}
	out := make([]LogLine, 0, len(lines))
	for _, line := range lines {
		ts, msg := splitLogLine(line)
		out = append(out, LogLine{TS: ts, Message: msg})
	}
	return out, nil
}

// splitLogLine parses a `short-iso` journal line
// ("2006-01-02T15:04:05+ZZ:ZZ host rfe[pid]: message") into its timestamp and
// message, dropping the "host rfe[pid]: " prefix and Go's own date stamp. A line
// with no space in it becomes a message with no timestamp.
func splitLogLine(line string) (ts, msg string) {
	fields := strings.SplitN(line, " ", minLogFields)
	if len(fields) < minLogFields {
		return "", line
	}
	rest := fields[1]
	if i := strings.Index(rest, ": "); i >= 0 {
		rest = rest[i+2:]
	}
	return fields[0], goLogPrefix.ReplaceAllString(rest, "")
}
