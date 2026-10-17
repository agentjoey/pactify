import { describe, expect, it, vi } from "vitest";
import {
  emptyLive,
  frameBatch,
  liveEdges,
  reduceLive,
  selectLiveJobs,
} from "./liveTree";
import type { Job } from "./types";
const job = (id: string, extra: Partial<Job> = {}) =>
  ({
    id,
    status: "running",
    createdAt: "2026-10-16T00:00:00Z",
    resumeOf: "",
    resumedBy: [],
    ...extra,
  }) as Job;
describe("safe event mapping", () => {
  it("maps codex turns, tool calls, cumulative usage, diff and quota", () => {
    const state = reduceLive(emptyLive(), [
      { kind: "state", state: "turn_started" },
      {
        kind: "tool",
        tool: {
          id: "/a",
          type: "shell",
          name: "/bin/zsh -lc 'go test ./...'",
          phase: "start",
        },
      },
      { kind: "tool", tool: { id: "/a", type: "shell", phase: "end" } },
      { kind: "usage", usage: { inputTokens: 100, outputTokens: 20 } },
      { kind: "usage", usage: { inputTokens: 140, outputTokens: 25 } },
      { kind: "diff", diff: { files: 2, added: 10, deleted: 3 } },
      {
        kind: "system",
        state: "rate_limits",
        rateLimit: "7-day window: 35% used",
      },
      { kind: "state", state: "turn_completed" },
    ]);
    expect(state.phase).toBe("done");
    expect(state.tools.test).toEqual({ calls: 1, active: 0, pulse: 2 });
    expect(state.usage).toEqual({ inputTokens: 140, outputTokens: 25 });
    expect(state.diff).toEqual({ files: 2, added: 10, deleted: 3 });
    expect(state.rateLimit).toContain("7-day");
    expect(state.costUSD).toBeUndefined();
  });
  it("maps opencode tools and known job price", () => {
    const state = reduceLive(emptyLive(), [
      { kind: "tool", tool: { id: "/part", name: "edit", phase: "start" } },
      {
        kind: "tool",
        tool: { id: "/part", name: "edit", phase: "end", status: "completed" },
      },
      {
        kind: "usage",
        usage: { inputTokens: 12, outputTokens: 2 },
        costUSD: 0.025,
      },
      { kind: "state", state: "turn_failed" },
    ]);
    expect(state.tools.edit?.calls).toBe(1);
    expect(state.tools.edit?.active).toBe(0);
    expect(state.phase).toBe("failed");
    expect(state.costUSD).toBe(0.025);
  });
  it("counts kimi in-progress metadata once and tolerates missing start/name", () => {
    const state = reduceLive(emptyLive(), [
      { kind: "tool", tool: { id: "/kimi", name: "", phase: "start" } },
      {
        kind: "tool",
        tool: {
          id: "/kimi",
          name: "",
          phase: "output",
          status: "in_progress",
          text: "{",
        },
      },
      {
        kind: "tool",
        tool: {
          id: "/kimi",
          name: "Reading file",
          type: "read",
          phase: "output",
          status: "in_progress",
        },
      },
      {
        kind: "tool",
        tool: { id: "/kimi", phase: "output", status: "completed" },
      },
    ]);
    expect(state.tools.read).toEqual({ calls: 1, active: 0, pulse: 2 });
    expect(state.pending).toEqual({});
  });
  it("ignores unknown and malformed events and never invents tools", () => {
    const state = reduceLive(emptyLive(), [
      null,
      false,
      "bad",
      [],
      { kind: "explorer" },
      { kind: "state", state: "mystery" },
      { kind: "tool", tool: { name: "web_search", phase: "start" } },
      { kind: "usage", usage: { inputTokens: -1, outputTokens: NaN } },
      { kind: "diff", diff: { files: "2" } },
    ]);
    expect(state.tools).toEqual({});
    expect(state.usage).toBeUndefined();
    expect(state.diff).toBeUndefined();
    expect(state.phase).toBeUndefined();
  });
  it("coalesces message deltas and bounds the session log without mutating input", () => {
    const original = emptyLive();
    const state = reduceLive(original, [
      { kind: "message", text: "Hel", time: "now" },
      { kind: "message", text: "lo" },
      { kind: "message", text: "Hello", final: true },
    ]);
    expect(state.log).toEqual([{ time: "now", text: "Hello" }]);
    expect(original.log).toEqual([]);
    expect(
      reduceLive(
        state,
        Array.from({ length: 100 }, () => ({
          kind: "message",
          text: "next",
          final: true,
        })),
      ).log,
    ).toHaveLength(60);
  });
});
it("preserves explicit resume forks, fallback chains and retry loops including missing peers", () => {
  const jobs = [
    job("a", { resumedBy: ["b", "c"] }),
    job("b", { resumeOf: "a", fallbackTo: "d" }),
    job("c", { resumeOf: "a" }),
    job("d", { fallbackFrom: "b", fallbackTo: "e", retryCount: 2 }),
  ];
  expect(liveEdges(jobs)).toEqual([
    { from: "a", to: "b", kind: "rework" },
    { from: "a", to: "c", kind: "rework" },
    { from: "b", to: "d", kind: "fallback" },
    { from: "d", to: "e", kind: "fallback" },
    { from: "d", to: "d", kind: "retry", count: 2 },
  ]);
});
it("selects all running jobs or the latest recorded run, with a truthful empty state", () => {
  expect(selectLiveJobs([job("q", { status: "queued" })]).jobs).toEqual([]);
  const done = job("done", { status: "succeeded", finishedAt: "2026-10-17" });
  expect(selectLiveJobs([done]).replay).toBe(true);
  expect(
    selectLiveJobs([done, job("a"), job("b")]).jobs.map((j) => j.id),
  ).toEqual(["a", "b"]);
  expect(selectLiveJobs([done, job("a")], "done").jobs).toEqual([done]);
});
it("batches a burst into exactly one frame and cancels queued work on dispose", () => {
  let next: FrameRequestCallback = () => {};
  const request = vi.fn((callback: FrameRequestCallback) => {
    next = callback;
    return 7;
  });
  const cancel = vi.fn();
  const consume = vi.fn();
  const batch = frameBatch<number>(consume, request, cancel);
  for (let i = 0; i < 1000; i++) batch.add(i);
  expect(request).toHaveBeenCalledTimes(1);
  expect(consume).not.toHaveBeenCalled();
  next(0);
  expect(consume).toHaveBeenCalledWith(
    Array.from({ length: 1000 }, (_, i) => i),
  );
  batch.add(1001);
  batch.dispose();
  expect(cancel).toHaveBeenCalledWith(7);
});
