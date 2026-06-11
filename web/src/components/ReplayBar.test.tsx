import { render, screen, waitFor, fireEvent, act } from "@testing-library/react";
import { describe, it, expect, vi, beforeEach } from "vitest";

// Mock the api module: getTimeline resolves a small index, getStateAt is a spy
// returning a folded state. ReplayBar owns the debounced getStateAt call, so the
// test asserts it fires with the slider value after fake timers. The factory is
// hoisted above module-level consts, so the fixtures live inside it.
const getStateAt = vi.fn();
vi.mock("../lib/api", () => {
  const timeline = {
    total: 3,
    events: [
      { n: 1, ts: "t1", type: "join", actor: "alice" },
      { n: 2, ts: "t2", type: "assign", actor: "alice", task: "T1" },
      { n: 3, ts: "t3", type: "awaiting_review", actor: "bob", task: "T1" },
    ],
  };
  const folded = { project: "demo", agents: [], features: [], awaiting_count: 0 };
  return {
    getTimeline: vi.fn().mockResolvedValue(timeline),
    getStateAt: (...args: unknown[]) => {
      getStateAt(...args);
      return Promise.resolve(folded);
    },
  };
});

import { ReplayBar } from "./ReplayBar";

describe("ReplayBar", () => {
  beforeEach(() => {
    getStateAt.mockClear();
  });

  it("scrubbing calls getStateAt with the slider value (debounced)", async () => {
    vi.useFakeTimers();
    try {
      const onEnter = vi.fn();
      const onSnapshot = vi.fn();
      const onLive = vi.fn();
      render(
        <ReplayBar
          project="demo"
          replayAt={null}
          onEnter={onEnter}
          onSnapshot={onSnapshot}
          onLive={onLive}
        />,
      );

      // Let the (mocked) getTimeline promise resolve so total/bounds populate.
      await act(async () => { await Promise.resolve(); });

      const slider = screen.getByLabelText("replay position") as HTMLInputElement;
      // Scrub to position 2.
      fireEvent.change(slider, { target: { value: "2" } });

      // Enters replay immediately, but the state fetch is debounced.
      expect(onEnter).toHaveBeenCalledWith(2);
      expect(getStateAt).not.toHaveBeenCalled();

      // Advance past the debounce → fetch fires with the slider value.
      await act(async () => { vi.advanceTimersByTime(200); });
      expect(getStateAt).toHaveBeenCalledWith("demo", 2);
    } finally {
      vi.useRealTimers();
    }
  });

  it("LIVE click exits replay (callback fired)", async () => {
    const onLive = vi.fn();
    render(
      <ReplayBar
        project="demo"
        replayAt={2}
        onEnter={vi.fn()}
        onSnapshot={vi.fn()}
        onLive={onLive}
      />,
    );
    await waitFor(() => expect(screen.getByLabelText("replay position")).toBeInTheDocument());
    fireEvent.click(screen.getByLabelText("resume live"));
    expect(onLive).toHaveBeenCalledTimes(1);
  });

  it("position 0 shows the 'start' caption without a network fetch", async () => {
    vi.useFakeTimers();
    try {
      const onSnapshot = vi.fn();
      const onEnter = vi.fn();
      const bar = (at: number | null) => (
        <ReplayBar
          project="demo"
          replayAt={at}
          onEnter={onEnter}
          onSnapshot={onSnapshot}
          onLive={vi.fn()}
        />
      );
      const { rerender } = render(bar(1));
      await act(async () => { await Promise.resolve(); });

      // Step back to 0: the empty fold is synthesized locally — onSnapshot gets
      // the EMPTY state and getStateAt is never hit.
      fireEvent.click(screen.getByLabelText("step back"));
      expect(onEnter).toHaveBeenCalledWith(0);
      rerender(bar(0)); // App parks replayAt=0
      await act(async () => { vi.advanceTimersByTime(200); });
      expect(screen.getByText("start")).toBeInTheDocument();
      expect(getStateAt).not.toHaveBeenCalled();
      expect(onSnapshot).toHaveBeenCalledWith(0, { project: "", agents: [], features: [], awaiting_count: 0 });

      // Stepping forward to 1 fetches at=1.
      fireEvent.click(screen.getByLabelText("step forward"));
      await act(async () => { vi.advanceTimersByTime(200); });
      expect(getStateAt).toHaveBeenCalledWith("demo", 1);
    } finally {
      vi.useRealTimers();
    }
  });

  it("fast scrubbing coalesces into a single fetch for the last position", async () => {
    vi.useFakeTimers();
    try {
      render(
        <ReplayBar
          project="demo"
          replayAt={null}
          onEnter={vi.fn()}
          onSnapshot={vi.fn()}
          onLive={vi.fn()}
        />,
      );
      await act(async () => { await Promise.resolve(); });

      const slider = screen.getByLabelText("replay position") as HTMLInputElement;
      fireEvent.change(slider, { target: { value: "1" } });
      fireEvent.change(slider, { target: { value: "2" } });
      await act(async () => { vi.advanceTimersByTime(200); });
      expect(getStateAt).toHaveBeenCalledTimes(1);
      expect(getStateAt).toHaveBeenCalledWith("demo", 2);
    } finally {
      vi.useRealTimers();
    }
  });
});
