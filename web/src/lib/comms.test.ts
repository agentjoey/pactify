import { describe, it, expect } from "vitest";
import { deriveComms, pulseTargets } from "./comms";
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

describe("deriveComms — wait edges", () => {
  it("awaiting_review → review edge owner→reviewer", () => {
    const r = deriveComms(mk(seats, [task({ id: "T1", status: "awaiting_review" })]));
    expect(r.edges).toEqual([
      { from: "worker", to: "rev", kind: "review", reason: "awaiting review: T1", taskId: "T1" },
    ]);
  });

  it("changes_requested → rework edge reviewer→owner", () => {
    const r = deriveComms(mk(seats, [task({ id: "T1", status: "changes_requested" })]));
    expect(r.edges).toEqual([
      { from: "rev", to: "worker", kind: "rework", reason: "changes requested: T1", taskId: "T1" },
    ]);
  });

  it("emits one dep edge per unmet dep (task ids), accepted deps excluded", () => {
    const r = deriveComms(mk(seats, [
      task({ id: "A", status: "accepted" }),
      task({ id: "B", status: "in_progress" }),
      task({ id: "T1", deps: ["A", "B"] }),
    ]));
    expect(r.edges).toEqual([
      { from: "T1", to: "B", kind: "dep", reason: "blocked by B", taskId: "T1" },
    ]);
  });
});

describe("deriveComms — blocked chains", () => {
  it("transitive: A deps B, B deps C (C not accepted) → A and B blocked, C not", () => {
    const r = deriveComms(mk(seats, [
      task({ id: "C", status: "in_progress" }),
      task({ id: "B", status: "in_progress", deps: ["C"] }),
      task({ id: "A", status: "in_progress", deps: ["B"] }),
    ]));
    expect(r.blockedTasks.sort()).toEqual(["A", "B"]);
    expect(r.blockedTasks).not.toContain("C");
  });

  it("accepted task is never blocked even with an unmet dep", () => {
    const r = deriveComms(mk(seats, [
      task({ id: "B", status: "in_progress" }),
      task({ id: "A", status: "accepted", deps: ["B"] }),
    ]));
    expect(r.blockedTasks).toEqual([]);
  });
});

describe("deriveComms — notJoined", () => {
  it("flags an owner missing from agents", () => {
    const r = deriveComms(mk([{ id: "rev", roles: ["reviewer"] }], [
      task({ id: "T1", owner: "ghost", reviewer: "rev" }),
    ]));
    expect(r.notJoined).toEqual(["ghost"]);
  });

  it("flags a reviewer missing from agents", () => {
    const r = deriveComms(mk([{ id: "worker", roles: ["worker"] }], [
      task({ id: "T1", owner: "worker", reviewer: "ghost" }),
    ]));
    expect(r.notJoined).toEqual(["ghost"]);
  });

  it("dedups in first-appearance order", () => {
    const r = deriveComms(mk([], [
      task({ id: "T1", owner: "a", reviewer: "b" }),
      task({ id: "T2", owner: "a", reviewer: "c" }),
    ]));
    expect(r.notJoined).toEqual(["a", "b", "c"]);
  });
});

describe("deriveComms — idleSeats", () => {
  it("seat owning no active task and reviewing nothing pending is idle", () => {
    // worker owns an in_progress task → active as owner; rev reviews an
    // awaiting_review task → active as reviewer; idle does neither → only idle.
    const r = deriveComms(mk(
      [{ id: "worker", roles: ["worker"] }, { id: "rev", roles: ["reviewer"] }, { id: "idle", roles: ["worker"] }],
      [
        task({ id: "T1", owner: "worker", status: "in_progress", reviewer: "rev" }),
        task({ id: "T2", owner: "other", status: "awaiting_review", reviewer: "rev" }),
      ],
    ));
    expect(r.idleSeats).toEqual(["idle"]);
  });

  it("owner of an in_progress task is not idle", () => {
    const r = deriveComms(mk(seats, [task({ id: "T1", owner: "worker", status: "in_progress", reviewer: "rev" })]));
    expect(r.idleSeats).not.toContain("worker");
  });

  it("reviewer of an awaiting_review task is not idle", () => {
    const r = deriveComms(mk(seats, [task({ id: "T1", owner: "worker", status: "awaiting_review", reviewer: "rev" })]));
    expect(r.idleSeats).not.toContain("rev");
  });

  it("accepted-only work leaves both seats idle", () => {
    const r = deriveComms(mk(seats, [task({ id: "T1", owner: "worker", status: "accepted", reviewer: "rev" })]));
    expect(r.idleSeats.sort()).toEqual(["rev", "worker"]);
  });
});

describe("deriveComms — clean state", () => {
  it("in-flight only → no edges, no blocked, no missing (reviewer idle until handoff)", () => {
    const r = deriveComms(mk(seats, [task({ id: "T1", owner: "worker", status: "in_progress", reviewer: "rev" })]));
    expect(r).toEqual({ edges: [], idleSeats: ["rev"], notJoined: [], blockedTasks: [] });
  });
});

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
