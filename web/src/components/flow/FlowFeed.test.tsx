import { describe, it, expect, vi } from "vitest";
import { render, screen, fireEvent } from "@testing-library/react";
import { FlowFeed } from "./FlowFeed";
import type { PactEvent, State } from "../../lib/types";

const agents: State["agents"] = [
  { id: "alice", roles: ["orchestrator"] },
  { id: "bob", roles: ["worker"] },
  { id: "carol", roles: ["reviewer"] },
];

const baseEvents: PactEvent[] = [
  {
    event_id: "j1",
    ts: "2026-01-01T00:00:00.000Z",
    agent_id: "bob",
    role: "worker",
    event_type: "join",
    task_id: "",
    feature: "F1",
    payload: {},
  },
  {
    event_id: "a1",
    ts: "2026-01-01T00:01:00.000Z",
    agent_id: "alice",
    role: "orchestrator",
    event_type: "assign",
    task_id: "T1",
    feature: "F1",
    payload: { owner: "bob", reviewer: "carol" },
  },
  {
    event_id: "cp1",
    ts: "2026-01-01T00:02:00.000Z",
    agent_id: "bob",
    role: "worker",
    event_type: "checkpoint",
    task_id: "T1",
    feature: "F1",
    payload: {},
  },
  {
    event_id: "ch1",
    ts: "2026-01-01T00:03:00.000Z",
    agent_id: "carol",
    role: "reviewer",
    event_type: "changes_requested",
    task_id: "T1",
    feature: "F1",
    payload: { reason: "fix it" },
  },
  {
    event_id: "ac1",
    ts: "2026-01-01T00:04:00.000Z",
    agent_id: "carol",
    role: "reviewer",
    event_type: "accept",
    task_id: "T1",
    feature: "F1",
    payload: {},
  },
  {
    event_id: "m1",
    ts: "2026-01-01T00:05:00.000Z",
    agent_id: "alice",
    role: "orchestrator",
    event_type: "merge",
    task_id: "T1",
    feature: "F1",
    payload: {},
  },
];

const defaultState: State = {
  project: "demo",
  awaiting_count: 0,
  agents,
  features: [],
};

function renderFeed(props: Omit<React.ComponentProps<typeof FlowFeed>, "state">) {
  return render(<FlowFeed {...props} state={defaultState} />);
}

describe("FlowFeed", () => {
  it("renders message cards for each verb", () => {
    renderFeed({ events: baseEvents, agents, selected: "", onSelect: () => {} });
    expect(screen.getByTestId("flow-msg-join")).toBeInTheDocument();
    expect(screen.getByTestId("flow-msg-assign")).toBeInTheDocument();
    expect(screen.getByTestId("flow-msg-checkpoint")).toBeInTheDocument();
    expect(screen.getByTestId("flow-msg-changes")).toBeInTheDocument();
    expect(screen.getByTestId("flow-msg-accept")).toBeInTheDocument();
    expect(screen.getByTestId("flow-msg-merge")).toBeInTheDocument();
  });

  it("marks the changes card with the danger frame semantic", () => {
    renderFeed({ events: baseEvents, agents, selected: "", onSelect: () => {} });
    expect(screen.getByTestId("flow-msg-changes")).toHaveAttribute("data-frame", "danger");
  });

  it("calls onSelect with the task id when a card with a task is clicked", () => {
    const onSelect = vi.fn();
    renderFeed({ events: baseEvents, agents, selected: "", onSelect });
    fireEvent.click(screen.getByTestId("flow-msg-assign"));
    expect(onSelect).toHaveBeenCalledWith("T1");
  });

  it("does not call onSelect for a card without a task", () => {
    const onSelect = vi.fn();
    renderFeed({ events: baseEvents, agents, selected: "", onSelect });
    fireEvent.click(screen.getByTestId("flow-msg-join"));
    expect(onSelect).not.toHaveBeenCalled();
  });

  it("reflects working live state in the seat chips", () => {
    const events = baseEvents.slice(0, 2); // join + assign → bob working on T1
    renderFeed({ events, agents, selected: "", onSelect: () => {} });
    expect(screen.getByText(/working · T1/)).toBeInTheDocument();
    expect(screen.getAllByText("idle").length).toBeGreaterThanOrEqual(2);
  });

  it("renders the empty state when there are no events", () => {
    renderFeed({ events: [], agents, selected: "", onSelect: () => {} });
    expect(screen.getByText("No activity yet")).toBeInTheDocument();
  });

  it("shows a blocked chip when the current task is blocked", () => {
    const state: State = {
      ...defaultState,
      features: [
        {
          id: "F1",
          branch: "F1",
          status: "open",
          tasks: [
            { id: "T1", owner: "bob", status: "working", reviewer: "carol", spec: "", evidence: "" },
            { id: "T2", owner: "bob", status: "working", reviewer: "carol", spec: "", evidence: "", deps: ["T1"] },
          ],
        },
      ],
    };
    const events: PactEvent[] = [
      {
        event_id: "j1",
        ts: "2026-01-01T00:00:00.000Z",
        agent_id: "bob",
        role: "worker",
        event_type: "join",
        task_id: "",
        feature: "F1",
        payload: {},
      },
      {
        event_id: "a2",
        ts: "2026-01-01T00:01:00.000Z",
        agent_id: "alice",
        role: "orchestrator",
        event_type: "assign",
        task_id: "T2",
        feature: "F1",
        payload: { owner: "bob", reviewer: "carol" },
      },
    ];
    render(<FlowFeed events={events} agents={agents} selected="" onSelect={() => {}} state={state} />);
    expect(screen.getByText(/blocked · 等 T1/)).toBeInTheDocument();
  });
});
