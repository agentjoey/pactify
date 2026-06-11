# Dashboard v2 — commercial-SaaS polish (Linear-grade) — Design

- **Date:** 2026-06-11 · **Status:** approved (brainstorm + 5 mockup boards validated)
- **Origin:** user: "按商业化 SaaS 产品的标准 polish 整体 UI 和交互,在审美和交互上做到最好".
- **Decisions locked:** scope = B 旗舰版 + Office mode; 气质 = Linear 精致密度感;
  palette = **B·Indigo**; avatars = **蚁群八品级** (rotated 45° cw); ⌘K in;
  **canvas dual-mode with Office as the DEFAULT landing mode**; ant-crawl edge motion.
- **Mockups (normative, committed):** `docs/superpowers/mockups/dashboard-v2/`
  board1 tokens/type · board2 canvas cards/stations · board2-ants avatar set ·
  board3 kanban+topbar · board4 ⌘K/timeline/detail/ant-crawl · board5 office mode.
  Implementation matches the boards; this doc carries the exact constants.

## §1 Design system foundation (LOCKED)

**Color tokens** (`web/src/tokens.css`, Tailwind v4 `@theme`):

```
--bg-page:    #191B26   --bg-surface: #20222F
--bg-raised:  #272A3A   --bg-overlay: #2F3246
--border-subtle: rgba(255,255,255,.07)   --border-strong: rgba(255,255,255,.10)
--text-1: rgba(255,255,255,.92)  --text-2: rgba(255,255,255,.60)  --text-3: rgba(255,255,255,.38)
--role-product: #ECC678  (orchestrator/编排 — was #ffd479, desaturated)
--role-design:  #93B4F2  (reviewer/评审   — was #8ab4ff)
--role-dev:     #7BD8A0  (worker/开发     — was #6ee7a0)
--role-qa:      #5FC8C3  --role-pm:   #E8A85C  --role-designer: #A89BF0
--role-research:#C99BD8  --role-ops:  #EE9A6B
--danger: #E5615C  --warn: #D9A23D  --success: #7BD8A0
radius: 4/6/10px · shadows 3 steps · site keeps its own palette (legacy role hues OK there)
```

**Typography:** Inter variable self-hosted, base **13px** lh 1.4, scale
11/12/13/15/18/24, weights 600/500/400, `tabular-nums`. Data voice (ids, seats,
event types, timestamps, branches) = JetBrains Mono self-hosted, half-step smaller,
`--text-3` unless focal. Type specimen = board1.

**Primitives** (`web/src/components/ui/`, sole source app-wide): Button
(primary/ghost/danger × sm/md), Input, Select, Modal (180ms scale+fade, Esc, focus
trap), Popover, Tooltip (400ms), Badge, Kbd, EmptyState, Avatar (ant SVG on
role-tinted gradient squircle). Motion: 120ms micro / 200ms layout,
`cubic-bezier(0.25,1,0.5,1)`, hover = luminance, all behind reduced-motion.

## §2 Ant avatar system (LOCKED — board2-ants)

One colony, eight castes; geometric stamp style (dark charcoal body `#2A2C3A`,
white-alpha outline, role color ONLY on two abdomen bands + the caste prop), whole
figure rotated **45° clockwise** (crawling toward upper-right). Assets =
`web/src/components/ui/ants/*.tsx` (inline SVG components, sizes 14/22/34/72 with a
bold-stroke small variant at ≤18px).

| Caste | Role key | Prop |
|---|---|---|
| 蚁后 Queen | orchestrator | crown + wings + amber bands |
| 兵蚁 Guard | reviewer | big head + mandibles + blue visor |
| 工蚁 Builder | worker (and fallback) | carried green cube |
| 捕虫蚁 Catcher | qa / test* | teal magnifier with red bug |
| 酿蜜蚁 Brewer | product / pm | checklist scroll |
| 彩绘蚁 Painter | design* | palette crown (3 dots) |
| 探路蚁 Scout | research | long antennae + signal arcs |
| 巡巢蚁 Keeper | ops / operation / devops | gear |

Mapping: protocol-hard roles map first (orchestrator>reviewer>worker priority for
multi-role seats); free-form roster role strings match by keyword (case-insensitive
substring); unmatched → Builder. Seat INDIVIDUALITY = avatar pad gradient derived
from seat-id hash within the caste's hue family + name; the ant itself is per-caste.

## §3 Canvas — dual mode, Office is DEFAULT

Mode segment top-left: **Office | Plan** (Office lands first).

**Office mode (board5) — agents are the subject.** Each seat = a draggable desk
station (position persisted under a SEPARATE layout sidecar key, e.g.
`layout.office`): header = 34px ant avatar + presence dot + name + vendor (mono) +
status badge; three zones with parcels (task chips: status-color left edge, icon,
id mono, title, age):
- 手上 doing — tasks they own in assigned/in_progress/changes_requested (rework
  returns to the owner's desk — board5's red lane; otherwise it would vanish
  from every zone)
- 收件 inbox — awaiting_review tasks where they are reviewer (+ blue glow)
- 等回音 waiting on — their own output parked elsewhere (owner of awaiting_review;
  blocked-by-dep tasks show here with the blocker named)

Desk status derivation (pure projection): **BUSY** green border+equalizer (owns
in_progress, shows current task) · **REVIEW DUE** amber (non-empty inbox) ·
**WAITING** amber (waiting-on non-empty, nothing doing) · **IDLE** 62% opacity +
"拖任务到这张桌子即派发" hint (drop target). Side furniture: 墙上看板 wall chart
(per-feature progress bars) + ✓ shipped 出货托盘 (accepted/merged parcels slide in).
Parcel transit animation: on checkpoint a carrier ant carries the parcel along a
lane to the reviewer desk (parcel half-lit at both ends while in transit); accept →
slides into the tray; changes → returns on a red lane.

**Plan mode** = existing feature/task-frame canvas re-skinned (board2-v2): dot-grid
+ ambient role-color glows, zoom HUD, role-tinted MiniMap, frosted toolbar/legend.
Task card v2: status MEDALLION (icon squircle) + id (mono) + title two-tier, 9%
status-color wash gradient, bottom LIFECYCLE segments (assigned→in_progress→review→
accepted, current glows) — NO hard color frames; awaiting keeps a soft glow; draft =
dashed. Feature frame: gradient header + branch mono + progress bar. Drag-over seat:
scale 1.04 + role halo. Context menu (Dispatch/Edit/View spec/Delete + Kbd), snap
guides, marquee select, dbl-click rename, Esc/Del.

**Ant-crawl edges (both modes + comms overlay):** custom React Flow edge embeds an
`animateMotion` ant along the real path (rotate=auto): wait edges = blue messenger
ant; dep/blocked = amber carrier ant with a tiny cargo cube (board4 §1 demo). Cap 6
concurrent ants (overflow → static dashed); reduced-motion → static dashed always.

## §4 Kanban + TopBar (board3)

TopBar: cable mini-mark + wordmark · project switcher chip (Popover+search) ·
centered segmented control with `1/2/3` Kbd hints · ⌘K hint · live badge (green
breathing / amber ● replay / gray offline) · acting-seat ant avatar. Observe-only:
"👁 observing" badge with hover explainer (how to start with --seat).

Kanban: 5 columns (assigned / in progress / awaiting review / changes req. /
accepted), headers = status dot + uppercase label + count chip; cards share the
task-card-v2 genome with 14px bold-stroke ant mini avatars (owner → reviewer);
in-progress keeps stale amber dot; awaiting glows; accepted column at 82% opacity;
empty column = dashed ghost text. No cross-column drag (hover header tooltip:
"status flows through pact verbs").

## §5 ⌘K · replay timeline · task detail panel (board4)

- **⌘K** (cmdk): groups Tasks (jump → canvas focus) / Actions (accept, request
  changes, replay-to-event — hidden in observe mode) / Navigate (view, project).
  Footer hints. `⌘P` project switch alias.
- **Replay timeline** replaces the slider: ruler with per-event ticks colored by
  type (amber assign/init · blue review-flow · green join/checkpoint/accept · red
  changes), played region gradient fill, unplayed ticks 35% opacity, white drag
  handle, hover tick → event preview card, ◀▶ steps + `←/→`, `?at=N` deep link
  (read+write URL), LIVE button ambers during replay.
- **Task detail panel** (RightRail v2, slides over on card click, dims content
  behind, outside-click closes): header (id·feature mono, title, status badge,
  owner→reviewer ant chips, time-in-flight) · Spec rendered from
  `.pact/tasks/<id>.md` (inline code highlighted) · Evidence (mono) · per-task event
  Timeline (ant avatar + relative time; entries link "replay to here") · action row
  (Accept primary / Changes ghost) gated by role + live mode.

## §6 Supplementary requirements (locked in brainstorm)

1. Unified feedback: humanized protocol-422 toasts (e.g. self-accept → explain the
   rule), API-failure toasts, loading skeletons, empty/error/loading triple states.
2. Observe-mode visibility (TopBar badge, §4).
3. Feature focus mode (Plan): click feature header → focus (others dim/recede), Esc.
4. Relative times on cards/desks ("3m ago", tabular).
5. Dynamic document title `«project» · N awaiting ●` + cable favicon.
6. Replay deep-link `?at=N` (§5).
7. No-project landing hero (serve with empty registry → guided register panel).
8. Office parcel-transit + crawl animations all reduced-motion-gated.

Deferred: browser notifications · full canvas keyboard nav · marquee batch dispatch ·
sounds · real seat heartbeat (presence derives from last event, labeled honestly).

## §7 Engineering

- New deps: `cmdk` only + self-hosted fonts (Inter var, JetBrains Mono subset).
- `tokens.css` → `ui/` primitives (each with vitest) → re-skin. Canvas.tsx splits:
  `canvas/{PlanView,OfficeView,Toolbar,ContextMenu,Hud,edges/AntEdge}.tsx`.
  Office desk positions: layout sidecar gains an OPTIONAL `office` key (additive,
  same PUT endpoint, lens isolation unchanged).
- **Three waves:** W1 foundation (tokens/fonts/ui/ants) + task-card v2 + kanban +
  TopBar; W2 Office mode + ant-crawl edges + detail panel; W3 ⌘K + timeline +
  focus mode + supplements (§6).
- Constraints: dist rebuilt+committed every web commit; vitest baseline (99) only
  grows; existing invariants regression-covered (replay read-only, observe-only,
  lens isolation, reduced-motion); no protocol/serve API changes EXCEPT the
  additive `office` layout key.

## §8 Testing

ui/ primitives unit tests; ant-avatar role-mapping pure fn (keyword/fallback);
office derivation pure fn (BUSY/REVIEW DUE/WAITING/IDLE per §3 rules, zone
membership); AntEdge renders ant ≤cap / static beyond; timeline tick coloring fn;
?at URL round-trip; detail panel action gating; existing 99 green throughout;
check-dist string asserts for new class names.

## §9 Out of scope

Light theme · onboarding tour · canvas inertia · proposal/approval flow (M3.3c
candidate) · site changes · M3.4 relay.
