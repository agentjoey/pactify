# Changelog

## Unreleased

- Add a privacy policy (`PRIVACY.md`) and a Data handling section to the plugin
  README: pactify has no telemetry or remote server, and briefs and code reach a
  provider only through the user's own harness CLI. Translate the remaining
  Chinese docs (status and cost contracts, Jobs view, web README) and code
  comments to English, make plugin links absolute so they work in an installed
  plugin, and add a marketplace description.

- Redesign the dashboard and read-only panel with the Pactify green palette,
  self-hosted Space Grotesk, shared status and live indicators, and persisted
  manual dark/light themes. Make production Lines the default Jobs view, with
  project/harness grouping, four stations, a sticky line rail, a phone station
  pager, Shipped expansion and station keyboard navigation. Retain the secondary
  List view, filters, job details, dashboard action confirmations and GET-only
  panel commands. Add a KPI-first dashboard and sectioned Settings with Appearance
  and Shortcuts; preserve storage cleanup and routing behavior. Keep phone
  stations aligned to the pager, disclose filters from the compact toolbar,
  align compact history cells, and use themed drawer scrims. Preserve the exact
  defaults-save confirmation and show storage locations with actual cache caps.

- Add conservative storage lifecycle management: 14-day finished-chain retention,
  protected stale worktrees, a 5 GB cache cap with LRU eviction to 80%, private
  session/log cleanup, and periodic background maintenance. Add `pactify sub prune`
  with a dry-run default, JSON, retention/cache overrides and explicit worktree
  discard. Delete eligible Codex threads through app-server so Remote Control
  stops listing them. Show cached storage estimates, policy and run results in
  Settings and doctor; add a reviewed dashboard cleanup action while keeping
  the local panel GET-only. Store policy/status in home-level JSON files.
  Summarize large plans by reason (`--verbose` lists all items), isolate native
  databases, bound job-start scheduling and lock waits, and recover interrupted
  removals through trash. Verify the real OpenCode v1 path contract, preserve
  user-config environment selection, bound repeated state scans, and count native
  session removals separately from measured database changes. Keep retry receipts
  outside mirrored homes and protect chains referenced by damaged metadata.
  Pruned jobs are removed from cost history.
- Promote opencode 1.x and 2.x to supported harnesses across CLI results, doctor,
  catalogs and plugin guides. Preserve no-OS-sandbox warnings, the file-tool
  directory guard and the shell-not-confined limitation; agy stays experimental.
  Support 1.x while `opencode-ai` publishes 1.x releases and its drift/contract
  check stays green; pin 2.x by the reviewed contract.
- Check v1 apply_patch permissions against every absolute `metadata.files`
  filePath and movePath, including recovered requests and subdirectory jobs.
  Fail closed on malformed file lists and paths.
- Make the parallel E2E isolation check ignore trailing whitespace in same.txt;
  wrong job contents, missing files and shared worktrees still fail.
- Allow the opencode plugin skill to be model-invoked on an explicit user
  delegation request, like kimi. Only agy requires an explicit slash command.
  Bump the plugin minor version to 0.4.0.

- Restrict opencode write-mode external directory permissions to the job workspace,
  job TMPDIR and Pactify cache, with native rules and adapter checks on both majors.
  Ask before in-root directory access so symlinks reach the adapter checks; use
  actual file paths from both API versions and preserve recovered requests.
  Support literal bracketed filenames and symlinked temp paths; prefer absolute
  v1 tool paths when worktree-relative permission patterns use a different base.
  Deny unknown permissions and doom loops; verify write policies in doctor and add
  an opt-in external-directory E2E scenario with informational shell results.
  Opencode still has no OS sandbox;
  shell commands can reach files beyond the directory checks.

- Make the plugin Claude Code only with a local prerequisite check, require explicit
  user requests for delegation, and restrict the experimental agy skill to
  slash commands with documented permission boundaries. Rename `/codex`, `/kimi`,
  `/agy`, `/opencode` to `/delegate-codex`, `/delegate-kimi`, `/delegate-agy`,
  `/delegate-opencode` (namespaced forms such as `/pactify:codex` become
  `/pactify:delegate-codex`). `/delegate` and `/jobs` keep their names.

- Reduce the local panel bundle by removing dashboard write dialogs and routing
  editing code at build time. Subset self-hosted fonts; add Jobs keyboard
  shortcuts and help, polling-driven status favicons, a two-plus-three mobile
  KPI layout, and a helpful panel page for rejected browser Hosts.

- Share local panel sessions with a second tab on the same origin. Pause hidden
  polling and back off on errors; show relative update times and attention counts.
  Add coloured, collapsible file diffs with stats and full-diff commands, broader
  job search, an attention filter, compact completed history, and per-job costs.
  Show stored doctor/version information, documentation and read-only guidance.
  Preserve panel sessions on reload and keep a manual detail-tab selection when
  the initial data arrives. Align compact history columns and simplify diff file
  paths; show one command list per viewport, with full desktop footer commands
  and a mobile expander.

- Discover opencode 1.x model effort variants for Settings and validate each
  requested effort against its model before prompting. Leave actual effort
  unknown when upstream does not report it. Verify read-only E2E evidence for
  both opencode majors through the saved session policy; pin 2.x drift CI to 2.1.0.
- Make the local job panel follow the system theme, improve text contrast, and
  provide two primary CLI next steps on cards, full instructions in job details,
  onboarding, loading and session error screens.
  Keep task titles consistent across run details and open active runs on Live.
  Pin detail actions outside the content scroll area; keep the panel's full CLI
  command list in that content with a compact Next steps footer. Constrain the
  fullscreen mobile drawer to the dynamic viewport and respect safe areas.
- kimi is no longer experimental.
- kimi supports read-only jobs: a separate private home with a generated config
  that keeps only model/provider settings and enables read-only tools for every
  agent; per-agent tools snapshots are verified after the job, with a
  fail-closed repository baseline. Early failures and cancellations keep their
  class and quota fallback; identical configs allow concurrent jobs; the
  generated config is removed after the last job; resume keeps the access mode.
- Adopt FSL-1.1-MIT: each version becomes MIT after two years; releases through
  v0.13.0 retain their original MIT license.
- Harden `serve` with Host validation, exact browser write-origin checks and
  configurable proxy origins. Non-loopback listeners require a bearer token;
  when configured, the token protects every API request, including reads and SSE.
  Add security headers and connection timeouts; align the development proxy.
- Classify an unknown agy model as a model failure; always create agy's private
  data directories so a logged-out agy never writes to the real `~/.gemini`; stop
  explicit agy models from inheriting the default effort; warn when a job's
  worktree HEAD moved off its base commit or new branches appeared while it ran.
- Remove private development material and personal settings from the public tree.
  Use native Codex/Kimi doctor defaults, prefer configured opencode models and
  allow live-test model policies to be configured through environment variables.

## v0.13.0 — 2026-10-20

- Introduce the harness router with codex as default and experimental kimi, agy
  and opencode support.
- Add background jobs, resume, cancel, logs, worktree isolation, quota fallback
  and `@alias` routing through the CLI and Claude Code plugin.
- Provide `sub doctor`, the local read-only `sub panel`, and a `serve` dashboard
  with Jobs, Settings and live job details.

Earlier history is not published.
