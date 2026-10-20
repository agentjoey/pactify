# Changelog

## Unreleased

- kimi is no longer experimental; read-only remains unavailable.
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
