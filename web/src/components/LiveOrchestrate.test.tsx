import { render, screen, fireEvent, waitFor } from "@testing-library/react";
import { describe, it, expect, vi, beforeEach } from "vitest";

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

describe("LiveOrchestrate panel", () => {
  beforeEach(() => {
    getOrchestrateStatus.mockReset();
    getParallelOrchestrate.mockReset();
    runOrchestrate.mockReset();
    resumeOrchestrate.mockReset();
    shipFeature.mockReset();
    getDiff.mockReset();
    // Default: no parallel run, so the serial fallback renders.
    getParallelOrchestrate.mockResolvedValue({ present: false });
  });

  function renderLive(props: Partial<React.ComponentProps<typeof LiveOrchestrate>> = {}) {
    return render(
      <LiveOrchestrate
        project="p1"
        refreshTick={0}
        author={true}
        agents={FULL_ROSTER}
        {...props}
      />,
    );
  }

  it("renders empty state when not present", async () => {
    getOrchestrateStatus.mockResolvedValue({ present: false });
    renderLive();
    await waitFor(() => {
      expect(screen.getByText("orchestrate hasn't run yet")).toBeTruthy();
    });
  });

  it("renders escalated banner when escalated", async () => {
    getOrchestrateStatus.mockResolvedValue({
      present: true,
      status: {
        feature: "feat-x",
        task: "t1",
        seat: "opencode",
        action: "stuck",
        phase: "owner",
        escalated: true,
        reason: "hit rework limit",
        done: false,
        total: 5,
        accepted: 2,
        iter: 3,
        updated_at: "2026-06-13T10:00:00Z",
      },
    });
    renderLive();
    await waitFor(() => {
      expect(screen.getByText("Orchestration escalated — needs manual intervention")).toBeTruthy();
      expect(screen.getByText("hit rework limit")).toBeTruthy();
    });
  });

  it("renders running state with task/seat/phase/progress", async () => {
    getOrchestrateStatus.mockResolvedValue({
      present: true,
      status: {
        feature: "feat-x",
        task: "t2",
        seat: "gemini-worker",
        action: "run_owner",
        phase: "review",
        escalated: false,
        done: false,
        total: 5,
        accepted: 3,
        iter: 2,
        updated_at: "2026-06-13T10:30:00Z",
      },
    });
    renderLive();
    await waitFor(() => {
      expect(screen.getByText("feat-x")).toBeTruthy();
      expect(screen.getByText("t2")).toBeTruthy();
      expect(screen.getByText("gemini-worker")).toBeTruthy();
      expect(screen.getByText("run_owner")).toBeTruthy();
      expect(screen.getByText("review")).toBeTruthy();
      expect(screen.getByText("3/5")).toBeTruthy();
      expect(screen.getByText("2")).toBeTruthy();
    });
  });

  it("renders done state", async () => {
    getOrchestrateStatus.mockResolvedValue({
      present: true,
      status: {
        feature: "feat-x",
        task: "t3",
        seat: "opencode",
        action: "done",
        phase: "",
        escalated: false,
        done: true,
        total: 5,
        accepted: 5,
        iter: 5,
        updated_at: "2026-06-13T11:00:00Z",
      },
    });
    renderLive();
    await waitFor(() => {
      expect(screen.getByText("Wrapped up / all delivered")).toBeTruthy();
    });
  });

  it("renders the parallel grid (one card per feature) when a parallel run is present", async () => {
    getOrchestrateStatus.mockResolvedValue({ present: false });
    getParallelOrchestrate.mockResolvedValue({
      present: true,
      features: [
        { feature: "fb", task: "b1", seat: "opencode", action: "run_owner", phase: "owner working", escalated: false, done: false, total: 2, accepted: 0, iter: 0, updated_at: "x" },
        { feature: "fa", task: "", seat: "", action: "done", phase: "done", escalated: false, done: true, total: 1, accepted: 1, iter: 3, updated_at: "x" },
      ],
    });
    renderLive();
    await waitFor(() => {
      expect(screen.getByTestId("parallel-panel")).toBeTruthy();
    });
    expect(screen.getAllByTestId("parallel-feature")).toHaveLength(2);
    expect(screen.getByText("2 features")).toBeTruthy();
    // both feature ids shown; serial single-status panel is NOT rendered
    expect(screen.getByText("fa")).toBeTruthy();
    expect(screen.getByText("fb")).toBeTruthy();
    expect(screen.queryByText("orchestrate hasn't run yet")).toBeNull();
  });

  it("escalated feature in the parallel grid shows its reason", async () => {
    getOrchestrateStatus.mockResolvedValue({ present: false });
    getParallelOrchestrate.mockResolvedValue({
      present: true,
      features: [
        { feature: "fx", task: "t1", seat: "w", action: "stuck", phase: "stuck", escalated: true, reason: "gate failed", done: false, total: 1, accepted: 0, iter: 1, updated_at: "x" },
      ],
    });
    renderLive();
    await waitFor(() => {
      expect(screen.getByText("gate failed")).toBeTruthy();
    });
  });

  it("Run button posts to orchestrate/run", async () => {
    getOrchestrateStatus.mockResolvedValue({ present: false });
    runOrchestrate.mockResolvedValue({ status_url: "/api/projects/p1/orchestrate/status" });
    const onNotify = vi.fn();
    renderLive({ onNotify });
    await waitFor(() => expect(screen.getByRole("button", { name: "Run" })).toBeTruthy());
    fireEvent.click(screen.getByRole("button", { name: "Run" }));
    await waitFor(() => expect(runOrchestrate).toHaveBeenCalledWith("p1"));
    await waitFor(() => expect(onNotify).toHaveBeenCalledWith("Orchestrate run started"));
  });

  it("Run button is disabled when roster is incomplete", async () => {
    getOrchestrateStatus.mockResolvedValue({ present: false });
    renderLive({ agents: [{ id: "claude", roles: ["reviewer"] }] });
    await waitFor(() => expect(screen.getByRole("button", { name: "Run" })).toBeTruthy());
    const btn = screen.getByRole("button", { name: "Run" }) as HTMLButtonElement;
    expect(btn.disabled).toBe(true);
  });

  it("Run button is disabled in observe mode", async () => {
    getOrchestrateStatus.mockResolvedValue({ present: false });
    renderLive({ author: false });
    await waitFor(() => expect(screen.getByRole("button", { name: "Run" })).toBeTruthy());
    const btn = screen.getByRole("button", { name: "Run" }) as HTMLButtonElement;
    expect(btn.disabled).toBe(true);
  });

  it("Resume and View diff buttons appear when escalated", async () => {
    getOrchestrateStatus.mockResolvedValue({
      present: true,
      status: {
        feature: "feat-x",
        task: "t1",
        seat: "opencode",
        action: "stuck",
        phase: "owner",
        escalated: true,
        reason: "rework limit",
        done: false,
        total: 2,
        accepted: 1,
        iter: 2,
        updated_at: "x",
      },
    });
    resumeOrchestrate.mockResolvedValue({ status_url: "/api/projects/p1/orchestrate/status" });
    getDiff.mockResolvedValue({ diff: "diff --git a/foo b/foo" });
    const onNotify = vi.fn();
    renderLive({ onNotify });

    await waitFor(() => expect(screen.getByRole("button", { name: "Resume" })).toBeTruthy());
    expect(screen.getByRole("button", { name: "View diff" })).toBeTruthy();

    fireEvent.click(screen.getByRole("button", { name: "Resume" }));
    await waitFor(() => expect(resumeOrchestrate).toHaveBeenCalledWith("p1"));
    await waitFor(() => expect(onNotify).toHaveBeenCalledWith("Orchestrate resumed"));

    fireEvent.click(screen.getByRole("button", { name: "View diff" }));
    await waitFor(() => expect(getDiff).toHaveBeenCalledWith("p1"));
    await waitFor(() => expect(screen.getByText("diff --git a/foo b/foo")).toBeTruthy());
  });

  it("Ship button opens PR form and posts ship", async () => {
    getOrchestrateStatus.mockResolvedValue({
      present: true,
      status: {
        feature: "feat-x",
        task: "",
        seat: "",
        action: "done",
        phase: "done",
        escalated: false,
        done: true,
        total: 2,
        accepted: 2,
        iter: 1,
        updated_at: "x",
      },
    });
    shipFeature.mockResolvedValue({ pushed: true, pr_url: "https://github.com/org/repo/pull/42" });
    const onNotify = vi.fn();
    renderLive({ onNotify });

    await waitFor(() => expect(screen.getByRole("button", { name: "Ship" })).toBeTruthy());
    fireEvent.click(screen.getByRole("button", { name: "Ship" }));

    await waitFor(() => expect(screen.getByText("Ship feature")).toBeTruthy());
    fireEvent.change(screen.getByPlaceholderText("feat-xyz"), { target: { value: "feat-x" } });
    fireEvent.change(screen.getByPlaceholderText("feat: …"), { target: { value: "feat: x" } });
    fireEvent.click(screen.getByRole("button", { name: "Open PR" }));

    await waitFor(() =>
      expect(shipFeature).toHaveBeenCalledWith("p1", {
        pr: true,
        head: "feat-x",
        title: "feat: x",
        body: "",
      }),
    );
    await waitFor(() => expect(screen.getByText("https://github.com/org/repo/pull/42")).toBeTruthy());
  });
});
