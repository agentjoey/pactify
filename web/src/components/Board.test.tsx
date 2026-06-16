import { render, screen } from "@testing-library/react";
import { describe, it, expect } from "vitest";
import type { State } from "../lib/types";
import { Board } from "./Board";

const fixture: State = {
  project: "demo",
  awaiting_count: 1,
  agents: [
    { id: "alice", roles: ["orchestrator"] },
    { id: "bob", roles: ["worker"] },
  ],
  features: [
    {
      id: "F1",
      branch: "feat/f1",
      status: "active",
      tasks: [
        { id: "T1", owner: "bob", status: "awaiting_review", reviewer: "alice", spec: "", evidence: "" },
        { id: "T2", owner: "bob", status: "in_progress", reviewer: "alice", spec: "", evidence: "" },
      ],
    },
  ],
};

describe("Board — live pulse", () => {
  it("a task id in pulses gets the pulse class + status-colored --pulse-color", () => {
    render(<Board state={fixture} selected="" onSelect={() => {}} pulses={new Set(["T1"])} />);
    const pulsing = screen.getByTestId("board-pulse");
    expect(pulsing.className).toContain("pulse");
    // T1 is awaiting_review → the pact-state color (warn) drives the glow, so the
    // transition pulse reads in the same vocabulary as the StatusPill.
    expect(pulsing.getAttribute("style")).toContain("--pulse-color");
    expect(pulsing.getAttribute("style")).toContain("--color-warn");
    // T1 carries the data-testid; T2 (not pulsing) does not.
    expect(pulsing.textContent).toContain("T1");
  });

  it("no pulses → no pulse marker (first snapshot / quiescent)", () => {
    render(<Board state={fixture} selected="" onSelect={() => {}} />);
    expect(screen.queryByTestId("board-pulse")).toBeNull();
  });
});
