import type { Node } from "@xyflow/react";
import type { State } from "./types";

// nextId returns the smallest `${prefix}${N}` (N≥1) not already present in
// `existing`. Used to seed the draft task/feature id forms with a sensible
// default so authors don't have to hand-type ids (the value stays editable and
// slug validation is unchanged). Ids that don't match `${prefix}<positive int>`
// are ignored — only the numeric series for THIS prefix participates.
//
//   nextId([], "t")            → "t1"
//   nextId(["t1","t2"], "t")   → "t3"
//   nextId(["t1","t3"], "t")   → "t2"   (smallest free, not t4)
//   nextId(["foo","t2"], "t")  → "t1"   (non-matching ids ignored)
export function nextId(existing: string[], prefix: string): string {
  const taken = new Set<number>();
  // prefix must be a literal slug ("t"/"f") — it is interpolated unescaped.
  const re = new RegExp(`^${prefix}([1-9][0-9]*)$`);
  for (const id of existing) {
    const m = re.exec(id);
    if (m) taken.add(Number(m[1]));
  }
  let n = 1;
  while (taken.has(n)) n++;
  return `${prefix}${n}`;
}

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

export type FlowNode = {
  id: string;
  type: "task" | "seat" | "feature" | "draft";
  position: { x: number; y: number };
  parentId?: string;
  data: Record<string, unknown>;
};

export type FlowEdge = { id: string; source: string; target: string; kind: "dep" };

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

// deriveFlow folds protocol state + saved layout + in-flight drafts into the
// node/edge graph the canvas renders. Pure and deterministic: it reads only its
// arguments and never mutates them.
export function deriveFlow(
  state: State,
  layout: LayoutJSON,
  drafts: Draft[],
  draftFeatures: DraftFeature[] = [],
): { nodes: FlowNode[]; edges: FlowEdge[] } {
  const nodes: FlowNode[] = [];
  const edges: FlowEdge[] = [];
  const positions = layout.positions ?? {};

  // pos picks the saved position when present, else the deterministic grid slot.
  const pos = (id: string, grid: { x: number; y: number }) => positions[id] ?? grid;

  // Grid fallbacks must AVOID nodes the user already placed (saved positions):
  // a new feature column or task row otherwise lands on top of whatever was
  // dragged there. Deterministic nudge: shift by one slot until free.
  const featureTaken: { x: number; y: number }[] = [];
  const placeFeature = (id: string, grid: { x: number; y: number }) => {
    const saved = positions[id];
    if (saved) {
      featureTaken.push(saved);
      return saved;
    }
    let p = { ...grid };
    const hits = (q: { x: number; y: number }) =>
      featureTaken.some((t) => Math.abs(t.x - q.x) < COL_W && Math.abs(t.y - q.y) < ROW_H * 2);
    while (hits(p)) p = { x: p.x + COL_W, y: p.y };
    featureTaken.push(p);
    return p;
  };
  const childTaken = new Map<string, { x: number; y: number }[]>();
  const placeChild = (featId: string, id: string, grid: { x: number; y: number }) => {
    const list = childTaken.get(featId) ?? [];
    const saved = positions[id];
    if (saved) {
      list.push(saved);
      childTaken.set(featId, list);
      return saved;
    }
    let p = { ...grid };
    const hits = (q: { x: number; y: number }) =>
      list.some((t) => Math.abs(t.x - q.x) < 200 && Math.abs(t.y - q.y) < ROW_H * 0.8);
    while (hits(p)) p = { x: p.x, y: p.y + ROW_H };
    list.push(p);
    childTaken.set(featId, list);
    return p;
  };

  // Every feature's FINAL absolute position (saved or collision-nudged grid),
  // recorded once at placement so drafts target the same coords as the node.
  const placedFeatures = new Map<string, { x: number; y: number }>();

  // Index seat roles so task nodes can color by their owner's roles.
  const rolesOf = new Map<string, string[]>();
  for (const a of state.agents) rolesOf.set(a.id, a.roles);

  // Seat nodes: left rail, fixed x, stacked y by roster order.
  state.agents.forEach((a, i) => {
    const id = `seat:${a.id}`;
    nodes.push({
      id,
      type: "seat",
      position: pos(id, { x: SEAT_X, y: SEAT_Y0 + i * SEAT_DY }),
      data: { roles: a.roles, roleColor: roleColorVar(a.roles) },
    });
  });

  // Feature columns + their task rows. The rail occupies the first column, so
  // features start one column to the right. All positions are ABSOLUTE here —
  // a child's grid fallback is its feature's absolute position plus the child's
  // relative row offset. toParentRelative() later rebases children for React
  // Flow's parent-relative coordinate space.
  state.features.forEach((f, fi) => {
    const featId = `feature:${f.id}`;
    const colX = (fi + 1) * COL_W;
    const featPos = placeFeature(featId, { x: colX, y: FEATURE_Y });
    placedFeatures.set(f.id, featPos);
    // Per-feature progress: accepted / total tasks, surfaced on the frame's
    // progress bar (FeatureGroup v2). all-accepted (total>0 && accepted===total)
    // tints the header green.
    const total = f.tasks.length;
    const accepted = f.tasks.filter((t) => t.status === "accepted").length;
    nodes.push({
      id: featId,
      type: "feature",
      position: featPos,
      data: { id: f.id, branch: f.branch, status: f.status, accepted, total },
    });

    f.tasks.forEach((t, ti) => {
      const id = `task:${t.id}`;
      const deps = t.deps ?? [];
      const ownerRoles = rolesOf.get(t.owner) ?? [];
      nodes.push({
        id,
        type: "task",
        parentId: featId,
        position: placeChild(featId, id, {
          x: featPos.x + PAD,
          y: featPos.y + TASK_REL_Y0 + ti * ROW_H,
        }),
        // Node data carries the Task object itself + the feature + the
        // pre-resolved owner/reviewer roles, so TaskNode reads them directly
        // (no re-materializing a fake Task, no per-seat role lookup).
        data: {
          task: t,
          feature: f.id,
          ownerRoles,
          reviewerRoles: rolesOf.get(t.reviewer) ?? [],
          roleColor: roleColorVar(ownerRoles),
        },
      });
      // Dep edge: source is the dependency, target is the dependent task. The
      // edge id uses an arrow separator so hyphenated slugs stay unambiguous.
      for (const from of deps) {
        edges.push({
          id: `dep:${from}→${t.id}`,
          source: `task:${from}`,
          target: id,
          kind: "dep",
        });
      }
    });
  });

  // Draft feature groups: rendered as empty containers one column past the
  // committed features, so authored-but-unassigned features get a column too.
  // featAbs (built next) folds these in so drafts can target them.
  const committedIds = new Set(state.features.map((f) => f.id));
  draftFeatures.forEach((df, dfi) => {
    if (committedIds.has(df.id)) return; // a committed feature already owns this id
    const featId = `feature:${df.id}`;
    const colX = (state.features.length + 1 + dfi) * COL_W;
    const dfPos = placeFeature(featId, { x: colX, y: FEATURE_Y });
    placedFeatures.set(df.id, dfPos);
    nodes.push({
      id: featId,
      type: "feature",
      position: dfPos,
      data: { id: df.id, branch: df.branch, status: "draft", draft: true, accepted: 0, total: 0 },
    });
  });

  // Draft nodes: rendered under their target feature, stacked below any tasks.
  // We index task counts per feature so drafts continue the row stacking.
  const taskRows = new Map<string, number>();
  for (const f of state.features) taskRows.set(f.id, f.tasks.length);
  // featAbs holds each feature's ABSOLUTE position (saved or grid fallback) so a
  // draft's grid slot can be computed relative to its parent, same as tasks.
  // Committed features come first, then draft features continue the columns.
  const featAbs = placedFeatures; // single source: the positions the nodes actually got
  const draftSeen = new Map<string, number>();

  for (const d of drafts) {
    const id = `draft:${d.id}`;
    const featId = `feature:${d.feature}`;
    const featP = featAbs.get(d.feature) ?? { x: (state.features.length + 1) * COL_W, y: FEATURE_Y };
    const base = taskRows.get(d.feature) ?? 0;
    const slot = draftSeen.get(d.feature) ?? 0;
    draftSeen.set(d.feature, slot + 1);
    nodes.push({
      id,
      type: "draft",
      parentId: featId,
      position: placeChild(featId, id, {
        x: featP.x + PAD,
        y: featP.y + TASK_REL_Y0 + (base + slot) * ROW_H,
      }),
      data: { draft: true, specMd: d.specMd, deps: d.deps },
    });
    for (const from of d.deps) {
      // A dep can point at a committed task OR another draft; prefix the source
      // node id accordingly so draft→draft edges resolve to the right node.
      const fromIsDraft = drafts.some((x) => x.id === from);
      edges.push({
        id: `dep:${from}→${d.id}`,
        source: `${fromIsDraft ? "draft" : "task"}:${from}`,
        target: id,
        kind: "dep",
      });
    }
  }

  return { nodes, edges };
}

// GraphNode is the position-LESS node identity the materialization pipeline
// works with: id + type + parentId + data, exactly as deriveFlow produces them
// but with NO position. Positions are owned solely by `layout` (computed once by
// placeNew at first appearance, thereafter only changed by user drag). deriveGraph
// produces GraphNode[]; placeNew assigns initial coords; mergeNodes folds graph +
// layout into the React Flow node array (merge-by-id, never a full rebuild).
export type GraphNode = {
  id: string;
  type: "task" | "seat" | "feature" | "draft";
  parentId?: string;
  data: Record<string, unknown>;
};

// deriveGraph is deriveFlow minus position: it folds protocol state + in-flight
// drafts into the node IDENTITIES (+ data + parentId) and the dep edges the
// canvas renders. Node/edge identity, data fields (ownerRoles/reviewerRoles/
// roleColor/feature progress/draft spec) and edge prefixes are byte-for-byte the
// same as deriveFlow — only `position` is gone (layout owns it now). Pure and
// deterministic.
export function deriveGraph(
  state: State,
  drafts: Draft[],
  draftFeatures: DraftFeature[] = [],
): { nodes: GraphNode[]; edges: FlowEdge[] } {
  const nodes: GraphNode[] = [];
  const edges: FlowEdge[] = [];

  // Index seat roles so task nodes can color by their owner's roles.
  const rolesOf = new Map<string, string[]>();
  for (const a of state.agents) rolesOf.set(a.id, a.roles);

  // Seat nodes: left rail (position assigned later by placeNew).
  for (const a of state.agents) {
    nodes.push({
      id: `seat:${a.id}`,
      type: "seat",
      data: { roles: a.roles, roleColor: roleColorVar(a.roles) },
    });
  }

  // Committed features + their tasks.
  for (const f of state.features) {
    const featId = `feature:${f.id}`;
    const total = f.tasks.length;
    const accepted = f.tasks.filter((t) => t.status === "accepted").length;
    nodes.push({
      id: featId,
      type: "feature",
      data: { id: f.id, branch: f.branch, status: f.status, accepted, total },
    });
    for (const t of f.tasks) {
      const id = `task:${t.id}`;
      const ownerRoles = rolesOf.get(t.owner) ?? [];
      nodes.push({
        id,
        type: "task",
        parentId: featId,
        data: {
          task: t,
          feature: f.id,
          ownerRoles,
          reviewerRoles: rolesOf.get(t.reviewer) ?? [],
          roleColor: roleColorVar(ownerRoles),
        },
      });
      for (const from of t.deps ?? []) {
        edges.push({ id: `dep:${from}→${t.id}`, source: `task:${from}`, target: id, kind: "dep" });
      }
    }
  }

  // Draft feature groups (authored but not yet in protocol state).
  const committedIds = new Set(state.features.map((f) => f.id));
  for (const df of draftFeatures) {
    if (committedIds.has(df.id)) continue;
    nodes.push({
      id: `feature:${df.id}`,
      type: "feature",
      data: { id: df.id, branch: df.branch, status: "draft", draft: true, accepted: 0, total: 0 },
    });
  }

  // Draft nodes parented to their target feature.
  for (const d of drafts) {
    const id = `draft:${d.id}`;
    nodes.push({
      id,
      type: "draft",
      parentId: `feature:${d.feature}`,
      data: { draft: true, specMd: d.specMd, deps: d.deps },
    });
    for (const from of d.deps) {
      const fromIsDraft = drafts.some((x) => x.id === from);
      edges.push({
        id: `dep:${from}→${d.id}`,
        source: `${fromIsDraft ? "draft" : "task"}:${from}`,
        target: id,
        kind: "dep",
      });
    }
  }

  return { nodes, edges };
}

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

  // Running feature column index: the Nth feature node placed (committed first,
  // then draft features in graph order) lands in column (N+1)*COL_W — matching
  // v1's (fi+1)/(committed+1+dfi) scheme since deriveGraph emits them in order.
  let featCol = 0;
  // Per-feature next child grid row, advanced as children are placed.
  const childRow = new Map<string, number>();
  // A saved feature counts toward the column index too, so the next NEW feature
  // doesn't reuse a column index a saved feature already consumed.

  for (const n of graph.nodes) {
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
// order (seats are emitted first, in roster order, by deriveGraph).
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

// toParentRelative converts deriveFlow's ABSOLUTE child positions into the
// parent-relative coordinates React Flow expects for nodes with extent:'parent'.
// It is the single rebase path: a child's relative position is its absolute
// position minus its parent feature's absolute position. Top-level nodes
// (features, seats) pass through unchanged. Pure — does not mutate its input.
export function toParentRelative(nodes: FlowNode[]): FlowNode[] {
  const absById = new Map(nodes.map((n) => [n.id, n.position]));
  return nodes.map((n) => {
    if (!n.parentId) return n;
    const parent = absById.get(n.parentId);
    if (!parent) return n;
    return {
      ...n,
      position: { x: n.position.x - parent.x, y: n.position.y - parent.y },
    };
  });
}

// applyConnect folds a new dep edge drawn in the canvas into the drafts list.
// Node ids arrive in their flow form ("task:T1" / "draft:D1"); we strip the
// prefix to the raw task id. Semantics: an edge A→B means "B depends on A", so
// the SOURCE is the prerequisite and the TARGET gains the source in its deps.
//
// Only a DRAFT target can change — its deps are still editable. If the target
// is a committed task, deps are fixed at assign time, so we return the drafts
// unchanged (the UI surfaces a toast explaining why). Same-feature constraint
// is enforced by the caller (Canvas) before invoking this helper.
//
// Pure: returns a new array (and a new Draft object for the touched draft);
// never mutates its input. A duplicate dep is a no-op.
export function applyConnect(
  drafts: Draft[],
  sourceId: string,
  targetId: string,
): Draft[] {
  const raw = (id: string) => id.replace(/^(task|draft):/, "");
  const from = raw(sourceId); // prerequisite (A)
  const to = raw(targetId);   // dependent (B) — must be a draft to change deps
  return drafts.map((d) => {
    if (d.id !== to) return d;
    if (d.deps.includes(from)) return d; // already a dep — no-op
    return { ...d, deps: [...d.deps, from] };
  });
}

// DepGraph is the minimal view isValidDep needs: every task/draft id mapped to
// its current dep ids, plus a feature lookup. Built by Canvas from committed
// tasks + drafts.
export interface DepGraph {
  deps: Map<string, string[]>;      // id → its prerequisite ids
  featureOf: (id: string) => string | undefined;
  committed: Set<string>;           // committed task ids (deps fixed at assign)
}

// isValidDep is the single source of truth for whether a dep edge A→B ("B
// depends on A") may be drawn. Rules (acceptance feedback 2.5, surfaced as the
// not-allowed cursor via React Flow's isValidConnection):
//   • no self-loop (A === B)
//   • same feature (deps are same-feature by protocol)
//   • target must be a DRAFT (committed-task deps are fixed at assign time)
//   • no cycle: A must not already (transitively) depend on B
// Pure: reads only its arguments. fromId/toId are RAW ids (no flow prefix).
export function isValidDep(g: DepGraph, fromId: string, toId: string): boolean {
  if (fromId === toId) return false;
  if (g.committed.has(toId)) return false;
  if (g.featureOf(fromId) !== g.featureOf(toId)) return false;
  // Cycle check: would B become a prerequisite of A while A already (transitively)
  // depends on B? Adding A→B means B depends on A; a cycle exists iff A already
  // reaches B through the existing dep chain.
  const seen = new Set<string>();
  const stack = [fromId];
  while (stack.length) {
    const cur = stack.pop()!;
    if (cur === toId) return false; // A reaches B → adding B→…→A closes a loop
    if (seen.has(cur)) continue;
    seen.add(cur);
    for (const d of g.deps.get(cur) ?? []) stack.push(d);
  }
  return true;
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

// childToAbsolute converts a dragged child's parent-relative position (what
// React Flow reports in onNodeDragStop) back to the absolute coordinate the
// layout sidecar stores. parentAbs is the parent feature's CURRENT absolute
// position from the live nodes state (it may itself have been dragged).
export function childToAbsolute(
  childRel: { x: number; y: number },
  parentAbs: { x: number; y: number },
): { x: number; y: number } {
  return { x: childRel.x + parentAbs.x, y: childRel.y + parentAbs.y };
}
