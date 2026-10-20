# Pactify

Pactify is a harness router. It delegates coding tasks to codex (the default),
kimi, agy and opencode, and records jobs, logs and results locally. The Claude
Code plugin provides slash commands; the Go CLI manages the same jobs.

The latest published release is **v0.13.0**, the first harness router release.
See [CHANGELOG](docs/CHANGELOG.md).

## Install

Install the published binary on macOS or Linux (amd64 / arm64):

```sh
curl -fsSL https://raw.githubusercontent.com/agentjoey/pactify/main/install.sh | sh
pactify sub --help
```

Or install with Go (version in `go.mod`):
`go install github.com/agentjoey/pactify/cmd/pactify@latest`. To build from a
checkout: `go build -o pactify ./cmd/pactify`.

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
echo "Inspect the repository and report findings" | pactify sub run --read-only --wait 0s
pactify sub run codex --write --worktree --wait 0s < brief.md   # the task brief is read from stdin
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

codex is the default; agy and opencode are experimental. Kimi read-only jobs use a
generated private config with an execution-time tool allowlist, snapshot verification
for every agent, and a fail-closed repository baseline. Shell, writes, sub-agents,
cron, MCP and network tools are unavailable. Use `kimi --read-only` for reviews;
`--with-user-config` is incompatible. Kimi has no OS sandbox for writes; permission
requests in write jobs are approved by pactify. `agy --read-only` uses plan mode and managed hooks. opencode uses
a per-job `opencode serve`; its read-only mode denies shell/edit tools, reports
`sandboxed=false`, and fails with `readonly_violation` if repository files change.
Experimental harnesses have no OS sandbox for writes; results vary by model.

## Dashboard

```sh
pactify serve --addr 127.0.0.1:7777
```

Open `http://127.0.0.1:7777` for Dashboard, Jobs and Settings; Live is embedded in
job details. The server provides `/api/sub` and embedded static assets. Every
request checks Host: loopback names (`127.0.0.1`, `localhost`, `[::1]`) must use
the listening port. Browser writes require an exact `http` origin on that port;
CLI/scripts without Origin remain supported.

For a reverse proxy that performs authentication, set `PACTIFY_ALLOWED_ORIGINS`
to its comma-separated full origins (for example `https://dash.example.com`).
Their hosts, with or without an explicitly configured port, are also accepted.
These checks do not authenticate users.

Non-loopback binds (including `0.0.0.0` and `::`) require `PACTIFY_SERVE_TOKEN`
with at least 32 characters. Whenever set, it requires `Authorization: Bearer
<token>` on every `/api/` request, including reads and SSE. Static assets remain
public behind the Host check. The dashboard UI does not send this token: token
mode is for reverse proxies that inject the header after authenticating users.
Non-loopback Host values must match the bound IP/hostname and port or an allowed
proxy host. See [deployment prerequisites](docs/jobs-view.md).

For a separate local read-only panel, run `pactify sub panel` (random loopback
port, opens the browser), or `pactify sub panel --port 17100 --no-open`.

[Architecture](docs/architecture.md) · [Development and deployment](docs/deployment.md) ·
[Operations](docs/operations.md) · [Contributing](CONTRIBUTING.md)

## License

Pactify is licensed under the [Functional Source License, Version 1.1, MIT Future
License](LICENSE) (FSL-1.1-MIT). You may use, modify and redistribute it for any
purpose except a Competing Use; internal use, non-commercial education and
research, and professional services are permitted. Each version becomes available
under the MIT license two years after its release. Versions up to and including
v0.13.0 were released under the MIT license and remain available under it.
