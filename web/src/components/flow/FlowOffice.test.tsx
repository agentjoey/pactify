import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { render, screen, fireEvent, act } from "@testing-library/react";
import { FlowOffice } from "./FlowOffice";
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

function renderOffice(props: Omit<React.ComponentProps<typeof FlowOffice>, "state">) {
  return render(<FlowOffice {...props} state={defaultState} />);
}

describe("FlowOffice", () => {
  it("renders a desk card for each agent with name, role and live state", () => {
    renderOffice({ events: baseEvents, agents, onSelect: () => {} });
    const desks = screen.getAllByTestId("flow-desk");
    expect(desks).toHaveLength(3);
    expect(screen.getByText("alice")).toBeInTheDocument();
    expect(screen.getByText("bob")).toBeInTheDocument();
    expect(screen.getByText("carol")).toBeInTheDocument();
    expect(screen.getByText("orchestrator")).toBeInTheDocument();
    expect(screen.getByText("worker")).toBeInTheDocument();
    expect(screen.getByText("reviewer")).toBeInTheDocument();
    // After the full sequence bob is in rework on T1.
    expect(screen.getByText(/rework · T1/)).toBeInTheDocument();
  });

  it("shows event counts per desk", () => {
    renderOffice({ events: baseEvents, agents, onSelect: () => {} });
    const bobDesk = document.querySelector('[data-testid="flow-desk"][data-agent="bob"]');
    expect(bobDesk).toHaveTextContent("events 2");
  });

  it("shows the main base with the merge count and latest merge", () => {
    renderOffice({ events: baseEvents, agents, onSelect: () => {} });
    const main = screen.getByTestId("flow-office-main");
    expect(main).toHaveTextContent("merges 1");
    expect(main).toHaveTextContent("T1");
    expect(main).toHaveTextContent("00:05");
  });

  it("shows the three most recent events in the ticker", () => {
    renderOffice({ events: baseEvents, agents, onSelect: () => {} });
    expect(screen.getByText(/alice merge · T1/)).toBeInTheDocument();
    expect(screen.getByText(/carol accept · T1/)).toBeInTheDocument();
    expect(screen.getByText(/carol changes · T1: fix it/)).toBeInTheDocument();
  });

  it("calls onSelect with the current task when a busy desk is clicked", () => {
    const onSelect = vi.fn();
    renderOffice({ events: baseEvents, agents, onSelect });
    const bobDesk = document.querySelector('[data-testid="flow-desk"][data-agent="bob"]');
    fireEvent.click(bobDesk!);
    expect(onSelect).toHaveBeenCalledWith("T1");
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
    render(<FlowOffice events={events} agents={agents} onSelect={() => {}} state={state} />);
    expect(screen.getByText(/blocked · 等 T1/)).toBeInTheDocument();
  });

  describe("elapsed timer", () => {
    beforeEach(() => {
      vi.useFakeTimers();
      vi.setSystemTime(new Date("2026-01-01T00:06:00.000Z"));
    });

    afterEach(() => {
      vi.useRealTimers();
    });

    it("shows elapsed minutes for an open stint and advances on tick", () => {
      renderOffice({ events: baseEvents, agents, onSelect: () => {} });
      const bobDesk = document.querySelector('[data-testid="flow-desk"][data-agent="bob"]');
      // Bob's rework stint started at 00:03:00; current fake time is 00:06:00.
      expect(bobDesk).toHaveTextContent("elapsed 3m");
      act(() => {
        vi.advanceTimersByTime(30_000);
      });
      expect(bobDesk).toHaveTextContent("elapsed 4m");
    });

    it("does not show elapsed for idle desks", () => {
      renderOffice({ events: baseEvents, agents, onSelect: () => {} });
      const aliceDesk = document.querySelector('[data-testid="flow-desk"][data-agent="alice"]');
      expect(aliceDesk).not.toHaveTextContent("elapsed");
    });
  });
});
