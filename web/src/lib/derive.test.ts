import { describe, it, expect } from "vitest";
import {
  boardColumns, allTasks, agentActivity, lastAction, findTask, canMergeFeature, COLUMNS,
  fmtDuration, fmtTokens, fmtCost, taskRuntimeMs, taskMetrics, designBoard,
} from "./derive";
import type { State, PactEvent, Task } from "./types";

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
  it("designBoard buckets into the 5 dark-handoff columns", () => {
    const cols = designBoard(state);
    expect(cols.working.map((b) => b.task.id)).toEqual(["T1"]);
    expect(cols.review.map((b) => b.task.id)).toEqual(["T0"]); // awaiting_review → review
    expect(cols.assigned).toEqual([]);
  });
  it("designBoard moves a shipped feature's tasks wholesale to shipped", () => {
    const shipped: State = {
      ...state,
      features: [{ id: "G", branch: "b", status: "shipped", tasks: [
        { id: "S1", owner: "opencode", status: "accepted", reviewer: "claude-opus", spec: "s", evidence: "" },
      ] }],
    };
    const cols = designBoard(shipped);
    expect(cols.shipped.map((b) => b.task.id)).toEqual(["S1"]);
    expect(cols.accepted).toEqual([]);
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

describe("canMergeFeature", () => {
  const mk = (statuses: string[]): State => ({
    project: "p",
    agents: [],
    awaiting_count: 0,
    features: [{ id: "F", branch: "b", status: "in_progress", tasks: statuses.map((s, i) => ({
      id: `T${i}`, owner: "o", status: s, reviewer: "r", spec: "", evidence: "",
    })) }],
  });
  it("true only when every task is accepted", () => {
    expect(canMergeFeature(mk(["accepted", "accepted"]), "F")).toBe(true);
  });
  it("false when any task is not accepted", () => {
    expect(canMergeFeature(mk(["accepted", "in_progress"]), "F")).toBe(false);
  });
  it("false for an empty feature (nothing to merge)", () => {
    expect(canMergeFeature(mk([]), "F")).toBe(false);
  });
  it("false for an unknown feature", () => {
    expect(canMergeFeature(mk(["accepted"]), "ZZ")).toBe(false);
  });
});

const iso = (ms: number) => new Date(ms).toISOString();

const mkTask = (status: string): Task => ({
  id: "T",
  owner: "kimi-worker",
  status,
  reviewer: "claude",
  spec: "s",
  evidence: "",
});

describe("stat helpers", () => {
  describe("fmtDuration", () => {
    it("formats 3m02s", () => expect(fmtDuration(182_000)).toBe("3m02s"));
    it("formats 12s", () => expect(fmtDuration(12_000)).toBe("12s"));
    it("formats 1h04m", () => expect(fmtDuration(3_840_000)).toBe("1h04m"));
    it("formats 0s", () => expect(fmtDuration(0)).toBe("0s"));
  });

  describe("fmtTokens", () => {
    it("formats 12.4k", () => expect(fmtTokens(12_400)).toBe("12.4k"));
    it("formats 38k", () => expect(fmtTokens(38_000)).toBe("38k"));
    it("formats 940", () => expect(fmtTokens(940)).toBe("940"));
    it("formats 1k", () => expect(fmtTokens(1_000)).toBe("1k"));
  });

  describe("fmtCost", () => {
    it("formats ~$0.21", () => expect(fmtCost(0.21)).toBe("~$0.21"));
    it("formats ~$0.00", () => expect(fmtCost(0)).toBe("~$0.00"));
  });

  describe("taskRuntimeMs", () => {
    it("measures assign → now for ongoing tasks", () => {
      const events: PactEvent[] = [
        { event_id: "1", ts: iso(1_000_000), agent_id: "o", role: "orchestrator", event_type: "assign", task_id: "T", feature: "F", payload: {} },
      ];
      expect(taskRuntimeMs(mkTask("in_progress"), events, 1_012_000)).toBe(12_000);
    });

    it("measures assign → accept for accepted tasks", () => {
      const events: PactEvent[] = [
        { event_id: "1", ts: iso(1_000_000), agent_id: "o", role: "orchestrator", event_type: "assign", task_id: "T", feature: "F", payload: {} },
        { event_id: "2", ts: iso(1_015_000), agent_id: "r", role: "reviewer", event_type: "accept", task_id: "T", feature: "F", payload: {} },
      ];
      expect(taskRuntimeMs(mkTask("accepted"), events, 9_999_000)).toBe(15_000);
    });

    it("measures assign → last checkpoint while awaiting_review", () => {
      const events: PactEvent[] = [
        { event_id: "1", ts: iso(1_000_000), agent_id: "o", role: "orchestrator", event_type: "assign", task_id: "T", feature: "F", payload: {} },
        { event_id: "2", ts: iso(1_020_000), agent_id: "w", role: "worker", event_type: "checkpoint", task_id: "T", feature: "F", payload: {} },
      ];
      expect(taskRuntimeMs(mkTask("awaiting_review"), events, 9_999_000)).toBe(20_000);
    });

    it("returns 0 when no assign event exists", () => {
      expect(taskRuntimeMs(mkTask("assigned"), [], 1_000_000)).toBe(0);
    });
  });

  describe("taskMetrics", () => {
    it("renders live RUN/TOK for in_progress tasks with session metadata", () => {
      const events: PactEvent[] = [
        { event_id: "1", ts: iso(1_000_000), agent_id: "o", role: "orchestrator", event_type: "assign", task_id: "T", feature: "F", payload: { session: { tokens: 12_400, iter: 2 } } },
      ];
      const items = taskMetrics(mkTask("in_progress"), events, 1_182_000);
      expect(items).toEqual([
        { label: "RUN", value: "3m02s", live: true, est: false },
        { label: "TOK", value: "12.4k", live: true, est: false },
        { label: "", value: "×2", live: true, est: false },
      ]);
    });

    it("renders italic estimates for assigned tasks", () => {
      const events: PactEvent[] = [
        { event_id: "1", ts: iso(1_000_000), agent_id: "w", role: "worker", event_type: "assign", task_id: "T", feature: "F", payload: { session: { duration_ms: 120_000, tokens: 6_000 } } },
      ];
      const items = taskMetrics(mkTask("assigned"), events, 1_000_000);
      expect(items).toEqual([
        { label: "RUN", value: "~2m00s", live: false, est: true },
        { label: "TOK", value: "~6k", live: false, est: true },
        { label: "", value: "×1", live: false, est: true },
      ]);
    });

    it("falls back to — and checkpoint count when session metadata is absent", () => {
      const events: PactEvent[] = [
        { event_id: "0", ts: iso(990_000), agent_id: "o", role: "orchestrator", event_type: "assign", task_id: "T", feature: "F", payload: {} },
        { event_id: "1", ts: iso(1_000_000), agent_id: "w", role: "worker", event_type: "checkpoint", task_id: "T", feature: "F", payload: {} },
        { event_id: "2", ts: iso(1_020_000), agent_id: "w", role: "worker", event_type: "checkpoint", task_id: "T", feature: "F", payload: {} },
      ];
      const items = taskMetrics(mkTask("awaiting_review"), events, 1_030_000);
      expect(items).toEqual([
        { label: "RUN", value: "30s", live: false, est: false },
        { label: "TOK", value: "—", live: false, est: false },
        { label: "", value: "×2", live: false, est: false },
      ]);
    });
  });
});
