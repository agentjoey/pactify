---
name: delegate-opencode
description: Use when the user explicitly asks to delegate a coding task to the opencode harness. no OS sandbox; write permission requests are approved by pactify; out-of-workspace directory access for file tools is denied; shell is not confined; results vary by model.
---

Before the first pactify command in a session, run `pactify-check --quiet` once (this plugin's `bin/`
is on the Bash PATH). No output means the CLI is ready. If `pactify-check` is not found, run
`pactify version` and compare it with the minimum version 0.14.0.
If the CLI is missing or too old, show the user these commands and ask them to install or upgrade it.
Never run these commands or any installer yourself:

```sh
curl -fsSL https://pactify.dev/install.sh | sh
go install github.com/agentjoey/pactify/cmd/pactify@latest
```

The host must allow `pactify sub` outside its sandbox, since harness CLIs run outside the workspace.

# Delegate to opencode

**opencode has no OS sandbox; write permission requests are approved by pactify;
results vary by model.** Use only when the user explicitly requests delegation.
Read-only permission requests are denied.
Out-of-workspace directory access for file tools is denied; shell is not confined.
Prefer the default codex harness (`/delegate` or `/delegate-codex`).

**If the user invoked this skill explicitly (`/delegate-opencode` or `/pactify:delegate-opencode`), always delegate —
whatever the task size. The user chose the harness; do not do the work yourself instead.**

Launch command (step 2 below):

    pactify sub run opencode --write --worktree --wait 0s --json <<'BRIEF'
    ...complete brief from step 1...
    BRIEF

Use `--read-only` instead for investigations and reviews. Use `--write` only when the user allows changes. Never raise permissions yourself.
Models come from the live v1 `/config/providers` or v2 `/api/model` catalog, with provider/model ids; preserve the requested model.
Omit `--model` to keep opencode's default. Resume with `--resume <job-id> --worktree` and the original cwd.

<!-- BEGIN SHARED-DELEGATION -->
Delegate only when the user explicitly asks: a pactify slash command, or a clear request to delegate
or hand off to a harness; otherwise do the work directly. Task size alone never authorizes delegation.
If `PACTIFY_JOB` is set, you are already running inside a pactify job: do the task directly and
never run `pactify sub run`; the CLI refuses it.
agy runs only via its explicit `/delegate-agy` slash command (including the
`/pactify:delegate-agy` namespaced form); never select it on your own.
Preserve the user's requested harness and access mode; harness permission restrictions still apply.
Configured aliases can be used with `/delegate @alias <task>`.
Inspect `pactify sub router --json` for available aliases and preserve any route
explicitly chosen by the user.

1. Write a self-contained brief using the five sections below. The callee starts cold and cannot see
   this conversation. Write a short title on the first line; if the user uses an issue tracker,
   its identifier can be included in the title. Include relevant paths, scope, access mode, numbered goals and exact acceptance
   commands. Known constraints and special cases must be written down: the callee cannot infer them.
   Before changing a rule, require checking callers, special cases and documented exceptions, and
   reporting those dependencies in the final reply.
   Do not edit existing tests to fit the new behavior unless the brief explicitly says the old behavior
   is wrong; name any such exception in the final reply. Require a failing regression test before each
   behavior change. The acceptance commands must include the project's **full** test command, not just
   affected packages.
   Forbid commits and credential access. Optionally add: "Do not spawn subagents or write report files;
   put conclusions and command output in the final reply."
   Use this complete example as a template, replacing its paths, constraints and commands for the project:

   ```text
   Fix concurrent cache refresh ordering
   Background:
   src/cache/ expires entries on read; concurrent refreshes can serve stale data.
   Goals:
   1. Fix refresh ordering in src/cache/ without changing its public API.
   2. Add regression coverage; first run each new test and see it fail.
   Known constraints and special cases:
   Zero TTL means no expiry; preserve it. Inspect callers and document exceptions in the final reply.
   General constraints:
   Write access only in src/cache/. Do not commit or inspect credentials.
   Do not edit existing tests to fit the new behavior; fix the implementation instead.
   Do not spawn subagents or write report files; reply with conclusions and command output.
   Acceptance commands:
   go build ./...
   go test ./...
   ```

2. Start the launch command above from the **main thread** (not a subagent — its background command
   notifies only that subagent). Supply the complete brief on stdin, with a quoted heredoc delimiter
   absent from the brief. Preserve requested models with `--model <name>` and effort with
   `--effort <level>`, quoting shell arguments; never silently switch models.
   Quota fallback can create a new job; `sub wait` and `sub result` follow the fallback chain from
   the first job id to its final result. Use the returned final `jobId` for review/apply/discard.
   When a quota failure already left changes, automatic fallback stops. Review the diff first,
   then start a fresh session with `pactify sub run <next> --worktree-from <job-id>` and a complete
   brief on stdin; this transfers the worktree while preserving its original base. For a legacy
   workspace job without a worktree, review and start a fresh workspace run instead.
   It exits 3 immediately with `{"jobId": ...}`: pending, not failure. Parse the job id from stdout.
   For any other error, report it and available models if supplied; do not blindly retry.
   Then run, with `run_in_background: true` **and `timeout: 7200000`** (otherwise the background
   command stops at the default ~10-minute limit while the job keeps running):

       pactify sub wait <jobId>

   If the background command is reported as stopped (time limit) rather than completed, check
   `pactify sub status`, then start the same wait again.

3. When the background command completes, review before using or applying its changes:
   - Read the summary and first-line warnings (`Actual model does not match the requested model`, `Tool-call limit exceeded`, `Idle timeout`).
     Retrieve it again with `pactify sub result <jobId>` if needed. Only claim independent model
     verification when the result confirms it; full logs stay at the reported path.
   - If the result warns `empty final reply` (after one follow-up, or when the session ended first), handle it by access mode:
     For read-only, the task is not done: check `pactify sub logs <id>`, then use `--resume <id>`
     with a short brief asking for the result. If it is still empty, rerun with another harness or model.
     For write, review the full diff and rerun acceptance as usual; get the handoff via `--resume <id>`,
     or write it from the diff and your own checks. Preserve the original harness, cwd and access mode
     when resuming, and keep `--worktree` for a worktree job.
   - Inspect the complete diff first. Large tasks use a detached worktree based on the original HEAD;
     uncommitted source changes are not carried over. Read `worktreePath` and `baseCommit` from the
     result JSON (or `Worktree` and `Base commit` in the summary), then run:

         git -C <worktreePath> diff <baseCommit> --
         git -C <worktreePath> ls-files --others --exclude-standard

     Open the listed new files too: `git diff` alone does not show untracked content.
   - The main thread must personally rerun all acceptance commands, including the full test suite,
     in the worktree. Do not treat the callee's test report as host verification.
   - Where feasible, personally try the change in the real environment: exercise the actual user
     flow and inspect output readability and actual model behavior when relevant. Record what was
     tried and what could not be verified; automated tests alone do not establish real-world behavior.
   - For high-risk changes involving concurrency, permissions, or data deletion, obtain an independent reviewer
     before approval. Give that reviewer the brief and complete diff; resolve findings and rerun acceptance.
   - Decide: on approval, run `pactify sub apply <jobId>` to apply without committing. Apply checks all
     affected paths first; related user edits or conflicts fail without partially applying. Resolve them
     deliberately; do not force or blindly retry. After successful apply, run `pactify sub discard <jobId>`
     to remove the worktree. For rework, keep it and follow step 4. To abandon, discard without apply;
     a running job must first be canceled with `pactify sub cancel <jobId>`.

4. To request rework, do not apply or discard. Pass `--resume <job-id> --worktree` on the next `run`
   with the id of the job being reviewed, the original `--cwd` and harness, preserving access mode,
   config mode and any requested model/effort. Supply a new brief on stdin and collect it with the
   same main-thread wait procedure in step 2. An exact id is safest; a unique prefix of the full id
   or its final random component also works. An ambiguous prefix fails and lists every candidate.
   Resume continues in the same worktree with the same base commit; the summary shows the full change
   across all turns. Omitting `--worktree` on resume also preserves existing isolation.
   Bare `--resume --worktree` still selects the most recent completed worktree session with a session
   id, including a failed or canceled turn; automatic legacy workspace selection requires success.
   With multiple resumable candidates it lists ids, titles and completion times on stderr and names
   the selected job. Prefer specifying the job id to avoid continuing a different task.
   The selected job must be finished and have a session id. Omitting the harness inherits that job's
   harness; an explicit harness must match. Check the returned worktree path/base commit.
   Do not start concurrent turns in one worktree.
   A discarded or missing worktree cannot resume. A legacy session cannot acquire isolation mid-session:
   start a fresh `--worktree` job instead. Apply and discard accept any job id in the resumed chain and
   affect its shared worktree; discard marks every turn as discarded.
   Use this rework message template, filling in concrete evidence and exact commands:

   ```text
   Continue the same task in the same worktree; the original scope and constraints still apply.
   Review findings: <file/line, observed behavior, expected behavior, reproduction or failing output>
   Required changes: <numbered fixes and constraints to preserve>
   Tests to add: <regression cases; see them fail first; do not weaken existing tests>
   Rerun acceptance: <every original acceptance command, including the full test suite>
   Reply with changes, command output and remaining gaps; do not commit or write report files.
   ```

   Repeat step 3 after every rework turn; apply only after all required checks and reviews pass.
<!-- END SHARED-DELEGATION -->

**Read-only access** uses `agent: "plan"` with only read/glob/grep/list (no shell), denies every
permission request, and fails with `readonly_violation` if the final repository diff is nonempty.
Doctor verifies the completed session's permission policy for both access profiles.
In write mode, pactify blocks opencode file tools from accessing directories outside
the job workspace, job TMPDIR and `$PACTIFY_HOME/cache`. Shell commands can still
reach files these checks cannot see. This is a guardrail, not isolation.
Both profiles report `sandboxed=false`: opencode has no OS sandbox, so repository checks do not
restrict effects outside the repository. Keep `--worktree` for delegated tasks.
