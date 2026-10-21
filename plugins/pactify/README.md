# Pactify harness plugin

**Claude Code only.** This plugin requires a local terminal and cannot run in claude.ai chat or Cowork.
The top-level `bin/` directory prevents installation in those environments.
Delegate tasks only on explicit user request to a locally installed codex / kimi / agy / opencode CLI, then review the result summary and workspace changes.
The default harness is codex. kimi read-only access uses a generated private config and a tool allowlist. Kimi has no OS sandbox for writes; permission requests are approved by pactify in write jobs.

## Installation

Run these commands in Claude Code:

```text
/plugin marketplace add agentjoey/pactify
/plugin install pactify@pactify
```

To update: `/plugin marketplace update pactify`, then update the pactify plugin from `/plugin` (Installed → pactify → Update) and run `/reload-plugins` or start a new session. Claude Code caches a plugin by its `plugin.json` version, so every change to this plugin bumps that version (CI enforces it). If an older install still shows stale skills, reinstall with `/plugin uninstall pactify@pactify` and `/plugin install pactify@pactify`.

Prerequisites: `pactify` is on PATH and `pactify sub --help` works; the official codex / kimi / agy / opencode CLI you want to call is installed and logged in.
Run `"${CLAUDE_PLUGIN_ROOT}/bin/pactify-check"` to report PATH availability, print `pactify version`, and check `pactify sub --help`.
The SessionStart hook reuses this POSIX shell check with `--quiet`: successful checks are silent, and failures suggest installing or upgrading without blocking the session.
The check prints these installation options if pactify is missing; it never runs installers or accesses the network:

```sh
curl -fsSL https://raw.githubusercontent.com/agentjoey/pactify/main/install.sh | sh
go install github.com/agentjoey/pactify/cmd/pactify@latest
```

The plugin does not proxy models, manage logins, or read or copy credentials.

## Usage

```text
/delegate Add expiration and regression tests to src/cache/, then run the project's full test suite
/delegate-codex Add expiration and regression tests to src/cache/, modify only that directory, and run the full npm test suite
/delegate-opencode Add expiration and regression tests to src/cache/ with write access, then run the project's full test suite
/delegate-agy Add expiration and regression tests to src/cache/ with write access, then run the project's full test suite
/delegate-kimi Add expiration and regression tests to src/cache/ with write access, then run the project's full test suite
```

Use `/pactify:delegate`, `/pactify:delegate-codex`, `/pactify:delegate-kimi`, `/pactify:delegate-agy`, or `/pactify:delegate-opencode` to avoid conflicts with other skills of the same name.
`/delegate` uses the default codex harness; `/delegate-codex`, `/delegate-kimi`, `/delegate-agy` and `/delegate-opencode` select a harness explicitly. An explicit invocation always delegates, regardless of task size.
Delegate only when the user explicitly asks: a pactify slash command, or a clear request to delegate or hand off to a harness; otherwise do the work directly.
Task size alone never authorizes delegation. Preserve the requested access mode; never raise permissions yourself.
The codex, kimi and opencode skills are model-invocable for explicit delegation requests.
The experimental agy skill sets `disable-model-invocation: true` and runs only via its slash command, including the namespaced form. The supported opencode skill, like kimi, is model-invocable only on an explicit user request to delegate.

The Claude main thread first writes a complete brief with Background, numbered goals, Known constraints and special cases, General constraints, and Acceptance commands.
Specify paths, scope, access, and known exceptions: the callee cannot infer constraints from the current conversation. Acceptance commands must include the project's full test suite.
Require inspection of callers, special cases, and documented exceptions before changing rules, and a failing regression test before each behavior change. Do not change existing tests to accommodate new behavior unless the brief explicitly identifies the old behavior as wrong; name any such changes in the final reply.
Forbid commits and credential access; optionally forbid subagents and report files.
The five delegation skills (delegate-codex, delegate-kimi, delegate-agy, delegate-opencode, delegate) embed the same template, complete example, and review workflow. Bats checks that their shared blocks are identical.

The main thread runs `pactify sub run [harness] --write --worktree --wait 0s --json`.
Omitting the harness uses codex: `/delegate` runs `pactify sub run --write --worktree --wait 0s --json`.
Use `--read-only` for read-only tasks, including kimi. Kimi requires its generated private config,
rejects `--with-user-config` in read-only mode, and requires the original access mode on resume.
Exit 3 means the job is still running and stdout contains `jobId`. The main thread runs
`pactify sub wait <jobId>` with `run_in_background: true` and `timeout: 7200000`, then reviews the result as described below.
If the background wait stops at its time limit, check `pactify sub status` and resume the same wait; do not start a duplicate job.
Exit codes 0 / 1 / 2 mean success, failure, and invalid arguments.
Retrieve the result again with `pactify sub result <jobId>`.

An empty normal reply gets exactly one same-session follow-up, independently of `--retries`.
If the result warns `empty final reply` (after one follow-up, or when the session ended first), a read-only task is not done:
check `pactify sub logs <id>`, then `--resume <id>` with a short brief asking for the result;
if still empty, rerun with another harness or model. For write tasks, review the full diff and
rerun acceptance as usual; get the handoff via `--resume <id>`, or write it from the diff and
your own checks. Resume preserves the original harness, cwd, access mode and worktree.

To select a model, name it explicitly in the task. If the CLI cannot honor it, execution must fail with the available models instead of silently falling back.
For rework, explicitly request `--resume <job-id> --worktree`, preserving the original cwd and harness.
For tasks you can wait about 9 minutes for, you can also use the `pactify:codex` / `pactify:kimi` / `pactify:agy` / `pactify:opencode` forwarding subagent:
it uses `model: haiku`, has only Bash, calls Bash once, and returns stdout verbatim. After a timeout, the main thread continues waiting.
Forwarders require an explicit delegation request; the agy forwarder may run only after the user invokes its skill slash command.
The callee leaves changes uncommitted for the host to review.

## Viewing delegation jobs

Use `/jobs` (or `/pactify:jobs`) to view the latest 10 jobs across repositories in a compact
table: title/issue, harness, status, repository and access/worktree mode, tokens, and activity
or attention. Running jobs appear first; pending matters are highlighted with next-step suggestions.
`/jobs all` reads all jobs but shows at most 20 rows within about 30 lines, counting the rest.
`/jobs <id>` shows one job's result excerpt, last 20 log lines, recorded diff statistics and
direct resume links. The namespaced forms also accept `all` or a job id.
Use `pactify sub cost` for token and cost totals by harness, model, repository or day, with recent quota snapshots; see the [cost contract](../../docs/sub-cost.md).

The jobs skill is read-only: it never applies, discards, cancels, dismisses or resumes jobs.
After host review, `pactify sub dismiss <id>` clears handled failure/warning attention; apply and discard also clear it, and pending worktrees must be handled first.
Suggestions follow the review workflow below: inspect the full diff and new files, personally
rerun acceptance including the full test suite, then apply after approval. Rework uses the
same worktree with `--resume <job-id> --worktree` after confirming the selected session.
Lists use `pactify sub status --json` (`--all` for all jobs); see the
[summary field contract](../../docs/sub-status.md). JSON is projected locally before entering
the conversation; list mode does not load individual results or logs.

The dashboard also has a **Jobs** tab with the same summaries, details, live logs,
diff and resume links as the local `pactify sub panel`. Open
`http://127.0.0.1:7777/#/jobs` after `pactify serve`; `#/jobs/<id>` links open a job
directly. serve does not provide login authentication: keep it on loopback, or put it
behind a reverse proxy that authenticates users (see the README Dashboard section). Alternatively, run `pactify sub panel` locally
and open its printed one-time URL (or use `--port 17100 --no-open`). Both browser
entries are read-only and share one Jobs UI; panel retains its independent Bearer
authentication. See [Jobs view](../../docs/jobs-view.md) for deployment prerequisites,
`PACTIFY_HOME`, limits and build commands.

## Reviewing a delegation

1. Read the summary and first-line warnings, then inspect the complete diff and new files. The worktree starts from the original repository HEAD without uncommitted source changes.
   Read `worktreePath` and `baseCommit` from the result, run `git -C <worktreePath> diff <baseCommit> --` and
   `git -C <worktreePath> ls-files --others --exclude-standard`, and open the listed new files.
2. The main thread personally reruns all Acceptance commands in the worktree, including the full test suite.
   The callee's report does not replace host validation.
3. Where possible, the main thread personally tries the real environment and observes the user flow, log readability, and actual model behavior as relevant.
   Record checks performed and gaps; automated tests do not establish real-environment acceptance.
4. Changes involving concurrency, permissions, or data deletion require independent review. Address findings and rerun acceptance checks.
5. After review passes, run `pactify sub apply <jobId>`, then `pactify sub discard <jobId>` to clean up the worktree.
   Apply leaves changes uncommitted and rejects the entire operation if affected paths have user changes or conflicts. Inspect the cause; do not force an overwrite or blindly retry.
   For rework, do not apply or discard. Run `sub run ... --resume <job-id> --worktree` with the original `--cwd` and harness,
   preserving access, requested model, and effort. Provide the rework brief on stdin, use the same background wait flow, and repeat steps 1–4.
   To abandon the task, skip apply and run `pactify sub discard <jobId>`. Cancel running jobs first with `pactify sub cancel <jobId>`.

Rework brief template:

```text
Continue the original task in the same worktree, preserving its scope and constraints.
Review findings: <file/line, actual vs expected behavior, reproduction steps or failing output>
Required changes: <individual fixes and constraints to preserve>
Tests to add: <regression scenarios; observe failure first; do not weaken existing tests>
Rerun acceptance: <all original acceptance commands, including the full test suite>
Include changes, command output, and remaining gaps in the final reply; do not commit or write report files.
```

Use `pactify sub run [harness] --resume <job-id>` (or `--resume=<job-id>`) to continue a particular job.
An exact id takes priority; a unique prefix of the full id or the final random component also works.
An ambiguous prefix fails and lists all matching jobs. The job must be finished and have a session id.
If the harness is omitted, it is inherited from the job; an explicit harness must match.
The new job records `resumeOf` as the selected job and keeps its original working directory.
Bare `--resume --worktree` selects the most recent completed worktree session with a session id for the original cwd and harness, including failed or canceled rounds.
With multiple resumable candidates, stderr lists their ids, titles and completion times, names the selected job, and suggests `--resume <job-id>`.
Check the target session before resuming and confirm the same worktree and base commit afterward. Do not run concurrent rounds in one worktree.
Resuming without `--worktree` still preserves the original isolation. Discarded or missing worktrees cannot be resumed.
Automatic selection of legacy sessions without isolation requires success. They cannot gain isolation midway; remove `--resume` to start a new worktree job.
Apply / discard accept any job id in a resume chain and act on its shared worktree; discard marks the whole chain.

## Result summary

- First line: `✓ codex succeeded`, `✗ codex failed` (or `canceled`), or `⚠️` followed by warnings joined with `; `.
- `Harness` and `CLI`: the tool and version actually used.
- `Model: <requested> → <actual> (unverified)`: requested and actual models; `(default)` when no model was requested. The unverified marker appears only without independent verification.
- `Access`: `read-only` or `write`.
- `Duration`, `Tokens: in N · out M`, optional `Cost` and `Quota`, and `Tool calls`: usage details; tokens do not establish an exact bill.
- `Changes: N files · +A/-D`: files and line counts for host diff review. `Worktree` and `Base commit` identify isolated changes.
- `Final reply`: at most 4096 bytes; `Session` and `Log` provide the session id and full log path for investigation.
- Warnings include `Actual model does not match the requested model`, `Tool-call limit exceeded`, and `Idle timeout`.

Jobs are stored in `$PACTIFY_HOME/jobs/<jobId>/`, defaulting to `~/.pactify/jobs/<jobId>/`.

**Session hiding**: delegated sessions are hidden when the job ends, so they do not appear in your own session / project lists. Every harness must declare its hiding mechanism, enforced by tests:

| Harness | Hiding mechanism | View the original session |
|---|---|---|---|
| codex | Archive the thread at completion (hidden in Codex desktop and mobile) | `codex unarchive <sessionId>` |
| kimi (minimal config, default) | Use the persistent shared `$PACTIFY_HOME/harness-homes/kimi` via `KIMI_CODE_HOME`; sessions never enter your own index. No `kimi web` or archive call. | `KIMI_CODE_HOME="$PACTIFY_HOME/harness-homes/kimi" kimi session list --cwd <directory>` (use `~/.pactify` if `PACTIFY_HOME` is unset) |
| kimi (`--with-user-config`) | Use the user's home and call kimi's archive API via a temporary localhost-only `kimi web --no-open`. If unavailable, fall back to the session archive field (CLI only, with a warning), then verify with `kimi session list`. | `kimi session list --archived --cwd <directory>`, or restore from archived sessions in kimi web |

Archived sessions are unhidden by `--resume` and hidden again afterward. Minimal kimi sessions stay in their separate home throughout resume. The `Session` line contains its id. Hiding failures appear on the first line, for example if a kimi update changes its session format. kimi derives workspace entries from session records and keeps them after archiving, just as when you archive a session manually.

Kimi uses separate 0700 homes: `harness-homes/kimi` for write jobs and
`harness-homes/kimi-readonly` for read-only jobs. Both link `credentials/`, `oauth/`
and `device_id` to the user's home; credential files are never linked individually.
Write jobs also link `config.toml`; read-only jobs parse at most 1 MiB and copy only
`default_model`, `models` and `providers`, including credential references. All other
user sections are omitted. Before each job, pactify renders a private 0600 config.
Identical regular configs share leases; differing bytes or unsafe metadata require
an idle home and an atomic replacement. The last job removes the generated config.
Config contents are never logged. Permission rules and tool policy are generated. The execution-time tool allowlist is Read,
Grep, Glob, ReadMediaFile, TodoList, TaskList, TaskOutput and WaitFor; additional
deny rules cover Write, Edit, Bash, Agent*, Cron* and TaskStop. Shell, sub-agent,
cron, MCP and network tools are unavailable, including for child agents. ACP uses
default mode; plan mode alone does not enforce this policy. Every agent's
`llm.tools_snapshot` is checked after the job; an unexpected tool fails with
`readonly_violation` even on failed or canceled jobs. Missing evidence fails successful
jobs; failed/canceled jobs retain their original classification and quota fallback. An unchanged
repository baseline is also required. This is a tool policy, not an OS sandbox.
The global tool policy also applies to project MCP/plugin/agent tools. Project-local
`.kimi-code/local.toml` only supplies workspace directories. Read-only jobs neutralize
execution-related Kimi environment switches while retaining model/authentication
variables. Neither home has `mcp.json`. Credential quarantine/refresh handling applies to both.
Keep these homes for resume. Kimi requires the original access and minimal/user
config modes on resume; use a fresh session to switch. Read-only cannot use
`--with-user-config`.
Codex's minimal configuration and `--with-user-config` behavior are unchanged.
Run `pactify sub doctor kimi` to verify the managed links, a successful probe in
that home, no probe entry in the user's index, and no `mcp.tools_discovered` event.
The result's Harness line reports `kimi (minimal config)` or `kimi (user config)`.

## agy (experimental)

agy is experimental: no OS sandbox; write permission requests are approved by pactify; results vary by model.
Run only via `/delegate-agy` or `/pactify:delegate-agy`. Read-only permission requests are denied.
The v2 adapter targets Antigravity CLI 1.4.0, with one persistent stream-json process
per job. Every call explicitly supplies a model, defaulting to router settings or
`gemini-3.8-flash-low`. Tokens come from step usage; result totals are cumulative.

Read-only access uses `--mode plan`, an exact command allowlist, and managed
PreToolUse hooks that deny writing tools and shell composition. A fail-closed
baseline covers tracked, untracked, and ignored files. Write jobs report
`sandboxed=false`; `--sandbox` is not used. Managed hooks reject `BypassSandbox`.
Doctor checks denied-write evidence (WARN if no write was attempted) and Network.
Empty results, empty denied results, and print timeouts fail. Incomplete replies get
up to two followups (`--completion-followups`), in the same process. Transient errors
reuse the conversation; process exits restart with `--conversation`. Non-billable
`/usage` snapshots before and after each job expose bucket fractions and reset times
in quota.json, result JSON, `sub cost`, and Dashboard Quota.
Cancellation snapshots all descendants before signaling agy, then sends surviving
descendants SIGTERM and SIGKILL as needed, verifying birth identity before each signal.
Normal completion leaves descendants alone.

Minimal config uses `HOME=$PACTIFY_HOME/harness-homes/agy` (0700). Every real HOME
top-level entry except `.gemini` is linked, refreshed under a lock before launch.
The private `.gemini/antigravity-cli` contains a link to the original
`antigravity-oauth-token`; pactify never reads or copies it. Sessions and native
runtime files stay in the private directory. Replaced link copies are removed
without following symlinks, with a warning. Keep this directory for resume.
`--with-user-config` restores real HOME and warns that sessions remain visible in
the user's list; agy has no supported archive API. `sub doctor agy` checks the
normal minimal configuration, including directory permissions and link integrity.

## Access modes

| | codex | kimi | agy | opencode |
|---|---|---|---|---|
| Support | Supported (default) | Supported | Experimental | Supported (1.x and 2.x) |
| `--write` | workspace-write sandbox, **network enabled** (supports tests with local servers and dependency downloads) | kimi default mode (direct writes, no sandbox) | `--dangerously-skip-permissions` (full write access) | build + per-type permissions + directory guardrail |
| `--read-only` | read-only sandbox, no network | private tool allowlist + tools snapshot + fail-closed baseline | plan + exact commands + managed hooks + fail-closed baseline | plan + read/glob/grep/list only (no shell) + fail-closed baseline |

## Known limitations

- kimi `--read-only` enforces a tool allowlist in a generated private config, checks tools snapshots and repository metadata, and rejects `--with-user-config`.
- A user-config job that fails during startup without returning a session id cannot be archived. Minimal kimi sessions remain separate even in this case.
- Each delegation has about 20–50 thousand tokens of fixed callee overhead, making it unsuitable for small tasks.
- Only locally installed, logged-in official CLIs are supported. Follow the summary warnings when independent model verification fails.
- Automated tests do not cover installation and delegation in real Claude Code. Release acceptance still requires a Claude Code smoke test.

## opencode

opencode is supported on both 1.x and 2.x: no OS sandbox; write permission requests are approved by pactify; results vary by model. Version 1.x uses v1 HTTP/SSE through a private authenticated
`opencode serve` process per job; 2.x uses the reviewed `@opencode/cli` API contract.
Use `/delegate-opencode`, `/pactify:delegate-opencode`, or an explicit user request
to delegate to opencode, with the shared delegation guide and host review workflow.
See the [support policy](../../docs/harness-updates.md) for both majors.
Out-of-workspace directory access for file tools is denied; shell is not confined.
Requested models use dynamic `provider/model` ids; omitting the model preserves opencode's default.
Assistant messages confirm the actual model. Tokens and cost use the session summary;
resumed jobs subtract the starting totals. Input includes cache reads and writes, and output includes reasoning.

Write access uses build with permissions decided by type. Pactify blocks opencode
file tools from accessing directories outside the job workspace, job TMPDIR and
`$PACTIFY_HOME/cache`. Allowed requests receive `once`; unknown permissions and
`doom_loop` are denied. Shell commands can still reach files those checks cannot
see. Opencode runs without an OS sandbox; this is a guardrail, not isolation.
Use `--read-only` for reviews: plan exposes only read/glob/grep/list (no shell), every permission
request is denied, and any repository diff fails as `readonly_violation`. Doctor probes
both access profiles and verifies the write session denies outside directories and
requires adapter checks for its writable roots (`outside=deny`, `inside=ask`).
Results report `sandboxed=false`: neither profile provides an OS sandbox. Use `--worktree`.

Two persistent 0700 mirrors at `$PACTIFY_HOME/harness-homes/opencode/share` and
`config` replace `XDG_DATA_HOME` and `XDG_CONFIG_HOME`. Each mirrors the user's
corresponding directory except `opencode`; only `share/opencode/auth.json` links
to the original. Credentials are never read or copied. Delegated sessions stay in
the private database. The three flags `OPENCODE_DISABLE_CLAUDE_CODE=1`,
`OPENCODE_DISABLE_EXTERNAL_SKILLS=1`, `OPENCODE_PURE=1` disable optional integrations.
`--with-user-config` restores both original XDG directories and integrations, with
a warning that sessions remain visible. Resume preserves both recorded directories
and requires the same config mode. `sub doctor opencode` verifies the default path.

Doctor accepts `--model <id>` with exactly one explicit harness, for example
`pactify sub doctor opencode --model provider/model`. The requested
model overrides doctor defaults and name heuristics for every probe. It uses the
same backend validation as `sub run --model`, including agy's fallback to CLI
validation when its model-list pre-check fails. Invalid or unconfirmed models fail
the check (exit 1); omitting the harness or supplying several is an argument error
(exit 2). Omitting `--model` preserves default model selection. Verified-version
records retain their existing format and require all applicable checks to pass.

Model-error alternatives in both `sub run` and doctor show at most 10 ids, preferring
the requested provider and then the closest model names. A truncated list includes
the omitted count and a full-list hint: `opencode models`, `agy models`,
`codex debug models`, or `/model` inside interactive `kimi`. When doctor's read-only
probe returns the same error as its write probe, its reason says
`same as above (Write access)` instead of repeating the alternatives.

Configured aliases can be used with `/delegate @alias <task>`; inspect
`pactify sub router --json` for available names. For quota groups, quota fallback
and `--worktree-from`, see [routing](../../docs/router.md).
