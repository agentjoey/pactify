import type { Node } from "@xyflow/react";

// Draft is a task being authored in the canvas but not yet assigned (no event
// in the log). It carries the in-flight spec markdown plus its target feature
// and intended deps so it can render alongside committed tasks.
export type Draft = { id: string; specMd: string; feature: string; deps: string[] };

// DraftFeature is a feature being authored in the canvas but not yet present in
// protocol state. It renders as an empty feature group so drafts can target it
// before any task lands. id is a slug; branch is its git branch label.
export type DraftFeature = { id: string; branch: string };

// LayoutJSON is the free-form canvas sidecar (stored verbatim server-side at
// .pact/squad/layout.json). `positions` maps a Plan-mode flow node id to its
// saved coords; `office` is an ADDITIVE sidecar key (T10) mapping a seat id to
// its Office-mode desk position. The two keys are independent coordinate spaces —
// Plan drags never touch `office` and Office drags never touch `positions`.
export type LayoutJSON = {
  v?: number; // 2 = current schema (LAYOUT_V); absent = legacy (pre-materialization)
  positions?: Record<string, { x: number; y: number }>; // top-level = absolute; child = parent-relative (RF native space)
  office?: Record<string, { x: number; y: number }>;
};

// LAYOUT_V is the current layout schema version. v2 = position-materialization
// model: layout is the single source of truth, child positions are stored
// PARENT-RELATIVE (v1 stored them absolute). A layout with no `v` field is a v1
// scratch layout; normalizeLayout discards it (migration isn't worth writing —
// only dev-era scratch projects exist; spec §1.3).
export const LAYOUT_V = 2;

// normalizeLayout coerces an opaque server-stored layout blob into a v2
// LayoutJSON. A v2 layout passes through untouched. A legacy layout (no `v`) or
// any non-object (null/undefined/string/number) yields a fresh empty {v:2} so
// every node re-materializes from the deterministic grid. Pure.
export function normalizeLayout(raw: unknown): LayoutJSON {
  if (raw && typeof raw === "object" && (raw as { v?: unknown }).v === LAYOUT_V) {
    return raw as LayoutJSON;
  }
  return { v: LAYOUT_V };
}

// mergeOfficePos folds one seat's Office-mode desk position into the layout
// sidecar's `office` key WITHOUT touching `positions` (the Plan coords) or any
// other office entry. Pure: returns a new object; the additive-sidecar
// invariant (T10) — Office drags never disturb Plan layout — lives here so it is
// unit-testable independent of React Flow's drag plumbing.
export function mergeOfficePos(
  layout: LayoutJSON,
  seatId: string,
  pos: { x: number; y: number },
): LayoutJSON {
  return { ...layout, office: { ...(layout.office ?? {}), [seatId]: pos } };
}

// roleColorVar maps a seat's protocol roles to a brand --role-* token.
//
// Priority: orchestrator → '--role-product', then reviewer → '--role-design',
// then worker → '--role-dev'; unknown/empty → '--role-dev'.
//
// Rationale: a seat may hold several roles, but its *defining duty* drives its
// color. Orchestrator (owns the spec, assigns/accepts — Product) is the most
// senior duty, so it wins when present; reviewer (owns the blueprint — Design)
// outranks plain worker; worker (builds/ships — Dev) is the baseline and the
// safe default for an unrecognized or seatless role set.
export function roleColorVar(roles: string[]): string {
  if (roles.includes("orchestrator")) return "--role-product";
  if (roles.includes("reviewer")) return "--role-design";
  if (roles.includes("worker")) return "--role-dev";
  return "--role-dev";
}

// Grid geometry: features lay out in columns, their tasks stack in rows; seats
// pin to a fixed left rail. Kept deterministic — same input yields same output.
const COL_W = 320; // horizontal gap between feature columns
const ROW_H = 120; // vertical gap between stacked task/draft rows
const FEATURE_Y = 0; // feature group nodes sit on the top row
const SEAT_X = 0; // seats pin to x:0 (the left rail)
const SEAT_Y0 = 0; // first seat row
const SEAT_DY = ROW_H; // vertical gap between stacked seats

// Child layout within a feature group. Children render parent-relative in React
// Flow, so a child's relative origin is (PAD, HEADER) and rows stack by ROW_H.
const PAD = 16; // inner padding of the feature container
const HEADER = 28; // height reserved for the feature header
const TASK_REL_Y0 = HEADER + PAD; // first task/draft row, relative to the feature

// CHILD_W is the nominal task/draft card width, used both for the feature
// container's default width and as the bbox estimate for an unmeasured child
// (no `measured` yet) when sizing the container around dragged children.
export const CHILD_W = 200;
const CHILD_H = 80; // nominal task/draft card height (bbox estimate when unmeasured)

// featureStyle returns the feature container's default {width,height} sized to
// hold `rows` stacked children (min 1 row). mergeNodes takes the max of this and
// the actual child bbox so a child dragged outside the default frame stays
// bounded. Lives in lib (was Canvas.tsx) so the merge layer can import it.
export function featureStyle(rows: number): { width: number; height: number } {
  const r = Math.max(rows, 1);
  return { width: CHILD_W + PAD * 2, height: HEADER + r * ROW_H + PAD };
}

// GraphNode is the position-LESS node identity the materialization pipeline
// works with: id + type + parentId + data. Positions are owned solely by
// `layout` (computed once by placeNew at first appearance, thereafter only
// changed by user drag). placeNew assigns initial coords; mergeNodes folds graph
// + layout into the React Flow node array (merge-by-id, never a full rebuild).
export type GraphNode = {
  id: string;
  type: "task" | "seat" | "feature" | "draft";
  parentId?: string;
  data: Record<string, unknown>;
};

// placeNew assigns a one-time initial position to every graph node that has NO
// entry in layout.positions yet. It is idempotent: an id already present in the
// layout NEVER appears in the result (its saved coords are authoritative). The
// return value is ONLY the new entries — the caller folds them into layout.
//
// Coordinate model (v2): top-level nodes (seat/feature) get ABSOLUTE coords;
// children (task/draft) get PARENT-RELATIVE coords (x=PAD, y=TASK_REL_Y0 + row*ROW_H)
// — the v1 absolute-then-rebase path is gone.
//
// Collision avoidance (carried over from v1 placeFeature/placeChild) runs against
// the union of "already-saved layout entries" and "siblings assigned earlier in
// THIS batch", split by id prefix so each node type avoids only its own kind:
//   • feature column: |dx|<COL_W && |dy|<ROW_H*2 → shift one column right.
//   • child row (parent-relative): |dy|<ROW_H*0.8 (same x) → drop one row.
export function placeNew(
  layout: LayoutJSON,
  graph: { nodes: GraphNode[] },
): Record<string, { x: number; y: number }> {
  const saved = layout.positions ?? {};
  const add: Record<string, { x: number; y: number }> = {};

  // Ids present in the CURRENT graph (visible nodes). Used to filter the
  // occupancy seeding below: a saved layout entry whose id is NOT in the graph
  // is an "orphan" (e.g. a node temporarily gone during replay(?at)) and must
  // NOT occupy a grid slot — an invisible node should not block placement of a
  // visible one. NOTE: we only skip it from the occupancy POOLS; the orphan
  // entry itself stays in layout.positions untouched (it is that node's position
  // MEMORY for when it reappears — never delete/prune it here).
  const graphIds = new Set(graph.nodes.map((n) => n.id));

  // Occupancy pools seeded from already-saved entries, split by node kind so a
  // feature only avoids features and a child only avoids its own feature's
  // children. Children are stored parent-relative in layout, so the saved values
  // are already in the per-feature relative space we compare in.
  const featureTaken: { x: number; y: number }[] = [];
  const childTaken = new Map<string, { x: number; y: number }[]>(); // featureId → relative slots
  // Map each known child id → its parent feature so saved child entries land in
  // the right per-feature pool.
  const parentOf = new Map<string, string>();
  for (const n of graph.nodes) if (n.parentId) parentOf.set(n.id, n.parentId);
  for (const [id, p] of Object.entries(saved)) {
    if (!graphIds.has(id)) continue; // orphan entry: keep in layout, exclude from occupancy
    if (id.startsWith("feature:")) {
      featureTaken.push(p);
    } else if (id.startsWith("task:") || id.startsWith("draft:")) {
      const feat = parentOf.get(id);
      if (feat) {
        const list = childTaken.get(feat) ?? [];
        list.push(p);
        childTaken.set(feat, list);
      }
    }
  }

  // Defensive de-dup: skip any id that appears more than once in graph.nodes
  // (first wins). This guards against a malformed graph silently double-counting
  // columns/rows or overwriting `add`.
  const seenIds = new Set<string>();

  // Running feature column index: the Nth feature node placed (committed first,
  // then draft features in graph order) lands in column (N+1)*COL_W — matching
  // v1's (fi+1)/(committed+1+dfi) scheme.
  let featCol = 0;
  // Per-feature next child grid row, advanced as children are placed.
  const childRow = new Map<string, number>();
  // A saved feature counts toward the column index too, so the next NEW feature
  // doesn't reuse a column index a saved feature already consumed.

  for (const n of graph.nodes) {
    if (seenIds.has(n.id)) continue; // dup id (first wins) — defensive, see above
    seenIds.add(n.id);
    if (n.type === "seat") {
      if (saved[n.id]) continue;
      const i = seatIndex(graph.nodes, n.id);
      add[n.id] = { x: SEAT_X, y: SEAT_Y0 + i * SEAT_DY };
      continue;
    }
    if (n.type === "feature") {
      const col = featCol++;
      if (saved[n.id]) continue; // saved features still consume a column index
      let p = { x: (col + 1) * COL_W, y: FEATURE_Y };
      const hits = (q: { x: number; y: number }) =>
        featureTaken.some((t) => Math.abs(t.x - q.x) < COL_W && Math.abs(t.y - q.y) < ROW_H * 2);
      while (hits(p)) p = { x: p.x + COL_W, y: p.y };
      featureTaken.push(p);
      add[n.id] = p;
      continue;
    }
    // task / draft — parent-relative position within the feature.
    const feat = n.parentId ?? "";
    const row = childRow.get(feat) ?? 0;
    childRow.set(feat, row + 1);
    if (saved[n.id]) continue;
    const list = childTaken.get(feat) ?? [];
    let p = { x: PAD, y: TASK_REL_Y0 + row * ROW_H };
    const hits = (q: { x: number; y: number }) =>
      list.some((t) => Math.abs(t.x - q.x) < CHILD_W && Math.abs(t.y - q.y) < ROW_H * 0.8);
    while (hits(p)) p = { x: p.x, y: p.y + ROW_H };
    list.push(p);
    childTaken.set(feat, list);
    add[n.id] = p;
  }

  return add;
}

// seatIndex returns a seat node's roster index among the seat nodes in graph
// order (seats are emitted first, in roster order).
function seatIndex(nodes: GraphNode[], id: string): number {
  let i = 0;
  for (const n of nodes) {
    if (n.type !== "seat") continue;
    if (n.id === id) return i;
    i++;
  }
  return 0;
}

// mergeNodes folds the derived graph + layout into the React Flow node array by
// MERGING BY ID — never a full rebuild, so React Flow's per-node measurement and
// interaction state survives. For an id already in `prev`, it keeps prev's RF
// write-back fields (position/measured/selected/dragging/width/height) and
// replaces only `data`. For a new id, position comes from layout.positions
// (fallback {0,0}); children get parentId + expandParent, tasks also extent:'parent'
// (drafts omit extent so they can be dragged out to the seat rail to dispatch).
// Vanished ids are dropped. Feature nodes are emitted first (RF requires a parent
// before its children); the rest follow graph order. NEW nodes carry no
// hand-written measured/handles — React Flow measures them; a prev node's
// measured is RF's own write-back and is preserved as-is.
//
// Feature container style {width,height} = max(featureStyle default, child bbox +
// PAD). A child's bbox uses its prev `measured` when present, else CHILD_W×CHILD_H.
export function mergeNodes(
  prev: Node[],
  graph: { nodes: GraphNode[] },
  layout: LayoutJSON,
): Node[] {
  const prevById = new Map(prev.map((n) => [n.id, n]));
  const positions = layout.positions ?? {};

  // Resolve each child's CURRENT parent-relative position (prev drag wins over
  // the freshly-placed layout value) so feature containers can be sized to bound
  // children that were dragged far from their default slot.
  const childRel = (id: string): { x: number; y: number } =>
    prevById.get(id)?.position ?? positions[id] ?? { x: 0, y: 0 };
  const childSize = (id: string): { w: number; h: number } => {
    const m = prevById.get(id)?.measured;
    return { w: m?.width ?? CHILD_W, h: m?.height ?? CHILD_H };
  };

  // Per-feature child row count + bbox of its children (for container sizing).
  const childCount = new Map<string, number>();
  const childExtent = new Map<string, { w: number; h: number }>();
  for (const n of graph.nodes) {
    if (!n.parentId) continue;
    childCount.set(n.parentId, (childCount.get(n.parentId) ?? 0) + 1);
    const rel = childRel(n.id);
    const sz = childSize(n.id);
    const cur = childExtent.get(n.parentId) ?? { w: 0, h: 0 };
    childExtent.set(n.parentId, {
      w: Math.max(cur.w, rel.x + sz.w),
      h: Math.max(cur.h, rel.y + sz.h),
    });
  }

  const build = (n: GraphNode): Node => {
    const existing = prevById.get(n.id);
    if (n.type === "feature") {
      const def = featureStyle(childCount.get(n.id) ?? 0);
      const ext = childExtent.get(n.id) ?? { w: 0, h: 0 };
      const style = {
        width: Math.max(def.width, ext.w + PAD),
        height: Math.max(def.height, ext.h + PAD),
      };
      if (existing) {
        // Reference stability: when nothing observable changed — same data
        // reference AND same computed style (width/height) — return the prev
        // object untouched so React Flow sees an identical reference and skips
        // re-render.
        const ex = existing.style as { width?: number; height?: number } | undefined;
        if (existing.data === n.data && ex?.width === style.width && ex?.height === style.height) {
          return existing;
        }
        return { ...existing, data: n.data, style };
      }
      return {
        id: n.id,
        type: "feature",
        position: positions[n.id] ?? { x: 0, y: 0 },
        data: n.data,
        style,
      };
    }
    // seat / task / draft
    if (existing) {
      // Reference stability: same data reference → return prev untouched (no new
      // object) so RF skips re-render.
      if (existing.data === n.data) return existing;
      return { ...existing, data: n.data };
    }
    const node: Node = {
      id: n.id,
      type: n.type,
      position: positions[n.id] ?? { x: 0, y: 0 },
      data: n.data,
    };
    if (n.parentId) {
      node.parentId = n.parentId;
      node.expandParent = true;
      if (n.type === "task") node.extent = "parent";
    }
    return node;
  };

  // Feature nodes first (RF parent-before-child), then everything else in graph
  // order. Vanished ids are simply never visited.
  const out: Node[] = [];
  for (const n of graph.nodes) if (n.type === "feature") out.push(build(n));
  for (const n of graph.nodes) if (n.type !== "feature") out.push(build(n));
  return out;
}

// AntEdgeKind classifies an ant-bearing edge for the AntEdge renderer:
//   "wait"    → messenger ant (review/rework comms wait edges)
//   "dep"     → carrier ant + cargo cube (unmet/derived dependency edges)
//   "blocked" → carrier ant + cargo cube (blocked dep edges; same visual as dep)
export type AntEdgeKind = "wait" | "dep" | "blocked";

// ANT_CAP is the per-render ceiling on simultaneously animated ant edges. Beyond
// it, ant-bearing edges fall back to a plain dashed path (no animateMotion) so
// the canvas never animates dozens of ants at once. Spec §3 / board4 §①.
export const ANT_CAP = 6;

// assignAntFlags decides which of the ant-eligible edges actually carry a
// crawling ant this render. Comms WAIT edges get priority over dep/blocked
// edges; within a kind, input order is preserved. The first ANT_CAP eligible
// edges get ant=true, the rest ant=false. Returns a Set of the edge ids that
// won the ant slot — pure, deterministic, no global mutable state.
//
// `edges` is the list of ant-eligible edges with their kind; the caller filters
// to only edges that should ever bear an ant (dep + wait), then this assigns the
// scarce animation budget. reducedMotion short-circuits to an empty set (no ant
// anywhere) so the cap logic and the motion gate share one decision point.
export function assignAntFlags(
  edges: { id: string; kind: AntEdgeKind }[],
  reducedMotion = false,
): Set<string> {
  if (reducedMotion) return new Set();
  const ordered = [
    ...edges.filter((e) => e.kind === "wait"),
    ...edges.filter((e) => e.kind !== "wait"),
  ];
  return new Set(ordered.slice(0, ANT_CAP).map((e) => e.id));
}
