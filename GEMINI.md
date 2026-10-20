# Pactify — GEMINI contributor guide

Pactify is a local harness router for coding tasks. The CLI and Claude Code plugin
route jobs to codex by default, with kimi support and experimental agy and opencode support.
Jobs provide logs, results, resume, cancellation, worktree isolation and quota fallback.
Codex write mode enables network access by default.

## Repository layout

- `cmd/pactify/`: Go CLI entry point and commands.
- `internal/`: harness adapters, job lifecycle, routing, local HTTP server and panel.
- `web/`: React dashboard, read-only panel and frontend tests.
- `plugins/pactify/`: Claude Code skills, forwarding agents and hooks.
- `tests/`: Bats CLI and plugin checks.
- `docs/`: public product and contributor documentation.

## Build and test

Use the Go version in `go.mod` and Node.js with npm for frontend development.

```sh
go build ./...
go vet ./...
PACTIFY_HOME="$(mktemp -d)" go test ./... -count=1
PACTIFY_HOME="$(mktemp -d)" go test -race -count=1 ./internal/serve/... ./internal/sub/...
go vet -tags harness_e2e ./internal/sub/e2e
test -z "$(gofmt -l cmd internal)"
(cd web && npm ci && npx tsc --noEmit && npx vitest run)
(cd web && npx playwright test --retries 0)
bats tests/
```

The live harness E2E suite is opt-in and can consume model quota; see its
[guide](internal/sub/e2e/README.md). Ordinary tests use isolated state and fakes.

## Contributor conventions

- Keep changes focused, preserve unrelated work and follow existing patterns.
- Add regression coverage for behavior changes and run relevant checks. Report
  failed or unrun checks accurately; never weaken tests to hide a regression.
- Format Go with `gofmt`. Keep public documentation and diagnostics in English.
- Web changes require TypeScript, Vitest and Playwright checks. Inspect screenshots
  for visual changes. Use `npm run dev` in `web/` for hot reload through the local
  server proxy (`PACTIFY_SERVE_URL`, default `http://127.0.0.1:7777`).
- Rebuild tracked embedded assets after web changes: run `npm run build` and
  `npm run build:panel` in `web/`, then rebuild the Go binary. Dashboard assets
  live in `internal/serve/dist/`; panel assets live in `internal/subpanel/dist/`.
- Bump the patch version in `plugins/pactify/.claude-plugin/plugin.json` whenever
  changing anything under `plugins/pactify/`. Keep the five SHARED-DELEGATION
  blocks identical and the three entry guides identical except their first line.
- Record user-visible changes under Unreleased in [CHANGELOG](docs/CHANGELOG.md).
- Keep credentials, personal settings and machine-specific deployment details out
  of tracked files. Private Claude Code instructions belong in `CLAUDE.local.md`.

## Documentation

[Architecture](docs/architecture.md) · [Installation](docs/deployment.md) ·
[Operations](docs/operations.md) · [Jobs dashboard](docs/jobs-view.md) ·
[Routing](docs/router.md) · [Status](docs/sub-status.md) ·
[Costs](docs/sub-cost.md) · [Doctor](docs/sub-doctor.md) ·
[Upstream compatibility](docs/harness-updates.md) ·
[Plugin](plugins/pactify/README.md)
