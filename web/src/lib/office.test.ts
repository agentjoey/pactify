import { describe, it, expect } from "vitest";
import { deriveOffice } from "./office";
import type { State, Task } from "./types";

// mk builds a minimal State with the given agents and one feature whose tasks are
// supplied verbatim (mirrors comms.test.ts). Multi-feature cases pass a custom
// features array via mkFeatures.
const mk = (agents: State["agents"], tasks: Task[]): State => ({
  project: "p",
  agents,
  awaiting_count: 0,
  features: [{ id: "F", branch: "feat/x", status: "in_progress", tasks }],
});

const mkFeatures = (agents: State["agents"], features: State["features"]): State => ({
  project: "p",
  agents,
  awaiting_count: 0,
  features,
});

const task = (over: Partial<Task> & { id: string }): Task => ({
  owner: "worker", status: "in_progress", reviewer: "rev", spec: "", evidence: "", ...over,
});

const seats = [{ id: "worker", roles: ["worker"] }, { id: "rev", roles: ["reviewer"] }];

const deskOf = (state: State, seatId: string) =>
  deriveOffice(state).desks.find((d) => d.seatId === seatId)!;

describe("deriveOffice — desk roster", () => {
  it("desks = joined agents only, in roster order; not-joined owner gets no desk", () => {
    const state = mk(
      [{ id: "rev", roles: ["reviewer"] }, { id: "worker", roles: ["worker"] }],
      // ghost owns/reviews tasks but never joined → no desk for ghost
      [task({ id: "T1", owner: "ghost", reviewer: "rev" })],
    );
    const { desks } = deriveOffice(state);
    expect(desks.map((d) => d.seatId)).toEqual(["rev", "worker"]); // roster order
    expect(desks.find((d) => d.seatId === "ghost")).toBeUndefined();
  });

  it("empty state → no desks, no shipped", () => {
    expect(deriveOffice(mk([], []))).toEqual({ desks: [], shipped: [] });
  });

  it("carries roles through from the roster seat", () => {
    const d = deskOf(mk(seats, []), "worker");
    expect(d.roles).toEqual(["worker"]);
  });
});

describe("deriveOffice — zones", () => {
  it("doing = own tasks in assigned/in_progress only", () => {
    const state = mk(seats, [
      task({ id: "A", owner: "worker", status: "assigned" }),
      task({ id: "B", owner: "worker", status: "in_progress" }),
      task({ id: "C", owner: "worker", status: "awaiting_review" }),
      task({ id: "D", owner: "worker", status: "accepted" }),
      task({ id: "E", owner: "rev", status: "in_progress" }), // not worker's
    ]);
    const d = deskOf(state, "worker");
    expect(d.doing.map((t) => t.id)).toEqual(["A", "B"]);
  });

  it("inbox = awaiting_review tasks where the seat is the reviewer", () => {
    const state = mk(seats, [
      task({ id: "A", owner: "worker", reviewer: "rev", status: "awaiting_review" }),
      task({ id: "B", owner: "worker", reviewer: "rev", status: "in_progress" }), // not yet
      task({ id: "C", owner: "rev", reviewer: "worker", status: "awaiting_review" }), // worker reviews
    ]);
    expect(deskOf(state, "rev").inbox.map((t) => t.id)).toEqual(["A"]);
    expect(deskOf(state, "worker").inbox.map((t) => t.id)).toEqual(["C"]);
  });

  it("waitingOn: own awaiting_review → {on: reviewer, reason: review}", () => {
    const state = mk(seats, [
      task({ id: "A", owner: "worker", reviewer: "rev", status: "awaiting_review" }),
    ]);
    const d = deskOf(state, "worker");
    expect(d.waitingOn).toEqual([
      { task: expect.objectContaining({ id: "A" }), on: "rev", reason: "review" },
    ]);
  });

  it("waitingOn: own non-accepted task with unmet deps → one item per unmet dep {on: depId, reason: dep}", () => {
    const state = mk(seats, [
      task({ id: "X", owner: "rev", status: "in_progress" }),      // unmet
      task({ id: "Y", owner: "rev", status: "accepted" }),         // met → excluded
      task({ id: "T", owner: "worker", status: "assigned", deps: ["X", "Y"] }),
    ]);
    const d = deskOf(state, "worker");
    const deps = d.waitingOn.filter((w) => w.reason === "dep");
    expect(deps).toEqual([
      { task: expect.objectContaining({ id: "T" }), on: "X", reason: "dep" },
    ]);
  });

  it("accepted task with an unmet dep does NOT produce a dep waitingOn item", () => {
    const state = mk(seats, [
      task({ id: "X", owner: "rev", status: "in_progress" }),
      task({ id: "T", owner: "worker", status: "accepted", deps: ["X"] }),
    ]);
    expect(deskOf(state, "worker").waitingOn).toEqual([]);
  });

  it("overlap: an in_progress task with an unmet dep appears in BOTH doing and waitingOn", () => {
    const state = mk(seats, [
      task({ id: "X", owner: "rev", status: "in_progress" }),
      task({ id: "T", owner: "worker", status: "in_progress", deps: ["X"] }),
    ]);
    const d = deskOf(state, "worker");
    expect(d.doing.map((t) => t.id)).toContain("T");
    expect(d.waitingOn).toEqual([
      { task: expect.objectContaining({ id: "T" }), on: "X", reason: "dep" },
    ]);
  });
});

describe("deriveOffice — multi-role seat", () => {
  it("an orchestrator+reviewer seat still gets an inbox for tasks it reviews", () => {
    const state = mk(
      [{ id: "lead", roles: ["orchestrator", "reviewer"] }, { id: "worker", roles: ["worker"] }],
      [task({ id: "A", owner: "worker", reviewer: "lead", status: "awaiting_review" })],
    );
    const d = deskOf(state, "lead");
    expect(d.roles).toEqual(["orchestrator", "reviewer"]);
    expect(d.inbox.map((t) => t.id)).toEqual(["A"]);
  });
});

describe("deriveOffice — status precedence", () => {
  it("BUSY requires an in_progress task (assigned alone is not busy)", () => {
    const state = mk(seats, [task({ id: "A", owner: "worker", status: "in_progress" })]);
    expect(deskOf(state, "worker").status).toBe("busy");
  });

  it("REVIEW_DUE when inbox non-empty (and no own in_progress)", () => {
    const state = mk(seats, [
      task({ id: "A", owner: "worker", reviewer: "rev", status: "awaiting_review" }),
    ]);
    expect(deskOf(state, "rev").status).toBe("review_due");
  });

  it("busy beats review_due: own in_progress AND a pending review", () => {
    const state = mk(
      [{ id: "rev", roles: ["reviewer"] }, { id: "worker", roles: ["worker"] }],
      [
        task({ id: "A", owner: "rev", status: "in_progress" }),               // rev is busy
        task({ id: "B", owner: "worker", reviewer: "rev", status: "awaiting_review" }), // rev reviews
      ],
    );
    expect(deskOf(state, "rev").status).toBe("busy");
  });

  it("WAITING when waitingOn non-empty and nothing busy/review (own awaiting_review)", () => {
    const state = mk(seats, [
      task({ id: "A", owner: "worker", reviewer: "rev", status: "awaiting_review" }),
    ]);
    expect(deskOf(state, "worker").status).toBe("waiting");
  });

  it("review_due beats waiting", () => {
    // rev has an inbox item (review_due) AND its own awaiting_review parked (waiting)
    const state = mk(
      [{ id: "rev", roles: ["reviewer"] }, { id: "worker", roles: ["worker"] }, { id: "boss", roles: ["orchestrator"] }],
      [
        task({ id: "A", owner: "worker", reviewer: "rev", status: "awaiting_review" }), // rev inbox
        task({ id: "B", owner: "rev", reviewer: "boss", status: "awaiting_review" }),   // rev waiting
      ],
    );
    expect(deskOf(state, "rev").status).toBe("review_due");
  });

  it("IDLE when no in_progress, empty inbox, empty waitingOn", () => {
    expect(deskOf(mk(seats, []), "worker").status).toBe("idle");
  });

  it("only-assigned desk reads IDLE-with-doing (assigned is not in_progress, no inbox/waiting)", () => {
    const state = mk(seats, [task({ id: "A", owner: "worker", status: "assigned" })]);
    const d = deskOf(state, "worker");
    expect(d.doing.map((t) => t.id)).toEqual(["A"]); // it has work on the desk
    expect(d.status).toBe("idle");                    // but no in_progress → not busy; reads idle
  });
});

describe("deriveOffice — shipped tray", () => {
  it("shipped = accepted tasks across features, most recent first (reverse appearance order)", () => {
    const state = mkFeatures(seats, [
      { id: "F1", branch: "feat/a", status: "merged", tasks: [
        task({ id: "A", status: "accepted" }),
        task({ id: "B", status: "in_progress" }),
      ]},
      { id: "F2", branch: "feat/b", status: "in_progress", tasks: [
        task({ id: "C", status: "accepted" }),
      ]},
    ]);
    // appearance order A, C → most-recent-first → C, A
    expect(deriveOffice(state).shipped.map((t) => t.id)).toEqual(["C", "A"]);
  });

  it("no accepted tasks → empty shipped", () => {
    expect(deriveOffice(mk(seats, [task({ id: "A", status: "in_progress" })])).shipped).toEqual([]);
  });
});

describe("deriveOffice — changes_requested (rework returns to the owner's desk)", () => {
  it("a changes_requested task sits in the owner's doing zone (board5: red lane back)", () => {
    const st = mk(
      [{ id: "worker", roles: ["worker"] }, { id: "rev", roles: ["reviewer"] }],
      [task({ id: "T1", owner: "worker", reviewer: "rev", status: "changes_requested" })],
    );
    const d = deskOf(st, "worker");
    expect(d.doing.map((t) => t.id)).toEqual(["T1"]);
    expect(d.inbox).toEqual([]);
    // rework is queued like assigned work: not busy (no in_progress) → idle-with-doing
    expect(d.status).toBe("idle");
  });
});
