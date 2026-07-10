import { describe, it, expect, vi } from "vitest";
import { render, screen, fireEvent } from "@testing-library/react";
import { FlowLanes } from "./FlowLanes";
import type { FlowModel, FlowStint, FlowArrow } from "../../lib/flowderive";
import type { State } from "../../lib/types";

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

const agents: State["agents"] = [
  { id: "alice", roles: ["orchestrator"] },
  { id: "bob", roles: ["worker"] },
];

describe("FlowLanes", () => {
  it("renders the empty state when the model has no activity", () => {
    render(<FlowLanes model={makeModel()} agents={agents} selected="" onSelect={() => {}} />);
    expect(screen.getByText("No activity yet")).toBeInTheDocument();
  });

  it("renders lane names and stint bars for each agent", () => {
    const stints: FlowStint[] = [
      { agent: "bob", task: "T1", kind: "work", t0: 0, t1: 100 },
      { agent: "alice", task: "T1", kind: "review", t0: 100, t1: null },
    ];
    const model = makeModel({
      lanes: [
        { id: "alice", firstT: 100 },
        { id: "bob", firstT: 0 },
      ],
      stints,
      tMin: 0,
      tMax: 100,
      x: (t: number) => t / 100,
    });
    render(<FlowLanes model={model} agents={agents} selected="" onSelect={() => {}} />);
    expect(screen.getByText("alice")).toBeInTheDocument();
    expect(screen.getByText("bob")).toBeInTheDocument();
    const stintBars = screen.getAllByTestId("flow-stint");
    expect(stintBars.length).toBe(2);
    expect(stintBars[0]).toHaveAttribute("data-task", "T1");
  });

  it("renders a changes arrow", () => {
    const arrows: FlowArrow[] = [
      { verb: "changes", from: "alice", to: "bob", task: "T1", t: 50 },
    ];
    const model = makeModel({
      lanes: [
        { id: "alice", firstT: 50 },
        { id: "bob", firstT: 0 },
      ],
      arrows,
      tMin: 0,
      tMax: 100,
      x: (t: number) => t / 100,
    });
    render(<FlowLanes model={model} agents={agents} selected="" onSelect={() => {}} />);
    expect(screen.getByTestId("flow-arrow-changes")).toBeInTheDocument();
  });

  it("calls onSelect with the task id when a stint is clicked", () => {
    const onSelect = vi.fn();
    const stints: FlowStint[] = [
      { agent: "bob", task: "T1", kind: "work", t0: 0, t1: 100 },
    ];
    const model = makeModel({
      lanes: [{ id: "bob", firstT: 0 }],
      stints,
      tMin: 0,
      tMax: 100,
      x: (t: number) => t / 100,
    });
    render(<FlowLanes model={model} agents={agents} selected="" onSelect={onSelect} />);
    fireEvent.click(screen.getByTestId("flow-stint"));
    expect(onSelect).toHaveBeenCalledWith("T1");
  });

  it("shows idle chips for agents with no open stint and working for open stints", () => {
    const model = makeModel({
      lanes: [{ id: "bob", firstT: 0 }],
      stints: [{ agent: "bob", task: "T1", kind: "work", t0: 0, t1: null }],
      tMin: 0,
      tMax: 100,
      x: (t: number) => t / 100,
    });
    render(<FlowLanes model={model} agents={agents} selected="" onSelect={() => {}} />);
    // alice has no open stint → idle; bob has an open work stint → working.
    expect(screen.getByText("idle")).toBeInTheDocument();
    expect(screen.getByText("working")).toBeInTheDocument();
  });
});
