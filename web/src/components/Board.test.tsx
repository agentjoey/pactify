import { render, screen, fireEvent } from "@testing-library/react";
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

describe("Board — accepted column folding", () => {
  it("collapses the accepted column into a summary bar by default", () => {
    const st: State = {
      project: "demo", awaiting_count: 0,
      agents: [{ id: "a", roles: ["orchestrator"] }],
      features: [
        { id: "f1", branch: "feat-1", status: "merged", tasks: [
          { id: "t1", owner: "a", status: "accepted", reviewer: "a", spec: "", evidence: "" },
          { id: "t2", owner: "a", status: "accepted", reviewer: "a", spec: "", evidence: "" },
        ]},
        { id: "f2", branch: "feat-2", status: "active", tasks: [
          { id: "t3", owner: "a", status: "accepted", reviewer: "a", spec: "", evidence: "" },
        ]},
      ],
    };
    render(<Board state={st} selected="" onSelect={() => {}} />);
    expect(screen.getByTestId("accepted-summary")).toHaveTextContent("3");
    expect(screen.queryByText("t1")).toBeNull();
  });

  it("expands the accepted column and groups by feature with shipped groups collapsed", () => {
    const st: State = {
      project: "demo", awaiting_count: 0,
      agents: [{ id: "a", roles: ["orchestrator"] }],
      features: [
        { id: "f1", branch: "feat-1", status: "merged", tasks: [
          { id: "t1", owner: "a", status: "accepted", reviewer: "a", spec: "", evidence: "" },
        ]},
        { id: "f2", branch: "feat-2", status: "active", tasks: [
          { id: "t3", owner: "a", status: "accepted", reviewer: "a", spec: "", evidence: "" },
        ]},
      ],
    };
    render(<Board state={st} selected="" onSelect={() => {}} />);
    fireEvent.click(screen.getByTestId("accepted-summary"));
    expect(screen.getAllByText("t3").length).toBeGreaterThan(0);
    expect(screen.queryAllByText("t1")).toHaveLength(0);
    fireEvent.click(screen.getByTestId("accepted-group-f1"));
    expect(screen.getAllByText("t1").length).toBeGreaterThan(0);
  });
});
