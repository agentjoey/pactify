# Dashboard v2 Polish Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Re-skin + interaction-upgrade the embedded dashboard to the locked Linear-grade design: Indigo token system, ant-colony avatars, Office-mode canvas (default), ant-crawl edges, ⌘K, replay timeline, task detail panel.

**Architecture:** Pure web/ presentation+interaction work — ZERO serve/protocol changes (verified: PUT layout stores opaque JSON, so the new `office` sidecar key needs no backend). Three waves = three branches/PRs, each independently shippable. NORMATIVE sources: spec `docs/superpowers/specs/2026-06-11-dashboard-v2-polish-design.md` (exact tokens §1, caste table §2, office derivation rules §3, etc.) and the six validated mockup boards in `docs/superpowers/mockups/dashboard-v2/` — implementers MUST open the relevant board file and lift its markup/CSS/SVG as the starting point (the boards contain the actual approved SVG ant paths, card markup, palette/timeline/desk layouts). Do not re-design; translate boards → components + tokens.

**Tech Stack:** React 19 + TS + Tailwind v4 (`@theme` tokens) + @xyflow/react v12 + cmdk (W3, only new dep) + self-hosted Inter var & JetBrains Mono.

**Standing constraints (EVERY task):** `npx tsc -b` clean; `npm test` green and only growing (baseline 99); every commit that touches web/src ships a rebuilt `internal/serve/dist` in the SAME commit (`npm run build` then `git add -A`); all animation behind `prefers-reduced-motion`; existing testids preserved; replay read-only / observe-only / layout-lens invariants must keep their existing tests green.

**Shared type anchors (defined once, used across tasks):**
```ts
// web/src/lib/ants.ts (T3)
export type Caste = "queen"|"guard"|"builder"|"catcher"|"brewer"|"painter"|"scout"|"keeper";
export function casteForRoles(roles: string[]): Caste;           // spec §2 mapping+fallback
export function padGradient(seatId: string, caste: Caste): { from: string; to: string };
// web/src/components/ui/ants/Ant.tsx (T3)
<Ant caste={Caste} size={14|22|34|72} />                          // ≤18 uses bold-stroke variant
// web/src/lib/lifecycle.ts (T4)
export function lifecycleStage(status: string): 0|1|2|3;          // assigned/in_progress/awaiting|changes/accepted
// web/src/lib/office.ts (T9)
export type DeskStatus = "busy"|"review_due"|"waiting"|"idle";
export interface WaitingItem { task: Task; on: string; reason: "review"|"dep" }
export interface DeskModel { seatId: string; roles: string[]; status: DeskStatus;
  doing: Task[]; inbox: Task[]; waitingOn: WaitingItem[] }
export function deriveOffice(state: State): { desks: DeskModel[]; shipped: Task[] };
```

---

## Wave 0 — acceptance-blocker hotfix (branch `fix/canvas-ux-hotfix`, ships BEFORE W1)

### Task T0: canvas state survival + dispatch seat bug + draft auto-ids

**Files:** Modify `web/src/App.tsx`, `web/src/components/Canvas.tsx`, `web/src/components/TaskEditor.tsx`, `web/src/components/DispatchModal.tsx` (+tests).

User acceptance feedback (greet repo, blocked at dispatch): ① switching canvas→ops→canvas WIPES the board — drafts/draftFeatures live in Canvas component state (Canvas.tsx:189-190) and App unmounts Canvas on view switch (App.tsx:230-237); ② dispatch modal showed NO seats to choose although STATE.yml has two agents (verified: `~/AgentWorks/Code_Claude/pact-dogfood-squad/.pact/STATE.yml` lists claude-opus + opencode; roster wiring is Canvas.tsx:448 `state.agents` → DispatchModal roster) — REPRODUCE first (register that repo, open dispatch via button AND via drop) and fix the actual cause; check whether seat NODES also failed to render (user "没有坐席" may mean the canvas left rail); ③ draft forms demand hand-typed ids.

- [ ] Lift `drafts`/`draftFeatures` (and comms toggle if trivial) from Canvas to App state passed down as props, so view switches preserve them. vitest: render App-ish harness, create draft, unmount/remount Canvas, draft persists.
- [ ] Systematic repro of ②: fixture state with 2 agents → open DispatchModal via button → assert both options present (this test may PASS — then repro deeper: through Canvas with a draft inside a draft feature, through TaskEditor path, and with the REAL serve against the dogfood repo via manual check). Fix root cause; regression test pinned to whatever the cause was.
- [ ] Auto-ids: draft task form defaults to next free `t<N>` (scan existing+draft ids), feature form defaults `f<N>`; still editable; slug-validated as today. TDD the `nextId(existing: string[], prefix: string)` helper.
- [ ] Gate (tsc/test, dist rebuild+commit). Single PR `fix(web): canvas drafts survive view switches, dispatch roster fix, draft auto-ids — dist rebuilt`; CI green → merge (authorized).

## Wave 1 — foundation + cards + kanban + TopBar (branch `feat/dashboard-v2-w1`)

### Task T1: tokens + fonts + base styles

**Files:** Create `web/src/tokens.css`, `web/public/fonts/` (InterVariable.woff2, JetBrainsMono subset woff2 — download once, commit); Modify `web/src/index.css`, `web/index.html`.

- [ ] Add fonts (self-hosted @font-face, `font-display: swap`), `web/src/tokens.css` with EXACTLY spec §1 values as Tailwind v4 `@theme` vars (`--color-bg-page: #191B26` etc.) PLUS the legacy aliases (`--role-product/--role-design/--role-dev` re-pointed to the new desaturated hexes so existing components shift hue automatically).
- [ ] index.css: import tokens, body → `var(--color-bg-page)` + Inter stack, `.mono` utility → JetBrains Mono; keep ALL existing animation blocks (pact-awaiting, pulse, comms) but re-point their hard-coded colors to tokens.
- [ ] vitest: a `tokens.test.ts` asserting the css file contains the six locked hex values (regression pin against drift).
- [ ] Gate (tsc/test/build+dist). Commit: `feat(web): dashboard v2 tokens — Indigo palette, self-hosted Inter/JetBrains Mono — dist rebuilt`

### Task T2: ui/ primitives

**Files:** Create `web/src/components/ui/{Button,Input,Select,Badge,Kbd,Modal,Popover,Tooltip,EmptyState}.tsx` + `ui/ui.test.tsx`.

- [ ] TDD per component (render variants, disabled state, focus-visible ring class; Modal: Esc closes + focus trap + overlay token; Tooltip: 400ms delayed appear with fake timers; Popover: outside-click closes). Visual specs = board1/board3 (paddings, radii, 120/200ms transitions, `cubic-bezier(0.25,1,0.5,1)`).
- [ ] Replace hand-rolled controls in DispatchModal.tsx, TaskEditor.tsx, ops/{Projects,Wiring,Seats}.tsx with ui/ primitives (mechanical swap, keep behavior + testids; ops danger confirms move from inline confirm to `<Modal variant="danger">`). Wiring codex snippet gains an explicit Copy button (navigator.clipboard + "copied" flash) — acceptance feedback 1.2.
- [ ] Gate + dist. Commit: `feat(web): ui primitives — buttons/inputs/modal/popover/tooltip/badge/kbd, app-wide swap — dist rebuilt`

### Task T3: ant avatar system

**Files:** Create `web/src/components/ui/ants/Ant.tsx` (one component, 8 caste paths inlined), `web/src/lib/ants.ts`, tests `ants.test.ts(x)`.

- [ ] Lift the EIGHT approved SVGs from `docs/superpowers/mockups/dashboard-v2/board2-avatars-v6-ants.html` verbatim (each `<g transform="rotate(45 24 24)">…` block) into `Ant.tsx`; sizes 14/22/34/72; ≤18px renders the bold-stroke simplified variant (board3 kanban card markup has the 14px variants for builder/guard — lift those; derive the remaining six by raising stroke widths to 1.6/2 and dropping legs, matching that look).
- [ ] `casteForRoles` TDD: orchestrator→queen beats reviewer→guard beats worker→builder (priority for multi-role); keyword matching case-insensitive substring per spec §2 table (`qa|test→catcher`, `product|pm→brewer`, `design→painter`, `research→scout`, `ops|operation|devops→keeper`); unknown/empty→builder. `padGradient` TDD: deterministic per seatId (simple hash → lightness jitter ±6% on the caste's locked pad gradient from the board), stable across calls.
- [ ] Replace `roleColorVar`-driven letter avatars in Agents.tsx / SeatNode.tsx with `<Ant>` on pad gradient (keep roleColorVar for edges/chips — it now resolves to the new hexes via T1 aliases).
- [ ] Gate + dist. Commit: `feat(web): ant colony avatars — 8 castes, role mapping, seat-hash pad gradients — dist rebuilt`

### Task T4: TaskCard v2 (shared genome) + kanban re-skin

**Files:** Create `web/src/components/TaskCard.tsx`, `web/src/lib/lifecycle.ts` (+tests); Modify `web/src/components/Board.tsx`, `web/src/components/nodes/TaskNode.tsx`.

- [ ] `lifecycleStage` TDD: assigned→0, in_progress→1, awaiting_review→2, changes_requested→2, accepted→3.
- [ ] TaskCard per board3 markup: medallion (status icon ◇/⚡/◉/↺/✓ on status-color squircle) + id (mono) + title + feature chip; bottom row owner→reviewer `<Ant size={14}>` chips + lifecycle segments (done/cur glow via `--st`); 9% status-wash gradient background; awaiting soft glow class; draft dashed variant; stale amber dot prop; relative-time prop ("12m") — add `web/src/lib/reltime.ts` (`relTime(tsISO, now): string` TDD: <60s "now", minutes "Nm", hours "Nh", days "Nd").
- [ ] Board.tsx re-skin per board3: 5 column headers (dot+uppercase+count), accepted column wrapper opacity .82, empty-column ghost, awaiting glow, header tooltip "status flows through pact verbs". TaskNode.tsx renders TaskCard (canvas + kanban share the genome; keep node testids + pulse/comms class plumbing intact).
- [ ] Gate (ALL existing Board/Canvas tests green — adjust selectors only where markup moved, never semantics) + dist. Commit: `feat(web): task card v2 + kanban re-skin — medallion/wash/lifecycle, ant chips — dist rebuilt`

### Task T5: TopBar v2 + dynamic title/favicon + observe badge

**Files:** Modify `web/src/components/TopBar.tsx`, `web/src/App.tsx`, `web/index.html` (favicon); Create `web/src/lib/docTitle.ts` (+test).

- [ ] TopBar per board3: cable mini-mark SVG + wordmark; project chip (ui/Popover with search listing projects); centered segmented control with `<Kbd>1/2/3</Kbd>`; ⌘K hint button (no-op until W3, dispatches a custom event); live badge 3 states (live green breathing / ● replay amber / offline gray — reuse existing live+replaying props); acting-seat `<Ant size={22}>`; observe-only → "👁 observing" Badge with Tooltip explaining `pactify serve --seat <id>`.
- [ ] Global keys 1/2/3 switch views (App keydown handler, ignored while typing in inputs/modals).
- [ ] `docTitle(project, awaitingCount)` → `«greet» · 2 awaiting ●` applied in App effect; favicon = cable mark SVG data-uri in index.html.
- [ ] Gate + dist. Commit: `feat(web): topbar v2 — project switcher, segmented views, observe badge, dynamic title — dist rebuilt`

### Task T6: Wave-1 close

- [ ] Full baseline (go test ./... untouched-green sanity, web tsc+test, `strings internal/serve/dist/assets/*.js | grep -c "ant\|medallion"`-style spot check), PR `Dashboard v2 · Wave 1: design system, ant avatars, task cards, kanban + topbar`, CI green → merge (authorized), pull main, rebuild `/opt/homebrew/bin/pactify`.

## Wave 2 — Office mode + canvas re-skin + ant edges + detail panel (branch `feat/dashboard-v2-w2`)

### Task T7: Plan-canvas surface re-skin

**Files:** Modify `web/src/components/Canvas.tsx` (extract `web/src/components/canvas/{Toolbar,Hud}.tsx`), `web/src/components/nodes/FeatureGroup.tsx`, `web/src/lib/canvas.ts`.

- [ ] Per board2-canvas-v2: stage dot-grid + two ambient radial glows + grid mask; frosted Toolbar (Feature/Task/Comms pills); `<Controls>`-equivalent zoom HUD (use React Flow's useReactFlow zoomIn/zoomOut/fitView; custom HUD markup) bottom-right; `<MiniMap>` bottom-left with `nodeColor` = role/status tint + frosted style; fitView entry animation 300ms.
- [ ] FeatureGroup v2: gradient header, branch mono, progress bar `accepted/total` (compute in deriveFlow data), all-accepted green tint + lit merge affordance. Seat drag-over: scale 1.04 + role halo class (replace border tint).
- [ ] Gate (Canvas tests green; new vitest for progress computation) + dist. Commit: `feat(web): plan canvas re-skin — grid/ambient/hud/minimap, feature frame v2 — dist rebuilt`

### Task T8: AntEdge + context menu + canvas interactions

**Files:** Create `web/src/components/canvas/edges/AntEdge.tsx`, `web/src/components/canvas/ContextMenu.tsx` (+tests); Modify Canvas.tsx, lib/canvas.ts (edge type wiring), lib/comms.ts (mergeComms emits `type:"ant"`).

- [ ] AntEdge: custom edge rendering the base path (dashed, colored as today) + an `<animateMotion rotate="auto"><mpath>` ant group — messenger ant (wait edges, edge color) / carrier ant with cargo cube (dep/blocked) — SVGs lifted from board4 §1. Module-level cap: only first 6 ant-bearing edges per render get ants (orderly: comms wait edges first), beyond → plain dash; `prefers-reduced-motion` (matchMedia) → never animate. TDD: ≤cap renders `<animateMotion>`, beyond-cap and reduced-motion render none (stub matchMedia).
- [ ] ContextMenu (board2-v2 markup): canvas right-click → New feature/New task; task node right-click → Dispatch/Edit/View spec/Delete draft (+Kbd hints); wired to existing handlers; Esc closes; only in author+live mode.
- [ ] Snap guides on drag (nearest node edge alignment ±6px → red guide line overlay), dbl-click draft rename (inline input), Del removes selected drafts, Esc chain (menu→selection→draft form). Marquee = React Flow `selectionOnDrag` with shift.
- [ ] **Connect UX fix (acceptance feedback 2.5 — connecting deps failed, anchors offset, poor feel):** task handles repositioned to card edge midpoints and enlarged hit area (≥12px, visible on hover), `connectionRadius={30}` magnetic snapping, valid drop targets highlight while dragging a connection, invalid (cross-feature/self/cycle per applyConnect) show not-allowed state. Manual verification against the dogfood repo is part of DONE.
- [ ] Gate + dist. Commit: `feat(web): ant-crawl edges, context menus, snap/rename/del interactions — dist rebuilt`

### Task T9: office derivation lib (pure)

**Files:** Create `web/src/lib/office.ts` + `office.test.ts`.

- [ ] TDD `deriveOffice` per spec §3 rules (shapes in the anchors block above): doing = own assigned+in_progress; inbox = awaiting_review where seat is reviewer; waitingOn = own awaiting_review tasks (`on=reviewer, reason:"review"`) + own tasks with unmet deps (`on=<depId>, reason:"dep"`, reuse comms.ts `unmet` logic — export it); status precedence busy > review_due > waiting > idle; shipped = accepted tasks (most recent first). Cases: each status, multi-role seat, empty state, not-joined owners excluded from desks (desks = joined agents only).
- [ ] Gate. Commit: `feat(web): office model derivation — desks, zones, status precedence`

### Task T10: OfficeView (default canvas mode)

**Files:** Create `web/src/components/canvas/OfficeView.tsx` (+test); Modify Canvas.tsx (mode segment Office|Plan, Office default), lib/api.ts nothing (layout PUT passthrough), layout helpers in lib/canvas.ts (`office` key read/write).

- [ ] Per board5: desk station nodes (React Flow custom node, draggable; positions from `layout.office[seatId]`, default grid placement; saved through the EXISTING layout PUT — extend the saved object with an `office` key, never touching `nodes` key semantics); desk markup = board5 (header/status badge/equalizer/three zones/parcel chips/empty hints); wall chart + shipped tray as fixed-position panels; drop a draft/assigned task onto a desk → dispatch with that seat pre-filled as owner (reuse DispatchModal flow).
- [ ] Parcel transit: when a task's status flips (reuse App `pulses` diff), animate a carrier ant + parcel along a lane between the two desks (checkpoint: owner→reviewer; changes: reviewer→owner red lane; accept: desk→tray). One transit at a time per task, 2.5s, reduced-motion → skip (parcel just re-renders in the new zone).
- [ ] Mode segment in Canvas toolbar; Office is the default landing; mode choice session-local (useState, not persisted). Replay + observe rules: Office in replay renders historical zones, transit/eq animations off, drop-dispatch disabled.
- [ ] vitest: desks render from deriveOffice fixture (status badges, zone counts, idle hint); drop→dispatch handler smoke; replay disables drop.
- [ ] Gate + dist. Commit: `feat(web): Office mode — agent desks, parcels, wall chart, shipped tray, transit ants — dist rebuilt`

### Task T11: task detail panel (RightRail v2)

**Files:** Rewrite `web/src/components/RightRail.tsx` → slide-over panel (keep filename), +test; Modify App.tsx (selected task opens panel over kanban AND canvas).

- [ ] Per board4 §4: header (id·feature mono, title, status Badge, owner→reviewer Ant chips, time-in-flight via relTime), Spec section (fetch task md? NO — spec field is a PATH; render the stored spec string + evidence from state; if spec looks like a path, label it `Spec · <path>`), Evidence mono, per-task event timeline filtered from the existing events stream (ant avatar + verb + relative time; each entry has "replay to here" → sets replayAt via existing enterReplay), action row Accept/Changes using existing author API calls, gated author+live.
- [ ] Slide-in 200ms, outside-click/Esc closes, content behind dims. Existing RightRail review/merge capabilities preserved (merge button lives in feature context of the panel when the selected task's feature is all-accepted).
- [ ] Gate + dist. Commit: `feat(web): task detail panel — spec/evidence/timeline/actions slide-over — dist rebuilt`

### Task T12: Wave-2 close

- [ ] Full baseline, PR `Dashboard v2 · Wave 2: Office mode, ant-crawl edges, canvas re-skin, detail panel`, CI green → merge, pull, rebuild binary.

## Wave 3 — ⌘K + timeline + supplements (branch `feat/dashboard-v2-w3`)

### Task T13: ⌘K command palette

**Files:** Create `web/src/components/CommandK.tsx` (+test); Modify App.tsx, package.json (`cmdk`).

- [ ] cmdk dialog on ⌘K/Ctrl-K (+ TopBar hint button event): groups per board4 §2 — Tasks (all tasks, search id+title, ↵ → switch to canvas + fitView focus node + open detail panel), Actions (Accept/Request changes for selected/awaiting tasks; Replay to task's last event; hidden when observe or replaying), Navigate (views with 1/2/3, projects with ⌘P filter). Footer hints. `?` opens a shortcut cheat-sheet Modal.
- [ ] vitest: opens on key, search filters, action gating in observe mode, navigate switches view.
- [ ] Gate + dist. Commit: `feat(web): ⌘K command palette + shortcut cheat sheet — dist rebuilt`

### Task T14: replay timeline

**Files:** Rewrite `web/src/components/ReplayBar.tsx` internals (keep component contract: project/replayAt/refreshTick/onEnter/onSnapshot/onLive), +tests update.

- [ ] Per board4 §3: ruler with per-event ticks colored by type (init/assign amber `--warn`-family; awaiting/review-flow blue; join/checkpoint/accept green; changes red), played gradient fill, unplayed ticks .35 opacity, drag handle (pointer events on the ruler, keyboard ←/→ preserved), hover tick → event preview card (`#n type · actor · ts`), ◀▶ buttons, LIVE ambered while replaying.
- [ ] `?at=N` deep link: on load App reads `?at` → enterReplay(N) after first timeline fetch; scrubbing updates the URL (history.replaceState); LIVE clears it. TDD: URL round-trip.
- [ ] Keep ALL existing ReplayBar behavioral tests passing (debounce/coalesce/at=0/stale-guard semantics unchanged — only the rendering changed).
- [ ] Gate + dist. Commit: `feat(web): replay timeline — typed ticks, hover preview, drag handle, ?at deep link — dist rebuilt`

### Task T15: supplements

**Files:** Create `web/src/lib/protocolErrors.ts` (+test), `web/src/components/Skeleton.tsx`, `web/src/components/NoProjects.tsx`; Modify Toasts.tsx, App.tsx, Canvas.tsx (focus mode).

- [ ] protocolErrors: map known engine error substrings → human messages (`cannot accept own task` → "工蚁不能给自己的活盖章——换个 reviewer 坐席操作"; `deps not accepted` → "前置任务还没验收,join 会被门控拦下"; unknown → verbatim). All author-API catch paths route through it into Toasts (toast gains error variant styling).
- [ ] Skeletons for board/canvas/ops first load; NoProjects hero (empty registry → centered cable mark + register form reusing ops Projects form).
- [ ] Feature focus mode (Plan): click feature header → others dim 25%/desaturate + edges fade, Esc exits, toolbar shows "focusing «id»" chip. vitest: focus class application + Esc.
- [ ] Gate + dist. Commit: `feat(web): humanized errors, skeletons, no-project hero, feature focus mode — dist rebuilt`

### Task T16: Wave-3 close + docs

- [ ] docs/architecture.md dashboard-v2 paragraph (token system, Office mode, ant assets); `.agent/` sprint + CURRENT records.
- [ ] Full baseline + PR `Dashboard v2 · Wave 3: ⌘K, replay timeline, polish supplements`, CI green → merge, pull, rebuild binary.

## Self-Review Notes
- Spec coverage: §1→T1/T2, §2→T3, §3→T7/T8/T9/T10, §4→T4/T5, §5→T11/T13/T14, §6→T5(observe/title)/T14(?at)/T15(rest), §7 waves→branch structure, §8 tests distributed per task. No gaps; §9 respected.
- Verified facts: layout PUT stores opaque JSON (author.go:288-291) → `office` key needs zero serve work; ReplayBar external contract enumerated in T14 to protect C3 tests.
- Type anchors block keeps cross-task signatures consistent (Caste/DeskModel/lifecycleStage used in T3/T4/T9/T10).
- Mockup boards are the markup source of truth — tasks say "lift from board N" deliberately; boards are committed, so implementers have them.
