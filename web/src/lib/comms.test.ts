import { describe, it, expect } from "vitest";
import { pulseTargets } from "./comms";
import type { State, Task } from "./types";

// mk builds a minimal State with the given agents and one feature whose tasks
// are supplied verbatim, so each test can stage exactly the shape it asserts on.
const mk = (agents: State["agents"], tasks: Task[]): State => ({
  project: "p",
  agents,
  awaiting_count: 0,
  features: [{ id: "F", branch: "feat/x", status: "in_progress", tasks }],
});

const task = (over: Partial<Task> & { id: string }): Task => ({
  owner: "worker", status: "in_progress", reviewer: "rev", spec: "", evidence: "", ...over,
});

const seats = [{ id: "worker", roles: ["worker"] }, { id: "rev", roles: ["reviewer"] }];

describe("pulseTargets", () => {
  const base = mk(seats, [task({ id: "T1", status: "in_progress" })]);

  it("prev null → empty (first snapshot must not pulse everything)", () => {
    expect(pulseTargets(null, base)).toEqual({ taskIds: [] });
  });

  it("detects a status change", () => {
    const next = mk(seats, [task({ id: "T1", status: "awaiting_review" })]);
    expect(pulseTargets(base, next)).toEqual({ taskIds: ["T1"] });
  });

  it("detects a newly appearing task", () => {
    const next = mk(seats, [task({ id: "T1", status: "in_progress" }), task({ id: "T2", status: "assigned" })]);
    expect(pulseTargets(base, next)).toEqual({ taskIds: ["T2"] });
  });

  it("unchanged → empty", () => {
    expect(pulseTargets(base, base)).toEqual({ taskIds: [] });
  });
});
