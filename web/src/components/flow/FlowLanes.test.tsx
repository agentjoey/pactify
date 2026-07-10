import { describe, it, expect, vi } from "vitest";
import { render, screen, fireEvent, waitFor } from "@testing-library/react";
import { FlowLanes } from "./FlowLanes";
import type { FlowModel, FlowStint, FlowArrow, FlowGap } from "../../lib/flowderive";
import type { State } from "../../lib/types";
import type { DataSource } from "../../lib/datasource";
import { DataSourceProvider } from "../../lib/datasource";

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

const defaultState: State = {
  project: "demo",
  awaiting_count: 0,
  agents,
  features: [],
};

function renderLanes(
  props: Omit<React.ComponentProps<typeof FlowLanes>, "state" | "project">,
  source?: DataSource,
) {
  return render(
    <DataSourceProvider source={source ?? mockSource()}>
      <FlowLanes {...props} state={defaultState} project="demo" />
    </DataSourceProvider>,
  );
}

function mockSource(statsReturn?: unknown): DataSource {
  return {
    capabilities: {
      canWrite: true,
      canOrchestrate: false,
      multiMachine: false,
      cockpit: false,
    },
    listProjects: vi.fn(),
    getState: vi.fn(),
    getStats: vi.fn().mockResolvedValue(statsReturn ?? { tasks: [], agents: [] }),
    subscribe: vi.fn(() => () => {}),
  } as unknown as DataSource;
}

describe("FlowLanes", () => {
  it("renders the empty state when the model has no activity", () => {
    renderLanes({ model: makeModel(), agents, selected: "", onSelect: () => {} });
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
    renderLanes({ model, agents, selected: "", onSelect: () => {} });
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
    renderLanes({ model, agents, selected: "", onSelect: () => {} });
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
    renderLanes({ model, agents, selected: "", onSelect });
    fireEvent.click(screen.getByTestId("flow-stint"));
    expect(onSelect).toHaveBeenCalledWith("T1");
  });

  it("shows idle chips for agents with no open stint and working for open stints when expanded", () => {
    const model = makeModel({
      lanes: [
        { id: "alice", firstT: 0 },
        { id: "bob", firstT: 0 },
      ],
      stints: [{ agent: "bob", task: "T1", kind: "work", t0: 0, t1: null }],
      tMin: 0,
      tMax: 100,
      x: (t: number) => t / 100,
    });
    renderLanes({ model, agents, selected: "", onSelect: () => {} });
    // alice has no activity and is folded.
    expect(screen.getByText("1 idle seats")).toBeInTheDocument();
    expect(screen.getByText("working")).toBeInTheDocument();
    fireEvent.click(screen.getByText("1 idle seats"));
    expect(screen.getByText("idle")).toBeInTheDocument();
  });

  it("skips ticks that fall inside compressed gaps", () => {
    // Times in minutes so HH:MM labels are distinct. Gap [64m,104m]:
    // working=160m, scale=0.98/160m → gap occupies x∈(0.392,0.412),
    // so the even tick at x=0.4 lands inside it and must be skipped.
    const M = 60_000;
    const gaps: FlowGap[] = [{ t0: 64 * M, t1: 104 * M }];
    const scale = 0.98 / (160 * M);
    const model = makeModel({
      lanes: [{ id: "bob", firstT: 0 }],
      stints: [{ agent: "bob", task: "T1", kind: "work", t0: 0, t1: 200 * M }],
      gaps,
      tMin: 0,
      tMax: 200 * M,
      x: (t: number) => {
        if (t <= 64 * M) return t * scale;
        if (t >= 104 * M) return 0.412 + (t - 104 * M) * scale;
        return 0.392 + ((t - 64 * M) / (40 * M)) * 0.02;
      },
    });
    const { container } = renderLanes({ model, agents, selected: "", onSelect: () => {} });
    const tickTexts = Array.from(container.querySelectorAll("text")).filter(
      (el) => el.getAttribute("y") === "18" && !el.querySelector("title"),
    );
    // 6 even x positions; the one at x=0.4 lands inside the gap and is skipped.
    expect(tickTexts.length).toBe(5);
    const labels = tickTexts.map((el) => el.textContent);
    expect(new Set(labels).size).toBe(labels.length);
  });

  it("renders a title with task, kind, start/end and duration for each stint", () => {
    const stints: FlowStint[] = [
      { agent: "bob", task: "T1", kind: "work", t0: 0, t1: 60 * 60 * 1000 },
    ];
    const model = makeModel({
      lanes: [{ id: "bob", firstT: 0 }],
      stints,
      tMin: 0,
      tMax: 60 * 60 * 1000,
      x: (t: number) => t / (60 * 60 * 1000),
    });
    const { container } = renderLanes({ model, agents, selected: "", onSelect: () => {} });
    const title = container.querySelector('[data-testid="flow-stint"] title');
    expect(title).not.toBeNull();
    const text = title?.textContent ?? "";
    expect(text).toContain("T1");
    expect(text).toContain("work");
    expect(text).toContain("1h");
  });

  it("renders a title showing the real idle duration on gap markers", () => {
    const gaps: FlowGap[] = [{ t0: 0, t1: 62 * 60 * 1000 }];
    const model = makeModel({
      lanes: [{ id: "bob", firstT: 0 }],
      gaps,
      tMin: 0,
      tMax: 62 * 60 * 1000,
      x: () => 0,
    });
    const { container } = renderLanes({ model, agents, selected: "", onSelect: () => {} });
    const gapTitle = container.querySelector("svg > g title");
    expect(gapTitle).not.toBeNull();
    expect(gapTitle?.textContent).toContain("idle (compressed)");
    expect(gapTitle?.textContent).toContain("1h02m");
  });

  it("switches zoom and scales the canvas width", () => {
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
    const { container } = renderLanes({ model, agents, selected: "", onSelect: () => {} });
    const svg = container.querySelector("svg");
    expect(svg).toHaveAttribute("width", "900");
    fireEvent.click(screen.getByText("×2"));
    expect(svg).toHaveAttribute("width", "1800");
    fireEvent.click(screen.getByText("×4"));
    expect(svg).toHaveAttribute("width", "3600");
  });

  it("renders idle lanes normally when every lane is idle", () => {
    const model = makeModel({
      lanes: [
        { id: "alice", firstT: 0 },
        { id: "bob", firstT: 0 },
      ],
      tMin: 0,
      tMax: 100,
      x: (t: number) => t / 100,
    });
    renderLanes({ model, agents, selected: "", onSelect: () => {} });
    expect(screen.queryByText(/idle seats/)).not.toBeInTheDocument();
    expect(screen.getByText("alice")).toBeInTheDocument();
    expect(screen.getByText("bob")).toBeInTheDocument();
  });

  it("folds idle lanes into a summary that can be expanded", () => {
    const model = makeModel({
      lanes: [
        { id: "alice", firstT: 0 },
        { id: "bob", firstT: 0 },
        { id: "carol", firstT: 0 },
      ],
      stints: [{ agent: "bob", task: "T1", kind: "work", t0: 0, t1: 100 }],
      tMin: 0,
      tMax: 100,
      x: (t: number) => t / 100,
    });
    const threeAgents: State["agents"] = [
      { id: "alice", roles: ["orchestrator"] },
      { id: "bob", roles: ["worker"] },
      { id: "carol", roles: ["worker"] },
    ];
    renderLanes({ model, agents: threeAgents, selected: "", onSelect: () => {} });
    expect(screen.getByText("2 idle seats")).toBeInTheDocument();
    expect(screen.queryByText("carol")).not.toBeInTheDocument();
    fireEvent.click(screen.getByText("2 idle seats"));
    expect(screen.getByText("carol")).toBeInTheDocument();
  });

  it("shows a blocked chip when the current task is blocked", () => {
    const model = makeModel({
      lanes: [{ id: "bob", firstT: 0 }],
      stints: [{ agent: "bob", task: "T2", kind: "work", t0: 0, t1: null }],
      tMin: 0,
      tMax: 100,
      x: (t: number) => t / 100,
    });
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
    render(
      <DataSourceProvider source={mockSource()}>
        <FlowLanes model={model} agents={state.agents} selected="" onSelect={() => {}} state={state} project="demo" />
      </DataSourceProvider>,
    );
    expect(screen.getByText(/blocked · 等 T1/)).toBeInTheDocument();
  });

  it("opens and closes the agent side card when clicking a lane row", async () => {
    const model = makeModel({
      lanes: [{ id: "bob", firstT: 0 }],
      stints: [{ agent: "bob", task: "T1", kind: "work", t0: 0, t1: null }],
      tMin: 0,
      tMax: 100,
      x: (t: number) => t / 100,
    });
    const source = mockSource({
      tasks: [],
      agents: [
        {
          seat: "bob",
          tasks: 3,
          accepted: 2,
          reworked: 1,
          tokens: 1200,
          added: 100,
          deleted: 20,
          duration_sec: 3600,
        },
      ],
    });
    render(
      <DataSourceProvider source={source}>
        <FlowLanes model={model} agents={agents} selected="" onSelect={() => {}} state={defaultState} project="demo" />
      </DataSourceProvider>,
    );
    const row = screen.getByTestId("flow-lane-row");
    fireEvent.click(row);
    await waitFor(() => {
      expect(screen.getByTestId("flow-agent-card")).toBeInTheDocument();
    });
    const card = screen.getByTestId("flow-agent-card");
    expect(card).toHaveTextContent("bob");
    expect(card).toHaveTextContent("3");
    expect(card).toHaveTextContent("2");
    expect(card).toHaveTextContent("1");
    expect(card).toHaveTextContent("1200");
    expect(card).toHaveTextContent("+80");
    expect(card).toHaveTextContent("1h");

    fireEvent.click(row);
    expect(screen.queryByTestId("flow-agent-card")).not.toBeInTheDocument();
  });

  it("shows stats unavailable when getStats fails", async () => {
    const model = makeModel({
      lanes: [{ id: "bob", firstT: 0 }],
      stints: [{ agent: "bob", task: "T1", kind: "work", t0: 0, t1: null }],
      tMin: 0,
      tMax: 100,
      x: (t: number) => t / 100,
    });
    const source = mockSource();
    source.getStats = vi.fn().mockRejectedValue(new Error("boom"));
    render(
      <DataSourceProvider source={source}>
        <FlowLanes model={model} agents={agents} selected="" onSelect={() => {}} state={defaultState} project="demo" />
      </DataSourceProvider>,
    );
    fireEvent.click(screen.getByTestId("flow-lane-row"));
    await waitFor(() => {
      expect(screen.getByText("stats unavailable")).toBeInTheDocument();
    });
  });

  it("closes the side card when clicking the close button", async () => {
    const model = makeModel({
      lanes: [{ id: "bob", firstT: 0 }],
      stints: [{ agent: "bob", task: "T1", kind: "work", t0: 0, t1: null }],
      tMin: 0,
      tMax: 100,
      x: (t: number) => t / 100,
    });
    render(
      <DataSourceProvider source={mockSource()}>
        <FlowLanes model={model} agents={agents} selected="" onSelect={() => {}} state={defaultState} project="demo" />
      </DataSourceProvider>,
    );
    fireEvent.click(screen.getByTestId("flow-lane-row"));
    await waitFor(() => {
      expect(screen.getByTestId("flow-agent-card")).toBeInTheDocument();
    });
    fireEvent.click(screen.getByTestId("flow-agent-card-close"));
    expect(screen.queryByTestId("flow-agent-card")).not.toBeInTheDocument();
  });
});
