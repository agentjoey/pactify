import { render, screen, fireEvent, waitFor } from "@testing-library/react";
import { describe, it, expect, vi, beforeEach } from "vitest";
import type { State, Feature, OrchestrateStatus } from "../lib/types";

const getOrchestrateStatus = vi.fn();
const getParallelOrchestrate = vi.fn();
const runOrchestrate = vi.fn();
const resumeOrchestrate = vi.fn();
const shipFeature = vi.fn();
const getDiff = vi.fn();
vi.mock("../lib/api", () => ({
  getOrchestrateStatus: (...args: unknown[]) => getOrchestrateStatus(...args),
  getParallelOrchestrate: (...args: unknown[]) => getParallelOrchestrate(...args),
  runOrchestrate: (...args: unknown[]) => runOrchestrate(...args),
  resumeOrchestrate: (...args: unknown[]) => resumeOrchestrate(...args),
  shipFeature: (...args: unknown[]) => shipFeature(...args),
  getDiff: (...args: unknown[]) => getDiff(...args),
}));

import { LiveOrchestrate } from "./LiveOrchestrate";

const FULL_ROSTER = [
  { id: "claude", roles: ["orchestrator", "reviewer"] },
  { id: "opencode-worker", roles: ["worker"] },
];

const task = (id: string, status: string, owner = "opencode-worker") => ({
  id, owner, status, reviewer: "claude", spec: "", evidence: "",
});
const st = (features: Feature[]): State => ({ project: "p1", agents: FULL_ROSTER, features, awaiting_count: 0 });
const EMPTY = st([]);

const status = (over: Partial<OrchestrateStatus>): OrchestrateStatus => ({
  feature: "feat-x", task: "t2", seat: "opencode-worker", action: "run_owner", phase: "owner",
  escalated: false, done: false, total: 3, accepted: 1, iter: 2, updated_at: "x", ...over,
});

describe("LiveOrchestrate (lanes redesign)", () => {
  beforeEach(() => {
    getOrchestrateStatus.mockReset();
    getParallelOrchestrate.mockReset();
    runOrchestrate.mockReset();
    resumeOrchestrate.mockReset();
    shipFeature.mockReset();
    getDiff.mockReset();
    getParallelOrchestrate.mockResolvedValue({ present: false });
  });

  function renderLive(props: Partial<React.ComponentProps<typeof LiveOrchestrate>> = {}) {
    return render(
      <LiveOrchestrate project="p1" state={EMPTY} refreshTick={0} author={true} agents={FULL_ROSTER} {...props} />,
    );
  }

  it("renders empty state + Run when nothing has run and there are no features", async () => {
    getOrchestrateStatus.mockResolvedValue({ present: false });
    renderLive();
    await waitFor(() => expect(screen.getByText("orchestrate hasn't run yet")).toBeTruthy());
    expect(screen.getByRole("button", { name: "Run" })).toBeTruthy();
  });

  it("Run is disabled when the roster is incomplete", async () => {
    getOrchestrateStatus.mockResolvedValue({ present: false });
    renderLive({ agents: [{ id: "claude", roles: ["reviewer"] }] });
    await waitFor(() => expect(screen.getByRole("button", { name: "Run" })).toBeTruthy());
    expect((screen.getByRole("button", { name: "Run" }) as HTMLButtonElement).disabled).toBe(true);
  });

  it("Run is disabled in observe mode", async () => {
    getOrchestrateStatus.mockResolvedValue({ present: false });
    renderLive({ author: false });
    await waitFor(() => expect(screen.getByRole("button", { name: "Run" })).toBeTruthy());
    expect((screen.getByRole("button", { name: "Run" }) as HTMLButtonElement).disabled).toBe(true);
  });

  it("renders a feature lane with task pipeline chips from state", async () => {
    getOrchestrateStatus.mockResolvedValue({ present: true, status: status({}) });
    renderLive({
      state: st([{ id: "feat-x", branch: "feat/x", status: "in_progress", tasks: [
        task("t1", "accepted"), task("t2", "in_progress"), task("t3", "assigned"),
      ] }]),
    });
    await waitFor(() => expect(screen.getByTestId("feature-lane")).toBeTruthy());
    expect(screen.getByText("feat-x")).toBeTruthy();
    expect(screen.getByText("feat/x")).toBeTruthy();
    expect(screen.getByText("t1")).toBeTruthy();
    expect(screen.getByText("t2")).toBeTruthy();
    expect(screen.getByText("t3")).toBeTruthy();
    expect(screen.getByText("working")).toBeTruthy();
  });

  it("shows the review gate with reason + Resume/See diff when a gate fails", async () => {
    getOrchestrateStatus.mockResolvedValue({ present: true, status: status({ escalated: true, action: "stuck", reason: "FAIL TestRetryCap" }) });
    resumeOrchestrate.mockResolvedValue({});
    getDiff.mockResolvedValue({ diff: "diff --git a/foo b/foo" });
    const onNotify = vi.fn();
    renderLive({
      onNotify,
      state: st([{ id: "feat-x", branch: "feat/x", status: "in_progress", tasks: [
        task("t1", "accepted"), task("t2", "in_progress"),
      ] }]),
    });
    await waitFor(() => expect(screen.getByTestId("review-gate")).toBeTruthy());
    expect(screen.getByText("Hard test gate failed — human decision required")).toBeTruthy();
    expect(screen.getByText("FAIL TestRetryCap")).toBeTruthy();

    fireEvent.click(screen.getByRole("button", { name: "Resume run ▸" }));
    await waitFor(() => expect(resumeOrchestrate).toHaveBeenCalledWith("p1"));
    await waitFor(() => expect(onNotify).toHaveBeenCalledWith("Orchestrate resumed"));

    fireEvent.click(screen.getByRole("button", { name: "See diff" }));
    await waitFor(() => expect(getDiff).toHaveBeenCalledWith("p1"));
    await waitFor(() => expect(screen.getByText("diff --git a/foo b/foo")).toBeTruthy());
  });

  it("run control shows accepted/total progress", async () => {
    getOrchestrateStatus.mockResolvedValue({ present: true, status: status({}) });
    renderLive({
      state: st([{ id: "feat-x", branch: "feat/x", status: "in_progress", tasks: [
        task("t1", "accepted"), task("t2", "in_progress"), task("t3", "assigned"),
      ] }]),
    });
    await waitFor(() => expect(screen.getAllByText("1 / 3 accepted").length).toBeGreaterThan(0));
    expect(screen.getByText("Orchestrating")).toBeTruthy();
  });

  it("Ship opens the PR form and posts ship when the run is done", async () => {
    getOrchestrateStatus.mockResolvedValue({ present: true, status: status({ done: true, action: "done", task: "", accepted: 2, total: 2 }) });
    shipFeature.mockResolvedValue({ pushed: true, pr_url: "https://github.com/org/repo/pull/42" });
    const onNotify = vi.fn();
    renderLive({
      onNotify,
      state: st([{ id: "feat-x", branch: "feat/x", status: "in_progress", tasks: [task("t1", "accepted"), task("t2", "accepted")] }]),
    });
    await waitFor(() => expect(screen.getByRole("button", { name: "Ship" })).toBeTruthy());
    fireEvent.click(screen.getByRole("button", { name: "Ship" }));
    await waitFor(() => expect(screen.getByText("Ship feature")).toBeTruthy());
    fireEvent.change(screen.getByPlaceholderText("feat-xyz"), { target: { value: "feat-x" } });
    fireEvent.change(screen.getByPlaceholderText("feat: …"), { target: { value: "feat: x" } });
    fireEvent.click(screen.getByRole("button", { name: "Open PR" }));
    await waitFor(() =>
      expect(shipFeature).toHaveBeenCalledWith("p1", { pr: true, head: "feat-x", title: "feat: x", body: "" }),
    );
    await waitFor(() => expect(screen.getByText("https://github.com/org/repo/pull/42")).toBeTruthy());
  });

  it("always renders the event stream pane", async () => {
    getOrchestrateStatus.mockResolvedValue({ present: false });
    renderLive();
    await waitFor(() => expect(screen.getByTestId("event-stream")).toBeTruthy());
  });
});
