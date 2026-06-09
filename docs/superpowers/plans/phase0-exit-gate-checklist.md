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

## Verdict
- [ ] PASS → proceed to Phase 1 (Go CLI). Record learnings into the M1.1 schema.
- [ ] FAIL → capture where the human had to intervene; pivot the protocol.

## Backlog from final code review (deferred, non-blocking)
- M6: `pact_join --roles` is self-declared and currently inert (authority derives from
  task owner/reviewer, not join roles). Consider validating join roles against the seat
  roster, or dropping the flag.
- I5: `task_status` test helper reads STATE.yml by `grep` (substring/order-fragile).
  Consider rewriting test assertions to query `_pact_project_json` via `jq`.
