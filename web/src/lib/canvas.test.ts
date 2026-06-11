import { describe, it, expect } from "vitest";
import {
  roleColorVar,
  deriveFlow,
  toParentRelative,
  childToAbsolute,
  applyConnect,
  nextId,
  type Draft,
  type DraftFeature,
  type LayoutJSON,
} from "./canvas";
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

const noLayout: LayoutJSON = {};
const noDrafts: Draft[] = [];

describe("deriveFlow", () => {
  it("emits feature, task and seat nodes with correct ids and parentIds", () => {
    const { nodes } = deriveFlow(state, noLayout, noDrafts);
    const ids = nodes.map((n) => n.id).sort();
    expect(ids).toEqual(
      ["feature:F1", "feature:F2", "seat:claude-opus", "seat:opencode", "task:T1", "task:T2", "task:T3"].sort(),
    );
    const byId = new Map(nodes.map((n) => [n.id, n]));
    expect(byId.get("task:T1")!.parentId).toBe("feature:F1");
    expect(byId.get("task:T2")!.parentId).toBe("feature:F1");
    expect(byId.get("task:T3")!.parentId).toBe("feature:F2");
    // feature/seat nodes have no parent
    expect(byId.get("feature:F1")!.parentId).toBeUndefined();
    expect(byId.get("seat:opencode")!.parentId).toBeUndefined();
  });

  it("task node carries the Task object, feature, resolved roles and color", () => {
    const { nodes } = deriveFlow(state, noLayout, noDrafts);
    const t2 = nodes.find((n) => n.id === "task:T2")!;
    // The node data carries the protocol Task object itself (no flattening).
    const task = t2.data.task as State["features"][number]["tasks"][number];
    expect(task.id).toBe("T2");
    expect(task.status).toBe("assigned");
    expect(task.owner).toBe("opencode");
    expect(task.reviewer).toBe("claude-opus");
    expect(task.deps).toEqual(["T1"]);
    expect(t2.data.feature).toBe("F1");
    // Pre-resolved roles: owner opencode is a worker, reviewer claude-opus is
    // orchestrator+reviewer.
    expect(t2.data.ownerRoles).toEqual(["worker"]);
    expect(t2.data.reviewerRoles).toEqual(["orchestrator", "reviewer"]);
    // owner opencode is a worker → dev
    expect(t2.data.roleColor).toBe("--role-dev");
    // T3 owned by orchestrator seat → product
    const t3 = nodes.find((n) => n.id === "task:T3")!;
    expect(t3.data.roleColor).toBe("--role-product");
  });

  it("feature node carries accepted/total progress (T7)", () => {
    const { nodes } = deriveFlow(state, noLayout, noDrafts);
    // F1: T1 in_progress, T2 assigned → 0 of 2 accepted.
    const f1 = nodes.find((n) => n.id === "feature:F1")!;
    expect(f1.data.total).toBe(2);
    expect(f1.data.accepted).toBe(0);
    // F2: T3 assigned → 0 of 1.
    const f2 = nodes.find((n) => n.id === "feature:F2")!;
    expect(f2.data.total).toBe(1);
    expect(f2.data.accepted).toBe(0);
  });

  it("feature progress counts accepted tasks; all-accepted reads total===accepted", () => {
    const allDone: State = {
      ...state,
      features: [
        {
          id: "F1",
          branch: "feat/x",
          status: "done",
          tasks: [
            { id: "T1", owner: "opencode", status: "accepted", reviewer: "claude-opus", spec: "", evidence: "" },
            { id: "T2", owner: "opencode", status: "accepted", reviewer: "claude-opus", spec: "", evidence: "" },
          ],
        },
      ],
    };
    const f1 = deriveFlow(allDone, noLayout, noDrafts).nodes.find((n) => n.id === "feature:F1")!;
    expect(f1.data.total).toBe(2);
    expect(f1.data.accepted).toBe(2);
  });

  it("an empty feature reports 0/0 (no division), draft feature too", () => {
    const empty: State = {
      ...state,
      features: [{ id: "F1", branch: "b", status: "active", tasks: [] }],
    };
    const f1 = deriveFlow(empty, noLayout, noDrafts, [{ id: "F9", branch: "feat/n" }]).nodes;
    const committed = f1.find((n) => n.id === "feature:F1")!;
    expect(committed.data.total).toBe(0);
    expect(committed.data.accepted).toBe(0);
    const draftFeat = f1.find((n) => n.id === "feature:F9")!;
    expect(draftFeat.data.total).toBe(0);
    expect(draftFeat.data.accepted).toBe(0);
  });

  it("seat node carries roles + roleColor and pins to the left rail (x:0)", () => {
    const { nodes } = deriveFlow(state, noLayout, noDrafts);
    const s = nodes.find((n) => n.id === "seat:claude-opus")!;
    expect(s.position.x).toBe(0);
    expect(s.data.roleColor).toBe("--role-product");
  });

  it("emits a dep edge with source=dependency, target=dependent", () => {
    const { edges } = deriveFlow(state, noLayout, noDrafts);
    expect(edges).toHaveLength(1);
    expect(edges[0]).toEqual({ id: "dep:T1→T2", source: "task:T1", target: "task:T2", kind: "dep" });
  });

  it("layout.positions overrides the deterministic grid", () => {
    const layout: LayoutJSON = { positions: { "task:T1": { x: 999, y: 888 } } };
    const { nodes } = deriveFlow(state, layout, noDrafts);
    const t1 = nodes.find((n) => n.id === "task:T1")!;
    expect(t1.position).toEqual({ x: 999, y: 888 });
    // an un-overridden node still uses the grid (deterministic, not 999/888)
    const t2 = nodes.find((n) => n.id === "task:T2")!;
    expect(t2.position).not.toEqual({ x: 999, y: 888 });
  });

  it("drafts produce a draft node (parented to feature) and a dep edge", () => {
    const drafts: Draft[] = [{ id: "D1", specMd: "# draft", feature: "F1", deps: ["T1"] }];
    const { nodes, edges } = deriveFlow(state, noLayout, drafts);
    const d = nodes.find((n) => n.id === "draft:D1")!;
    expect(d.type).toBe("draft");
    expect(d.parentId).toBe("feature:F1");
    expect(d.data.draft).toBe(true);
    expect(d.data.specMd).toBe("# draft");
    expect(d.data.deps).toEqual(["T1"]);
    expect(edges).toContainEqual({ id: "dep:T1→D1", source: "task:T1", target: "draft:D1", kind: "dep" });
  });

  it("a draft→draft dep edge sources from the draft node, not a task node", () => {
    const drafts: Draft[] = [
      { id: "A", specMd: "x", feature: "F1", deps: [] },
      { id: "B", specMd: "y", feature: "F1", deps: ["A"] },
    ];
    const { edges } = deriveFlow(state, noLayout, drafts);
    expect(edges).toContainEqual({ id: "dep:A→B", source: "draft:A", target: "draft:B", kind: "dep" });
  });

  it("empty state yields only seat nodes — none when there are no agents", () => {
    const empty: State = { project: "p", agents: [], features: [], awaiting_count: 0 };
    const { nodes, edges } = deriveFlow(empty, noLayout, noDrafts);
    expect(nodes).toEqual([]);
    expect(edges).toEqual([]);
    const seatsOnly: State = { ...empty, agents: state.agents };
    const r = deriveFlow(seatsOnly, noLayout, noDrafts);
    expect(r.nodes.map((n) => n.id).sort()).toEqual(["seat:claude-opus", "seat:opencode"]);
  });

  it("is deterministic: two runs serialize identically", () => {
    const drafts: Draft[] = [{ id: "D1", specMd: "x", feature: "F1", deps: ["T1"] }];
    const a = JSON.stringify(deriveFlow(state, noLayout, drafts));
    const b = JSON.stringify(deriveFlow(state, noLayout, drafts));
    expect(a).toBe(b);
  });
});

describe("deriveFlow — build mode (drafts + draft features)", () => {
  it("renders a draft feature group one column past committed features", () => {
    const dfs: DraftFeature[] = [{ id: "F9", branch: "feat/new" }];
    const { nodes } = deriveFlow(state, noLayout, noDrafts, dfs);
    const f9 = nodes.find((n) => n.id === "feature:F9");
    expect(f9).toBeDefined();
    expect(f9!.type).toBe("feature");
    expect(f9!.data.draft).toBe(true);
    expect(f9!.data.branch).toBe("feat/new");
    // two committed features (F1,F2) → draft feature sits in column 3.
    expect(f9!.position.x).toBe(3 * 320);
  });

  it("a draft feature id that clashes with a committed feature is skipped", () => {
    const dfs: DraftFeature[] = [{ id: "F1", branch: "dup" }];
    const { nodes } = deriveFlow(state, noLayout, noDrafts, dfs);
    // only ONE feature:F1 (the committed one, branch feat/x).
    const f1s = nodes.filter((n) => n.id === "feature:F1");
    expect(f1s).toHaveLength(1);
    expect(f1s[0].data.branch).toBe("feat/x");
  });

  it("a draft can target a draft feature and parents to it", () => {
    const dfs: DraftFeature[] = [{ id: "F9", branch: "feat/new" }];
    const drafts: Draft[] = [{ id: "D1", specMd: "x", feature: "F9", deps: [] }];
    const { nodes } = deriveFlow(state, noLayout, drafts, dfs);
    const d = nodes.find((n) => n.id === "draft:D1")!;
    expect(d.parentId).toBe("feature:F9");
  });

  it("add → edit → remove a draft round-trips through deriveFlow", () => {
    // add
    let drafts: Draft[] = [{ id: "D1", specMd: "# v1", feature: "F1", deps: [] }];
    let r = deriveFlow(state, noLayout, drafts);
    expect(r.nodes.find((n) => n.id === "draft:D1")!.data.specMd).toBe("# v1");
    // edit (spec change)
    drafts = drafts.map((d) => (d.id === "D1" ? { ...d, specMd: "# v2" } : d));
    r = deriveFlow(state, noLayout, drafts);
    expect(r.nodes.find((n) => n.id === "draft:D1")!.data.specMd).toBe("# v2");
    // remove
    drafts = drafts.filter((d) => d.id !== "D1");
    r = deriveFlow(state, noLayout, drafts);
    expect(r.nodes.find((n) => n.id === "draft:D1")).toBeUndefined();
  });
});

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

describe("toParentRelative", () => {
  it("emits children whose feature-absolute + relative position equals derive's absolute", () => {
    const { nodes } = deriveFlow(state, noLayout, noDrafts);
    const abs = new Map(nodes.map((n) => [n.id, n.position]));
    const rel = toParentRelative(nodes);
    const relById = new Map(rel.map((n) => [n.id, n]));
    for (const n of nodes) {
      if (!n.parentId) {
        // top-level nodes pass through unchanged
        expect(relById.get(n.id)!.position).toEqual(n.position);
        continue;
      }
      const featAbs = abs.get(n.parentId)!;
      const r = relById.get(n.id)!.position;
      expect({ x: featAbs.x + r.x, y: featAbs.y + r.y }).toEqual(abs.get(n.id));
    }
  });

  it("does not mutate its input", () => {
    const { nodes } = deriveFlow(state, noLayout, noDrafts);
    const before = JSON.stringify(nodes);
    toParentRelative(nodes);
    expect(JSON.stringify(nodes)).toBe(before);
  });
});

// Regression for the drag-persistence bug: a child (task/draft) reports a
// parent-relative position in onNodeDragStop, which must be converted to
// ABSOLUTE before being written into layout.positions. On the next derive the
// saved absolute value must round-trip back to the same absolute coordinate so
// the node stays exactly where it was dropped (no snap to a wrong spot).
describe("child drag round-trip (single coordinate system)", () => {
  it("dragging task:T1 by +100/+50 persists absolute, re-derives to the same spot", () => {
    // 1) derive a fixture, take T1's absolute position and its feature's absolute.
    const { nodes } = deriveFlow(state, noLayout, noDrafts);
    const t1Abs = nodes.find((n) => n.id === "task:T1")!.position;
    const featAbs = nodes.find((n) => n.id === "feature:F1")!.position;

    // 2) toParentRelative gives the parent-relative coords React Flow renders.
    const rel = toParentRelative(nodes);
    const t1Rel = rel.find((n) => n.id === "task:T1")!.position;
    expect({ x: featAbs.x + t1Rel.x, y: featAbs.y + t1Rel.y }).toEqual(t1Abs);

    // 3) simulate a drag of the relative node by +100/+50 (what RF reports).
    const draggedRel = { x: t1Rel.x + 100, y: t1Rel.y + 50 };
    // The expected absolute drop target.
    const expectedAbs = { x: t1Abs.x + 100, y: t1Abs.y + 50 };

    // 4) onNodeDragStop conversion: child-relative → absolute via parent abs.
    const savedAbs = childToAbsolute(draggedRel, featAbs);
    expect(savedAbs).toEqual(expectedAbs);

    // 5) write ABSOLUTE into layout.positions and re-derive.
    const layout: LayoutJSON = { positions: { "task:T1": savedAbs } };
    const re = deriveFlow(state, layout, noDrafts);
    const reFeatAbs = re.nodes.find((n) => n.id === "feature:F1")!.position;
    const reRel = toParentRelative(re.nodes);
    const reT1Rel = reRel.find((n) => n.id === "task:T1")!.position;

    // 6) feature-absolute + relative === the dragged absolute, exactly.
    expect({ x: reFeatAbs.x + reT1Rel.x, y: reFeatAbs.y + reT1Rel.y }).toEqual(expectedAbs);
    // and the derived absolute itself equals the dragged absolute.
    expect(re.nodes.find((n) => n.id === "task:T1")!.position).toEqual(expectedAbs);
  });

  it("dragging a child after its parent moved still round-trips (parent abs is live)", () => {
    // Feature F1 has been dragged to a new absolute spot; T1 dragged relative.
    const layout: LayoutJSON = { positions: { "feature:F1": { x: 500, y: 200 } } };
    const { nodes } = deriveFlow(state, layout, noDrafts);
    const featAbs = nodes.find((n) => n.id === "feature:F1")!.position;
    expect(featAbs).toEqual({ x: 500, y: 200 });

    const rel = toParentRelative(nodes);
    const t1Rel = rel.find((n) => n.id === "task:T1")!.position;
    const draggedRel = { x: t1Rel.x + 30, y: t1Rel.y + 70 };

    // Convert with the parent's CURRENT (moved) absolute position.
    const savedAbs = childToAbsolute(draggedRel, featAbs);

    const re = deriveFlow(
      state,
      { positions: { "feature:F1": { x: 500, y: 200 }, "task:T1": savedAbs } },
      noDrafts,
    );
    const reRel = toParentRelative(re.nodes);
    const reFeatAbs = re.nodes.find((n) => n.id === "feature:F1")!.position;
    const reT1Rel = reRel.find((n) => n.id === "task:T1")!.position;
    expect({ x: reFeatAbs.x + reT1Rel.x, y: reFeatAbs.y + reT1Rel.y }).toEqual(savedAbs);
  });
});


describe("grid fallback collision avoidance", () => {
  it("a new feature is nudged off a column the user moved another feature onto", () => {
    // F2 has no saved position; its grid slot is column 2 — but the user
    // dragged F1 there. F2 must shift right instead of stacking.
    const st = twoFeatureState();
    const layout = { positions: { "feature:F1": { x: 640, y: 0 } } }; // F1 sits on F2's grid slot (col 2)
    const { nodes } = deriveFlow(st, layout, []);
    const f1 = nodes.find((n) => n.id === "feature:F1")!;
    const f2 = nodes.find((n) => n.id === "feature:F2")!;
    expect(f1.position).toEqual({ x: 640, y: 0 });
    expect(Math.abs(f2.position.x - f1.position.x)).toBeGreaterThanOrEqual(320); // a full column away
  });

  it("a draft is nudged below a task the user moved onto its grid row", () => {
    const st = oneFeatureOneTaskState();
    // The draft's grid row would be row 1 (below T1) — move T1 onto row 1 so the
    // draft must drop further down.
    const layout = { positions: { "task:T1": { x: 336, y: 148 } } }; // exactly the draft's fallback slot
    const { nodes } = deriveFlow(st, layout, [{ id: "D1", specMd: "x", feature: "F1", deps: [] }]);
    const t1 = nodes.find((n) => n.id === "task:T1")!;
    const d1 = nodes.find((n) => n.id === "draft:D1")!;
    expect(Math.abs(d1.position.y - t1.position.y)).toBeGreaterThanOrEqual(96); // pushed a row down
  });
});

function twoFeatureState(): State {
  return {
    project: "p",
    agents: [{ id: "a", roles: ["worker"] }],
    features: [
      { id: "F1", branch: "b1", status: "in_progress", tasks: [] },
      { id: "F2", branch: "b2", status: "in_progress", tasks: [] },
    ],
    awaiting_count: 0,
  };
}

function oneFeatureOneTaskState(): State {
  return {
    project: "p",
    agents: [{ id: "a", roles: ["worker"] }],
    features: [
      {
        id: "F1", branch: "b1", status: "in_progress",
        tasks: [{ id: "T1", owner: "a", status: "assigned", reviewer: "r", spec: "", evidence: "" }],
      },
    ],
    awaiting_count: 0,
  };
}

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
