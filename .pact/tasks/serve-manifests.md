# Task: serve endpoints for custom-agent manifests (Phase C)

Implement **Phase C / Task C1** of the plan
`docs/superpowers/plans/2026-06-17-custom-agent-manifest.md` — read it for the
exact code.

## What to build
- Create `internal/serve/manifests.go` with `registerManifestRoutes` +
  `handleManifestList` (GET) + `handleManifestCreate` (POST, 422 on invalid) +
  `handleManifestDelete` (DELETE). Use the code in the plan's Task C1 verbatim.
- Create `internal/serve/manifests_test.go` (the plan's Task C1 test) — covers
  POST valid → file written, POST invalid → 422, GET list, DELETE.
- Wire `s.registerManifestRoutes(mux)` next to `s.registerAuditRoutes(mux)` in
  `internal/serve/api.go`.

The backend already has `internal/agentmanifest` (Load/Install/Remove) — use it.

## Rules
- Do NOT modify any other package.
- POST body is raw TOML (text/plain); on invalid manifest return HTTP 422 with the
  error message via the existing `writeErr` helper.

## Acceptance (reviewer re-runs this)
verify: go test ./internal/serve/ -run Manifest
Also must not break the build: `go build ./...`
