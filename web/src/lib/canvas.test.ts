import { describe, it, expect } from "vitest";
import {
  roleColorVar,
  applyConnect,
  isValidDep,
  assignAntFlags,
  ANT_CAP,
  mergeOfficePos,
  nextId,
  normalizeLayout,
  deriveGraph,
  placeNew,
  mergeNodes,
  featureStyle,
  LAYOUT_V,
  type Draft,
  type DepGraph,
  type LayoutJSON,
} from "./canvas";
import type { Node } from "@xyflow/react";
import type { State } from "./types";

describe("roleColorVar", () => {
  it("orchestrator wins over reviewer → product", () => {
    expect(roleColorVar(["orchestrator", "reviewer"])).toBe("--role-product");
  });
  it("reviewer wins over worker → design", () => {
    expect(roleColorVar(["reviewer", "worker"])).toBe("--role-design");
  });
  it("worker alone → dev", () => {
    expect(roleColorVar(["worker"])).toBe("--role-dev");
  });
  it("empty roles → dev (default)", () => {
    expect(roleColorVar([])).toBe("--role-dev");
  });
});

// fixture: 2 features / 3 tasks (T2 depends on T1) / 2 seats.
const state: State = {
  project: "p",
  agents: [
    { id: "claude-opus", roles: ["orchestrator", "reviewer"] },
    { id: "opencode", roles: ["worker"] },
  ],
  features: [
    {
      id: "F1",
      branch: "feat/x",
      status: "in_progress",
      tasks: [
        { id: "T1", owner: "opencode", status: "in_progress", reviewer: "claude-opus", spec: "s", evidence: "" },
        { id: "T2", owner: "opencode", status: "assigned", reviewer: "claude-opus", spec: "s", evidence: "", deps: ["T1"] },
      ],
    },
    {
      id: "F2",
      branch: "feat/y",
      status: "in_progress",
      tasks: [
        { id: "T3", owner: "claude-opus", status: "assigned", reviewer: "claude-opus", spec: "s", evidence: "" },
      ],
    },
  ],
  awaiting_count: 0,
};

const noDrafts: Draft[] = [];

describe("applyConnect", () => {
  const drafts: Draft[] = [
    { id: "D1", specMd: "x", feature: "F1", deps: [] },
    { id: "D2", specMd: "y", feature: "F1", deps: ["T1"] },
  ];

  it("draft target gains the source task id as a dep (A→B = B depends on A)", () => {
    const out = applyConnect(drafts, "task:T1", "draft:D1");
    expect(out.find((d) => d.id === "D1")!.deps).toEqual(["T1"]);
    // input not mutated
    expect(drafts.find((d) => d.id === "D1")!.deps).toEqual([]);
  });

  it("source may itself be a draft (draft → draft dep)", () => {
    const out = applyConnect(drafts, "draft:D1", "draft:D2");
    expect(out.find((d) => d.id === "D2")!.deps).toEqual(["T1", "D1"]);
  });

  it("a duplicate dep is a no-op", () => {
    const out = applyConnect(drafts, "task:T1", "draft:D2");
    expect(out.find((d) => d.id === "D2")!.deps).toEqual(["T1"]);
  });

  it("a committed (non-draft) target is unchanged — deps fixed at assign time", () => {
    // target task:T2 is not in drafts, so nothing changes.
    const out = applyConnect(drafts, "task:T1", "task:T2");
    expect(out).toEqual(drafts);
  });
});

describe("nextId", () => {
  it("empty list → prefix + 1", () => {
    expect(nextId([], "t")).toBe("t1");
    expect(nextId([], "f")).toBe("f1");
  });

  it("contiguous run → next number", () => {
    expect(nextId(["t1", "t2"], "t")).toBe("t3");
  });

  it("gap → smallest free number, not next-highest", () => {
    expect(nextId(["t1", "t3"], "t")).toBe("t2");
  });

  it("ignores ids that don't match the prefix series", () => {
    // 'foo' and a different-prefix 'f2' must not influence the 't' series.
    expect(nextId(["foo", "f2", "t2"], "t")).toBe("t1");
  });

  it("ignores leading-zero / non-positive matches", () => {
    // 't0' and 't01' are not valid `${prefix}<positive-int>` ids.
    expect(nextId(["t0", "t01"], "t")).toBe("t1");
  });

  it("counts both committed tasks and current drafts (caller passes the union)", () => {
    expect(nextId(["t1", "t2", "t3"], "t")).toBe("t4");
  });
});

describe("isValidDep (connect UX validation, T8)", () => {
  // graph helper: f1 owns t1,t2,t3 (t3 committed); f2 owns x1. t2 depends on t1.
  const make = (): DepGraph => ({
    deps: new Map<string, string[]>([
      ["t1", []],
      ["t2", ["t1"]],
      ["t3", ["t2"]],
      ["x1", []],
    ]),
    featureOf: (id) => (id === "x1" ? "f2" : "f1"),
    committed: new Set(["t3"]),
  });

  it("rejects a self-loop", () => {
    expect(isValidDep(make(), "t1", "t1")).toBe(false);
  });

  it("rejects a cross-feature edge", () => {
    expect(isValidDep(make(), "x1", "t1")).toBe(false);
  });

  it("rejects a committed target (deps fixed at assign)", () => {
    expect(isValidDep(make(), "t1", "t3")).toBe(false);
  });

  it("rejects an edge that would create a cycle", () => {
    // t2 already depends (transitively) on t1; adding t1→t2 means t2 depends on
    // t1 again — fine — but t2→t1 (t1 depends on t2) would close a loop since
    // t2 reaches t1 via its existing deps.
    expect(isValidDep(make(), "t2", "t1")).toBe(false);
  });

  it("accepts a valid same-feature, draft-target, acyclic edge", () => {
    // t1→t2: t2 already depends on t1, but that's a duplicate not a cycle on the
    // *new* direction. Use an independent draft target to be unambiguous.
    const g = make();
    g.deps.set("t4", []);
    g.featureOf = (id) => (id === "x1" ? "f2" : "f1");
    expect(isValidDep(g, "t1", "t4")).toBe(true);
  });
});

describe("assignAntFlags (cap + priority, T8)", () => {
  it("caps at ANT_CAP, first eligible win", () => {
    const edges = Array.from({ length: ANT_CAP + 2 }, (_, i) => ({ id: `e${i}`, kind: "dep" as const }));
    const flags = assignAntFlags(edges);
    expect(flags.size).toBe(ANT_CAP);
    expect(flags.has("e0")).toBe(true);
    expect(flags.has(`e${ANT_CAP}`)).toBe(false);
  });

  it("wait edges jump ahead of dep edges", () => {
    const deps = Array.from({ length: ANT_CAP }, (_, i) => ({ id: `d${i}`, kind: "dep" as const }));
    const flags = assignAntFlags([...deps, { id: "w", kind: "wait" }]);
    expect(flags.has("w")).toBe(true);
    expect(flags.has(`d${ANT_CAP - 1}`)).toBe(false);
  });

  it("reduced motion → empty", () => {
    expect(assignAntFlags([{ id: "e", kind: "wait" }], true).size).toBe(0);
  });
});

describe("mergeOfficePos", () => {
  it("adds a seat's desk position under the office key", () => {
    const out = mergeOfficePos({}, "bob", { x: 5, y: 6 });
    expect(out.office).toEqual({ bob: { x: 5, y: 6 } });
  });

  it("never disturbs the Plan positions key (additive sidecar invariant)", () => {
    const layout: LayoutJSON = { positions: { "task:T1": { x: 1, y: 2 } } };
    const out = mergeOfficePos(layout, "alice", { x: 9, y: 9 });
    expect(out.positions).toEqual({ "task:T1": { x: 1, y: 2 } });
    expect(out.office).toEqual({ alice: { x: 9, y: 9 } });
    // input is not mutated
    expect(layout.office).toBeUndefined();
  });

  it("merges alongside existing office entries without dropping them", () => {
    const layout: LayoutJSON = { office: { bob: { x: 1, y: 1 } } };
    const out = mergeOfficePos(layout, "alice", { x: 2, y: 2 });
    expect(out.office).toEqual({ bob: { x: 1, y: 1 }, alice: { x: 2, y: 2 } });
  });
});

describe("normalizeLayout", () => {
  it("a v2 layout passes through unchanged", () => {
    const raw = {
      v: 2,
      positions: { "feature:F1": { x: 320, y: 0 }, "task:T1": { x: 16, y: 44 } },
      office: { "claude-opus": { x: 60, y: 40 } },
    };
    expect(normalizeLayout(raw)).toEqual(raw);
  });

  it("an old layout with no v field → drop positions/office, return {v:2}", () => {
    const old = { positions: { "task:T1": { x: 1, y: 2 } }, office: { bob: { x: 3, y: 4 } } };
    expect(normalizeLayout(old)).toEqual({ v: LAYOUT_V });
  });

  it("a non-object / null → {v:2}", () => {
    expect(normalizeLayout(null)).toEqual({ v: LAYOUT_V });
    expect(normalizeLayout(undefined)).toEqual({ v: LAYOUT_V });
    expect(normalizeLayout("nope")).toEqual({ v: LAYOUT_V });
    expect(normalizeLayout(42)).toEqual({ v: LAYOUT_V });
  });
});

describe("deriveGraph", () => {
  it("emits seat/feature/task/draft nodes with the same identity/data/parentId as deriveFlow but NO position", () => {
    const drafts: Draft[] = [{ id: "D1", specMd: "# draft", feature: "F1", deps: ["T1"] }];
    const { nodes } = deriveGraph(state, drafts);
    const byId = new Map(nodes.map((n) => [n.id, n]));
    const ids = nodes.map((n) => n.id).sort();
    expect(ids).toEqual(
      [
        "feature:F1", "feature:F2", "seat:claude-opus", "seat:opencode",
        "task:T1", "task:T2", "task:T3", "draft:D1",
      ].sort(),
    );
    // no position on any node
    for (const n of nodes) {
      expect("position" in n).toBe(false);
    }
    // parentId carried
    expect(byId.get("task:T1")!.parentId).toBe("feature:F1");
    expect(byId.get("draft:D1")!.parentId).toBe("feature:F1");
    expect(byId.get("feature:F1")!.parentId).toBeUndefined();
    expect(byId.get("seat:opencode")!.parentId).toBeUndefined();
    // data parity with deriveFlow on a task node
    const t2 = byId.get("task:T2")!;
    expect((t2.data.task as { id: string }).id).toBe("T2");
    expect(t2.data.feature).toBe("F1");
    expect(t2.data.ownerRoles).toEqual(["worker"]);
    expect(t2.data.reviewerRoles).toEqual(["orchestrator", "reviewer"]);
    expect(t2.data.roleColor).toBe("--role-dev");
    // feature progress data parity
    const f1 = byId.get("feature:F1")!;
    expect(f1.data.total).toBe(2);
    expect(f1.data.accepted).toBe(0);
    // seat data parity
    expect(byId.get("seat:claude-opus")!.data.roleColor).toBe("--role-product");
  });

  it("dep edges carry id/source/target/kind (the one dep in the fixture: T2→T1)", () => {
    // The fixture's only dependency is T2 depends on T1; deriveFlow's edge output
    // was deleted with that function, so the expected edge set is inlined here.
    const graphEdges = deriveGraph(state, noDrafts).edges;
    expect(graphEdges).toEqual([
      { id: "dep:T1→T2", source: "task:T1", target: "task:T2", kind: "dep" },
    ]);
  });

  it("a draft→draft dep sources from the draft node (prefix correct)", () => {
    const drafts: Draft[] = [
      { id: "A", specMd: "x", feature: "F1", deps: [] },
      { id: "B", specMd: "y", feature: "F1", deps: ["A", "T1"] },
    ];
    const { edges } = deriveGraph(state, drafts);
    expect(edges).toContainEqual({ id: "dep:A→B", source: "draft:A", target: "draft:B", kind: "dep" });
    expect(edges).toContainEqual({ id: "dep:T1→B", source: "task:T1", target: "draft:B", kind: "dep" });
  });
});

describe("placeNew", () => {
  it("empty layout: feature columns, task rows (parent-relative), seat rail match the v1 grid", () => {
    const { nodes } = deriveGraph(state, noDrafts);
    const add = placeNew({ v: LAYOUT_V }, { nodes });
    // seats: left rail x:0, stacked by SEAT_DY=120
    expect(add["seat:claude-opus"]).toEqual({ x: 0, y: 0 });
    expect(add["seat:opencode"]).toEqual({ x: 0, y: 120 });
    // features: column (fi+1)*320, y:0
    expect(add["feature:F1"]).toEqual({ x: 320, y: 0 });
    expect(add["feature:F2"]).toEqual({ x: 640, y: 0 });
    // tasks: PARENT-RELATIVE (x:PAD=16, y:44 + row*120)
    expect(add["task:T1"]).toEqual({ x: 16, y: 44 });
    expect(add["task:T2"]).toEqual({ x: 16, y: 164 });
    // T3 is first child of F2 → row 0
    expect(add["task:T3"]).toEqual({ x: 16, y: 44 });
  });

  it("a draft stacks below committed tasks in its feature (parent-relative rows)", () => {
    const drafts: Draft[] = [{ id: "D1", specMd: "x", feature: "F1", deps: [] }];
    const { nodes } = deriveGraph(state, drafts);
    const add = placeNew({ v: LAYOUT_V }, { nodes });
    // F1 has T1(row0) T2(row1) → draft is row2
    expect(add["draft:D1"]).toEqual({ x: 16, y: 44 + 2 * 120 });
  });

  it("ids already present in layout.positions never appear in the result (idempotent)", () => {
    const { nodes } = deriveGraph(state, noDrafts);
    const layout: LayoutJSON = {
      v: LAYOUT_V,
      positions: { "task:T1": { x: 16, y: 44 }, "feature:F1": { x: 320, y: 0 } },
    };
    const add = placeNew(layout, { nodes });
    expect("task:T1" in add).toBe(false);
    expect("feature:F1" in add).toBe(false);
    // others still placed
    expect("task:T2" in add).toBe(true);
    expect("feature:F2" in add).toBe(true);
  });

  it("a new feature column avoids a saved feature on its grid slot (v1 collision rule)", () => {
    const { nodes } = deriveGraph(state, noDrafts);
    // F1 dragged onto F2's grid column (col 2 = x:640)
    const layout: LayoutJSON = { v: LAYOUT_V, positions: { "feature:F1": { x: 640, y: 0 } } };
    const add = placeNew(layout, { nodes });
    expect("feature:F1" in add).toBe(false);
    // F2's grid slot is 640 but F1 occupies it → nudged right a full column
    expect(add["feature:F2"].x).toBeGreaterThanOrEqual(640 + 320);
  });

  it("a new task row avoids an already-saved sibling in the same feature (parent-relative collision)", () => {
    const { nodes } = deriveGraph(state, noDrafts);
    // T1 saved at the parent-relative slot that T2 would otherwise take (row 1 = y:164)
    const layout: LayoutJSON = { v: LAYOUT_V, positions: { "task:T1": { x: 16, y: 164 } } };
    const add = placeNew(layout, { nodes });
    expect("task:T1" in add).toBe(false);
    // T2's natural row-1 slot (y:164) is taken → pushed down a row
    expect(add["task:T2"].y).toBeGreaterThanOrEqual(164 + 120 * 0.8);
  });

  it("two successive calls (second after merging the first into layout) returns an empty object", () => {
    const { nodes } = deriveGraph(state, noDrafts);
    const first = placeNew({ v: LAYOUT_V }, { nodes });
    const layout2: LayoutJSON = { v: LAYOUT_V, positions: { ...first } };
    const second = placeNew(layout2, { nodes });
    expect(second).toEqual({});
  });

  it("an orphan layout entry (id not in graph) does not block placement and is not returned", () => {
    // Replay(?at) can leave a saved position for a node that has temporarily
    // vanished from the graph. That orphan entry must NOT occupy a grid slot
    // (an invisible node shouldn't push a visible one aside), and placeNew must
    // never emit the orphan id in its result.
    const { nodes } = deriveGraph(state, noDrafts);
    const layout: LayoutJSON = {
      v: LAYOUT_V,
      positions: {
        // orphan feature sitting on F2's natural grid column (x:640)
        "feature:GHOST": { x: 640, y: 0 },
        // orphan child sitting on T2's natural parent-relative row (y:164) of F1
        "task:GHOSTKID": { x: 16, y: 164 },
      },
    };
    const add = placeNew(layout, { nodes });
    // orphan ids are never emitted
    expect("feature:GHOST" in add).toBe(false);
    expect("task:GHOSTKID" in add).toBe(false);
    // placement is UNaffected by the orphans: F2 lands on its natural column…
    expect(add["feature:F2"]).toEqual({ x: 640, y: 0 });
    // …and T2 takes its natural row-1 slot (orphan child did not push it down)
    expect(add["task:T2"]).toEqual({ x: 16, y: 164 });
  });
});

describe("mergeNodes", () => {
  const graphOf = (drafts: Draft[] = noDrafts) => deriveGraph(state, drafts);

  it("an existing node keeps prev's position/measured/selected/dragging/width/height; data is updated", () => {
    const graph = graphOf();
    const layout: LayoutJSON = { v: LAYOUT_V, positions: placeNew({ v: LAYOUT_V }, graph) };
    const prev: Node[] = [
      {
        id: "task:T1",
        type: "task",
        position: { x: 999, y: 888 },
        data: { stale: true },
        measured: { width: 200, height: 80 },
        width: 200,
        height: 80,
        selected: true,
        dragging: true,
        parentId: "feature:F1",
      },
    ];
    const out = mergeNodes(prev, graph, layout);
    const t1 = out.find((n) => n.id === "task:T1")!;
    expect(t1.position).toEqual({ x: 999, y: 888 });
    expect(t1.measured).toEqual({ width: 200, height: 80 });
    expect(t1.width).toBe(200);
    expect(t1.height).toBe(80);
    expect(t1.selected).toBe(true);
    expect(t1.dragging).toBe(true);
    // data is replaced with the fresh graph data (the prev stale flag is gone)
    expect((t1.data.task as { id: string }).id).toBe("T1");
    expect(t1.data.stale).toBeUndefined();
  });

  it("a new node takes its position from layout (child = parent-relative) + parentId + expandParent", () => {
    const graph = graphOf();
    const layout: LayoutJSON = { v: LAYOUT_V, positions: placeNew({ v: LAYOUT_V }, graph) };
    const out = mergeNodes([], graph, layout);
    const t1 = out.find((n) => n.id === "task:T1")!;
    expect(t1.position).toEqual(layout.positions!["task:T1"]);
    expect(t1.position).toEqual({ x: 16, y: 44 });
    expect(t1.parentId).toBe("feature:F1");
    expect(t1.expandParent).toBe(true);
  });

  it("a task carries extent:'parent'; a draft does not", () => {
    const drafts: Draft[] = [{ id: "D1", specMd: "x", feature: "F1", deps: [] }];
    const graph = graphOf(drafts);
    const layout: LayoutJSON = { v: LAYOUT_V, positions: placeNew({ v: LAYOUT_V }, graph) };
    const out = mergeNodes([], graph, layout);
    const t1 = out.find((n) => n.id === "task:T1")!;
    const d1 = out.find((n) => n.id === "draft:D1")!;
    expect(t1.extent).toBe("parent");
    expect(d1.extent).toBeUndefined();
    // draft still gets parentId + expandParent (so it sits in the group)
    expect(d1.parentId).toBe("feature:F1");
    expect(d1.expandParent).toBe(true);
  });

  it("nodes that vanished from the graph are removed; feature nodes are emitted before their children", () => {
    const graph = graphOf();
    const layout: LayoutJSON = { v: LAYOUT_V, positions: placeNew({ v: LAYOUT_V }, graph) };
    const prev: Node[] = [
      { id: "task:GONE", type: "task", position: { x: 0, y: 0 }, data: {}, parentId: "feature:F1" },
    ];
    const out = mergeNodes(prev, graph, layout);
    expect(out.find((n) => n.id === "task:GONE")).toBeUndefined();
    // feature nodes all come before any non-feature node (RF parent-before-child)
    const lastFeatureIdx = out.map((n) => n.type).lastIndexOf("feature");
    const firstChildIdx = out.findIndex((n) => n.type !== "feature");
    expect(lastFeatureIdx).toBeLessThan(firstChildIdx);
  });

  it("feature style is at least the featureStyle default, and grows to bound a child dragged far away", () => {
    const graph = graphOf();
    const layout: LayoutJSON = { v: LAYOUT_V, positions: placeNew({ v: LAYOUT_V }, graph) };
    // default sizing (no prev): F1 holds 2 tasks
    const out0 = mergeNodes([], graph, layout);
    const f1def = out0.find((n) => n.id === "feature:F1")!;
    const def = featureStyle(2);
    expect((f1def.style as { width: number }).width).toBeGreaterThanOrEqual(def.width);
    expect((f1def.style as { height: number }).height).toBeGreaterThanOrEqual(def.height);

    // now drag T1 far down inside F1 (prev measured present) → container grows to bound it
    const prev: Node[] = [
      {
        id: "task:T1",
        type: "task",
        position: { x: 16, y: 2000 },
        data: {},
        parentId: "feature:F1",
        measured: { width: 200, height: 80 },
      },
    ];
    const out1 = mergeNodes(prev, graph, layout);
    const f1big = out1.find((n) => n.id === "feature:F1")!;
    expect((f1big.style as { height: number }).height).toBeGreaterThan(def.height);
    // tall enough to contain the child bottom edge (y:2000 + 80) plus padding
    expect((f1big.style as { height: number }).height).toBeGreaterThanOrEqual(2000 + 80);
  });

  it("output nodes carry no hand-written measured/handles on NEW nodes (anti-regression)", () => {
    const graph = graphOf([{ id: "D1", specMd: "x", feature: "F1", deps: [] }]);
    const layout: LayoutJSON = { v: LAYOUT_V, positions: placeNew({ v: LAYOUT_V }, graph) };
    const out = mergeNodes([], graph, layout);
    for (const n of out) {
      expect("handles" in n).toBe(false);
      // new nodes (no prev) must not seed measured
      expect(n.measured).toBeUndefined();
    }
  });

  it("an existing node's prev measured is preserved on the merge output (existing path)", () => {
    // The anti-regression test above only covers NEW nodes. This covers the
    // existing-node path: a prev node carrying RF-written `measured` must keep
    // it verbatim through mergeNodes (RF's own write-back, not re-seeded/dropped).
    const graph = graphOf();
    const layout: LayoutJSON = { v: LAYOUT_V, positions: placeNew({ v: LAYOUT_V }, graph) };
    const prev: Node[] = [
      {
        id: "task:T1",
        type: "task",
        position: { x: 16, y: 44 },
        data: { stale: true },
        parentId: "feature:F1",
        measured: { width: 123, height: 45 },
      },
    ];
    const out = mergeNodes(prev, graph, layout);
    const t1 = out.find((n) => n.id === "task:T1")!;
    expect(t1.measured).toEqual({ width: 123, height: 45 });
  });

  it("a draft targeting a non-existent feature gets parentId feature:<ghost>; placeNew is safe + idempotent", () => {
    // A draft can reference a feature that isn't in state yet. The node still
    // parents to feature:<ghost>; placeNew must not throw and must be idempotent.
    const drafts: Draft[] = [{ id: "D1", specMd: "x", feature: "ghost", deps: [] }];
    const graph = graphOf(drafts);
    const d1 = graph.nodes.find((n) => n.id === "draft:D1")!;
    expect(d1.parentId).toBe("feature:ghost");

    expect(() => placeNew({ v: LAYOUT_V }, graph)).not.toThrow();
    const first = placeNew({ v: LAYOUT_V }, graph);
    expect("draft:D1" in first).toBe(true);
    // idempotent: re-running with the first batch merged in yields nothing new
    const second = placeNew({ v: LAYOUT_V, positions: { ...first } }, graph);
    expect(second).toEqual({});
  });

  it("reference identity: unchanged data + unchanged style returns the prev node object (Object.is)", () => {
    const graph = graphOf();
    const layout: LayoutJSON = { v: LAYOUT_V, positions: placeNew({ v: LAYOUT_V }, graph) };
    // First merge produces the canonical nodes with the graph's data references.
    const first = mergeNodes([], graph, layout);
    // Second merge with the SAME graph (same data references) + same prev: every
    // node should come back as the identical object (no new allocation).
    const second = mergeNodes(first, graph, layout);
    const firstById = new Map(first.map((n) => [n.id, n]));
    for (const n of second) {
      expect(Object.is(n, firstById.get(n.id))).toBe(true);
    }
  });
});
