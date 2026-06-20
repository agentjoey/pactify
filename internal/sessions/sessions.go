package sessions

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// Runner runs an external command in working directory dir and returns combined
// output. dir matters because some agent session CLIs (opencode) scope their
// session store to the cwd — listing/deleting must run in the project (or
// worktree) the agent worked in, or they target the wrong store. An empty dir
// inherits the caller's cwd. Injected so tests fake the agent CLIs without
// spawning processes.
type Runner func(dir, name string, args ...string) (string, error)

// Spec declares how to manage one agent kind's sessions. Command is the CLI
// binary; List lists sessions; Prune deletes them in bulk; Delete deletes one
// session. IndexDelete marks a CLI whose delete takes a per-LISTING index rather
// than a stable id (gemini): such a kind supports bulk prune (loop delete from the
// highest index down) but NOT targeted CleanupByTitle (no stable id / pact title).
// An empty field means that capability is unverified for the kind (treated as
// unsupported — a graceful no-op, never an error).
type Spec struct {
	Command     string
	List        []string
	Prune       []string
	Delete      []string
	IndexDelete bool
}

// specs holds the per-kind session commands, filled from real `--help` + live
// probing (2026-06-15, gemini/kimi re-probed 2026-06-17).
//   - opencode: list + delete-by-id, AND sessions carry the `pact:<seat>` title the
//     runner stamps → the only kind with verified TARGETED cleanup (CleanupByTitle).
//     It's also the one most worth cleaning (persistent daemon, heavy session DB).
//   - gemini-cli: `--list-sessions` + `--delete-session <INDEX>` (no stable id, no
//     pact title) → bulk prune only (IndexDelete), no targeted cleanup.
//   - kimi: NO headless list/delete CLI and no --title flag (re-probed against
//     kimi-code 0.18, 2026-06-20). Sessions live as files under ~/.kimi-code/
//     sessions/<wd>/<session>/state.json (indexed in ~/.kimi-code/session_index.jsonl).
//     It is therefore absent from this CLI-command map and cleaned at the filesystem
//     level instead — see CleanupKimiSeat, which matches the seat marker the briefing
//     leaves in each session's title.
//   - claude/codex: no verified headless prune/delete; intentionally absent.
var specs = map[string]Spec{
	"opencode":   {Command: "opencode", List: []string{"session", "list"}, Delete: []string{"session", "delete"}},
	"gemini-cli": {Command: "gemini", List: []string{"--list-sessions"}, Delete: []string{"--delete-session"}, IndexDelete: true},
}

// sessionIDRe matches an opencode session id (the first column of `session list`).
var sessionIDRe = regexp.MustCompile(`ses_[A-Za-z0-9]+`)

// geminiIndexRe matches the leading "  N." listing index in `gemini --list-sessions`.
var geminiIndexRe = regexp.MustCompile(`(?m)^\s*(\d+)\.`)

// SessionTag is the per-seat title an orchestrated agent stamps on its session so
// the driver can find and delete exactly its own sessions later. Kept here as the
// single source of truth shared by the runner (tags at launch) and CleanupByTitle
// (matches at cleanup).
func SessionTag(seat string) string { return "pact:" + seat }

// Manager prunes/lists sessions via an injected Runner. Dir is the working
// directory the session CLI runs in (the agent's repo/worktree); empty inherits
// the caller's cwd.
type Manager struct {
	Run Runner
	Dir string
}

// Supported reports whether kind has any known session command.
func Supported(kind string) bool {
	s, ok := specs[kind]
	return ok && s.Command != ""
}

// CanPrune reports whether kind has a verified bulk-prune capability — either a
// dedicated Prune command, or list + index-delete (gemini).
func CanPrune(kind string) bool {
	s, ok := specs[kind]
	if !ok {
		return false
	}
	return len(s.Prune) > 0 || (s.IndexDelete && len(s.List) > 0 && len(s.Delete) > 0)
}

// CanCleanup reports whether kind supports TARGETED cleanup (list + delete-by-id +
// pact title matching) — the capability CleanupByTitle needs. Index-delete kinds
// (gemini) are excluded: they have no stable id / pact title to match on.
func CanCleanup(kind string) bool {
	s, ok := specs[kind]
	return ok && s.Command != "" && len(s.List) > 0 && len(s.Delete) > 0 && !s.IndexDelete
}

// List returns the kind's session listing (or an error if unsupported).
func (m Manager) List(kind string) (string, error) {
	s, ok := specs[kind]
	if !ok || s.Command == "" || len(s.List) == 0 {
		return "", fmt.Errorf("sessions: listing not supported for kind %q", kind)
	}
	return m.Run(m.Dir, s.Command, s.List...)
}

// Prune deletes the kind's sessions in bulk. For an index-delete kind (gemini) it
// lists then deletes every session from the highest index down (so each delete
// doesn't shift the indices still to be removed). When the kind has no verified
// prune capability it returns (skipped=true, nil) — a graceful no-op, NOT an error.
func (m Manager) Prune(kind string) (output string, skipped bool, err error) {
	s, ok := specs[kind]
	if !ok || s.Command == "" {
		return "", true, nil
	}
	if s.IndexDelete && len(s.List) > 0 && len(s.Delete) > 0 {
		return m.pruneByIndex(s)
	}
	if len(s.Prune) == 0 {
		return "", true, nil
	}
	out, err := m.Run(m.Dir, s.Command, s.Prune...)
	return out, false, err
}

// pruneByIndex lists sessions, parses their listing indices, and deletes them from
// the highest index down (a delete shifts only HIGHER indices, so descending order
// keeps the remaining targets valid). Best-effort: returns the first delete error.
func (m Manager) pruneByIndex(s Spec) (string, bool, error) {
	out, err := m.Run(m.Dir, s.Command, s.List...)
	if err != nil {
		return "", false, fmt.Errorf("sessions: list: %w", err)
	}
	var idx []int
	for _, mm := range geminiIndexRe.FindAllStringSubmatch(out, -1) {
		if n, e := strconv.Atoi(mm[1]); e == nil {
			idx = append(idx, n)
		}
	}
	sort.Sort(sort.Reverse(sort.IntSlice(idx)))
	var firstErr error
	deleted := 0
	for _, n := range idx {
		args := append(append([]string{}, s.Delete...), strconv.Itoa(n))
		if _, derr := m.Run(m.Dir, s.Command, args...); derr != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("sessions: delete index %d: %w", n, derr)
			}
			continue
		}
		deleted++
	}
	return fmt.Sprintf("pruned %d session(s)", deleted), false, firstErr
}

// CleanupByTitle deletes every session of kind whose listing row contains tag —
// used to close exactly the sessions a finished task's agent created (tagged via
// SessionTag). Best-effort: it deletes every match it can, returning the deleted
// ids and the FIRST delete error (if any). Kinds without list+delete-by-id support
// — including index-delete kinds (gemini) that have no stable id / pact title —
// return (nil, skipped=true, nil), a graceful no-op.
func (m Manager) CleanupByTitle(kind, tag string) (deleted []string, skipped bool, err error) {
	s, ok := specs[kind]
	if !ok || s.Command == "" || len(s.List) == 0 || len(s.Delete) == 0 || s.IndexDelete {
		return nil, true, nil
	}
	out, listErr := m.Run(m.Dir, s.Command, s.List...)
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
		if _, derr := m.Run(m.Dir, s.Command, args...); derr != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("sessions: delete %s: %w", id, derr)
			}
			continue
		}
		deleted = append(deleted, id)
	}
	return deleted, false, firstErr
}

// kimiSeatMarker is the substring a pact briefing leaves in a kimi session's
// title. The worker/reviewer briefs both open with
// "# pact {worker,reviewer} — seat `<seat>`", and kimi (which has no --title flag)
// adopts that first line as the session title — so the backtick-wrapped seat is a
// stable, pact-specific key that never matches a user's own kimi session. Matching
// on the title (not the recorded workDir) also sidesteps symlink-resolution
// mismatches (kimi stores /private/tmp where the repo path is /tmp).
func kimiSeatMarker(seat string) string { return "seat `" + seat + "`" }

// KimiHome returns the kimi-code data dir (~/.kimi-code), overridable in tests.
// kimi-code 0.18 keeps sessions under <home>/sessions/<wd>/<session>/state.json
// (state.json carries the prompt-derived `title`) and indexes them in
// <home>/session_index.jsonl. It has no --title flag and no delete CLI, so its
// sessions can only be closed at the filesystem level (see CleanupKimiSeat).
var KimiHome = func() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".kimi-code")
}

// IsKimi reports whether kind is the kimi runner — the one kind whose sessions are
// cleaned by file ops (CleanupKimiSeat) rather than a session CLI.
func IsKimi(kind string) bool { return kind == "kimi-cli" }

// CleanupKimiSeat removes the kimi-code session directories a pact seat created
// under home, identified by the seat marker the briefing stamps into each
// session's title, and prunes their lines from session_index.jsonl. kimi has no
// headless list/delete command, so this is the only way to close them.
// Best-effort: it deletes every match it can, returning the deleted session ids
// (the session_<uuid> dir names) and the first removal error. A missing home is a
// graceful no-op (nil, nil).
func CleanupKimiSeat(home, seat string) (deleted []string, err error) {
	marker := kimiSeatMarker(seat)
	matches, _ := filepath.Glob(filepath.Join(home, "sessions", "*", "*", "state.json"))
	var removedDirs []string
	for _, sj := range matches {
		b, e := os.ReadFile(sj)
		if e != nil {
			continue
		}
		var st struct {
			Title string `json:"title"`
		}
		if json.Unmarshal(b, &st) != nil || !strings.Contains(st.Title, marker) {
			continue
		}
		dir := filepath.Dir(sj) // the session_<uuid> dir
		if e := os.RemoveAll(dir); e != nil {
			if err == nil {
				err = fmt.Errorf("sessions: remove kimi session %s: %w", filepath.Base(dir), e)
			}
			continue
		}
		deleted = append(deleted, filepath.Base(dir))
		removedDirs = append(removedDirs, dir)
	}
	if len(removedDirs) > 0 {
		pruneKimiIndex(filepath.Join(home, "session_index.jsonl"), removedDirs)
	}
	return deleted, err
}

// pruneKimiIndex rewrites session_index.jsonl, dropping the lines whose sessionDir
// is one of removedDirs. Best-effort: a missing or unwritable index is ignored
// (the session files are already gone — a stale index line resolves to nothing).
func pruneKimiIndex(path string, removedDirs []string) {
	b, e := os.ReadFile(path)
	if e != nil {
		return
	}
	gone := make(map[string]bool, len(removedDirs))
	for _, d := range removedDirs {
		gone[d] = true
	}
	var keep []string
	for _, line := range strings.Split(string(b), "\n") {
		if line == "" {
			continue
		}
		var e struct {
			SessionDir string `json:"sessionDir"`
		}
		if json.Unmarshal([]byte(line), &e) == nil && gone[e.SessionDir] {
			continue
		}
		keep = append(keep, line)
	}
	out := strings.Join(keep, "\n")
	if out != "" {
		out += "\n"
	}
	_ = os.WriteFile(path, []byte(out), 0o644)
}
