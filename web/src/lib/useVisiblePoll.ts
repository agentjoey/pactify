import { useEffect, useRef } from "react";

/**
 * Run `fn` on an interval while the document is visible. Pauses in the background
 * (document.visibilityState === "hidden") and executes `fn` immediately when the
 * page becomes visible again before resuming the interval.
 */
export function useVisiblePoll(fn: () => void, ms: number): void {
  const cbRef = useRef(fn);
  useEffect(() => {
    cbRef.current = fn;
  }, [fn]);

  useEffect(() => {
    if (typeof document === "undefined") return;

    let timer: ReturnType<typeof setInterval> | null = null;
    const tick = () => cbRef.current();
    const isVisible = () => document.visibilityState === "visible";

    const start = () => {
      if (timer) return;
      tick();
      timer = setInterval(tick, ms);
    };
    const stop = () => {
      if (timer) {
        clearInterval(timer);
        timer = null;
      }
    };

    const onVisibility = () => {
      if (isVisible()) start();
      else stop();
    };

    if (isVisible()) start();
    document.addEventListener("visibilitychange", onVisibility);
    return () => {
      stop();
      document.removeEventListener("visibilitychange", onVisibility);
    };
  }, [ms]);
}
