import { useEffect, useRef, useState } from "react";
import type { State, Timeline } from "../lib/types";
import { getTimeline, getStateAt } from "../lib/api";
import { tickColorVar } from "../lib/timelineTicks";

const EMPTY: State = { project: "", agents: [], features: [], awaiting_count: 0 };

// ReplayBar — full-state time-travel scrubber, rendered under kanban AND canvas
// (never ops). It owns the timeline index (fetched once on mount/project change)
// and drives the App's replay position via the callbacks below.
//
// Contract with App (FROZEN):
//   - replayAt: current position (null = live). App derives `replaying` from it.
//   - onEnter(n): App flips into replay mode at position n (sets replayAt).
//   - onSnapshot(n, state): the fetched historical snapshot for display. App
//     parks it in replayState (display precedence over the live state).
//   - onLive(): exit replay — App refetches the live state and resumes SSE apply.
//
// Debounce: while dragging the handle / scrubbing, the historical state fetch is
// debounced ~150ms so a fast drag doesn't hammer the endpoint (at=0 short-circuits
// to the empty fold without a network call). Step buttons, ruler clicks, keyboard
// arrows and pointer drag all funnel through the SAME debounced go() path.
//
// T14: the old input[type=range] is replaced by a board4 §③ ruler — a thin base
// track, per-event ticks colored by event type, a played-region gradient fill,
// a draggable white handle, a hover event-preview card, and ◀/▶ step buttons.
// The component stays URL-agnostic; App owns the `?at` deep link.

const DEBOUNCE_MS = 150;

export function ReplayBar({
  project,
  replayAt,
  refreshTick = 0,
  onEnter,
  onSnapshot,
  onLive,
}: {
  project: string;
  // null = live; a number = current replay position (0..total).
  replayAt: number | null;
  // Bumped by App on every applied live snapshot — re-fetches the timeline so
  // the bounds track new events during a long live session. Frozen while
  // replaying (SSE snapshots are skipped, so the position doesn't move).
  refreshTick?: number;
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
  const [hover, setHover] = useState<number | null>(null); // hovered tick index (0-based)
  const debounce = useRef<ReturnType<typeof setTimeout> | null>(null);
  const ruler = useRef<HTMLDivElement | null>(null);
  // The last position THIS component drove through go(). Used to detect a replay
  // position set EXTERNALLY by App (the `?at` deep link, or a "replay to here"
  // jump elsewhere) so we fetch+park that snapshot exactly once. -1 = nothing
  // driven yet; reset to null-equivalent on resume.
  const lastDriven = useRef<number | null>(null);
  // While a pointer drag is active we capture moves on the ruler and keep
  // scrubbing until pointerup. dragging guards the move handler.
  const dragging = useRef(false);

  const replaying = replayAt !== null;
  const total = timeline?.total ?? 0;

  // Reset the index on project change only (a refresh keeps the old index
  // visible until the new one lands — no caption flicker).
  useEffect(() => {
    setTimeline(null);
    setHover(null);
    // Cancel any pending position fetch on project switch and on unmount.
    return () => { if (debounce.current) clearTimeout(debounce.current); };
  }, [project]);

  // Fetch the timeline index (cheap, no payloads) on project change and on
  // every applied live snapshot, so the bounds track new events.
  useEffect(() => {
    let alive = true;
    if (!project) return;
    getTimeline(project)
      .then((t) => { if (alive) setTimeline(t); })
      .catch(() => { if (alive) setTimeline((cur) => cur ?? { total: 0, events: [] }); });
    return () => { alive = false; };
  }, [project, refreshTick]);

  // go moves to position `at`: enter replay if currently live, then debounce the
  // historical-state fetch. at=0 → empty fold (no network). The fetched snapshot
  // is handed up via onSnapshot for display precedence.
  function go(at: number) {
    const clamped = Math.max(0, Math.min(at, total));
    lastDriven.current = clamped;
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
    lastDriven.current = null;
    onLive();
  }

  // Externally-set replay position (the `?at` deep link, applied by App once the
  // first project is current): when replayAt arrives at a position WE didn't
  // drive through go() and the timeline is loaded (so go() can clamp), fetch+park
  // that snapshot exactly once via the SAME go() path. Normal scrubbing already
  // set lastDriven, so this no-ops then. Cleared to null on resume.
  useEffect(() => {
    if (replayAt === null) { lastDriven.current = null; return; }
    if (!timeline) return; // wait for bounds so the clamp is meaningful
    if (lastDriven.current === replayAt) return;
    go(replayAt);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [replayAt, timeline]);

  // Current 0..total position. When live, the position sits at the end (total).
  const pos = replayAt ?? total;
  const pct = (n: number) => (total > 0 ? (n / total) * 100 : 0);

  // Map a clientX on the ruler to the nearest position (clamped, rounded).
  function posFromClientX(clientX: number): number {
    const el = ruler.current;
    if (!el || total <= 0) return 0;
    const r = el.getBoundingClientRect();
    const frac = r.width > 0 ? (clientX - r.left) / r.width : 0;
    return Math.max(0, Math.min(total, Math.round(frac * total)));
  }

  function onPointerDown(e: React.PointerEvent<HTMLDivElement>) {
    if (total <= 0) return;
    dragging.current = true;
    try { e.currentTarget.setPointerCapture(e.pointerId); } catch { /* jsdom */ }
    go(posFromClientX(e.clientX));
  }
  function onPointerMove(e: React.PointerEvent<HTMLDivElement>) {
    if (!dragging.current) return;
    go(posFromClientX(e.clientX));
  }
  function endDrag(e: React.PointerEvent<HTMLDivElement>) {
    if (!dragging.current) return;
    dragging.current = false;
    try { e.currentTarget.releasePointerCapture(e.pointerId); } catch { /* jsdom */ }
  }

  function onKeyDown(e: React.KeyboardEvent) {
    if (e.key === "ArrowLeft") { e.preventDefault(); go(pos - 1); }
    else if (e.key === "ArrowRight") { e.preventDefault(); go(pos + 1); }
  }

  // Caption for the current position. Position 0 (or live-at-0) → "start";
  // otherwise the 1-based event at `pos` (events[pos-1]).
  let caption: string;
  if (!timeline) {
    caption = "…";
  } else if (pos <= 0) {
    caption = "start";
  } else {
    const ev = timeline.events?.[pos - 1];
    caption = ev
      ? `#${ev.n} ${ev.type} · ${ev.actor}${ev.task ? ` · ${ev.task}` : ""} · ${ev.ts}`
      : `#${pos}`;
  }

  // Hover preview card content (board .evcard): `#n type · actor · ts`.
  const hoverEv = hover !== null && timeline ? timeline.events?.[hover] : null;

  return (
    <div
      data-testid="replay-bar"
      className="relative border-t border-gray-800 bg-[#0f1419] px-4 pt-4 pb-1.5 text-xs"
    >
      {/* Hover event-preview card, floating above the ruler. */}
      {hoverEv && (
        <div
          data-testid="replay-evcard"
          className="pointer-events-none absolute -translate-x-1/2 rounded-lg border border-white/15 bg-[#2F3246] px-2.5 py-1.5 font-mono text-[10.5px] text-white/85 shadow-[0_10px_28px_rgba(0,0,0,0.55)] whitespace-nowrap"
          style={{ left: `calc(${pct(hoverEv.n)}% + 16px)`, bottom: "46px" }}
        >
          <span style={{ color: "var(--color-role-design)" }}>#{hoverEv.n} {hoverEv.type}</span>
          {" · "}{hoverEv.actor}
          {hoverEv.task ? <> · {hoverEv.task}</> : null}
          <span className="text-white/40"> · {hoverEv.ts}</span>
        </div>
      )}

      <div className="flex items-center gap-3">
        <button
          aria-label="step back"
          className="rounded border border-gray-700 px-1.5 py-0.5 text-gray-300 disabled:opacity-40 hover:border-gray-500"
          onClick={() => go(pos - 1)}
          disabled={pos <= 0}
        >
          ◀
        </button>

        {/* Ruler: base track + played gradient + typed ticks + drag handle.
            The handle carries the slider semantics; the ruler captures clicks
            and pointer drags. */}
        <div
          ref={ruler}
          data-testid="replay-ruler"
          className="relative h-[26px] flex-1 cursor-pointer touch-none select-none"
          onPointerDown={onPointerDown}
          onPointerMove={onPointerMove}
          onPointerUp={endDrag}
          onPointerCancel={endDrag}
        >
          {/* base track */}
          <div className="absolute inset-x-0 top-3 h-[3px] rounded-sm bg-white/10" />
          {/* played region — left of the current position */}
          <div
            data-testid="replay-played"
            className="absolute left-0 top-3 h-[3px] rounded-sm"
            style={{
              width: `${pct(pos)}%`,
              background: "linear-gradient(90deg, var(--color-success), var(--color-role-design))",
            }}
          />
          {/* per-event ticks, colored by type; unplayed dimmed, current taller */}
          {timeline?.events?.map((ev, i) => {
            const played = ev.n <= pos;
            const current = ev.n === pos;
            return (
              <span
                key={ev.n}
                data-testid="replay-tick"
                data-tick-n={ev.n}
                className="absolute -translate-x-1/2 rounded-[1.5px]"
                style={{
                  left: `${pct(ev.n)}%`,
                  top: current ? "5px" : "7px",
                  width: "2.5px",
                  height: current ? "17px" : "13px",
                  background: tickColorVar(ev.type),
                  opacity: played ? 1 : 0.35,
                }}
                onPointerEnter={() => setHover(i)}
                onPointerLeave={() => setHover((h) => (h === i ? null : h))}
              />
            );
          })}
          {/* drag handle — white circle with role-design ring; carries slider role */}
          <div
            role="slider"
            tabIndex={0}
            aria-label="replay position"
            aria-valuemin={0}
            aria-valuemax={total}
            aria-valuenow={pos}
            data-testid="replay-handle"
            onKeyDown={onKeyDown}
            className="absolute top-1 h-[17px] w-[17px] -translate-x-1/2 rounded-full bg-white shadow-[0_2px_8px_rgba(0,0,0,0.5)] outline-none focus-visible:ring-2 focus-visible:ring-[color:var(--color-role-design)]"
            style={{ left: `${pct(pos)}%`, border: "3.5px solid var(--color-role-design)" }}
          />
        </div>

        <button
          aria-label="step forward"
          className="rounded border border-gray-700 px-1.5 py-0.5 text-gray-300 disabled:opacity-40 hover:border-gray-500"
          onClick={() => go(pos + 1)}
          disabled={pos >= total}
        >
          ▶
        </button>

        <span className="min-w-[16ch] max-w-[28ch] truncate font-mono text-gray-400" title={caption}>
          {caption}
        </span>

        <button
          aria-label="resume live"
          className={`ml-auto flex items-center gap-1.5 rounded-full border px-3 py-1 text-[11px] font-semibold ${
            replaying
              ? "border-[color:var(--color-warn)]/50 bg-[color:var(--color-warn)]/10 text-[#E8C27A]"
              : "border-gray-700 text-gray-600"
          }`}
          onClick={live}
          disabled={!replaying}
        >
          {replaying && (
            <span
              className="h-1.5 w-1.5 rounded-full"
              style={{ background: "var(--color-warn)" }}
            />
          )}
          LIVE
        </button>
      </div>
    </div>
  );
}
