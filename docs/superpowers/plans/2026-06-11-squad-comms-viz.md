# Squad Comms Visualization Implementation Plan (M3.3b)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Who-waits-on-whom overlay on the canvas + full-state replay scrubber under kanban/canvas + live event pulse — all read-only, zero protocol changes.

**Architecture:** Bottom-up: (C1) serve read-only endpoints (timeline + `state?at=N` prefix fold); (C2) pure derivation libs in web (`comms.ts` wait edges, pulse diff helper); (C3) ReplayBar + replay-mode read-only guards in App; (C4) canvas comms overlay + pulse CSS; (C5) bats e2e + docs + dist + PR. The spec (`docs/superpowers/specs/2026-06-11-squad-comms-viz-design.md`) §1 table and §2 endpoint contracts are NORMATIVE — implement exactly those semantics. Conventions established: api.go/author.go handler + error shapes, dto.go `toDTO`, web lib pure-fn + vitest patterns (`derive.ts`/`canvas.ts`), App.applyState diff (drives review toasts — pulse reuses the same diff point).

**Branch:** `feat/squad-comms` (spec committed here).

---

### Task C1: serve — timeline endpoint + `state?at=N`

- [ ] TDD (`internal/serve/timeline_test.go`, mirror api_test fixtures): GET timeline on a scripted log → `{total, events:[{n,ts,type,actor,task?,feature?}]}` with n 1-based ordered, task/feature omitempty; unknown project 404. GET `state?at=0` → empty fold (project name only after init? assert equals `toDTO(projection.Project(evs[:0]))`); `at=k` mid-log equals `Project(evs[:k])` DTO; `at` ≥ total clamps to full; `at=junk` → 400 `{"error"}`; NO `at` param → byte-identical JSON to current handler output.
- [ ] Implement `internal/serve/timeline.go`: `GET /api/projects/{id}/timeline` via `event.ReadAll(logPath(root))`; DTO in dto.go. Extend `handleState`: parse optional `at` (strconv; <0 or non-int → 400 via the author `writeErr` convention), slice prefix before fold — factor `ProjectStateAt(root string, at int)` next to `ProjectState` (ProjectState calls it with -1 = all).
- [ ] Gate: `go test ./... -race ./internal/serve/`. Commit: `feat(serve): timeline index + state?at=N prefix fold (replay, read-only)`

### Task C2: web — `lib/comms.ts` derivation + pulse diff helper

- [ ] TDD (`web/src/lib/comms.test.ts`): one case per spec §1 table row — awaiting_review → owner→reviewer edge; changes_requested → reviewer→owner; unmet dep → task→dep edge + transitive amber chain (A blocked by B blocked by C: A and B both flagged); owner not in agents → notJoined marker; joined seat with no active work → idle; clean state → empty result. Pulse helper: prev/next StateDTO diff → changed task ids + actor role color var (reuse `roleColorVar`).
- [ ] Implement `deriveComms(state: StateDTO): {edges: WaitEdge[], idleSeats: string[], notJoined: string[], blockedTasks: string[]}` and `pulseTargets(prev, next): {taskIds: string[]}` — pure, no React.
- [ ] Gate: `npm test` in web/ (existing suites green). Commit: `feat(web): comms derivation — wait edges, blocked chains, idle/not-joined, pulse diff`

### Task C3: web — ReplayBar + read-only replay mode

- [ ] api.ts: `getTimeline(id)`, `getStateAt(id, at)` (error-verbatim pattern); types.ts additions.
- [ ] `components/ReplayBar.tsx`: slider 0..total + caption `#n type · actor · ts` + ±1 step buttons + LIVE button. App: `replayAt: number | null` state; non-null → fetch `state?at` (debounce ~150ms) and IGNORE SSE-applied snapshots; LIVE → null, refetch live state, resume SSE application. Bar rendered under kanban AND canvas (not ops). Live indicator shows "replay" while scrubbing.
- [ ] Read-only guards: `replaying` prop — Canvas drag/drop/dispatch short-circuit, TaskEditor/DispatchModal/draft creation hidden, ops untouched (ops not under replay).
- [ ] vitest: ReplayBar interactions (scrub → getStateAt called with at; LIVE → live state restored); guard test (replaying → mutation affordances absent). Keep all existing green.
- [ ] Gate (`npm test`, `tsc -b`). Commit: `feat(web): replay scrubber — full-state time travel with read-only mode`

### Task C4: web — canvas comms overlay + live pulse

- [ ] Canvas toolbar toggle "comms" (default off, component-local state): when on, merge `deriveComms` output into the flow — dashed role-colored wait edges with reason chip labels (edge ids `wait:${from}→${to}:${taskId}` to avoid colliding with `dep:` edges), idle seats dimmed (CSS class), blockedTasks amber outline, notJoined warning badge; legend chip row (waiting / blocked / idle). Derived only — NEVER written to layout.json.
- [ ] Pulse: in App.applyState live-mode diff, compute `pulseTargets(prev, next)` → transient set passed to Canvas/Board; node gets `pulse` class with role-colored box-shadow keyframe (~900ms), removed on `animationend`. CSS gated: `@media (prefers-reduced-motion: reduce){ .pulse{animation:none} }`.
- [ ] vitest: toggle merges wait edges into deriveFlow output (fixture with awaiting_review); pulse class derivation already covered in C2 — here a smoke that Canvas applies the class. jsdom stubs per Canvas.test.tsx (ResizeObserver/DOMMatrix seeds established).
- [ ] Gate (`npm test`, `tsc -b`, `npm run build` + commit dist — REQUIRED, go:embed ships dist). Commit: `feat(web): comms overlay + live pulse — dist rebuilt`

### Task C5: e2e + docs + PR

- [ ] `tests/serve_comms.bats`: scripted repo (init + join + assign + checkpoint via CLI) → curl timeline (total + n ordering), `state?at=1` shows only first event's effect, plain state equals `?at=<total>`.
- [ ] docs/architecture.md comms paragraph; .agent sprint/CURRENT records.
- [ ] Full baseline (go test ./... -race serve, bats all, web npm test), verify dist committed (check-dist pattern: `strings` spot-check optional), PR `Squad M3.3b: comms visualization — waits overlay, replay scrubber, live pulse`, CI green → merge (authorized), pull main, rebuild `/opt/homebrew/bin/pactify`.

## Self-Review Notes
- Spec §1→C2/C4, §2→C1, §3→C3/C4, §4 distributed, §5 respected.
- Known seams: handleState backward-compat (no-`at` path byte-identical — C1 asserts);
  SSE-ignore while replaying must not leak into firstSnapshot toast guard (C3 — pass
  through applyState only when live); dist rebuild gate in C4 (PR #11 failure mode).
- Type consistency: `WaitEdge {from,to,kind,reason,taskId}` (spec §1) used in C2 and C4; `ProjectStateAt(root, at)` defined C1, used only there.
