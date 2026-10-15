# Pactify

**Use other coding agents as subagents from Claude Code.**

Delegate large tasks — multi-file changes, investigations, anything that needs test runs — from Claude Code to Codex (default) or Kimi (experimental). pactify runs them as background jobs, independently verifies the model that actually ran, reports tokens and quota, isolates the change in a git worktree for you to review, apply or discard, and leaves no trace in the agents' own session lists.

## Install

Install the pactify CLI using either path below, then add the Claude Code plugin.
The Codex or Kimi CLI you want to delegate to must also be installed and logged in.

**Go:**

```bash
go install github.com/agentjoey/pactify/cmd/pactify@latest
```

**Install script:**

```bash
curl -fsSL https://pactify.dev/install.sh | sh
```

**Claude Code plugin:** inside Claude Code, run:

```text
/plugin marketplace add agentjoey/pactify
/plugin install pactify@pactify
```

Check the installed harnesses before your first delegation:

```bash
pactify sub doctor
```

This makes real harness calls, consumes tokens, and records CLI versions only after
all applicable checks pass. See [what doctor checks](docs/sub-doctor.md).
These instructions describe the next release; if your installed binary lacks a
listed command or flag, upgrade to a release containing it. See the [changelog](docs/CHANGELOG.md).

## Quickstart

1. Run `pactify sub doctor` as above.
2. In Claude Code, delegate a self-contained task:

   ```text
   /delegate Add an expiry policy to src/cache/ with regression tests. Use a worktree, leave changes uncommitted, and run the project's full test suite.
   ```

   `/delegate` uses codex. `/codex` selects it explicitly; `/kimi` selects the
   experimental harness. If short names conflict with another plugin, use
   `/pactify:delegate`, `/pactify:codex` or `/pactify:kimi`.
3. Claude starts the job in an isolated git worktree, waits in the background,
   and reads the result summary. Review the actual diff and new files using the
   `Worktree` and base commit from that summary:

   ```bash
   git -C <worktree-path> diff <base-commit> --
   git -C <worktree-path> ls-files --others --exclude-standard
   ```

   Open the listed new files too: `git diff` does not show untracked content.
4. Accept the change into your original workspace, without committing:

   ```bash
   pactify sub apply <job-id>
   pactify sub discard <job-id>  # remove the worktree after a successful apply
   ```

   To reject the change, run only `pactify sub discard <job-id>`. Cancel a running
   job before discarding it. Apply refuses affected paths that have changed in
   the original workspace instead of overwriting your edits.

Worktrees start from the original `HEAD`; uncommitted source changes are not
copied in, and a dirty source workspace produces a warning. CLI callers must
pass `--worktree` explicitly; omitting it runs in the current workspace.

## What comes back

The summary follows [`Result.Markdown()`](internal/sub/envelope.go). This is an
illustrative successful result: angle-bracket values are placeholders, not a
record of a measured run. The field labels reflect the current CLI output.

````text
✓ codex succeeded

Harness: codex · CLI: <cli-version>
Model: <requested-model> → <actual-model>
Access: write
Duration: <duration>
Tokens: in <input-tokens> · out <output-tokens>
Quota: <reported-quota-window>
Tool calls: <tool-calls>
Changes: <file-count> files · +<added>/-<deleted>
Worktree: <worktree-path>
Base commit: <base-commit>
- src/cache/expiry.ts (+<added>/-<deleted>)
- src/cache/expiry.test.ts (+<added>/-<deleted>)

Final reply:
```
<The agent's final reply: changes, checks run, and remaining gaps.>
```

Session: <session-id>
Log: <pactify-home>/jobs/<job-id>/log.jsonl
````

Warnings replace the success heading when present, including experimental-harness,
model verification and session hiding warnings. Quota appears when the harness
reports it; dollar cost appears only when available. Token counts are usage,
not an exact bill. Use `pactify sub result <job-id> --json` for structured output.

## Harnesses and evidence

Codex is the default for writing and read-only investigations. Kimi is
**experimental** and offers no read-only mode; use codex for read-only work.

[Phase 0](docs/phase0-report.md) covered **26 real delegations**. Codex completed
**9 of 10 large tasks in one attempt**, and all **3 read-only tasks** returned
findings. Kimi missed both the large-task and read-only criteria, so it remains
experimental. Median additional context on the Claude side was about **3.3k
tokens per delegation**; this is separate from the delegated agent's own usage.

## Jobs, rework and session lists

The CLI surface is `pactify sub run | wait | status | logs | result | cancel | apply | discard | doctor`.
For direct CLI use, send a complete brief on stdin, including scope, permissions
and the full test command:

```bash
pactify sub run --write --worktree --wait 0s --json < brief.txt
pactify sub wait <job-id>
pactify sub status <job-id>
pactify sub logs <job-id>
pactify sub result <job-id>
```

`run` defaults to codex when the harness is omitted. Exit code `3` means the job
is still running; use the returned job id with `wait`. `cancel <job-id>` stops a
job. Codex supports `--read-only` instead of `--write`; Kimi does not.

For rework, run from the same original directory with the same harness:

```bash
pactify sub run --resume --write --worktree --wait 0s --json < rework.txt
```

This continues the latest completed isolated session with a session id, including
a failed or canceled turn. It reuses the worktree and base commit; the next
summary covers the full change across turns. Do this before applying or
discarding the work. A discarded worktree cannot resume. Use `--fresh` for a new
session; an existing session that ran without isolation cannot acquire it mid-session.

**No trace in your agents' own session lists:** codex threads are archived when
the job closes; kimi sessions are archived through its own interface, with a
file-based fallback and warning if that interface fails. Resume reopens the
session and hides it again afterward. This is session-list cleanup: pactify
keeps job state, results and logs under `$PACTIFY_HOME/jobs/` (default:
`~/.pactify/jobs/`). Hiding failures are reported, and an early startup failure
that yields no session id may leave a visible session.

Read the [plugin guide](plugins/pactify/README.md) for delegation details and
permissions, the [harness design](docs/specs/harness-subagents.md) for scope and
tradeoffs, and the [architecture](docs/architecture.md) for implementation.

## Governance mode (optional)

The pact protocol remains available for teams coordinating through plain files
in `.pact/`: `log.jsonl` is the source of truth and `STATE.yml` is its projection.
Workers cannot accept their own work; a feature cannot merge until all its tasks
are accepted. The existing orchestrator and local `pactify serve` dashboard are
frozen governance tools. Harness delegation does not require setting up `.pact/`.
The hosted relay is retired.

- [Protocol specification](docs/specs/pact-protocol.md) and [JSON schemas](schemas/)
- [Governance onboarding](docs/onboarding.md) and [operations](docs/operations.md)
- [Contributing](CONTRIBUTING.md) and [changelog](docs/CHANGELOG.md)

## License

[MIT](LICENSE) © 2026 Joey (agentjoey).
