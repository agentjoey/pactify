<!-- pact:begin (managed by pactify — edit outside this block) -->
# p — Pact Charter (protocol_version: 1)

This repo uses the **pact protocol** (v1). Any agent that can read files + run git can participate.

## Roles
- **orchestrator** — split spec→tasks; assign; merge; maintain charter
- **worker** — implement; at checkpoint set awaiting_review + write evidence
- **reviewer** — verify diff+evidence → accept / changes_requested
- **human** — start button + final authority

## The two rules (the pact)
1. A worker cannot self-accept. Only a task's reviewer may accept it (owner != reviewer), and only when awaiting_review.
2. A feature cannot merge until all its tasks are accepted.

## Seats
- `opencode` — roles: worker — entry: AGENTS.md

## Commands
Run `pactify help` for the verb reference.
<!-- pact:end -->
