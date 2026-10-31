# Changelog

## Unreleased

- Dismiss now works on any finished job with a pending worktree, so those cards can be closed from the dashboard. The worktree stays pending and untouched.

- Jobs replaces the always-empty Queued station with In review, for finished jobs whose delegating session has not yet applied or discarded the worktree. Queued jobs count as Running.

- Quota fallback now stays on the same harness. Existing cross-harness entries in `router.json` are ignored and reported by `pactify sub router`; the dashboard refuses to save them.

- Job titles skip generic headings such as "Background" and use the next line of the brief.

- Dashboard segmented controls slide between options, and canceling a running job takes two confirmations.

- Job cards show the status, one harness/model label, one duration and the diff, with larger text and no run id. Finished jobs read Done; a pending worktree no longer counts as Needs you, and the dashboard no longer offers Apply or Discard, leaving those to the delegating session or the CLI.

- Dashboard counts use smaller numbers with larger labels, show In progress in blue in both themes, and the dot matrix beside them flows left to right while a job runs.

- The Live session log no longer cuts its top row in half when it follows the newest output.

- Restyle the dashboard and read-only panel with a light terminal palette, neutral dark theme and shared controls.

- Prioritize active Jobs lines, collapse quiet lines with a remembered Active/All lines choice, and add Dismiss for handled failures and warnings.

- The Claude Code plugin manifest lists keywords and its documentation, support and
  privacy links for the plugin directory, and the plugins and marketplaces name
  Pactify as author (plugin 0.6.1).
- Prepare the Claude Code plugin for the plugin directory (plugin 0.6.0):
  - **Breaking:** the forwarding subagents are renamed `pactify:sub-codex`,
    `pactify:sub-kimi`, `pactify:sub-agy` and `pactify:sub-opencode`, so they are not
    mistaken for the harnesses' own agents. Update any instructions that name the old
    `pactify:codex`-style agents. Slash commands and CLI commands are unchanged.
  - The plugin no longer installs a SessionStart hook. Skills run `pactify-check --quiet`
    once before the first pactify command in a session instead.
  - The plugin ships a listing icon, links its homepage to pactify.dev, and its README
    adds example use cases, troubleshooting and support sections. Instructions for
    other hosts now live only in the main README.
  - The install instructions add the marketplace from
    `https://pactify.dev/marketplace.json`. Existing marketplaces added from the GitHub
    repository keep working.
- The dashboard header and the dashboard and panel favicons use the official pactify mark.
- Correct how the plugin README and the uninstall guide describe credentials: logins
  are linked as directories or single files, kimi read-only renders a private config
  that can hold provider API keys, and an opencode 2.x login lives in
  `$PACTIFY_HOME` (plugin 0.5.6).

## v0.14.0 — 2026-10-25

kimi and opencode leave experimental and kimi gains a read-only mode; job storage gets a
lifecycle with `pactify sub prune`; the Codex, Copilot and Cursor plugins and the
`.agents/skills` folders ship alongside the Claude Code plugin; and pactify.dev becomes the
install address.

- The portable plugins and skills require pactify 0.14.0, the first release with
  `pactify sub run --brief-file` (plugin 0.5.5).
- Install with `curl -fsSL https://pactify.dev/install.sh | sh`. The README, the
  installation guide, the installer's usage line and the plugins' missing-CLI hint now
  point to pactify.dev instead of the raw GitHub URL; the installer still downloads
  the release from GitHub and verifies its checksum (plugin 0.5.4).

- `pactify sub <unknown>` now reports `unknown command` with suggestions and a
  `--help` hint (exit 2) instead of `accepts 0 arg(s), received 1`. The
  `--read-only` help text describes the harness's own read-only mechanism instead
  of "Deny all permission requests" (AJ-844).
- `sub result`, `sub wait` and `sub run` exit 2 for a model failure only when the
  result's `failureClass` is `model`; the error text is no longer matched. Model
  refusals recognized by classification, such as codex "model is not supported when
  using codex with a chatgpt account" and agy "invalid model selection", now exit 2
  instead of 1. Other failures of a job started with `--model` (for example auth or
  quota) now exit 1 instead of 2. A codex server that starts a different model than
  requested is classified as `model` (AJ-844).
- Write jobs without `--worktree` print a note on stderr that they edit the directory
  in place (not with `--json`, whose callers may merge stderr into parsed output) (AJ-845).
- Document upgrade and uninstall steps for the CLI, `$PACTIFY_HOME` (including the
  credential quarantine folder) and host plugins (AJ-846).

- Refuse `pactify sub run` and `pactify sub doctor` inside a job. Harnesses run with
  `PACTIFY_JOB` set, and a delegated opencode job that found a committed pactify skill
  delegated twice more. Both commands (including `--resume` and `--worktree-from`) now
  exit 2 there; read-only commands still work and harness-native subagents are
  unaffected. This is a guard against accidental recursion, not a security boundary.
  The skills say so too (plugin 0.5.3). CLI test packages clear `PACTIFY_JOB` (AJ-842).
- Discard a worktree whose original directory is gone, for example a job started
  inside another job's worktree. Discard finds the main repository through the
  worktree's `.git` file and ignores an enclosing repository; without a repository
  it removes only the job's own `jobs/<id>/wt` directory through the anchored store
  root, marks the job discarded and notes the leftover git metadata. Apply reports
  the missing directory instead of a git chdir error (AJ-843).

- Add `pactify sub run --brief-file <path>` to read the brief from a file instead of
  stdin. Codex applies command rules such as `prefix_rule(pattern=["pactify", "sub"])`
  only to simple commands, so a heredoc launch ran inside its sandbox and could not
  create the job. The portable skills now write the brief to a temporary file and run
  each `pactify sub` command as one simple command, use `--timeout 540s` (the
  documented `540` was rejected as a duration), and note read-only worktrees in
  host sandboxes (plugin 0.5.2).

- Generate every plugin distribution from one host-agnostic source in
  `plugins/src` (`go run ./internal/plugingen/cmd/plugingen`; CI runs `-check`).
  The Claude Code skills are unchanged byte for byte. New outputs: an Agent
  Plugins 1.0 bundle for Codex, Copilot and Cursor in `plugins/agent-plugin`
  (no hooks or forwarding agents; waits by polling `pactify sub wait --timeout`),
  a plain `.agents/skills` copy in `plugins/agents-skills`, and a reproducible
  `pactify-delegate.zip` for Agensi attached to each release. The plugin (0.5.0)
  now checks for pactify CLI 0.13.0 or newer and prints an upgrade hint.
  Repository-root host marketplaces point into the bundle, because Git
  marketplace hosts read the repository root even with a sparse checkout
  (plugin 0.5.1).

- Make the first run work cleanly. `pactify sub run` checks the harness CLI is on
  PATH before creating a job or worktree and exits 2 with a doctor hint when it
  is missing. `pactify sub doctor` without arguments checks only installed
  harnesses, lists missing ones as SKIP and marks experimental ones. `go install`
  builds report their module version and VCS revision instead of `dev`. Bare
  `--resume` matches the working directory by identity, so case or symlink
  spellings on macOS find the previous session. install.sh downloads from the
  GitHub `releases/latest/download` URL instead of the rate-limited API, and
  release tags now run the test suite before goreleaser.

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
