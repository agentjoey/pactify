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

  it("scrubbing calls getStateAt with the new position (debounced)", async () => {
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

      // T14: the old input[type=range] is gone. The handle is a role="slider"
      // driven by ←/→ (same debounced go() path). Live position sits at total=3;
      // ArrowLeft scrubs to 2.
      const slider = screen.getByRole("slider", { name: "replay position" });
      fireEvent.keyDown(slider, { key: "ArrowLeft" });

      // Enters replay immediately, but the state fetch is debounced.
      expect(onEnter).toHaveBeenCalledWith(2);
      expect(getStateAt).not.toHaveBeenCalled();

      // Advance past the debounce → fetch fires with the new position.
      await act(async () => { vi.advanceTimersByTime(200); });
      expect(getStateAt).toHaveBeenCalledWith("demo", 2);
    } finally {
      vi.useRealTimers();
    }
  });

  it("clicking the ruler jumps to the nearest position (debounced)", async () => {
    vi.useFakeTimers();
    try {
      const onEnter = vi.fn();
      const onSnapshot = vi.fn();
      render(
        <ReplayBar
          project="demo"
          replayAt={null}
          onEnter={onEnter}
          onSnapshot={onSnapshot}
          onLive={vi.fn()}
        />,
      );
      await act(async () => { await Promise.resolve(); });

      const rulerEl = screen.getByTestId("replay-ruler");
      // jsdom getBoundingClientRect is all-zero, so a click maps to fraction 0 →
      // position 0 (the empty-fold short-circuit). Stub the rect so 2/3 of the
      // width → round(2.0) = position 2 with total=3.
      rulerEl.getBoundingClientRect = () =>
        ({ left: 0, width: 300, top: 0, right: 300, bottom: 0, height: 26, x: 0, y: 0, toJSON() {} }) as DOMRect;
      fireEvent.pointerDown(rulerEl, { clientX: 200, pointerId: 1 });
      fireEvent.pointerUp(rulerEl, { clientX: 200, pointerId: 1 });

      expect(onEnter).toHaveBeenCalledWith(2);
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
    await waitFor(() => expect(screen.getByRole("slider", { name: "replay position" })).toBeInTheDocument());
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

      // Two rapid ruler clicks land on positions 1 then 2 (total=3). Because the
      // fetch is debounced, only the LAST (2) survives as a single network call.
      const rulerEl = screen.getByTestId("replay-ruler");
      rulerEl.getBoundingClientRect = () =>
        ({ left: 0, width: 300, top: 0, right: 300, bottom: 0, height: 26, x: 0, y: 0, toJSON() {} }) as DOMRect;
      // 100/300 → round(1.0) = 1; 200/300 → round(2.0) = 2.
      fireEvent.pointerDown(rulerEl, { clientX: 100, pointerId: 1 });
      fireEvent.pointerUp(rulerEl, { clientX: 100, pointerId: 1 });
      fireEvent.pointerDown(rulerEl, { clientX: 200, pointerId: 1 });
      fireEvent.pointerUp(rulerEl, { clientX: 200, pointerId: 1 });
      await act(async () => { vi.advanceTimersByTime(200); });
      expect(getStateAt).toHaveBeenCalledTimes(1);
      expect(getStateAt).toHaveBeenCalledWith("demo", 2);
    } finally {
      vi.useRealTimers();
    }
  });

  it("renders one typed tick per event (colored by type)", async () => {
    render(
      <ReplayBar
        project="demo"
        replayAt={null}
        onEnter={vi.fn()}
        onSnapshot={vi.fn()}
        onLive={vi.fn()}
      />,
    );
    await waitFor(() => expect(screen.getAllByTestId("replay-tick")).toHaveLength(3));
    const ticks = screen.getAllByTestId("replay-tick");
    // join (n=1) → success; assign (n=2) → warn; awaiting_review (n=3) → role-design.
    expect(ticks[0].style.background).toBe("var(--color-success)");
    expect(ticks[1].style.background).toBe("var(--color-warn)");
    expect(ticks[2].style.background).toBe("var(--color-role-design)");
  });

  it("positions ticks at (n/total)*100% and fills the played region to the current position", async () => {
    render(
      <ReplayBar
        project="demo"
        replayAt={2}
        onEnter={vi.fn()}
        onSnapshot={vi.fn()}
        onLive={vi.fn()}
      />,
    );
    await waitFor(() => expect(screen.getAllByTestId("replay-tick")).toHaveLength(3));
    const ticks = screen.getAllByTestId("replay-tick");
    expect(ticks[0].style.left).toBe(`${(1 / 3) * 100}%`); // n=1
    expect(ticks[2].style.left).toBe("100%");              // n=3
    // played width tracks replayAt=2 → 2/3.
    expect(screen.getByTestId("replay-played").style.width).toBe(`${(2 / 3) * 100}%`);
    // unplayed tick (n=3, after pos=2) is dimmed; played ticks are full opacity.
    expect(ticks[2].style.opacity).toBe("0.35");
    expect(ticks[0].style.opacity).toBe("1");
  });

  it("hovering a tick floats the #n type · actor · ts preview card; leaving clears it", async () => {
    render(
      <ReplayBar
        project="demo"
        replayAt={null}
        onEnter={vi.fn()}
        onSnapshot={vi.fn()}
        onLive={vi.fn()}
      />,
    );
    await waitFor(() => expect(screen.getAllByTestId("replay-tick")).toHaveLength(3));
    const ticks = screen.getAllByTestId("replay-tick");
    expect(screen.queryByTestId("replay-evcard")).toBeNull();
    fireEvent.pointerEnter(ticks[1]); // assign · alice · T1
    const card = screen.getByTestId("replay-evcard");
    expect(card.textContent).toContain("#2 assign");
    expect(card.textContent).toContain("alice");
    expect(card.textContent).toContain("T1");
    fireEvent.pointerLeave(ticks[1]);
    expect(screen.queryByTestId("replay-evcard")).toBeNull();
  });

  it("exposes slider semantics on the handle (valuemin/max/now)", async () => {
    render(
      <ReplayBar
        project="demo"
        replayAt={2}
        onEnter={vi.fn()}
        onSnapshot={vi.fn()}
        onLive={vi.fn()}
      />,
    );
    const handle = await screen.findByRole("slider", { name: "replay position" });
    expect(handle).toHaveAttribute("aria-valuemin", "0");
    await waitFor(() => expect(handle).toHaveAttribute("aria-valuemax", "3"));
    expect(handle).toHaveAttribute("aria-valuenow", "2");
  });
});
