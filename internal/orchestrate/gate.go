// Package orchestrate drives the pact state machine: it headlessly invokes the
// owner/reviewer agents at each transition and merges a feature only after an
// independent hard test gate passes. This file implements that gate plus the
// extraction of a machine-readable verify command from a task spec.
package orchestrate

import (
	"context"
	"fmt"
	"strings"

	"github.com/agentjoey/pactify/internal/gitx"
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

// tierPrefix marks the machine-readable complexity tier inside a task spec
// (spec execution-tiering §4.1). Convention: a single frontmatter-style line
// `tier: L0|L1|L2|L3`. Its absence resolves to TierL1, so every existing spec
// keeps today's behavior byte-for-byte.
const tierPrefix = "tier:"

// Tier is a task's complexity grade. TierL1 is the default.
type Tier string

const (
	TierL0 Tier = "L0"
	TierL1 Tier = "L1" // default
	TierL2 Tier = "L2"
	TierL3 Tier = "L3"
)

// ParseTier normalizes a raw tier string. Case-insensitive, whitespace-trimmed.
// Any absent, empty, or unrecognized value resolves to TierL1 so every existing
// spec keeps today's behavior byte-for-byte.
func ParseTier(raw string) Tier {
	switch Tier(strings.ToUpper(strings.TrimSpace(raw))) {
	case TierL0:
		return TierL0
	case TierL2:
		return TierL2
	case TierL3:
		return TierL3
	default:
		return TierL1
	}
}

// extractTier pulls `tier:` out of a task spec markdown, defaulting to TierL1.
func extractTier(specMarkdown string) Tier {
	raw, _ := extractField(specMarkdown, tierPrefix)
	return ParseTier(raw)
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
	// Run executes command in dir with the supplied env vars merged into the
	// process environment, returning its exit code, combined output, and any
	// spawn-level error (e.g. binary not found). A non-zero exit code is reported
	// via exitCode, not err.
	Run(ctx context.Context, dir, command string, env map[string]string) (exitCode int, output string, err error)
}

// runGate independently re-runs a feature's acceptance command as a
// deterministic safety net beneath the LLM reviewer: even after every task has
// been accepted, the driver will not merge unless this gate passes.
//
// It runs command in dir via exec. exit 0 → (true, detail). A non-zero exit or
// an exec-level error → (false, detail) where detail carries an output/error
// summary for the escalation record.
func runGate(ctx context.Context, exec cmdExec, dir, command string, env map[string]string) (ok bool, detail string) {
	exitCode, output, err := exec.Run(ctx, dir, command, env)
	if err != nil {
		return false, fmt.Sprintf("gate exec error for %q: %v\n%s", command, err, summarize(output))
	}
	if exitCode != 0 {
		return false, fmt.Sprintf("gate FAILED (exit %d) for %q:\n%s", exitCode, command, summarize(output))
	}
	return true, fmt.Sprintf("gate passed for %q", command)
}

// filesPlaceholder is substituted with the merge-base-relative changed file list
// for gates that want to scope their work (e.g. `eslint {files}`).
const filesPlaceholder = "{files}"

// runGateScoped runs a gate command with the feature's merge-base change set
// injected. Commands containing `{files}` get the list substituted; an empty
// change set skips the gate with a pass (so `eslint {files}` does not degrade to
// the whole repo). Commands without `{files}` run byte-for-the-command-byte but
// receive PACT_CHANGED_FILES in their environment. If the base branch or its
// merge-base cannot be resolved, `{files}` gates fail closed.
func runGateScoped(ctx context.Context, exec cmdExec, dir, command, base string) (ok bool, detail string) {
	changed, changedErr := changedFiles(dir, base)
	hasPlaceholder := strings.Contains(command, filesPlaceholder)

	env := map[string]string{}
	if changedErr == nil {
		env["PACT_CHANGED_FILES"] = strings.Join(changed, "\n")
	} else {
		env["PACT_CHANGED_FILES"] = ""
	}

	if hasPlaceholder {
		if changedErr != nil {
			return false, fmt.Sprintf("gate skipped: cannot resolve changed files for %q: %v", command, changedErr)
		}
		if len(changed) == 0 {
			return true, fmt.Sprintf("no changed files — gate skipped for %q", command)
		}
		quoted := make([]string, len(changed))
		for i, f := range changed {
			quoted[i] = shellQuote(f)
		}
		command = strings.ReplaceAll(command, filesPlaceholder, strings.Join(quoted, " "))
	}

	return runGate(ctx, exec, dir, command, env)
}

// changedFiles returns the files changed on the current branch relative to base.
// An empty base or an unreachable base is an error so callers can fail closed.
// `.pact/` paths are excluded: the ledger churns on every verb (checkpoint
// auto-commits log/STATE), so it would inject machine files into every {files}
// gate — a linter has no business seeing them, and their presence would defeat
// the empty-set "skip" semantics for tasks that only touched the ledger.
func changedFiles(dir, base string) ([]string, error) {
	if base == "" {
		return nil, fmt.Errorf("no base branch configured")
	}
	files, err := gitx.ChangedFiles(dir, base)
	if err != nil {
		return nil, err
	}
	out := files[:0]
	for _, f := range files {
		if strings.HasPrefix(f, ".pact/") {
			continue
		}
		out = append(out, f)
	}
	return out, nil
}

// shellQuote returns a single-quoted shell word that safely contains s.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
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
