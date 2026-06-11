import { useEffect, useRef, useState } from "react";
import type { State, Timeline } from "../lib/types";
import { getTimeline, getStateAt } from "../lib/api";

const EMPTY: State = { project: "", agents: [], features: [], awaiting_count: 0 };

// ReplayBar — full-state time-travel scrubber, rendered under kanban AND canvas
// (never ops). It owns the timeline index (fetched once on mount/project change)
// and drives the App's replay position via the callbacks below.
//
// Contract with App:
//   - replayAt: current position (null = live). App derives `replaying` from it.
//   - onEnter(n): App flips into replay mode at position n (sets replayAt).
//   - onSnapshot(n, state): the fetched historical snapshot for display. App
//     parks it in replayState (display precedence over the live state).
//   - onLive(): exit replay — App refetches the live state and resumes SSE apply.
//
// Debounce: while dragging the slider, the historical state fetch is debounced
// ~150ms so a fast drag doesn't hammer the endpoint (at=0 short-circuits to the
// empty fold without a network call). Step buttons reuse the same path.

const DEBOUNCE_MS = 150;

export function ReplayBar({
  project,
  replayAt,
  onEnter,
  onSnapshot,
  onLive,
}: {
  project: string;
  // null = live; a number = current replay position (0..total).
  replayAt: number | null;
  // Called with the position when entering replay from live (App sets replayAt).
  onEnter: (at: number) => void;
  // Called with the fetched historical state for a given position. App parks it
  // in replayState (display precedence). The position is passed so App can drop
  // a stale response if it ever needs to.
  onSnapshot: (at: number, state: State) => void;
  // LIVE pressed — exit replay, App refetches live state and resumes SSE.
  onLive: () => void;
}) {
  const [timeline, setTimeline] = useState<Timeline | null>(null);
  const debounce = useRef<ReturnType<typeof setTimeout> | null>(null);

  const replaying = replayAt !== null;
  const total = timeline?.total ?? 0;

  // Fetch the timeline index once per project (cheap, no payloads). Reset on
  // project change so the slider bounds track the selected project.
  useEffect(() => {
    let alive = true;
    setTimeline(null);
    if (!project) return;
    getTimeline(project)
      .then((t) => { if (alive) setTimeline(t); })
      .catch(() => { if (alive) setTimeline({ total: 0, events: [] }); });
    return () => { alive = false; };
  }, [project]);

  useEffect(() => () => { if (debounce.current) clearTimeout(debounce.current); }, []);

  // go moves to position `at`: enter replay if currently live, then debounce the
  // historical-state fetch. at=0 → empty fold (no network). The fetched snapshot
  // is handed up via onSnapshot for display precedence.
  function go(at: number) {
    const clamped = Math.max(0, Math.min(at, total));
    onEnter(clamped);
    if (debounce.current) clearTimeout(debounce.current);
    debounce.current = setTimeout(() => {
      if (clamped <= 0) { onSnapshot(0, EMPTY); return; }
      getStateAt(project, clamped)
        .then((s) => onSnapshot(clamped, s))
        .catch(() => {});
    }, DEBOUNCE_MS);
  }

  function live() {
    if (debounce.current) clearTimeout(debounce.current);
    onLive();
  }

  // Caption for the current position. Position 0 (or live-at-0) → "start";
  // otherwise the 1-based event at `replayAt` (events[replayAt-1]).
  const pos = replayAt ?? total;
  let caption: string;
  if (!timeline) {
    caption = "…";
  } else if (pos <= 0) {
    caption = "start";
  } else {
    const ev = timeline.events[pos - 1];
    caption = ev
      ? `#${ev.n} ${ev.type} · ${ev.actor}${ev.task ? ` · ${ev.task}` : ""} · ${ev.ts}`
      : `#${pos}`;
  }

  return (
    <div
      data-testid="replay-bar"
      className="flex items-center gap-2 border-t border-gray-800 bg-[#0f1419] px-3 py-1.5 text-xs"
    >
      <button
        aria-label="step back"
        className="rounded border border-gray-700 px-1.5 py-0.5 text-gray-300 disabled:opacity-40 hover:border-gray-500"
        onClick={() => go(pos - 1)}
        disabled={pos <= 0}
      >
        ◀
      </button>
      <input
        type="range"
        aria-label="replay position"
        className="flex-1 accent-[#3fb950]"
        min={0}
        max={total}
        value={pos}
        onChange={(e) => go(Number(e.target.value))}
      />
      <button
        aria-label="step forward"
        className="rounded border border-gray-700 px-1.5 py-0.5 text-gray-300 disabled:opacity-40 hover:border-gray-500"
        onClick={() => go(pos + 1)}
        disabled={pos >= total}
      >
        ▶
      </button>
      <span className="min-w-[16ch] truncate font-mono text-gray-400" title={caption}>
        {caption}
      </span>
      <button
        aria-label="resume live"
        className={`rounded border px-2 py-0.5 ${
          replaying
            ? "border-[#3fb950] text-[#3fb950] hover:bg-[#3fb950]/10"
            : "border-gray-700 text-gray-600"
        }`}
        onClick={live}
        disabled={!replaying}
      >
        LIVE
      </button>
    </div>
  );
}
