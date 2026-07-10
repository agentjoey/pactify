import { describe, it, expect } from "vitest";
import { deriveFlow } from "./flowderive";
import type { PactEvent } from "./types";

const iso = (ms: number) => new Date(ms).toISOString();

const orc = "claude-opus";
const worker = "opencode";
const reviewer2 = "gemini";

function fullLifecycle(): PactEvent[] {
  const task = "T1";
  const feature = "F1";
  return [
    {
      event_id: "j1",
      ts: iso(1_000_000),
      agent_id: orc,
      role: "orchestrator",
      event_type: "join",
      task_id: "",
      feature: "",
      payload: {},
    },
    {
      event_id: "a1",
      ts: iso(1_001_000),
      agent_id: orc,
      role: "orchestrator",
      event_type: "assign",
      task_id: task,
      feature,
      payload: { owner: worker, reviewers: [orc, reviewer2] },
    },
    {
      event_id: "c1",
      ts: iso(1_010_000),
      agent_id: worker,
      role: "worker",
      event_type: "checkpoint",
      task_id: task,
      feature,
      payload: {},
    },
    {
      event_id: "ch1",
      ts: iso(1_020_000),
      agent_id: orc,
      role: "reviewer",
      event_type: "changes_requested",
      task_id: task,
      feature,
      payload: {},
    },
    {
      event_id: "c2",
      ts: iso(1_050_000),
      agent_id: worker,
      role: "worker",
      event_type: "checkpoint",
      task_id: task,
      feature,
      payload: {},
    },
    {
      event_id: "ac1",
      ts: iso(1_060_000),
      agent_id: orc,
      role: "reviewer",
      event_type: "accept",
      task_id: task,
      feature,
      payload: {},
    },
    {
      event_id: "ac2",
      ts: iso(1_070_000),
      agent_id: reviewer2,
      role: "reviewer",
      event_type: "accept",
      task_id: task,
      feature,
      payload: {},
    },
    {
      event_id: "m1",
      ts: iso(1_080_000),
      agent_id: orc,
      role: "orchestrator",
      event_type: "merge",
      task_id: task,
      feature,
      payload: {},
    },
  ];
}

function withoutX(model: ReturnType<typeof deriveFlow>) {
  const { x: _, ...rest } = model;
  return rest;
}

function compareModels(
  a: ReturnType<typeof deriveFlow>,
  b: ReturnType<typeof deriveFlow>,
) {
  expect(withoutX(a)).toEqual(withoutX(b));
  const samples = [a.tMin - 1, a.tMin, a.tMax, a.tMax + 1];
  for (const s of samples) {
    expect(a.x(s)).toBeCloseTo(b.x(s), 10);
  }
  for (const g of a.gaps) {
    expect(a.x(g.t0)).toBeCloseTo(b.x(g.t0), 10);
    expect(a.x(g.t1)).toBeCloseTo(b.x(g.t1), 10);
    expect(a.x((g.t0 + g.t1) / 2)).toBeCloseTo(
      b.x((g.t0 + g.t1) / 2),
      10,
    );
  }
}

describe("deriveFlow", () => {
  it("derives full T1 lifecycle with lanes, stints, arrows and marks", () => {
    const m = deriveFlow(fullLifecycle());

    expect(m.lanes.map((l) => l.id)).toEqual([orc, worker, reviewer2]);
    expect(m.lanes[0].firstT).toBe(1_000_000);

    expect(m.marks).toEqual([
      { agent: orc, verb: "join", t: 1_000_000 },
      { agent: orc, verb: "merge", task: "T1", t: 1_080_000 },
    ]);

    expect(m.arrows).toHaveLength(8);
    expect(
      m.arrows.filter((a) => a.verb === "checkpoint"),
    ).toHaveLength(4);
    expect(
      m.arrows.filter(
        (a) => a.verb === "checkpoint" && a.t === 1_050_000,
      ).length,
    ).toBe(2);
    expect(
      m.arrows.find((a) => a.verb === "changes"),
    ).toEqual({
      verb: "changes",
      from: orc,
      to: worker,
      task: "T1",
      t: 1_020_000,
    });

    expect(m.stints).toHaveLength(6);
    const work = m.stints.find((s) => s.kind === "work");
    expect(work).toEqual({
      agent: worker,
      task: "T1",
      kind: "work",
      t0: 1_001_000,
      t1: 1_010_000,
    });

    const reworks = m.stints.filter((s) => s.kind === "rework");
    expect(reworks).toHaveLength(1);
    expect(reworks[0]).toEqual({
      agent: worker,
      task: "T1",
      kind: "rework",
      t0: 1_020_000,
      t1: 1_050_000,
    });

    const reviews = m.stints.filter((s) => s.kind === "review");
    expect(reviews).toHaveLength(4);
    expect(reviews.every((s) => s.t1 !== null)).toBe(true);
  });

  it("returns the same result for shuffled input", () => {
    const events = fullLifecycle();
    const expected = deriveFlow(events);
    for (let i = 0; i < 5; i++) {
      const shuffled = events
        .map((e) => ({ e, sort: Math.random() }))
        .sort((a, b) => a.sort - b.sort)
        .map(({ e }) => e);
      compareModels(expected, deriveFlow(shuffled));
    }
  });

  it("compresses gaps > gapMinMs and keeps x monotonic", () => {
    const events: PactEvent[] = [
      {
        event_id: "e1",
        ts: iso(1_000_000),
        agent_id: orc,
        role: "orchestrator",
        event_type: "join",
        task_id: "",
        feature: "",
        payload: {},
      },
      {
        event_id: "e2",
        ts: iso(1_000_000 + 2 * 60 * 60_000),
        agent_id: worker,
        role: "worker",
        event_type: "join",
        task_id: "",
        feature: "",
        payload: {},
      },
      {
        event_id: "e3",
        ts: iso(1_000_000 + 2 * 60 * 60_000 + 60_000),
        agent_id: worker,
        role: "worker",
        event_type: "join",
        task_id: "",
        feature: "",
        payload: {},
      },
    ];
    const m = deriveFlow(events);
    expect(m.gaps).toHaveLength(1);
    expect(m.gaps[0]).toEqual({
      t0: 1_000_000,
      t1: 1_000_000 + 2 * 60 * 60_000,
    });
    expect(m.x(m.tMin)).toBe(0);
    expect(m.x(m.tMax)).toBe(1);
    const insideGap = m.gaps[0].t0 + 60_000;
    expect(m.x(insideGap)).toBeGreaterThan(m.x(m.gaps[0].t0));
    expect(m.x(insideGap)).toBeLessThan(m.x(m.gaps[0].t1));
    // Width at the interior of the gap is exactly the compressed gap width.
    expect(
      m.x(m.gaps[0].t1 - 1) - m.x(m.gaps[0].t0),
    ).toBeCloseTo(0.02, 6);
  });

  it("handles invalid ts and missing owner without throwing", () => {
    const events: PactEvent[] = [
      {
        event_id: "bad-ts",
        ts: "not-a-date",
        agent_id: worker,
        role: "worker",
        event_type: "join",
        task_id: "",
        feature: "",
        payload: {},
      },
      {
        event_id: "bad-owner",
        ts: iso(1_000_000),
        agent_id: orc,
        role: "orchestrator",
        event_type: "assign",
        task_id: "T",
        feature: "F",
        payload: {}, // owner missing
      },
      {
        event_id: "good",
        ts: iso(1_001_000),
        agent_id: worker,
        role: "worker",
        event_type: "join",
        task_id: "",
        feature: "",
        payload: {},
      },
    ];
    expect(() => deriveFlow(events)).not.toThrow();
    const m = deriveFlow(events);
    expect(m.lanes.map((l) => l.id)).toEqual([orc, worker]);
    expect(m.arrows).toHaveLength(0);
    expect(m.stints).toHaveLength(0);
  });

  it("leaves unfinished stints with t1 === null", () => {
    const events: PactEvent[] = [
      {
        event_id: "a1",
        ts: iso(1_000_000),
        agent_id: orc,
        role: "orchestrator",
        event_type: "assign",
        task_id: "T",
        feature: "F",
        payload: { owner: worker, reviewer: orc },
      },
      {
        event_id: "c1",
        ts: iso(1_010_000),
        agent_id: worker,
        role: "worker",
        event_type: "checkpoint",
        task_id: "T",
        feature: "F",
        payload: {},
      },
      {
        event_id: "ch1",
        ts: iso(1_020_000),
        agent_id: orc,
        role: "reviewer",
        event_type: "changes_requested",
        task_id: "T",
        feature: "F",
        payload: {},
      },
    ];
    const m = deriveFlow(events);
    const rework = m.stints.find((s) => s.kind === "rework");
    expect(rework).toBeDefined();
    expect(rework!.t1).toBeNull();
  });

  it("returns an empty model with x === 0 for empty input", () => {
    const m = deriveFlow([]);
    expect(m.lanes).toEqual([]);
    expect(m.stints).toEqual([]);
    expect(m.arrows).toEqual([]);
    expect(m.marks).toEqual([]);
    expect(m.gaps).toEqual([]);
    expect(m.x(0)).toBe(0);
    expect(m.x(999_999)).toBe(0);
  });
});
