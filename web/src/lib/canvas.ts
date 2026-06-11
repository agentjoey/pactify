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
// .pact/squad/layout.json). positions maps a flow node id to its saved coords.
export type LayoutJSON = { positions?: Record<string, { x: number; y: number }> };

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
