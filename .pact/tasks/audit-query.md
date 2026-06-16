# Task: audit query + summarize

Implement **Task 2** of the plan `docs/superpowers/plans/2026-06-16-native-audit-layer.md`.

## What to build
Append to `internal/audit/audit.go` and `internal/audit/audit_test.go` exactly as
shown in the plan's **Task 2**: `Filter` (with `match`), `Query` (folds day-files,
newest-first, skips torn lines), and `Summary` + `Summarize`. Use the plan's code
verbatim — types must match Task 1's `Record`.

## Rules
- Do NOT modify any other package.
- Depends on Task 1 (`Record`, `home`, `storePath` already exist).
- `Query` returns nil (not an error) when the project has no audit dir yet.

## Acceptance (reviewer re-runs this)
verify: go test ./internal/audit/
Also must not break the build: `go build ./...`
