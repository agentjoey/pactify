package sessions

import (
	"fmt"
	"regexp"
	"strings"
)

// Runner runs an external command and returns combined output. Injected so tests
// fake the agent CLIs without spawning processes.
type Runner func(name string, args ...string) (string, error)

// Spec declares how to manage one agent kind's sessions. Command is the CLI
// binary; List lists sessions; Prune deletes them in bulk; Delete deletes one
// session (its id is appended). An empty field means that capability is
// unverified for the kind (treated as unsupported — a graceful no-op, never an
// error).
type Spec struct {
	Command string
	List    []string
	Prune   []string
	Delete  []string
}

// specs holds the per-kind session commands, filled from real `--help` probing
// (2026-06-15). Only opencode has a verified list + delete-by-id pair — and it's
// also the kind worth cleaning (a persistent daemon with a heavy session DB).
// gemini lists but its delete is per-INDEX (no stable id), so it carries List
// only. Others (claude/kimi/codex) have no verified headless prune/delete and
// are intentionally absent.
var specs = map[string]Spec{
	"opencode":   {Command: "opencode", List: []string{"session", "list"}, Delete: []string{"session", "delete"}},
	"gemini-cli": {Command: "gemini", List: []string{"--list-sessions"}},
}

// sessionIDRe matches an opencode session id (the first column of `session list`).
var sessionIDRe = regexp.MustCompile(`ses_[A-Za-z0-9]+`)

// SessionTag is the per-seat title an orchestrated agent stamps on its session so
// the driver can find and delete exactly its own sessions later. Kept here as the
// single source of truth shared by the runner (tags at launch) and CleanupByTitle
// (matches at cleanup).
func SessionTag(seat string) string { return "pact:" + seat }

// Manager prunes/lists sessions via an injected Runner.
type Manager struct{ Run Runner }

// Supported reports whether kind has any known session command.
func Supported(kind string) bool {
	s, ok := specs[kind]
	return ok && s.Command != ""
}

// CanPrune reports whether kind has a verified bulk-prune command.
func CanPrune(kind string) bool {
	s, ok := specs[kind]
	return ok && len(s.Prune) > 0
}

// CanCleanup reports whether kind supports targeted cleanup (list + delete-by-id),
// the capability CleanupByTitle needs.
func CanCleanup(kind string) bool {
	s, ok := specs[kind]
	return ok && s.Command != "" && len(s.List) > 0 && len(s.Delete) > 0
}

// List returns the kind's session listing (or an error if unsupported).
func (m Manager) List(kind string) (string, error) {
	s, ok := specs[kind]
	if !ok || s.Command == "" || len(s.List) == 0 {
		return "", fmt.Errorf("sessions: listing not supported for kind %q", kind)
	}
	return m.Run(s.Command, s.List...)
}

// Prune deletes the kind's sessions. When the kind has no verified prune command
// it returns (skipped=true, nil) — a graceful no-op, NOT an error — so callers can
// report "nothing to prune (unsupported)" instead of failing.
func (m Manager) Prune(kind string) (output string, skipped bool, err error) {
	s, ok := specs[kind]
	if !ok || s.Command == "" || len(s.Prune) == 0 {
		return "", true, nil
	}
	out, err := m.Run(s.Command, s.Prune...)
	return out, false, err
}

// CleanupByTitle deletes every session of kind whose listing row contains tag —
// used to close exactly the sessions a finished task's agent created (tagged via
// SessionTag). Best-effort: it deletes every match it can, returning the deleted
// ids and the FIRST delete error (if any). Kinds without list+delete support
// return (nil, skipped=true, nil) — a graceful no-op.
func (m Manager) CleanupByTitle(kind, tag string) (deleted []string, skipped bool, err error) {
	s, ok := specs[kind]
	if !ok || s.Command == "" || len(s.List) == 0 || len(s.Delete) == 0 {
		return nil, true, nil
	}
	out, listErr := m.Run(s.Command, s.List...)
	if listErr != nil {
		return nil, false, fmt.Errorf("sessions: list %s: %w", kind, listErr)
	}
	var firstErr error
	for _, line := range strings.Split(out, "\n") {
		if tag == "" || !strings.Contains(line, tag) {
			continue
		}
		id := sessionIDRe.FindString(line)
		if id == "" {
			continue
		}
		args := append(append([]string{}, s.Delete...), id)
		if _, derr := m.Run(s.Command, args...); derr != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("sessions: delete %s: %w", id, derr)
			}
			continue
		}
		deleted = append(deleted, id)
	}
	return deleted, false, firstErr
}
