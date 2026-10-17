# AJ-711 flash reevaluation follow-up

Changes remain uncommitted in the supplied worktree. No subagents were used.
Plugin files and version are unchanged. No p0eval files were modified. User-facing
messages added by this change are English.

## Evidence and dependency audit

Before changing the rules, inspected:

- `cockpit/agy_policy.go`, `opencode_permissions.go` and the read-only tests:
  agy depended on opencode's exact command list. Opencode uses ordered static
  patterns with deny rules last; it now remains independent and unchanged.
  `TestReviewReadonlyOrderedBash` retains all ten existing shell/output/find/preprocessor
  attack cases and runs in both required Go gates.
- The native permissions probe in `docs/harness-v2/probe-report.md`: settings
  use `command(program)` prefixes. The hook still owns argument validation;
  bypass requests, malformed input, unknown access values and writing tools
  retain their existing fail-closed behavior. Workspace hooks and user-config
  read-only refusals, filesystem baselines, doctor denied-write parsing,
  and Git helper suppression remain in force.
- `sub/worker.go`, quota ledgers/router/cost readers, final result publication
  and quota review tests: terminal status precedes closing metadata updates;
  snapshot errors remain informational and must not hide actual job failures.
  The legacy exhausted-route ledger remains separate from snapshot observations.
- `cockpit/agy.go`, `sub/retry.go`, collectors and AJ-693 tests: only terminal
  failures enter the existing bounded same-session retry loop; cancellation,
  model mismatch, quota and retry limits retain their existing priorities.
- Codex `item/started`, `item/completed` and output-delta dispatch, the shared
  worker, and cockpit manager/session idle tracking. The job worker resets its
  clock on protocol events or tree changes. The cockpit manager has a separate
  30-minute session watchdog. The new active-tool budget belongs to the shared
  job worker and uses the same normalized start/end semantics across adapters.
- Current docs contain historical exact-list and five-second-budget decisions.
  The new changelog entry and current doctor documentation supersede them;
  historical acceptance reports were preserved.

T07's supplied `log.jsonl` ends with a normalized `turn_failed` carrying the
malformed-call diagnostic, after a final summary. There is no raw result event
in that recording. Inspection of the adapter shows this exact terminal error
is copied from `result.error` when `result.status != SUCCESS`; treating the
upstream result as failed is therefore an implementation-backed inference,
not a directly recorded raw status. The fix classifies this terminal diagnostic
as transient; intermediate occurrences do not end a turn. New terminal events
retain the raw result so future evidence includes its event and status.

T02's final ACTIVE tool in the supplied log is a recursive Python glob. The
preceding `go test ... TestMergeHardGate` command had already returned. The
requested longer ACTIVE-tool limit applies to that situation as well.

## Implementation and regression coverage

- Parse agy read commands with `go-shellwords`, without environment expansion
  or execution. Reject shell control/redirection, newlines and dollar expansion;
  permit correctly parsed globs. Check each allowed program's arguments.
  Native settings grant prefixes for exactly the same program list.
- Deny Git output/executable options (including long-option abbreviations),
  branch mutation, config injection and cat-file filters. Preserve safe global
  options such as `--no-pager` and `-C`. Rewrite approved Git invocations with
  quoted arguments and disabled fsmonitor, hooks, pager and applicable diff/
  textconv helpers. Go env persistence and executable tool overrides are denied;
  ordinary `go doc -u` remains allowed. Go checks cover both `-flag` and `--flag` spellings.
- Start a 20-second opening quota probe concurrently with harness startup.
  Merge its result on the worker goroutine through a buffered channel. Publish
  terminal state before pending quota supplementation, with a 20-second closing
  budget. Keep warnings, exhausted-route reset recovery and atomic publication.
- Classify the observed terminal malformed-function-call message as transient.
  The worker reuses the same session and AJ-693 retry bounds. Replay tests cover
  both an intermediate diagnostic and a failed raw result followed by a
  successful continuation, plus the recorded normalized T07 terminal sequence.
- Count active tool starts/ends, including overlap; reset tracking on terminal
  turns. Use 30 minutes while active and the existing 10-minute limit otherwise.
  Configure with a positive `PACTIFY_SUB_ACTIVE_TOOL_IDLE_TIMEOUT` duration or
  `WorkerConfig.ActiveToolIdleTimeout`. Events and tree changes still reset idle.

New regressions were run failing before their corresponding implementation:
argument-policy positives and attacks, native prefix settings, the seven-second
opening quota source, terminal T07 replay, active-tool survival, configurable
active timeout, raw terminal evidence, safe global Git options, and boundary
cases for Go doc/Git output/filter options. Existing cancellation/idle tests
and opencode attack tests are retained.

The only changed existing assertion is `reviewQuotaBackend.Quota` in
`internal/sub/agy_review_test.go`: the obsolete opening deadline of at most
5.1 seconds now requires approximately 20 seconds. This is explicitly
superseded by the requested 20-second behavior. It is shared by
`TestAgyReviewQuotaWarningsAndPublication` and
`TestAgyReviewClosingQuotaHasTimeAfterPublication`. No existing test was removed
or weakened to accommodate unrelated behavior.

## Flash live verification

All CLI paths specify a flash model, including version/model discovery and
`/usage`. Live jobs used isolated `PACTIFY_HOME=/private/tmp/aj711-live-final-044134/home`,
agy 1.4.0 and `gemini-3.8-flash-low`:

| Probe | Job | Evidence |
|---|---|---|
| Investigation | `20261017-045436-c13933` | All four exact commands (`git log -n 3`, `grep -rn patrol .`, `find . -type f`, `ls`) produced tool output; final conclusion and succeeded result; clean repository afterward. |
| Attack | `20261017-045718-5d93fa` | All three requested commands were actually invoked and returned hook-denial ERROR events; `.env` remained, `x.txt` absent, repository clean. |
| Write | `20261017-045521-c05dbd` | Created only `hello.txt`, verified content, succeeded; persisted result contains both opening and closing quota observations and no `signal: killed` warning. |

The investigation, attack and write probes were repeated after the core changes.
One repeat attack attempt (`20261017-045507-bbddc5`) only refused verbally and
was excluded; the table records actual tool-denial evidence from the next attempt.
The final double-dash Go flag guard was checked by regressions; these live probes
do not invoke Go flags. The two doctor runs used the earlier isolated home
`/private/tmp/aj711-live/home`; their validated probe paths were unchanged by the
subsequent Git/Go option-boundary refinements.
Both consecutive `sub doctor agy --model gemini-3.8-flash-low` calls exited 0
with no FAIL. Their existing Sandbox row is WARN because agy has no OS sandbox.
The user agy directory metadata snapshot was identical before and after all
live probes: **12,600 entries unchanged**, including inode, mode, size, mtime
and symlink targets. No user file contents or credentials were inspected;
the harness's existing managed-home credential symlink was used for login.

## Required acceptance output

The checkout is detached, so the required startup `git pull` could not select a
branch; `.agent/CURRENT.md` was absent. Initial macOS `mktemp -d` attempts selected
an unwritable system directory, so subsequent test commands use an explicit
writable template. Initial full/race runs using Apple's Git shim stalled in
existing idle tests: under a temporary HOME, `xcrun` printed a different denied
cache filename on each Git query, making the existing fingerprint appear to
change continuously. Those initial fixture jobs were canceled and those runs
are failed, not passing evidence. Final acceptance puts Xcode's actual Git first
on PATH without changing production Git behavior or existing tests.

The first race run with that environment found an overly short 40 ms interval
in the new active-tool regression under full-suite load. Only the new timing
fixtures were widened (500 ms ordinary idle, 1.2 s active wait; configurable
cases use a 1.5 s active cap). All three new idle scenarios then passed three
consecutive race repetitions. The required gates were rerun afterward.

The final required commands and their output follow. Each acceptance Go test invocation
uses a fresh `PACTIFY_HOME`; mktemp uses an explicit writable temporary path in
this restricted environment. No tests are reported passed unless they complete.

The final gates ran with `PATH="/Applications/Xcode.app/Contents/Developer/usr/bin:$PATH"`. All four exited **0**.

```text
$ go build ./...
(no output)
exit: 0
```

```text
$ PACTIFY_HOME="$(mktemp -d /private/tmp/aj711-gate-race.XXXXXX)" go test -race ./internal/cockpit/... ./internal/sub/... -count=1
ok  	github.com/agentjoey/pactify/internal/cockpit	114.806s
ok  	github.com/agentjoey/pactify/internal/cockpit/codexschema	5.179s
ok  	github.com/agentjoey/pactify/internal/cockpit/golden	5.714s
ok  	github.com/agentjoey/pactify/internal/sub	149.158s
exit: 0
```

```text
$ PACTIFY_HOME="$(mktemp -d /private/tmp/aj711-gate-all.XXXXXX)" go test ./... -count=1
ok  	github.com/agentjoey/pactify/cmd/pactify	265.058s
ok  	github.com/agentjoey/pactify/internal/acp	5.299s
ok  	github.com/agentjoey/pactify/internal/agent	2.417s
ok  	github.com/agentjoey/pactify/internal/agentcfg	1.231s
ok  	github.com/agentjoey/pactify/internal/agentmanifest	1.613s
ok  	github.com/agentjoey/pactify/internal/agentreg	2.776s
ok  	github.com/agentjoey/pactify/internal/audit	1.979s
ok  	github.com/agentjoey/pactify/internal/cockpit	70.432s
ok  	github.com/agentjoey/pactify/internal/cockpit/codexschema	3.571s
ok  	github.com/agentjoey/pactify/internal/cockpit/golden	3.569s
ok  	github.com/agentjoey/pactify/internal/diffstat	5.075s
ok  	github.com/agentjoey/pactify/internal/doctor	5.520s
ok  	github.com/agentjoey/pactify/internal/event	5.078s
ok  	github.com/agentjoey/pactify/internal/finish	5.030s
ok  	github.com/agentjoey/pactify/internal/gitx	6.434s
ok  	github.com/agentjoey/pactify/internal/ledger	5.841s
ok  	github.com/agentjoey/pactify/internal/lockx	5.142s
ok  	github.com/agentjoey/pactify/internal/mcp	5.692s
ok  	github.com/agentjoey/pactify/internal/orchestrate	43.667s
ok  	github.com/agentjoey/pactify/internal/pact	18.856s
ok  	github.com/agentjoey/pactify/internal/paths	4.072s
ok  	github.com/agentjoey/pactify/internal/planner	6.079s
ok  	github.com/agentjoey/pactify/internal/procx	3.232s
ok  	github.com/agentjoey/pactify/internal/projection	14.165s
ok  	github.com/agentjoey/pactify/internal/recipe	3.772s
ok  	github.com/agentjoey/pactify/internal/registry	3.032s
ok  	github.com/agentjoey/pactify/internal/roles	3.181s
ok  	github.com/agentjoey/pactify/internal/runguard	2.769s
ok  	github.com/agentjoey/pactify/internal/sarif	2.446s
ok  	github.com/agentjoey/pactify/internal/schedule	2.521s
?   	github.com/agentjoey/pactify/internal/secret	[no test files]
ok  	github.com/agentjoey/pactify/internal/serve	14.790s
ok  	github.com/agentjoey/pactify/internal/sessions	2.864s
ok  	github.com/agentjoey/pactify/internal/stats	2.844s
ok  	github.com/agentjoey/pactify/internal/sub	124.941s
ok  	github.com/agentjoey/pactify/internal/subpanel	17.186s
?   	github.com/agentjoey/pactify/internal/testenv	[no test files]
ok  	github.com/agentjoey/pactify/internal/tokens	2.486s
ok  	github.com/agentjoey/pactify/internal/wizard	2.850s
exit: 0
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
exit: 0
```

The focused timing stability check also passed:

```text
$ PACTIFY_HOME="$(mktemp -d /private/tmp/aj711-active-race.XXXXXX)" go test -race ./internal/sub -run TestActiveTool -count=3
ok  	github.com/agentjoey/pactify/internal/sub	15.014s
```

## Doctor output

Run 1, exit 0:

```text
$ PACTIFY_HOME=/private/tmp/aj711-live/home pactify sub doctor agy --model gemini-3.8-flash-low
Storage
  PASS Disk space: /private/tmp/aj711-live/home: 15.75 GiB available (16913534976 bytes); warn below 5 GiB
  PASS Shared cache: /private/tmp/aj711-live/home/cache: 0.00 GiB (0 bytes of files)
agy 1.4.0
  PASS Installed: CLI version 1.4.0
  PASS Write access: status=succeeded, file=true, final reply=true, tokens=24555, version=1.4.0
  PASS Model: requested=gemini-3.8-flash-low, actual=gemini-3.8-flash-low, independently verified=true
  PASS Read-only access: status=succeeded, write target absent=true, denied write verified=true, tool calls=2, attempts=1
  PASS Read-only hook: status=succeeded, hook denial verified=true, write target absent=true
  PASS Network: local port bind=true, external HTTPS=true, valid evidence=true
  WARN Sandbox: not sandboxed: writes are not confined by the OS
  PASS Failure classification: offline protocol error injection: expected=transient, actual=transient; no CLI or network call
  PASS Session hiding: All probe sessions were hidden through the normal job lifecycle; no hiding or archiving failures reported
  PASS Managed home: Probe succeeded in the shared minimal home with intact links and private sessions
```

Run 2, exit 0:

```text
$ PACTIFY_HOME=/private/tmp/aj711-live/home pactify sub doctor agy --model gemini-3.8-flash-low
Storage
  PASS Disk space: /private/tmp/aj711-live/home: 15.77 GiB available (16929439744 bytes); warn below 5 GiB
  PASS Shared cache: /private/tmp/aj711-live/home/cache: 0.00 GiB (0 bytes of files)
agy 1.4.0
  PASS Installed: CLI version 1.4.0
  PASS Write access: status=succeeded, file=true, final reply=true, tokens=24552, version=1.4.0
  PASS Model: requested=gemini-3.8-flash-low, actual=gemini-3.8-flash-low, independently verified=true
  PASS Read-only access: status=succeeded, write target absent=true, denied write verified=true, tool calls=2, attempts=1
  PASS Read-only hook: status=succeeded, hook denial verified=true, write target absent=true
  PASS Network: local port bind=true, external HTTPS=true, valid evidence=true
  WARN Sandbox: not sandboxed: writes are not confined by the OS
  PASS Failure classification: offline protocol error injection: expected=transient, actual=transient; no CLI or network call
  PASS Session hiding: All probe sessions were hidden through the normal job lifecycle; no hiding or archiving failures reported
  PASS Managed home: Probe succeeded in the shared minimal home with intact links and private sessions
```

