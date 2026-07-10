import { describe, it, expect } from "vitest";
import { liveStates, type FlowModel } from "./flowderive";

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
