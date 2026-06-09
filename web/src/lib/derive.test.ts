import { describe, it, expect } from "vitest";
import { boardColumns, allTasks, agentActivity, lastAction, findTask, COLUMNS } from "./derive";
import type { State, PactEvent } from "./types";

const state: State = {
  project: "p",
  agents: [{ id: "claude-opus", roles: ["orchestrator", "reviewer"] }, { id: "opencode", roles: ["worker"] }],
  features: [{ id: "F", branch: "feat/x", status: "in_progress", tasks: [
    { id: "T1", owner: "opencode", status: "in_progress", reviewer: "claude-opus", spec: "s", evidence: "" },
    { id: "T0", owner: "opencode", status: "awaiting_review", reviewer: "claude-opus", spec: "s", evidence: "PASS ok" },
  ] }],
  awaiting_count: 1,
};

const events: PactEvent[] = [
  { event_id: "1", ts: "t1", agent_id: "claude-opus", role: "orchestrator", event_type: "assign", task_id: "T1", feature: "F", payload: {} },
  { event_id: "2", ts: "t2", agent_id: "opencode", role: "worker", event_type: "checkpoint", task_id: "T0", feature: "F", payload: { evidence: "PASS ok" } },
];

describe("derive", () => {
  it("boardColumns groups tasks by status across features", () => {
    const cols = boardColumns(state);
    expect(cols["in_progress"].map((b) => b.task.id)).toEqual(["T1"]);
    expect(cols["awaiting_review"].map((b) => b.task.id)).toEqual(["T0"]);
    expect(COLUMNS).toContain("assigned");
  });
  it("allTasks flattens with feature id", () => {
    expect(allTasks(state).map((b) => b.task.id).sort()).toEqual(["T0", "T1"]);
  });
  it("agentActivity returns that agent's events newest-first", () => {
    const a = agentActivity("opencode", events);
    expect(a.map((e) => e.event_id)).toEqual(["2"]);
  });
  it("lastAction returns the most recent event for an agent", () => {
    expect(lastAction("claude-opus", events)?.event_type).toBe("assign");
    expect(lastAction("nobody", events)).toBeUndefined();
  });
  it("findTask locates a task + its feature", () => {
    expect(findTask(state, "T0")?.feature).toBe("F");
    expect(findTask(state, "nope")).toBeUndefined();
  });
});
