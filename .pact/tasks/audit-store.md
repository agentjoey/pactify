# Task: audit store (Record + Append)

Implement **Task 1** of the plan `docs/superpowers/plans/2026-06-16-native-audit-layer.md`.

## What to build
Create `internal/audit/audit.go` and `internal/audit/audit_test.go` exactly as
shown in the plan's **Task 1** (the `Record` struct, `home`/`dayOf`/`storePath`
helpers, and `Append`). Use the test code and implementation code from the plan
verbatim — they are complete and consistent with later tasks.

## Rules
- Do NOT modify any other package.
- The store path is `~/.pactify/audit/<project>/<YYYY-MM-DD>.jsonl`, honoring the
  `PACTIFY_HOME` env override (tests rely on it).
- `Append` is best-effort: returns an error, never panics.

## Acceptance (reviewer re-runs this)
verify: go test ./internal/audit/
Also must not break the build: `go build ./...`
