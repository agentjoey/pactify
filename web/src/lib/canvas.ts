import type { State } from "./types";

// Draft is a task being authored in the canvas but not yet assigned (no event
// in the log). It carries the in-flight spec markdown plus its target feature
// and intended deps so it can render alongside committed tasks.
export type Draft = { id: string; specMd: string; feature: string; deps: string[] };

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
const TASK_Y0 = ROW_H; // first task/draft row sits one row below its feature
const SEAT_X = 0; // seats pin to x:0 (the left rail)
const SEAT_Y0 = 0; // first seat row
const SEAT_DY = ROW_H; // vertical gap between stacked seats

// deriveFlow folds protocol state + saved layout + in-flight drafts into the
// node/edge graph the canvas renders. Pure and deterministic: it reads only its
// arguments and never mutates them.
export function deriveFlow(
  state: State,
  layout: LayoutJSON,
  drafts: Draft[],
): { nodes: FlowNode[]; edges: FlowEdge[] } {
  const nodes: FlowNode[] = [];
  const edges: FlowEdge[] = [];
  const positions = layout.positions ?? {};

  // pos picks the saved position when present, else the deterministic grid slot.
  const pos = (id: string, grid: { x: number; y: number }) => positions[id] ?? grid;

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
  // features start one column to the right.
  state.features.forEach((f, fi) => {
    const featId = `feature:${f.id}`;
    const colX = (fi + 1) * COL_W;
    nodes.push({
      id: featId,
      type: "feature",
      position: pos(featId, { x: colX, y: FEATURE_Y }),
      data: { id: f.id, branch: f.branch, status: f.status },
    });

    f.tasks.forEach((t, ti) => {
      const id = `task:${t.id}`;
      const deps = t.deps ?? [];
      const ownerRoles = rolesOf.get(t.owner) ?? [];
      nodes.push({
        id,
        type: "task",
        parentId: featId,
        position: pos(id, { x: colX, y: TASK_Y0 + ti * ROW_H }),
        data: {
          status: t.status,
          owner: t.owner,
          reviewer: t.reviewer,
          deps,
          roleColor: roleColorVar(ownerRoles),
        },
      });
      // Dep edge: source is the dependency, target is the dependent task.
      for (const from of deps) {
        edges.push({
          id: `dep:${from}-${t.id}`,
          source: `task:${from}`,
          target: id,
          kind: "dep",
        });
      }
    });
  });

  // Draft nodes: rendered under their target feature, stacked below any tasks.
  // We index task counts per feature so drafts continue the row stacking.
  const taskRows = new Map<string, number>();
  for (const f of state.features) taskRows.set(f.id, f.tasks.length);
  const featCol = new Map<string, number>();
  state.features.forEach((f, fi) => featCol.set(f.id, fi + 1));
  const draftSeen = new Map<string, number>();

  for (const d of drafts) {
    const id = `draft:${d.id}`;
    const featId = `feature:${d.feature}`;
    const col = featCol.get(d.feature) ?? state.features.length + 1;
    const base = taskRows.get(d.feature) ?? 0;
    const slot = draftSeen.get(d.feature) ?? 0;
    draftSeen.set(d.feature, slot + 1);
    nodes.push({
      id,
      type: "draft",
      parentId: featId,
      position: pos(id, { x: col * COL_W, y: TASK_Y0 + (base + slot) * ROW_H }),
      data: { draft: true, specMd: d.specMd, deps: d.deps },
    });
    for (const from of d.deps) {
      edges.push({
        id: `dep:${from}-${d.id}`,
        source: `task:${from}`,
        target: id,
        kind: "dep",
      });
    }
  }

  return { nodes, edges };
}
