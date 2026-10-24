# Pactify

Pactify is a harness router. It delegates coding tasks to codex (the default),
kimi, agy and opencode, and records jobs, logs and results locally. The Claude
Code plugin provides slash commands; the Go CLI manages the same jobs. See the
[privacy policy](https://github.com/agentjoey/pactify/blob/main/PRIVACY.md).

The latest published release is **v0.14.0**.
See [CHANGELOG](docs/CHANGELOG.md).

Storage is retained under `$PACTIFY_HOME` (default `~/.pactify`). Finished resume
chains expire after 14 days; pending worktrees are kept. Shared caches have a
5 GB cap and are evicted to 80% of that cap. Preview with `pactify sub prune`,
then use `pactify sub prune --yes` to execute. Settings shows storage status;
the dashboard can confirm cleanup and the read-only panel provides the CLI
command. Pruning deletes Codex threads, removing them from Remote Control;
archiving alone can leave them listed. See [storage operations](docs/operations.md#storage-lifecycle)
for configuration, safeguards and exit codes.


Pruning permanently removes deleted jobs from cost history. The default plan groups
items by reason with up to five examples; `pactify sub prune --verbose` lists every
item. JSON output retains the full plan. `storage.json` requires retention of at
least 24 hours and a cache cap of at least 1 GB.

## Install

Install the published binary on macOS or Linux (amd64 / arm64):

```sh
curl -fsSL https://pactify.dev/install.sh | sh
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
`/delegate-codex`, `/delegate-kimi`, `/delegate-agy` or `/delegate-opencode`. `/jobs` (or `/pactify:jobs`) is read-only:
`/jobs all` includes older jobs and `/jobs <id>` shows one job. Read the
[plugin guide](plugins/pactify/README.md) for brief writing and result review.
The plugin is for Claude Code only. Delegation requires an explicit user request;
the experimental agy skill runs only through its slash command. The supported
opencode skill, like kimi, can be invoked by the model only on an explicit user
request to delegate.

## Other hosts

The [portable Agent Plugins 1.0 bundle](https://github.com/agentjoey/pactify/tree/main/plugins/agent-plugin)
provides the same delegation and review workflow with bounded polling.
The install locations below are **pending the final repository location** and may change;
these examples target `agentjoey/pactify` and `plugins/agent-plugin`.

For Codex:

```sh
codex plugin marketplace add agentjoey/pactify --sparse .agents/plugins --sparse plugins/agent-plugin
```

Then select pactify in the host's plugin interface. For Copilot:

```sh
copilot plugin install agentjoey/pactify:plugins/agent-plugin/pactify
```

For Cursor, open Settings → Plugins and paste
`https://github.com/agentjoey/pactify/tree/main/plugins/agent-plugin` into plugin search.
The bundle includes marketplace files for all three hosts.
See the [Codex packaging guide](https://developers.openai.com/plugins/build/plugins),
[Copilot plugin reference](https://docs.github.com/en/copilot/reference/copilot-cli-reference/cli-plugin-reference)
and [Cursor plugins guide](https://cursor.com/docs/plugins) for host installation details.

Alternatively, copy the folders from
[plugins/agents-skills](https://github.com/agentjoey/pactify/tree/main/plugins/agents-skills)
into `.agents/skills/`. The release's `pactify-delegate.zip` contains one Agensi skill.
Checks only detect pactify and show install commands; agents must never run installers.

The host must allow `pactify sub` outside its sandbox because pactify runs harness CLIs
outside the workspace. For example, in Codex's `~/.codex/rules/default.rules`:

```python
prefix_rule(pattern=["pactify", "sub"], decision="allow")
```

Codex matches rules against simple commands only, so the portable skills pass the brief
with `--brief-file` instead of a heredoc.


## CLI basics

```sh
echo "Inspect the repository and report findings" | pactify sub run --read-only --wait 0s
pactify sub run codex --write --worktree --wait 0s < brief.md   # the task brief is read from stdin
pactify sub status --all
pactify sub wait <job-id>
pactify sub result <job-id>
pactify sub logs <job-id> --tail 20
```

Write jobs without `--worktree` edit the directory in place; the CLI prints a note on stderr
for them (except with `--json`). Use `--worktree` to review changes before applying them.
Review the complete worktree diff and untracked files, and run acceptance checks
before `pactify sub apply <job-id>`. Use `pactify sub discard <job-id>` to discard
a result, or `pactify sub run --resume <job-id> --worktree` for rework in the same
worktree. Discard still works when the original directory is gone; it finds the main
repository through the worktree. Harnesses run with `PACTIFY_JOB` set, and `pactify sub run`
and `pactify sub doctor` refuse to start inside a job, so a delegated harness does not
delegate again. A terminal opened from a process that a job left running (for example a dev
server) inherits the marker; open a fresh terminal instead.
Job IDs accept an unambiguous prefix. See [router configuration](docs/router.md) for aliases and fallback,
[job status](docs/sub-status.md) and [cost reporting](docs/sub-cost.md).

codex is the default; kimi and opencode are supported, and agy is experimental. Kimi read-only jobs use a
generated private config with an execution-time tool allowlist, snapshot verification
for every agent, and a fail-closed repository baseline. Shell, writes, sub-agents,
cron, MCP and network tools are unavailable. Use `kimi --read-only` for reviews;
`--with-user-config` is incompatible. Kimi has no OS sandbox for writes; permission
requests in write jobs are approved by pactify. `agy --read-only` uses plan mode and managed hooks. opencode uses
a per-job server (`opencode serve` on 1.x, `@opencode/cli` on 2.x); its read-only mode denies shell/edit tools, reports
`sandboxed=false`, and fails with `readonly_violation` if repository files change.
In write mode, pactify blocks opencode file tools from accessing directories outside
the job workspace, job TMPDIR and `$PACTIFY_HOME/cache`. Shell commands can still
reach files those checks cannot see. Opencode runs without an OS sandbox; these
permission checks are a guardrail, not isolation. Experimental harness results vary by model.

| Harness | Support policy |
| --- | --- |
| codex | Supported; default harness |
| kimi | Supported |
| opencode 1.x (`opencode-ai`) | Supported while `opencode-ai` still publishes 1.x releases and the v1 drift/contract check stays green |
| opencode 2.x (`@opencode/cli`) | Supported; pinned by the reviewed contract (currently 2.1.0) |
| agy | Experimental |

See [upstream compatibility](docs/harness-updates.md) for contract checks and evaluation gates.

## Upgrade and uninstall

Rerun the installer or `go install …@latest` to upgrade; job state carries over.
To uninstall, resolve pending worktree jobs, delete `$PACTIFY_HOME` (after checking
its `credential-quarantine` folder), the binary and the host plugins. See
[upgrade and uninstall](docs/operations.md#upgrade-and-uninstall) for each step.

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
