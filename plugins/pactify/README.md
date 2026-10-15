# Pactify harness plugin

Delegate large tasks from Claude Code to the official codex / kimi / agy CLI installed locally, then review the result summary and workspace changes.
The default harness is codex; kimi is an experimental harness (Phase 0: large tasks completed in one attempt 6/10; read-only work never converged).

## Installation

Run these commands in Claude Code:

```text
/plugin marketplace add agentjoey/pactify
/plugin install pactify@pactify
```

Prerequisites: `pactify` is on PATH and `pactify sub --help` works; the official codex / kimi / agy CLI you want to call is installed and logged in.
The SessionStart hook only checks that pactify and its `sub` command are available. It suggests installing or upgrading when needed without blocking the session.
The plugin does not proxy models, manage logins, or read or copy credentials.

## Usage

```text
/delegate Add expiration and regression tests to src/cache/, then run the project's full test suite
/codex Add expiration and regression tests to src/cache/, modify only that directory, and run the full npm test suite
/agy Review src/cache/ with read-only access using built-in file tools only; do not run shell commands
/kimi Add expiration and regression tests to src/cache/ with write access, then run the project's full test suite
```

Use `/pactify:delegate`, `/pactify:codex`, `/pactify:kimi`, or `/pactify:agy` to avoid conflicts with other skills of the same name.
`/delegate` uses the default codex harness; `/codex`, `/kimi` and `/agy` select a harness explicitly. An explicit invocation always delegates, regardless of task size.
Delegation suits tasks involving 3 or more files, exploration and testing, or more than about 10 minutes of work. Claude can handle small edits directly.
When choosing autonomously, avoid delegating tasks that depend on extensive conversation context that cannot fit in a self-contained brief, or require frequent user interaction.
Explicit invocations still delegate, but kimi's read-only restriction remains: never raise permissions yourself.

The Claude main thread first writes a complete brief with Background, numbered goals, Known constraints and special cases, General constraints, and Acceptance commands.
Specify paths, scope, access, and known exceptions: the callee cannot infer constraints from the current conversation. Acceptance commands must include the project's full test suite.
Require inspection of callers, special cases, and documented exceptions before changing rules, and a failing regression test before each behavior change. Do not change existing tests to accommodate new behavior unless the brief explicitly identifies the old behavior as wrong; name any such changes in the final reply.
Forbid commits and credential access; optionally forbid subagents and report files.
All four skills embed the same template, complete example, and review workflow. Bats checks that their shared blocks are identical.

The main thread runs `pactify sub run [harness] --write --worktree --wait 0s --json`.
Omitting the harness uses codex: `/delegate` runs `pactify sub run --write --worktree --wait 0s --json`.
For read-only tasks needing commands, use codex `--read-only`. agy `--read-only` supports file-only reviews using built-in file tools; kimi still rejects read-only tasks (exit 2).
Exit 3 means the job is still running and stdout contains `jobId`. The main thread runs
`pactify sub wait <jobId>` with `run_in_background: true` and `timeout: 7200000`, then reviews the result as described below.
If the background wait stops at its time limit, check `pactify sub status` and resume the same wait; do not start a duplicate job.
Exit codes 0 / 1 / 2 mean success, failure, and invalid arguments.
Retrieve the result again with `pactify sub result <jobId>`.

To select a model, name it explicitly in the task. If the CLI cannot honor it, execution must fail with the available models instead of silently falling back.
For rework, explicitly request `--resume --worktree`, preserving the original cwd and harness.
For tasks you can wait about 9 minutes for, you can also use the `pactify:codex` / `pactify:kimi` / `pactify:agy` forwarding subagent:
it uses `model: haiku`, has only Bash, calls Bash once, and returns stdout verbatim. After a timeout, the main thread continues waiting.
The callee leaves changes uncommitted for the host to review.

## Reviewing a delegation

1. Read the summary and first-line warnings, then inspect the complete diff and new files. The worktree starts from the original repository HEAD without uncommitted source changes.
   Read `worktreePath` and `baseCommit` from the result, run `git -C <worktreePath> diff <baseCommit> --` and
   `git -C <worktreePath> ls-files --others --exclude-standard`, and open the listed new files.
2. The main thread personally reruns all Acceptance commands in the worktree, including the full test suite. Use `PACTIFY_HOME="$(mktemp -d)"` for Go tests.
   The callee's report does not replace host validation.
3. Where possible, the main thread personally tries the real environment and observes the user flow, log readability, and actual model behavior as relevant.
   Record checks performed and gaps; automated tests do not establish real-environment acceptance.
4. Changes involving concurrency, permissions, or data deletion require independent review. Address findings and rerun acceptance checks.
5. After review passes, run `pactify sub apply <jobId>`, then `pactify sub discard <jobId>` to clean up the worktree.
   Apply leaves changes uncommitted and rejects the entire operation if affected paths have user changes or conflicts. Inspect the cause; do not force an overwrite or blindly retry.
   For rework, do not apply or discard. Run `sub run ... --resume --worktree` with the original `--cwd` and harness,
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

`--resume` selects the most recent completed worktree session with a session id for the original cwd and harness, including failed or canceled rounds.
Check the target session before resuming and confirm the same worktree and base commit afterward. Do not run concurrent rounds in one worktree.
Resuming without `--worktree` still preserves the original isolation. Discarded or missing worktrees cannot be resumed.
Legacy sessions without isolation must have succeeded to resume. They cannot gain isolation midway; remove `--resume` to start a new worktree job.
Apply / discard accept any job id in a resume chain and act on its shared worktree; discard marks the whole chain.

## Result summary

- First line: `✓ codex succeeded`, `✗ codex failed` (or `canceled`), or `⚠️` followed by warnings joined with `; `.
- kimi jobs include the experimental warning on the first line. It is stored in `Result.Warnings` without changing job status.
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

Kimi's managed home is 0700 and has no `mcp.json`. Before each launch pactify
repairs three symlinks: `config.toml`, `credentials/`, and `device_id`, pointing
to the process's `KIMI_CODE_HOME` or `~/.kimi-code` when unset. Missing originals
require `kimi login`; pactify never reads or copies credential/config contents.
If kimi replaces a link with a regular file or directory, the job warns and
removes that private copy at completion; the next launch recreates the link.
Keep this shared home for resume. Kimi resume preserves the recorded home and
requires the original minimal/user config mode; use a fresh session to switch.
Codex's minimal configuration and `--with-user-config` behavior are unchanged.
Run `pactify sub doctor kimi` to verify the managed links, a successful probe in
that home, no probe entry in the user's index, and no `mcp.tools_discovered` event.
The result's Harness line reports `kimi (minimal config)` or `kimi (user config)`.

## agy (experimental)

agy is an experimental harness (evaluation pending), adapted to Antigravity CLI 1.3.5.
The stream-json backend sends prompts on stdin, verifies `init.model`, and counts
input tokens as `input_tokens + cache_read_tokens`. `agy models` provides the dynamic
model ids; unsupported requested models fail with available choices. SIGINT cancels
the CLI; a grace timeout escalates to kill and waits for exit. Headless auto-denials
fail the job even when the CLI returns SUCCESS, because required work did not execute.

Read-only agy tasks can use built-in file tools only. Every shell command, including
`git log` and `grep`, requires command permission, is auto-denied in headless mode,
and ends the turn without a usable final reply; writes are denied the same way.
The job remains failed and explains this restriction. Do not request commands or
tests in an agy read-only brief; the host runs acceptance. Choose codex `--read-only`
for commands, or agy `--write` only with write permission. `--mode=plan` alone is
not a write sandbox: plan plus `--dangerously-skip-permissions` permits writes.
Read-only access must never include skip-permissions. Doctor deliberately attempts
a shell write: denial plus an absent target and no diff passes its read-only check,
even though that probe job failed. Session hiding and Managed home use the write probe.
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

| | codex | kimi | agy |
|---|---|---|---|
| `--write` | workspace-write sandbox, **network enabled** (supports tests with local servers and dependency downloads) | kimi default mode (direct writes, no sandbox) | `--dangerously-skip-permissions` (full write access) |
| `--read-only` | read-only sandbox, no network | Not offered in the first release | `--mode=plan` without skip; built-in file tools only; shell commands/writes auto-deny and end the turn |

## Known limitations

- kimi read-only access is not offered in the first release (`sub run kimi --read-only` exits 2): Phase 0 plan mode never converged on read-only tasks. Use codex instead.
- A user-config job that fails during startup without returning a session id cannot be archived. Minimal kimi sessions remain separate even in this case.
- Each delegation has about 20–50 thousand tokens of fixed callee overhead, making it unsuitable for small tasks.
- Only locally installed, logged-in official CLIs are supported. Follow the summary warnings when independent model verification fails.
- Automated tests do not cover installation and delegation in real Claude Code. Release acceptance still requires a Claude Code smoke test.
