import { render, screen, fireEvent } from "@testing-library/react";
import { describe, it, expect } from "vitest";
import type { PactEvent, State } from "../../lib/types";
import { EventDrawer } from "./EventDrawer";

const AGENTS = [
  { id: "claude", roles: ["orchestrator", "reviewer"] },
  { id: "opencode-worker", roles: ["worker"] },
];
const STATE: State = { project: "p1", agents: AGENTS, features: [], awaiting_count: 0 };

const ev = (over: Partial<PactEvent>): PactEvent => ({
  event_id: Math.random().toString(36).slice(2),
  ts: "2026-07-07T12:34:56Z",
  agent_id: "opencode-worker",
  role: "worker",
  event_type: "checkpoint",
  task_id: "t1",
  feature: "feat-x",
  payload: {},
  ...over,
});

describe("EventDrawer (the former Live event-stream pane)", () => {
  it("collapsed by default: shows the latest-event ticker, not the terminal", () => {
    render(<EventDrawer events={[ev({ task_id: "t1" }), ev({ event_type: "accept", agent_id: "claude", task_id: "t2" })]} agents={AGENTS} state={STATE} />);
    expect(screen.getByTestId("event-drawer").dataset.state).toBe("collapsed");
    expect(screen.queryByTestId("event-stream")).toBeNull();
    // Ticker shows the LATEST event (accept by claude on t2).
    const toggle = screen.getByTestId("event-drawer-toggle");
    expect(toggle.textContent).toContain("claude");
    expect(toggle.textContent).toContain("accept");
    expect(toggle.textContent).toContain("t2");
  });

  it("expands to the full terminal + seat presence footer", () => {
    render(<EventDrawer events={[ev({}), ev({ event_type: "accept", agent_id: "claude" })]} agents={AGENTS} state={STATE} />);
    fireEvent.click(screen.getByTestId("event-drawer-toggle"));
    expect(screen.getByTestId("event-drawer").dataset.state).toBe("open");
    const stream = screen.getByTestId("event-stream");
    expect(stream.textContent).toContain("checkpoint");
    expect(stream.textContent).toContain("accept");
    // Seat presence footer: both seats listed (agent ids also appear in the
    // stream lines, so assert on the footer's presence + multiplicity).
    expect(screen.getByText("SEATS")).toBeTruthy();
    expect(screen.getAllByText("opencode-worker").length).toBeGreaterThanOrEqual(2);
  });

  it("renders the empty placeholder when there are no events", () => {
    render(<EventDrawer events={[]} agents={AGENTS} state={STATE} />);
    fireEvent.click(screen.getByTestId("event-drawer-toggle"));
    expect(screen.getByText("no events yet…")).toBeTruthy();
  });
});
