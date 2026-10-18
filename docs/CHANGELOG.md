# Pactify — Changelog

> 版本历史。**当前发布版见 [`CLAUDE.md`](../CLAUDE.md) 的 Version 段**；
> 本文件只追加，最新在最上。每条记录写的是「改了什么 + 为什么这么改 + 门跑了哪些」，
> 不是提交列表——提交列表用 `git log`。

> 2026-08-29 从 `CLAUDE.md` 的单行 Version 段拆出（原文逐字保留，只补了小标题与分段）。

---

## Unreleased — AJ-740 · router read-only keeps every harness that supports it

- With a router config present, `--read-only` used to keep only codex routes, so even `pactify sub run opencode --read-only` failed with `no routes support read-only access`. That rule dated from when codex was the only read-only harness (AJ-694). `Router.Select` now skips only harnesses that refuse read-only (`NoReadOnly`, currently kimi) and unknown harnesses, for both the chosen route and the remaining fallback chain.
- Tests: read-only agy is chosen and its chain keeps opencode and codex while skipping kimi; read-only opencode is chosen after kimi; kimi/unknown-only chains are still refused. Restoring the codex-only rule fails the test. Live with the real router config: `sub run opencode --read-only` (MiniMax-M3, 1.19.3) succeeds with no changes, where the previous binary refused it.
## Unreleased — AJ-739 · kimi matches 429 only as a status code

- The kimi failure classifier used to treat any "429" substring as rate limiting (transient). A missing-login error quotes its home path, so a path containing 429 (for example a random `t.TempDir()`) turned an auth failure into a retried transient. That caused about 1% of CI runs to fail in `TestManagedMissingLoginFailureIsAuth/kimi`. 429 now matches only in status context (`HTTP 429`, `status code: 429`, `code=429`, `error 429`, `429 Too Many Requests`, `429 RESOURCE_EXHAUSTED`), like the existing 5xx rule; `rate limit` / `rate_limit` / `too many requests` still match.
- Tests: new classifier cases (status forms stay transient; a login error with `…1429…` in its path stays auth; a plain path with 4290 stays other). `TestManagedMissingLoginFailureIsAuth` now always uses a home path containing 1429, so the old flake reproduces deterministically. Reverting to substring matching fails both tests.

## Unreleased — AJ-735 · remove the orchestrator

- Remove `internal/orchestrate`, `planner`, `schedule`, `audit`, `sarif`, `sessions`, `tokens`, `secret`, `recipe`, `finish`, `runguard`, `agentcfg` and `roles`, plus their exclusive tests and fixtures. Pactify's product direction is the harness router; tag **`v0.12.0` remains the rollback anchor**. There are now **23 top-level internal packages**, measured with `go list ./internal/... | sed 's#.*/internal/##' | cut -d/ -f1 | sort -u | wc -l`.
- Remove CLI commands `orchestrate`, `plan`, `run`, `schedule`, `audit` (including SARIF), `sessions`, `recipe`, `finish`, `role`, and orchestrator-only `agent config`, `config gate/critic` and `checkpoint --force`. Remove effective launch config from `agent scan`, role-binding diagnostics from doctor, and the CLI/MCP checkpoint run guard. Legacy run status files no longer block checkpoints; regression tests cover both paths. Pact protocol roles, including `join --roles`, remain intact.
- Remove serve's scheduler, project discovery and acting-seat wiring. `--seat` and repeatable `--project` remain accepted as hidden no-op compatibility flags, with the same warning behavior as retired relay flags; launchd arguments continue to start serve. Keep `/api/sub`, CSRF protection, harness code, web sources and both embedded dists. Update current docs, generated agent onboarding and stale setup hints; mark retained architecture/spec history as removed.
- `go mod tidy` succeeds without module changes. Reviewed workflows, `.goreleaser.yaml` and ignore rules; retain applicable CI/release jobs and remove the unused session `data/` ignore entry. Preserve pact/agent registry APIs and stored-data compatibility; `agentcfg` and `roles` have no remaining non-orchestrator users. `deadcode` is unavailable; reference searches and vet cover removal leftovers.
- Validation completed so far: `go build ./...`, `go vet ./...`, `go vet -tags record,harness_e2e ./...`, `go mod tidy -diff` (also offline with `GOPROXY=off GOFLAGS=-mod=mod`), changed-file `gofmt -l`, `git diff --check`, and the required removed-package Go reference search all pass. Focused CLI/MCP/doctor regression tests pass. Built-binary smoke passes for `serve --help`, `serve --addr 127.0.0.1:0 --seat claude --project /tmp` (clean SIGINT shutdown), and rejection of `orchestrate` as an unknown command.
- Bats: **136/136 pass** with isolated `HOME`/`PACTIFY_HOME`, temporary `jsonschema` dependencies via `PYTHONPATH`, and a temporary `mktemp` wrapper routing bare `mktemp -d` to writable `/private/tmp`. Initial runs failed only because `jsonschema` was absent and macOS's default temp directory is sandbox-blocked; installer cases also pass alone with the wrapper. No installer or schema test changes. The final isolated full Go suite (`go test -json ./...`) passes: 26 packages pass and two contain no tests. The initial runs caught a deleted test helper and an incorrect compatibility-warning expectation; both are fixed, the focused regression rerun passes, and the final complete rerun has no failures.
## Unreleased — AJ-734 · opencode 1.x read-only denies the shell

- opencode 1.x read-only now denies `bash` outright, in both the config (`"bash": "deny"`) and the session rules (`*` deny, read/glob/grep/list allow, `bash *` deny). The 15 exact-command allows, the dangerous-pattern denies and `orderedBashPolicy` are gone. This matches the 2.x policy since AJ-720: an exact-command whitelist depends on opencode's shell parsing, and 2.x already skips the permission check for commands it cannot parse. Write mode is unchanged.
- opencode removes a wholly denied tool from the model's toolset. A live 1.19.3 read-only job (MiniMax-M3) listed only `read`, `glob` and `grep`, read the file and left the repository unchanged. The doctor's old probe asked the model to run `echo x > probe.txt` through bash, so it could only report "inconclusive". For opencode (1.x and 2.x), doctor now reopens the probe session after the job and evaluates its own permission rules: bash/shell, edit and external_directory must be deny, and the write target must be absent. `Store.CheckOpencode2Readonly` became `Store.CheckOpencodeReadonly` and dispatches on the recorded CLI version; the new `cockpit.CheckOpencodeReadonly` reads `GET /session/{id}` and applies opencode's last-match wildcard rules. The denied-write log parser is now agy-only.
- Tests: exact session-rule and config assertions; the old ordered-whitelist regression now checks that every command, including the former allows and bypass shapes, is denied under a permissive user config; a fake-server read-back test where a write session (`bash ask`) is rejected; last-match wildcard cases; doctor tests now inject the policy check. Mutations (re-allowing `ls`; ignoring the deny result; dropping the doctor's policy gate) each fail the new tests.
- Docs: `docs/sub-doctor.md`, plugin README (the access table still said opencode read-only was not offered) and the opencode skill. Plugin 0.2.8.
- Gates (host): `go build ./...`, `go vet ./...` (default and `record,harness_e2e`), isolated `go test ./...`, bats; real `pactify sub doctor opencode --model minimax-coding-plan/MiniMax-M3` on 1.19.3: Read-only access PASS ("shell/edit/external_directory deny=true"), all other checks PASS/expected WARN/SKIP.

## Unreleased — AJ-723 · remove the legacy serve API

- Remove the unused governance HTTP routes (projects/state/worktrees/events, orchestrate/fallback approval, cockpit, registry/author verbs, agents/sessions, plan/setup, filesystem browsing, wiring/seats, stats, recipes, manifests and audit). Web v2 consumes `/api/sub`; unknown API routes now return 404 instead of the SPA index.
- Delete the route-only handlers, watchers, SSE hub, DTOs, fixtures (including the former cloud TypeScript goldens), tests and three legacy serve Bats suites. Remove `internal/stats`, `internal/diffstat`, the unused cockpit Manager, Claude backend/SDK bridge and doctor's `--setup-bridge` flag/checks. Keep the fake backend/session because retained cockpit tests use them. There are now **35 top-level internal packages** (down from 37; `go list ./internal/...` lists 39 including nested packages).
- Preserve `/api/sub` reads/writes and job-events SSE, embedded static assets and the CSRF Origin guard. CSRF tests now exercise real router writes and verify disk effects. Remove only `sec_m3_plan_traversal_test.go`: its feature-path traversal regression guarded the deleted plan-review file reader; the removed route (including an encoded traversal) is now covered by a 404 regression.
- Preserve `StartScheduler` through the minimal schedule → orchestrate path in `scheduler.go`: startup project discovery, derived roster hints, serial conflict guards, child logs and reaping. `--seat` (default `$PACT_AGENT_ID`) and repeatable `--project` keep configuring the scheduler's acting seat and extra projects — the local launchd plist passes `--seat claude` — and are no longer described as serve-API flags; `TestScheduleSpawnUsesSetSeat` and the serve startup test cover them. No orchestrator engine or ACP backend changes.
- `GOPROXY=off GOSUMDB=off go mod tidy` succeeds without downloads or dependency changes; fsnotify remains required by MCP.
- Gates passed: `go build ./...`; `gofmt -l` on changed Go files (empty); `go vet ./...`; `go vet -tags record,harness_e2e ./...`; isolated `go test ./...`; isolated `go test -race ./internal/serve/... ./internal/sub/...`; and `git diff --check`. The first race run also hit the unchanged `TestAgyQuotaProcessSurvivesTerminalCleanup` fake-process startup deadline; the complete rerun passed without changing sub. The initial new roster-hint test used an ambiguous seat name; its corrected fixture passes in both final suites.
- Real-server smoke passed with isolated homes and `--seat claude`: `/api/sub/jobs` returned 200 without additional headers, removed `/api/projects` returned 404, and SIGINT shut the server down cleanly. Startup tests cover repeatable `--project` (it adds scheduler projects) and assert neither flag is reported as ignored.
- **Host re-verification**: isolated `go test ./...` passes except `TestSubAgyReadOnlyRestored`, which timed out under load in the full run and passes alone 2/2; race tests pass; bats **139/139** with Python `jsonschema` available. A serve built from this branch with the plist arguments (`--seat claude`), real job data and a copied router served Dashboard, Jobs and Settings with no console errors; `/api/sub/{jobs,harnesses,router,cost}` 200, `/api/{projects,registry,agents}` 404.

## Unreleased — AJ-722 · dead code and stale artifacts

- Remove 17 obsolete screenshot scripts, the gallery entry and its exclusive UI/icon helpers, test-only components and their tests, unused styles/exports, Phosphor, and Vite template assets. Web v2 now has one shared Dashboard / Jobs / Settings shell with embedded Live.
- Remove unreachable `gitx.Merge`, `Store.FinalJobSnapshot`, `Store.LastSessionFor`, `Store.ResumeJob` and `prepareKimiHome`, confirmed by reference searches and deadcode with `record,harness_e2e,agye2e,acpsmoke`. Keep `Store.Actions` and `Store.Summarize` for now: serve tests still call them. Keep the live-tree/statistics helpers private because production still calls them. Doctor and worktree action tests now call the underlying active APIs.
- Delete showcase seeds, tracked Python bytecode and the obsolete release hook; ignore Python caches, drop the Rust ignore rule, align four-harness plugin copy and bump the plugin to 0.2.7. Refresh Jobs, frontend, archive and screenshot documentation.
- **Gates (host)**: `go build ./...`, `go vet ./...` (default and `record,harness_e2e`), isolated `go test ./...` all pass; `npm install` leaves the edited lock unchanged; `tsc --noEmit`, vitest 120/120, both dists rebuilt, Playwright 26/26 with `--retries 0`, `shots.mjs` screenshots from the final build checked; bats 145/0. The entry files (CLAUDE.md / AGENTS.md / GEMINI.md) still name the removed `live-gate-shot.mjs` / `shot-dispatch-review.mjs`; they are updated separately because they carry uncommitted Owner edits.

## Unreleased — opencode permission replies outlive the event stream (AJ-720 follow-up)

- **Why**: CI intermittently failed `TestOpencode2Flows/permission` and `TestOpencodeServeWorkerV2/permission` with `Approval failed … context canceled`. Both opencode backends sent the permission reply with the event-stream context captured when the request arrived; approvers answer asynchronously, so a stream that had ended or reconnected cancelled the reply and the job failed.
- **What**: replies (1.x and 2.x) now use the session's lifetime context; the server call keeps its own 15-second timeout, and a reply after `Close` still fails.
- **Gates**: new `TestOpencodePermissionReplyOutlivesStream` (fails if the reply ignores the session lifetime); `go build ./...`, `go vet ./...`, isolated cockpit and sub tests, `-race -count=3` on the opencode cockpit tests.

## Unreleased — AJ-724 · delegation path no longer depends on the orchestrator or governance packages

- **Why**: `internal/sub` pulled in orchestrate, audit, sessions, agent, pact, ledger and projection for two calls only, so the frozen orchestrator could not be removed on its own.
- **What**: the cross-vendor API-key isolation table and `Strip` moved to a new dependency-free `internal/vendorenv` (orchestrate keeps a thin wrapper; the M11 table test moved with it, the ACP wiring test stays in orchestrate). `cockpit`'s ACP command mapping keeps the gemini-cli removal message inline instead of importing `agent`. `sub`, `subpanel` and `cockpit` now depend only on `acp`, `credentiallinks`, `gitx`, `lockx`, `procx` and `vendorenv`; `TestDelegationPathAvoidsFrozenPackages` pins that.
- **Gates**: `go build ./...`, `go vet ./...`, isolated `go test ./...`; the new dependency test fails when the old `agent` import is restored.

## Unreleased — AJ-721 · read-only tool-call limit 40 → 200

- **Why**: in the AJ-718 end-to-end suite every harness with read-only support (codex, opencode 1.x/2.x, agy) failed `readonly_long` at call 41 — a 60-file read is an ordinary review, so 40 was too tight. Owner decision 2026-10-18.
- **What**: `sub.DefaultReadOnlyToolCallLimit = 200` replaces the hard-coded 40 in `sub run` and `sub doctor`. The limit still applies only to read-only jobs; `0` still disables it.
- **Gates**: new `TestRunJobDefaultToolCallLimitAllowsSixtyFileRead` (fails with the old value), `go build ./...`, `go vet ./...`, isolated `go test ./...`.

## Unreleased — AJ-720 · opencode 2.x backend

- **One `opencode` harness, two backends**: the installed major version (one cached probe) selects the existing 1.x backend or the new 2.x backend; ≥ 3 still fails fast. Resuming across majors is refused using the previous job's recorded CLI version (both majors use `ses_` ids). 1.x behaviour, tests and goldens are unchanged.
- **2.x backend**: a private `serve --stdio` server per job (stdin lease, random `OPENCODE_PASSWORD`, redaction, process-tree cleanup), native session, model/variant, permission, event and history APIs, the first terminal of the prompt's own busy period, recursive child-session usage, typed failures (auth/quota/transient/model), reconnect without replay, and cancellation that also stops running tool processes. SSE heartbeats keep a long silent tool alive; repository `AGENTS.md` is honoured.
- **Private login (Owner, 2026-10-18)**: 2.x jobs use pactify's own persistent home (`harness-homes/opencode2`, all four XDG roots); credentials and sessions live in its `opencode.db`, never the user's. `pactify sub login opencode` runs `opencode auth login --standalone` there with project config disabled. Unavailable models suggest the login only for a connectable provider with no connection.
- **Read-only**: 2.x read-only sessions expose only `read`, `glob` and `grep`. opencode 2.x does not permission-check shell statements without a command word (`packages/core/src/tool/plugin/shell.ts`), so the shell tool is removed instead of allow-listed; the host reproduced the bypass on 2.0.32 and confirmed the fix. 1.19 has the same upstream pattern (`packages/opencode/src/tool/shell.ts:282`); 1.x rules are unchanged here and tracked separately.
- **Concurrency**: only startup holds an exclusive lease on the private home (bootstrap races with "database is locked" on an empty DB); running jobs share it. Host check: three jobs on a fresh home all succeeded concurrently.
- **Drift and evidence**: v2 OpenAPI projection, non-billable permission probe (13 cases on the real binary), v2 CI matrix, separate release baselines per channel, five real free-model goldens (write, readonly, resume, cancel with a running `sleep`, failure) replayed offline, and v2 support in the AJ-718 suite.
- **Live results (host, 2.0.32, `opencode/deepseek-v4.1-flash-free`)**: `sub doctor opencode` all PASS (only the informational "not sandboxed" warning every non-codex harness gets); AJ-718 e2e **13/14**. The only failure is `readonly_long` at the shared 40-call cap. 1.x scored 12/14.
- **Gates**: `go build ./...`, gofmt, `go vet ./...` (plus `harness_e2e` and `record` tags), isolated `go test ./...`, Opencode2 race tests, both Python suites, static drift, bats 145/0.
## Unreleased — AJ-706 · event-driven Live agent tree

- Add a Live view above the Dashboard metrics and as a tab in job details, shared with the read-only `pactify sub panel`. The tree shows the host, the router's aliases, fallbacks and exhausted routes, each observed job (harness · model · effort, duration, tokens, calls, diff, quota), tool categories and a session log; resume and fallback links and retry loops are drawn from recorded relations. Nothing is inferred: no probabilities, no invented sub-agents, and rows without a timestamp (real `log.jsonl` events carry none) show no time.
- Add a read-only `/jobs/{id}/events` SSE on the existing shared handler and `Store.Logs` reader. It keeps the 1 MiB initial window, the 100-event tail and cancellation, and forwards only structured projections: no raw payloads, and credential assignments and flags are masked in display text. Job summaries gain the recorded effort and a known positive cost.
- Events are batched per animation frame, and Live streams are capped at four across the page, with summary polling for the rest. Streams close on unmount or when the page is hidden. Reduced motion disables pulses, and mobile stacks tools vertically.
- The detail drawer keeps the intrinsic height of its children, so long Live content scrolls inside the drawer instead of overlapping the action footer; four Playwright geometry regressions (web and panel at 1440×1100 and 375×812) fail when the fix is removed.
- Gates: TypeScript; 165 Vitest tests; both embedded builds; Go build, vet and the serve, subpanel and sub suites. Playwright ran 26/26 outside the sandbox with `--retries 0`. Screenshots were checked against mocks and against a real running codex job through the panel.

## Unreleased — AJ-719 · harness update process; opencode 2.x fails fast

- opencode 2.0 ships as a new npm package, `@opencode/cli`, and replaces every v1 server route and the `serve` startup line that pactify drives. Measured on 2.0.32 in an isolated home: the deployed pactify waited 30 s per probe and failed with `opencode serve startup timed out`. Pactify now rejects opencode ≥ 2 before starting any harness process, in jobs, `sub doctor` and model discovery, with a message that names the fallback. The local CLI (1.19.0) is unaffected, and `opencode-ai@latest` is still 1.19.3. See [opencode-2-impact.md](harness-v2/opencode-2-impact.md); whether to build a v2 adapter is an open Owner decision.
- Add a weekly release watch (`internal/cockpit/harnessdrift/releases.py`, the `releases` job in `harness-drift.yml`). It reads registry and installer metadata for every official channel, successor packages included, and fails the scheduled run only for an untriaged new major line, a deprecated channel or an unparseable version. Registry outages are skipped. The process from detection to response is in [harness-updates.md](harness-updates.md).
- Gates: `go build ./...`, `go vet ./...`, `go test ./...`, bats, the drift unit tests and the static drift check; the doctor before/after comparison was run against the real 2.0.32 binary.

## Unreleased — AJ-718 · end-to-end harness integration suite

- Promotion gate 2 is now an end-to-end integration suite plus three endurance tasks that judge only integration signals; one-attempt completion of large tasks is reported per harness + model as a capability reference (Owner confirmed 2026-10-17). See [harness-v2.md §4](specs/harness-v2.md).
- Add the opt-in `harness_e2e` suite (`internal/sub/e2e`): 14 scenarios driven through the real `pactify sub` CLI in private homes, from worktree write/apply/discard and resume to cancel, a 4-minute tool, 5 MB output, parallel jobs, model fulfillment, failure surfaces, record consistency, AGENTS.md pickup, untouched user directories and the Claude Code plugin host path. Live run outside the sandbox: codex 11 / opencode 12 / agy 13 of 14 pass; the only shared failure is the read-only 40-call cap, which cuts off a 60-file read on all three. Kimi, rerun after the Owner logged in again (2026-10-18), passes 12 with 2 N/A, because kimi offers no read-only mode. Its first two failures were gaps in the suite: kimi streams tool arguments as in-progress `tool_call_update` text, and reads usage from `wire.jsonl` after the turn instead of logging it. Both checks are fixed and have regression tests. Kimi rotated its refresh token during the run and the new token was written back correctly. Results in [aj718-e2e-report.md](harness-v2/aj718-e2e-report.md).
- Fix defects the suite found: codex and agy invalid models and opencode missing login / kimi invalid grant were classified `other`; successful git stderr kept refreshing the idle watchdog; opencode native tool truncation never reached warnings (now an informational warning that does not replace the job error); codex `willRetry` notifications no longer become the terminal error or a quota class.
- Credential refresh: per-file credential links lost rotated refresh tokens, because kimi rewrites its credentials atomically and rotates the refresh token. This invalidated the Owner's kimi login after the AJ-713 recording and the first suite run. Managed homes now restore a refreshed private credential over the user's file when the source is unchanged (keeping its permissions), and otherwise move it to `~/.pactify/credential-quarantine` with a warning; they never delete it. Configuration rewrites are still discarded. Kimi shares its `credentials` and `oauth` directories, so pactify and an interactive kimi use the same refresh lock. The AJ-713 recording guide is corrected. The remaining check-then-rename window is tracked in [backlog.md](backlog.md).

## Unreleased — agy · Sonnet 5.5 comparison

- Ran agy with `claude-sonnet-5-5-high` on the same Phase 0 tasks to tell model gaps from harness gaps: 7 of the 9 tasks it finished were complete in one attempt (T09 minor, T07 incomplete), against 3 of the same 9 for gemini-3.8-flash-high, with 9/9 jobs succeeded and passed acceptance, no gofmt misses, and about 4x less time per task. The gap is model-side. T05 and the read-only tasks wait for the 3p weekly quota reset. Results in [agy-eval.md](agy-eval.md); the experimental notices are unchanged.

## Unreleased — Linux process snapshot ignores processes exiting mid-read

- On Linux, a process that exits while its `/proc/<pid>/stat` is being read fails the read with ESRCH wrapped in a `*PathError`. The process snapshot compared the error with `==`, so the vanished process failed the whole snapshot and `Close` returned `read /proc/<pid>/stat: no such process` (CI flake in `TestReviewReadonlySessionOverridesUserConfig`). It now uses `errors.Is`, like `Identity.Alive`. Regression: `TestSnapshotSkipsProcessExitingDuringRead` (Linux only).

## Unreleased — agy final report survives timer wake-ups

- agy can wake the model again after it has reported, through a background-task notification or a timer the model scheduled. The reply to that wake-up ("That was just the timer…") used to replace the report as the job's final text: 3 of 10 jobs in the agy + Sonnet 5.5 evaluation lost their report this way (T07, T09, T10). The final text is now the longest answer since the last working tool step, followed by any later answers; `manage_task` and `schedule` count as waiting, not work. Completion followup detection still reads only the last answer. Regression `TestAgyV2ReportSurvivesTimerReply` fails on the old parser; replaying the real T07, T09 and T10 streams recovers their reports, and T03 is unchanged.

## Unreleased — Harness v2 · opencode evaluation (MiniMax-M3)

- Evaluated opencode v2 (`opencode serve`) on the Phase 0 tasks with `minimax-coding-plan/MiniMax-M3`: 4/10 large tasks complete in one attempt, 10/10 jobs succeeded and passed acceptance, no new full-suite failures; read-only 1/3, as R1 and R3 were cut off by the 40-call cap. opencode stays experimental. Results in [opencode-eval.md](opencode-eval.md); notices now cite both models (plugin 0.2.6).
- Eval reports note that the v1 job directories were removed during the 2026-10-17 disk cleanup; per-task logs and verdicts remain.

## Unreleased — AJ-711 · agy v2 re-evaluation

- Re-evaluated agy v2 on the Phase 0 tasks (gemini-3.8-flash-high): large tasks 3/10 complete in one attempt (v1: 1/10), so agy stays experimental; read-only converged 3/3 with usable conclusions when the read-only tool-call cap was raised to 120 in an eval-only build, 2/3 under the default cap of 40. Results in [agy-eval.md](agy-eval.md); the experimental notices now cite 3/10 (plugin 0.2.5).
- Classify agy transport failures before an HTTP status (`request failed: Post …`, e.g. `use of closed network connection` after a long tool run) as transient, so the existing same-session retry resumes them instead of failing a finished turn.
- Refuse git `--git-dir`/`--work-tree` anywhere in agy read-only commands after an independent re-review of the allowlist found no bypass; residual risks are recorded in the [allowlist report](harness-v2/aj711-policy-allowlist.md).

## Unreleased — AJ-711 read-only allowlist review

- Replace agy grep/rg option and find predicate denylists with explicit allowlists; remove tree and go list/vet, restrict go env to queries, and reject Git repository redirection/no-index, file magic compilation/custom magic and wc file-list indirection.
- Execute every approved command from quoted argv so shell glob/brace/tilde expansion cannot change validated arguments. Document literal-glob behavior and recursive-search alternatives; native program prefixes follow the same hook list. Opencode remains unchanged.
- Fix the async quota probe inheriting a harness marker and deleted job temp; a real seven-second fake /usage process now verifies both snapshots survive terminal cleanup and private temps are removed.
- Add regressions for all confirmed bypasses, reviewed scope/expansion risks and R1/R3 commands. Superseded prior positive assertions and fresh live/gate evidence are recorded in the [allowlist report](harness-v2/aj711-policy-allowlist.md).

## Unreleased — AJ-711 flash reevaluation follow-up

- Replace agy's exact read-command list with shell-words parsing and program-specific argument checks; align native prefix permissions while leaving opencode's static policy unchanged.
- Collect the opening quota snapshot concurrently with startup, allow 20 seconds, and supplement terminal results within a 20-second closing budget. Retry terminal malformed-call diagnostics through AJ-693 in the same conversation and retain raw terminal evidence.
- Use a configurable 30-minute idle limit while tools are active; preserve the ordinary 10-minute event/tree-change watchdog and cancellation behavior across adapters.
- Regressions were run failing before implementation. Required commands, dependency audit, superseded deadline assertion, flash live probes and user-directory comparisons are recorded in the [follow-up report](harness-v2/aj711-flash-followup.md). Plugin files and version remain unchanged.

## Unreleased — AJ-713 · consistency gates, part one

- Add opt-in sanitized raw-protocol recording and adapter replay, with five real opencode and four real kimi transcripts, field-rename mutation checks, denied-write evidence, and sensitive-data scanning. Agy live recording remains deferred until host evaluation finishes.
- Align all four doctors to ten rows, retaining live probes and explicit N/A reasons; add sandbox declarations and non-billable offline failure-classifier injection.
- Add weekly/manual kimi/agy drift checks with offline-only PR checks, version-change doctor hints, and installation skip reasons. Keep Experimental labels and plugin 0.2.4 unchanged.
- Add the C1–C17 evidence matrix. Validation output, user-directory metadata comparisons, and the three explicitly superseded doctor test contracts are recorded in the [AJ-713 report](harness-v2/aj713-report.md).

## Unreleased — AJ-712 · kimi P0

- Read durable wire failures before retry/fallback classification; sum all agent usage with per-agent resume baselines; discover ACP models without a prompt, with warned offline fallback and shared catalog caching.
- Keep kimi read-only disabled: shared Git state and absolute symlinks defeat the proposed isolation. Restore K1 refusals and doctor Read-only SKIP; retain Network evidence and `sandboxed=false`. Plugin version: 0.2.4.
- Review fixes cover main-only usage completion, precise transient/quota/auth classification, retained ACP and wire evidence, prompt-error wire verification, and independent catalog locking/cleanup. Validation and host live evidence: [AJ-712 report](harness-v2/aj712-report.md).

## Unreleased — AJ-711 · agy v2

- Review fixes: scope doctor process ownership to its disposable store, replace Python hooks with a compiled fail-closed policy, remove native prefix grants, refuse workspace hooks in read-only mode, and conservatively detect pending results.
- Keep quota observations in a separate rollback-compatible ledger; cap opening probes at five seconds, publish terminal status before bounded closing probes, and keep snapshot failures out of job errors.
- Add independent doctor hook evidence, flash-only doctor selection, bounded stdin cancellation, sticky model mismatch rejection, per-turn tool tracking, and user-config hook warnings. Validate with failing replay regressions and flash live diagnostics; full evidence and remaining browser-launch restriction are in docs/harness-v2/aj711-report.md.

- One persistent stream-json process per job, explicit model selection on all CLI paths, conversation continuation/restart, step usage, classified empty results, and bounded completion followups.
- Restore read-only plan access with exact commands, managed PreToolUse policy, and the shared fail-closed tracked/untracked/ignored baseline. Write jobs remain `sandboxed=false`; `--sandbox` is not used.
- Collect structured `/usage` snapshots before/after jobs, retain quota reset times, and render agy buckets in Dashboard Quota. Rebuild both embedded dist bundles.
- Add static effort constraints and agy doctor denial/network checks; correct the shared doctor retry access mode. Plugin version: 0.2.3.
- Validation, superseded AJ-683 tests, isolation evidence, and outstanding live/browser gates: [AJ-711 report](harness-v2/aj711-report.md).

## Unreleased

2026-10-15～16 合入 main，**尚未发布**；当前发布版仍为 v0.12.0。M1 功能已合入，v0.13.0 挂起，Owner 选择继续内部自用。

- **修复 panel 手机抽屉高度**（AJ-701 / PR #130）：独立 panel 的 Tailwind 扫描原先遗漏共享 Jobs / UI 源码，最终 CSS 缺少定位、flex 和滚动工具类；显式补齐扫描，两种入口共用手机 `fixed`、视口高度和内部滚动规则，桌面保留右侧抽屉。打开抽屉时锁住背景，关闭恢复原 overflow；用 `@supports` 保留生产 CSS 的 `100vh` 兜底及 `100dvh`。仅将三处既有像素尺寸断言改为容差比较，新增长结果滚动 e2e、滚动锁定及独立构建 CSS 回归。验证：新增回归先红后绿；TypeScript、vitest 66 文件 / 629 项、两个前端 build、`go build ./...` 通过；Playwright 待宿主机以 `--retries 0 --repeat-each 5` 验证。
- **Jobs 看板与运行链**（AJ-701）：dashboard / 本机 panel 共用 Board 风格的工具栏、四列看板与详情抽屉；按显式 resume / fallback 链合并卡片，保留逐轮日志、结果、diff 和深链接，窄屏堆叠看板并全屏显示抽屉。Routing 独立页签继续沿用原保存校验，panel 仍只读；用量置于底部。删除摘要 `issue` 字段与编号提取，标题只取 brief 第一条非空行；五份委派 skill 共享块补充标题约定。验证：新增回归先红后绿；指定四包 race、`go build ./...`、隔离 HOME / PACTIFY_HOME 的 Go 全量测试、TypeScript、vitest 64 文件 / 626 项、两个前端 build、bats 144/144 通过；更新 Jobs / panel / Routing e2e 并增加运行链、分列、抽屉与 390px 覆盖，Playwright 和最终 build 视觉验收待宿主机运行。
- **harness 路由与 M1**（#69–#91）：Claude Code 插件与 `pactify sub` 提供委派、后台 job、续接、取消、日志、worktree 隔离与 `sub doctor`；codex 成为默认，kimi 保持实验性且不开放只读。默认精简 callee 配置、隔离会话，补齐结果、实际模型及 token 核实，减少委派开销与用户会话污染。
- **M2 harness 扩展**（#92–#94、#101–#104、#109、#113）：接入实验性的 agy（命令行后端、动态模型表、镜像 HOME）与 opencode（ACP、隔离 XDG、模型与成本核实）；修正 agy 终稿和模型列表失败处理、opencode export 管道问题与用量统计。大任务一次完成：agy 1/10（见 [评估](agy-eval.md)），opencode + deepseek-flash 6/10（见 [评估](opencode-eval.md)）。
- **关闭只读档**（AJ-683）：因只读评估均为 0/3，agy、opencode 保持实验性，关闭只读档；入口及旧只读 job 续接退出 2 并指向 `codex --read-only`，doctor 标记 SKIP，删除无调用方的只读执行与拒绝证据逻辑。验证：相关包 race、`go build ./...`、隔离 HOME / `PACTIFY_HOME` 的 Go 全量测试与 bats 142/142 通过。
- **任务查看与续接**（#95–#99、#105）：共享 `sub status --json` 摘要、Claude Code `/jobs`、本地只读 `pactify sub panel`，展示活动、结果、日志和 diff；`--resume <job-id>` 可续接指定任务，版本变化不再覆盖真实失败原因。
- **退役清理**（#97、#100、#106、#107、AJ-664、AJ-665）：删除 Go 云端与 web 托管模式，移除 gemini-cli（web 与 Go 两侧，已注册的 gemini-cli 座席会提示改用 agy）。云端文档归档至 `docs/archive/`，精简部署、运维与架构说明，同步三份入口文件。Owner 决定 linx 整体退役，共享 workspace 及构建 / 发布 CI 已删除（AJ-664），Go DTO golden 迁至 `internal/serve/testdata/`，保留 `UPDATE_GOLDEN` 更新入口；agy 的 Gemini 模型与 GEMINI.md 入口保留。

**本次文档清理验证**：`go build ./...`、隔离用户目录与 `PACTIFY_HOME` 的 `go test ./...` 全仓通过；指定 bats 4/4 通过。全量 bats 140 项中 9 项因隔离环境缺少 Python `jsonschema` 失败，补齐临时依赖后复跑所在两个文件 10/10 通过，未改测试。三份入口区段逐字一致，托管块未变，四份归档内容逐字节保留。

**AJ-664 验证**：fixture 路径迁移先红后绿，`UPDATE_GOLDEN=1` 在新位置生成的 golden 与原文件逐字节一致；`go build ./...`、隔离 HOME / `PACTIFY_HOME` 的 `go test ./... -count=1`、web TypeScript 检查与 vitest（59 文件 / 578 项）、指定 bats（4/4）通过。Go 全量首轮受系统 Git 的 xcrun 沙箱外缓存错误影响，中止后改用已安装的 CommandLineTools Git 重跑通过，未改测试断言；web 生产构建输出到临时目录验证通过。三份入口正文同步、托管区未变，历史版本节与 archive 未改；Playwright 由宿主机补跑。

---

## v0.12.0 — 2026-10-15 · 编排器方向的最后一个正式版本（转向前的回滚锚点）

**为什么发这一版**：2026-10-15 Human Owner 拍板产品转向「harness 路由器」（`docs/specs/harness-subagents.md`），协议 / 账本 / 看板 / 云端降级冻结。本版把 v0.11.0 之后 `main` 上积累的 PR #48–#64 连同一次死代码清理正式发出，作为编排器方向的最后一个版本和回滚锚点：`go install github.com/agentjoey/pactify/cmd/pactify@v0.12.0`。

**自 v0.11.0 起合入的内容**：
- **#48 `[FALLBACK-PAR]`** 并行 run 的 fallback 提案闭环；版本历史从 `CLAUDE.md` 拆出到本文件；CI 缓存与时间上限修复。
- **#49** checkpoint 的 in-place run 守卫（`[UI-GATE]` 止血）+ ACP 丢弃 model pin 时的醒目警告 + codex 审批卡标题（`[CODEX-APPROVAL-NAME]`）。⚠️ 该守卫在生产里实际不生效，见下方遗留 `[RUNGUARD-DEAD]`。
- **#50** cloud workspace 独立 CI job；**#51 / #52** 仓库公开前清理（误提交的 16MB 工具目录、被跟踪文件里的本机绝对路径）；**#53** 仓库公开（全历史 1476 个提交凭据审查零命中）。
- **#54 WS-A** 账本读写路径收敛到 `internal/ledger`，顺带修掉一处读错项目的 bug；**#64 WS-B** 账本 ref 存储 + CAS，双写双读校验，flag 默认关、不改现网行为；**#60** 单一规范账本 spec 定稿。
- **#55 `[ACP-MODEL]` opencode 根治**：`opencode acp` 无模型参数，改用 `OPENCODE_CONFIG_CONTENT` 注入（合并不覆盖），端到端验证实际 provider 随绑定切换；#56 状态回填。
- **#57** 并行 merge 路径三处缺陷（升级会丢 / 失败无声 / 失败报成 shipped）；**#58** 评审动作跟随任务状态 + ReviewGate 证据区对比度达标；**#59** 审批决策词表的跨边界对账守卫。
- **#61** Settings 的 agent 面板改版（两页合并为 Agents，加 Rescan / 收起展开 / 连通性 Test）；**#62** 删除已采纳的 mockup；**#63** Settings 外壳窄屏纵向堆叠。
- **#66 发版前死代码清理**（deadcode / staticcheck U1000 / knip，含 `--production` 模式）：删除连测试都不可达的 12 个 Go 函数与 1 个类型；删除已被 `ApplyTx` 取代、把座席硬编码成 "claude" 的 `planner.Apply`（测试改为覆盖 `ApplyTx`）；删除回放视图（2026-07 已移除）残留的 `GET /timeline` 路由与 `?at=` 参数及其 bats 用例；删除只被自身测试引用、界面早已不挂载的 8 个前端模块（Setup、AgentRoster、Projects、PlanDock、RosterDock、Popover、Tooltip、timelineTicks）及若干无调用方的函数；只给测试用的 helper 挪进 `_test.go`。约 −2.5k 行，不改任何仍在使用的对外行为。刻意保留：测试替身、随云端退役整体删除的 `cloudauth` / `cloud/` / web identity、在建的 WS-B 代码。

**跑过的门**（发版提交上，独立 clone 中执行，避免测试污染主副本）：`go build ./...` · `go test ./...` 除 `internal/cockpit/codexschema` 的 `TestSchemaDrift` 外全绿（本机 codex 0.162.1 新于随附 schema 0.144.4 才会红，CI 无 codex 时跳过；与本版改动无关）· `go test -race`（pact / orchestrate / serve）全绿 · bats 112/112（清理前 114，删掉的 2 个只测已删除的回放接口）· vitest 639/639（清理前 685，差额为随死代码删除的测试）· `tsc -b` · Playwright e2e 7/7 · 内嵌 dist 重建 · CI 全绿。

**遗留（随降级冻结，证据链在 backlog）**：`[RUNGUARD-DEAD]` run 守卫解析 `updated_at` 用 RFC3339，而真实 run 写 `20060102-150405`，守卫永远看不到在跑的 run · `[UI-GATE]` Board 无补 checkpoint 入口，`CommitAll` 全树提交的归属问题未动 · `[ACP-MODEL]` kimi / gemini / claude / codex 走 ACP 时模型绑定仍只有警告 · 随附的 codex app-server schema 需刷新到当前版本（harness 路由 Phase 0 的前置）· 云端已退役不再部署。

---

## v0.11.0 — 2026-08-24 · backlog 全量核实后的清理批次

**v0.11.0**（2026-08-24）— backlog 全量核实后的清理批次（PR #47，14 个实质提交）。起点是对 `docs/backlog.md` 全部 113 个条目逐条**真实核实**（读代码/跑测试，非文本互证），据此排期实现；每条均过独立复核（新会话、不继承实现上下文），复核判 FAIL 的两处已定位修复。主线：**[SANDBOX] 默认沙箱可用**（tracked-`.pact` 仓里 seed 的账本是对 tracked 路径的未提交修改，checkout feature 分支必被 git 拒绝 → 此前只能退回有腐化风险的 `--in-place`；改为保账本的 checkout：快照→还原→checkout→union 回写，未 tracked 时逐字节走原路径）· **relay 广播 antigravity**（⚠️ 只改 Go 侧会造成线上事故：wire 枚举缺该值而 relay 走 throwing 的 `MachineInfo.parse`，一台 agy 机器能让整个账户机器列表 500；两侧同改 + conformance guard。**部署顺序硬约束：relay 必须先于二进制上线**）· **[KIND-2] `--seat-kind` 真的赢过 role binding**（并新增 `--roster-kind` 非显式渠道——独立复核揪出的跨进程回归：serve 派生的 kind 曾被当作显式覆盖，会让 role binding 对所有 dashboard/Dispatch/schedule/fallback 发起的 run 静默失效）· **[CODEX-MAP] codex cockpit 后端 7 条协议映射错误**（写下即错非漂移：MCP 工具调用从不产生事件、TOK 恒 0、`resume`/`interrupt` 发成通知从未生效、permissions 审批回复结构非法；真机抓包做 fixture）· **agy 审计接入**（`strings` 挖出原生 PreToolUse hook + 内嵌官方文档，机制与 gemini 血统完全不同）· **agy 会话清理**（无 CLI 且无法打标——`--project` 被接受但哪里都不落；改按 pactify 自己为 resume 记录的 `conversation_id` 做只删点名 id 的文件级清理，UUID 守卫承重：放宽后 id `..` 会 RemoveAll 整个数据目录）· **[CODEX-SCHEMA] 修好失效的预警**（本机恒红+CI 永远 skip，等于没有；新增不需装 codex 的 `TestSchemaContract` 常驻 CI + 周期拉最新版的 workflow，硬门=契约破坏、软信号=字节漂移）· **[DOCTOR] 退出码只由本项目用到的 kind 决定** · **[PROBE] 共享 entry 文件按 kind 精确归属**（并补掉 `pact.BakeEntry` 这条主路径）· **[REGISTER]/[REGISTRY-2] 自动注册跳过临时目录 + 未装机警告**。gate：go 全仓 + `-race` · bats · vitest · tsc · cloud workspace · CI 全绿。遗留（已记 backlog）：`[DIST-STALE]` main 上的 dist 本就陈旧（本机与 CI 各自重建产出同一新哈希，排除构建不确定性）· `[TEST-POLLUTION-2]` `acprunner_test.go` 写用户真实 audit 目录 · `[CODEX-APPROVAL-NAME]`/`[CODEX-DECISION-CANCEL]`。

---

## v0.10.0 — 2026-08-23 · 自 v0.9.0 起 299 个实质提交的合并发布

v0.10.0（2026-08-23）— 自 v0.9.0 起 299 个实质提交的合并发布。主线：**phase-0 安全加固**（PR #36，3 critical + 3 high + M1 diff 脱敏 / M2 fs-browse 限域 / M8 cancel 守卫 / M9 merge 崩溃恢复 / M11 跨厂商凭据隔离 / M14 park 陈旧守卫 / M15 未回灌账本保全 / M16-17 fixRounds 重置+心跳）· **seat-identity 三段**（身份解析链 `.As()>env>.pact/seat` + `pactify seat` verb + 接线层去身份化 + 隔离 worktree 播种）· **role routing + failure policy**（PR #43-45：机器级 role profile + seat 绑定、按 seat 解析 kind/model、失败分 env/logic 两类、fallback 提案链 + `--approve-fallback`/`--reset-task`、planner 按 role 路由、dashboard 审批卡）· **execution tiering**（`tier: L0..L3` 经 spec 文件travel、不动冻结的 pact v1；`EffortForTier` + `budgetFor` 派生修复轮/返工/失败/critic/QA 预算，显式 flag 经 `Flags().Changed()` 优先；前端 tier 徽标三态 NO TIER/L0-L3/冲突）· **antigravity(agy) 接入**（GUI 座席改造为可 headless 驱动的 CLI kind：`--add-dir` 工作区锚定、`--conversation` 续接、单对象 JSON 的 head+tail 双窗口解析、status 门控会话记录、刻意不声明 `EffortArgs`——`--effort` 与模型名后缀冲突会 exit 1，档位改用带后缀的模型名）· **auto-register + registry 热重载**（PR #39/#41）· **ACP npx 握手根修**（PR #42，bridge 直调 bin —— 此前 `npx -y` 从不回 initialize，ACP 实际从未握手成功）· **RELAY-2**（relay 不可用不再阻塞 serve 启动）· **e2e 五连修**（PR #37）· **sandbox base 来源统一**（PR #40）· **测试污染根治**（`internal/testenv` + 7 个 TestMain——被编排 agent 跑 Go 测试曾把真账本从 622 条清成 1 条并覆写 CLAUDE.md/AGENTS.md/PROJECT.md，两次伪装成「worker 越界交付」误导评审）· product UI v2 / Flow 视图 / hosted cockpit / ACCT-A1。gate：go test 全仓（`internal/cockpit/codexschema` 的 `TestSchemaDrift` 红属既有——vendored schema 停在 codex 0.142.5，该包零 Go 消费方，仅为未开工的 DEEP-INT 留的参考件）· `-race`（pact/orchestrate/serve）· bats 114/114 · `-tags agye2e` 真跑 agy e2e。

---

## review-runtime（包 2，`feat-review-runtime`）— 2026-07-05 · 评审深化 + 运行时

review-runtime（包2，feat-review-runtime，2026-07-05）— 评审深化 + 运行时 8 任务:**fix-until-green 自修环**(checkpoint 后评审前跑 verify 门,红则同 worker fix 轮 default 2 不计 MaxFails,board 显示修复中 n/2)、**quorum 多评审**(`--reviewers/--quorum`,投影按本轮不同 reviewer accept 数判定,单 reviewer 字节不变,两不变量神圣)、**critic 预评**(CRITIC_SCORE 注入 reviewer 无门控权)、**QA 门**(`qa:` 行真跑软件 QA_RESULT,FAIL 走共享 fix 预算,宽松)、**账本持久快照**(`.pact/state-snapshot.json` 增量 replay,5k **11.5x**,损坏静默全量回退)、**动态座席**(`join --kind` + driver 每轮实时读 seat→kind + planner 自动 staff)、**定时 run**(`pactify schedule` daily/every,serve ticker)。gate:go 全仓+`-race` · vitest 521 · tsc · bats 111。**混合模式自驱交付**(承 P4/P5 后:pactify 账本协调 + subagent 执行 + orchestrator 亲做 pact/git,每棒 checkpoint 归属 owner 座席 + 即时提交)。（follow-up "validate 只认 init 座席" 已过时并于 2026-07-19 移除：validate 名册来自 log 投影，dynamic join 座席会入册，跨厂商强化测试 P5 实测 valid。）

---

## driver-modernization（包 1，`feat-driver-modernization`）— 2026-07-05 · 驱动层现代化

**driver-modernization**（包1，feat-driver-modernization，2026-07-05）— orchestrate 驱动层现代化 9 任务:**ACP transport**(`internal/acp` Go JSON-RPC/stdio 客户端 + `orchestrate.AcpRunner` + `RoutedLocalRunner` 按 kind 选传输,`--transport kind=acp`,默认全 cmd 零行为变化;kimi=`kimi acp`、claude/codex/gemini 经 npx bridge;permission 三档 auto/escalate/deny)、**ACP usage→token store**(修 TOK=0 旧债,`session/update` usage 落 `internal/tokens`)、**session 续接**(`.pact/orchestrate/sessions.json` lockx,支持 loadSession 则续接省 token)、**知识层**(`.pact/memory.md` + `.pact/skills/*.md` role/keyword 注入 briefing,无文件字节不变)、**doctor per-vendor 预检**(binary+auth+ACP 可用性,serve 启动非阻断)、**per-seat 可靠度**(`AgentStat.Accepted/Reworked` fold + OfficeView 徽标)。gate:go 全仓+`-race`绿 · vitest 521 · tsc clean · bats 111。**dogfood 交付方式**:先经真 orchestrate 自驱(dm-acp-core/runner 高质量交付),后因 orchestrate `--in-place` 的 worker-对协议文件-git-surgery 腐化(P4)+ sandbox `total=0`(P5)切**混合模式**(pactify 账本做协调事实源 + 受控 subagent 执行 + orchestrator 亲做 pact/git),问题全记 dogfood 报告。

---

## v0.9.0 — 2026-07-02 · 全仓 review 驱动的三轮加固

**v0.9.0**（全仓 review 驱动的三轮加固：ledger 并发/崩溃安全 + 协议语义修正 + 可观测性接线，✅ 已发布 tag v0.9.0）— 2026-07-02 对全仓做了一次 8 路并行 code review(`.agent/reviews/code-review-2026-07-02.md`),按 P0/P1 分三轮修复,commit `d8bd6bd`(RUN 时长改由 `assign` 事件推导,不再读不存在的 `in_progress` 事件)→`aff6509`(sandbox run 加**mid-run ledger mirror**——每轮迭代把 sandbox ledger union 回主目录,board 不再冻结到 run 结束;新增**任务级 `start` 事件**,worker 未走 `pactify join` 时也能让 board 显示 in_progress,spec §2.6 记录)→`5f10ba2`(第一轮 7 个 P0:driver 清洁退出救援补线、并行 drain 三分支处理、serve 项目重命名后 SSE 换键、双击 New task 竞态锁+子进程收尸、session 清理 tag 定界符防跨座席误删、Board Accept/Changes 补错误处理)→`595bd3d`(第二轮并发/崩溃安全:全 13 个 pact verb 统一 `withLedgerLock` 跨进程锁、`Checkpoint` 写序翻转为先 commit 后记事件、`GateConfig` fail-closed、orchestrator 角色门、Assign 时 slug/ref 校验;orchestrate sandbox 的 `writeLedger` 加锁+原子写、park 分支身份持久化+拒跑陈旧 park、teardown 走 defer 兜 panic)→`46e733e`(第三轮:serve 全量 fold 加 memo、worktree 视图补 events 端点让指标不再归零、live agent 流按 `Last-Event-ID` 续传防重连重复、orchestrate History 持久化跨重启存活、`join` 只抬座席第一个可动工任务(不再污染整批)、base 锁改走 `GitCommonPath`(修跨 worktree merge 不互斥)、cancelled 依赖不再死锁依赖者、TOK 指标接上真实 `/stats`(删掉读不存在 payload 的死机制)、audit 脱敏扩展、Plan-mode 死代码清理)。**同期产出**统一架构方案 `.agent/plans/unified-architecture-2026-07-03.md`(分析 linx 的 relay/认证/账户架构,规划 pactify 接入同一套 relay+账户体系的 U0–U4 路线,relay.go 空壳与 derive 纯函数层已按此方向保持薄/可复用)。gate:go test 25 包 + `-race`(pact/orchestrate/serve)绿 · vitest 450 · tsc clean · bats e2e 111/111。

---

## v0.8.2 — dashboard 隐藏 pact 内部 worktree（PR #31）

v0.8.2（dashboard 隐藏 pact 内部 worktree，PR #31, tag v0.8.2）— `serve` 的 worktree 列表(`gitWorktrees`/`filterInternalWorktrees`)过滤掉 pact 自己的 run worktree(`.pact/orchestrate/` 下的 sandbox/parallel 树 + `pact-*-park` 分支),不再把它们当成可选项目板抛给 dashboard——消除「同一任务在不同 tree 下状态不一致」(run 中 worker 隔离 worktree 的 live `.pact` vs 被 park 的主树 seed,因 `.pact` gitignored 每树各一份)。primary(被 park 的主树)永远保留为规范视图;`resolveWorktreePath` 共用过滤,内部板也不可经 `state?wt=` 寻址。这是 option A(对症);彻底解=单一规范 ledger(ledger 与 git 工作目录解耦)留作后续重构。

---

## v0.8.1 — worker 投递完整性补丁（PR #29–#30）

v0.8.1（worker 投递完整性补丁，PR #29–#30, tag v0.8.1）— 修 worker 在隔离 worktree 下的「投递丢失」：**#29** runner 给 worker 注**绝对 `PACT_DIR=<RepoDir>/.pact`** + `pact.At(".")` 的 git 工作目录随绝对 `PACT_DIR` 取 `dirname`,把 worker 的 checkpoint 钉到 driver 的 worktree(此前 opencode 项目级 MCP 独立解析项目根 → checkpoint 落到岔开的 ledger/分支、worktree 销毁即丢 → driver 报「干完活但看不到、分支空、failure limit」);**#30** 回灌 `writeLedger` 按 `event_id` **union 合并**(不再整文件覆盖、不冲掉并发写)+ escalation **归因**(worker 干净退出却没 checkpoint 时明说「无提交落盘」,不再裸 failure limit)。

---

## v0.8.0 — coordination-authority + dark product UI（PR #24–#28）

v0.8.0（coordination-authority + dark product UI，PR #24–#28, tag v0.8.0）— 把「协调权威」从隐式（进程 cwd / 裸 git ref）升格为**显式·可寻址·可加锁的资源**(spec `docs/specs/coordination-authority.md`)：**#24** runtime ignore 走 `.git/info/exclude`(零提交) + autonomous **默认 sandbox**(`--in-place` opt-out；status/streams/escalation 镜像主目录,dashboard 仍见 live) + 每个 `merge` 持**跨进程/worktree flock 锁**(`internal/lockx`) + MCP 每工具加 `project` 入参 + `projects` 工具(一个 session 按名驱动任意注册项目,解开 cwd 绑定)；**#25 base 写入契约**——只有 `merge` 写 base、merge **fetch-aware**(`fetch origin`→base ff 到 `origin/base`→feature rebase 其上,失败即报错,绝不分叉)、默认不 push(`--push` opt-in)、`accept` 不连带 merge、`checkpoint` 在 base 上而 feature 声明了分支即报错；**#26** per-project 硬门 `config gate`(类型默认 pnpm/npm/cargo/go,优先级 task `verify:` > config gate > 默认)；**#27** 空 feature 分支拒绝 ship(杜绝 phantom)；**#28** 机器提交一律 `--no-verify`(`CommitAll`/`CommitPaths`/`MergeNoFF` 不跑 commitlint/lint-staged——linx phantom 真因=pre-commit 钩子拒机器提交)。**dogfood=linx**(pnpm SvelteKit 应用)端到端揪出并修。gate：go test 25 包 · go vet · CI test/e2e/site 绿。**同版一并发布 dark product UI refresh** — dashboard 从浅色切到**深色**，照 `design_handoff_dark_product_ui` 设计稿重制全部 6 屏。本质是 token swap（`tokens.css` `@theme` 改深色，role 三色回归 brand.md 一直定义的深色基底，≈70% 经 `var()` 自动迁移）+ `index.css` 硬编码浅色值 re-tone + 少量新 pattern：`ui/MetricStrip`（mono `RUN/TOK/×iter` 统计条）；**Board** 全宽 5 列（Assigned/Working/Review/Accepted/Shipped）+ 上下文头条（feature chip + 座席簇 + New task）+ 卡片统计条 + Review 列内联 Accept/Changes，去固定左 dock，accepted+shipped 折叠到最近 6；**Canvas** 底部 NL 命令坞；**Live** 两栏 + 右侧事件流终端（`#07090d`）；**Setup** 旅程步进器 + 角色切换 pill；**Settings** scope 双栏（Project/Machine/Account）。附带修 stats：`WithLOC`→`WithTaskLOC`，按各任务 checkpoint commit 精确归因代码量（旧逻辑同分支每任务显示相同分支总量）。gate：vitest 438 · tsc clean · build green · 6 屏 playwright 实拍比对设计稿。**用 Pactify 自身 dogfood 产出**（kimi=worker 跑 t1–t9，配额耗尽后 claude 接管 t5/t6/t7；claude=reviewer 逐任务视觉评审）。

---

## v0.7.3 — Dispatch/orchestrate 健壮性

v0.7.3（Dispatch/orchestrate 健壮性，dogfood 揪出的 3 个 gap）— ①orchestrate run 守卫加 stale 判定（旧 run `done:false` 不再永久堵新 run，按 updated_at>10min 或不可解析视为 dead）；②serve 触发的 run 改 `resolveSeatKinds`（init→roster Kind→按座席名对 `agent.Kinds()` 启发式，opencode-worker→opencode 等，修「座席没记 kind 跑不动」）；③planner prompt 加「verify 范围化」规范（禁全仓 `eslint .` 之类会撞范围外历史债的门）。

---

## v0.7.2 — 删 Canvas Plan mode

v0.7.2（删 Canvas Plan mode）— Canvas 的 Plan 页面（拖拽/连线拆任务图）在有了 Dispatch 面板 + 只读 PlanDock 后冗余，整块移除：mode 切换 + TaskNode/SeatNode/FeatureGroup/ConnectionLine/ConnectingFlag 节点组件 + canvas.ts/comms.ts 的 Plan-only exports + canvas e2e spec/switchToPlan helper + CheatSheet plan 手势行；Canvas Office 视图与其余不动。**本版由 Dispatch dogfood 端到端产出**（opencode/kimi/gemini worker + claude 评审/e2e 收尾，vitest 398 + e2e 5 双绿）。

---

## v0.7.1 — Dispatch planner 错误透出

v0.7.1（Dispatch planner 错误透出）— `defaultRunPlanner` 改 `CombinedOutput()`，把 planner 子进程输出尾部带进 plan-gen error（不再只剩 "exit status 1"，能看到 "exec: claude: not found" 之类真实原因）。⚠️ 部署：launchd serve 的 plist PATH 必须含 `~/.local/bin`+`~/.opencode/bin`（claude/kimi/gemini/opencode 二进制位置），否则 Dispatch/orchestrate 起不了 agent。

---

## v0.7.0 — Sprint2 端到端下发 + worktree-aware 项目

v0.7.0（Sprint2 端到端下发 + worktree-aware 项目）— **Dispatch Panel**：dashboard 从「只能看」→「一句话下发」，右侧滑出面板 goal→后台拉起 planner agent 生成任务图→审阅→确认 = apply+orchestrate run（后端 `POST/GET .../plan/generate[/status]` claude 写；前端 DispatchPanel kimi 经 pact orchestrate dogfood 写，A/B worktree 隔离并行）。**worktree-aware 项目**：`GET .../worktrees` + `state?wt=`，ProjectMenu 项目下缩进列出 git worktree、可切换查看各 worktree 的 board（非主轮询、主 SSE）。

---

## v0.6.1 — IA v2 收尾 + UI 修正

v0.6.1（IA v2 收尾 + UI 修正）— RosterDock 重设计为精致身份列表（按角色分组的 logo+名+角色标签，两张悬浮卡居中偏上、Board-only 避开 Canvas/Office 工具栏）、项目下拉每行状态灯、Board 左 gutter 防遮挡、accepted 列显示最近 10 张折叠其余；#22 清理（删 Sidebar/PlanReview/TopBar 死代码、View/CableMark 迁出、RosterDock 齿轮定位座席 Settings）。

---

## v0.6.0 — IA v2 dashboard 信息架构重做

v0.6.0（IA v2 dashboard 信息架构重做）— header 项目下拉（状态灯+改名+projwiz）、悬浮 RosterDock（磨砂卡片，orchestrator 第一）、Settings modal（整合 ops+setup）、视图 7→3（Board/Canvas/Live，默认 Board，快捷键 1/2/3）、PlanDock 只读悬浮窗、Recipes 进 ⌘K、Board accepted 列折叠/分组/限渲染、删 ReplayBar+`?at=` 回放、删 Sidebar；后端 `PUT /api/registry/{name}` 改名 + SeatDTO 透出 kind + planner kebab-slug 命名校验/prompt；

---

## v0.5.2 / v0.5.1 / v0.5.0 / v0.4.0

v0.5.2：SSE 反代健壮性修复；v0.5.1：orchestrate per-task token 捕获；v0.5.0：custom-agent manifest API；v0.4.0：orchestrate 自主驱动 + planner + 浅色 dashboard + native audit layer + pactify.dev 文档站

---
