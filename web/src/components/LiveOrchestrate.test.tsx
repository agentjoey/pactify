import { render, screen, waitFor } from "@testing-library/react";
import { describe, it, expect, vi, beforeEach } from "vitest";

const getOrchestrateStatus = vi.fn();
const getParallelOrchestrate = vi.fn();
vi.mock("../lib/api", () => ({
  getOrchestrateStatus: (...args: unknown[]) => getOrchestrateStatus(...args),
  getParallelOrchestrate: (...args: unknown[]) => getParallelOrchestrate(...args),
}));

import { LiveOrchestrate } from "./LiveOrchestrate";

describe("LiveOrchestrate panel", () => {
  beforeEach(() => {
    getOrchestrateStatus.mockReset();
    getParallelOrchestrate.mockReset();
    // Default: no parallel run, so the serial fallback renders.
    getParallelOrchestrate.mockResolvedValue({ present: false });
  });

  it("renders empty state when not present", async () => {
    getOrchestrateStatus.mockResolvedValue({ present: false });
    render(<LiveOrchestrate project="p1" refreshTick={0} />);
    await waitFor(() => {
      expect(screen.getByText("orchestrate 尚未运行")).toBeTruthy();
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
    render(<LiveOrchestrate project="p1" refreshTick={0} />);
    await waitFor(() => {
      expect(screen.getByText("编排已升级 — 需人工介入")).toBeTruthy();
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
    render(<LiveOrchestrate project="p1" refreshTick={0} />);
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
    render(<LiveOrchestrate project="p1" refreshTick={0} />);
    await waitFor(() => {
      expect(screen.getByText("已收工 / 全部交付")).toBeTruthy();
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
    render(<LiveOrchestrate project="p1" refreshTick={0} />);
    await waitFor(() => {
      expect(screen.getByTestId("parallel-panel")).toBeTruthy();
    });
    expect(screen.getAllByTestId("parallel-feature")).toHaveLength(2);
    expect(screen.getByText("2 features")).toBeTruthy();
    // both feature ids shown; serial single-status panel is NOT rendered
    expect(screen.getByText("fa")).toBeTruthy();
    expect(screen.getByText("fb")).toBeTruthy();
    expect(screen.queryByText("orchestrate 尚未运行")).toBeNull();
  });

  it("escalated feature in the parallel grid shows its reason", async () => {
    getOrchestrateStatus.mockResolvedValue({ present: false });
    getParallelOrchestrate.mockResolvedValue({
      present: true,
      features: [
        { feature: "fx", task: "t1", seat: "w", action: "stuck", phase: "stuck", escalated: true, reason: "gate failed", done: false, total: 1, accepted: 0, iter: 1, updated_at: "x" },
      ],
    });
    render(<LiveOrchestrate project="p1" refreshTick={0} />);
    await waitFor(() => {
      expect(screen.getByText("gate failed")).toBeTruthy();
    });
  });
});
