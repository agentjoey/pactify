// Package testenv isolates a test binary from pact state that belongs to
// whoever launched it. It is imported only from _test files.
package testenv

import "os"

// Isolate neutralizes the INHERITED PACT_DIR for the whole test binary and
// returns a restore func (call it from TestMain after m.Run).
//
// Why this exists — a real incident, twice: orchestrate injects an ABSOLUTE
// PACT_DIR into every agent it launches (orchestrate/runner.go, added in v0.8.1
// so a worker's checkpoint lands in the driver's worktree). That is correct for
// the agent — but the agent's very next move is usually to run this repo's own
// verify gate. Any test that resolves a pact dir from the environment then
// treats the REAL repository's .pact as its scratch space, and on 2026-08-22 a
// test run wiped .pact/log.jsonl from 622 events to 1 and overwrote CLAUDE.md /
// AGENTS.md / .pact/PROJECT.md. The append-only ledger is the single source of
// truth, so this is data loss, and it is silent.
//
// It is also actively misleading: the wreckage (stray files, a rewritten
// CLAUDE.md) looks exactly like a worker that scribbled outside its task, so it
// corrupts REVIEW decisions, not just state. That happened twice before the
// cause was found.
//
// `git worktree --detach` does NOT contain it: worktrees share one .git common
// dir, and an absolute PACT_DIR points straight back at the primary repo
// regardless. Only clearing the variable works.
//
// Scope notes:
//   - Unset, not "set to a temp dir": unset is exactly the hermetic convention
//     the suite already assumes (paths.Dir falls back to a repo-relative
//     ".pact"), and it leaves per-test t.Setenv("PACT_DIR", ...) free to exercise
//     the override deliberately — internal/mcp and internal/paths both do.
//   - PACTIFY_HOME is a sibling hazard (tests that forget it read the
//     developer's real ~/.pactify) but is deliberately NOT touched here: its
//     meaning is not uniform across the suite — internal/serve/security_test.go
//     joins ".pactify" ONTO it, treating it as a home root — so a blanket
//     default would change what those tests assert. Tracked separately.
//
// This replaces a convention that had already failed twice: a scattering of
// per-test t.Setenv("PACT_DIR", "") calls across planner/mcp/orchestrate, which
// only ever protected the tests somebody remembered to patch.
//   - PACTIFY_LEDGER_REF (WS-B 的 ref 镜像开关) is cleared for the same reason
//     as PACT_DIR: a developer who exported it to try the dark launch would
//     otherwise have EVERY test in the suite start writing a git ref, silently
//     changing what the suite exercises. Per-test t.Setenv still works, which is
//     how internal/ledger and internal/pact drive both branches deliberately.
func Isolate() func() {
	prev, had := os.LookupEnv("PACT_DIR")
	os.Unsetenv("PACT_DIR")
	prevRef, hadRef := os.LookupEnv("PACTIFY_LEDGER_REF")
	os.Unsetenv("PACTIFY_LEDGER_REF")
	return func() {
		if had {
			os.Setenv("PACT_DIR", prev)
		}
		if hadRef {
			os.Setenv("PACTIFY_LEDGER_REF", prevRef)
		}
	}
}
