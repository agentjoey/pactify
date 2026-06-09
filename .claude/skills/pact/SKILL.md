---
name: pact
description: Use when coordinating multi-agent work in a repo that has a .pact/ directory — playing orchestrator, worker, or reviewer roles via the pact protocol.
---

# pact — multi-agent coordination

This repo uses the **pact protocol**. The protocol itself lives in `.pact/` and is
agent-agnostic. This skill is only Claude's convenience layer — the source of truth is
`.pact/PROJECT.md` and `pact_help`.

## On start
```bash
source .pact/bin/pact.sh
pact_help          # verb reference + the two rules
pact_status        # current state
```
Your seat + role are declared in your entry file (CLAUDE.md). Export `PACT_AGENT_ID`
before any verb.

## Your job by role
- **orchestrator**: write `tasks/<id>.md` (spec + acceptance), then
  `pact_assign <id> --feature <f> --branch <b> --owner <w> --reviewer <you>`.
  When all tasks accepted: `pact_merge <feature>`.
- **worker**: `pact_join <seat> --roles worker`, read your assigned task, implement,
  then `pact_checkpoint <id> --evidence "<test/build output>"`.
- **reviewer**: read the diff + evidence, run verification, then `pact_accept <id>`
  or `pact_changes <id> --reason "..."`.

## The two rules (enforced by pact.sh)
1. A worker cannot self-accept — only the task's reviewer accepts.
2. A feature cannot merge until all its tasks are accepted.

## Recovery
If STATE looks wrong: `pact_log --replay` rebuilds it from the log (the source).
If a worker crashed mid-task: re-`pact_join` the same seat and resume.
