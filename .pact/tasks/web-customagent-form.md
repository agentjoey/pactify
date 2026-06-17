# Task: Settings "Add custom agent" form (Phase D)

Implement **Phase D / Task D1** of the plan
`docs/superpowers/plans/2026-06-17-custom-agent-manifest.md` — read it for the
exact test + interfaces.

## What to build
1. `web/src/lib/api.ts`: add `ManifestRow` interface + `listManifests`,
   `createManifest(toml)`, `deleteManifest(kind)` (per the plan's Task D1 code).
   `createManifest` POSTs raw TOML to `/api/agents/manifests`; on non-2xx it throws
   the server's `{error}` message.
2. `web/src/components/ops/CustomAgentForm.tsx`: a form with fields kind / binary /
   entry / args (comma-separated) / default_model / models / mcp path/scope/format.
   On submit it assembles a TOML string and calls `createManifest`. On error it
   renders the message; on success it calls the `onCreated` prop.
   Required `data-testid`s: `ca-kind`, `ca-binary`, `ca-args`. The submit button
   text must contain "Add custom agent". The args CSV `"run,{briefing}"` becomes
   `args = ["run", "{briefing}"]` (split on comma, trim, JSON-quote each).
3. `web/src/components/ops/CustomAgentForm.test.tsx`: the plan's Task D1 vitest
   (posts assembled TOML on submit; shows the server field error).
4. Mount `<CustomAgentForm>` in `web/src/components/ops/OpsView.tsx` under a
   collapsible "Add custom agent" beside `AgentRoster`.

Match the existing component style/tokens (see `AgentRoster.tsx` / `Settings`
components). Use the `ui/` primitives (Button/Input/Select) where they fit.

## Rules
- Do NOT touch Go code or other web components beyond api.ts + OpsView mount.
- The Canvas工艺规约 gate is vitest + Playwright e2e double-green.

## Acceptance (reviewer re-runs this)
verify: cd web && npm test -- CustomAgentForm
Also: `cd web && npx tsc --noEmit` clean, and the full `cd web && npm test` stays green.
