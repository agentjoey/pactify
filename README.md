# Pactify

Pactify is a harness router. It delegates coding tasks to codex (the default),
kimi, agy and opencode, and records jobs, logs and results locally. The Claude
Code plugin provides slash commands; the Go CLI manages the same jobs.

The latest published release is **v0.12.0**. Router M1 is merged but unreleased;
v0.13.0 is on hold for internal use. Build this checkout to use the current
router. The pact protocol, ledger, MCP server and agent wiring were retired in
AJ-744; tag `v0.12.0` remains the rollback anchor. See [CHANGELOG](docs/CHANGELOG.md).

## Install

The installer downloads a published binary for macOS or Linux:

```sh
curl -fsSL https://pactify.dev/install.sh | sh
```

To build the current router (Go version in `go.mod`):

```sh
go build -o pactify ./cmd/pactify
./pactify --help
```

Install and authenticate the harness CLI you intend to use. Check its integration
with `pactify sub doctor <harness>`; see [doctor](docs/sub-doctor.md).

## Claude Code plugin

```text
/plugin marketplace add agentjoey/pactify
/plugin install pactify@pactify
```

Use `/delegate` (or `/pactify:delegate`) for default codex delegation, or select
`/codex`, `/kimi`, `/agy` or `/opencode`. `/jobs` (or `/pactify:jobs`) is read-only:
`/jobs all` includes older jobs and `/jobs <id>` shows one job. Read the
[plugin guide](plugins/pactify/README.md) for brief writing and result review.

## CLI basics

```sh
pactify sub run --read-only --wait 0s "Inspect the repository and report findings"
pactify sub run codex --write --worktree --wait 0s "Implement the requested change"
pactify sub status --all
pactify sub wait <job-id>
pactify sub result <job-id>
pactify sub logs <job-id> --tail 20
```

Review the complete worktree diff and untracked files, and run acceptance checks
before `pactify sub apply <job-id>`. Use `pactify sub discard <job-id>` to discard
a result, or `pactify sub run --resume <job-id> --worktree` for rework in the same
worktree. Job IDs accept an unambiguous prefix. See [router configuration](docs/router.md) for aliases and fallback,
[job status](docs/sub-status.md) and [cost reporting](docs/sub-cost.md).

codex is the default; kimi, agy and opencode are experimental. kimi has no
read-only mode. `agy --read-only` uses plan mode and managed hooks. opencode uses
a per-job `opencode serve`; its read-only mode denies shell/edit tools, reports
`sandboxed=false`, and fails with `readonly_violation` if repository files change.
See [harness contracts](docs/specs/harness-subagents.md) for access boundaries.

## Dashboard

```sh
pactify serve --addr 127.0.0.1:17082
```

Open `http://127.0.0.1:17082` for Dashboard, Jobs and Settings; Live is embedded in
job details. The server provides `/api/sub` and embedded static assets. Keep it on
loopback. External access uses Cloudflare Tunnel + Access; configure
`PACTIFY_ALLOWED_ORIGINS` for the proxy origin. The Origin guard is not login
authentication. See [deployment prerequisites](docs/jobs-view.md).

For a separate local read-only panel, run `pactify sub panel` (random loopback
port, opens the browser), or `pactify sub panel --port 17100 --no-open`.

[Architecture](docs/architecture.md) · [Development and deployment](docs/deployment.md) ·
[Operations](docs/operations.md) · [Contributing](CONTRIBUTING.md)
