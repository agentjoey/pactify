# Dashboard v2 — commercial-SaaS polish (Linear-grade) — Design

- **Date:** 2026-06-11 · **Status:** approved direction (brainstorm); visual constants
  pending the mockup gate (§8) — marked `[MOCKUP]` throughout.
- **Origin:** user: "按商业化 SaaS 产品的标准 polish 整体 UI 和交互,在审美和交互上做到最好".
- **Decisions locked:** scope = B 旗舰版 (design system + full interaction upgrade);
  气质 = **Linear 精致密度感** (dense, refined, keyboard-first); **⌘K palette = in**.
- **Benchmarks:** Linear (density/keyboard/speed), Vercel (dark restraint), n8n/Langflow
  (commercial React Flow canvases), pactify.dev site (brand continuity: 3 persona colors,
  cable/pulse motion language, mono-as-data).

## §0 Current-state audit (what "简陋" means, concretely)

Six systemic gaps: (1) no token layer — 3 role colors + ~21 components with hard-coded
GitHub-dark hexes; (2) zero typography design — system-ui, 9–12px everywhere, no scale;
(3) no spacing discipline; (4) hand-rolled one-off buttons/inputs/modals with incomplete
states and no focus rings; (5) React Flow defaults — no grid/minimap/zoom HUD/node
elevation; (6) no empty states or guidance. ~2,600 lines across web/src/components.

## §1 Design system foundation

**Color tokens** (`web/src/tokens.css`, Tailwind v4 `@theme`; exact values `[MOCKUP]`):
- Backgrounds, 4 layers ~4% luminance apart: `--bg-page` (near-black blue-gray) →
  `--bg-surface` (cards) → `--bg-raised` (popovers/hover) → `--bg-overlay` (modals).
- Borders: translucent white only — `--border-subtle` (≈6% white), `--border-strong`
  (≈10%). No opaque grays anywhere.
- Brand persona colors desaturated 10–15% for dense UI: `--role-product` (yellow),
  `--role-design` (blue), `--role-dev` (green) — identity & status accents ONLY.
- Semantic: `--success`, `--warn`, `--danger`, `--info`.
- Text 3 layers: `--text-1` 92% / `--text-2` 60% / `--text-3` 38% white.
- Radius 3 (4/6/10px), shadow 3 (card/raised/overlay), all tokens.

**Typography:**
- UI: **Inter variable**, self-hosted (no network fetch), base **13px**, line-height 1.4,
  6-step scale (11/12/13/15/18/24), `tabular-nums` for numerics.
- Data voice: task ids, seat ids, event types, timestamps, branch names in **JetBrains
  Mono** (self-hosted), one half-step smaller, `--text-3` unless focal — the
  "data vs chrome" two-voice system continuing the site's terminal-real brand.
- Weights: 600 headings / 500 interactive / 400 body.

**Primitives** (`web/src/components/ui/`, each with vitest; the ONLY source of these
controls app-wide — replaces all hand-rolled instances):
`Button` (primary/ghost/danger × sm/md, full hover/focus/active/disabled + focus ring),
`Input`, `Select`, `Modal` (180ms scale+fade, Esc, focus trap, overlay token),
`Popover`, `Tooltip` (400ms delay), `Badge` (status variants), `Kbd`, `EmptyState`,
`Avatar` (seat initial, role-color 15%-alpha bg + solid ring).

**Motion system:** two durations (120ms micro / 200ms layout), single curve
`cubic-bezier(0.25, 1, 0.5, 1)`; hover = luminance shift, never translation; every
animation behind `prefers-reduced-motion` (existing discipline).

## §2 Canvas redesign

- **Surface:** dot-grid background (~2% white dots `[MOCKUP]`), zoom HUD bottom-right
  (−/percent/+/fit), **MiniMap** bottom-left (nodes tinted by role color, frosted
  backdrop), fitView entry with 300ms ease.
- **Task card** (shared genome with kanban cards): 4px status-color left edge bar;
  13px/600 title + hover-revealed `⋯` menu; one-line spec excerpt in `--text-3`;
  owner→reviewer Avatar chips (mono labels); status Badge + deps count; hover = raise
  shadow + border one step brighter. Stale dot & awaiting pulse preserved on the new card.
- **Feature frame:** translucent panel; header = name + branch (mono) + progress ring
  `accepted/total`; all-accepted state tints header green and lights the merge button.
- **Seat station:** Avatar + name + role chips + status line (mono: "⚡ working on
  t1-core" / "idle"); drag-over = scale 1.04 + role-color halo (replaces border tint).
- **Edges:** dep = smoothstep + slow flowing dash; comms wait edges keep dash but reason
  chips become pill labels; selected edge thickens + highlights both endpoints.
- **Interactions:** snap alignment guides while dragging; marquee multi-select;
  right-click context menus (task: dispatch/edit/view spec/delete-draft; canvas: new
  feature/new task); double-click rename (drafts); Esc cancels layer-by-layer; Del
  removes selected drafts.
- **Empty state:** dashed placeholder frame + "⌘K or click to create your first
  feature" + 3-step mini guide.
- **Invariants preserved:** layout-lens isolation (display layers never reach
  layout.json), comms overlay semantics, replay read-only short-circuits,
  observe-only hides all mutations.

## §3 Kanban

Column header = status dot + name + count badge; cards reuse §2 task card; higher
density (Linear-list tightness); awaiting_review column keeps the blue breathing ring on
its cards. No cross-column drag (protocol verbs drive state) — hover tooltip explains
"status flows through pact verbs" to prevent the drag expectation.

## §4 Ops

Left anchor nav (Projects/Wiring/Seats) + card-based content panes; all forms on ui/
primitives; Wiring rows = icon + status text + probe detail (mono); destructive actions
(remove project, machine-global write) through a unified red confirm Modal. Existing
consent-gating semantics unchanged.

## §5 Shell, ⌘K, keyboard

- **TopBar:** left = 3-color cable mini-mark + project switcher (Popover with search);
  center = segmented control Kanban/Canvas/Ops with `1/2/3` Kbd hints; right = live
  badge (green breathing dot / amber "replay" / gray offline) + acting-seat Avatar.
- **⌘K palette** (new dep: `cmdk`, ~5KB): switch project, switch view, jump to task
  (search id/title → canvas fitView focuses the card), new feature/task, dispatch,
  jump replay to event N. Footer shows shortcut hints. Observe-only mode hides mutating
  commands.
- **Global keys:** `1/2/3` views, `⌘K`, `?` shortcut cheat-sheet panel, Esc chain.
- **ReplayBar → timeline:** thin ruler with per-event ticks colored by event type
  `[MOCKUP: tick palette]`, hover tick floats an event preview card, drag handle
  replaces the native range input (keyboard ←/→ still steps ±1); LIVE button with
  breathing red dot when live, amber state while replaying.

## §6 Engineering

- **New deps:** `cmdk` only, plus self-hosted font files (Inter var + JetBrains Mono
  subset). Everything else = Tailwind v4 `@theme` tokens + CSS.
- **Architecture:** `tokens.css` → `components/ui/*` → business components re-skinned.
  Canvas.tsx (635 lines) splits out `canvas/Toolbar.tsx`, `canvas/ContextMenu.tsx`,
  `canvas/Hud.tsx` (minimap/zoom). No logic rewrites — visual + interaction layer only.
- **Two waves:** Wave 1 = §1 + §2 cards/surface + §3 + empty states (visual
  transformation). Wave 2 = ⌘K + context menus + marquee + snap + timeline + §4 + §5
  TopBar.
- **Constraints carried over:** dist rebuilt+committed in every web-touching commit;
  vitest baseline (99) only grows; a11y (focus rings, aria on menus/palette);
  reduced-motion on every animation; replay/observe-only/lens invariants regression-
  tested after re-skin.

## §7 Testing

- ui/ primitives: unit tests per component (variants, disabled, focus ring class,
  Esc/focus-trap for Modal).
- Re-skin regressions: existing 99 tests stay green (testids preserved); new tests for
  context menu actions, marquee selection state, ⌘K command dispatch, timeline
  tick→jump, keyboard view switching.
- Visual sanity: check-dist-style string asserts for key new classnames in built dist.

## §8 Mockup gate (blocking Wave 1 implementation)

When the user is back at the dev machine, validate 4 boards via the visual companion;
lock `[MOCKUP]` constants into this spec afterwards:
1. Token palette + type specimen (the §1 sheet rendered).
2. Canvas hi-fi (new task card / feature frame / seat station / edges / HUD).
3. Kanban + TopBar.
4. ⌘K palette + replay timeline.

## §9 Out of scope (this round)

Onboarding tour · light theme · canvas inertia/space-pan (C-tier, next round) ·
proposal/approval flow (M3.3c candidate) · any protocol/serve API change — this
milestone is strictly web/ presentation + interaction.
