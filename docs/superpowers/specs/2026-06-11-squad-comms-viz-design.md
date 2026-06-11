# M3.3b — Squad Comms Visualization (waits overlay + replay scrubber) — Design

- **Date:** 2026-06-11 · **Status:** approved (brainstorm)
- **Origin:** Phase 3 spec sketch — "comms visualization: who-waits-on-whom graph derived
  from the log (blocked chains, idle seats), event-flow replay scrubber".
- **Depends on:** M3.1/M3.2 (canvas, author API) + M3.3a (ops panel) merged.

## Decisions locked in brainstorm

1. **Carrier: canvas overlay, not a new view.** A "comms" toggle on the canvas draws
   wait edges (with reason chips) over the EXISTING node layout and dims idle seats /
   highlights blocked chains. The replay scrubber is a bottom bar usable in BOTH
   kanban and canvas. No fourth TopBar view; seat positions stay single-sourced.
2. **Replay: full state time-travel.** The projection is a pure fold
   (`projection.Project(evs)`), so historical state = `Project(evs[:n])`. serve gains
   `?at=N` on the state endpoint + a lightweight timeline index endpoint. A "live"
   button returns to the SSE stream. Read-only; zero protocol changes.
3. **Live pulse: yes.** When an SSE event arrives (live mode only), the related task
   node and its wait edges pulse once in the actor's role color — the site's cable-pulse
   brand idiom in the product. Pure CSS animation, fully disabled under
   `prefers-reduced-motion`.

## §1 Wait-edge semantics (pure derivation, client-side)

Derived in `web/src/lib/comms.ts` from the existing `StateDTO` snapshot — **no engine
or protocol changes**. Edge = `{from, to, kind, reason, taskId}` where from/to are
seat ids (or task ids for dep blocks):

| Condition | Edge / marker | Reason chip |
|---|---|---|
| task `awaiting_review` | owner → reviewer | `awaiting review: <task>` |
| task `changes_requested` | reviewer → owner | `changes requested: <task>` |
| task has `deps` with any dep not `accepted` | task → each unmet dep task | `blocked by <dep>` |
| task owner/reviewer not in `agents` (never joined) | warning badge on the task + seat chip | `not joined` |
| joined seat owning no `assigned`/`in_progress` task and reviewing no `awaiting_review` | seat rendered dimmed | `idle` |

Blocked-chain highlight: tasks transitively blocked through dep edges get an amber
outline (walk the dep edges from each unmet dep — pure graph reachability, already
cycle-free by the assign-time DFS guard).

Visibility: comms overlay is a toggle button on the canvas toolbar (default off).
Overlay state is component-local (not persisted in layout.json — it's a lens, not
layout). Wait edges render as dashed, role-colored React Flow edges with a small
reason chip label; they are derived nodes/edges merged into `deriveFlow`'s output
when the toggle is on, never written back to the layout sidecar.

## §2 serve (read-only additions)

`/api/projects/{id}/events` is already the SSE stream — the new endpoints don't touch it:

```
GET /api/projects/{id}/timeline        → {total, events:[{n, ts, type, actor, task?, feature?}]}
GET /api/projects/{id}/state?at=N      → StateDTO folded from the first N events
```

- `timeline` reads `event.ReadAll` once; `n` is 1-based position; `task`/`feature`
  included when the event carries them (omitempty). No payloads (keep it light).
- `state?at=N`: `N` integer ≥ 0; `at=0` → empty-fold state; `N ≥ len(evs)` → full
  state (clamp, NOT an error — the scrubber's "end" equals live shape); malformed
  `at` → 400 `{"error":"..."}` (author convention); absent `at` → unchanged behavior
  (full state — existing handler path untouched for existing clients).
- Both read-only: no mutex needed beyond the existing read path (ReadAll is a
  point-in-time file read; same consistency model as today's handleState).

## §3 UI (web/)

- **ReplayBar** (new component, rendered under kanban AND canvas): a slider over
  `0..total` with the current event's `type/actor/ts` caption, ▶ step buttons
  (±1 event), and a **LIVE** button. Entering replay mode: fetch timeline, then
  fetch `state?at=N` per scrub position (debounced); SSE snapshots are ignored while
  scrubbing (live indicator shows "replay"). LIVE: refetch current state, resume
  applying SSE.
- **Replay mode is read-only**: all author mutations (dispatch, task editor, drag
  handlers, ops mutations) disabled/hidden while not live — guard in App passes
  `replaying` down; Canvas drag/drop short-circuits; DispatchModal unreachable.
- **Comms toggle** on the canvas toolbar: merges wait edges/markers from
  `deriveComms(state)` into the flow; legend chip row (waiting / blocked / idle).
- **Pulse**: on each applied SSE snapshot in live mode, diff identifies changed
  task(s) (status transitions — reuse the applyState diff that drives review toasts);
  those node ids get a transient `pulse` class (~900ms, role-colored box-shadow
  keyframe), removed on animationend. Disabled under `prefers-reduced-motion`
  (CSS `@media` gate — no JS branch needed).
- Kanban in replay shows the historical columns as-is (no extra affordance needed —
  the bar's caption carries the context).

## §4 Testing

- **serve:** httptest — timeline shape (n ordering, omitempty task/feature);
  `state?at=0` empty, `at=k` mid-log matches `Project(evs[:k])` DTO, `at` ≥ total
  clamps to full, `at=junk` → 400, no-`at` byte-identical to today; unknown project 404.
- **web vitest:** `deriveComms` pure-fn cases (one per table row in §1 + transitive
  block chain + clean state → no edges); ReplayBar interaction (scrub → fetch with
  `at`, LIVE → resume); replay-mode guard (mutations hidden/disabled); pulse class
  derivation from snapshot diff (pure helper).
- **bats:** `serve_comms.bats` — scripted repo with a few verbs → curl timeline
  (count + ordering), `state?at=1` shows only the first join, `?at=` full equals
  plain state.
- Existing suites (interop, schema, engine) untouched — this milestone writes no events.

## §5 Out of scope

M3.4 relay/export hook · cross-project comms · persisting replay position or overlay
toggle · layout versioning (replay reuses the CURRENT layout sidecar — historical
tasks missing from layout fall back to the existing collision-aware grid placement) ·
seat-to-seat messaging (visualization only) · bash reference changes (none needed).
