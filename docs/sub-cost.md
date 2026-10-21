# Delegated cost summary contract

```sh
pactify sub cost                         # --since 7d --by harness
pactify sub cost --since 3d --by harness
pactify sub cost --since 30d --by repo
pactify sub cost --since 24h --json
pactify sub cost --since 2026-10-01 --by model
pactify sub cost --by repo --json
pactify sub cost --by day
```

Reads only the same `$PACTIFY_HOME/jobs/*/{state,result}.json` as other `sub` commands; when
`PACTIFY_HOME` is unset it uses `~/.pactify/jobs`. It does not call Recover, create directories or locks, recover or
modify jobs, or read logs, harness homes or credentials. The Jobs page reuses this summary through the shared read-only API;
HTTP extension fields and refresh behavior are described in [Jobs view](jobs-view.md#api-and-boundaries). The CLI JSON contract remains unchanged.

## Time, grouping and fault tolerance

- Filter by State's `createdAt`, over the range `[since, until]`, where `until` is the command start time.
  `--since` accepts `<N>d`, `<N>h` or `YYYY-MM-DD`, and still defaults to `7d`.
- `<N>d` follows status's calendar-day convention: it includes local-timezone today and the preceding N−1 days, starting
  at midnight of the first day. `1d` starts at today's midnight, `3d` at the day before yesterday's midnight, `7d` at six days ago's midnight;
  `30d` works the same way. Daylight saving time still uses calendar days.
- `<N>h` goes back exactly N hours from the command start time, preserving seconds and nanoseconds; `24h` means the past 24 hours,
  which differs from `1d`'s calendar-day meaning. The date format still starts at midnight of that day in the local timezone and includes that day.
- N must be a positive integer containing only decimal digits and may have leading zeros; reject 0, negative numbers, signed or decimal
  values, other units and combined durations (such as `1d2h`). Extremely large values, or values that make the start time exceed the RFC3339
  year range `0000–9999`, report an error without overflow or time wraparound. Dates do not accept future dates.
  `all` is not supported.
- `harness` uses the requested harness from State, falling back to the result's harness when missing; `model` uses the result's
  `modelActual`, then `modelRequested`, then State's requested model.
  `repo` reuses `JobRepo`: a worktree's `originalRoot` takes precedence over State's `repo`,
  and older jobs can resolve the repository root from the request directory if it still exists. `day` groups by local-timezone creation day.
- Groups are sorted by key ascending. Unknown models or repositories keep an empty-string key and display as `(unknown)`;
  metadata is not guessed from the session, directory name or job id.
- Succeeded, failed and canceled counts use the terminal state of valid results; results are published atomically before the terminal State,
  so a running State with a valid terminal result can also be counted. `jobs` is the sum of these three.
- Missing, unreadable or corrupt results are skipped and counted, without failing the command. JSON `null`, no terminal state,
  a mismatched non-empty jobId, or negative tokens or cost are also treated as corrupt. State that is missing, unreadable, corrupt, has a mismatched id
  or lacks a creation time is counted separately. Reads are confined to a single job directory and do not follow links outside it.
- `skipped.missingResult` / `invalidResult` count only jobs with a known State within the time range;
  `invalidState` cannot determine a creation time, so it counts every bad State found during scanning. Non-directory entries (including symlinked
  job directories) are ignored. A nonexistent store returns an empty summary; an unreadable store itself still reports an error.

## Cost and incomplete usage

Only numeric `costUSD` explicitly provided by the result is aggregated; currently it is normally provided by opencode.
Missing or `null` values are not filled with zero; an explicit `0` is a valid reported cost. `costUSD` is JSON `null` when no
US-dollar cost was reported. When codex, kimi or agy does not provide a cost it is counted as a subscription and displayed as `subscription`;
when another harness has no cost it displays `unknown (N)`. Mixed groups and totals display both reported US dollars and
`subscription` / `unknown (N)`, for example `subscription + unknown (2)` or
`$0.125000 + subscription + unknown (2)`.

CLI-generated headers, hints, errors, help and cost labels consistently use English. The JSON group enum `by`
is fixed to `harness` / `model` / `repo` / `day`; subscriptions are represented in JSON by the numeric field
`subscriptionJobs`, with no Chinese enums or display labels. Model, repository and quota text from jobs
is preserved as data and not translated.

Existing Result encoding uses `costUSD,omitempty`, which may omit a true zero value; historical data cannot distinguish
an unreported cost from an omitted zero, so this command classifies such opencode jobs as unknown, does not estimate prices, and does not rewrite results.

`usagePartialJobs` separately counts jobs with `usagePartial: true`, and the table's `PARTIAL` column displays the count;
their recorded tokens and cost still count toward totals, and the summary must not be understood as complete usage.
`retries` counts jobs whose result.retries is non-empty; a single job retried multiple times still counts as 1, and manual `resumeOf` is not counted in retries. `fallbacks` remains reserved for R3 and is currently always 0.

## JSON structure

Top-level fields are fixed as `since`, `until`, `by`, `groups`, `total`, `skipped` and `quotaSnapshots`.
Times are RFC3339. Empty arrays are always `[]`, and all totals and skipped fields are always output.

```json
{
  "since": "2026-10-10T00:00:00+08:00",
  "until": "2026-10-16T12:00:00+08:00",
  "by": "harness",
  "groups": [
    {
      "key": "codex",
      "jobs": 1,
      "succeeded": 1,
      "failed": 0,
      "canceled": 0,
      "retries": 0,
      "fallbacks": 0,
      "inputTokens": 100,
      "outputTokens": 10,
      "costUSD": null,
      "costReportedJobs": 0,
      "subscriptionJobs": 1,
      "unpricedJobs": 0,
      "usagePartialJobs": 0
    }
  ],
  "total": {
    "jobs": 1,
    "succeeded": 1,
    "failed": 0,
    "canceled": 0,
    "retries": 0,
    "fallbacks": 0,
    "inputTokens": 100,
    "outputTokens": 10,
    "costUSD": null,
    "costReportedJobs": 0,
    "subscriptionJobs": 1,
    "unpricedJobs": 0,
    "usagePartialJobs": 0
  },
  "skipped": {"missingResult": 0, "invalidResult": 0, "invalidState": 0},
  "quotaSnapshots": [
    {
      "harness": "codex",
      "jobId": "example-job",
      "observedAt": "2026-10-16T11:00:00+08:00",
      "rateLimit": "7d 30% (reset 2026-10-20T00:00:00Z)"
    }
  ]
}
```

Each `groups` item is a `key` plus the totals fields; `total` is the aggregate of all valid jobs.
`costReportedJobs`, `subscriptionJobs` and `unpricedJobs` are mutually exclusive, and their sum equals `jobs`.
US dollars are summed from the result's original numeric values, displayed with six decimal places in text, without pricing estimates.

Quota snapshots are not filtered by `--since`: each harness takes the latest non-empty
`rateLimit` from all valid historical results, sorted by harness. Observation time prefers State's `finishedAt`, then
`updatedAt`, then `createdAt`; ties use the lexicographically larger job id. Future creation or future observation
times are ignored. A later empty `rateLimit` does not overwrite an older snapshot. The source job and time are retained; snapshots do not represent a live query,
and the CLI summary does not read R1's `quota.json`; the Jobs HTTP API reads it separately to provide `exhaustedRoutes`,
without changing the meaning of the historical snapshots above.

## Relationship to existing queries

`sub status` / `/jobs` / panel summaries and error handling remain unchanged; in particular, status text usage
aggregation still errors on missing or corrupt terminal results, while this command independently uses a skip-and-count strategy.
Result `float64` / `omitempty` encoding remains unchanged, and worker, result and log recovery callers are unaffected.
