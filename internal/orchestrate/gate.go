// Package orchestrate drives the pact state machine: it headlessly invokes the
// owner/reviewer agents at each transition and merges a feature only after an
// independent hard test gate passes. This file implements that gate plus the
// extraction of a machine-readable verify command from a task spec.
package orchestrate

import (
	"context"
	"fmt"
	"strings"
)

// verifyPrefix marks the machine-readable acceptance command inside a task
// spec. Convention: a single frontmatter-style line `verify: <command>` (e.g.
// `verify: go test ./internal/serve/ -run Relay`). The line may live anywhere
// in the markdown; the command may optionally be wrapped in single/double
// quotes and surrounded by whitespace, all of which are trimmed.
const verifyPrefix = "verify:"

// qaPrefix marks the OPTIONAL, experimental QA-agent hint inside a task spec
// (spec review-runtime-deepening §4 WS-I). Convention: a single frontmatter-style
// line `qa: <one sentence describing what to run-and-verify>`. It parses exactly
// like verifyPrefix and may coexist with a `verify:` line; its absence leaves the
// driver's flow byte-for-byte unchanged (no QA stint).
const qaPrefix = "qa:"

// extractVerify pulls the machine-readable acceptance command out of a task
// spec markdown. On success it returns (command, true); when no `verify:` line
// is present it returns ("", false) so the caller can fall back to a
// conservative full command (e.g. `go build ./... && go test ./...`).
//
// The first `verify:` line wins. Surrounding whitespace and a single pair of
// wrapping quotes (matching ' or ") are stripped from the command.
func extractVerify(specMarkdown string) (string, bool) {
	return extractField(specMarkdown, verifyPrefix)
}

// extractQA pulls the experimental QA hint out of a task spec markdown (spec §4
// WS-I). It returns (hint, true) when a non-empty `qa:` line is present, else
// ("", false) — the signal to the driver that this task opts OUT of the QA gate
// and its flow stays unchanged. Same parse rules as extractVerify.
func extractQA(specMarkdown string) (string, bool) {
	return extractField(specMarkdown, qaPrefix)
}

// extractField is the shared frontmatter-line parser behind extractVerify and
// extractQA: the first line whose trimmed text starts with prefix wins, its value
// is trimmed and unquoted, and a bare prefix with no value is treated as absent.
func extractField(specMarkdown, prefix string) (string, bool) {
	for _, raw := range strings.Split(specMarkdown, "\n") {
		line := strings.TrimSpace(raw)
		if !strings.HasPrefix(line, prefix) {
			continue
		}
		val := strings.TrimSpace(strings.TrimPrefix(line, prefix))
		val = unquote(val)
		if val == "" {
			// A bare `verify:` / `qa:` with no value is treated as absent.
			continue
		}
		return val, true
	}
	return "", false
}

// unquote strips a single matching pair of surrounding single or double quotes.
func unquote(s string) string {
	if len(s) >= 2 {
		if (s[0] == '"' && s[len(s)-1] == '"') || (s[0] == '\'' && s[len(s)-1] == '\'') {
			return strings.TrimSpace(s[1 : len(s)-1])
		}
	}
	return s
}

// cmdExec abstracts command execution so the production path (os/exec) and the
// test path (a deterministic fake) share one interface — no test stub leaks
// into production code (spec §7).
type cmdExec interface {
	// Run executes command in dir, returning its exit code, combined output,
	// and any spawn-level error (e.g. binary not found). A non-zero exit code
	// is reported via exitCode, not err.
	Run(ctx context.Context, dir, command string) (exitCode int, output string, err error)
}

// runGate independently re-runs a feature's acceptance command as a
// deterministic safety net beneath the LLM reviewer: even after every task has
// been accepted, the driver will not merge unless this gate passes.
//
// It runs command in dir via exec. exit 0 → (true, detail). A non-zero exit or
// an exec-level error → (false, detail) where detail carries an output/error
// summary for the escalation record.
func runGate(ctx context.Context, exec cmdExec, dir, command string) (ok bool, detail string) {
	exitCode, output, err := exec.Run(ctx, dir, command)
	if err != nil {
		return false, fmt.Sprintf("gate exec error for %q: %v\n%s", command, err, summarize(output))
	}
	if exitCode != 0 {
		return false, fmt.Sprintf("gate FAILED (exit %d) for %q:\n%s", exitCode, command, summarize(output))
	}
	return true, fmt.Sprintf("gate passed for %q", command)
}

// summarize trims gate output to a bounded tail so escalation records stay
// readable without dropping the failure signal (which tends to land last).
func summarize(output string) string {
	const maxLines = 40
	out := strings.TrimRight(output, "\n")
	if out == "" {
		return "(no output)"
	}
	lines := strings.Split(out, "\n")
	if len(lines) <= maxLines {
		return out
	}
	tail := lines[len(lines)-maxLines:]
	return "...(truncated)...\n" + strings.Join(tail, "\n")
}
