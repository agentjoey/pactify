# AJ-720: opencode 2.x implementation and review rework

Worktree only, 2026-10-18. No commit, push, merge, credential access, CLI upgrade or inference request was performed by the agent. The host has supplied five real golden files; this rework preserves them unchanged and replays them offline. No live e2e/model execution was performed by the agent.

## Rework implementation and regression tests

| Review item | Implementation | Regression coverage |
| --- | --- | --- |
| B1, S1 | `opencode2.go`: readonly is exactly wildcard deny, read/glob/grep allow, final shell wildcard deny; no shell whitelist or pattern exceptions. Write remains edit/shell ask and question deny. v1 rules untouched. Exported `Opencode2Permissions` feeds the Go helper consumed by Python. | `TestOpencode2PermissionPolicyAndLease`, `TestOpencode2LastMatchingRules`: complete ordered rule equality and last action match; Python exported-policy equality. |
| Startup lease, N2 | `opencode2_server.go`: private-home startup acquires exclusive lock, releases on every return including ready/failure/cancel. Wait bounded at two minutes with actionable hint. Login holds lock for its entire command. User mode acquires no lock and creates no lock directory. | `TestOpencode2StartupLeaseScope`, `TestOpencode2StartupFailureReleasesLease`, `TestOpencode2LoginHoldsLease`; existing exclusivity test checks timeout hint. |
| S1 existing children | Resume discovers and baselines descendants before prompt, then PATCHes full policies to root, child and grandchild. | `TestOpencode2ResumeAndModelDiscovery`, `TestOpencode2ResumeReadonlyExistingChildren`; fixture exposes descendants before any prompt. |
| S2 | History presence marks missed delivery; success requires a root terminal event or a prompt-bounded durable outcome. Historical success clears reconnect exhaustion. `/wait` closure finishes reconciliation directly. Two-minute stream inactivity bound. | `TestOpencode2RecoveryEvidence`: missing boundary, missing outcome, missed delivery with failing wait, exhausted reconnect with historical success, wait close without extra reconnect, inactive stream. |
| S3, N5, N6 | Job resume into v2 requires explicitly recorded major 2; missing/unknown old result rejected. v1 missing-result compatibility retained with an informational warning. Saved private-home paths checked before preparation. Backend no longer mislabels id prefixes as v1/v2 provenance. | `TestOpencode2ResumeRequiresKnownMajorAndPrivateHome`, cross-version tests, API 404/StartError test. |
| S4 | Login uses MinimalConfig environment, private OPENCODE_CONFIG_DIR, empty CONFIG/CONFIG_CONTENT, project/Claude config disabled, neutral managed directory. | `TestSubOpencodeLogin`: hostile inherited config env, exact args, all XDG roots, config/cwd isolation, stream forwarding. |
| N1 | DB pinning only for MinimalConfig; user mode retains native DB/channel selection. | `TestOpencode2UserDatabaseSelectionAndLegacyFailures`. |
| N3, N4 | Historical interruption without a user reason fails explicitly; final text/outcome comes from the first idle after the prompt, excluding later background wakes. | Recovery interrupted case and `TestOpencode2Flows/background` with later assistant text and failure. |
| N8, N11, N12 | Suppress reasoning/progress/usage increments; typed failure shortcut only for recognized v2 namespaces; missing and null default model both ErrModelUnavailable. | Flow noise assertions, legacy APIError with extra type field, missing/null default-model cases. |
| N10 | An interrupt that returns interrupted:false is retained and retried when the prompt is delivered. | Recovery cancel-before-delivery case requires two interrupt requests. |
| Cancel recording | v2 synthetic replay waits tool.called or owned shell.created. Real recording waits shell.created; 2.0.32 omits pid, so the host added a fallback binding the live private-server descendant tree at that instant. It then interrupts and asserts that the captured processes exited. No ps/environment read is needed. | `TestOpencode2CancelRecordingWaitsForRunningSleep` starts a local sleep via the fake peer; five-scene synthetic replay requires tool start for cancel. |
| Observed events | Owned permission.rejected and shell events tolerated; shell.created metadata establishes ownership, shell.exited correlates by id. Global integration/provider/model inventory updates ignored by job filter. Contract event catalog updated. | Synthetic flow includes permission/inventory updates; real-process cancellation exercises shell.created. |

## Source verification and readonly safety

Read-only source inspection confirmed `packages/core/src/tool/plugin/shell.ts:140-148` skips permission.assert when `parsed.commands` is empty. `shell/parse.ts:173-198` allows redirect-only forms to produce an empty command list. Host independently confirmed a real write without approval on 2.0.32. `core/src/tool.ts:231,292-295` omits a tool when its last action match has resource `*` and effect `deny`. Therefore v2 readonly shell is entirely disabled; only read/glob/grep remain allowed. No inference-free per-session exposed-tool inventory endpoint was identified in the inspected protocol groups. Host has now confirmed the model sees only glob, grep and read, and `> marker.tmp` did not create a file. The agent's permission API probe itself only proves rule evaluation.

`schema/src/shell.ts:38-59` advertises an optional PID, but the host confirmed actual 2.0.32 shell.created omits it: info contains command/cwd/file/id/metadata/shell/status/time. The retained host recorder snapshots live descendants when pid is absent. shell.exited carries id/status/exit. The six additional event types were observed by the host in real 2.0.32 recordings, not newly observed by the agent through inference.

## Database concurrency conclusion

Host measurements (provided with the rework request):

- Three concurrent servers against an empty DB: two failed with SQLite database locked.
- Against an initialized DB: all three started successfully in three trials.
- Three servers performed 75 concurrent session create/PATCH/evaluate/read/delete writes with zero errors and mutually visible writes.

Source: `core/src/database/database.ts:39-47,61-70` sets WAL before busy_timeout and uses a process-local migration semaphore. `server/src/process.ts:92-133` announces ready after initialization. The implementation therefore serializes every private-home startup only until ready; initialized servers run concurrently on the same persistent DB, preserving in-place credential refresh. Login has no observed startup-ready boundary and holds the lock throughout. The host measurements were not repeated by the agent; fake-server tests verify lock lifetime and failure cleanup, not SQLite internals. User-config mode opts into native user database concurrency and creates no pactify lock there.

## Rework 1 non-billable real binary checks

Real 2.0.32 in a fresh HOME and all four XDG roots: authenticated stdio OpenAPI probe and v2 projection check passed, without a prompt. The Python permission probe uses Go-exported policies and creates/deletes only disposable sessions:

| Policy | Action/resources | Result |
| --- | --- | --- |
| readonly | shell: git status; ls -la; > output; X=value > /dev/null; rm -rf x; echo x > f; [git status, sh]; sh | deny (all eight cases) |
| readonly | edit; external_directory; webfetch | deny (all three cases) |
| write | edit | ask |
| write | question | deny |

All 13 cases passed in rework 1. This agent probe did not invoke a shell tool or inspect model-visible inventory. Host has since verified inventory, parallel execution and real cancellation and supplied the five recordings; see rework 2 below. Login/refresh was not independently exercised by the agent.

## Mutation evidence

Go overlays (real workspace sources unchanged) were used to rerun the new assertions. All three mutation commands exited 1 with assertion failures: write question deny → allow failed `TestOpencode2PermissionPolicyAndLease` (ordered write rules); readonly shell wildcard deny → allow failed the same test (ordered readonly rules); empty resume child PATCH range failed `TestOpencode2ResumeReadonlyExistingChildren` (missing ses_child PATCH). Other per-fix rollback variants were not exhaustively executed; do not interpret regression coverage as a completed mutation run for every fix.

## Deferred items (also in local docs/backlog.md)

- B1: reassess readonly shell whitelist only after upstream fixes the empty-command permission bypass and host real execution verifies it. Upstream locations are recorded above and in the code comment.
- N7: frozen governance `internal/serve/cockpit.go:106` still constructs NewACPBackend("opencode"); leave unchanged per host decision and revisit only if that path is revived.
- Raw SSE event payloads remain encoded strings in OpenAPI; projection cannot detect their field drift. Fake events and host real golden recordings cover that separate contract.

`docs/backlog.md` is gitignored in this repository; these same entries are repeated here to survive a diff-only review.

## Host commands

Set `OC2_BIN` to the supplied v2 executable; prepend its directory to PATH, without modifying the installed 1.x CLI. Run from this worktree.

Non-billable reproducible probes:

```sh
export OC2_BIN=/path/to/2.0.32/opencode
python3 internal/cockpit/opencodeapi/probe.py --v2 --binary "$OC2_BIN" /private/tmp/oc2-openapi.json
python3 internal/cockpit/opencodeapi/check.py --v2 /private/tmp/oc2-openapi.json
python3 internal/cockpit/opencodeapi/permissions_probe.py --binary "$OC2_BIN"
```

Record the five real golden scenarios (these commands do send model requests; the agent did not execute them):

```sh
export PATH="$(dirname "$OC2_BIN"):$PATH"
record_root=$(mktemp -d /private/tmp/aj720.XXXXXX)
mkdir -p "$record_root"/{home,share,config,state,cache}
export HOME="$record_root/home" XDG_DATA_HOME="$record_root/share"
export XDG_CONFIG_HOME="$record_root/config" XDG_STATE_HOME="$record_root/state"
export XDG_CACHE_HOME="$record_root/cache" TMPDIR=/private/tmp
export PACTIFY_HOME="$record_root" PACTIFY_RECORD_HOME="$record_root"
export PACTIFY_RECORD_MODEL=opencode/deepseek-v4.1-flash-free
for scenario in write readonly resume cancel failure; do
  PACTIFY_RECORD_SCENARIO="$scenario" go test -tags record ./internal/cockpit \
    -run '^TestRecordOpencode2Golden$' -count=1 -v -timeout=5m || break
done
PACTIFY_HOME="$(mktemp -d /private/tmp/aj720.XXXXXX)" \
  go test ./internal/cockpit -run '^TestOpencode2GoldenReplay$' -count=1 -v
```

Keep the recording root: it is the private session DB needed by resume. Recordings use free models or explicitly selected `minimax-coding-plan/*`; signing into the recording home, if needed, is `opencode auth login --standalone` in that isolated environment. Never link/copy a user's DB or credential file.

For CLI live verification, use another isolated HOME and four XDG roots, a new PACTIFY_HOME, the v2 PATH above, and a disposable git repository:

```sh
go build -o /private/tmp/pactify-aj720 ./cmd/pactify
/private/tmp/pactify-aj720 sub login opencode  # optional for a credentialed provider
/private/tmp/pactify-aj720 sub doctor opencode
printf '%s\n' 'Write smoke.txt containing alpha and report the result.' | \
  /private/tmp/pactify-aj720 sub run opencode --cwd /path/to/disposable/repo \
  --write --model opencode/deepseek-v4.1-flash-free --json
printf '%s\n' 'Read smoke.txt with the read tool and report its content.' | \
  /private/tmp/pactify-aj720 sub run opencode --resume=<previous-job-id> \
  --write --model opencode/deepseek-v4.1-flash-free --json
printf '%s\n' 'Read smoke.txt with the read tool. Explain why shell and writing tools are unavailable; do not seek a workaround.' | \
  /private/tmp/pactify-aj720 sub run opencode --cwd /path/to/disposable/repo \
  --read-only --model opencode/deepseek-v4.1-flash-free --json
printf '%s\n' 'Run sleep 60 and wait for it.' | \
  /private/tmp/pactify-aj720 sub run opencode --cwd /path/to/disposable/repo \
  --write --model opencode/deepseek-v4.1-flash-free --wait 0 --json
/private/tmp/pactify-aj720 sub cancel <sleep-job-id>
/private/tmp/pactify-aj720 sub result <sleep-job-id>
```

The AJ-718 suite now selects its fixture through the product ForVersion selector. v2 uses the private fixture home and product /api/model discovery; its readonly scenarios use permission evaluation instead of impossible attempted-write events. The live suite remains host-only; the agent runs only its offline unit tests and vet. Cross-major resume should also be exercised by changing only the test PATH between a v1 job and its resume, using an isolated PACTIFY_HOME.

## Rework 1 gate results

All required commands ran in this worktree and passed. Every Go test invocation used `PACTIFY_HOME="$(mktemp -d /private/tmp/aj720.XXXXXX)"`; recording commands below are instructions for the host, not agent executions.

| Command | Actual result |
| --- | --- |
| `go build ./...` | PASS |
| requested changed/new-file `gofmt -l` command | PASS, empty output |
| `go vet ./...` | PASS |
| isolated `go vet -tags harness_e2e ./internal/sub/e2e/...` | PASS |
| isolated `go vet -tags record ./internal/cockpit/...` | PASS |
| isolated `go test ./... -count=1` | PASS; CLI 274.467s, cockpit 88.411s, sub 251.052s, all packages passed |
| isolated `go test -race ./internal/cockpit ./internal/sub -run 'Opencode2' -count=1` | PASS on final code; cockpit 24.940s, sub 12.038s |
| final N6 provenance-warning regression | PASS, sub 0.421s; final race run above includes it |
| opencodeapi Python unittest discovery | PASS, 12 tests; final cleanup rerun 0.414s |
| harnessdrift Python unittest discovery | PASS, 12 tests |
| `python3 internal/cockpit/harnessdrift/check.py --static` | PASS |
| isolated real 2.0.32 `probe.py --v2` then `check.py --v2` | PASS, authenticated schema matched; no prompt |
| isolated real 2.0.32 `permissions_probe.py` | PASS, all 13 cases listed above |
| three Go overlay mutation runs | Each exited 1 with the intended assertion failure; original sources unchanged |
| `git diff --check` | PASS |

The full suite completed while the small N6 informational-warning refinement was being added. That final refinement was then verified by its focused regression and the final cockpit/sub Opencode2 race run; build/format/all vet gates were rerun after it. At that time real v2 golden replay skipped because host recordings were absent. They are now supplied and replayed in rework 2. Synthetic five-scene replay and the running local sleep cancellation test pass without inference.

Startup git pull could not proceed on the detached job HEAD; .agent/CURRENT.md was absent. No branch or base changes were made.

## Rework 2: host corrections and live evidence

Host changes retained without rollback:

- Interrupt is the only consumed v2 response without a data wrapper: POST /api/session/{id}/interrupt returns {"interrupted":bool}. `opencode2_session.go` uses call, not call2; the fake peer matches it.
- Actual shell.created omits pid. `recordedOpencode2Sleep` uses the live private-server descendant tree at shell creation when pid is zero, then verifies exit. It does not read process environment or credentials.
- Five recordings under `internal/cockpit/testdata/golden/opencode2/` use `opencode/deepseek-v4.1-flash-free`. The host cancel recording observed shell creation, interrupt, shell deletion, tool failure and execution.interrupted, and asserted process exit. This agent's explicit offline replay passed all five scenarios; the files were not edited.

Host live results (reported with rework 2, not repeated through inference by the agent):

- B1: readonly model inventory was exactly glob/grep/read; redirect-only `> marker.tmp` created no file.
- Three concurrent v2 jobs against a fresh private home/empty DB all succeeded in about eight seconds each, with independent worktrees. Startup serialization and parallel execution were observed together.
- Doctor passed Write, independently verified Model, Network, Session hiding and Managed home. Read-only access was the old evidence mismatch addressed in this rework.

## Rework 2: doctor and live-suite criteria

`internal/cockpit/opencode2_readonly.go` opens a private stdio server and evaluates shell (`touch x`), edit and external_directory on the completed probe's exact session id. `internal/sub/opencode2_readonly.go` loads that job's saved four XDG roots. It neither creates/resumes a session nor PATCHes permissions, so it cannot repair or replace the policy being checked. No prompt is sent. Doctor v2 passes only when the job succeeds, probe.txt is absent and all three effects are deny. No tool-call count or attempted-write event is required. Its English reason explicitly states that writing tools were removed and confirmed by permission evaluation. v1 doctor logic is unchanged.

`Harness.ForVersion` exposes the same existing selector and sets the selected Opencode2 marker; e2e never parses a major itself. `Opencode2ManagedHomes` supplies the private XDG paths to both product home preparation and fixture discovery. v2 fixtures inherit no user auth.json or database, use each fixture's PACTIFY_HOME, and discover enabled models through the selected product Models function (/api/model). Absent PACTIFY_E2E_MODEL, selection stays opencode/*-free, preferring flash.

`readonly_denied` and `readonly_long` require absent targets and the same three own-session deny evaluations. The former also requires job success; the latter keeps its exact 60 separate reads and correct concatenated answer checks. ToolCallLimit was 40 for these runs (raised to 200 by AJ-721); a resulting limit failure is reported with status/call count/warnings, never waived. This live scenario was not run by the agent.

`user_dirs_clean` snapshots metadata only for all four real XDG roots plus the user's opencode.db. v2 permits no credential-refresh exemptions in user paths. `failure_surface` requests a nonexistent provider/model to force preflight auth/model classification without relying on free-model login requirements. The existing model scenario similarly accepts v2's actionable no-login classification for an unavailable model. Other scenarios share existing code; cancellation additionally accepts the owned shell.created event as start evidence because it can precede tool.called.

Offline tests:

- `TestOpencode2DoctorReadonlyPermissionEvidence`: deny passes with zero tool calls; separate shell/edit/external_directory allow cases fail; present target and failed job fail. Checks the own-session endpoint and no session creation/PATCH/prompt. Both Go overlay mutation runs failed assertions as intended: removing the v2 doctor branch made the zero-call deny case FAIL instead of PASS; skipping permission evaluation made all three allow cases PASS instead of FAIL. Original sources were not changed by these runs.
- Untagged fixture tests cover product selection for 1.x/2.x and rejection of 3.x, inherited-variable stripping, all private XDG paths, native v1 environment preservation, metadata-only user DB change detection, and v2 shell-spawn evidence.
- Tagged offline tests `TestOpencode2LiveFixtureIsolation` and `TestOpencode2FixtureFreeModelDiscovery` construct a disposable fixture, confirm no login links and exercise private discovery/default free-model selection using an injected model function. They do not run TestHarnessE2E or a real harness.

Host live command (not executed by the agent):

```sh
PATH=<v2-bin-dir>:$PATH PACTIFY_HOME=~/.pactify-e2e/opencode2 \
PACTIFY_E2E_HARNESS=opencode PACTIFY_E2E_HOST=1 \
go test -tags harness_e2e ./internal/sub/e2e -run '^TestHarnessE2E$' \
  -count=1 -v -timeout 120m
```

Keep PACTIFY_E2E_MODEL unset for the available free-model default. The supplied PACTIFY_HOME is suite bookkeeping; every scenario constructs its own fixture PACTIFY_HOME and private v2 database.

## Rework 2 gates

All commands below passed in this worktree. All Go test invocations used `PACTIFY_HOME="$(mktemp -d /private/tmp/aj720.XXXXXX)"`.

| Command | Actual result |
| --- | --- |
| `go build ./...` | PASS |
| requested changed/new Go-file `gofmt -l` | PASS, empty output |
| `go vet ./...` | PASS |
| isolated `go vet -tags harness_e2e ./internal/sub/e2e/...` | PASS; repeated after the final tagged-fixture refinement |
| isolated `go vet -tags record ./internal/cockpit/...` | PASS |
| isolated `go test ./... -count=1` | PASS; final run CLI 276.643s, cockpit 94.734s, sub 262.491s, e2e offline helpers 6.354s; all packages passed |
| isolated `go test -race ./internal/cockpit ./internal/sub -run 'Opencode2' -count=1` | PASS; final cockpit 53.301s, sub 37.620s |
| isolated `go test -tags harness_e2e ./internal/sub/e2e -run '^TestOpencode2' -count=1` | PASS, final 0.813s; offline fixture tests only, never TestHarnessE2E |
| isolated `go test ./internal/cockpit -run '^TestOpencode2GoldenReplay$' -count=1 -v` | PASS, all five host recordings, 2.186s |
| opencodeapi Python unittest discovery | PASS, 12 tests |
| harnessdrift Python unittest discovery | PASS, 12 tests |
| `python3 internal/cockpit/harnessdrift/check.py --static` | PASS |
| two doctor Go overlay mutations | Both exited 1 with the expected PASS/FAIL assertion failures |
| `git diff --check` | PASS |
| supplied five golden SHA-256 values | Identical before and after this rework |

The final default-tag suite includes the absolute-path regression for a relative PACTIFY_HOME, preserving the preexisting managed-home behavior after extracting the shared home map. The last tagged-only refinement preserves other harnesses' original isolated version commands; harness_e2e vet and offline fixture tests were rerun afterward. No real model/e2e/doctor inference was run. readonly_long's live 40-call outcome remains for the host to report. Host live concurrency and inventory observations above are attributed to the host, not claimed as agent reruns.

Startup git pull was denied writing the shared FETCH_HEAD; .agent/CURRENT.md was absent. No base or branch changes were made.

## Host final verification (2026-10-18)

Fixes made by the host after rework 2, each with a regression test that fails without it:

| Live symptom | Cause | Fix | Test |
| --- | --- | --- | --- |
| `long_tool` retried once during a 240 s silent tool | the 2-minute inactivity timer was reset only by parsed events; 2.0.32 sends only `: heartbeat` comments while a tool runs | `activityReader` resets the timer on every byte of the stream | `TestOpencode2HeartbeatKeepsQuietToolAlive` (the fake now reports the session active and history without a terminal during the quiet window) |
| `instructions`: AGENTS.md ignored | `OPENCODE_DISABLE_PROJECT_CONFIG=1` also disables project AGENTS.md discovery | removed from the job environment; kept for `sub login` | `TestOpencode2MinimalEnvKeepsProjectInstructions`, `TestSubOpencodeLogin` |
| invalid model classified `auth`; unlogged MiniMax classified `model` | the login hint used the connection count only, and `/api/provider` lists connected providers only | suggest login only when `/api/integration` lists the provider with zero connections and no available model belongs to it | `TestOpencode2UnavailableModelLoginHint` |
| `user_dirs_clean` flagged other apps' files | the check watched whole XDG roots, and the user's own long-running opencode 1.19 server refreshes `~/.cache/opencode` and `~/.local/state/opencode` | watch `share/opencode`, `config/opencode` and the user DB (as 1.x does) | `TestOpencode2UserMetadataRootsAndDatabase` |

`TestSubOpencodeLogin` now compares the physical cwd, so it also passes with macOS's default `/var` → `/private/var` temp directories.

Live AJ-718 suite on 2.0.32 with `opencode/deepseek-v4.1-flash-free` and a persistent `PACTIFY_HOME=~/.pactify-e2e/opencode2`: 13/14 PASS (write_worktree, readonly_denied, resume, cancel, long_tool, large_output, parallel, model, failure_surface, records, instructions, user_dirs_clean, host_path); `readonly_long` stops at the 40-call cap. `sub doctor opencode` on the final build: all checks PASS, including Read-only access (shell/edit/external_directory deny confirmed); Sandbox is the usual informational WARN.
