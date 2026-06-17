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

describe("Board — accepted column recent + fold", () => {
  // 13 accepted tasks across two features, in log order t01..t13.
  const manyAccepted: State = {
    project: "demo", awaiting_count: 0,
    agents: [{ id: "a", roles: ["orchestrator"] }],
    features: [
      { id: "f1", branch: "feat-1", status: "merged",
        tasks: Array.from({ length: 8 }, (_, i) => ({
          id: `t${String(i + 1).padStart(2, "0")}`, owner: "a", status: "accepted", reviewer: "a", spec: "", evidence: "",
        })) },
      { id: "f2", branch: "feat-2", status: "active",
        tasks: Array.from({ length: 5 }, (_, i) => ({
          id: `t${String(i + 9).padStart(2, "0")}`, owner: "a", status: "accepted", reviewer: "a", spec: "", evidence: "",
        })) },
    ],
  };

  it("shows the 10 most-recent accepted cards and folds the rest behind a 'more' button", () => {
    render(<Board state={manyAccepted} selected="" onSelect={() => {}} />);
    // Most-recent-first: t13 (newest) is visible, t01 (oldest, 13th) is folded.
    expect(screen.getAllByText("t13").length).toBeGreaterThan(0);
    expect(screen.queryAllByText("t01")).toHaveLength(0);
    // 13 total − 10 shown = 3 folded.
    expect(screen.getByTestId("accepted-more")).toHaveTextContent("3 more accepted");
  });

  it("expands to show all accepted, then collapses back to recent 10", () => {
    render(<Board state={manyAccepted} selected="" onSelect={() => {}} />);
    fireEvent.click(screen.getByTestId("accepted-more"));
    expect(screen.getAllByText("t01").length).toBeGreaterThan(0); // folded one now visible
    fireEvent.click(screen.getByTestId("accepted-less"));
    expect(screen.queryAllByText("t01")).toHaveLength(0); // folded again
  });

  it("does not render a fold button when 10 or fewer accepted", () => {
    const few: State = {
      project: "demo", awaiting_count: 0,
      agents: [{ id: "a", roles: ["orchestrator"] }],
      features: [{ id: "f1", branch: "feat-1", status: "active", tasks: [
        { id: "only", owner: "a", status: "accepted", reviewer: "a", spec: "", evidence: "" },
      ]}],
    };
    render(<Board state={few} selected="" onSelect={() => {}} />);
    expect(screen.getAllByText("only").length).toBeGreaterThan(0);
    expect(screen.queryByTestId("accepted-more")).toBeNull();
  });
});
