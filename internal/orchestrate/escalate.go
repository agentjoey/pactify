package orchestrate

import (
	"fmt"
	"os"
	"path/filepath"
)

// writeEscalation records a human-readable escalation under
// <dir>/.pact/orchestrate/escalation-<ts>.md and returns the written path.
//
// The driver escalates (rather than terminates) when a task cannot converge —
// the rework limit is hit or an agent fails repeatedly. A human reads this
// record, fixes the underlying issue, and resumes with `pactify orchestrate
// --resume`.
//
// ts is supplied by the caller (e.g. "20260613-140530") rather than read from
// time.Now inside the package, keeping this function pure and deterministically
// testable. The orchestrate directory is created with os.MkdirAll if absent.
func writeEscalation(dir, ts, task, reason, evidence, suggestion string) (path string, err error) {
	outDir := filepath.Join(dir, ".pact", "orchestrate")
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return "", fmt.Errorf("create orchestrate dir: %w", err)
	}

	path = filepath.Join(outDir, "escalation-"+ts+".md")
	content := fmt.Sprintf(`# Escalation %s

The orchestrate driver could not converge this task and has paused for human
intervention. Fix the underlying issue, then resume with `+"`pactify orchestrate --resume`"+`.

## Task

%s

## Reason

%s

## Evidence

%s

## Suggestion

%s
`, ts, task, reason, evidence, suggestion)

	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return "", fmt.Errorf("write escalation file: %w", err)
	}
	return path, nil
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
