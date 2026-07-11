import { describe, it, expect } from "vitest";
import type { State, PactEvent, OrchestrateStatusResponse } from "./types";

type OStat = NonNullable<OrchestrateStatusResponse["status"]>;
import type { ProjectStats } from "./api";
import {
  deriveDashboardKPIs,
  deriveFeatureLanes,
  deriveSeatRoster,
  deriveActivityFeed,
  deriveRunProgress,
  deriveRunStats,
} from "./dashboard";

const iso = (secAgo: number) => new Date(Date.now() - secAgo * 1000).toISOString();

const baseState: State = {
  project: "relay-core",
  awaiting_count: 1,
  agents: [
    { id: "claude", roles: ["orchestrator", "reviewer"] },
    { id: "opencode", roles: ["worker"] },
    { id: "gemini", roles: ["worker"] },
  ],
  features: [
    {
      id: "feat-rh",
      branch: "feat/retry-harden",
      status: "in_progress",
      tasks: [
        { id: "t-cfg", owner: "opencode", status: "accepted", reviewer: "claude", spec: "", evidence: "" },
        { id: "t-impl", owner: "opencode", status: "awaiting_review", reviewer: "claude", spec: "", evidence: "FAIL TestRetryCap" },
        { id: "t-harden", owner: "opencode", status: "in_progress", reviewer: "claude", spec: "", evidence: "" },
      ],
    },
    {
      id: "feat-cache",
      branch: "feat/cache-concurrency",
      status: "in_progress",
      tasks: [
        { id: "t-cache", owner: "gemini", status: "in_progress", reviewer: "claude", spec: "", evidence: "" },
        { id: "t-guard", owner: "gemini", status: "accepted", reviewer: "claude", spec: "", evidence: "" },
        { id: "t-warm", owner: "gemini", status: "changes_requested", reviewer: "claude", spec: "", evidence: "" },
      ],
    },
  ],
};

const events: PactEvent[] = [
  { event_id: "j1", ts: iso(600), agent_id: "claude", role: "orchestrator", event_type: "join", task_id: "", feature: "", payload: {} },
  { event_id: "j2", ts: iso(590), agent_id: "opencode", role: "worker", event_type: "join", task_id: "", feature: "", payload: {} },
  { event_id: "j3", ts: iso(580), agent_id: "gemini", role: "worker", event_type: "join", task_id: "", feature: "", payload: {} },
  { event_id: "a1", ts: iso(500), agent_id: "claude", role: "orchestrator", event_type: "assign", task_id: "t-cfg", feature: "feat-rh", payload: { owner: "opencode", reviewer: "claude" } },
  { event_id: "c1", ts: iso(480), agent_id: "opencode", role: "worker", event_type: "checkpoint", task_id: "t-cfg", feature: "feat-rh", payload: {} },
  { event_id: "ac1", ts: iso(470), agent_id: "claude", role: "reviewer", event_type: "accept", task_id: "t-cfg", feature: "feat-rh", payload: {} },
  { event_id: "a2", ts: iso(450), agent_id: "claude", role: "orchestrator", event_type: "assign", task_id: "t-impl", feature: "feat-rh", payload: { owner: "opencode", reviewer: "claude" } },
  { event_id: "c2", ts: iso(120), agent_id: "opencode", role: "worker", event_type: "checkpoint", task_id: "t-impl", feature: "feat-rh", payload: {} },
  { event_id: "a3", ts: iso(300), agent_id: "claude", role: "orchestrator", event_type: "assign", task_id: "t-harden", feature: "feat-rh", payload: { owner: "opencode", reviewer: "claude" } },
  { event_id: "a4", ts: iso(400), agent_id: "claude", role: "orchestrator", event_type: "assign", task_id: "t-cache", feature: "feat-cache", payload: { owner: "gemini", reviewer: "claude" } },
  { event_id: "a5", ts: iso(380), agent_id: "claude", role: "orchestrator", event_type: "assign", task_id: "t-guard", feature: "feat-cache", payload: { owner: "gemini", reviewer: "claude" } },
  { event_id: "c3", ts: iso(360), agent_id: "gemini", role: "worker", event_type: "checkpoint", task_id: "t-guard", feature: "feat-cache", payload: {} },
  { event_id: "ac2", ts: iso(350), agent_id: "claude", role: "reviewer", event_type: "accept", task_id: "t-guard", feature: "feat-cache", payload: {} },
  { event_id: "a6", ts: iso(340), agent_id: "claude", role: "orchestrator", event_type: "assign", task_id: "t-warm", feature: "feat-cache", payload: { owner: "gemini", reviewer: "claude" } },
  { event_id: "ch1", ts: iso(800), agent_id: "claude", role: "reviewer", event_type: "changes_requested", task_id: "t-warm", feature: "feat-cache", payload: {} },
  { event_id: "m1", ts: iso(1300), agent_id: "claude", role: "orchestrator", event_type: "merge", task_id: "", feature: "feat-init", payload: {} },
];

const stats: ProjectStats = {
  tasks: [
    { task_id: "t-cfg", feature: "feat-rh", owner: "opencode", reviewer: "claude", status: "accepted", duration_sec: 120, added: 0, deleted: 0, tokens: 7100 },
    { task_id: "t-impl", feature: "feat-rh", owner: "opencode", reviewer: "claude", status: "awaiting_review", duration_sec: 300, added: 0, deleted: 0, tokens: 9200 },
    { task_id: "t-harden", feature: "feat-rh", owner: "opencode", reviewer: "claude", status: "in_progress", duration_sec: 180, added: 0, deleted: 0, tokens: 12400 },
    { task_id: "t-cache", feature: "feat-cache", owner: "gemini", reviewer: "claude", status: "in_progress", duration_sec: 200, added: 0, deleted: 0, tokens: 5800 },
    { task_id: "t-guard", feature: "feat-cache", owner: "gemini", reviewer: "claude", status: "accepted", duration_sec: 240, added: 0, deleted: 0, tokens: 8600 },
    { task_id: "t-warm", feature: "feat-cache", owner: "gemini", reviewer: "claude", status: "changes_requested", duration_sec: 0, added: 0, deleted: 0, tokens: 0 },
  ],
  agents: [
    { seat: "claude", tasks: 6, duration_sec: 1000, added: 0, deleted: 0, tokens: 43100, accepted: 2, reworked: 1 },
    { seat: "opencode", tasks: 3, duration_sec: 600, added: 0, deleted: 0, tokens: 28700, accepted: 1, reworked: 0 },
    { seat: "gemini", tasks: 3, duration_sec: 440, added: 0, deleted: 0, tokens: 14400, accepted: 1, reworked: 1 },
  ],
};

describe("deriveDashboardKPIs", () => {
  const now = Date.now();

  it("reports active run as 1 when orchestrate status is running", () => {
    const kpis = deriveDashboardKPIs(baseState, stats, { present: true, status: { done: false, escalated: false, total: 6, accepted: 2 } as unknown as OStat }, events, now);
    expect(kpis.activeRun).toEqual({ count: 1, label: "orchestrating", live: true });
  });

  it("reports active run as 0/idle when no run is present", () => {
    const kpis = deriveDashboardKPIs(baseState, stats, { present: false }, events, now);
    expect(kpis.activeRun).toEqual({ count: 0, label: "idle", live: false });
  });

  it("uses state awaiting_count for awaiting review", () => {
    const kpis = deriveDashboardKPIs(baseState, stats, { present: false }, events, now);
    expect(kpis.awaitingReview).toEqual({ count: 1, label: "human decision" });
  });

  it("sums token stats for tokens today", () => {
    const kpis = deriveDashboardKPIs(baseState, stats, { present: false }, events, now);
    expect(kpis.tokensToday.tokens).toBe("43.1k");
    expect(kpis.tokensToday.cost).toBe("~$0.17");
  });

  it("counts merge events within 7 days as shipped", () => {
    const kpis = deriveDashboardKPIs(baseState, stats, { present: false }, events, now);
    expect(kpis.shipped7d).toEqual({ count: 1, label: "to local main" });
  });

  it("ignores merge events older than 7 days", () => {
    const oldMerge: PactEvent = { event_id: "m2", ts: iso(8 * 24 * 60 * 60), agent_id: "claude", role: "orchestrator", event_type: "merge", task_id: "", feature: "old", payload: {} };
    const kpis = deriveDashboardKPIs(baseState, stats, { present: false }, [...events, oldMerge], now);
    expect(kpis.shipped7d.count).toBe(1);
  });
});

describe("deriveFeatureLanes", () => {
  const now = Date.now();

  it("returns one lane per non-shipped feature with tasks", () => {
    const lanes = deriveFeatureLanes(baseState, stats, events, now);
    expect(lanes.map((l) => l.feature.id)).toEqual(["feat-rh", "feat-cache"]);
  });

  it("skips shipped features", () => {
    const withShipped: State = {
      ...baseState,
      features: [...baseState.features, { id: "feat-shipped", branch: "main", status: "shipped", tasks: [{ id: "t-ship", owner: "opencode", status: "accepted", reviewer: "claude", spec: "", evidence: "" }] }],
    };
    const lanes = deriveFeatureLanes(withShipped, stats, events, now);
    expect(lanes.some((l) => l.feature.id === "feat-shipped")).toBe(false);
  });

  it("identifies the first awaiting_review task as reviewTask", () => {
    const lanes = deriveFeatureLanes(baseState, stats, events, now);
    const rh = lanes.find((l) => l.feature.id === "feat-rh");
    expect(rh?.reviewTask?.id).toBe("t-impl");
  });

  it("computes progress done/total and sums tokens", () => {
    const lanes = deriveFeatureLanes(baseState, stats, events, now);
    const rh = lanes.find((l) => l.feature.id === "feat-rh");
    expect(rh?.progress).toEqual({ done: 1, total: 3 });
    expect(rh?.tokens).toBe(28700);
  });
});

describe("deriveSeatRoster", () => {
  it("marks opencode working on its in_progress task and claude as active reviewer", () => {
    const roster = deriveSeatRoster(baseState, events, stats);
    const opencode = roster.find((r) => r.seat.id === "opencode");
    const claude = roster.find((r) => r.seat.id === "claude");
    expect(opencode?.status).toBe("working");
    expect(opencode?.currentTask).toBe("t-harden");
    expect(claude?.status).toBe("active");
    expect(claude?.currentTask).toBe("t-impl");
  });

  it("marks idle seats and surfaces stat totals", () => {
    const state: State = { ...baseState, agents: baseState.agents.filter((a) => a.id === "gemini") };
    const roster = deriveSeatRoster(state, events, stats);
    const gemini = roster.find((r) => r.seat.id === "gemini");
    expect(gemini?.status).toBe("working");
    expect(gemini?.tokens).toBe(14400);
  });
});

describe("deriveActivityFeed", () => {
  const now = Date.now();

  it("includes awaiting, started, accepted, changes and shipped items", () => {
    const { items } = deriveActivityFeed(events, baseState, now, 0);
    const kinds = items.map((i) => i.kind);
    expect(kinds).toContain("awaiting");
    expect(kinds).toContain("started");
    expect(kinds).toContain("accepted");
    expect(kinds).toContain("changes");
    expect(kinds).toContain("shipped");
  });

  it("sorts newest first", () => {
    const { items } = deriveActivityFeed(events, baseState, now, 0);
    for (let i = 1; i < items.length; i++) {
      expect(items[i - 1].ts).toBeGreaterThanOrEqual(items[i].ts);
    }
  });

  it("counts items newer than lastSeen as new", () => {
    const lastSeen = Date.now() - 1000 * 1000; // older than all fixture events
    const { newCount, items } = deriveActivityFeed(events, baseState, now, lastSeen);
    expect(newCount).toBe(items.filter((i) => i.ts > lastSeen).length);
    expect(newCount).toBeGreaterThan(0);
  });
});

describe("deriveRunProgress", () => {
  it("uses orchestrate status total/accepted when present", () => {
    expect(deriveRunProgress(baseState, { present: true, status: { total: 6, accepted: 3 } as unknown as OStat })).toBe(0.5);
  });

  it("falls back to accepted + shipped tasks over total tasks", () => {
    expect(deriveRunProgress(baseState, { present: false })).toBe(2 / 6);
  });

  it("returns 0 for empty state", () => {
    expect(deriveRunProgress({ project: "p", agents: [], features: [], awaiting_count: 0 })).toBe(0);
  });
});

describe("deriveRunStats", () => {
  const now = Date.now();

  it("builds the run stats line from stats and orchestrate status", () => {
    const s = deriveRunStats(baseState, stats, { present: true, status: { iter: 7, total: 6, accepted: 3 } as unknown as OStat }, events, now);
    expect(s.features).toBe(2);
    expect(s.concurrency).toBe(2);
    expect(s.iter).toBe(7);
    expect(s.tokens).toBe(43100);
    expect(s.elapsedMs).toBeGreaterThan(0);
    expect(s.cost).toBe("~$0.17");
  });
});
