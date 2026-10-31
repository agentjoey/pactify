# Delegated task summary contract

Cost aggregation uses the separate read-only command `pactify sub cost`; see [sub-cost.md](sub-cost.md) for the JSON and fault-tolerance conventions.

`internal/sub.JobSummary` is the shared data layer for `/jobs` and the local panel.
`Store.Summarize(state, peers, now)` receives State read atomically through `Store.Load` / `Store.Recover`, reads the job's optional result and log tail, and expects `peers` to contain the State of every job so it can produce complete reverse resume relationships. The method does not modify the job.

```sh
pactify sub status --json                    # latest 10
pactify sub status --all --json              # all
pactify sub status --repo /path/to/repo --json
pactify sub status <id> --json                # a single object
```

The list is sorted by `createdAt` descending, with ties broken by id descending; an empty list is `[]`. `--repo` filters before truncating to 10 entries, matches by original repository root, and accepts repository subdirectories, relative paths and symbolic links. The human-readable usage summary also counts only that repository, but still includes jobs beyond the display limit. A historical repository that no longer exists is matched by absolute path. A single job that does not match `--repo` exits 2.

## JSON compatibility

The `issue` field and issue-number extraction are removed. The title uses only the first non-empty line of the brief (no longer skipping section labels such as `Background:`), retaining Markdown cleanup and the 80-character limit; when using an issue tracker, you can write the number into the title yourself.

The list remains an array of objects, retaining `jobId`, `status`, `harness`, `cwd`, `createdAt` and optional `finishedAt`, and adding the summary fields below; `id` is the same as `jobId`.
A single job remains an object, retaining the existing fields of the complete State (including `request`, `updatedAt`, `error`, process information and worktree metadata) and `duration`, and adding the summary fields; it has no `jobId` alias. A single object also retains the historical zero-value `finishedAt` to mean unfinished; the list omits `finishedAt` for unfinished jobs.

**Compatibility exception for directory fields**: Go's `JobSummary.Cwd` is the actual execution directory; the CLI's `cwd` keeps its historical meaning of `request.cwd` (the original request directory), and the actual execution directory is obtained consistently through the new **`runCwd`**. Consumers should use `repo` to locate the original repository and `runCwd` to locate the execution directory. This adds information without changing the meaning of the existing `cwd`.

## Fields

| Field | Type | Meaning |
| --- | --- | --- |
| `id` | string | job id |
| `title` | string | first non-empty line of the brief, with Markdown heading markers, `*` / `_` emphasis markers and backticks removed and collapsed to one line; at most 80 Unicode characters (including the truncation ellipsis) |
| `harness` | string | requested harness |
| `effort` | string, optional | requested effort recorded in State; not inferred from the router's current configuration when unknown |
| `modelRequested` / `modelActual` | string | requested model / actual model recorded by the result; empty when unknown |
| `access` | string | requested access mode |
| `configMode` | string | configuration mode recorded in State; empty for older jobs that did not record it |
| `repo` | string | original repository root; for worktree jobs this is `originalRoot`; ordinary jobs record it at creation, and older jobs can resolve it from the request directory if that still exists |
| `cwd` / `runCwd` | string | in the CLI, the original request directory / actual run directory respectively; for ordinary jobs both are the same |
| `worktree` | object, optional | retains the existing worktree fields and adds `status`: `pending` / `applied` / `discarded`; omitted for non-worktree jobs |
| `failureClass` | string, optional | failure class: `transient` / `quota` / `auth` / `model` / `idle_timeout` / `canceled` / `refusal` / `max_tokens` / `max_turn_requests` / `readonly_violation` / `other`; required for new failed and canceled jobs, not written for successful or unfinished jobs; may be absent for older jobs |
| `quotaResetAt` | RFC3339 string, optional | known quota reset time; omitted when it cannot be parsed, and not written for other classes |
| `retryAttempt` / `retryLimit` | number, optional | for a running job, the retry sequence number / limit currently waiting or executing; when present, show `retrying (n/N)`; the underlying status remains running |
| `retryCount` | number, optional | number of automatic retries already started; terminal jobs show the retry count, omitted when 0; manual resumes are not counted |
| `status` | string | `queued` / `running` / `succeeded` / `failed` / `canceled` |
| `createdAt` | RFC3339 string | creation time |
| `startedAt` | RFC3339 string, optional | time the worker claimed the job and it entered running; not guessed from `updatedAt` for older jobs |
| `finishedAt` | RFC3339 string, optional | finish time (see the historical zero-value exception for a single object above) |
| `dismissedAt` | RFC3339 string, optional | time the CLI or dashboard first acknowledged the job as handled; omitted when not dismissed |
| `duration` | string | elapsed time from creation to finish or query time, including queue time, rounded to seconds; keeps the old semantics, such as `3s`, `1m2s` |
| `attention` | array | list of `{kind, message}`; `[]` when nothing needs attention; only terminal jobs produce entries |
| `activity` | object, optional | present only for running jobs, described below |
| `resumeOf` | string | id of the direct source job selected when created with `--resume`; empty when not resumed or not recorded |
| `resumedBy` | string array | ids of direct successors whose `resumeOf` points to this job, sorted by id; forks are supported, and `[]` when empty |
| `inputTokens` / `outputTokens` | number | final token counts from the result; 0 when there is no result or they were not recorded |
| `costUSD` | number, optional | known positive US-dollar price in the result; old envelopes default to 0 and cannot distinguish unknown from free, so this is omitted |
| `usagePartial` | bool, optional | carries over the result's marker; when true, only recorded usage is available and the final segment may be missing, so it must not be treated as an exact total for the whole job |
| `changedFiles` | number | number of changed files in the result |
| `addedLines` / `deletedLines` | number | added and deleted line counts in the result |
| `warningCount` | number | number of warnings in the result |
| `sessionId` | string | harness session id from the result; empty when unknown |

`attention.kind` is one of the following string enums, and `message` is a one-sentence human-readable explanation; consumers should decide based on kind:

| kind | Condition |
| --- | --- |
| `worktree_pending` | the job has finished and the worktree has not recorded apply or discard |
| `failed` | the job status is failed and it has not yet been acknowledged as handled |
| `warnings` | the result contains at least one warning that needs handling and it has not yet been acknowledged as handled |

A terminal job counts as acknowledged when its `dismissedAt` is non-zero, or when the worktree status is `applied` / `discarded`,
and no longer produces `failed` / `warnings` attention. The `worktree_pending` rule is unchanged.
The summary's `actions` array includes `dismiss` only for terminal jobs with unacknowledged failure or actionable warnings, and for any undismissed job with a pending worktree. Applied or discarded worktrees already acknowledge those issues, so they do not offer Dismiss. Serve accepts `POST /api/sub/jobs/{id}/dismiss` with an optional empty JSON object; it uses the same acknowledgement operation as `pactify sub dismiss <id>`. The result and worktree remain unchanged; the read-only panel registers no write endpoint.
Apply and discard mark all existing turns that share the worktree, so failures and warnings from those turns also clear attention;
new resume turns are still handled according to their own pending status. Fields such as `status`, `failureClass` and `warningCount` keep their original values.

After review is complete and no further action is needed, run `pactify sub dismiss <id>` to clear failure and warning attention,
including for read-only jobs without a worktree. The command accepts only one complete job id, and requires the job to be finished. A pending worktree stays pending and keeps its
`worktree_pending` attention. Repeated execution succeeds and keeps the first time. It only updates State under the lifecycle lock and does not delete or rewrite the result, logs or resume relationships;
like apply/discard, it does not provide `--json`.

Informational warnings do not produce `warnings` attention: harness informational warnings (agy experimental or opencode permission boundary), `dirtyWorktreeWarning`,
messages ending with `has not been checked; run pactify sub doctor first`, and
`Usage may be incomplete; only recorded usage is included` (`partialUsageWarning`) and messages beginning with
`CLI version changed: `. They still remain in the result, `pactify sub result` output and `warningCount`.
Other warnings still need handling, including empty-reply warnings.

A worktree's `applied` is a persisted record of a successful apply (a successful apply of an empty patch also counts). A successful apply marks existing jobs that share the worktree; new resume jobs reset the marker to pending. After discard, the status displays discarded with priority. Queries do not guess whether apply happened based on current repository contents, nor do they treat a disappeared directory as discard. Applies not recorded before a version upgrade cannot be reconstructed and display pending. Only metadata is recorded here; the existing apply conflict detection, patch operations and discard behavior remain unchanged.

The resume source is recorded in the new job's State, while `request.resume` continues to store the original session id; the two do not replace each other. For historical jobs, even if the session id is the same, `resumeOf` is not inferred backward, to avoid mistaking a fork in the same conversation for a direct predecessor relationship.

## Running activity

| Field | Type | Meaning |
| --- | --- | --- |
| `summary` | string | readable summary of the most recent complete tool event or message, reusing `logs_readable` message merging and tool formatting; at most 240 characters plus an ellipsis, only one line; empty when there are no events yet |
| `inputTokens` / `outputTokens` | number, optional | most recent usage within the tail window; Codex prefers each thread's running total and then sums the threads visible in the window; omitted when there is no usage, and 0 is retained when 0 was reported |
| `usagePartial` | bool, optional | true when the log exceeds the tail window and usage exists, meaning usage from threads outside the window may be missing; do not treat it as an exact total for the whole job |

Each read reads at most the last **256 KiB** of `log.jsonl`, using the file size after opening as a snapshot. The first record truncated by the window, the last record without a newline commit, and corrupt records are ignored; the entire large log is never scanned. Usage may not be in the window, in which case token counts are not fabricated. Final usage is based on the top-level token fields of the result, combined with the top-level `usagePartial` to judge whether it may be incomplete; Kimi's independent usage verification usually enters the result only after the job ends.

When a job fails, is canceled, hits an idle timeout or the connection closes early, the usage already received is retained, `usagePartial: true` is conservatively set, and the result warns `Usage may be incomplete; only recorded usage is included`. A failed independent usage verification also sets the same marker without changing the success status of the task. A job canceled before it started is not marked partial for this reason. When the worker itself disappears, the fallback result recovers usage from complete events in the log; opencode serve usage events carry `accounting: job_delta`, which can recover this turn's cost; historical ACP logs lack this marker, so their session-cumulative cost is still not treated as this job's cost.

opencode uses the v1 `GET /session/{id}` tokens/cost session summary and subtracts the baseline taken at this Start: input is input + cache.read + cache.write, output is output + reasoning. Automatic retries share the startup baseline, while manual resumes take a new baseline after a server restart so history is not counted twice. Individual usage returned by `/message` does not participate in accumulation, and `opencode export` is no longer called. When a complete summary cannot be obtained, `usagePartial` is set.

State keeps the atomic rename convention, and status keeps using the existing Recover and lifecycle lock handling for lost workers. When the JSON summary encounters a missing or corrupt result it still displays the job, with result fields left empty or 0; the full usage summary in human-readable output continues to error on corrupt/missing terminal results instead of silently giving an incomplete summary. Older jobs missing the new metadata are output normally.

List queries skip job directories that have not yet written `state.json` (including
`sub status --all --json` used by `/jobs`); directly querying such a job still reports the missing state file.

The panel log stream reads only the last 1 MiB on its first connection, ignores the first record truncated by the window, and displays at most 100 lines;
afterward it reads incrementally from the current offset. In follow mode, a single record retains at most 64 KiB, and overly long lines are truncated;
unfinished lines are not copied when there are no new bytes. Non-follow reads in the CLI and full history reads with explicit `--tail -1` are preserved.

Every 100 ms the worker saves the PIDs and kernel start times of descendant processes to `state.json`,
including independently observed process groups, and continues tracking saved descendants that are still alive. On recovery, the live snapshot is merged,
identities are verified one by one before cleanup, and the job TMPDIR is removed only after registered processes are confirmed to have exited.
TMPDIR is retained when checking, persisting or terminating fails. Periodic snapshots still have sampling gaps and cannot guarantee recording a process that was created between two samples and immediately left the entire known process tree.

## Failure classification and quota memory (R1)

`result.json` and the summary share `failureClass` and `quotaResetAt`. Classification is new metadata,
and the original `error` text in State and the existing exit codes remain unchanged. Each harness's matching table is placed next to its cockpit adapter;
structured evidence such as Codex `rateLimits.primary/secondary.usedPercent >= 100`, `codexErrorInfo`,
ACP `AUTH_REQUIRED (-32000)` and opencode `APIError.data` is preferred, with error text matched last.
opencode `isRetryable=true` or 5xx is transient; a 429 with quota wording such as balance/quota/credit/budget is quota, and other 429s are transient.
SSE session.error configuration/model errors are other, with the readable error preserved. ACP only completes on end_turn; cancelled/refusal/max_tokens/max_turn_requests all fail and retain their classes.
`-32603` is only a generic Internal Error and cannot be classified as transient on its own; for example, insufficient balance is still quota.
User cancellation takes precedence over backend errors; idle timeout is idle_timeout. Read-only denials, git environment errors,
killed processes and orphan jobs without a cancellation record remain other. Error keywords are not matched against successful replies or the task body.

Authentication evidence: [Kimi ACP documentation](https://github.com/MoonshotAI/kimi-cli/blob/main/docs/en/reference/kimi-acp.md)
confirms that not being logged in returns -32000; Codex's unauthorized comes from a fixed protocol schema in the repository;
agy's expired-login prompt comes from the [official CHANGELOG 1.3.3](https://github.com/google-antigravity/antigravity-cli/blob/main/CHANGELOG.md),
and also covers this repository's managed-home error that explicitly requires `run agy login first`. Unknown prompts are classified as other.

Quota records live in `$PACTIFY_HOME/quota.json`, with JSON object keys of `harness/model`,
preferring the actual model and then the requested model; when both models are unknown the key is just `harness`. Values contain
`harness`, optional `model`, `resetAt` (null when unknown), `jobId`, `recordedAt` and `message`
(a single-line summary of the original error, at most 512 Unicode characters). File permissions are 0600, writes hold a separate
`quota.json.lock` file lock, and the file is atomically replaced with a temporary file in the same directory followed by rename; older records on the same route do not overwrite newer records.

**Expiry rules**: ignored when `now >= resetAt`; entries without resetAt expire **24 hours** after being recorded.
This is the local cache's validity period, not an upstream-confirmed quota reset. Reads do not modify disk, and later writes clear expired records.
When multiple Codex windows are exhausted, the latest reset time is used; when an exhausted window has no reported time, it is conservatively recorded as unknown.
Reading a corrupt quota file reports an error; a failed write keeps the original file and appends a warning to the job result,
still publishing the original failure and classification without rewriting State.error.

R3 uses `Store.ExhaustedRoutes(now)` to read all valid routes (sorted by key, with `[]` for an empty list).
The corresponding CLI read-only interface is `pactify sub quota --json`, which returns an array of QuotaRoute without changing the existing JSON shape of status.
`sub status` human-readable output shows a quota line at the top when exhausted routes exist (not limited to the current repository or the 10 displayed jobs),
and keeps the original output when there are no exhausted routes; `sub quota` human-readable output can explicitly query the empty state.
`sub status` / `sub result` display messages such as `failed (quota, resets in 6d 23h)`;
the `/jobs` skill shows currently exhausted routes through a separate quota query, and Jobs cards show class labels.
Historical results without a class are neither written back nor guessed, and remain compatible for display.


## Automatic retry of transient errors (R2)

`sub run --retries N` accepts 0–5 and defaults to 2; 0 disables automatic retries. The Request stores the
`retries` value, and older requests without it use the default. A retry happens only when a turn fails and is classified as transient by R1,
the job has not been canceled and the session is still usable. quota, auth, model, idle_timeout, canceled and other
are not retried; error keywords in a successful reply also do not trigger a retry. The first wait is 30 seconds, and the second and subsequent waits are 120 seconds.
Context cancellation ends the wait immediately; when no signal is received, the persisted cancellation marker is checked every 100 ms.
Backoff does not trigger the idle timeout, and the next turn recalculates idle time.

A retry sends a fixed prompt:

> The previous turn was interrupted by a transient backend error. Continue the task from where you left off; do not redo completed steps.

The same job, session object, harness, model and effort continue to be used; Start/Resume is not called to create
another session, and the session is not closed or hidden early. Each adapter's path has been confirmed from the implementation:

| Harness | Next turn in the same session |
| --- | --- |
| codex | `turn/start` again on the original threadId; only the final Close archives it |
| kimi | `session/prompt` again on the original ACP sessionId |
| agy | the next process receives `--conversation <id>`; when no model was explicitly selected, the model reported in the first turn is locked |
| opencode | `prompt_async` again on the original v1 session id; manual resumes preserve the managed database |

All four have resume interfaces. Classification still follows R1 exactly; currently Kimi's matching table has no transient
evidence, so unrecognized backend errors remain other, and retry scope is not expanded just because same-session prompts are supported.
For adapters that do not support retries, or when there is no session id, `transient retry unsupported for <harness>` is recorded in result.warnings,
the original failure is retained, and no new session is opened.

During backoff and retry execution the status is still `status: running`; `retryAttempt` / `retryLimit` indicate which retry attempt
is in progress, for the CLI, `/jobs` and the Jobs page to display `retrying (n/N)`. `retryCount` counts only retries already started,
and is not incremented when cancellation happens during backoff. Result adds:

```json
{"retries":[{"class":"transient","error":"UNAVAILABLE (code 503)","at":"2026-10-16T04:00:00Z"}]}
```

Each entry corresponds to one retry; error is the previous turn's error that triggered that retry, and at is the start time.
State also stores the history for the fallback result after the worker disappears; the terminal summary's retryCount is based on the result.
The final text comes only from the last turn; if the last turn failed, its classification applies (exhausted retries are still transient), and success clears
failureClass. User cancellation still takes precedence. Earlier transient errors move into retries and do not mask the last turn's error.

Multi-turn tokens / cost accumulate under the existing conventions: Codex thread running totals and agy / opencode
adapter cumulative values are not added twice; Kimi prompt usage accumulates across turns. Kimi wire and opencode session summaries
are still verified only at the final end, with the baseline covering all prompts of the current job, including failed turns. An abnormal end may
miss the final segment's usage, so usagePartial is conservatively retained even after a successful retry. `sub cost` RETRIES is the number of
jobs that had retries, counting a single job only once even if it retried multiple times; manual `--resume` creates a new job and does not count as an automatic retry.

## Empty-reply follow-up

For every harness (codex, kimi, agy, opencode), when a turn ends normally and the final text is still empty after `strings.TrimSpace`,
the worker asks exactly once more in the same live session:

> You ended without a written answer. Reply now with your result: what you found or changed and which checks you ran. Do not start new work.

The insertion point is after turn failure classification and before the transient retry decision. The follow-up is sent only when the turn is done, there is no failure class, there is no prompt/turn
error, the job was not canceled/interrupted/timed out or grace expired, and the session has not closed. Before the follow-up, queued tail events of the turn are drained to identify a closed stream, and the persisted cancellation request is checked again.
The follow-up is independent of `--retries`, does not consume a retry, and `--retries 0` still applies; if a transient retry after the follow-up produces another empty reply,
there is no second follow-up. A failed follow-up turn uses normal failure handling. Read-only policy, cumulative tool-call limit and idle/total timeout continue to apply.

`result.json` / `pactify sub result --json` adds an optional numeric `emptyReplyFollowups`:
omitted when there was no follow-up, and 1 after a follow-up; it is not added to the status summary's `retryCount`. The log records a system event
`state: "empty_reply_followup"` with `text` as the harness name. Output within the turn is cleared in the same way as transient retries,
usage and tool calls remain accumulated, and Live logs use the same usage convention.

When the follow-up receives text, the job succeeds normally and only the count is recorded, without a warning. If the follow-up ends normally but is still empty, the job is still `succeeded`,
and the following text is appended to `Result.Warnings` (`<id>` replaced with the actual job id):

> empty final reply after one follow-up: the harness ended without a text answer. Check pactify sub logs <id>, or resume to ask for the result.

If the session has already closed after the empty-reply turn and a follow-up cannot be sent, the job is likewise judged `succeeded` and the following is appended:

> empty final reply: the session ended before a follow-up could be sent. Check pactify sub logs <id>, or resume to ask for the result.

Both warnings need handling and are not informational filter items; `sub status` `attention.kind` includes `warnings`.
A read-only task is not yet delivered at this point: first check `pactify sub logs <id>`, then use `--resume <id>` with a short brief to request the result;
if it is still empty, rerun with another harness or model. For write tasks, review the complete diff and rerun acceptance as usual, obtain the handoff via `--resume <id>`,
or write it yourself based on the diff and your own checks. Resuming preserves the original harness, cwd, access and worktree.

A normal empty reply from agy no longer produces an `empty_answer` failure; a `print_timeout` with no content remains `timeout`.
agy's separate `CompletionFollowups` (2 by default, waiting for running commands and reporting actual results) keeps its original behavior
and is counted separately from the worker's single empty-reply follow-up.

## Sandbox fields in the result envelope

`result.json` and `pactify sub result --json` always include the boolean field `sandboxed`: true for codex and false for other harnesses.
It declares OS sandbox capability and is independent of the requested access. opencode read-only relies on plan, permission denials and a final diff check;
side effects outside the repository have no OS-level restriction. ACP's cumulative final text is published with the terminal event's `text` / `final`, and event backpressure does not lose text or tool counts.


## Live event stream

The Dashboard's `GET /api/sub/jobs/{id}/events` and the local read-only panel's
`GET /api/jobs/{id}/events` reuse the read, follow and cancel logic of `Store.Logs`.
The original `/logs` still returns readable strings, while `/events` SSE `data` returns structured JSON objects;
termination and read failures still use `done` / `log-error`. Both entry points are read-only and keep their respective authentication boundaries.

The initial read is limited to the last 1 MiB and at most 100 recognized events; while following, a single physical record is limited to 64 KiB.
Corrupt, window-truncated and uncommitted follow lines and unknown events are not rendered.
Structured output does not pass `raw` and projects only known tool identity/type/phase, cumulative usage, diff counts and quota summary;
it preserves adapter redaction before log writing and additionally masks common credential assignments in displayed text.
Codex uses each thread's running total, and Kimi automatic retries use the Collector's per-turn accumulation convention;
only opencode known prices with `accounting: job_delta` enter event `costUSD`.
When there is no price, the default zero value of Usage is not treated as free.

Live opens an event stream only while the Dashboard or the detail Live tab is mounted, with at most 4 globally;
other jobs show the numbers from the existing two-second summary polling while waiting for an idle connection.
Streams are canceled when the page is hidden, the page is switched or the detail view is closed; when the page is shown again, the bounded tail is reread and observation counts are reset.
Tool calls, tool nodes and session logs represent only the current connection's observation window; final tokens/diffs are based on the summary.
When there are no running jobs, the tail of recent runs is shown statically, without simulating timing or probability or inferring internal subagents.
Animations are triggered by events and disabled under `prefers-reduced-motion`.

Acceptance uses `web/e2e/live.spec.ts`; embedded-build screenshots can be produced with
`cd web && node scripts/shots-live.mjs`, which writes to `artifacts/live/` (repository root) by default
and covers Dashboard, details, panel and phone widths. No substitute screenshots are generated when the browser cannot start.
