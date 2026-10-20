# Changelog

## Unreleased

- Make the plugin Claude Code only with a local prerequisite check, require explicit
  user requests for delegation, and restrict experimental agy/opencode skills to
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
