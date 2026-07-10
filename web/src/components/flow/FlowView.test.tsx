import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, fireEvent } from "@testing-library/react";
import { FlowView } from "./FlowView";
import type { State, PactEvent } from "../../lib/types";

const state: State = {
  project: "demo",
  awaiting_count: 0,
  agents: [{ id: "bob", roles: ["worker"] }],
  features: [],
};

const events: PactEvent[] = [
  {
    event_id: "e1",
    ts: "2026-01-01T00:00:00.000Z",
    agent_id: "bob",
    role: "worker",
    event_type: "join",
    task_id: "",
    feature: "",
    payload: {},
  },
];

beforeEach(() => {
  localStorage.clear();
});

describe("FlowView", () => {
  it("defaults to lanes mode and renders the lanes renderer", () => {
    render(<FlowView state={state} events={events} project="demo" selected="" onSelect={() => {}} />);
    expect(screen.getByTestId("flow-tab-lanes")).toBeInTheDocument();
    // The lanes renderer is present (one seat lane shown).
    expect(screen.getByText("bob")).toBeInTheDocument();
  });

  it("switches to feed placeholder and remembers the mode in localStorage", () => {
    render(<FlowView state={state} events={events} project="demo" selected="" onSelect={() => {}} />);
    fireEvent.click(screen.getByTestId("flow-tab-feed"));
    expect(screen.getByText("会话流渲染器 — 下一任务")).toBeInTheDocument();
    expect(localStorage.getItem("pactify:flowMode")).toBe("feed");
  });

  it("switches to office placeholder", () => {
    render(<FlowView state={state} events={events} project="demo" selected="" onSelect={() => {}} />);
    fireEvent.click(screen.getByTestId("flow-tab-office"));
    expect(screen.getByText("办公室渲染器 — 下一任务")).toBeInTheDocument();
  });

  it("restores the saved flow mode from localStorage", () => {
    localStorage.setItem("pactify:flowMode", "office");
    render(<FlowView state={state} events={events} project="demo" selected="" onSelect={() => {}} />);
    expect(screen.getByText("办公室渲染器 — 下一任务")).toBeInTheDocument();
  });

  it("forwards task selection from a stint click", () => {
    const onSelect = vi.fn();
    const assignEvent: PactEvent = {
      event_id: "e2",
      ts: "2026-01-01T00:01:00.000Z",
      agent_id: "bob",
      role: "worker",
      event_type: "assign",
      task_id: "T1",
      feature: "F1",
      payload: { owner: "bob" },
    };
    render(<FlowView state={state} events={[...events, assignEvent]} project="demo" selected="" onSelect={onSelect} />);
    fireEvent.click(screen.getByTestId("flow-stint"));
    expect(onSelect).toHaveBeenCalledWith("T1");
  });
});
