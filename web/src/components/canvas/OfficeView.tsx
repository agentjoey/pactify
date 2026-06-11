import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import {
  ReactFlow,
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
import { statusColorVar } from "../../lib/lifecycle";
import { CarrierAnt } from "./edges/AntEdge";

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
  return "rgba(255,255,255,.25)";
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
  author: boolean;
  dropTarget: boolean; // currently dragged-over by a dock parcel
  onSelectTask?: (id: string) => void;
  onDeskClick?: (seatId: string) => void;
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
      onClick={() => d.onDeskClick?.(desk.seatId)}
    >
      <div className="dhead">
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
        {/* 手上 doing */}
        <div className="zlab">
          手上 · doing <span className="zc">{desk.doing.length}</span>
        </div>
        {desk.doing.length === 0 ? (
          <div className="dempty">{desk.status === "idle" ? "闲——拖一个任务到这张桌子即派发" : "空"}</div>
        ) : (
          desk.doing.map((t) => <Parcel key={t.id} task={t} onClick={() => d.onSelectTask?.(t.id)} />)
        )}

        {/* 收件 inbox */}
        <div className="zlab">
          收件 · inbox <span className="zc">{desk.inbox.length}</span>
        </div>
        {desk.inbox.length === 0 ? (
          <div className="dempty">空</div>
        ) : (
          desk.inbox.map((t) => <Parcel key={t.id} task={t} glow onClick={() => d.onSelectTask?.(t.id)} />)
        )}

        {/* 等回音 waiting on */}
        <div className="zlab">
          等回音 · waiting on <span className="zc">{desk.waitingOn.length}</span>
        </div>
        {desk.waitingOn.length === 0 ? (
          <div className="dempty">空</div>
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

function prefersReducedMotion(): boolean {
  return (
    typeof window !== "undefined" &&
    typeof window.matchMedia === "function" &&
    window.matchMedia("(prefers-reduced-motion: reduce)").matches
  );
}

// Transit kind → lane color + ant species feel (carrier ant always; the parcel
// rides it). checkpoint owner→reviewer (blue), changes reviewer→owner (red),
// accept desk→tray (green).
type Transit = {
  taskId: string;
  from: { x: number; y: number };
  to: { x: number; y: number };
  color: string;
};

export function OfficeView({
  state,
  layout,
  author,
  replaying,
  pulses,
  drafts,
  onSaveOffice,
  onSelectTask,
  onDispatchDraft,
}: {
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
}) {
  const { desks, shipped } = useMemo(() => deriveOffice(state), [state]);
  const dropDispatch = author && !replaying;

  // Desk position bookkeeping (RF state). Positions: saved office key → grid.
  const [nodes, setNodes] = useState<Node[]>([]);
  const [dropSeat, setDropSeat] = useState<string | null>(null);
  const dropSeatRef = useRef<string | null>(null);
  dropSeatRef.current = dropSeat;

  // Single draft → click-dispatch convenience target.
  const onDeskClick = useCallback(
    (seatId: string) => {
      if (!dropDispatch) return;
      if (drafts.length === 1) onDispatchDraft(drafts[0], seatId);
    },
    [dropDispatch, drafts, onDispatchDraft],
  );

  // Rebuild desk nodes from the derived office whenever it (or layout/flags)
  // change. Positions come from the office sidecar; missing seats get the grid.
  useEffect(() => {
    const office = layout.office ?? {};
    setNodes(
      desks.map((desk, i) => ({
        id: `desk:${desk.seatId}`,
        type: "desk",
        position: office[desk.seatId] ?? gridPos(i),
        data: {
          desk,
          author,
          dropTarget: dropSeatRef.current === desk.seatId,
          onSelectTask,
          onDeskClick,
        } satisfies DeskNodeData,
        draggable: !replaying,
      })),
    );
  }, [desks, layout.office, author, replaying, onSelectTask, onDeskClick]);

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
  // at a time per task; reduced-motion → skip (the parcel just re-renders).
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
        // tray sits bottom-right of the stage; aim toward it.
        to = { x: (from?.x ?? 0) + 400, y: (from?.y ?? 0) + 240 };
        color = "var(--color-success)";
      }
      if (from && to) {
        setTransit({ taskId: id, from, to, color });
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
  const onDeskDragOver = (e: React.DragEvent, seatId: string) => {
    if (!dropDispatch) return;
    e.preventDefault();
    e.dataTransfer.dropEffect = "move";
    if (dropSeatRef.current !== seatId) setDropSeat(seatId);
  };
  const onDeskDrop = (e: React.DragEvent, seatId: string) => {
    if (!dropDispatch) return;
    e.preventDefault();
    setDropSeat(null);
    const id = e.dataTransfer.getData("text/pactify-draft");
    const d = drafts.find((x) => x.id === id);
    if (d) onDispatchDraft(d, seatId);
  };

  // The desk DOM (RF node) is the drop zone; we attach the HTML5 handlers at the
  // pane level by delegating via data-testid lookup. RF nodes already render the
  // desk markup, so we listen on the wrapping pane and resolve the desk under the
  // pointer through elementFromPoint at drop time.
  const onPaneDragOver = (e: React.DragEvent) => {
    if (!dropDispatch) return;
    const el = document.elementFromPoint(e.clientX, e.clientY) as HTMLElement | null;
    const desk = el?.closest("[data-testid^='desk-']") as HTMLElement | null;
    const seatId = desk?.getAttribute("data-testid")?.replace(/^desk-/, "");
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
    const desk = el?.closest("[data-testid^='desk-']") as HTMLElement | null;
    const seatId = desk?.getAttribute("data-testid")?.replace(/^desk-/, "");
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

  // Suppress unused warnings for the per-desk DnD handlers kept for symmetry;
  // the pane-delegated handlers are the active path under RF's node layering.
  void onDeskDragOver;
  void onDeskDrop;
  void onParcelDragStart;

  return (
    <div className={`office-view absolute inset-0${replaying ? " replaying" : ""}`} data-testid="office-view" onDragOver={onPaneDragOver} onDrop={onPaneDrop}>
      {/* transit overlay (single lane + carrier ant carrying the parcel) */}
      {transit && (
        <svg
          data-testid="office-transit"
          style={{ position: "absolute", inset: 0, width: "100%", height: "100%", overflow: "visible", zIndex: 5, pointerEvents: "none" }}
        >
          <path
            id={`office-lane-${transit.taskId}`}
            className="office-lane"
            d={`M ${transit.from.x} ${transit.from.y} L ${transit.to.x} ${transit.to.y}`}
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
      )}

      <ReactFlow
        nodes={nodes}
        nodeTypes={nodeTypes}
        onNodesChange={onNodesChange}
        onNodeDragStop={onNodeDragStop}
        nodesDraggable={!replaying}
        nodesConnectable={false}
        elementsSelectable={false}
        panOnDrag
        zoomOnScroll
        fitView
        proOptions={{ hideAttribution: true }}
        colorMode="dark"
        style={{ background: "transparent" }}
      />

      {/* Wall chart (fixed top-right) — board5 `.wall`. */}
      <div className="office-wall" data-testid="office-wall">
        <div className="wt">📋 墙上看板 · features</div>
        {wallRows.length === 0 && <div className="dempty">无 feature</div>}
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

      {/* Shipped tray (fixed bottom-right) — board5 `.tray`. */}
      <div className="office-tray" data-testid="office-tray">
        <div className="tt">✓ shipped 出货托盘</div>
        {trayVisible.length === 0 && <div className="dempty">尚无出货</div>}
        {trayVisible.map((t) => (
          <Parcel key={t.id} task={t} onClick={() => onSelectTask?.(t.id)} />
        ))}
        {trayMore > 0 && <div className="tmore">+{trayMore} more</div>}
      </div>

      {/* Draft dock (fixed bottom-left) — drag a draft onto a desk to dispatch.
          Author-and-live only; hidden in replay/observe. */}
      {dropDispatch && drafts.length > 0 && (
        <div className="office-dock" data-testid="office-dock">
          <div className="dt">✎ drafts — drag onto a desk</div>
          {drafts.map((d) => (
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
          ))}
        </div>
      )}
    </div>
  );
}
