import { render, screen, fireEvent, within, waitFor } from "@testing-library/react";
import { describe, it, expect, vi, beforeEach } from "vitest";
import type { State, PactEvent, OrchestrateStatusResponse } from "../lib/types";
import type { ProjectStats } from "../lib/api";
import { Dashboard } from "./Dashboard";
import { DataSourceProvider } from "../lib/datasource";

const iso = (secAgo: number) => new Date(Date.now() - secAgo * 1000).toISOString();

const baseState: State = {
  project: "relay-core",
  awaiting_count: 1,
  agents: [
    { id: "claude", roles: ["orchestrator", "reviewer"] },
    { id: "opencode", roles: ["worker"] },
    { id: "gemini", roles: ["worker"] },
  ],
  features: [
    {
      id: "feat-rh",
      branch: "feat/retry-harden",
      status: "in_progress",
      tasks: [
        { id: "t-cfg", owner: "opencode", status: "accepted", reviewer: "claude", spec: "", evidence: "" },
        { id: "t-impl", owner: "opencode", status: "awaiting_review", reviewer: "claude", spec: "", evidence: "FAIL TestRetryCap" },
        { id: "t-harden", owner: "opencode", status: "in_progress", reviewer: "claude", spec: "", evidence: "" },
      ],
    },
    {
      id: "feat-cache",
      branch: "feat/cache-concurrency",
      status: "in_progress",
      tasks: [
        { id: "t-cache", owner: "gemini", status: "in_progress", reviewer: "claude", spec: "", evidence: "" },
        { id: "t-guard", owner: "gemini", status: "accepted", reviewer: "claude", spec: "", evidence: "" },
        { id: "t-warm", owner: "gemini", status: "changes_requested", reviewer: "claude", spec: "", evidence: "" },
      ],
    },
  ],
};

const events: PactEvent[] = [
  { event_id: "j1", ts: iso(600), agent_id: "claude", role: "orchestrator", event_type: "join", task_id: "", feature: "", payload: {} },
  { event_id: "j2", ts: iso(590), agent_id: "opencode", role: "worker", event_type: "join", task_id: "", feature: "", payload: {} },
  { event_id: "j3", ts: iso(580), agent_id: "gemini", role: "worker", event_type: "join", task_id: "", feature: "", payload: {} },
  { event_id: "a1", ts: iso(500), agent_id: "claude", role: "orchestrator", event_type: "assign", task_id: "t-cfg", feature: "feat-rh", payload: { owner: "opencode", reviewer: "claude" } },
  { event_id: "c1", ts: iso(480), agent_id: "opencode", role: "worker", event_type: "checkpoint", task_id: "t-cfg", feature: "feat-rh", payload: {} },
  { event_id: "a2", ts: iso(450), agent_id: "claude", role: "orchestrator", event_type: "assign", task_id: "t-impl", feature: "feat-rh", payload: { owner: "opencode", reviewer: "claude" } },
  { event_id: "c2", ts: iso(120), agent_id: "opencode", role: "worker", event_type: "checkpoint", task_id: "t-impl", feature: "feat-rh", payload: {} },
  { event_id: "a3", ts: iso(300), agent_id: "claude", role: "orchestrator", event_type: "assign", task_id: "t-harden", feature: "feat-rh", payload: { owner: "opencode", reviewer: "claude" } },
  { event_id: "a4", ts: iso(400), agent_id: "claude", role: "orchestrator", event_type: "assign", task_id: "t-cache", feature: "feat-cache", payload: { owner: "gemini", reviewer: "claude" } },
  { event_id: "a5", ts: iso(380), agent_id: "claude", role: "orchestrator", event_type: "assign", task_id: "t-guard", feature: "feat-cache", payload: { owner: "gemini", reviewer: "claude" } },
  { event_id: "c3", ts: iso(360), agent_id: "gemini", role: "worker", event_type: "checkpoint", task_id: "t-guard", feature: "feat-cache", payload: {} },
  { event_id: "ac1", ts: iso(350), agent_id: "claude", role: "reviewer", event_type: "accept", task_id: "t-guard", feature: "feat-cache", payload: {} },
  { event_id: "a6", ts: iso(340), agent_id: "claude", role: "orchestrator", event_type: "assign", task_id: "t-warm", feature: "feat-cache", payload: { owner: "gemini", reviewer: "claude" } },
  { event_id: "ch1", ts: iso(800), agent_id: "claude", role: "reviewer", event_type: "changes_requested", task_id: "t-warm", feature: "feat-cache", payload: {} },
  { event_id: "m1", ts: iso(1300), agent_id: "claude", role: "orchestrator", event_type: "merge", task_id: "", feature: "feat-init", payload: {} },
];

const stats: ProjectStats = {
  tasks: [
    { task_id: "t-cfg", feature: "feat-rh", owner: "opencode", reviewer: "claude", status: "accepted", duration_sec: 120, added: 0, deleted: 0, tokens: 7100 },
    { task_id: "t-impl", feature: "feat-rh", owner: "opencode", reviewer: "claude", status: "awaiting_review", duration_sec: 300, added: 0, deleted: 0, tokens: 9200 },
    { task_id: "t-harden", feature: "feat-rh", owner: "opencode", reviewer: "claude", status: "in_progress", duration_sec: 180, added: 0, deleted: 0, tokens: 12400 },
    { task_id: "t-cache", feature: "feat-cache", owner: "gemini", reviewer: "claude", status: "in_progress", duration_sec: 200, added: 0, deleted: 0, tokens: 5800 },
    { task_id: "t-guard", feature: "feat-cache", owner: "gemini", reviewer: "claude", status: "accepted", duration_sec: 240, added: 0, deleted: 0, tokens: 8600 },
    { task_id: "t-warm", feature: "feat-cache", owner: "gemini", reviewer: "claude", status: "changes_requested", duration_sec: 0, added: 0, deleted: 0, tokens: 9000 },
  ],
  agents: [
    { seat: "claude", tasks: 6, duration_sec: 1000, added: 0, deleted: 0, tokens: 52100, accepted: 2, reworked: 1 },
    { seat: "opencode", tasks: 3, duration_sec: 600, added: 0, deleted: 0, tokens: 28700, accepted: 1, reworked: 0 },
    { seat: "gemini", tasks: 3, duration_sec: 440, added: 0, deleted: 0, tokens: 23400, accepted: 1, reworked: 1 },
  ],
};

function makeSource(overrides: Record<string, unknown> = {}) {
  return {
    capabilities: { canWrite: true, canOrchestrate: true, multiMachine: false, cockpit: true },
    listProjects: vi.fn(),
    getState: vi.fn(),
    getStats: vi.fn().mockResolvedValue(stats),
    subscribe: vi.fn(),
    verb: vi.fn().mockResolvedValue(undefined),
    runOrchestrate: vi.fn().mockResolvedValue({ status_url: "/status" }),
    stopOrchestrate: vi.fn().mockResolvedValue(undefined),
    ...overrides,
  };
}

function renderDashboard(
  ui: React.ReactElement,
  sourceOverrides?: Partial<ReturnType<typeof makeSource>>,
) {
  return render(<DataSourceProvider source={makeSource(sourceOverrides)}>{ui}</DataSourceProvider>);
}

beforeEach(() => {
  localStorage.clear();
});

describe("Dashboard", () => {
  it("renders the context bar with project selector, subtitle and new-task button", () => {
    renderDashboard(
      <Dashboard
        project="relay-core"
        projects={[{ id: "relay-core", name: "relay-core", path: "/x", project: "relay-core", feature_count: 2, awaiting_count: 1 }]}
        state={baseState}
        events={events}
        author
        onSelectProject={() => {}}
        onRenameProject={() => {}}
        onDeleteProject={() => {}}
        onAddProject={() => {}}
        onOpenBoard={() => {}}
        onOpenCockpit={() => {}}
      />,
    );
    expect(screen.getByTestId("dashboard-context-bar")).toBeInTheDocument();
    expect(screen.getByTestId("project-menu-trigger")).toHaveTextContent("relay-core");
    expect(screen.getByText("main · 2 features · 3 seats")).toBeInTheDocument();
    expect(screen.getByTestId("dashboard-new-task")).toHaveTextContent("New task");
  });

  it("renders the four KPI cards", async () => {
    renderDashboard(
      <Dashboard
        project="relay-core"
        projects={[]}
        state={baseState}
        events={events}
        orchestrateStatus={{ present: true, status: { done: false, escalated: false, total: 6, accepted: 2, iter: 7 } as unknown as OrchestrateStatusResponse["status"] }}
        onSelectProject={() => {}}
        onRenameProject={() => {}}
        onDeleteProject={() => {}}
        onAddProject={() => {}}
        onOpenBoard={() => {}}
        onOpenCockpit={() => {}}
      />,
    );
    expect(screen.getByTestId("dashboard-kpi-strip")).toBeInTheDocument();
    expect(screen.getByTestId("kpi-active-run")).toHaveTextContent("Active run");
    expect(screen.getByTestId("kpi-active-run-value")).toHaveTextContent("1");
    expect(screen.getByTestId("kpi-active-run")).toHaveTextContent("orchestrating");
    expect(screen.getByTestId("kpi-awaiting-review")).toHaveTextContent("Awaiting review");
    expect(screen.getByTestId("kpi-awaiting-review-value")).toHaveTextContent("1");
    await waitFor(() => expect(screen.getByTestId("kpi-tokens-today-value")).toHaveTextContent("52.1k"));
    expect(screen.getByTestId("kpi-shipped-7d-value")).toHaveTextContent("1");
  });

  it("renders run control with progress bar when running", () => {
    renderDashboard(
      <Dashboard
        project="relay-core"
        projects={[]}
        state={baseState}
        events={events}
        orchestrateStatus={{ present: true, status: { done: false, escalated: false, total: 6, accepted: 2, iter: 7 } as unknown as OrchestrateStatusResponse["status"] }}
        onSelectProject={() => {}}
        onRenameProject={() => {}}
        onDeleteProject={() => {}}
        onAddProject={() => {}}
        onOpenBoard={() => {}}
        onOpenCockpit={() => {}}
      />,
    );
    expect(screen.getByText("Orchestrating")).toBeInTheDocument();
    expect(screen.getByTestId("run-control-stop")).toBeInTheDocument();
    expect(screen.getByTestId("run-control-progress")).toBeInTheDocument();
  });

  it("renders Run button when idle", () => {
    renderDashboard(
      <Dashboard
        project="relay-core"
        projects={[]}
        state={baseState}
        events={events}
        orchestrateStatus={{ present: false }}
        author
        onSelectProject={() => {}}
        onRenameProject={() => {}}
        onDeleteProject={() => {}}
        onAddProject={() => {}}
        onOpenBoard={() => {}}
        onOpenCockpit={() => {}}
      />,
    );
    expect(screen.getByTestId("run-control-run")).toBeInTheDocument();
  });

  it("renders feature lanes with mini-pipeline and review gate", () => {
    renderDashboard(
      <Dashboard
        project="relay-core"
        projects={[]}
        state={baseState}
        events={events}
        author
        onSelectProject={() => {}}
        onRenameProject={() => {}}
        onDeleteProject={() => {}}
        onAddProject={() => {}}
        onOpenBoard={() => {}}
        onOpenCockpit={() => {}}
      />,
    );
    expect(screen.getByTestId("feature-lane-feat-rh")).toBeInTheDocument();
    expect(screen.getByTestId("feature-lane-feat-cache")).toBeInTheDocument();
    expect(screen.getByTestId("feature-lane-pipeline-feat-rh")).toBeInTheDocument();
    expect(screen.getByTestId("review-gate")).toBeInTheDocument();
  });

  it("opens Board on See diff and Cockpit on Take over", () => {
    const onOpenBoard = vi.fn();
    const onOpenCockpit = vi.fn();
    renderDashboard(
      <Dashboard
        project="relay-core"
        projects={[]}
        state={baseState}
        events={events}
        author
        onSelectProject={() => {}}
        onRenameProject={() => {}}
        onDeleteProject={() => {}}
        onAddProject={() => {}}
        onOpenBoard={onOpenBoard}
        onOpenCockpit={onOpenCockpit}
      />,
    );
    fireEvent.click(screen.getByTestId("review-gate-diff"));
    expect(onOpenBoard).toHaveBeenCalledWith("t-impl");
    fireEvent.click(screen.getByTestId("review-gate-takeover"));
    expect(onOpenCockpit).toHaveBeenCalledWith("opencode");
  });

  it("renders the seats roster with status", () => {
    renderDashboard(
      <Dashboard
        project="relay-core"
        projects={[]}
        state={baseState}
        events={events}
        onSelectProject={() => {}}
        onRenameProject={() => {}}
        onDeleteProject={() => {}}
        onAddProject={() => {}}
        onOpenBoard={() => {}}
        onOpenCockpit={() => {}}
      />,
    );
    expect(screen.getByTestId("seat-row-claude")).toBeInTheDocument();
    expect(screen.getByTestId("seat-row-opencode")).toBeInTheDocument();
    expect(screen.getByText("3 seated")).toBeInTheDocument();
    const opencodeRow = screen.getByTestId("seat-row-opencode");
    expect(within(opencodeRow).getByText("working")).toBeInTheDocument();
  });

  it("renders the activity feed with rows", () => {
    renderDashboard(
      <Dashboard
        project="relay-core"
        projects={[]}
        state={baseState}
        events={events}
        onSelectProject={() => {}}
        onRenameProject={() => {}}
        onDeleteProject={() => {}}
        onAddProject={() => {}}
        onOpenBoard={() => {}}
        onOpenCockpit={() => {}}
      />,
    );
    expect(screen.getByText("Activity")).toBeInTheDocument();
    expect(screen.getAllByTestId("activity-row").length).toBeGreaterThan(0);
  });

  it("disables review actions when read-only", () => {
    renderDashboard(
      <Dashboard
        project="relay-core"
        projects={[]}
        state={baseState}
        events={events}
        author
        onSelectProject={() => {}}
        onRenameProject={() => {}}
        onDeleteProject={() => {}}
        onAddProject={() => {}}
        onOpenBoard={() => {}}
        onOpenCockpit={() => {}}
      />,
      { capabilities: { canWrite: false, canOrchestrate: true, multiMachine: false, cockpit: true } },
    );
    expect(screen.getByTestId("review-gate-accept")).toBeDisabled();
    expect(screen.getByTestId("review-gate-changes")).toBeDisabled();
  });
});
