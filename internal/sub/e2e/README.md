# Live harness CLI integration tests (AJ-718)

These tests launch real models and may consume subscription quota. They are
excluded from ordinary builds/tests. Pure evidence and metadata helpers live in untagged `_test.go` files and have
ordinary tests that use synthetic records and fake executables only. Run from a disposable checkout. Nothing is
committed, merged or pushed. All job lifecycle operations use a pactify binary
built from the current source, through subprocess CLI commands. Assertions read
CLI JSON, job-owned log records, files and process identities; they do not call
adapter or worker inference functions. Opencode v2 uses the product metadata-only model discovery and own-session permission evaluation helpers.

```sh
export TMPDIR=/private/tmp # or another writable temporary directory
PACTIFY_HOME="$(mktemp -d)" \
PACTIFY_E2E_HARNESS=codex \
PACTIFY_E2E_HOST=1 \
PACTIFY_E2E_REPORT="$TMPDIR/aj718-codex.json" \
go test -tags harness_e2e ./internal/sub/e2e -run '^TestHarnessE2E$' \
  -count=1 -v -timeout 90m
```

Run harnesses **serially**, in order: codex, opencode, agy, kimi. Scenarios also
run serially, except the three worktree jobs inside `parallel`. Each scenario
gets a fresh git repository and `PACTIFY_HOME`. Resume rounds share their
scenario's private session store. `records` recreates scenario 1's write-worktree job
rather than depending on another scenario, so filtered runs remain independent.
The CLI wait bounds each normal job to five minutes, cancellation startup to
90 seconds, `long_tool` to seven minutes, and the Claude host to ten minutes.
Cancellation requires an OS-observed `sleep` identity among the recorded
descendants (birth identity rechecked around the name read), then verifies all
recorded processes have exited. It never reads process argv or environment.
Unfinished tracked jobs are canceled before their scenario result is emitted.
Fixture files remain private until the parent suite completes, allowing failure
diagnostics to inspect all rounds and negative cases before cleanup.

Environment variables:

| Variable | Meaning |
| --- | --- |
| `PACTIFY_E2E_HARNESS` | Required: codex, opencode, agy or kimi |
| `PACTIFY_E2E_MODEL` | Optional allowed model override |
| `PACTIFY_E2E_SCENARIOS` | Optional comma-separated scenario names; default all |
| `PACTIFY_E2E_HOST=1` | Include `host_path`; otherwise it is omitted, never mislabeled N/A |
| `PACTIFY_E2E_REPORT` | Optional JSON file to replace with this harness's summary |

Scenario names, in AJ-718 numbering: `write_worktree`, `readonly_denied`,
`readonly_long`, `resume`, `cancel`, `long_tool`, `large_output`, `parallel`,
`model`, `failure_surface`, `records`, `instructions`, `user_dirs_clean`,
`host_path`. Metadata is captured before the entire run and checked after the
host probe, so `user_dirs_clean` executes last. Unknown scenario names fail.

Codex and kimi use their subscription defaults; explicit kimi overrides must
stay under `kimi-code/*`. Agy is restricted to
`gemini-3.8-flash`. Opencode 1.x launches a private `opencode serve` and reads the live v1
`/config/providers` catalog without a prompt (the sole upstream metadata HTTP
request outside the pactify CLI), selects an `opencode/*-free` model (preferring flash), and validates
explicit overrides against that catalog: only `opencode/*-free` or
`minimax-coding-plan/MiniMax-M3` are allowed. The unauthenticated opencode 1.x probe
uses the latter, because a free endpoint does not require authentication. No
DeepSeek paid provider or other paid API channel is configured by the suite.

Authentication uses metadata and managed symlinks only: codex `auth.json`,
opencode 1.x `auth.json`, agy's OAuth login file, kimi `config.toml` and `device_id`,
and kimi's **entire `credentials/` and `oauth/` directories**. Directory links
preserve atomic refreshes and share kimi's interactive refresh lock. Kimi creates
`oauth/` lazily; when absent, prepare creates the shared source directory as 0700
before linking it. The suite does the same when preparing login inheritance;
`user_dirs_clean` reports this legitimate lock-directory creation separately. Never link
individual kimi credential files. No credential contents are read or copied.
Private HOME, CODEX_HOME, KIMI_CODE_HOME and XDG directories are automatic;
inherited API keys and harness overrides are removed.

Every explicit credential file link is checked at teardown, including codex and
agy (their atomic-write behavior has not been established). Opencode currently
writes auth.json in place but receives the same guard. A replaced link fails
loudly; its replacement is renamed, without opening it, into the owned 0700
`$PACTIFY_HOME/credential-quarantine/refresh-*/credential` in the outer test
process (default `~/.pactify/credential-quarantine`), outside every scenario fixture
root. Its absolute path is logged. This outer store is deliberately not removed
by fixture cleanup; retain it until quarantined refreshes are resolved. A failed
preservation retains the entire fixture root. Production uses
`filepath.Dir(store.Root)/credential-quarantine`, on the same
volume as managed homes. Only explicitly marked **credential** links are eligible
for write-back: opencode auth.json, agy's OAuth token, and kimi's shared credentials
directory (whose normal atomic writes go straight to the source). Codex has no
production managed-home file links; its E2E auth.json link still has the strict
fixture guard. Kimi config.toml and device_id are configuration and private
replacements are discarded, never written back. These file-configuration discard
warnings are informational and do not become a successful job's State.Error.
Before write-back, the private
credential file is chmod'd to the source's original permissions; chmod failure
quarantines it and emits a warning. Changed source metadata also quarantines it.
Metadata validation and rename cannot exclude an interactive agy in-place
refresh between the two operations; this remaining window is tracked in
`docs/backlog.md`. Inner fixture-store quarantines are moved as whole directories
to the outer quarantine before fixture deletion; a failed move retains the fixture.
Agy's `/usage` is a built-in, non-billable command, captured before/after all scenarios. Gemini
weekly quota below 20%, or an unavailable/unrecognized safety snapshot, prevents
live agy prompts; these rows are FAIL with an explicit skipped reason, not N/A.
`sub quota` only reads previously recorded exhausted routes, so it cannot replace
this live remaining-quota check.

Every selected scenario emits `E2E <harness> <scenario> PASS|FAIL|N/A <reason>`.
Only upstream unsupported features receive N/A (kimi read-only, after an actual
CLI probe verifies exit code 2 and the unsupported-capability diagnostic). An
unexpected job ID is tracked, canceled immediately and fails the scenario;
cancellation failure also fails the scenario. The manually
untriggerable quota-exhaustion subcase is documented as N/A / covered by golden
replay in `failure_surface`; it does not make other subcases pass. Environment
restrictions and model noncompliance are failures with evidence, not upstream
capability exemptions. The unchanged CLI read-only 40-tool-call cap can make
`readonly_long` fail; this test deliberately requires 60 separate invocations.

Failures log job IDs, selected status fields and `sub logs --tail 40`, sanitized
with the existing AJ-713 sanitizer before output/persistence. Full job logs are
only in temporary job storage and are removed on normal completion. Reports do
not preserve credentials or raw user configuration. Metadata snapshots use
Lstat/WalkDir without following links and compare inode, mode, size, mtime and
symlink target. Changes to existing 0600 regular login files with unchanged mode are listed
separately as possible credential refreshes; the suite cannot prove the cause
from metadata. Kimi oauth lock creation/removal and stable oauth-directory mtime changes are
exempted separately. Leftover `*.tmp.*` files, permission changes and symlink
replacements fail. Other creation or deletion changes are not assumed
to be refreshes. Related parent-directory size/mtime changes are exempted only
when all changed descendants are known refresh candidates.
Concurrent user sessions can cause a conservative `user_dirs_clean` failure.

`modelVerified` retains the adapter's existing meaning; the suite does not claim
an independent provider audit. `records` compares fields where each command
actually exposes them; cost has token/model grouping but no per-job duration or
worktree columns. Missing usage must be partial, rather than a fabricated known
zero. `truncated` describes final-text truncation, not every harness's own tool
output clipping. The large-output row also reports native boolean truncation
flags observed in tool records and requires a result warning when one is present;
absence of a flag is not proof of complete upstream output. Opencode read-only uses the existing AJ-713 diagnostic plan
prompt override to elicit actual denied tools without changing permissions.
Agy's instruction probe supplies both AGENTS.md and GEMINI.md and says so.

The host probe requires Claude login and runs the non-billable `claude auth
status --json` before any prompt; missing login or an unavailable status command
fails explicitly. Claude 2.1.301's installed code supports
`CLAUDE_SECURESTORAGE_CONFIG_DIR`: on macOS it is set to the empty string to
select the default `Claude Code-credentials` keychain entry; on Linux it points
to the user's `.claude` directory. Host configuration remains in a private
`CLAUDE_CONFIG_DIR`; no `.credentials.json` symlink is created because Claude
opens it with no-follow. Teardown checks both private HOME/.claude and the
private host config directory for an unexpected plaintext .credentials.json
fallback; any file is renamed to the outer quarantine without opening it and
fails the scenario. Preservation failure retains the fixture. This secure-storage exception intentionally uses the
existing login store. The probe uses `claude -p --model haiku --plugin-dir ...`, and
puts the built pactify first on PATH. It checks both a real callee job and the
byte-exact applied payload, so a direct host write alone cannot pass. It enables
bypassPermissions only inside the disposable host fixture; callee permissions
remain those of the plugin's normal job path. Environments that prohibit nested
sandbox creation cannot establish codex write/read/cancel behavior and will
report failed probes instead of silently disabling the sandbox.

`long_tool` requires a recorded `sleep 240` command, actual tool-output marker,
and job-record duration of at least 240 seconds. A final answer repeating the
prompt marker cannot pass. Read-only denial checks use native error structures;
`error: null`, successful tool records and verbal refusals do not qualify.

If credential preservation fails, the diagnostic names the retained file and the
job may remain blocked on every subsequent prepare. Stop pactify jobs and
interactive sessions for that harness first. Do not delete the retained file or
its fixture. Resolve directory ownership/0700 permissions and same-volume rename
availability, then retry; prepare will safely re-evaluate source metadata. If
manual recovery is needed, the Owner must choose between restoring the retained
file to the correct login source by rename (preserving the original source mode)
or renaming it to an owned 0700 persistent quarantine on the retained file's
volume. Do not blindly overwrite a source that also refreshed. If neither is
possible, keep the complete fixture and its reported path for recovery. No
credential contents need to be opened, printed or copied for these steps.

Opencode 2.x uses the product's cached version probe and backend selector. Each
fixture has a private persistent v2 home under its own PACTIFY_HOME; no user
login file or database is linked. Discovery uses the selected product /api/model
metadata API. The default remains an available opencode/*-free model. The four
real user XDG roots and user opencode.db are compared by metadata only, without
credential-refresh exemptions.

The v2 readonly checks require job success, absent write targets, and deny
results for shell/edit/external_directory evaluated on that job's own session.
Write tools are removed before model execution, so attempted-write events are
not required. readonly_long still demands 60 separate reads and a correct
answer; the 40-call limit is unchanged and any limit failure remains a FAIL.
The v2 failure surface uses an unavailable provider/model preflight (auth or
model classification), since free endpoints require no login.

```sh
PATH=<v2-bin-dir>:$PATH PACTIFY_HOME=~/.pactify-e2e/opencode2 \
PACTIFY_E2E_HARNESS=opencode PACTIFY_E2E_HOST=1 \
go test -tags harness_e2e ./internal/sub/e2e -run '^TestHarnessE2E$' \
  -count=1 -v -timeout 120m
```

Offline tagged fixture checks (no model calls):

```sh
PACTIFY_HOME="$(mktemp -d /private/tmp/aj720.XXXXXX)" \
go test -tags harness_e2e ./internal/sub/e2e -run '^TestOpencode2' -count=1
```
