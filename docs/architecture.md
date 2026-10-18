# Pactify — Architecture

> Last updated: **2026-10-18** | Status: **v0.12.0 已发布**；M1 已合入但未发布，v0.13.0 挂起、继续内部自用。历史与未发布变更见 [`CHANGELOG.md`](CHANGELOG.md)。

Pactify is a local harness router. The CLI and Claude Code plugin delegate tasks
to codex (default), kimi, agy and opencode. Jobs, results and worktrees are managed
locally. Governance mode was retired in AJ-744; tag `v0.12.0` is the rollback anchor.

## Components

| Component | Responsibility |
|-----------|----------------|
| `cmd/pactify` | Cobra CLI: `serve`, `sub`, `version` |
| `internal/sub` | Job storage and workers, routing/fallback, resume/cancel/logs/results, isolated worktrees, harness homes and `sub doctor` |
| `internal/cockpit` | Harness backends and session events, including golden contracts, codex schema and opencode API checks |
| `internal/acp` | ACP transport used by harness backends |
| `internal/procx` | Process identity and process-tree cleanup |
| `internal/serve` | Dashboard HTTP server, `/api/sub`, job event SSE and embedded dashboard dist |
| `internal/subpanel` | Local read-only job panel and its embedded dist |
| `web/` | Vite + React + Tailwind v4 sources, unit tests and Playwright e2e |
| `plugins/pactify` | Claude Code delegation skills, forwarding agents and job inspection |
| `internal/vendorenv`, `credentiallinks`, `gitx`, `lockx`, `testenv` | Harness environment, credential links, repository reads, locking and test fixtures |

There are **11 top-level internal packages**. The plugin invokes the CLI; the
web dashboard consumes only `/api/sub`. Both web builds are tracked under
`internal/serve/dist` and `internal/subpanel/dist` and embedded into the Go binary.

## Job execution

`sub run` records a job and starts a background worker. Routing selects the
harness and model; job state, structured events, logs and results support status
inspection, waiting, cancellation and resume. Write jobs can use an isolated
Git worktree. The host reviews and verifies the diff before explicitly applying
or discarding it. See [router](router.md), [status](sub-status.md) and
[harness contracts](specs/harness-subagents.md).

Harness backends use each CLI's supported transport: codex app-server, ACP or
streaming CLI sessions, and per-job opencode HTTP/SSE servers. Managed harness
homes isolate sessions and configuration; explicit user-configuration opt-in and
read-only access boundaries are documented in the harness contracts.

## HTTP and local storage

`serve --addr` defaults to `127.0.0.1:7777`; the local deployment uses
`127.0.0.1:17082`. It serves Dashboard, Jobs and Settings, with Live inside job
details. Unknown `/api/*` routes return 404. Browser writes check Origin;
external access uses Cloudflare Tunnel + Access, with allowed proxy origins
configured through `PACTIFY_ALLOWED_ORIGINS`. See [operations](operations.md).

Job and router data live under `PACTIFY_HOME` (default `~/.pactify`). No pact
protocol files, ledger projections, MCP server or agent configuration wiring are
part of the current product. `.agent/` is this repository's ignored development
workbench.
