# Harness routing

`pactify sub run` reads `$PACTIFY_HOME/router.json` (default
`~/.pactify/router.json`). Without this file, it uses codex and no quota fallback.
Invalid configuration names the file and field, exits with code 2 and creates no job.

The following uses placeholder model IDs; replace them with installed models:

```json
{
  "defaults": {
    "codex": {"model": "default-model", "effort": "high"}
  },
  "aliases": {
    "primary": {"harness": "opencode", "model": "provider/model-a"},
    "backup": {"harness": "opencode", "model": "provider/model-b"}
  },
  "fallback": {
    "codex": ["@primary", "kimi"],
    "agy": ["@backup"]
  },
  "quotaGroups": {
    "shared-models": {
      "harness": "opencode",
      "models": ["provider/model-a", "provider/model-b"]
    }
  }
}
```

## Aliases and defaults

```sh
printf '%s\n' 'Update the installation guide and check links' | pactify sub run @primary --worktree
printf '%s\n' 'Implement the task' | pactify sub run @backup --model provider/model-c --effort high
pactify sub router
pactify sub router --json
```

Model and effort resolve independently: explicit `--model` / `--effort`, then
alias values, then `defaults[harness]`, then the harness's own default. Explicit
empty values override configuration. Omitted alias fields inherit defaults.
Unknown aliases list available names. Claude Code supports `/delegate @alias <task>` for any locally configured alias; inspect `pactify sub router` first.

For harnesses whose model names already carry the effort tier (agy), an explicit
`--model` or alias model does not inherit `defaults[harness].effort`, so the model
suffix cannot conflict with a default effort. Explicit `--effort` (including an empty
value) and alias effort keep the precedence above; without an explicit model the
default effort still applies, and other harnesses are unchanged.

## Startup selection

1. An explicit harness or alias is preferred; otherwise codex is preferred.
2. Each fallback key is a harness and its value is an ordered list of harnesses
   or aliases. This is its complete chain; other fallback chains are not
   recursively appended. Duplicate routes are removed.
3. Each route checks remembered exhaustion for harness+model, the whole harness
   and its quota groups. Skip routes with any unexpired constraint. CLI model
   and effort overrides apply only to the preferred route; fallback routes use
   their own alias and defaults.
4. Exhausting any model in a quota group exhausts the whole group within that
   harness. Expired entries do not constrain routing. Entries without a known
   reset time expire from local memory after 24 hours.
5. If all routes are exhausted, exit 2 without creating a job and show the
   earliest recovery time. Every constraint on a route must reset before use.
6. Read-only jobs skip unknown harnesses and those that cannot enforce read-only
   access. Codex, kimi, agy and opencode remain eligible. Explicitly
   choosing an unsupported access mode still reports an access error.

Selection warnings say `quota exhausted for X; using Y`, with a reset countdown
when known. `sub router` shows configuration path, defaults, aliases, fallback,
quota groups and exhausted routes. Known resets include a countdown and UTC
instant; unknown resets show local memory expiry. JSON includes expanded
`resolvedFallback` routes. Running jobs retain the remaining routes and groups
from launch; edits affect later jobs.

`--resume` continues the original harness session and inherits its model and
effort. Only explicit flags, including empty values, override these fields;
alias and router defaults do not select resume models. Resume does not silently
change harness because of quota. Use `--worktree-from` to switch sessions.

## Fallback during execution

Only quota failures trigger fallback. Transient failures use same-session
retries; other errors and cancellation do not. Close the previous harness and
record exhaustion before checking later routes and read-only eligibility.

- No changes since the original baseline: start a new job with the same brief,
  access, configuration mode and retry settings. The next route selects its own
  model and effort. Workspace jobs use their existing stash snapshot, or a
  temporary index tree without HEAD; worktrees use their base commit. Failure
  to read a baseline or diff prevents automatic fallback.
- Existing changes: retain the failed result and suggest reviewing the complete
  diff and untracked files, then starting `pactify sub run <route>
  --worktree-from <job-id>` with the full brief on stdin. Workspace jobs without
  isolation require a separately reviewed new workspace task.
- No eligible successor: retain the final failure and quota/access explanation.

Old jobs record `fallbackTo`; new jobs record `fallbackFrom`. `sub wait`,
`sub result` and `sub cancel` follow the chain. The returned `jobId` identifies
the final attempt. Status and logs preserve every attempt. Jobs groups the chain
into one card with selectable attempts; `sub cost` counts linked fallback jobs.

## New session in an existing worktree

```sh
printf '%s\n' 'The existing diff is reviewed. Keep it and complete the acceptance checks.' |
  pactify sub run @backup --worktree-from <job-id> --wait 0s --json
```

The source job must be finished; its worktree must exist, remain undiscarded and
have no active shared session. This option conflicts with `--resume` and implies
worktree isolation. IDs accept the usual unique prefixes.

The new job takes the existing worktree and original baseline, without loading
the previous harness session or home. Old IDs point to the current holder for
start/apply/discard operations. `worktree.owner` remains the shared lock/group
identifier; `worktree.holder` identifies the new holder. Same-session resume
chains retain their behavior until a transfer.

## Dashboard routing editor

Jobs → Routing shows configuration path, defaults, aliases, ordered fallback
chains, quota groups and exhaustion records. It refreshes every 30 seconds while
preserving an active draft's original configuration version. Unknown reset times
remain unknown; local memory expiry is not a provider reset.

Edit adds, removes and reorders routes and group members. Save uses the same
validation as the CLI: 422 errors appear beside fields, and 409 reports
`Config changed elsewhere — reload to see the latest`. Reload discards the draft
and loads the new configuration; Cancel discards edits. Changes affect future jobs.

Clear requires inline confirmation and removes only a local quota record; it
does not reset provider quota. Other exhausted members can still block a group.
The panel is read-only; edit from the dashboard or the router configuration file.

| Operation | Serve | Panel |
| --- | --- | --- |
| Read | `GET /api/sub/router` | `GET /api/router` (Bearer) |
| Save | `PUT /api/sub/router` | Unavailable |
| Clear quota | `DELETE /api/sub/quota/{key}` | Unavailable |

GET returns configuration metadata, `config`, `resolvedFallback` and exhaustion
records. PUT accepts `{config, version}` where version is the SHA-256 of the raw file
(empty input if absent), detecting concurrent edits. Config is strictly validated;
unknown fields and alias cycles return field-specific errors. A valid but unknown
harness/model is retained with a warning. Writes lock the file and use atomic
rename with mode 0600; 64 KiB requests are bounded. Quota keys are escaped as one
path component; Clear locks and removes only the selected entry. The panel never
registers mutation endpoints.
See [Jobs dashboard](jobs-view.md) for API access and security.

## Harness discovery and job actions

`GET /api/sub/harnesses` (panel: `GET /api/harnesses`, requiring Bearer auth)
returns `harnesses` rows with `name`, `installed`, `version`, `verifiedVersion`,
`experimental`, `readOnly`, `sandboxed`, `models`, `default` and `efforts`.
Only agy is experimental; opencode 1.x and 2.x are supported.
`models` includes items, errors, warnings and the latest discovery timestamp.
Router defaults take precedence over advertised native defaults. Effort metadata
can contain the union advertised by models; it does not guarantee that every
model supports every effort. The selected backend validates the final choice.

Discovery sends no inference requests:

- Codex starts a minimal app-server, completes the handshake and pages through
  `model/list`, then closes it without creating a thread or turn.
- Kimi reads only model names and `default_model` from its managed configuration
  link, falling back to the configured user path when absent. It neither loads
  credential/device files nor starts a session; parse errors omit config contents.
- Agy runs its model-list command, with a compatibility fallback for older CLIs.
- Opencode starts an isolated server and reads v1 provider/config metadata or
  v2 model/default metadata, then stops the process group.

Discovery uses a ten-minute cache and `?refresh=1` requests an explicit refresh.
Stale data can remain visible while refresh runs; failures preserve the last good
models, efforts and native defaults. Uninstalled harnesses run no discovery.
Installed versions use the normal version probe; verified versions come from
`verified-versions.json`. Doctor's billable probes are separate; see
[doctor defaults and checks](sub-doctor.md).

Serve also exposes the following POST endpoints; the panel registers none:

| Path | Request and response |
| --- | --- |
| `/api/sub/jobs/{id}/cancel` | Optional `{}`; follows fallback to cancel the current attempt, returns `{ok:true}` |
| `/api/sub/jobs/{id}/resume` | `{brief:"..."}`; starts a background job and returns `{jobId:"..."}` |
| `/api/sub/jobs/{id}/apply` | Optional `{}`; applies safely to the source workspace, returns `{ok:true}` |
| `/api/sub/jobs/{id}/discard` | Optional `{}`; removes the worktree, returns `{ok:true}` |
| `/api/sub/jobs/{id}/dismiss` | Optional `{}`; acknowledges terminal failure/warnings without changing results or worktrees, returns `{ok:true}`; requires unacknowledged attention and no pending worktree |

Host, Origin and configured token checks apply. Bodies must be JSON (charset is
allowed) and at most 64 KiB: non-JSON is 415, oversized bodies 413, malformed JSON
or empty briefs 400, missing jobs 404, invalid states or apply conflicts 409.
Errors are `{error:"English message"}`; conflicts never force file replacement.

Resume shares the CLI's session/worktree locks and worker startup, inheriting the
original harness, access, model, effort, configuration mode and retries. The lock
covers active-turn checks and new-job creation even without a worktree. The brief
is job data; worker arguments contain only the job ID. HTTP returns without
waiting for inference; the server reaps its background workers.

Each store allows at most four dashboard-started queued/running/retrying jobs;
excess starts return 429. CLI jobs do not count. Completion, cancellation and
startup failure release the slot. Multiple server processes share the store lock.

Job summaries always contain an `actions` array. Active jobs can be canceled.
Resume is available only for the latest finished turn with a session and no active
turn in its chain. Apply/discard require an untransferred worktree with no shared
active job. Applied worktrees can still resume or discard when eligible, but
cannot apply twice; discarded worktrees cannot resume. A missing directory still
allows discard to clean up Git registration. HTTP cancellation of terminal jobs
returns 409, while the CLI retains its terminal cleanup behavior.

`sandboxed` is always a boolean: true for codex, false for other harnesses.
Kimi read-only uses a generated private tool allowlist, verifies per-agent tools
snapshots and checks the repository baseline. Opencode read-only removes write/shell
tools and checks the repository baseline. Neither provides an OS sandbox. Model-name heuristics do not infer free usage
from `cost=0`.
