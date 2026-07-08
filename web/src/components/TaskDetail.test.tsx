import { render, screen, fireEvent, waitFor } from "@testing-library/react";
import { describe, it, expect, vi, beforeEach } from "vitest";
import { TaskDetail } from "./TaskDetail";
import { DataSourceProvider } from "../lib/datasource";
import type { DataSource } from "../lib/datasource";
import type { PactEventDetail } from "../lib/types";

function makeSource(
  getEvents?: DataSource["getEvents"],
  caps: DataSource["capabilities"] = { canWrite: false, canOrchestrate: false, multiMachine: true, cockpit: false },
): DataSource {
  return {
    capabilities: caps,
    listProjects: vi.fn(),
    getState: vi.fn(),
    getStats: vi.fn(),
    subscribe: vi.fn(),
    getEvents,
  } as unknown as DataSource;
}

function renderDetail(ui: React.ReactElement, source: DataSource) {
  return render(<DataSourceProvider source={source}>{ui}</DataSourceProvider>);
}

const detail = (over: Partial<PactEventDetail> = {}): PactEventDetail => ({
  seq: 1,
  eventType: "checkpoint",
  task: "t1",
  feature: "f1",
  ts: 1_700_000_000_000,
  body: {
    event_id: "e1",
    ts: "2023-11-14T22:13:20Z",
    agent_id: "alice",
    role: "worker",
    event_type: "checkpoint",
    task_id: "t1",
    feature: "f1",
    payload: { evidence: "go test ./... ok" },
  },
  ...over,
});

beforeEach(() => {
  vi.clearAllMocks();
});

describe("TaskDetail — hosted-mode event history", () => {
  it("gracefully degrades to null when source has no getEvents", () => {
    const source = makeSource(undefined, { canWrite: true, canOrchestrate: true, multiMachine: false, cockpit: true });
    const { container } = renderDetail(
      <TaskDetail project="p1" taskId="t1" onClose={() => {}} />,
      source,
    );
    expect(container.firstChild).toBeNull();
  });

  // Regression: the parent (App) keeps TaskDetail mounted in hosted mode and only
  // toggles taskId ("" → selected) when a card is clicked. If the empty-taskId
  // guard early-returns before the hooks, the hook count changes between renders
  // and React tears down the whole tree ("rendered more hooks than during the
  // previous render") — the reported "clicking a card blanks the dashboard" bug.
  it("survives taskId toggling from empty to set without a hooks-count crash", async () => {
    const source = makeSource(vi.fn().mockResolvedValue([detail()]));
    const { rerender } = renderDetail(<TaskDetail project="p1" taskId="" />, source);
    // No panel while nothing is selected.
    expect(screen.queryByTestId("task-detail-panel")).toBeNull();
    // Selecting a card must not throw (would blank the app if hooks mismatch).
    rerender(
      <DataSourceProvider source={source}>
        <TaskDetail project="p1" taskId="t1" />
      </DataSourceProvider>,
    );
    await waitFor(() => expect(screen.getByTestId("task-detail-panel")).toBeInTheDocument());
  });

  it("shows loading state then renders decrypted events", async () => {
    const events = [detail({ seq: 1, eventType: "checkpoint" })];
    const source = makeSource(vi.fn().mockResolvedValue(events));
    renderDetail(<TaskDetail project="p1" taskId="t1" />, source);

    expect(screen.getByTestId("task-detail-loading")).toBeInTheDocument();
    await waitFor(() => expect(screen.queryByTestId("task-detail-loading")).toBeNull());
    expect(screen.getByTestId("task-detail-panel")).toBeInTheDocument();
    const entries = screen.getAllByTestId("task-detail-event");
    expect(entries).toHaveLength(1);
    expect(entries[0].textContent).toContain("checkpoint");
    expect(entries[0].textContent).toContain("go test ./... ok");
  });

  it("filters events to the selected task and sorts newest-first", async () => {
    const events = [
      detail({ seq: 1, task: "t1", ts: 1_700_000_000_000, body: { ...detail().body, task_id: "t1" } }),
      detail({
        seq: 2,
        task: "t2",
        ts: 1_700_000_100_000,
        body: { ...detail().body, task_id: "t2", payload: { evidence: "other task" } },
      }),
      detail({
        seq: 3,
        task: "t1",
        ts: 1_700_000_200_000,
        eventType: "accept",
        body: {
          ...detail().body,
          event_id: "e3",
          event_type: "accept",
          task_id: "t1",
          payload: {},
        },
      }),
    ];
    const source = makeSource(vi.fn().mockResolvedValue(events));
    renderDetail(<TaskDetail project="p1" taskId="t1" />, source);

    await waitFor(() => expect(screen.getAllByTestId("task-detail-event")).toHaveLength(2));
    const entries = screen.getAllByTestId("task-detail-event");
    // Newest first: accept (seq 3) then checkpoint (seq 1).
    expect(entries[0].textContent).toContain("accept");
    expect(entries[1].textContent).toContain("checkpoint");
  });

  it("shows empty state when no events match the task", async () => {
    const source = makeSource(vi.fn().mockResolvedValue([]));
    renderDetail(<TaskDetail project="p1" taskId="t1" />, source);
    await waitFor(() => expect(screen.getByTestId("task-detail-empty")).toBeInTheDocument());
  });

  it("shows error state when getEvents rejects", async () => {
    const source = makeSource(vi.fn().mockRejectedValue(new Error("relay down")));
    renderDetail(<TaskDetail project="p1" taskId="t1" />, source);
    await waitFor(() => expect(screen.getByTestId("task-detail-error")).toHaveTextContent("relay down"));
  });

  it("closes on scrim click and Escape", async () => {
    const onClose = vi.fn();
    const source = makeSource(vi.fn().mockResolvedValue([]));
    renderDetail(<TaskDetail project="p1" taskId="t1" onClose={onClose} />, source);
    await waitFor(() => expect(screen.getByTestId("task-detail-empty")).toBeInTheDocument());

    fireEvent.click(screen.getByTestId("task-detail-scrim"));
    expect(onClose).toHaveBeenCalled();

    onClose.mockClear();
    fireEvent.keyDown(window, { key: "Escape" });
    expect(onClose).toHaveBeenCalled();
  });

  it("renders assign fields (owner/reviewer/spec)", async () => {
    const events = [
      detail({
        seq: 1,
        eventType: "assign",
        body: {
          ...detail().body,
          event_type: "assign",
          payload: { owner: "alice", reviewer: "bob", spec: "spec.md", branch: "feat-f1" },
        },
      }),
    ];
    const source = makeSource(vi.fn().mockResolvedValue(events));
    renderDetail(<TaskDetail project="p1" taskId="t1" />, source);
    await waitFor(() => expect(screen.getAllByTestId("task-detail-event")).toHaveLength(1));
    const entry = screen.getByTestId("task-detail-event");
    expect(entry.textContent).toContain("alice");
    expect(entry.textContent).toContain("bob");
    expect(entry.textContent).toContain("spec.md");
  });

  it("renders changes_requested reason", async () => {
    const events = [
      detail({
        seq: 1,
        eventType: "changes_requested",
        body: {
          ...detail().body,
          event_type: "changes_requested",
          payload: { reason: "needs tests" },
        },
      }),
    ];
    const source = makeSource(vi.fn().mockResolvedValue(events));
    renderDetail(<TaskDetail project="p1" taskId="t1" />, source);
    await waitFor(() => expect(screen.getAllByTestId("task-detail-event")).toHaveLength(1));
    expect(screen.getByTestId("task-detail-event").textContent).toContain("needs tests");
  });
});
