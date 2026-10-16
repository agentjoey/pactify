# AJ-713 — consistency gates, part one

2026-10-17. Changes remain in the current worktree; no commit, plugin changes,
Experimental-label changes, live agy invocation, p0eval access, or sub-agents.

## Changes and dependency review

Before changing behavior, read harness-v2 §2/§4, AJ-710's HTTP/SSE adapter,
AJ-711's persistent process, and AJ-712's report, ACP/wire verification and tests.

- `StartOpts` is shared by cockpit, worker, catalog and doctor. Raw recording is an
  optional pointer with a nil default; ordinary transports keep their existing
  behavior. ACP's existing `Spawn` signature and pipe-backed tests remain valid;
  `SpawnObserved` attaches observation before reader/writer goroutines start.
  Agy's observer sees encoded user lines and raw stdout without starting another
  process. Opencode's opt-in transport retains the existing HTTP client/auth and
  wraps only response bodies. Recording helpers never open credential files.
- Failure classification is consumed by `internal/sub/retry.go`, worker retries,
  quota/fallback and final results. No category rules, retry limits, fallback
  behavior, reserved harness names, or stored result formats changed. The new
  doctor check injects a known offline protocol error into the same production
  classifier and asserts `transient`; it performs no inference or network call.
- Doctor's live write/model/read-only/hook/network/session/home probes retain
  their existing conditions, version-drift checks, cleanup and verified-version
  update rules. All preflight and completion paths now normalize to ten ordered
  rows. Sandbox is declaration-only: codex PASS, the others WARN with the exact
  requested text. Non-agy hooks and kimi read-only explicitly explain N/A.
  Codex's Managed home describes native per-process configuration overrides;
  it does not claim a separate HOME directory.
- Existing `TestDoctorChecksAndIsolation` hard-coded six codex / seven kimi rows.
  AJ-713 explicitly supersedes that contract: only its row-count expectation was
  changed to ten after observing failure. Its existing pass/fail, kimi SKIP,
  probe-count, isolation and verified-version assertions remain intact. The first
  race run also exposed `TestAgyDoctorEvidence`
  (eight rows → ten) and `TestOpencodeDoctorNetworkV2` (new Sandbox WARN and
  explicit N/A rows; this fixture deliberately sets Home=nil). Those assertions
  were migrated only for the newly requested rows; all prior probe rows still
  require PASS. No existing failure condition was weakened.
- Weekly/manual `harness-drift.yml` checks latest kimi/agy help, unauthenticated
  kimi ACP initialize, and agy fixture-derived event constants. PRs run only the
  offline static part. Installation unavailability is a documented SKIP, not a
  passed drift check. Version differences recommend `run pactify sub doctor`.
  The baseline versions are 2.2.0 / 1.4.0, supported by AJ-712 / AJ-711 reports.
  Official installation references: [Kimi](https://moonshotai.github.io/kimi-code/)
  and [Antigravity](https://antigravity.google/docs/cli/install/).
- §4 now contains C1–C17 × four harnesses with evidence in every cell. Remaining
  gaps are marked honestly, including opencode effort-list export and deferred
  real agy goldens. No promotion is claimed. The plugin stays at 0.2.4.

## Regression-first evidence

The first full race run failed on the two additional old doctor contracts
noted above; it was not counted as a pass. The first full Go run, compiled before
those assertion migrations, failed on the same two contracts. Initial runs also
failed on undefined sanitizer/recording APIs, missing real recordings
(`need 5 opencode + 4 kimi real recordings, got 0`), missing uniform-doctor helper,
and the old six/seven-row assertion. The multiline redaction regression exposed
unredacted NDJSON fields and token-shaped strings before the fix. A separate
managed-directory regression failed before metadata-only containment validation
was added. Concurrent snapshot coverage checks recorder ownership under race.

`TestGoldenReadOnlyContainsDeniedWrite` failed on a model's verbal refusal before
successful re-recording. `TestGoldenDetectsRenamedTokens` preserves valid JSON,
changes `tokens` / wire `usage` to `tokenz`, and requires a replay outcome mismatch;
it does not count an unrelated launch/parse error as success.

## Real recordings

Nine sanitized transcripts are checked in the worktree under
`internal/cockpit/testdata/golden/`. Tests run the real adapters with recorded
external boundaries and compare turn-event sequence, final text, exact usage,
failure category and session ID. Housekeeping metadata/menu/empty-usage callbacks
are excluded from ordering because they race session registration/turn completion;
raw frames remain available. Cancellation uses the lifecycle's `canceled` category
while retaining raw ACP/SSE failure evidence. Child-wire and per-agent baseline
behavior remains covered by the existing AJ-712 regressions.

| Harness | Scenario | Recorded result / evidence |
|---|---|---|
| opencode 1.19.0 | Write | Created `golden.txt` containing alpha; `GOLDEN_WRITE_OK` |
| opencode 1.19.0 | Plan read-only | `ls` succeeded; actual bash `echo x > denied.txt` was denied by permission rules; no file created |
| opencode 1.19.0 | Resume | Same session ID; tool read alpha; `GOLDEN_RESUME_alpha`; baseline-subtracted usage |
| opencode 1.19.0 | Cancel | Recorded abort request and `MessageAbortedError`; canceled with zero usage |
| opencode 1.19.0 | Failure | Nonexistent submitted model produced real SSE `session.error`; `other`, zero usage |
| kimi 2.2.0 | Write | Created `golden.txt` containing alpha; `GOLDEN_WRITE_OK` |
| kimi 2.2.0 | Resume | Same session ID; read alpha; `GOLDEN_RESUME_alpha`; per-agent wire delta |
| kimi 2.2.0 | Cancel | Recorded `session/cancel`; canceled, zero usage |
| kimi 2.2.0 | Failure | Generated managed provider config and dummy key; loopback peer immediately closed connections; wire `provider.connection_error` → transient, zero usage |

Final opencode goldens use the live-listed `opencode/mimo-v2.6-flash-free`.
Discarded read-only attempts used only free models or the explicitly allowed
`minimax-coding-plan/MiniMax-M3.1-Flash-Preview`. Verbal refusals and provider errors
were not accepted as final read-only evidence. To elicit an actual attempt, the
final recorder overrides only the plan agent's diagnostic prompt; the native
`agent: plan` identity and deny permissions are unchanged. The transcript notes
this setup. The real tool error, rather than the final answer, proves denial.
The unknown-model case starts with a validated free model, then injects the invalid
model only into the submitted prompt so local catalog validation cannot mask SSE.

Kimi had eight recording invocations: four retained scenarios and four unsuccessful
setup/write attempts. Two write attempts exited at session setup due to native file
watch errors; one invalid generated provider config failed authentication before
prompt; one write attempt failed when OAuth tried to create a temporary credential
through the directory link. Polling resolved the watcher problem; generated config
uses the supported `openai` provider; credential leaf links resolved private refresh.
No more than ten recording invocations were made. The retained offline failure
included the CLI's own local retries; every request stayed on loopback and could
not bill. Future generated failure configs cap attempts at one. A separate drift
smoke used only version/help/ACP initialize, without session creation or prompt.

Kimi read-only is intentionally absent and explained by
`TestGoldenKimiReadOnlyUnavailable`. No real agy was invoked; stdin/stdout capture
is tested with the Go test executable. The agy drift fixture is explicitly
synthetic AJ-711 evidence, with live recording deferred until host evaluation ends.

## Redaction and user-directory isolation

Structured token/password/Authorization/cookie/device/email fields (including the
provider catalog's `key` field), common standalone token formats, and absolute user
paths are redacted before saving. HTTP headers are omitted. The independent scan
walks embedded JSON, NDJSON and SSE; errors identify field paths, never secret values.
No credential contents were read, printed, or copied by the agent/recorder.
Authentication was inherited only through links used by the CLI.

Metadata snapshots compare inode, mode, size, nanosecond modification time and link
target for `~/.config/opencode`, `~/.local/share/opencode` and `~/.kimi-code`.
The snapshot contains 10,738 entries, does not follow symlinks, and excludes recursive
`.git`, `node_modules` and `bin` contents. The executable itself was never updated.
After all live calls: **zero changes in all three user-directory trees**.
Kimi's OAuth refresh wrote a private replacement inside the managed credentials
directory; the user's credentials metadata did not change. The private replacement
is removed after recording without opening it. No user config file was edited.

The recording entry point is `go test -tags record`, with an explicitly supplied
isolated PACTIFY_HOME and private HOME/XDG/KIMI_CODE_HOME. `ManagedDirectory` rejects
symlinked or escaping roots before CLI launch. Ordinary tests cannot record.
Usage and setup are documented in `internal/cockpit/golden/README.md`.

Startup `git pull` failed because this worktree is detached; `.agent/CURRENT.md`
was absent. No branch switch or commit was attempted. macOS validation uses the
already-installed Xcode git directly to avoid the known Apple shim/xcrun failures
under test HOME isolation. All test PACTIFY_HOME values are disposable; live resume
recordings retained one private recording root across turns. No real `~/.pactify`,
`~/.codex` or `~/.claude` was used as test storage.

## Validation output

The final acceptance outputs follow. The record-tag build check deliberately
SKIPs the live recorder without explicit recording environment variables; this
is the expected opt-in guard, not missing replay coverage.

### Build

```sh
PATH="/Applications/Xcode.app/Contents/Developer/usr/bin:$PATH" go build ./...
```

Exit 0.

```text
(no stdout/stderr)
```

### Race acceptance

```sh
PATH="/Applications/Xcode.app/Contents/Developer/usr/bin:$PATH" PACTIFY_HOME="$(mktemp -d /private/tmp/aj713.XXXXXX)" go test -race ./internal/cockpit/... ./internal/sub/... -count=1
```

Exit 0.

```text
ok  	github.com/agentjoey/pactify/internal/cockpit	95.069s
ok  	github.com/agentjoey/pactify/internal/cockpit/codexschema	1.602s
ok  	github.com/agentjoey/pactify/internal/cockpit/golden	2.064s
ok  	github.com/agentjoey/pactify/internal/sub	143.011s
```

### Full Go acceptance

```sh
PATH="/Applications/Xcode.app/Contents/Developer/usr/bin:$PATH" PACTIFY_HOME="$(mktemp -d /private/tmp/aj713.XXXXXX)" go test ./... -count=1
```

Exit 0.

```text
ok  	github.com/agentjoey/pactify/cmd/pactify	257.715s
ok  	github.com/agentjoey/pactify/internal/acp	5.350s
ok  	github.com/agentjoey/pactify/internal/agent	1.212s
ok  	github.com/agentjoey/pactify/internal/agentcfg	1.481s
ok  	github.com/agentjoey/pactify/internal/agentmanifest	1.752s
ok  	github.com/agentjoey/pactify/internal/agentreg	2.040s
ok  	github.com/agentjoey/pactify/internal/audit	2.321s
ok  	github.com/agentjoey/pactify/internal/cockpit	64.040s
ok  	github.com/agentjoey/pactify/internal/cockpit/codexschema	3.004s
ok  	github.com/agentjoey/pactify/internal/cockpit/golden	3.129s
ok  	github.com/agentjoey/pactify/internal/diffstat	2.846s
ok  	github.com/agentjoey/pactify/internal/doctor	3.404s
ok  	github.com/agentjoey/pactify/internal/event	2.926s
ok  	github.com/agentjoey/pactify/internal/finish	2.924s
ok  	github.com/agentjoey/pactify/internal/gitx	4.429s
ok  	github.com/agentjoey/pactify/internal/ledger	3.946s
ok  	github.com/agentjoey/pactify/internal/lockx	2.958s
ok  	github.com/agentjoey/pactify/internal/mcp	3.928s
ok  	github.com/agentjoey/pactify/internal/orchestrate	43.690s
ok  	github.com/agentjoey/pactify/internal/pact	18.874s
ok  	github.com/agentjoey/pactify/internal/paths	2.847s
ok  	github.com/agentjoey/pactify/internal/planner	4.971s
ok  	github.com/agentjoey/pactify/internal/procx	2.402s
ok  	github.com/agentjoey/pactify/internal/projection	12.761s
ok  	github.com/agentjoey/pactify/internal/recipe	2.261s
ok  	github.com/agentjoey/pactify/internal/registry	2.169s
ok  	github.com/agentjoey/pactify/internal/roles	2.301s
ok  	github.com/agentjoey/pactify/internal/runguard	2.124s
ok  	github.com/agentjoey/pactify/internal/sarif	1.861s
ok  	github.com/agentjoey/pactify/internal/schedule	1.703s
?   	github.com/agentjoey/pactify/internal/secret	[no test files]
ok  	github.com/agentjoey/pactify/internal/serve	15.383s
ok  	github.com/agentjoey/pactify/internal/sessions	2.425s
ok  	github.com/agentjoey/pactify/internal/stats	2.454s
ok  	github.com/agentjoey/pactify/internal/sub	110.814s
ok  	github.com/agentjoey/pactify/internal/subpanel	16.443s
?   	github.com/agentjoey/pactify/internal/testenv	[no test files]
ok  	github.com/agentjoey/pactify/internal/tokens	2.132s
ok  	github.com/agentjoey/pactify/internal/wizard	2.427s
```

### Plugin bats

```sh
bats tests/plugin_pactify.bats
```

Exit 0.

```text
1..32
ok 1 delegation guides recommend selecting the resume job explicitly
ok 2 marketplace lists the pactify plugin
ok 3 plugin.json is valid and named pactify
ok 4 forwarding agents use haiku and only Bash
ok 5 skills route large tasks and wait on the main thread
ok 6 session-start hook reports a missing pactify without failing the session
ok 7 session-start hook configuration invokes an executable script with a quoted plugin path
ok 8 session-start hook reports a CLI without sub and exits successfully
ok 9 session-start hook silently accepts a CLI with sub
ok 10 kimi skill does not offer read-only (Phase 0 K1)
ok 11 delegate skill is self-contained and uses the default harness
ok 12 kimi skill labels its experimental status and Phase 0 limitations
ok 13 README documents default delegation and experimental kimi
ok 14 skills isolate large tasks and share the review apply discard resume workflow
ok 15 all three skills embed the same complete delegation guide
ok 16 skills provide a complete brief example within 25 lines
ok 17 skills require host validation before apply and give actionable rework instructions
ok 18 forwarders preserve isolation and leave validation and rework decisions to the host
ok 19 agy skill and forwarder preserve the shared guide and explicit permissions
ok 20 agy documents restored read-only access and persistent protocol
ok 21 jobs skill exists with valid frontmatter
ok 22 jobs is read-only and never executes lifecycle commands
ok 23 jobs documents default all and single job invocations
ok 24 jobs bounds list output and preserves summary field semantics
ok 25 all delegation skills share the short title instruction and jobs uses titles without extraction
ok 26 jobs next steps require review and host acceptance before apply
ok 27 jobs details include bounded result logs diff and resume links
ok 28 READMEs and plugin description advertise jobs and all harnesses
ok 29 opencode plugin is experimental and shares the delegation contract
ok 30 agy read-only restored after AJ-683 with explicit write permission
ok 31 opencode read-only uses plan and declares its sandbox boundary (AJ-710)
ok 32 agy documents managed hook opt-out and bumps plugin version (AJ-711 review)
```

### Additional checks

`PACTIFY_HOME="$(mktemp -d /private/tmp/aj713.XXXXXX)" go test ./internal/cockpit -run '^TestGolden' -count=5`:

```text
ok  	github.com/agentjoey/pactify/internal/cockpit	38.006s
```

ACP and recorder race checks (isolated PACTIFY_HOME):

```text
ok  	github.com/agentjoey/pactify/internal/acp	6.993s
ok  	github.com/agentjoey/pactify/internal/cockpit/golden	1.875s
```

`python3 -m unittest discover -s internal/cockpit/harnessdrift -p '*_test.py' -v`:

```text
test_handshake_is_typed (check_test.ContractTests.test_handshake_is_typed) ... ok
test_missing_flag_fails (check_test.ContractTests.test_missing_flag_fails) ... ok
test_renamed_event_is_detected (check_test.ContractTests.test_renamed_event_is_detected) ... ok
test_static_adapter_contract (check_test.ContractTests.test_static_adapter_contract) ... ok
test_version_drift_prompts_doctor (check_test.ContractTests.test_version_drift_prompts_doctor) ... ok

----------------------------------------------------------------------
Ran 5 tests in 0.001s

OK
```

`python3 internal/cockpit/harnessdrift/check.py --static`: exit 0, no output.
Local non-billable kimi drift smoke:

```text
kimi: installed 2.2.0, matches pactify contract baseline
PASS kimi: help and unauthenticated ACP initialize; no session or prompt created.
```

The record-tag build/opt-in guard (isolated PACTIFY_HOME, no recording variables):

```text
=== RUN   TestRecordGolden
    golden_record_test.go:28: set PACTIFY_RECORD_HOME and an isolated PACTIFY_HOME explicitly
--- SKIP: TestRecordGolden (0.00s)
PASS
ok  	github.com/agentjoey/pactify/internal/cockpit	0.680s
```

`git diff --check`: exit 0, no output.

The scheduled latest-install workflow has not been run from this worktree. Its
static contract tests and local kimi handshake passed; installation-dependent agy
checks remain CI's responsibility. No live agy check is claimed.

Final metadata comparison after all acceptance tests:

```text
~/.config/opencode: 0 metadata changes
~/.local/share/opencode: 0 metadata changes
~/.kimi-code: 0 metadata changes
```
