import { describe, it, expect } from "vitest";
import { diffAwaiting } from "./Toasts";
import type { State } from "../lib/types";

const mk = (statuses: Record<string, string>): State => ({
  project: "p",
  agents: [],
  awaiting_count: 0,
  features: [
    {
      id: "F",
      branch: "b",
      status: "in_progress",
      tasks: Object.entries(statuses).map(([id, status]) => ({
        id,
        owner: "o",
        status,
        reviewer: "r",
        spec: "",
        evidence: "",
      })),
    },
  ],
});

describe("diffAwaiting", () => {
  it("flags a task that newly enters awaiting_review", () => {
    const prev = mk({ T1: "in_progress", T2: "assigned" });
    const next = mk({ T1: "awaiting_review", T2: "assigned" });
    expect(diffAwaiting(prev, next)).toEqual(["T1"]);
  });

  it("does not re-fire for a task already awaiting_review", () => {
    const prev = mk({ T1: "awaiting_review" });
    const next = mk({ T1: "awaiting_review" });
    expect(diffAwaiting(prev, next)).toEqual([]);
  });

  it("flags a brand-new task that appears already awaiting_review", () => {
    const prev = mk({ T1: "in_progress" });
    const next = mk({ T1: "in_progress", T2: "awaiting_review" });
    expect(diffAwaiting(prev, next)).toEqual(["T2"]);
  });

  it("a task leaving awaiting_review does not fire", () => {
    const prev = mk({ T1: "awaiting_review" });
    const next = mk({ T1: "accepted" });
    expect(diffAwaiting(prev, next)).toEqual([]);
  });
});
