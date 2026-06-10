# Phase 3 — Pact-Squad Visual Orchestration — Design

- **Date:** 2026-06-10 · **Status:** approved (brainstorm) — M3.1+M3.2 in depth, M3.3/M3.4 sketched
- **Depends on:** Pact-Base complete (protocol v1, engine, serve+SSE, web/ dashboard, MCP)
- **Commercial frame:** ADR-001 — Squad main features free; paid features deferred ("后定义"), **no gating infrastructure this phase** (YAGNI).

## Decisions locked in brainstorm

1. **UI carrier: extend the existing `web/` app, embedded in `pactify serve`.** One
   command, one binary; SSE/registry/embed all reused. The dashboard grows into Squad.
2. **Write-path actor: a serve-configured acting seat.** `pactify serve --seat <id>`
   (fallback `PACT_AGENT_ID`). Every UI write is logged as that seat; the UI header
   permanently shows "acting as `<seat>`". The seat must exist in the target project's
   roster (fail closed per request); engine rules apply unchanged (if the acting seat
   owns a task, the engine rejects its accept — §1 holds for humans too).
3. **Task dependencies: additive optional `deps` field** on the assign payload
   (forward-compatible under protocol v1's unknown-field rule). The canvas draws real
   DAG edges; the engine **enforces** the gate: a task cannot be `join`ed while any of
   its deps is not yet accepted (enforced, not prompted — but it is task-level gating,
   not a third numbered rule).
4. **Scope tonight: M3.1 canvas + M3.2 dispatch, all free, no gating.**

## Architecture

### A. Dir-aware engine (prerequisite refactor)

`internal/pact` verbs are cwd-bound today (`paths.Dir()` relative, `gitx` with `"."`).
serve hosts multiple registered projects, so the write path needs engine entry points
that take an explicit project dir:

- Introduce `pact.Project` (a small struct holding `dir`) with methods mirroring the
  verbs: `p.Assign(...)`, `p.Accept(...)`, `p.Changes(...)`, `p.Merge(...)`,
  `p.Checkpoint(...)`, `p.Join(...)`, `p.Status()`, `p.Validate()`. Existing package
  funcs become thin wrappers over `pact.At(".")` — zero behavior change for CLI/MCP/bats.
- `paths` gains dir-aware helpers (`paths.LogIn(dir)` etc. or the Project carries them);
  `gitx` already takes a dir argument everywhere — pass the project dir instead of `"."`.
- **Concurrency:** one mutex per registered project around mutating verbs (git ops are
  not concurrent-safe); reads (existing serve API) stay lock-free as today.

### B. `deps` protocol extension (additive, v1)

- `assign` payload gains optional `deps: ["T1", ...]` (same-feature task ids only).
- Validation: every dep must reference an existing task in the same feature; no cycles
  (DFS check at assign time); unknown fields remain ignored by old implementations —
  protocol_version stays 1. JSON Schema: add the optional property.
- Projection: STATE task entries render `deps: [..]` **only when present** — logs
  without deps stay byte-identical to the bash renderer (interop suite unchanged).
  The bash reference is NOT extended this phase; documented as a Go-first additive
  feature (`docs/specs/pact-protocol.md` gains a §addendum describing `deps` and the
  join gate).
- Enforcement: `join` on a task whose deps are not all `accepted` → engine error
  naming the blocking dep. (`checkpoint` needs no extra gate — you can't checkpoint
  what you couldn't join.)

### C. serve author API (M3.2 backbone)

New authenticated-by-locality endpoints (localhost serve, same trust model as today):

```
POST /api/projects/{name}/tasks         {id, spec_md}            → writes .pact/tasks/<id>.md
POST /api/projects/{name}/verbs/assign  {task, feature, branch, owner, reviewer, spec, deps[]}
POST /api/projects/{name}/verbs/accept  {task}
POST /api/projects/{name}/verbs/changes {task, reason}
POST /api/projects/{name}/verbs/merge   {feature}
GET  /api/acting-seat                   → {seat}   (and per-project roster validation state)
```

- All verbs execute via the dir-aware engine as the acting seat; engine errors map to
  HTTP 422 with the engine message verbatim (the UI shows it raw — pact's errors are
  the product voice).
- SSE already broadcasts on log change → the UI needs no extra refresh wiring.
- `checkpoint`/`join` are NOT exposed in the UI (they're worker moves; Squad dispatches
  and reviews). The API surface stays minimal.

### D. M3.1 — orchestration canvas (web/)

- New **Canvas** view alongside the kanban (React Flow, MIT, the one new npm dep).
- **Nodes:** task nodes (status chip + owner/reviewer seat chips) grouped by feature;
  seat nodes in a side rail. Node accent color = owner seat's primary protocol role
  mapped through the brand system (worker→green/dev, orchestrator→yellow/product,
  reviewer→blue/design — roles come from the roster, personas are presentation,
  per docs/brand.md).
- **Edges:** deps edges (drag task→task creates a dep on the next assign; existing
  deps rendered as arrows). Feature grouping via React Flow group nodes.
- **Build mode:** create feature (id+branch), create task (id + spec markdown editor →
  POST /tasks), wire deps, then dispatch (M3.2). Until assigned, draft tasks live only
  in the canvas's local state (the log stays protocol-pure).
- **Layout persistence:** node positions in `.pact/squad/layout.json` — a UI sidecar,
  explicitly NOT protocol surface (validate ignores it; documented in the addendum).
- Live updates: the existing SSE stream patches node status in place.

### E. M3.2 — dispatch + tracking

- **Drag-to-dispatch:** drop a draft/unassigned task onto a seat → assign modal
  (owner prefilled = drop target; reviewer picker enforcing owner≠reviewer; branch
  prefilled from feature; deps from canvas edges) → POST assign.
- **Review flow:** awaiting_review tasks pulse (role-blue); Accept / Request-changes
  buttons (changes requires a reason); Merge button per feature, disabled until all
  tasks accepted (engine remains the authority — the button state is UX, the 422 is law).
- **Checkpoint reminders:** SSE-driven toast when a task enters awaiting_review;
  stale-task indicator (in_progress with no event for >N min, default 30, pure UI).
- Kanban view stays; canvas and kanban are two views over the same store.

## M3.3 / M3.4 (sketch only — later milestones)

- M3.3 comms visualization: who-waits-on-whom graph derived from the log (blocked
  chains, idle seats), event-flow replay scrubber.
- M3.4 relay interface: an export hook on the serve watcher (event → HTTP POST to a
  configurable relay endpoint) — the seam Pact-Team's cloud plugs into. Design later.

## Testing

- **Engine refactor:** unit tests for `pact.At(dir)` verbs from a foreign cwd; full
  existing suite must stay green untouched (wrappers preserve behavior); doctor/serve
  reuse where applicable.
- **deps:** schema validation tests; assign-time validation (missing dep / cross-feature
  / cycle); join-gate test (blocked until dep accepted); projection render-only-when-
  present; interop byte-parity suite unchanged (deps-free logs).
- **serve author API:** httptest against temp git repos — acting-seat validation
  (unknown seat → 403/422), each verb happy path + engine-rule rejection passthrough
  (self-accept via API → 422), task-file write, per-project mutex sanity.
- **web:** vitest for the canvas store (nodes/edges derivation from STATE DTO, dispatch
  payload building, layout persistence round-trip); component smoke for Canvas view.
  React Flow rendering itself is not deep-tested (library trust).
- **bats:** serve author API curl coverage in `tests/serve.bats` style (minimal).
- CI: existing `test` job covers all (Go+bats+vitest). No new jobs.

## Out of scope (this phase)

Paid-feature gating · M3.3/M3.4 implementation · multi-user/auth (serve stays
localhost-trust) · bash reference deps support · cross-feature deps · canvas mobile UX.
