import { render, screen, fireEvent } from "@testing-library/react";
import { describe, it, expect, vi, beforeEach } from "vitest";
import { RightRail } from "./RightRail";
import type { State, PactEvent, Task } from "../lib/types";

vi.mock("../lib/api", () => ({
  postVerb: vi.fn(() => Promise.resolve()),
  getStats: vi.fn(() => Promise.resolve({ tasks: [], agents: [] })),
  getAudit: vi.fn(() => Promise.resolve([])),
  fmtDuration: (s: number) => `${s}s`,
}));
import { postVerb } from "../lib/api";

const task = (over: Partial<Task> = {}): Task => ({
  id: "t2-cli",
  owner: "opencode",
  status: "awaiting_review",
  reviewer: "claude-opus",
  spec: ".pact/tasks/t2-cli.md",
  evidence: "go test ./... ok",
  ...over,
});

const baseState = (t: Task = task()): State => ({
  project: "greet",
  agents: [
    { id: "opencode", roles: ["worker"] },
    { id: "claude-opus", roles: ["orchestrator", "reviewer"] },
  ],
  features: [{ id: "greet-cli", branch: "feat/greet-cli", status: "open", tasks: [t] }],
  awaiting_count: 1,
});

const ev = (over: Partial<PactEvent> = {}): PactEvent => ({
  event_id: "e1",
  ts: new Date(Date.now() - 3 * 60_000).toISOString(),
  agent_id: "opencode",
  role: "worker",
  event_type: "checkpoint",
  task_id: "t2-cli",
  feature: "greet-cli",
  payload: {},
  ...over,
});

beforeEach(() => {
  vi.clearAllMocks();
});

describe("RightRail — task detail panel", () => {
  it("renders nothing when no task is selected", () => {
    const { container } = render(
      <RightRail state={baseState()} events={[]} selected="" project="greet" author />,
    );
    expect(container.firstChild).toBeNull();
  });

  it("opens the panel when a task is selected (header, badge, ant chips)", () => {
    render(
      <RightRail state={baseState()} events={[]} selected="t2-cli" project="greet" author />,
    );
    const panel = screen.getByTestId("task-panel");
    expect(panel.textContent).toContain("t2-cli");
    expect(panel.textContent).toContain("greet-cli");
    // status badge
    expect(panel.textContent).toContain("awaiting review");
    // owner→reviewer ant chips render (svg ants have a <title>)
    const titles = Array.from(panel.querySelectorAll("title")).map((t) => t.textContent);
    expect(titles).toContain("opencode");
    expect(titles).toContain("claude-opus");
  });

  it("closes on Esc and on scrim click (deselect called)", () => {
    const onSelect = vi.fn();
    render(
      <RightRail
        state={baseState()}
        events={[]}
        selected="t2-cli"
        project="greet"
        author
        onSelect={onSelect}
      />,
    );
    fireEvent.keyDown(window, { key: "Escape" });
    expect(onSelect).toHaveBeenCalledWith("");

    onSelect.mockClear();
    fireEvent.click(screen.getByTestId("panel-scrim"));
    expect(onSelect).toHaveBeenCalledWith("");
  });

  it("renders a spec path as a mono path line with a hint", () => {
    render(
      <RightRail state={baseState()} events={[]} selected="t2-cli" project="greet" author />,
    );
    const panel = screen.getByTestId("task-panel");
    expect(panel.textContent).toContain(".pact/tasks/t2-cli.md");
    expect(panel.textContent).toContain("task spec file in repo");
  });

  it("renders an inline spec value as plain text (no path hint)", () => {
    const t = task({ spec: "implement greet CLI entry point" });
    render(
      <RightRail state={baseState(t)} events={[]} selected="t2-cli" project="greet" author />,
    );
    const panel = screen.getByTestId("task-panel");
    expect(panel.textContent).toContain("implement greet CLI entry point");
    expect(panel.textContent).not.toContain("task spec file in repo");
  });

  it("filters the timeline to the selected task and shows relative time", () => {
    const events = [
      ev({ event_id: "e1", task_id: "t2-cli", event_type: "checkpoint" }),
      ev({ event_id: "e2", task_id: "OTHER", event_type: "assign" }),
    ];
    render(
      <RightRail state={baseState()} events={events} selected="t2-cli" project="greet" author />,
    );
    const entries = screen.getAllByTestId("panel-timeline-entry");
    expect(entries).toHaveLength(1);
    expect(entries[0].textContent).toContain("checkpoint");
    expect(entries[0].textContent).toContain("3m");
  });

  it("shows Accept/Changes for an author on an awaiting_review task", () => {
    render(
      <RightRail state={baseState()} events={[]} selected="t2-cli" project="greet" author />,
    );
    const panel = screen.getByTestId("task-panel");
    expect(panel.textContent).toContain("Accept");
    fireEvent.click(screen.getByText("✓ Accept"));
    expect(postVerb).toHaveBeenCalledWith("greet", "accept", { task: "t2-cli" });
  });

  it("hides the action row for an observer (author=false)", () => {
    render(
      <RightRail
        state={baseState()}
        events={[]}
        selected="t2-cli"
        project="greet"
        author={false}
      />,
    );
    expect(screen.queryByText("✓ Accept")).toBeNull();
    // merge affordance is also author-gated
    expect(screen.queryByText(/^Merge/)).toBeNull();
  });

  it("hides the action row when the task is not awaiting_review", () => {
    const t = task({ status: "in_progress" });
    render(
      <RightRail state={baseState(t)} events={[]} selected="t2-cli" project="greet" author />,
    );
    expect(screen.queryByText("✓ Accept")).toBeNull();
  });

  it("shows the merge affordance for authors, enabled only when all tasks accepted", () => {
    // Mixed: one awaiting → merge disabled.
    render(
      <RightRail state={baseState()} events={[]} selected="t2-cli" project="greet" author />,
    );
    const merge = screen.getByText(/^Merge greet-cli/) as HTMLButtonElement;
    expect(merge.closest("button")).toBeDisabled();
  });

  it("enables merge when every task in the feature is accepted", () => {
    const t = task({ status: "accepted" });
    render(
      <RightRail state={baseState(t)} events={[]} selected="t2-cli" project="greet" author />,
    );
    const merge = screen.getByText(/^Merge greet-cli/);
    const btn = merge.closest("button") as HTMLButtonElement;
    expect(btn).not.toBeDisabled();
    fireEvent.click(btn);
    expect(postVerb).toHaveBeenCalledWith("greet", "merge", { feature: "greet-cli" });
  });
});
