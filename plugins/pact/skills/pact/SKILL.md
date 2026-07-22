---
name: pact
description: Use when coordinating multi-agent work in a repo with a .pact/ directory — playing orchestrator, worker, or reviewer via the pact protocol's MCP tools.
---

# pact — multi-agent coordination

This repo uses the **pact protocol**. The source of truth is `.pact/PROJECT.md`. The
`pact` MCP server (bundled with this plugin) exposes every verb as a tool.

## On start
Call the `status` tool to see current state, then `join` (registers your seat + checks
out your feature branch). `validate` checks protocol conformance. Run `pactify doctor`
if tools are missing.

**Seat identity:** your seat resolves from `PACT_AGENT_ID` (inherited from the
environment **Claude Code was launched from** — exporting it in this session won't
reach the server) else the untracked `.pact/seat` file. If `join` reports the seat is
unset, run `pactify seat use <id>` (binds this working copy — no restart needed for
CLI verbs; for MCP, the file is read on the next tool call), or launch Claude Code from
a shell with `PACT_AGENT_ID=<seat>` exported. For concurrent seats in one repo, use a
separate git worktree per seat.

## Your job by role
- **orchestrator**: write `.pact/tasks/<id>.md` (spec + acceptance), then call `assign`
  with feature/branch/owner/reviewer. When all tasks accepted: `merge`.
- **worker**: `join`, read your task, implement, then `checkpoint` with evidence.
- **reviewer**: read the diff + evidence, verify, then `accept` or `changes`.

## The two rules
1. A worker cannot self-accept — only the task's reviewer accepts.
2. A feature cannot merge until all its tasks are accepted.
