import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, fireEvent } from "@testing-library/react";
import { FlowView } from "./FlowView";
import type { State, PactEvent } from "../../lib/types";
import type { DataSource } from "../../lib/datasource";
import { DataSourceProvider } from "../../lib/datasource";

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

function mockSource(): DataSource {
  return {
    capabilities: {
      canWrite: true,
      canOrchestrate: false,
      multiMachine: false,
      cockpit: false,
    },
    listProjects: vi.fn(),
    getState: vi.fn(),
    getStats: vi.fn().mockResolvedValue({ tasks: [], agents: [] }),
    subscribe: vi.fn(() => () => {}),
  } as unknown as DataSource;
}

function renderFlowView(props: React.ComponentProps<typeof FlowView>) {
  return render(
    <DataSourceProvider source={mockSource()}>
      <FlowView {...props} />
    </DataSourceProvider>,
  );
}

beforeEach(() => {
  localStorage.clear();
});

describe("FlowView", () => {
  it("defaults to lanes mode and renders the lanes renderer", () => {
    renderFlowView({ state, events, project: "demo", selected: "", onSelect: () => {} });
    expect(screen.getByTestId("flow-tab-lanes")).toBeInTheDocument();
    // The lanes renderer is present (one seat lane shown).
    expect(screen.getByText("bob")).toBeInTheDocument();
  });

  it("switches to feed renderer and remembers the mode in localStorage", () => {
    renderFlowView({ state, events, project: "demo", selected: "", onSelect: () => {} });
    fireEvent.click(screen.getByTestId("flow-tab-feed"));
    expect(screen.getByTestId("flow-msg-join")).toBeInTheDocument();
    expect(localStorage.getItem("pactify:flowMode")).toBe("feed");
  });

  it("switches to office renderer", () => {
    renderFlowView({ state, events, project: "demo", selected: "", onSelect: () => {} });
    fireEvent.click(screen.getByTestId("flow-tab-office"));
    expect(screen.getByTestId("flow-office-main")).toBeInTheDocument();
  });

  it("restores the saved flow mode from localStorage", () => {
    localStorage.setItem("pactify:flowMode", "office");
    renderFlowView({ state, events, project: "demo", selected: "", onSelect: () => {} });
    expect(screen.getByTestId("flow-office-main")).toBeInTheDocument();
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
    renderFlowView({ state, events: [...events, assignEvent], project: "demo", selected: "", onSelect });
    fireEvent.click(screen.getByTestId("flow-stint"));
    expect(onSelect).toHaveBeenCalledWith("T1");
  });
});
