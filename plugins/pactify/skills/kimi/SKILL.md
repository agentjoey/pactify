---
name: kimi
description: Delegate a large coding task (multi-file change, investigation, or anything that needs running tests) to the kimi harness and get back a verified result summary. Not for small edits you can do directly.
---

# Delegate to kimi

Use this only for **large** tasks: touching 3+ files, or needing exploration and test runs, or likely to take
you more than ~10 minutes. Small edits: do them yourself — each delegation carries a fixed cost of tens of
thousands of tokens on the callee side.

1. Write a self-contained brief. The callee starts cold and cannot see this conversation. Include:
   goal; relevant file paths; constraints (what must not change); acceptance criteria (exact commands to run);
   whether it is read-only. Explicitly forbid committing code; leave changes in the workspace for the host
   to review. Do not include credentials or ask the callee to inspect them.
2. Start it from the **main thread** (not a subagent — a subagent's background command notifies only that
   subagent). The worker runs in the background:

       pactify sub run kimi --write --wait 0s --json <<'BRIEF'
       ...brief...
       BRIEF

   Replace `--write` with `--read-only` for read-only tasks; never pass both. Preserve an explicitly
   requested model with `--model <name>` and effort with `--effort <level>`, quoting shell arguments.
   Use a quoted heredoc delimiter absent from the brief. Never silently choose a different model.
   It exits 3 immediately with `{"jobId": ...}`. This means pending, not failure. Parse the job id from
   stdout. For any other error, report the error and available models if supplied; do not blindly retry.
   Then run, with `run_in_background: true`:

       pactify sub wait <jobId>

3. When the background command completes, read its output: the result summary. The first line carries any
   warning (model mismatch, tool-call limit, idle timeout). Review the diff it lists before building on it.
   If needed, retrieve the summary again with `pactify sub result <jobId>`. Only claim independent model
   verification when the result confirms it. Full logs stay at the reported path.
4. To continue the same kimi session in this repository, pass `--resume` on the next `run`.

For a quick task you are willing to block on (under ~9 minutes), you may instead delegate the complete
brief to the `pactify:kimi` subagent, which calls `pactify sub run kimi --wait 540s --json` once and
returns stdout verbatim. If it returns a pending job id (exit 3), the main thread must still run
`pactify sub wait <jobId>` with `run_in_background: true` to collect the result.

只读任务：kimi 用 plan 模式。结果首行若出现「工具调用超限」，说明 kimi 在只读模式下没收敛；
可改用 codex，或在用户明确允许写入后改成可写任务。不要自行提高权限。
