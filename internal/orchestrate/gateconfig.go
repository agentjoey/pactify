package orchestrate

import (
	"os"
	"path/filepath"

	"github.com/agentjoey/pactify/internal/pact"
)

// resolveGate returns the hard-gate command used for a feature's tasks that
// declare no per-task `verify:` line: the project's explicitly configured gate
// (`pactify config gate`) when set, else a default inferred from the detected
// project type. This replaces a single hardcoded gate so polyglot repos (pnpm /
// go / cargo …) get a sensible build+test gate, and any project can pin its own.
func resolveGate(dir string) string {
	g, ok, err := pact.At(dir).GateConfig()
	if err != nil {
		// Fail closed: an unreadable log means we cannot know whether a gate was
		// configured, and degrading to the type-default here would silently strip a
		// configured safety gate at merge time. resolveGate's callers (gateCommands →
		// the pre-merge hard gate) have no error channel, so surface the failure as a
		// command that always fails loudly — the gate blocks instead of waving through.
		return `echo "pact: gate config unreadable (.pact/log.jsonl) — refusing type-default gate; fix the log and retry" && false`
	}
	if ok {
		return g
	}
	return detectDefaultGate(dir)
}

// detectDefaultGate infers a conservative build+test gate from the project's
// manifest/lockfiles. Order matters: a pnpm lockfile beats a bare package.json.
// Go is the final fallback (matching pactify's historical default) so existing
// repos are unchanged.
func detectDefaultGate(dir string) string {
	exists := func(name string) bool {
		_, err := os.Stat(filepath.Join(dir, name))
		return err == nil
	}
	switch {
	case exists("pnpm-lock.yaml"):
		return "pnpm build && pnpm test"
	case exists("package.json"):
		return "npm run build && npm test"
	case exists("Cargo.toml"):
		return "cargo build && cargo test"
	case exists("go.mod"):
		return fallbackGate
	default:
		return fallbackGate
	}
}
