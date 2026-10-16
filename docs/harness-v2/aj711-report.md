# AJ-711 — agy v2 review fixes and acceptance

All changes remain uncommitted in the supplied worktree. No subagents were used.
This report supersedes the earlier unauthenticated acceptance report. The host's
write/background/resume/cancel evidence was supplied with the rework request;
the live checks below were performed in this rework using flash only.

## Interruption diagnosis and dependencies checked

Doctor's disposable stores use the human-readable IDs `write`, `read-only`, and
`read-only-2`. `RunJob` previously exported `PACTIFY_JOB=write` without a store
identity. `State.KillHarnesses` calls `procx.FindJobProcesses(st.ID)`, which searches
all current-user processes. `procx.TerminateDescendants` sends SIGTERM to matches.
A different store's job named `write` can therefore terminate a doctor stream.

The host trace confirms overlapping replay activity, but the named
`TestZZSnapshotWarningBecomesReason` peer uses ID `job`, so it is not itself a
`write` collider. The historical sending PID was not recorded. A controlled
flash reproduction with the instrumented old binary established the mechanism:
a separate `State{ID:"write"}.KillHarnesses()` sender (PID 39142) issued SIGTERM
immediately after the real stream's init. Its log exactly reproduced:

```text
init.model=gemini-3.8-flash-low
error: interrupted
turn_failed "interrupted"
internal session Interrupt trace: absent
```

The negative probe deliberately used its own private PACTIFY_HOME and is not
counted as a passing doctor run. Other fake doctor workers also use the repeated
probe IDs, so shared marker cleanup is a concrete cross-store interruption path;
attributing the original host event to a particular unrecorded worker remains
an inference. The latest scoped doctor sequence repeats the same foreign legacy
cleanup after init to verify that the new marker survives.

Doctor now derives its process marker from the disposable store path and probe
ID, persists it with state, and uses it for cleanup. Regression coverage also
keeps a real detached `PACTIFY_JOB=write` peer alive across doctor cleanup.
Normal job markers and legacy state retain their existing ID fallback. No quota
child context cancels its parent; opening and closing probes have separate budgets.

Dependencies reviewed before changes:

- AJ-710's shared exact command list, read-only baseline, Finish/recovery checks,
  failure classification and doctor denied-write parser; kimi's closed read-only
  mode and existing worker cancellation/legacy error priorities remain intact.
- AJ-693's transient continuation and retry bounds; model-list prechecks remain
  best-effort for ordinary jobs, while `init.model` is authoritative.
- Cross-store process cleanup, cancellation, recovery, normal marker injection,
  detached tool ownership and worktree cleanup callers.
- Legacy `quota.json` readers/router fallback, cost snapshots, dashboard timestamp
  selection, shared managed HOME leases and the write user-config opt-out.
- Dashboard version discovery, model-list diagnostics, plugin shared delegation
  blocks, effort suffix constraints, and repository-local Git helpers.

## Fixes

- B1: `pactify sub agy-hook` replaces Python. Only the exact environment value
  `0` enables writes. Missing/empty/unknown values, malformed input, handler
  errors and input deadlines produce deny. Launch failure has a shell deny
  fallback. Native prefix grants are empty; the compiled hook approves only the
  exact command list shared with opencode. Thus `git diff --output=...`, shell
  redirection, pipes, `-delete` and `-exec` cannot inherit a prefix permission.
  Exact Git tools disable fsmonitor/external diff/textconv; host Git snapshots
  also disable fsmonitor without disabling normal write-operation Git hooks.
  The shared idle fingerprint disables fsmonitor and optional index writes too;
  its worker and orchestrate delivery-detection callers retain the same data contract.
- B2/S4: completion detection examines the final paragraph, requires an explicit
  running/launched operation with results still missing, and excludes reported
  results. The review's five complete answers are negative table cases. Active,
  started and ended tool maps reset per turn; every non-ACTIVE state is terminal.
  Followup exhaustion retains the last non-empty answer with a warning.
- S1/S2: observations live in `quota-snapshots.json`; `quota.json` contains only
  exhausted routes and remains compatible with rollback binaries. Legacy
  unreleased snapshot entries are pruned when quota storage is rewritten.
  Opening probes have a five-second budget. Terminal result/state publish first;
  the worker then allows up to ten seconds for a closing probe and atomically
  supplements quota metadata. Quota errors remain warnings, never State.Error.
  Opening ResetAt evidence is published before the closing probe starts.
- S3/S5: the context watcher runs before asynchronous stdin writes and writing
  holds no session mutex. Interrupt captures descendants before closing stdin or signaling, then bounds
  cleanup. A protected detached child that exits its parent on EOF is covered by
  a failing-then-passing regression and a fresh live cancellation check.
  A model mismatch immediately interrupts, latches `model_mismatch`, and cannot
  be overwritten by a later 503 or retried as transient.
- S6/S7: doctor adds an independent write-mode probe with the read-only hook
  environment and requires `denied by pre-tool hook: Pactify read-only policy`
  evidence for PASS. Missing attempts produce WARN. Doctor selects only
  `gemini-3.8-flash-*`; absent flash or an explicit non-flash model fails preflight.
- S8/S9: no workspace-hook disabling flag was found in the installed help or
  authoritative probe report. Read-only refuses `.agents`, `.agent` and `_agents`
  directories from the real cwd through the repository root, resolving symlinks.
  This preflight runs before model discovery, version and quota probes; an omitted
  cwd resolves to the actual working directory.
  The filesystem baseline covers repository tracked/untracked/ignored entries;
  writes outside that scope rely on the hook's deny rules. Write user config is
  retained with `managed hooks are not installed with --with-user-config`.
  Read-only still rejects user config. Plugin docs explain both limits; version
  advances from 0.2.2 to **0.2.3** (0.2.1 was the original checkout version).
- Nits: remove the test-only denial wrappers, obsolete AgyNoReadOnly and unreachable
  multiple-result branch; optionless Resume returns an error. All stream/model/
  usage/version paths and command hints pin a model. Dashboard version discovery
  uses the isolated version probe. Denial diagnostics reset per turn and only
  reject empty replies. Invalid stream records use descendant-aware interruption.
  Quota parsing tolerates an informational stdout line and skips malformed reset
  buckets (including invalid field types). Cost selects snapshots by the actual
  observation time after terminal publication. Conversation-cumulative result counters are excluded when step usage
  is absent, marking usage partial. Dashboard compares parsed timestamps.

## Existing tests deliberately migrated

The initial AJ-711 report's explicit AJ-683 read-only test migrations still apply.
This review additionally supersedes these expectations, as expressly requested:

- B1: `agy_policy_test.go` runs the compiled Go handler instead of a removed Python
  file. The shared fake v2 peer now has a repository root so workspace-hook checks
  cannot accidentally inspect the real user's ancestor directories.
- B2/S4: completion-limit tests expect a retained answer plus warning; the bare
  future promise without an explicit started operation is no longer pending.
- S1: snapshot-storage assertions read the new separate file.
- S6/S7: agy doctor fixtures, including cmd_sub_doctor_model_test.go, provide flash models, account for the extra hook
  probe, and keep read-only retry assertions scoped to plan-mode probes. Other
  harness model override rules stay unchanged.
- S5: the failed-usage fixture reports the explicitly pinned flash model, so it
  still tests all failed step counters instead of triggering the newly required
  immediate mismatch abort. Usage assertions are unchanged.
- Model-hint tests, including serve's catalog error expectation, now include
  `--model gemini-3.8-flash-low`, as required by the review.

Existing cancellation reason (`Task canceled`) and the legacy structured quota
error presentation (`Turn failed`) initially caught an overly broad error-priority
change. The implementation was corrected; those tests were not changed.

All substantive fixes had failing regressions before implementation. Red logs
are retained under `.agent/aj711-rework/`: cockpit-red, sub-red, hook-red,
doctor-hook-red, doctor-marker-red, nits-red, ui-red, plugin-red,
quota-publication-red, symlink-red, catalog-error-red, git-helper-red,
workspace-preflight-red, cwd-preflight-red, cost-observation-red,
quota-bucket-types-red, cancel-eof-red and fingerprint-red.

## Live evidence

All real agy invocations pinned `gemini-3.8-flash-low`, including help, version,
model listing and usage. `PACTIFY_HOME=.agent/aj711-rework/live-home` is private.
Normal pactify managed-HOME preparation linked the existing login file; no
credential contents were inspected or copied by the agent. Native fault probes
used that same pactify-created managed HOME.

- Read-only: both `cat README.md > x.txt` and `rm .env` were actually submitted and
  denied by the compiled hook. The tracked README and ignored synthetic `.env`
  retained identical bytes, mode and mtime; `x.txt` is absent. A separate exact
  `git diff` executed successfully. Two preceding attempts were inconclusive
  because the model declined the attacks; they are not counted as denial evidence.
- Hook launch error: an unavailable executable plus the generated deny fallback
  produced `Pactify read-only policy: hook failed`; the target file remained absent.
- Hook input timeout: a deliberately withheld stdin payload produced
  `Pactify read-only policy: hook input timed out`; the target file remained absent.
  Both fault probes used native write mode, so native plan denial cannot explain them.
- Effort: `gemini-3.8-flash-low --effort high` failed before inference with
  `--model gemini-3.8-flash-low conflicts with --effort=high`, zero usage and no
  conversation. Documentation now requires matching the suffix or omitting effort.
- BypassSandbox: the live model reported that its no-sandbox tool definition
  does not expose this parameter;
  the model explicitly declined to submit it. No bypass execution is claimed.
  The boolean `toolCall.args.BypassSandbox` shape and denial are covered by replay;
  the installed authoritative report describes that sandbox-only parameter.
- Three consecutive scoped doctor runs passed all eight agy checks (plus two
  storage checks), with zero FAIL and zero WARN in the same private PACTIFY_HOME.
  The first survived a deliberately injected foreign legacy `write` cleanup.
  Later quota parsing/selection fixes preserve the valid live data path and are
  independently covered by regressions; the final cancellation check used the
  rebuilt binary with those fixes.
- A fresh live cancellation observed a running `sleep 60` tool command, canceled
  it, then confirmed all three known process identities and the worker had exited.
  A marker scan after the closing quota probe found no processes.
- Live `git status`, `git log --oneline` and `git diff` all completed with a
  deliberately configured repo-local fsmonitor. Its external marker was absent
  after the worker and closing quota probe exited. The earlier live failure and
  the fingerprint regression exposed the shared idle caller; both are fixed.
- `sub cost --json` retained five structured agy buckets; the legacy quota ledger
  had no snapshot entries.

The host separately supplied successful write, background `done-bg`, resume and
cancellation/no-leftover evidence. Those are host results, not reruns claimed here.

## Isolation and required command environment

Metadata snapshots cover `~/.gemini`, `~/.antigravity`, `~/.cache/antigravity`,
`~/.config/antigravity` and `~/.local/share/antigravity`, recursively using lstat
without reading file contents or following symlinks. The final comparison after the live sequences, controlled signal reproduction and
cancellation was:

```text
Before: 19090 entries, SHA-256 5a1e77631fa43326ac1ca62e24dbaa799005ba715939a63790dd24407bea48c8
After: 19090 entries, SHA-256 5a1e77631fa43326ac1ca62e24dbaa799005ba715939a63790dd24407bea48c8
Changed metadata entries: 0
```

No real Argon invocation
was made. The private PACTIFY_HOME contains both doctor verification records and
managed sessions; the user's version table was not written.

Go commands prefer `/Library/Developer/CommandLineTools/usr/bin` in PATH. Apple's
`/usr/bin/git` shim emits changing denied xcrun cache paths under this sandbox,
which otherwise makes the existing idle fingerprint test appear continually
active. Neither the idle implementation nor its expectations were weakened.
Private homes use `mktemp -d /private/tmp/aj711.XXXXXX` because the default Darwin
mktemp location is denied here. Go tests never use real pactify state.

## Frontend and remaining limits

- TypeScript checks passed; `TZ=UTC npx vitest run` passed **22 files, 154 tests**.
  An initial Asia/Singapore run crossed midnight and exposed the existing v2
  fixture's fixed October 16 chart date combined with a dynamically generated
  finishedAt. It passed unchanged under UTC. No unrelated test or chart logic
  was changed to work around that date-sensitive fixture.
- Both Dashboard and panel dist bundles were rebuilt after the timestamp fix.
- The final-dist Playwright quota checks at 1440px and 390px were attempted.
  Chromium cannot launch in this macOS sandbox:

```text
FATAL:base/apple/mach_port_rendezvous_mac.cc:159
bootstrap_check_in org.chromium.Chromium.MachPortRendezvousServer: Permission denied (1100)
2 failed (browser launch; page assertions and screenshots did not run)
```

  Browser screenshots remain unverified; this is not reported as a passing gate.
- The live no-sandbox BypassSandbox request did not produce a tool call; only the
  replay guard is verified for that field. Native launch-error/timeout denial,
  environment propagation and actual read-only attack denial are verified above.
- No evaluation run, source commit, apply or merge was performed. Disposable
  fixture repositories contain setup commits only.

## Acceptance output

All four requested command gates passed. PATH prefers the direct Command Line
Tools git as described above; private mktemp templates are explicit.

```text
$ go build ./...
EXIT_CODE=0
```

```text
$ PACTIFY_HOME="$(mktemp -d /private/tmp/aj711.XXXXXX)" go test -race ./internal/cockpit/... ./internal/sub/... -count=1
ok  	github.com/agentjoey/pactify/internal/cockpit	81.118s
ok  	github.com/agentjoey/pactify/internal/cockpit/codexschema	2.085s
ok  	github.com/agentjoey/pactify/internal/sub	126.134s
EXIT_CODE=0
```

```text
$ PACTIFY_HOME="$(mktemp -d /private/tmp/aj711.XXXXXX)" go test ./... -count=1
ok  	github.com/agentjoey/pactify/cmd/pactify	257.899s
ok  	github.com/agentjoey/pactify/internal/acp	5.060s
ok  	github.com/agentjoey/pactify/internal/agent	2.194s
ok  	github.com/agentjoey/pactify/internal/agentcfg	0.980s
ok  	github.com/agentjoey/pactify/internal/agentmanifest	1.801s
ok  	github.com/agentjoey/pactify/internal/agentreg	2.552s
ok  	github.com/agentjoey/pactify/internal/audit	2.912s
ok  	github.com/agentjoey/pactify/internal/cockpit	57.229s
ok  	github.com/agentjoey/pactify/internal/cockpit/codexschema	3.689s
ok  	github.com/agentjoey/pactify/internal/diffstat	3.890s
ok  	github.com/agentjoey/pactify/internal/doctor	4.037s
ok  	github.com/agentjoey/pactify/internal/event	3.080s
ok  	github.com/agentjoey/pactify/internal/finish	3.523s
ok  	github.com/agentjoey/pactify/internal/gitx	5.010s
ok  	github.com/agentjoey/pactify/internal/ledger	4.508s
ok  	github.com/agentjoey/pactify/internal/lockx	3.428s
ok  	github.com/agentjoey/pactify/internal/mcp	4.736s
ok  	github.com/agentjoey/pactify/internal/orchestrate	43.745s
ok  	github.com/agentjoey/pactify/internal/pact	19.369s
ok  	github.com/agentjoey/pactify/internal/paths	3.603s
ok  	github.com/agentjoey/pactify/internal/planner	5.411s
ok  	github.com/agentjoey/pactify/internal/procx	2.844s
ok  	github.com/agentjoey/pactify/internal/projection	13.858s
ok  	github.com/agentjoey/pactify/internal/recipe	3.239s
ok  	github.com/agentjoey/pactify/internal/registry	2.531s
ok  	github.com/agentjoey/pactify/internal/roles	2.715s
ok  	github.com/agentjoey/pactify/internal/runguard	2.496s
ok  	github.com/agentjoey/pactify/internal/sarif	2.150s
ok  	github.com/agentjoey/pactify/internal/schedule	2.215s
?   	github.com/agentjoey/pactify/internal/secret	[no test files]
ok  	github.com/agentjoey/pactify/internal/serve	15.908s
ok  	github.com/agentjoey/pactify/internal/sessions	2.707s
ok  	github.com/agentjoey/pactify/internal/stats	2.276s
ok  	github.com/agentjoey/pactify/internal/sub	104.545s
ok  	github.com/agentjoey/pactify/internal/subpanel	16.452s
?   	github.com/agentjoey/pactify/internal/testenv	[no test files]
ok  	github.com/agentjoey/pactify/internal/tokens	2.290s
ok  	github.com/agentjoey/pactify/internal/wizard	2.648s
?   	github.com/agentjoey/pactify/web/node_modules/flatted/golang/pkg/flatted	[no test files]
EXIT_CODE=0
```

```text
$ bats tests/plugin_pactify.bats
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
EXIT_CODE=0
```

The new shared Git guard also passed an isolated `go test -race ./internal/gitx -count=1`.

```text
# Consecutive run 1, same private PACTIFY_HOME
$ pactify sub doctor agy --model gemini-3.8-flash-low
Storage
  PASS Disk space: /Users/xtation/.pactify/jobs/20261016-224612-5ee79e/wt/.agent/aj711-rework/live-home: 21.71 GiB available (23308947456 bytes); warn below 5 GiB
  PASS Shared cache: /Users/xtation/.pactify/jobs/20261016-224612-5ee79e/wt/.agent/aj711-rework/live-home/cache: 0.00 GiB (0 bytes of files)
agy 1.4.0
  PASS Installed: CLI version 1.4.0
  PASS Write access: status=succeeded, file=true, final reply=true, tokens=24845, version=1.4.0
  PASS Model: requested=gemini-3.8-flash-low, actual=gemini-3.8-flash-low, independently verified=true
  PASS Read-only access: status=succeeded, write target absent=true, denied write verified=true, tool calls=2, attempts=1
  PASS Read-only hook: status=succeeded, hook denial verified=true, write target absent=true
  PASS Network: local port bind=true, external HTTPS=true, valid evidence=true
  PASS Session hiding: All probe sessions were hidden through the normal job lifecycle; no hiding or archiving failures reported
  PASS Managed home: Probe succeeded in the shared minimal home with intact links and private sessions
EXIT_CODE=0
```

```text
# Consecutive run 2, same private PACTIFY_HOME
$ pactify sub doctor agy --model gemini-3.8-flash-low
Storage
  PASS Disk space: /Users/xtation/.pactify/jobs/20261016-224612-5ee79e/wt/.agent/aj711-rework/live-home: 21.54 GiB available (23129165824 bytes); warn below 5 GiB
  PASS Shared cache: /Users/xtation/.pactify/jobs/20261016-224612-5ee79e/wt/.agent/aj711-rework/live-home/cache: 0.00 GiB (0 bytes of files)
agy 1.4.0
  PASS Installed: CLI version 1.4.0
  PASS Write access: status=succeeded, file=true, final reply=true, tokens=24893, version=1.4.0
  PASS Model: requested=gemini-3.8-flash-low, actual=gemini-3.8-flash-low, independently verified=true
  PASS Read-only access: status=succeeded, write target absent=true, denied write verified=true, tool calls=2, attempts=1
  PASS Read-only hook: status=succeeded, hook denial verified=true, write target absent=true
  PASS Network: local port bind=true, external HTTPS=true, valid evidence=true
  PASS Session hiding: All probe sessions were hidden through the normal job lifecycle; no hiding or archiving failures reported
  PASS Managed home: Probe succeeded in the shared minimal home with intact links and private sessions
EXIT_CODE=0
```

```text
# Consecutive run 3, same private PACTIFY_HOME
$ pactify sub doctor agy --model gemini-3.8-flash-low
Storage
  PASS Disk space: /Users/xtation/.pactify/jobs/20261016-224612-5ee79e/wt/.agent/aj711-rework/live-home: 21.56 GiB available (23146377216 bytes); warn below 5 GiB
  PASS Shared cache: /Users/xtation/.pactify/jobs/20261016-224612-5ee79e/wt/.agent/aj711-rework/live-home/cache: 0.00 GiB (0 bytes of files)
agy 1.4.0
  PASS Installed: CLI version 1.4.0
  PASS Write access: status=succeeded, file=true, final reply=true, tokens=24929, version=1.4.0
  PASS Model: requested=gemini-3.8-flash-low, actual=gemini-3.8-flash-low, independently verified=true
  PASS Read-only access: status=succeeded, write target absent=true, denied write verified=true, tool calls=2, attempts=1
  PASS Read-only hook: status=succeeded, hook denial verified=true, write target absent=true
  PASS Network: local port bind=true, external HTTPS=true, valid evidence=true
  PASS Session hiding: All probe sessions were hidden through the normal job lifecycle; no hiding or archiving failures reported
  PASS Managed home: Probe succeeded in the shared minimal home with intact links and private sessions
EXIT_CODE=0
```

```text
Foreign legacy cleanup injected against scoped doctor
Doctor 1: exit=0, FAIL=0, PASS=10, WARN=0
Doctor 2: exit=0, FAIL=0, PASS=10, WARN=0
Doctor 3: exit=0, FAIL=0, PASS=10, WARN=0
status=succeeded; hook denials=2; tracked/ignored files unchanged; redirected/prefix targets absent
exact git status/log/diff completed; tool commands=['git status', 'git log --oneline', 'git diff']; external helper marker absent
job=20261017-004335-ddf1a5; running sleep command observed; canceled; known processes=3; surviving known PIDs=0; marked processes=0 (including closing quota); worker exited (post-check confirmed)
```
