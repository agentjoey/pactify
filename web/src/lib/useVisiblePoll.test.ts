import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";

// document.visibilityState is read-only; tests flip it via defineProperty.
function setVisibility(v: DocumentVisibilityState) {
  Object.defineProperty(document, "visibilityState", { configurable: true, value: v });
}
import { renderHook } from "@testing-library/react";
import { useVisiblePoll } from "./useVisiblePoll";

describe("useVisiblePoll", () => {
  beforeEach(() => {
    vi.useFakeTimers();
    setVisibility("visible");
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  it("runs immediately and then on every interval while visible", () => {
    const fn = vi.fn();
    renderHook(() => useVisiblePoll(fn, 5000));
    expect(fn).toHaveBeenCalledTimes(1);
    vi.advanceTimersByTime(5000);
    expect(fn).toHaveBeenCalledTimes(2);
    vi.advanceTimersByTime(5000);
    expect(fn).toHaveBeenCalledTimes(3);
  });

  it("pauses when hidden and resumes with an immediate tick when visible", () => {
    const fn = vi.fn();
    renderHook(() => useVisiblePoll(fn, 4000));
    expect(fn).toHaveBeenCalledTimes(1);

    setVisibility("hidden");
    document.dispatchEvent(new Event("visibilitychange"));
    vi.advanceTimersByTime(4000);
    expect(fn).toHaveBeenCalledTimes(1); // still paused

    setVisibility("visible");
    document.dispatchEvent(new Event("visibilitychange"));
    expect(fn).toHaveBeenCalledTimes(2); // immediate tick on resume
    vi.advanceTimersByTime(4000);
    expect(fn).toHaveBeenCalledTimes(3);
  });

  it("cleans up the interval on unmount", () => {
    const fn = vi.fn();
    const { unmount } = renderHook(() => useVisiblePoll(fn, 3000));
    unmount();
    vi.advanceTimersByTime(3000);
    expect(fn).toHaveBeenCalledTimes(1);
  });
});
