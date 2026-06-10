---
name: pact
description: Use when coordinating multi-agent work in a repo with a .pact/ directory — playing orchestrator, worker, or reviewer via the pact protocol's MCP tools.
---

# pact — multi-agent coordination

This repo uses the **pact protocol**. The source of truth is `.pact/PROJECT.md`. The
`pact` MCP server (bundled with this plugin) exposes every verb as a tool.

## On start
Set your seat, then use the MCP tools:
```bash
export PACT_AGENT_ID=<your-seat>   # or run `pactify setup`
```
Call the `status` tool to see current state, then `join` (registers your seat + checks
out your feature branch). Run `pactify doctor` if tools are missing.

## Your job by role
- **orchestrator**: write `.pact/tasks/<id>.md` (spec + acceptance), then call `assign`
  with feature/branch/owner/reviewer. When all tasks accepted: `merge`.
- **worker**: `join`, read your task, implement, then `checkpoint` with evidence.
- **reviewer**: read the diff + evidence, verify, then `accept` or `changes`.

## The two rules
1. A worker cannot self-accept — only the task's reviewer accepts.
2. A feature cannot merge until all its tasks are accepted.
