# Phase 0 Exit Gate — manual dogfood checklist

Goal: drive a real Phase 1 task (e.g. the Go `pactify init` command) through the pact
protocol with Claude (orchestrator+reviewer) and opencode (worker). Human only says "start."

Prereq: `.pact/bin/pact.sh` exists on this branch (46/46 unit tests green).

## Setup (Claude, seat claude-opus)
- [ ] In a fresh feature branch, run `pact_init --project pactify \
      --seat "claude-opus:orchestrator,reviewer:CLAUDE.md" --seat "opencode:worker:AGENTS.md"`
- [ ] Write `.pact/tasks/T1.md` (spec + acceptance) for the first Go CLI command
- [ ] `pact_assign T1 --feature <F> --branch <b> --owner opencode --reviewer claude-opus`

## Worker run (opencode) — the relay test
- [ ] Start opencode in the repo. Say only: "start."
- [ ] CONFIRM: opencode auto-reads AGENTS.md, runs pact_join, reads STATE, identifies T1
      as its task — WITHOUT the human pasting the task/context.
- [ ] opencode implements, then `pact_checkpoint T1 --evidence "<go test output>"`

## Review + merge (Claude)
- [ ] Claude reads the branch diff + evidence, runs verification, `pact_accept T1`
- [ ] `pact_merge <F>` — feature → shipped

## Exit Gate assertions (all must hold)
- [ ] Human never pasted task content / context / diff — only said "start"
- [ ] opencode bootstrapped purely from `git pull` + auto-read entry file
- [ ] Rule 1 held in practice (worker could not self-accept)
- [ ] One induced crash (kill opencode mid-task) recovered via re-join + resume

## Verdict — ✅ PASS (2026-06-09)

Ran in scratch repo `~/AgentWorks/Code_Claude/pact-dogfood` (separate repo so pact_init
wouldn't clobber pactify's own CLAUDE.md/AGENTS.md — see Finding 3). Task: implement a Go
`Greet` function with tests. Claude = orchestrator+reviewer (`claude-opus`), opencode = worker.

**Relay trail (log.jsonl):** `init → assign` (claude-opus) → **`join → checkpoint` (opencode, self-bootstrapped)** → `accept` (claude-opus) → `merge` → feature shipped.

- [x] Human only said "start" — opencode read AGENTS.md, ran pact_join, found T1 in STATE,
      read the spec, wrote `greet.go`+`greet_test.go`, ran `go test`, and checkpointed with
      evidence — **without the human pasting any task/context/diff.** ✅ Core claim holds.
- [x] opencode bootstrapped purely from its entry file. ✅
- [x] Rule 1 held live: opencode's `pact_accept T1` was BLOCKED
      ("only the reviewer claude-opus may accept"). ✅
- [~] Crash recovery: not exercised live this run; covered by the integration unit test
      (`rm STATE → pact_log --replay → task still in_progress`).
- Reviewer verified independently: `go test ./... -v` PASS (both cases), `go vet` clean.

**Verdict: PASS on the core thesis — the human dropped from "context courier" to "start button."**
Proceed to Phase 1, feeding the findings below into M1.

## Dogfood findings → Phase 1 backlog

- **F1 (worker branch/commit discipline) — FIXED 2026-06-09 (commit 324cbb6).** Chose the
  minimal-automation approach: `pact_join` auto-checks-out the worker's feature branch
  (creates from HEAD if absent); `pact_checkpoint` auto-commits the worker's work; `pact_init`
  records `base_branch` and `pact_merge` returns to it before merging. Flow is now hands-off
  end-to-end (verified: worker joins → writes code → checkpoints → reviewer accepts → merge
  → feature shipped on base, clean merge commit, validate OK). Limitation: single shared
  working tree, one feature in flight at a time; true concurrent isolation (git worktree) is
  still Phase 1.
- **F2 (multi-line evidence) — FIXED 2026-06-09 (commit on feat/phase0-pact-skill).** Real
  `go test -v` output (newlines+tabs) broke STATE.yml rendering and made `pact_validate`
  checks 4/5 silently skip (`echo "$fresh" | jq` corrupts JSON when the shell's echo expands
  `\n`). Fixed: render folds newlines/tabs; validate uses here-strings. Regression test added.
- **F3 (pact_init clobbers existing entry files) — FIXED 2026-06-09 (commit 324cbb6).**
  `_pact_bake_entry` now writes a managed `<!-- pact:begin/end -->` block: preserves existing
  entry-file content, idempotent on re-init (replaces the block, no duplication), and never
  writes through a symlink (resolves the AGENTS.md→CLAUDE.md corruption). pact_init is now
  safe to run in a repo that already has CLAUDE.md/AGENTS.md.
- **F4 (entry must state shell-persistence) — minor, mitigated.** Agents whose bash calls
  don't share state need `export PACT_AGENT_ID=… && source … &&` on every pact call; the
  baked entry should say so (added manually this run).

## Backlog from final code review (deferred, non-blocking)
- M6: `pact_join --roles` is self-declared and currently inert (authority derives from
  task owner/reviewer, not join roles). Consider validating join roles against the seat
  roster, or dropping the flag.
- I5: `task_status` test helper reads STATE.yml by `grep` (substring/order-fragile).
  Consider rewriting test assertions to query `_pact_project_json` via `jq`.
