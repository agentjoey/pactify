# Pactify — Architecture

> Status: harness routing is the active product direction; governance is frozen and hosted relay is retired. See [CHANGELOG](CHANGELOG.md).

## Overview

**Use other coding agents as subagents from Claude Code.**

Delegate large tasks — multi-file changes, investigations, anything that needs test runs — from Claude Code to Codex (default) or Kimi (experimental). pactify runs them as background jobs, independently verifies the model that actually ran, reports tokens and quota, isolates the change in a git worktree for you to review, apply or discard, and leaves no trace in the agents' own session lists.

The host is Claude Code. The supported callees are codex (default) and kimi
(experimental, without read-only mode). The runtime uses their installed local
CLIs; it routes harness sessions rather than proxying model API requests.
The [design](specs/harness-subagents.md) records decisions and future milestones;
the structure below describes the current repository, not a claim that the next
release has shipped.

## Delegation path

```text
Claude Code
  /delegate (codex) · /codex · /kimi (experimental)
          │ self-contained brief on stdin
          ▼
plugins/pactify/ ──► cmd/pactify: sub run --worktree
                              │ detached worker + persisted job id
                              ▼
                       internal/sub
                 job lifecycle · worktree · result
                              │ cockpit.Backend / Session
                 ┌────────────┴─────────────┐
                 ▼                          ▼
       internal/cockpit/codex.go   internal/cockpit/acp.go
          codex app-server               kimi acp
                 └────────────┬─────────────┘
                              ▼
                 events · usage · final reply
                              │ independent model verification
                              ▼
                 result.json + Markdown summary
                              │
       sub wait / status / logs / result / cancel
                              │ review actual diff + new files
                    sub apply / discard
```

The Claude main thread writes the brief and runs `sub wait` as a background Bash
command so completion returns to that thread. The plugin also provides forwarding
subagents for a bounded foreground wait. The background worker owns the callee
session; returning a pending job id does not end the worker.

## Active components

| Component | Responsibility |
|---|---|
| `plugins/pactify/` | Claude Code skills `/delegate`, `/codex`, `/kimi`, forwarding subagents and an installation check hook. Turns user intent into a brief and guides review, rework and apply/discard. |
| `cmd/pactify/cmd_sub*.go` | CLI entry points for job execution, inspection, cancellation, review and self-check; detached worker launch and command exit codes. |
| `internal/sub` | Harness registry, durable job state, lifecycle recovery/cancellation, event collection, result summaries, CLI version checks, model verification, session concealment and worktree review operations. |
| `internal/cockpit` | Shared `Backend` / `Session` interfaces: start/resume, prompt, events, approvals, interrupt and close. The codex app-server and ACP backends supply the delegation runtime. Other existing backends do not imply additional supported `sub` harnesses. |
| `internal/procx`, `internal/lockx` | Process identity and lifecycle locking used to coordinate workers, cancellation and shared worktrees. |

`sub` uses the cockpit session abstraction, not `internal/orchestrate.Runner`.
The existing dashboard also consumes cockpit events, but no dashboard, MCP server,
seat registration or pact ledger is required to delegate a task.

### Backends and evidence

| Harness | Transport and verification | Permissions and session lists |
|---|---|---|
| codex (default) | `codex app-server`; actual model from the server's session response; token usage and quota from protocol events. | `--write`: workspace-write with network; `--read-only`: read-only without network. Hidden threads are archived on close and unarchived for resume. |
| kimi (experimental) | `kimi acp` through the ACP backend; model selection through protocol configuration, independently checked against kimi's local session record, which also supplies tokens. | Write mode uses kimi's native permissions, without a codex-style sandbox. Read-only is rejected. Concealment uses kimi's archive interface, falls back to archive fields with a warning, and checks the CLI session list. |

The registry in `internal/sub/harness.go` owns the default, experimental warning,
read-only exclusion and concealment policy. Model verification and hiding failures
surface in the result's warnings. Sessions are hidden from normal lists, not
erased: job logs and resume handles remain available. Startup failures without a
session id may leave a session visible; kimi's archive fallback may hide it only
from the CLI list, and kimi retains derived workspace entries.

### Job persistence and lifecycle

Jobs live under `$PACTIFY_HOME/jobs/<job-id>/`, defaulting to
`~/.pactify/jobs/<job-id>/`:

```text
state.json    request, status, timestamps, process identities and worktree metadata
log.jsonl     collected backend events
result.json   structured result, also rendered as Markdown
worker.out    detached worker output
wt/           isolated git worktree, when --worktree is used
```

The lifecycle is `queued → running → succeeded | failed | canceled`. State
mutations are locked; the result is published before the terminal state.
Recovery detects absent or replaced workers using process identity. Cancellation
covers registered harness processes, not just the foreground CLI.
`run` and `wait` return pending status when their wait expires; the job continues.

`internal/sub/envelope.go` defines the review summary: status/warnings, harness
and CLI version, requested/actual model and verification, access, duration,
tokens, optional cost/quota, tool calls, diff statistics, worktree/base commit,
final reply, session id and log path. The [README example](../README.md#what-comes-back)
follows that renderer. Unknown or unavailable evidence is not a successful check.

### Worktree isolation and rework

The plugin uses `--worktree` for large tasks. The CLI requires the flag explicitly;
without it, a fresh job runs in the supplied working directory. Isolation creates
a detached worktree from the original `HEAD`, preserves the relative working
directory and records its base commit and original repository. Uncommitted source
changes are not copied in; a dirty source produces a warning.

`--resume` selects the latest completed isolated session with a session id for
the same harness and original directory, including failed/canceled turns. It
reuses the worktree and base, so diff statistics cover the cumulative change.
Legacy sessions without worktrees require a successful turn and cannot gain
isolation on resume. Missing or discarded worktrees cannot resume; concurrent
turns in the same worktree are refused.

Review includes committed, unstaged and untracked changes against the recorded
base. `apply` checks affected original paths and patch applicability before
writing, then applies without committing. `discard` removes that job's worktree;
it does not revert an already applied change. Resume, apply and discard share a
worktree lock across all turns, and require it to be idle. Discard marks every
turn using that worktree as discarded. Rework should happen before apply/discard.

### Compatibility checks

`pactify sub doctor` makes real calls in temporary repositories and job storage.
It checks installation/version, write execution and usage, actual model,
read-only behavior where supported, codex write networking and session hiding.
Only a harness that passes all applicable checks updates
`$PACTIFY_HOME/verified-versions.json`. A normal job warns when its CLI version
has not been verified; the table does not prove current login or service health.
See [sub doctor](sub-doctor.md) for probe details and limitations.

The [Phase 0 report](phase0-report.md) records 26 real delegations: codex completed
9 of 10 large tasks in one attempt and returned findings for all 3 read-only
tasks. Kimi missed the large-task and read-only criteria, explaining its
experimental status and read-only exclusion. Median additional Claude context
was about 3.3k tokens per delegation; that is not total callee usage.

## Governance mode (optional, frozen)

The protocol, ledger, orchestrator and local dashboard remain in the repository
as optional governance tooling. New product investment is frozen. They are
independent of the default harness-delegation path.

| Retained component | Contract and status |
|---|---|
| `.pact/`, `internal/pact`, `internal/projection` | Protocol v1: `PROJECT.md` for roles/conventions, `tasks/` for task briefs, append-only `log.jsonl` as source of truth, `STATE.yml` as a regenerable projection. A worker cannot self-accept; all tasks must be accepted before feature merge. |
| `internal/agent`, `internal/mcp`, `plugins/pact/` | Governance entry-file wiring, kind attribution, shell/MCP access and the separate pact plugin. Managed entry blocks remain distinct from the harness plugin. |
| `internal/orchestrate` | Existing seat/task driver, command/ACP runners, worker/reviewer loops and verification gates. Frozen; not the engine behind `sub`. |
| `internal/serve`, `web/` | Local `pactify serve` dashboard and APIs, with embedded Vite/React/Tailwind assets. Frozen; UI changes still require the repository's existing test gates. |
| Single canonical ledger | Paused at WS-B, feature flag off by default; no new ledger rollout is implied. |
| `cloud/`, hosted relay, `internal/cloudauth` | Retired and frozen; hosted relay is not deployed and will not be restored under this direction. Retained code is not an active service. Deletion is a separate decision. |

Governance communication remains shell/MCP → append-only log → projection;
`serve` watches local events and streams updates to its dashboard. The retained
relay code describes a retired implementation, not a current cloud product or
a required part of delegation.

- [Protocol specification](specs/pact-protocol.md) and [schemas](../schemas/)
- [Governance onboarding](onboarding.md) and [operations](operations.md)
- [Paused ledger design](specs/single-canonical-ledger.md)
- [Changelog](CHANGELOG.md) for historical architecture and releases
