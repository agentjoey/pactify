---
name: kimi
description: Delegate a large coding task (multi-file change, investigation, or anything that needs running tests) to the kimi harness and get back a verified result summary. Not for small edits you can do directly.
---

# Delegate to kimi

**If the user invoked this skill explicitly (`/kimi` or `/pactify:kimi`), always delegate — whatever the task
size. The user chose the harness; do not do the work yourself instead.**

When you are deciding on your own whether to delegate, use kimi only for **large** tasks: touching 3+ files, or
needing exploration and test runs, or likely to take you more than ~10 minutes. Small edits: do them yourself —
each delegation carries a fixed cost of tens of thousands of tokens on the callee side.

1. Write a self-contained brief. The callee starts cold and cannot see this conversation. Include:
   goal; relevant file paths; constraints (what must not change); acceptance criteria (exact commands to run);
   whether it is read-only. Explicitly forbid committing code; leave changes in the workspace for the host
   to review. Do not include credentials or ask the callee to inspect them.
   The acceptance commands must include the project's **full** test command (e.g. `go test ./...`), not just
   the packages you expect to change: callees break callers in other packages, and a narrow command hides it.
   State any non-obvious invariant you know of (reserved names, special cases) — the callee cannot infer it.
   Do not edit existing tests to fit the new behavior unless the brief says the old behavior is wrong.
2. Start it from the **main thread** (not a subagent — a subagent's background command notifies only that
   subagent). The worker runs in the background:

       pactify sub run kimi --write --wait 0s --json <<'BRIEF'
       ...brief...
       BRIEF

   Always pass `--write` (kimi has no read-only mode in this version; see below). Preserve an explicitly
   requested model with `--model <name>` and effort with `--effort <level>`, quoting shell arguments.
   Use a quoted heredoc delimiter absent from the brief. Never silently choose a different model.
   It exits 3 immediately with `{"jobId": ...}`. This means pending, not failure. Parse the job id from
   stdout. For any other error, report the error and available models if supplied; do not blindly retry.
   Then run, with `run_in_background: true` **and `timeout: 7200000`** (a background command
   otherwise stops at the default ~10-minute limit while the job keeps running):

       pactify sub wait <jobId>

   If the background command is reported as stopped (time limit) rather than completed, the
   job is still running: check `pactify sub status`, then start the same wait again.

3. When the background command completes, read its output: the result summary. The first line carries any
   warning (model mismatch, tool-call limit, idle timeout). Review the diff it lists before building on it.
   If needed, retrieve the summary again with `pactify sub result <jobId>`. Only claim independent model
   verification when the result confirms it. Full logs stay at the reported path.
4. To continue the same kimi session in this repository, pass `--resume` on the next `run`.

For a quick task you are willing to block on (under ~9 minutes), you may instead delegate the complete
brief to the `pactify:kimi` subagent, which calls `pactify sub run kimi --wait 540s --json` once and
returns stdout verbatim. If it returns a pending job id (exit 3), the main thread must still run
`pactify sub wait <jobId>` with `run_in_background: true` and `timeout: 7200000` to collect the result.

**Read-only tasks are not offered for kimi in this version**: `pactify sub run kimi --read-only` exits 2.
In Phase 0 kimi's plan mode never converged on read-only work (3 of 3 hit the tool-call limit with no
findings). Send read-only investigations and reviews to codex (`/codex`, `--read-only`). Only use kimi with
`--write` when the user has allowed changes; do not raise permissions on your own.
