# Operations

## Diagnostics

```sh
pactify sub status --all
pactify sub logs <job-id> --tail 20
pactify sub result <job-id>
pactify sub doctor <harness>
```

Doctor checks harness compatibility and runs real probes that may consume model
quota. See [doctor](sub-doctor.md) and [upstream compatibility](harness-updates.md).

## Storage lifecycle

All projects share `$PACTIFY_HOME/jobs/`. The default home is `~/.pactify`.
Retention applies uniformly across projects:

- Keep a finished job for 14 days after its recorded finish time. Delete its
  entire directory only when every member of its resume/fallback/worktree chain
  is eligible. Running, queued, unknown, recent or unverifiable members protect
  the whole chain. Results without verifiable session ownership are retained.
- Pending worktrees are never automatically deleted. After 14 days they appear
  as stale in Settings and doctor. Apply/discard them individually, or explicitly
  use manual `--include-worktrees`, which invokes the existing discard operation
  before removing an eligible job. Applied/discarded jobs follow normal retention.
- Above 5 GB (5,000,000,000 bytes), evict cache entries by last access/modification
  time until usage is at most 80% of the cap. Extracted Go modules are evicted as
  units; read-only directories are made writable without chmodding linked files.
- Remove orphan sessions and expired logs only inside Pactify's private harness
  homes. Keep credentials, symlinks, unrecognized layouts and shared configuration.
  OpenCode 1.x and 2.x use separate homes, including `harness-homes/opencode2`.
  Its database sweep uses the native session API; reclaimable database bytes
  cannot be estimated without running that API and are excluded from estimates.
  Cleanup requires the matching installed OpenCode major; a mismatched home is
  retained with a failure so the other major cannot migrate its database.

Before removing a job, prune deletes the sessions from every eligible turn:
Codex uses the existing app-server client and `thread/delete`, including archived
threads, without unarchiving. Already deleted threads are accepted. Without
Codex, or when its API fails, the job stays for retry. **Deleting these threads
removes them from Codex Remote Control; ordinary completion only archives them
and Remote Control can still show archived threads.** Kimi uses its local web
delete API with a verified private-file fallback. OpenCode uses its native
private server API. Agy removes only recognized private conversation files;
an unsupported layout is retained with a failure. Non-Codex sessions created
with `--with-user-config` are retained because prune never cleans real user homes.

No cleanup traverses a credential link or an external symlink. Incomplete scans,
unreadable state, unsafe paths and ownership errors keep affected data and are
reported. Prune takes the lifecycle state locks, the creation/resume gate and
private-home leases. Manual cleanup during active jobs protects shared caches
and homes. Automatic cleanup waits until jobs are idle, so starting a job cannot
consume the daily interval while shared storage is protected.

```sh
pactify sub prune                              # plan only, including kept items/reasons
pactify sub prune --json                       # machine-readable plan
pactify sub prune --yes                        # execute the default policy
pactify sub prune --older-than 720h --cache-max 5GiB --yes
pactify sub prune --include-worktrees --yes     # also discard eligible stale worktrees
```

`--older-than` accepts positive Go durations (14 days is `336h`). `--cache-max`
accepts positive bytes or KB/MB/GB and KiB/MiB/GiB, including decimal values.
Overrides affect that invocation only. `--include-worktrees` is manual only;
the dashboard action and automatic runs never enable it. Exit codes: **0** means
the plan/execution completed without reported failures, **1** means an operational
or partial-cleanup failure (inspect JSON `failures`), and **2** means invalid
arguments. Intentionally protected items do not by themselves cause failure.

Configure defaults in `$PACTIFY_HOME/storage.json`, beside `router.json`:

```json
{
  "olderThan": "336h",
  "cacheMaxBytes": 5000000000,
  "interval": "24h"
}
```

Missing fields use these defaults; invalid policies stop cleanup. `sub run`
starts a detached, silent maintenance child after its worker starts. The child
waits up to two minutes for idle jobs. The resident `pactify serve` checks once per
minute. The configured interval is checked under a cross-process prune lock,
so automatic runs occur at most once per interval. Manual runs also reset that
interval. Maintenance does not change the job's output or exit code.

`$PACTIFY_HOME/storage-status.json` records start/end times, per-location bytes
freed and item counts, and failures. An absent end time means the run is still
running or was interrupted. Settings and doctor report this history and stale
worktrees. Polled size estimates use metadata only, bounded to 200,000 entries
and five seconds per scan; status polling caches them for one minute and omits
item lists. Explicit plans and cleanup do not share the polling budget and honor
command/request cancellation. A polling limit therefore cannot prevent cleanup
of a larger store; unsafe or unverifiable individual items are still retained.
Symlink targets and credential contents are excluded from size estimates.

Read endpoints are `GET /api/sub/storage` and `GET /api/sub/storage/plan` in
serve, and `/api/storage` and `/api/storage/plan` in the read-only panel. GETs
never run harnesses or write storage. The dashboard previews a fresh plan, then
sends its version to `POST /api/sub/storage/prune`. A changed plan returns 409
and requires review again. The POST uses the existing bearer-token/exact-Origin
guard. The panel remains GET-only and shows a copyable
`pactify sub prune --yes` command.

Pruning permanently removes deleted jobs from cost history. The default plan groups
items by reason with up to five examples; `pactify sub prune --verbose` lists every
item. JSON output retains the full plan. `storage.json` requires retention of at
least 24 hours and a cache cap of at least 1 GB.

Job-start maintenance checks the recorded interval before spawning a child. A
nonblocking scheduler lock allows one poller, running from the resolved Pactify
home for at most two minutes. A busy store is retried by a later job start or
the resident server. Job creation waits at most two seconds for the snapshot
gate; cleanup revalidates each chain and uses its existing resume-scope lock.
Directories move atomically into `trash/` before file removal; marked partial
removals are retried on the next cleanup. Damaged job metadata protects its
chain and shared homes/cache, while unrelated verified chains remain eligible.

Opencode cleanup strips inherited database/config/data overrides for private-home
jobs and probes, while `--with-user-config` retains native environment selection.
Cleanup pins the relative database name. It confirms v1's reported state/config
paths against private XDG roots; v2 can also confirm its reported data path.
Unrecognized responses fail closed and include reported/expected paths.
Native session deletion is counted separately from a labelled database size delta;
checkpointing without session deletion does not count as removed data. The
database delta is the size before minus the size after: positive values indicate
a reduction and negative values indicate growth.
Local native session preflight completes before deletion, including a Codex
executable check. Agy retry receipts live in `storage-receipts/agy.json`, outside
mirrored homes; existing receipts migrate on use.

Unreadable job state protects every chain whose IDs can be recovered from its raw
metadata. Without recoverable references, all job deletions are protected. Shared
storage remains protected when a running job cannot be ruled out. Empty or
lock-only crash directories stop blocking shared cleanup after a one-hour grace;
they remain visible and age out under retention. Orphan trash markers also expire
after one hour. Shared-storage passes recheck state every 64 items or one second,
and detect new job directories between those checks. Pruned jobs are permanently
removed from cost history.

## Upgrade and uninstall

Upgrade the CLI the way it was installed. The installer replaces the binary in
`PACTIFY_BINDIR` (default: the first writable of `/usr/local/bin` and
`~/.local/bin`); `go install` writes to `$(go env GOBIN)`, or `$(go env GOPATH)/bin`
when `GOBIN` is unset:

```sh
curl -fsSL https://pactify.dev/install.sh | sh
go install github.com/agentjoey/pactify/cmd/pactify@latest
pactify version
```

Job state in `$PACTIFY_HOME` carries over. Update the host integrations as well:

- Claude Code: `claude plugin marketplace update pactify`, then
  `claude plugin update pactify@pactify`, then `/reload-plugins` or a new session.
- Codex: `codex plugin marketplace upgrade pactify`.
- Copilot and Cursor: update the plugin through the host's plugin manager.
- Copied `.agents/skills` folders: copy them again from the new release.

To uninstall:

1. Run `pactify sub status --all`. Apply or discard every job that still has a
   pending worktree, and cancel running jobs with `pactify sub cancel <job-id>`.
   Deleting the home discards unapplied worktree changes.
2. Stop any `pactify serve` or `pactify sub panel` process you started.
3. Check `$PACTIFY_HOME/credential-quarantine` (default
   `~/.pactify/credential-quarantine`). When a harness refreshed a login file that
   pactify could not write back, the private copy is kept there; move anything you
   still need before deleting it.
4. Delete `$PACTIFY_HOME` (default `~/.pactify`). Harness logins are linked into
   `harness-homes/` as directories or single files; `rm -rf` removes the links, not
   your own login files. An opencode 2.x login made with `pactify sub login opencode`
   lives only in `$PACTIFY_HOME` and is removed with it.
5. Delete the binary: `command -v pactify` shows its location.
6. Remove host integrations:
   - Claude Code: `claude plugin uninstall pactify@pactify`, then
     `claude plugin marketplace remove pactify`.
   - Codex: `codex plugin remove pactify@pactify`, then
     `codex plugin marketplace remove pactify`. Also delete the
     `prefix_rule(pattern=["pactify", "sub"], ...)` line if you added it to
     `~/.codex/rules/default.rules`.
   - Copilot and Cursor: remove the plugin through the host's plugin manager.
   - Delete copied `.agents/skills/delegate*` and `.agents/skills/jobs` folders.

Pactify never edits your own harness configuration or credentials, so no other
cleanup is required.


## Local server

Run `pactify serve` (default `127.0.0.1:7777`), or specify `--addr`. SIGINT shuts
it down. Read jobs with `curl http://127.0.0.1:7777/api/sub/jobs`.

Every request, including static assets and SSE, validates Host. Loopback binds
accept `127.0.0.1`, `localhost` and `[::1]` with the listening port. Browser writes
require an exact `http` loopback Origin on that port or an entry in
`PACTIFY_ALLOWED_ORIGINS`. CLI requests without Origin remain allowed.

`PACTIFY_ALLOWED_ORIGINS` is a comma-separated list of full origins for use
behind an authenticating reverse proxy. Their hosts are accepted with
or without explicitly configured ports. Host and Origin checks do not authenticate users.

Non-loopback binds, including `0.0.0.0`, `::` and an omitted host, require
`PACTIFY_SERVE_TOKEN` of at least 32 characters. Host must match the bound
IP/hostname and port or an allowed proxy host. Setting a token on any bind makes
all `/api/` requests, including reads and SSE, require `Authorization: Bearer
<token>`. Static resources remain public behind Host validation. The dashboard
requires an authenticating proxy that injects this header; it does not send the
token itself. Workers inherit neither the token nor allowed origins.

Responses include `X-Content-Type-Options: nosniff`, `Referrer-Policy: no-referrer`
and `Content-Security-Policy: frame-ancestors 'none'`. Header timeout is 10
seconds and idle timeout is 60 seconds; SSE has no overall write deadline.
See [Jobs dashboard](jobs-view.md) for deployment details.

## macOS background services

Locally built binaries installed as macOS background services need ad-hoc signing:
`codesign --force -s - ./pactify`. This applies to local service installations,
not ordinary foreground use. Release installation and local builds are separate.

See [installation](deployment.md) and [CHANGELOG](CHANGELOG.md).
