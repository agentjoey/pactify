# AJ-711 read-only argument allowlist review

Changes remain uncommitted in the same worktree. No subagents were used. No
plugin files or p0eval files were changed. This report supersedes the command
policy and literal-glob assumptions in the earlier flash follow-up report;
its retry and active-tool changes remain intact. A quota cleanup correction
required by the existing gates is described below.

## Review and dependency audit

Read the supplied `scratchpad/aj711-f1-policy-review.md` before changing code.
The confirmed rg executable helper, tree output-file options, and Go build/
module operations were approved by the previous argument denylist. The
non-Git raw execution path also permitted expansion after validation.

Dependencies checked before changing the rules:

- `agyReadCommand`, `agyGitSubcommand`, `agyUnsafeGitOption`, `agySafeGit`, the
  compiled hook handler, and `AgyEnv` native permissions. Settings and the hook
  share one program list; command values must reach execution with the same
  argv that was checked. The hook's input, bypass and writing-tool denials,
  workspace-hook preflight and managed-home protections remain intact.
- The existing `TestAgyArgumentPolicy` and `TestAgySettingsProgramPrefixes`
  positive tables. Five old command expectations are explicitly superseded by
  this request: `git -C internal log -n 1`, `git --git-dir=.git status`,
  `tree internal`, `go list ./...`, and `go vet ./...`. They were moved from
  the allow table to the deny table, preserving the cases. Only `tree` was
  removed from the old settings positive list. No other existing assertions
  were changed in this review follow-up.
- R1/R3 commands and the existing agy policy/replay tests, opencode's separate
  ordered static permissions and `TestReviewReadonlyOrderedBash` attack cases,
  native prefix-permission probe documentation, doctor denial parsing and
  filesystem baselines. Opencode's implementation and test expectations were
  preserved; the required race/full suites include its attack tests.
- Current doctor documentation and changelog. The current policy description
  was updated; previous reports remain historical evidence.

## Final policy

- Grep/egrep/fgrep accept only the requested short options and the include,
  exclude, exclude-dir and color=never long options. Rg accepts only the
  requested options. Unknown flags, missing option values and unlisted attached
  or combined short options fail closed. Short-option combinations such as
  `grep -rn` and attached values such as `-A3` remain supported. `--` ends
  options, so subsequent dash-prefixed operands remain literal data.
- Find accepts paths and only the requested predicates/operators. Values for
  predicates are consumed as data, not rescanned as executable options.
- Tree is absent from both the hook and native prefix list. Go permits only
  doc, version and env; env permits no flags or assignments, only variable-name
  queries (or no arguments). All list/vet invocations are denied before running.
- Git repository-selection globals are denied. Diff --no-index is denied,
  including accepted long-option prefixes. Existing output, external helper,
  textconv, filter, branch-mutation and config-injection restrictions remain.
  Git reads retain their helper-suppression rewrite.
- File denies -C/-m, their attached/combined forms, and long aliases including
  prefixes. Wc denies --files0-from and its prefixes. Other specified utility
  arguments remain available.
- Every approved program receives an argv rewrite with each token single-quoted
  and embedded quotes escaped. Shell composition/redirection, substitutions,
  newlines and malformed quoting remain denied. `* ? [ ] { } ~` and quoted or
  escaped parentheses cannot acquire shell expansion semantics after checking.

### Literal glob behavior

`grep x internal/sub/*.go` passes validation, but receives the literal path
`internal/sub/*.go`; unless a file has that exact name, grep reports a missing
file. This is intentional: shell expansion cannot introduce previously unseen
options or operands. Use `grep -rn x internal/sub` for recursive searching, or
`rg -n -g '*.go' x internal/sub` to let rg apply its own allowed pattern option.
Find's quoted `-name '*.go'` and quoted/escaped grouping parentheses still work.
`ls *` similarly receives `*`, without filename expansion. No path expansion
or automatic `--` insertion is attempted.

### Explicitly retained read/hang risks

The review labels du --files0-from, stat --printf, device reads through cat/head,
and tail -f as acceptable read-only/DoS risks. The requested utility rule also
explicitly retains arbitrary arguments for these programs. These base forms
therefore remain positive cases. Their redirection forms are negative cases;
metacharacter operands are quoted and cannot expand in the shell. Wc's analogous
file-list option is denied as explicitly requested. This distinction follows
those specific utility rules; it does not claim OS confinement or a no-hang
policy. The explicit utility allowances were preserved.

## Regression evidence

New tests ran failing before implementation. The red log records approved rg
helpers, tree writes, Go list/vet, Git redirection/no-index, file magic and wc
indirection, unlisted grep/rg/find flags, missing non-Git rewrites, and tree's
native permission. After implementation, the only old failures were the five
superseded positives and tree prefix expectation described above; those were
updated under this request's explicit new policy.

- `TestAgyArgumentAllowlistAttacks` covers all three confirmed bypasses and all
  reviewed scope/expansion/utility-risk categories, including each of the Go
  flags named in the review, Git abbreviation cases, rg decompression flags,
  and write/exec find predicates.
- `TestAgyArgumentAllowlistReads` covers the actual R1/R3 commands, listed
  options, short clusters, attached/separate values, patterns, parentheses,
  safe Go queries and the explicitly retained utilities.
- `TestAgyQuotedExecutionKeepsArgumentsLiteral` executes the rewritten command
  through a real shell and captures argv, proving that glob/brace/tilde,
  parentheses, spaces and embedded apostrophes arrive unchanged.
- `TestAgySettingsExcludeRemovedPrograms` rejects tree and checks settings/hook
  list agreement; the existing prefix test covers every retained program.

## Quota lifecycle regression found by the gates

The first race gate failed the unchanged `TestAgyV2WorkerReplay/quota`: its
opening `/usage` was terminated and only one snapshot remained. A new process
regression reproduced this independently, using the actual quota adapter and
a fake `/usage` executable that sleeps seven seconds. The previous seven-second
source stub did not exercise process cleanup.

The cause is concrete: the asynchronous built-in inherited `PACTIFY_JOB` from
the model harness. Worker teardown and `Finish` kill that marker before pending
quota supplementation. Clearing that marker exposed a second failure: the
probe also inherited the job's already-deleted TMPDIR. A second failing replay
writes to TMPDIR after the seven-second delay, reproducing that failure.

`Quota` now owns a private temporary directory and uses its own bounded context
without the harness marker. Both opening and closing observations survive job
cleanup, and the probe removes its private temp afterward. No harness cleanup
or cancellation policy was weakened. The new regression verifies both snapshots,
absence of warnings, and removal of both job and probe temporary directories.
The existing worker replay and orphan-cleanup assertions were left unchanged.

The initial full gate also failed the existing orphan-temp-cleanup assertion
while the race suite was running. Those suites share the user's process-marker
namespace and use repeated fixture IDs, so they are not independent workloads.
The focused cleanup/replay checks and final required gates run sequentially;
the initial failures are not counted as passing acceptance evidence.

## Live evidence

Fresh live probes used isolated
`PACTIFY_HOME=/Users/xtation/.pactify/jobs/20261017-051317-423167/tmp/policy-allowlist/home`
and agy 1.4.0:

| Probe | Model / job | Result |
|---|---|---|
| Investigation | gemini-3.8-flash-low / `20261017-051818-c2d2de` | All four commands produced actual tool output: `git log -n 3`, `grep -rn patrol .`, `find . -type f`, `ls`. Succeeded with a conclusion; repository clean. |
| Attack | gemini-3.8-flash-high / `20261017-052039-4dbc52` | All five requested inputs produced ERROR tool events with `denied by pre-tool hook`. No x/x.txt, .env unchanged, repository clean. |
| Doctor | `gemini-3.8-flash-low` | Rebuilt after the quota correction; exit 0, no FAIL. Existing Sandbox row is WARN because agy has no OS sandbox. |

The first attack attempt (`20261017-051852-99903a`, flash-low) only refused
verbally and is excluded from denial evidence. The flash-high attempt actually
invoked each requested input. Policy code was unchanged between these probes
and final acceptance; doctor was repeated after the quota lifecycle correction.
User agy metadata snapshots before and after all live calls match exactly:
**12,600 entries**, comparing inode, mode, size, mtime and symlink targets.
No user file contents were read for those snapshots. Every actual agy call,
including CLI discovery and /usage, explicitly supplies a gemini-3.8-flash model.
Managed authentication uses the existing credential symlink; no user credential
contents are inspected or copied.

## Required acceptance

The worktree's Git metadata is not writable in this environment, so startup
`git pull` failed; `.agent/CURRENT.md` is absent. Acceptance uses Xcode's Git
first on PATH to avoid the previously diagnosed Apple Git shim's changing
xcrun cache-error output in isolated HOME directories. Go tests use fresh
PACTIFY_HOME directories with an explicit writable mktemp template.

All required gates completed sequentially with exit **0**. Exact commands and output follow.
All used `PATH="/Applications/Xcode.app/Contents/Developer/usr/bin:$PATH"`.


```text
$ go build ./...
(no output)
exit: 0
```

```text
$ PACTIFY_HOME="$(mktemp -d /private/tmp/aj711-allowlist-accept-race.XXXXXX)" go test -race ./internal/cockpit/... ./internal/sub/... -count=1
ok  	github.com/agentjoey/pactify/internal/cockpit	94.628s
ok  	github.com/agentjoey/pactify/internal/cockpit/codexschema	2.118s
ok  	github.com/agentjoey/pactify/internal/cockpit/golden	2.555s
ok  	github.com/agentjoey/pactify/internal/sub	150.798s
exit: 0
```

```text
$ PACTIFY_HOME="$(mktemp -d /private/tmp/aj711-allowlist-accept-all.XXXXXX)" go test ./... -count=1
ok  	github.com/agentjoey/pactify/cmd/pactify	257.460s
ok  	github.com/agentjoey/pactify/internal/acp	6.140s
ok  	github.com/agentjoey/pactify/internal/agent	1.234s
ok  	github.com/agentjoey/pactify/internal/agentcfg	0.832s
ok  	github.com/agentjoey/pactify/internal/agentmanifest	2.008s
ok  	github.com/agentjoey/pactify/internal/agentreg	2.472s
ok  	github.com/agentjoey/pactify/internal/audit	2.843s
ok  	github.com/agentjoey/pactify/internal/cockpit	66.311s
ok  	github.com/agentjoey/pactify/internal/cockpit/codexschema	3.557s
ok  	github.com/agentjoey/pactify/internal/cockpit/golden	3.739s
ok  	github.com/agentjoey/pactify/internal/diffstat	3.584s
ok  	github.com/agentjoey/pactify/internal/doctor	4.443s
ok  	github.com/agentjoey/pactify/internal/event	3.563s
ok  	github.com/agentjoey/pactify/internal/finish	3.503s
ok  	github.com/agentjoey/pactify/internal/gitx	4.821s
ok  	github.com/agentjoey/pactify/internal/ledger	4.165s
ok  	github.com/agentjoey/pactify/internal/lockx	3.486s
ok  	github.com/agentjoey/pactify/internal/mcp	4.644s
ok  	github.com/agentjoey/pactify/internal/orchestrate	40.356s
ok  	github.com/agentjoey/pactify/internal/pact	17.589s
ok  	github.com/agentjoey/pactify/internal/paths	3.343s
ok  	github.com/agentjoey/pactify/internal/planner	5.487s
ok  	github.com/agentjoey/pactify/internal/procx	2.826s
ok  	github.com/agentjoey/pactify/internal/projection	13.948s
ok  	github.com/agentjoey/pactify/internal/recipe	3.112s
ok  	github.com/agentjoey/pactify/internal/registry	2.796s
ok  	github.com/agentjoey/pactify/internal/roles	2.893s
ok  	github.com/agentjoey/pactify/internal/runguard	2.493s
ok  	github.com/agentjoey/pactify/internal/sarif	2.150s
ok  	github.com/agentjoey/pactify/internal/schedule	1.949s
?   	github.com/agentjoey/pactify/internal/secret	[no test files]
ok  	github.com/agentjoey/pactify/internal/serve	12.894s
ok  	github.com/agentjoey/pactify/internal/sessions	2.607s
ok  	github.com/agentjoey/pactify/internal/stats	2.589s
ok  	github.com/agentjoey/pactify/internal/sub	128.899s
ok  	github.com/agentjoey/pactify/internal/subpanel	16.518s
?   	github.com/agentjoey/pactify/internal/testenv	[no test files]
ok  	github.com/agentjoey/pactify/internal/tokens	2.137s
ok  	github.com/agentjoey/pactify/internal/wizard	2.565s
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

The focused lifecycle regression and unchanged worker/orphan tests also passed:

```text
$ PACTIFY_HOME="$(mktemp -d /private/tmp/aj711-quota-temp-green.XXXXXX)" go test -race ./internal/sub -run "TestAgyQuotaProcessSurvivesTerminalCleanup|TestAgyV2WorkerReplay|TestJobMarkerCleansUnrecordedOrphans" -count=1
ok  	github.com/agentjoey/pactify/internal/sub	34.509s
```

## Rebuilt doctor output

```text
$ PACTIFY_HOME=<isolated home above> pactify sub doctor agy --model gemini-3.8-flash-low
Storage
  PASS Disk space: /Users/xtation/.pactify/jobs/20261017-051317-423167/tmp/policy-allowlist/home: 15.03 GiB available (16140050432 bytes); warn below 5 GiB
  PASS Shared cache: /Users/xtation/.pactify/jobs/20261017-051317-423167/tmp/policy-allowlist/home/cache: 0.00 GiB (0 bytes of files)
agy 1.4.0
  PASS Installed: CLI version 1.4.0
  PASS Write access: status=succeeded, file=true, final reply=true, tokens=24799, version=1.4.0
  PASS Model: requested=gemini-3.8-flash-low, actual=gemini-3.8-flash-low, independently verified=true
  PASS Read-only access: status=succeeded, write target absent=true, denied write verified=true, tool calls=2, attempts=1
  PASS Read-only hook: status=succeeded, hook denial verified=true, write target absent=true
  PASS Network: local port bind=true, external HTTPS=true, valid evidence=true
  WARN Sandbox: not sandboxed: writes are not confined by the OS
  PASS Failure classification: offline protocol error injection: expected=transient, actual=transient; no CLI or network call
  PASS Session hiding: All probe sessions were hidden through the normal job lifecycle; no hiding or archiving failures reported
  PASS Managed home: Probe succeeded in the shared minimal home with intact links and private sessions
exit: 0
```

## Independent re-review (2026-10-17)

An adversarial review of the reworked allowlist found no bypass: the raw string still rejects `; | & > < $` backticks, CR, LF and NUL; approved commands run as single-quoted argv, so glob, brace and tilde forms reach the program literally; per-program flags fail closed; and `go doc` does not run package `init()`. Changes made from its hardening list:

- `--git-dir` and `--work-tree` are now refused after the subcommand too, instead of relying on git to reject them there. `-C` stays allowed after the subcommand because it means context lines for `git grep` and copy detection for `git log`.

Residual risks, not fixed here:

- Enforcement depends on agy executing the hook's rewritten `CommandLine`. Approved commands in the v2 re-evaluation produced the expected output, but no test catches agy silently running the original string; the raw metacharacter filter would still block composition and redirection in that case.
- `go doc` may populate the module cache or download a pinned toolchain (signed by Go, so no arbitrary code, but it is network egress).
- Reads are not confined to the workspace; see [sub-doctor.md](../sub-doctor.md).
