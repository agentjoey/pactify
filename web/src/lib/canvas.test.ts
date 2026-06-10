import { describe, it, expect } from "vitest";
import { roleColorVar, deriveFlow, type Draft, type LayoutJSON } from "./canvas";
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

  it("task node carries owner's role color, status, owner, reviewer, deps", () => {
    const { nodes } = deriveFlow(state, noLayout, noDrafts);
    const t2 = nodes.find((n) => n.id === "task:T2")!;
    expect(t2.data.status).toBe("assigned");
    expect(t2.data.owner).toBe("opencode");
    expect(t2.data.reviewer).toBe("claude-opus");
    expect(t2.data.deps).toEqual(["T1"]);
    // owner opencode is a worker → dev
    expect(t2.data.roleColor).toBe("--role-dev");
    // T3 owned by orchestrator seat → product
    const t3 = nodes.find((n) => n.id === "task:T3")!;
    expect(t3.data.roleColor).toBe("--role-product");
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
    expect(edges[0]).toEqual({ id: "dep:T1-T2", source: "task:T1", target: "task:T2", kind: "dep" });
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
    expect(edges).toContainEqual({ id: "dep:T1-D1", source: "task:T1", target: "draft:D1", kind: "dep" });
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
