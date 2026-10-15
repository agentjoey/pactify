---
name: codex
description: Delegate a large coding task (multi-file change, investigation, or anything that needs running tests) to the codex harness and get back a verified result summary. Not for small edits you can do directly.
---

# Delegate to codex

**If the user invoked this skill explicitly (`/codex` or `/pactify:codex`), always delegate — whatever the task
size. The user chose the harness; do not do the work yourself instead.**

Launch command (step 2 below):

    pactify sub run codex --write --worktree --wait 0s --json <<'BRIEF'
    ...complete brief from step 1...
    BRIEF

For read-only tasks replace `--write` with `--read-only`; never pass both.

<!-- BEGIN SHARED-DELEGATION -->
When deciding on your own, delegate only large tasks: 3+ files, exploration and test runs, or more than
~10 minutes. Handle small edits directly; each delegation costs tens of thousands of callee tokens.
Do not delegate tasks that depend on conversation context you cannot turn into a self-contained brief,
or tasks needing frequent user interaction. Explicit skill invocations still delegate regardless of size;
harness permission restrictions still apply.

1. Write a self-contained brief using the five sections below. The callee starts cold and cannot see
   this conversation. Include relevant paths, scope, access mode, numbered goals and exact acceptance
   commands. Known constraints and special cases must be written down: the callee cannot infer them.
   Before changing a rule, require checking callers, special cases and documented exceptions, and
   reporting those dependencies in the final reply.
   Do not edit existing tests to fit the new behavior unless the brief explicitly says the old behavior
   is wrong; name any such exception in the final reply. Require a failing regression test before each
   behavior change. The acceptance commands must include the project's **full** test command, not just
   affected packages. For Go tests use `PACTIFY_HOME="$(mktemp -d)"` to isolate pactify state.
   Forbid commits and credential access. Optionally add: "Do not spawn subagents or write report files;
   put conclusions and command output in the final reply."
   Use this complete example as a template, replacing its paths, constraints and commands for the project:

   ```text
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
   PACTIFY_HOME="$(mktemp -d)" go test ./... -count=1
   ```

2. Start the launch command above from the **main thread** (not a subagent — its background command
   notifies only that subagent). Supply the complete brief on stdin, with a quoted heredoc delimiter
   absent from the brief. Preserve requested models with `--model <name>` and effort with
   `--effort <level>`, quoting shell arguments; never silently switch models.
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

4. To request rework, do not apply or discard. Pass `--resume --worktree` on the next `run` with the
   original `--cwd` and harness, preserving access mode and any requested model/effort. Supply a new
   brief on stdin and collect it with the same main-thread wait procedure in step 2.
   Resume continues in the same worktree with the same base commit; the summary shows the full change
   across all turns. Omitting `--worktree` on resume also preserves existing isolation.
   It selects the most recent completed worktree session with a session id, including a failed or canceled
   turn; legacy workspace sessions still require success. Confirm this is the intended session before
   resuming, and check the returned worktree path/base commit. Do not start concurrent turns in one worktree.
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

For a quick task you are willing to block on (under ~9 minutes), you may instead delegate the complete
brief to the `pactify:codex` subagent, which calls `pactify sub run codex --worktree --wait 540s --json` once,
with access/model/effort flags from the brief, and returns stdout verbatim. If it returns a pending job id (exit 3), the main thread must still run
`pactify sub wait <jobId>` with `run_in_background: true` and `timeout: 7200000` to collect the result.
