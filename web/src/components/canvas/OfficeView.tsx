import { useCallback, useEffect, useMemo, useRef, useState, type MouseEvent as ReactMouseEvent } from "react";
import {
  ReactFlow,
  ReactFlowProvider,
  useReactFlow,
  useViewport,
  type Node,
  type NodeProps,
  type NodeTypes,
  type NodeChange,
  applyNodeChanges,
} from "@xyflow/react";
import type { State, Task } from "../../lib/types";
import type { Draft, LayoutJSON } from "../../lib/canvas";
import { deriveOffice, type DeskModel, type DeskStatus } from "../../lib/office";
import { casteForRoles, padGradient } from "../../lib/ants";
import { Ant } from "../ui/ants/Ant";
import { StatusPill, type PactStatus } from "../ui/StatusPill";
import { statusColorVar } from "../../lib/lifecycle";
import { CarrierAnt } from "./edges/AntEdge";
import { Hud } from "./Hud";

// OfficeView (T10, spec §3 / board5) — the DEFAULT canvas mode. Agents are the
// subject: one draggable desk station per JOINED seat (deriveOffice), with three
// zones (手上 doing / 收件 inbox / 等回音 waiting on) holding parcel chips, plus
// fixed furniture panels (wall chart, shipped tray) and a draft dock strip.
//
// DOCUMENTED DEVIATIONS from the board5 mockup (state lacks the fields):
//   • board shows a vendor line ("opencode cli · 工蚁"); State carries no client
//     name, so the desk sub-line shows the seat's ROLES instead.
//   • board shows a per-parcel age ("12m"); Task has no per-task timestamp in
//     State, so the age chip is omitted (only the review-handoff "→ <seat>" meta
//     is shown for waiting-on parcels).
//
// DISPATCH AFFORDANCE (chosen): a draft DOCK strip (bottom-left) lists current
// drafts as draggable parcel chips (HTML5 drag-and-drop). Dropping a draft onto
// a desk opens DispatchModal with that seat pre-filled as owner. Idle desks
// double as the drop affordance (their hint reads "拖一个任务到这张桌子即派发").
// As a discoverability fallback, clicking an idle desk also opens DispatchModal
// pre-filled (only when there is exactly one draft to dispatch — otherwise the
// dock drag is the path). All drop/click affordances are gated to author+live.

const MEDALLION: Record<string, string> = {
  assigned: "◇",
  in_progress: "⚡",
  awaiting_review: "◉",
  changes_requested: "↺",
  accepted: "✓",
};

// Desk status badge content + color token. Lifted from board5 `.dstat`.
function statusBadge(status: DeskStatus): { label: string; color: string; eq: boolean } {
  switch (status) {
    case "busy":
      return { label: "BUSY", color: "var(--color-success)", eq: true };
    case "review_due":
      return { label: "📥 REVIEW DUE", color: "var(--color-warn)", eq: false };
    case "waiting":
      return { label: "WAITING", color: "var(--color-warn)", eq: false };
    case "idle":
    default:
      return { label: "◐ IDLE", color: "var(--color-text-2)", eq: false };
  }
}

// presenceColor — the dot on the desk avatar (board5 `.presence`).
function presenceColor(status: DeskStatus): string {
  if (status === "busy") return "var(--color-success)";
  if (status === "review_due" || status === "waiting") return "var(--color-warn)";
  return "rgba(18,22,31,.25)";
}

// Parcel chip. `dim` = parked output (waiting-on / shipped tray), `glow` = inbox.
function Parcel({
  task,
  meta,
  dim,
  glow,
  onClick,
}: {
  task: Task;
  meta?: string;
  dim?: boolean;
  glow?: boolean;
  onClick?: () => void;
}) {
  return (
    <div
      className={`parcel${dim ? " dim" : ""}${glow ? " inbox-glow" : ""}`}
      style={{ ["--st" as string]: statusColorVar(task.status), cursor: onClick ? "pointer" : undefined }}
      data-testid={`parcel-${task.id}`}
      onClick={
        onClick
          ? (e) => {
              // Don't let a parcel click bubble to the desk's click-dispatch.
              e.stopPropagation();
              onClick();
            }
          : undefined
      }
    >
      <span className="pico">{MEDALLION[task.status] ?? "◇"}</span>
      <span className="pid">{task.id}</span>
      <span className="ptitle">{task.spec ? task.spec.split("/").pop() : task.id}</span>
      {meta && <span className="pmeta">{meta}</span>}
    </div>
  );
}

// DeskNodeData rides each desk RF node.
interface DeskNodeData {
  desk: DeskModel;
  dropTarget: boolean; // currently dragged-over by a dock parcel
  onSelectTask?: (id: string) => void;
  onDeskClick?: (seatId: string) => void;
  onDeskContextMenu?: (e: ReactMouseEvent, seatId: string) => void;
  onSelectSeat?: (seatId: string) => void; // Phase 4: open the Links panel
  [key: string]: unknown;
}

// DeskNode renders one agent station from a DeskModel (board5 `.desk`).
function DeskNode({ data }: NodeProps) {
  const d = data as DeskNodeData;
  const { desk } = d;
  const caste = casteForRoles(desk.roles);
  const pad = padGradient(desk.seatId, caste);
  const badge = statusBadge(desk.status);
  const roleLine = desk.roles.join(" · ") || "seat";

  // Map a waiting-on item's `on` to a human meta ("→ <seat> review" / blocked-by).
  const waitMeta = (on: string, reason: "review" | "dep") =>
    reason === "review" ? `→ ${on}` : `blocked: ${on}`;

  return (
    <div
      className={`desk ${desk.status}${d.dropTarget ? " drop-target" : ""}`}
      data-testid={`desk-${desk.seatId}`}
      data-desk-id={desk.seatId}
      onClick={() => d.onDeskClick?.(desk.seatId)}
      onContextMenu={(e) => d.onDeskContextMenu?.(e, desk.seatId)}
    >
      <div
        className="dhead"
        style={{ cursor: "pointer" }}
        title="View relationships"
        onClick={(e) => { e.stopPropagation(); d.onSelectSeat?.(desk.seatId); }}
      >
        <span className="dava" style={{ background: `linear-gradient(135deg, ${pad.from}, ${pad.to})` }}>
          <Ant caste={caste} size={34} title={`${caste} — ${roleLine}`} />
          <span className="presence" style={{ background: presenceColor(desk.status) }} />
        </span>
        <div>
          <div className="dname">{desk.seatId}</div>
          <div className="dvend mono">{roleLine}</div>
        </div>
        <span
          className="dstat"
          data-testid={`desk-status-${desk.seatId}`}
          style={{ background: `color-mix(in srgb, ${badge.color} 12%, transparent)`, color: badge.color }}
        >
          {badge.eq && (
            <span className="eq">
              <i /><i /><i />
            </span>
          )}
          {badge.label}
        </span>
      </div>

      <div className="dzone">
        {/* doing */}
        <div className="zlab">
          Doing <span className="zc">{desk.doing.length}</span>
        </div>
        {desk.doing.length === 0 ? (
          <div className="dempty">{desk.status === "idle" ? "Idle — drop a task on this desk to dispatch" : "Empty"}</div>
        ) : (
          desk.doing.map((t) => <Parcel key={t.id} task={t} onClick={() => d.onSelectTask?.(t.id)} />)
        )}

        {/* inbox */}
        <div className="zlab">
          Inbox <span className="zc">{desk.inbox.length}</span>
        </div>
        {desk.inbox.length === 0 ? (
          <div className="dempty">Empty</div>
        ) : (
          desk.inbox.map((t) => <Parcel key={t.id} task={t} glow onClick={() => d.onSelectTask?.(t.id)} />)
        )}

        {/* waiting on */}
        <div className="zlab">
          Waiting on <span className="zc">{desk.waitingOn.length}</span>
        </div>
        {desk.waitingOn.length === 0 ? (
          <div className="dempty">Empty</div>
        ) : (
          desk.waitingOn.map((w) => (
            <Parcel
              key={`${w.task.id}-${w.on}-${w.reason}`}
              task={w.task}
              dim
              meta={waitMeta(w.on, w.reason)}
              onClick={() => d.onSelectTask?.(w.task.id)}
            />
          ))
        )}
      </div>
    </div>
  );
}

const nodeTypes: NodeTypes = { desk: DeskNode };

// LinksPanel — Phase 4 (Make-Grid "Links" gold mine): the selected seat's
// relationships as directional pills. owns → its tasks (each with status + who
// reviews it); reviews → tasks it reviews. A fixed slide-over on the right.
function LinkRow({ dir, dirColor, taskId, status, sub, onClick }: {
  dir: string; dirColor: string; taskId: string; status: PactStatus; sub?: React.ReactNode; onClick?: () => void;
}) {
  return (
    <button type="button" onClick={onClick} className="flex w-full flex-col gap-1 rounded-lg border border-[var(--color-border-subtle)] bg-[var(--color-bg-surface)] px-2.5 py-2 text-left transition-colors hover:border-[var(--color-text-3)]">
      <div className="flex items-center gap-2">
        <span className="rounded-full px-1.5 py-0.5 text-[9px] font-semibold uppercase tracking-[.3px]" style={{ color: dirColor, background: `color-mix(in srgb, ${dirColor} 14%, transparent)`, border: `1px solid color-mix(in srgb, ${dirColor} 28%, transparent)` }}>{dir}</span>
        <span className="mono text-[11.5px] font-[650] text-[var(--color-text-1)]">{taskId}</span>
        <span className="ml-auto"><StatusPill status={status} /></span>
      </div>
      {sub && <div className="pl-0.5 text-[10px] text-[var(--color-text-3)]">{sub}</div>}
    </button>
  );
}

function LinksPanel({ seat, state, onClose, onSelectTask }: {
  seat: string; state: State; onClose: () => void; onSelectTask?: (id: string) => void;
}) {
  const roles = state.agents.find((a) => a.id === seat)?.roles ?? [];
  const all = state.features.flatMap((f) => f.tasks.map((t) => ({ ...t })));
  const owns = all.filter((t) => t.owner === seat);
  const reviews = all.filter((t) => t.reviewer === seat);
  const ST = (s: string) => (s as PactStatus);
  return (
    <aside
      data-testid="office-links"
      className="card-frost absolute right-0 top-0 bottom-0 z-20 flex w-[262px] flex-col gap-3 overflow-y-auto border-l border-[var(--color-border-subtle)] px-3.5 py-3 shadow-[var(--shadow-raised)]"
    >
      <div className="flex items-center gap-2">
        <span className="grid h-7 w-7 place-items-center rounded-lg bg-[var(--color-bg-inset)]"><Ant caste={casteForRoles(roles)} size={20} /></span>
        <div className="min-w-0 flex-1">
          <div className="mono text-[13px] font-[680] text-[var(--color-text-1)]">{seat}</div>
          <div className="text-[10px] text-[var(--color-text-3)]">{roles.join(" · ") || "seat"}</div>
        </div>
        <button type="button" onClick={onClose} title="Close" className="grid h-5 w-5 place-items-center rounded text-[var(--color-text-3)] hover:bg-[var(--color-bg-inset)] hover:text-[var(--color-text-1)]">×</button>
      </div>

      {owns.length > 0 && (
        <div>
          <div className="mb-1.5 text-[9.5px] font-semibold uppercase tracking-[.5px] text-[var(--color-text-3)]">owns · {owns.length}</div>
          <div className="flex flex-col gap-1.5">
            {owns.map((t) => (
              <LinkRow key={t.id} dir="owns →" dirColor="var(--color-role-dev)" taskId={t.id} status={ST(t.status)}
                sub={t.reviewer && t.reviewer !== seat ? <>← reviewed by <span className="mono">{t.reviewer}</span></> : undefined}
                onClick={() => onSelectTask?.(t.id)} />
            ))}
          </div>
        </div>
      )}

      {reviews.length > 0 && (
        <div>
          <div className="mb-1.5 text-[9.5px] font-semibold uppercase tracking-[.5px] text-[var(--color-text-3)]">reviews · {reviews.length}</div>
          <div className="flex flex-col gap-1.5">
            {reviews.map((t) => (
              <LinkRow key={t.id} dir="reviews →" dirColor="var(--color-role-design)" taskId={t.id} status={ST(t.status)}
                sub={<>owned by <span className="mono">{t.owner}</span></>}
                onClick={() => onSelectTask?.(t.id)} />
            ))}
          </div>
        </div>
      )}

      {owns.length === 0 && reviews.length === 0 && (
        <div className="text-[11px] italic text-[var(--color-text-3)]">No task relationships yet.</div>
      )}
    </aside>
  );
}

// Default grid placement: 2 columns, 300px pitch (≈ desk width + gap).
const GRID_COLS = 2;
const GRID_PITCH_X = 300;
const GRID_PITCH_Y = 300;
const GRID_X0 = 60;
const GRID_Y0 = 40;

function gridPos(i: number): { x: number; y: number } {
  return {
    x: GRID_X0 + (i % GRID_COLS) * GRID_PITCH_X,
    y: GRID_Y0 + Math.floor(i / GRID_COLS) * GRID_PITCH_Y,
  };
}

// placeNewDesks materializes office positions ONCE for seats with no layout.office
// entry — the Office-mode analogue of placeNew (spec §1/§3). It mirrors the Plan
// pipeline's idempotence: a seat already in layout.office is never re-placed, so a
// new seat joining can't jostle an already-materialized desk (the old gridPos(i)
// re-derived EVERY desk by roster index, which shifted unsaved desks when the
// roster grew). Grid slots are walked in order; a candidate cell already occupied
// by an existing office entry (or a sibling assigned earlier in THIS batch) is
// skipped (|dx|<pitch*0.8 && |dy|<pitch*0.8) and placement falls through to the
// next free slot. Returns only the NEW entries (empty when every seat is placed).
function placeNewDesks(
  office: Record<string, { x: number; y: number }>,
  seatIds: string[],
): Record<string, { x: number; y: number }> {
  const taken: { x: number; y: number }[] = seatIds
    .filter((id) => office[id])
    .map((id) => office[id]);
  const hits = (q: { x: number; y: number }) =>
    taken.some(
      (t) =>
        Math.abs(t.x - q.x) < GRID_PITCH_X * 0.8 && Math.abs(t.y - q.y) < GRID_PITCH_Y * 0.8,
    );
  const add: Record<string, { x: number; y: number }> = {};
  let slot = 0;
  for (const id of seatIds) {
    if (office[id]) continue; // already materialized — idempotent, never re-place
    let p = gridPos(slot++);
    while (hits(p)) p = gridPos(slot++);
    taken.push(p);
    add[id] = p;
  }
  return add;
}

// OfficeMenu describes an open office context menu: a pane menu (New task / New
// feature / fit view) or a desk menu (one dispatch entry per draft → this seat).
type OfficeMenu =
  | { kind: "pane"; x: number; y: number }
  | { kind: "desk"; x: number; y: number; seatId: string };

function prefersReducedMotion(): boolean {
  return (
    typeof window !== "undefined" &&
    typeof window.matchMedia === "function" &&
    window.matchMedia("(prefers-reduced-motion: reduce)").matches
  );
}

// Transit kind → lane color + ant species feel (carrier ant always; the parcel
// rides it). checkpoint owner→reviewer (blue), changes reviewer→owner (red),
// accept desk→tray (green). `from`/`to` are FLOW-space coordinates; the
// TransitOverlay (rendered INSIDE <ReactFlow>) transforms them to screen space
// via the live viewport so the lane tracks pan/zoom. `toTray` flags the accept
// lane whose true target is the screen-fixed shipped tray — the overlay resolves
// the tray's flow position from its DOM rect (falling back to `to`).
type Transit = {
  taskId: string;
  from: { x: number; y: number };
  to: { x: number; y: number };
  toTray?: boolean;
  color: string;
};

// TransitOverlay renders the single parcel-transit lane + carrier ant INSIDE
// <ReactFlow> (mirrors SnapGuides in Canvas.tsx): `from`/`to` arrive in FLOW
// space and are transformed to screen space via the live viewport (pos * zoom +
// offset) so the lane and the SMIL-driven ant track pan/zoom/fitView. For the
// accept lane (`toTray`), the true target is the screen-fixed shipped tray — we
// read its DOM rect and convert screen→flow via screenToFlowPosition, falling
// back to the precomputed `to` offset when the rect is unavailable (e.g. jsdom).
function TransitOverlay({
  transit,
  trayRef,
}: {
  transit: Transit;
  trayRef: React.RefObject<HTMLDivElement | null>;
}) {
  const { x: vx, y: vy, zoom } = useViewport();
  const { screenToFlowPosition } = useReactFlow();
  const toScreen = (p: { x: number; y: number }) => ({ x: p.x * zoom + vx, y: p.y * zoom + vy });

  // Resolve the accept lane's flow-space target from the tray's actual rect.
  let toFlow = transit.to;
  if (transit.toTray && trayRef.current) {
    const r = trayRef.current.getBoundingClientRect();
    if (r.width > 0 || r.height > 0) {
      toFlow = screenToFlowPosition({ x: r.left + r.width / 2, y: r.top + r.height / 2 });
    }
  }
  const from = toScreen(transit.from);
  const to = toScreen(toFlow);

  return (
    <svg
      data-testid="office-transit"
      style={{ position: "absolute", inset: 0, width: "100%", height: "100%", overflow: "visible", zIndex: 5, pointerEvents: "none" }}
    >
      <path
        id={`office-lane-${transit.taskId}`}
        className="office-lane"
        d={`M ${from.x} ${from.y} L ${to.x} ${to.y}`}
        fill="none"
        stroke={transit.color}
        strokeWidth="1.4"
        opacity="0.5"
      />
      <g style={{ overflow: "visible" }}>
        <CarrierAnt color={transit.color} />
        <animateMotion dur="2.5s" repeatCount="1" rotate="auto" fill="freeze">
          <mpath href={`#office-lane-${transit.taskId}`} />
        </animateMotion>
      </g>
    </svg>
  );
}

// OfficeViewProps — shared by the inner component and the provider wrapper.
interface OfficeViewProps {
  state: State;
  layout: LayoutJSON;
  // Author-and-live: drop-to-dispatch + click-dispatch only when true. App
  // already passes author=false while replaying; `replaying` is a belt-and-
  // suspenders guard echoing the Plan canvas.
  author: boolean;
  replaying?: boolean;
  // Raw task ids that changed on the latest live snapshot (App's diff). Drives
  // the parcel-transit animation; undefined while replaying.
  pulses?: Set<string>;
  drafts: Draft[];
  // Persist a seat's Office-mode desk position (debounced PUT in Canvas). NEVER
  // touches the Plan `positions` key.
  onSaveOffice: (seatId: string, pos: { x: number; y: number }) => void;
  onSelectTask?: (id: string) => void;
  // Open DispatchModal pre-filled with this draft + seat as owner.
  onDispatchDraft: (draft: Draft, seatId: string) => void;
  // Open the TaskEditor (dock empty-state + pane context menu). Author only.
  onOpenNewTask?: () => void;
  // Open the inline New-feature form (pane context menu). Author only.
  onOpenNewFeature?: () => void;
  // Report freshly materialized office entries up to Canvas, which folds them
  // into layout.office (local always; PUT when author && !replaying). The Office
  // analogue of the Plan placeNew→setLayout fold.
  onMaterializeOffice?: (entries: Record<string, { x: number; y: number }>) => void;
}

// OfficeView wraps the inner surface in a ReactFlowProvider so the office context
// menu's "fit view" item (and any future store reads at this level) can call
// useReactFlow OUTSIDE the <ReactFlow> subtree. <ReactFlow> alone only provisions
// the store for its CHILDREN (Hud/TransitOverlay); the component RENDERING it is
// not inside that store. The provider hoists the store one level up so both the
// menu handler and the in-flow children share it.
export function OfficeView(props: OfficeViewProps) {
  return (
    <ReactFlowProvider>
      <OfficeViewInner {...props} />
    </ReactFlowProvider>
  );
}

function OfficeViewInner({
  state,
  layout,
  author,
  replaying,
  pulses,
  drafts,
  onSaveOffice,
  onSelectTask,
  onDispatchDraft,
  onOpenNewTask,
  onOpenNewFeature,
  onMaterializeOffice,
}: OfficeViewProps) {
  const { desks, shipped } = useMemo(() => deriveOffice(state), [state]);
  const dropDispatch = author && !replaying;

  // Desk position bookkeeping (RF state). Positions: saved office key → grid.
  const [nodes, setNodes] = useState<Node[]>([]);
  const [dropSeat, setDropSeat] = useState<string | null>(null);
  const dropSeatRef = useRef<string | null>(null);
  dropSeatRef.current = dropSeat;

  // The office stage element, so context-menu coords are stage-relative.
  const stageRef = useRef<HTMLDivElement>(null);
  // Office context menu (author && !replaying). Pane = New task/feature/fit;
  // desk = one "dispatch <draft> → <seat>" entry per draft.
  const [menu, setMenu] = useState<OfficeMenu | null>(null);
  // dockPulse flashes the dock when an idle desk is clicked with >1 draft (the
  // guided "go drag from the dock" cue). Cleared after 1.5s by an effect below.
  const [dockPulse, setDockPulse] = useState(false);
  // Phase 4: the seat whose Links (relationships) panel is open. Stable callback
  // so it doesn't churn the desk node data.
  const [linksSeat, setLinksSeat] = useState<string | null>(null);
  const onSelectSeat = useCallback((id: string) => setLinksSeat((s) => (s === id ? null : id)), []);
  const { fitView } = useReactFlow();

  // Single draft → click-dispatch convenience target. Idle desks ONLY (the file
  // header's affordance contract): clicking a busy/review/waiting desk is a
  // read gesture, not a dispatch. With MORE than one draft, an idle-desk click
  // can't disambiguate which draft to dispatch — pulse the dock instead to guide
  // the author toward the drag-from-dock path.
  const onDeskClick = useCallback(
    (seatId: string) => {
      if (!dropDispatch) return;
      const desk = desks.find((d) => d.seatId === seatId);
      if (desk?.status !== "idle") return;
      if (drafts.length === 1) onDispatchDraft(drafts[0], seatId);
      else if (drafts.length > 1) setDockPulse(true);
    },
    [dropDispatch, desks, drafts, onDispatchDraft],
  );

  // stageXY maps a browser event to office-stage-relative coords for menu placement.
  const stageXY = (e: { clientX: number; clientY: number }) => {
    const r = stageRef.current?.getBoundingClientRect();
    return { x: e.clientX - (r?.left ?? 0), y: e.clientY - (r?.top ?? 0) };
  };

  // Desk right-click → desk context menu (author && !replaying only). Stops the
  // event so the pane handler doesn't also fire.
  const onDeskContextMenu = useCallback(
    (e: ReactMouseEvent, seatId: string) => {
      if (!dropDispatch) return;
      e.preventDefault();
      e.stopPropagation();
      const { x, y } = stageXY(e);
      setMenu({ kind: "desk", x, y, seatId });
    },
    [dropDispatch],
  );

  // Pane right-click → pane context menu (New task / New feature / fit view).
  const onPaneContextMenu = useCallback(
    (e: ReactMouseEvent | MouseEvent) => {
      if (!dropDispatch) return;
      e.preventDefault();
      const { x, y } = stageXY(e);
      setMenu({ kind: "pane", x, y });
    },
    [dropDispatch],
  );

  // Office desk positions are materialized ONCE per seat (spec §3): build the
  // effective office map = saved entries + freshly placed new seats, report the
  // new entries up so Canvas folds them into layout.office (local always; PUT
  // when author&&!replaying), and place every node from that map. Idempotent —
  // a seat already in layout.office keeps its saved coords, so a new seat joining
  // never jostles an existing desk.
  useEffect(() => {
    const office = layout.office ?? {};
    const add = placeNewDesks(office, desks.map((d) => d.seatId));
    if (Object.keys(add).length > 0) onMaterializeOffice?.(add);
    const effective = { ...office, ...add };

    // Merge-by-id (same principle as the Plan canvas's mergeNodes): an already-
    // mounted desk node keeps its PREV object — and with it RF's live truth
    // (position from the last drag, measured, selected, dragging). We produce a
    // fresh object ONLY when the desk's observable data actually changed (desk
    // model, dropTarget, or a callback identity), otherwise we return prev by
    // reference so RF skips the re-render and never resets drag/selection state.
    // A whole-group `setNodes(desks.map(...))` instead rebuilt every node on each
    // layout.office change (e.g. a drag-stop save echoing the same coords back),
    // wiping dragging/selected/measured mid-interaction. CRITICAL: a desk's
    // position source is prev.position for existing nodes (the post-drag RF truth;
    // layout.office is merely its persisted copy) — only NEW seats take `effective`.
    setNodes((prev) => {
      const prevById = new Map(prev.map((n) => [n.id, n]));
      return desks.map((desk) => {
        const id = `desk:${desk.seatId}`;
        const existing = prevById.get(id);
        const dropTarget = dropSeatRef.current === desk.seatId;
        if (existing) {
          const ex = existing.data as DeskNodeData;
          // Unchanged observable data → return prev untouched (reference-stable),
          // preserving prev.position/measured/selected/dragging.
          if (
            ex.desk === desk &&
            ex.dropTarget === dropTarget &&
            ex.onSelectTask === onSelectTask &&
            ex.onDeskClick === onDeskClick &&
            ex.onDeskContextMenu === onDeskContextMenu &&
            ex.onSelectSeat === onSelectSeat &&
            existing.draggable === !replaying
          ) {
            return existing;
          }
          // Data changed: new object, but KEEP prev.position (post-drag RF truth)
          // and any other RF-owned fields via the spread.
          return {
            ...existing,
            draggable: !replaying,
            data: {
              desk,
              dropTarget,
              onSelectTask,
              onDeskClick,
              onDeskContextMenu,
              onSelectSeat,
            } satisfies DeskNodeData,
          };
        }
        // New seat: materialize from the effective office map.
        return {
          id,
          type: "desk",
          position: effective[desk.seatId],
          data: {
            desk,
            dropTarget,
            onSelectTask,
            onDeskClick,
            onDeskContextMenu,
            onSelectSeat,
          } satisfies DeskNodeData,
          draggable: !replaying,
        };
      });
    });
  }, [desks, layout.office, replaying, onSelectTask, onDeskClick, onDeskContextMenu, onSelectSeat, onMaterializeOffice]);

  // Clear the dock pulse 1.5s after it fires.
  useEffect(() => {
    if (!dockPulse) return;
    const h = setTimeout(() => setDockPulse(false), 1500);
    return () => clearTimeout(h);
  }, [dockPulse]);

  // Esc closes the office context menu (outside-click is handled by RF's
  // onPaneClick + the menu's own buttons).
  useEffect(() => {
    if (!menu) return;
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") setMenu(null);
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [menu]);

  // Reflect drop-target highlight without rebuilding the whole node array.
  useEffect(() => {
    setNodes((nds) =>
      nds.map((n) =>
        (n.data as DeskNodeData).dropTarget === (dropSeat === (n.id as string).replace(/^desk:/, ""))
          ? n
          : { ...n, data: { ...n.data, dropTarget: dropSeat === (n.id as string).replace(/^desk:/, "") } },
      ),
    );
  }, [dropSeat]);

  const onNodesChange = useCallback((changes: NodeChange[]) => {
    setNodes((nds) => applyNodeChanges(changes, nds));
  }, []);

  const onNodeDragStop = useCallback(
    (_e: unknown, node: Node) => {
      if (replaying) return;
      const seatId = (node.id as string).replace(/^desk:/, "");
      onSaveOffice(seatId, { x: node.position.x, y: node.position.y });
    },
    [replaying, onSaveOffice],
  );

  // --- transit animation (parcel rides a carrier ant between desks) ---------
  // We track the previous status per task; when `pulses` flags a changed task we
  // compute the lane (owner desk → reviewer desk etc.) and play one transit. One
  // global transit, latest-wins, first changed task per snapshot; reduced-motion
  // → skip (the parcel just re-renders). Lane endpoints are FLOW-space — the
  // TransitOverlay child transforms them to screen space via the live viewport.
  const prevStatus = useRef<Map<string, string>>(new Map());
  const [transit, setTransit] = useState<Transit | null>(null);
  const deskCenter = useCallback(
    (seatId: string): { x: number; y: number } | null => {
      const n = nodes.find((x) => x.id === `desk:${seatId}`);
      if (!n) return null;
      // Center-ish anchor (desk width 264, header band ~40).
      return { x: n.position.x + 132, y: n.position.y + 40 };
    },
    [nodes],
  );

  useEffect(() => {
    // Build the current status map; on first pass just seed it.
    const cur = new Map<string, string>();
    for (const f of state.features) for (const t of f.tasks) cur.set(t.id, t.status);
    const prev = prevStatus.current;
    prevStatus.current = cur;
    if (replaying || prefersReducedMotion()) return;
    if (!pulses || pulses.size === 0) return;

    // Find the first pulsed task whose status actually changed and resolve a lane.
    for (const id of pulses) {
      const before = prev.get(id);
      const after = cur.get(id);
      if (before === undefined || before === after) continue;
      const task = cur.has(id)
        ? state.features.flatMap((f) => f.tasks).find((t) => t.id === id)
        : undefined;
      if (!task) continue;

      let from: { x: number; y: number } | null = null;
      let to: { x: number; y: number } | null = null;
      let toTray = false;
      let color = "var(--color-role-design)";
      if (after === "awaiting_review") {
        from = deskCenter(task.owner);
        to = deskCenter(task.reviewer);
        color = "#93B4F2";
      } else if (after === "changes_requested") {
        from = deskCenter(task.reviewer);
        to = deskCenter(task.owner);
        color = "#E5615C";
      } else if (after === "accepted") {
        from = deskCenter(task.owner);
        // True target is the screen-fixed shipped tray — TransitOverlay resolves
        // it from the tray's DOM rect. This flow-space offset is only the
        // fallback when the rect is unavailable (e.g. jsdom).
        to = { x: (from?.x ?? 0) + 400, y: (from?.y ?? 0) + 240 };
        toTray = true;
        color = "var(--color-success)";
      }
      if (from && to) {
        setTransit({ taskId: id, from, to, toTray, color });
        break;
      }
    }
  }, [pulses, state, replaying, deskCenter]);

  // Clear a transit after its 2.5s run.
  useEffect(() => {
    if (!transit) return;
    const h = setTimeout(() => setTransit(null), 2500);
    return () => clearTimeout(h);
  }, [transit]);

  // --- draft dock (drop-to-dispatch) ----------------------------------------
  const onParcelDragStart = (e: React.DragEvent, draftId: string) => {
    e.dataTransfer.setData("text/pactify-draft", draftId);
    e.dataTransfer.effectAllowed = "move";
  };

  // The desk DOM (RF node) is the drop zone; we attach the HTML5 handlers at the
  // pane level by delegating via a dedicated data-desk-id attribute on the .desk
  // root. closest("[data-desk-id]") matches self-first, so resolving by this
  // attribute (NOT data-testid^='desk-') avoids snapping onto the desk-status
  // badge, whose testid `desk-status-<seat>` would otherwise be matched and let
  // header-targeted drops silently die. RF nodes render the desk markup, so we
  // listen on the wrapping pane and resolve the desk under the pointer through
  // elementFromPoint at drop time.
  const onPaneDragOver = (e: React.DragEvent) => {
    if (!dropDispatch) return;
    const el = document.elementFromPoint(e.clientX, e.clientY) as HTMLElement | null;
    const desk = el?.closest("[data-desk-id]") as HTMLElement | null;
    const seatId = desk?.getAttribute("data-desk-id") ?? undefined;
    if (seatId && desks.some((d) => d.seatId === seatId)) {
      e.preventDefault();
      e.dataTransfer.dropEffect = "move";
      if (dropSeatRef.current !== seatId) setDropSeat(seatId);
    } else if (dropSeatRef.current) {
      setDropSeat(null);
    }
  };
  const onPaneDrop = (e: React.DragEvent) => {
    if (!dropDispatch) return;
    const el = document.elementFromPoint(e.clientX, e.clientY) as HTMLElement | null;
    const desk = el?.closest("[data-desk-id]") as HTMLElement | null;
    const seatId = desk?.getAttribute("data-desk-id") ?? undefined;
    setDropSeat(null);
    if (!seatId) return;
    e.preventDefault();
    const id = e.dataTransfer.getData("text/pactify-draft");
    const d = drafts.find((x) => x.id === id);
    if (d) onDispatchDraft(d, seatId);
  };

  // Wall chart rows: per-feature accepted/total progress.
  const wallRows = useMemo(
    () =>
      state.features.map((f) => {
        const total = f.tasks.length;
        const accepted = f.tasks.filter((t) => t.status === "accepted").length;
        return { id: f.id, accepted, total, pct: total ? Math.round((accepted / total) * 100) : 0 };
      }),
    [state.features],
  );

  const trayVisible = shipped.slice(0, 5);
  const trayMore = shipped.length - trayVisible.length;

  // The shipped tray's DOM, so the accept-lane transit can anchor to its actual
  // on-screen rect (TransitOverlay → screenToFlowPosition).
  const trayRef = useRef<HTMLDivElement>(null);

  return (
    <div
      ref={stageRef}
      className={`office-view absolute inset-0${replaying ? " replaying" : ""}`}
      data-testid="office-view"
      onDragOver={onPaneDragOver}
      onDrop={onPaneDrop}
      // Pane context menu is wired on the stage div (not only RF's
      // onPaneContextMenu) so a right-click anywhere on the empty office surface
      // opens the menu — RF's onPaneContextMenu only fires on the inner
      // .react-flow__pane, which jsdom doesn't reliably hit. DeskNode's own
      // onContextMenu stopPropagation()s, so a desk right-click never reaches
      // this handler (it opens the desk menu instead).
      onContextMenu={onPaneContextMenu}
    >
      <ReactFlow
        nodes={nodes}
        nodeTypes={nodeTypes}
        onNodesChange={onNodesChange}
        onNodeDragStop={onNodeDragStop}
        onPaneClick={() => setMenu(null)}
        nodesDraggable={!replaying}
        nodesConnectable={false}
        elementsSelectable={false}
        // panOnDrag must NOT include button 2, or React Flow swallows the pane
        // contextmenu (mirrors the Plan canvas).
        panOnDrag
        zoomOnScroll
        fitView
        proOptions={{ hideAttribution: true }}
        colorMode="dark"
        style={{ background: "transparent" }}
      >
        {/* Zoom HUD + minimap (shared with Plan) — gives Office real zoom
            controls (spec §3/#4). Must live inside <ReactFlow> (reads the store). */}
        <Hud minimap={false} />
        {/* transit overlay (single lane + carrier ant carrying the parcel) —
            INSIDE <ReactFlow> so it can read the live viewport and draw the lane
            in screen space (flow→screen), tracking pan/zoom/fitView. */}
        {transit && <TransitOverlay transit={transit} trayRef={trayRef} />}
      </ReactFlow>

      {/* Wall chart (fixed top-right) — board5 `.wall`. The fixed furniture panels
          sit INSIDE the office-view div, so their right-click would bubble to the
          stage's onContextMenu and open the pane menu over the panel. Stop it. */}
      <div className="office-wall" data-testid="office-wall" onContextMenu={(e) => e.stopPropagation()}>
        <div className="wt">📋 Wall board · features</div>
        {wallRows.length === 0 && <div className="dempty">No features</div>}
        {wallRows.map((r) => (
          <div className="wrow" key={r.id} style={{ opacity: r.total === 0 ? 0.55 : 1 }}>
            <span className="mono" style={{ fontSize: 10 }}>{r.id}</span>
            <span className="wbar"><i style={{ width: `${r.pct}%` }} /></span>
            <span className="mono" style={{ fontSize: 9, color: "var(--color-text-3)" }}>
              {r.accepted}/{r.total}
            </span>
          </div>
        ))}
      </div>

      {/* Links panel (Phase 4): the selected seat's relationships, right slide-over. */}
      {linksSeat && (
        <LinksPanel seat={linksSeat} state={state} onClose={() => setLinksSeat(null)} onSelectTask={onSelectTask} />
      )}

      {/* Shipped tray (fixed bottom-right) — board5 `.tray`. */}
      <div className="office-tray" data-testid="office-tray" ref={trayRef} onContextMenu={(e) => e.stopPropagation()}>
        <div className="tt">✓ Shipped tray</div>
        {trayVisible.length === 0 && <div className="dempty">Nothing shipped yet</div>}
        {trayVisible.map((t) => (
          <Parcel key={t.id} task={t} onClick={() => onSelectTask?.(t.id)} />
        ))}
        {trayMore > 0 && <div className="tmore">+{trayMore} more</div>}
      </div>

      {/* Draft dock (fixed bottom-left) — always shown in author+live (even with
          zero drafts, so the surface always has a create entry; spec §3). Drag a
          draft onto a desk to dispatch; the empty state offers a New-task button.
          Hidden in replay/observe. */}
      {dropDispatch && (
        <div
          className={`office-dock${dockPulse ? " pulse" : ""}`}
          data-testid="office-dock"
          onContextMenu={(e) => e.stopPropagation()}
        >
          <div className="dt">✎ drafts — drag onto a desk</div>
          {drafts.length === 0 ? (
            <button
              type="button"
              className="dock-new"
              data-testid="dock-new-task"
              onClick={() => onOpenNewTask?.()}
            >
              ＋ New task
            </button>
          ) : (
            drafts.map((d) => (
              <div
                key={d.id}
                className="parcel"
                data-testid={`dock-${d.id}`}
                draggable
                onDragStart={(e) => onParcelDragStart(e, d.id)}
              >
                <span className="pico">◇</span>
                <span className="pid">{d.id}</span>
                <span className="ptitle">{d.feature}</span>
              </div>
            ))
          )}
        </div>
      )}

      {/* Office context menu (author && !replaying). Reuses the .canvas-ctx
          frosted shell; the menu semantics here are Office-specific (pane:
          New task/feature/fit view; desk: one dispatch entry per draft). */}
      {menu && dropDispatch && (
        <div
          className="canvas-ctx"
          data-testid="office-ctx"
          style={{ left: menu.x, top: menu.y }}
          onContextMenu={(e) => e.preventDefault()}
        >
          {menu.kind === "pane" && (
            <>
              <button
                className="ci"
                data-testid="office-ctx-new-task"
                onClick={() => { onOpenNewTask?.(); setMenu(null); }}
              >
                <span className="ci-ic">＋</span>New task
              </button>
              <button
                className="ci"
                data-testid="office-ctx-new-feature"
                onClick={() => { onOpenNewFeature?.(); setMenu(null); }}
              >
                <span className="ci-ic">▭</span>New feature
              </button>
              <div className="ci-sep" />
              <button
                className="ci"
                data-testid="office-ctx-fit"
                onClick={() => { fitView({ duration: 200 }); setMenu(null); }}
              >
                <span className="ci-ic">⊡</span>fit view
              </button>
            </>
          )}
          {menu.kind === "desk" && (
            drafts.length === 0 ? (
              <div className="ci" style={{ opacity: 0.6 }}>No drafts to dispatch</div>
            ) : (
              drafts.map((d) => (
                <button
                  key={d.id}
                  className="ci hot"
                  data-testid={`office-ctx-dispatch-${d.id}`}
                  onClick={() => { onDispatchDraft(d, menu.seatId); setMenu(null); }}
                >
                  <span className="ci-ic">⚡</span>Dispatch {d.id} → {menu.seatId}
                </button>
              ))
            )
          )}
        </div>
      )}
    </div>
  );
}
