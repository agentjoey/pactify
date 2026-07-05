package orchestrate

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// writeEscalation records a human-readable escalation under
// <dir>/.pact/orchestrate/escalation-<feature>-<task>-<ts>.md (feature/task
// segments dropped when empty — see escalationFilename) and returns the
// written path.
//
// The driver escalates (rather than terminates) when a task cannot converge —
// the rework limit is hit or an agent fails repeatedly. A human reads this
// record, fixes the underlying issue, and resumes with `pactify orchestrate
// --resume`.
//
// ts is supplied by the caller (e.g. "20260613-140530") rather than read from
// time.Now inside the package, keeping this function pure and deterministically
// testable. The orchestrate directory is created with os.MkdirAll if absent.
//
// Naming the feature/task in the filename (not just the timestamp) matters in
// practice: a repo accumulates escalations across many runs and days, and a
// bare `escalation-<ts>.md` gives a human (or another agent) zero way to tell
// "is this about MY current run?" without opening every file — precisely how a
// days-old, unrelated escalation got mistaken for the current run's state
// during the 2026-07-05 dogfood (P1). Filtering/archiving by feature (see
// archiveEscalationsForFeature) becomes possible once the name carries it.
func writeEscalation(dir, ts, feature, task, reason, evidence, suggestion string) (path string, err error) {
	outDir := filepath.Join(dir, ".pact", "orchestrate")
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return "", fmt.Errorf("create orchestrate dir: %w", err)
	}

	path = filepath.Join(outDir, escalationFilename(feature, task, ts))
	content := fmt.Sprintf(`# Escalation %s

The orchestrate driver could not converge this task and has paused for human
intervention. Fix the underlying issue, then resume with `+"`pactify orchestrate --resume`"+`.

## Feature

%s

## Task

%s

## Reason

%s

## Evidence

%s

## Suggestion

%s
`, ts, feature, task, reason, evidence, suggestion)

	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return "", fmt.Errorf("write escalation file: %w", err)
	}
	return path, nil
}

// escalationSlugRe strips anything that isn't filesystem-safe from a feature/
// task id before it goes into a filename (protocol ids are slugs already; this
// is belt-and-suspenders, never a validation gate).
var escalationSlugRe = regexp.MustCompile(`[^a-zA-Z0-9._-]+`)

func escalationSlug(s string) string { return escalationSlugRe.ReplaceAllString(s, "_") }

// escalationFilename builds the on-disk name: feature and task segments are
// included when present (most escalations have both; a feature-level one like
// "iteration limit exceeded" or "hard gate failed" has no single task) so a
// human scanning `.pact/orchestrate/` can tell which run/feature an escalation
// is about without opening it.
func escalationFilename(feature, task, ts string) string {
	name := "escalation"
	if feature != "" {
		name += "-" + escalationSlug(feature)
	}
	if task != "" {
		name += "-" + escalationSlug(task)
	}
	return name + "-" + ts + ".md"
}

// archiveEscalationsForFeature moves feature's own escalation files out of the
// live `.pact/orchestrate/` directory into an `archive/` subdirectory once the
// feature ships — a resolved escalation for a shipped feature is no longer
// actionable, and leaving it sitting alongside live escalations for OTHER
// features is exactly what caused a days-old, unrelated escalation to be
// mistaken for the current run's state (2026-07-05 dogfood P1). Best-effort and
// silent: archiving is housekeeping, never something that should fail a merge.
//
// Matching is by the file's `## Feature` CONTENT section (exact id match), NOT
// by filename prefix: feature and task ids both contain hyphens, so a filename
// like "escalation-f-old-t9-<ts>.md" is ambiguous between (feature "f", task
// "old-t9") and (feature "f-old", task "t9") — a prefix match for feature "f"
// would wrongly sweep feature "f-old"'s files (caught in the 2026-07-05
// post-fix review). Files without a Feature section (pre-P1 records, or
// feature-less escalations) are never auto-archived — leaving debris is safe,
// archiving someone else's live escalation is not.
func archiveEscalationsForFeature(dir, feature string) {
	if feature == "" {
		return
	}
	outDir := filepath.Join(dir, ".pact", "orchestrate")
	entries, err := os.ReadDir(outDir)
	if err != nil {
		return
	}
	archiveDir := filepath.Join(outDir, "archive")
	for _, e := range entries {
		if e.IsDir() || !strings.HasPrefix(e.Name(), "escalation-") {
			continue
		}
		p := filepath.Join(outDir, e.Name())
		b, rerr := os.ReadFile(p)
		if rerr != nil || escalationFeatureOf(string(b)) != feature {
			continue
		}
		if err := os.MkdirAll(archiveDir, 0o755); err != nil {
			return
		}
		_ = os.Rename(p, filepath.Join(archiveDir, e.Name()))
	}
}

// escalationFeatureOf extracts the feature id from an escalation record's
// `## Feature` section ("" when absent — pre-P1 records have no such section).
func escalationFeatureOf(content string) string {
	const header = "## Feature\n"
	i := strings.Index(content, header)
	if i < 0 {
		return ""
	}
	rest := content[i+len(header):]
	// The section body is the first non-empty line before the next section.
	for _, line := range strings.Split(rest, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "##") {
			return "" // empty section (feature-less escalation)
		}
		return line
	}
	return ""
}

// Notifier abstracts how an escalation reaches a human. The loop layer injects a
// concrete notifier; tests inject a fake that records messages. A nil-safe
// silent fallback is the caller's responsibility (the loop may pass nil when no
// notifier is configured).
type Notifier interface {
	Notify(message string)
}

// StdoutNotifier is the default production Notifier: it prints the escalation
// message to stdout. Desktop/other channels can be added later behind the same
// interface.
type StdoutNotifier struct{}

// Notify writes the message to stdout.
func (StdoutNotifier) Notify(message string) {
	fmt.Println(message)
}
