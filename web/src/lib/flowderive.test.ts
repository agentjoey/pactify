import { describe, it, expect } from "vitest";
import { deriveFlow, liveStates, tAt, type FlowModel } from "./flowderive";
import type { PactEvent } from "./types";

function makeModel(overrides: Partial<FlowModel> = {}): FlowModel {
  return {
    lanes: [],
    stints: [],
    arrows: [],
    marks: [],
    gaps: [],
    tMin: 0,
    tMax: 0,
    x: () => 0,
    ...overrides,
  };
}

function joinEvent(ts: number, agent = "alice", id = "e"): PactEvent {
  return {
    event_id: id,
    ts: new Date(ts).toISOString(),
    agent_id: agent,
    role: "worker",
    event_type: "join",
    task_id: "",
    feature: "",
    payload: {},
  };
}

describe("liveStates", () => {
  it("returns work for an open work stint", () => {
    const model = makeModel({
      lanes: [{ id: "bob", firstT: 0 }],
      stints: [
        { agent: "bob", task: "T1", kind: "work", t0: 0, t1: null },
      ],
    });
    expect(liveStates(model).bob).toEqual({ kind: "work", task: "T1" });
  });

  it("returns idle when all stints are closed", () => {
    const model = makeModel({
      lanes: [{ id: "bob", firstT: 0 }],
      stints: [
        { agent: "bob", task: "T1", kind: "work", t0: 0, t1: 100 },
      ],
    });
    expect(liveStates(model).bob).toEqual({ kind: "idle" });
  });

  it("prefers the first open stint when multiple exist", () => {
    const model = makeModel({
      lanes: [{ id: "bob", firstT: 0 }],
      stints: [
        { agent: "bob", task: "T1", kind: "work", t0: 0, t1: null },
        { agent: "bob", task: "T2", kind: "review", t0: 10, t1: null },
      ],
    });
    expect(liveStates(model).bob).toEqual({ kind: "work", task: "T1" });
  });
});

describe("tAt", () => {
  it("round-trips x for a model with no gaps", () => {
    const model = deriveFlow([
      joinEvent(0, "alice", "e1"),
      joinEvent(100_000, "bob", "e2"),
      joinEvent(200_000, "alice", "e3"),
    ]);
    expect(model.gaps).toHaveLength(0);
    for (let i = 0; i <= 10; i++) {
      const xi = i / 10;
      const ti = tAt(model, xi);
      expect(model.x(ti)).toBeCloseTo(xi, 10);
    }
  });

  it("returns the gap start when x falls inside a compressed gap", () => {
    const gapMs = 60 * 60 * 1000; // 1 hour gap
    const model = deriveFlow([
      joinEvent(0, "alice", "e1"),
      joinEvent(10_000, "bob", "e2"),
      joinEvent(10_000 + gapMs + 1000, "alice", "e3"),
      // Working time after the gap so "just after the gap" exists.
      joinEvent(10_000 + gapMs + 1000 + 600_000, "bob", "e4"),
    ]);
    expect(model.gaps).toHaveLength(1);
    const g = model.gaps[0];
    const x0 = model.x(g.t0);
    const x1 = model.x(g.t1);
    expect(x1).toBeGreaterThan(x0);

    // Mid-gap maps to gap start.
    const tMid = tAt(model, (x0 + x1) / 2);
    expect(tMid).toBe(g.t0);

    // Just before the gap maps to a real time before the gap.
    const tBefore = tAt(model, x0 - 0.001);
    expect(tBefore).toBeLessThan(g.t0);
    expect(model.x(tBefore)).toBeCloseTo(x0 - 0.001, 3);

    // Just after the gap maps to a real time after the gap.
    const tAfter = tAt(model, x1 + 0.001);
    expect(tAfter).toBeGreaterThan(g.t1);
    expect(model.x(tAfter)).toBeCloseTo(x1 + 0.001, 3);
  });

  it("returns tMin/tMax at the edges", () => {
    const model = deriveFlow([
      joinEvent(1_000, "alice", "e1"),
      joinEvent(2_000, "bob", "e2"),
    ]);
    expect(tAt(model, 0)).toBe(model.tMin);
    expect(tAt(model, 1)).toBe(model.tMax);
  });

  it("returns tMin for degenerate models", () => {
    const model = makeModel();
    expect(tAt(model, 0.5)).toBe(0);
  });
});
