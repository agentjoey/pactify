import { useState } from "react";
import type { PactEvent, Seat, State } from "../../lib/types";
import { taskTokens, fmtTokens } from "../../lib/derive";

// EventDrawer — the Board's collapsible bottom terminal (formerly the Live
// view's right-hand EventStream pane, PR2 of the views consolidation): a
// colorized tail of pact log.jsonl + a per-seat presence footer (runs · tokens,
// attributed from owned tasks). Collapsed by default to a one-line ticker
// showing the latest event; expanding reveals the full terminal.
export function EventDrawer({ events, agents, state }: { events: PactEvent[]; agents: Seat[]; state: State }) {
  const [open, setOpen] = useState(false);
  const recent = events.slice(-200);
  const latest = recent.length ? recent[recent.length - 1] : null;
  const glyph: Record<string, { ch: string; color: string }> = {
    checkpoint: { ch: "$", color: "var(--color-role-dev)" },
    accept: { ch: "✓", color: "var(--color-role-design)" },
    assign: { ch: "·", color: "var(--color-text-3)" },
    join: { ch: "→", color: "var(--color-text-3)" },
    merge: { ch: "✓", color: "var(--color-role-dev)" },
    changes_requested: { ch: "!", color: "var(--color-warn)" },
  };
  const seats = agents.map((a) => {
    const runs = events.filter((e) => e.agent_id === a.id && (e.event_type === "checkpoint" || e.event_type === "accept")).length;
    const tok = state.features
      .flatMap((f) => f.tasks)
      .filter((t) => t.owner === a.id)
      .reduce((n, t) => n + taskTokens(t.id, events), 0);
    return { id: a.id, runs, tok };
  });

  return (
    <div
      data-testid="event-drawer"
      data-state={open ? "open" : "collapsed"}
      className="flex flex-none flex-col border-t border-[var(--color-border-subtle)]"
      style={{ background: "var(--color-bg-terminal,#07090d)" }}
    >
      <button
        type="button"
        data-testid="event-drawer-toggle"
        aria-label="Toggle event stream"
        aria-expanded={open}
        onClick={() => setOpen((v) => !v)}
        className="flex w-full items-center gap-2 px-4 py-2 text-left"
      >
        <span className="mono text-[10px] uppercase tracking-[1px] text-[var(--color-text-3)]">Event stream</span>
        <span className="inline-flex items-center gap-[5px] text-[9.5px] font-medium" style={{ color: "var(--color-success)" }}>
          <span className="status-pill-dot-live h-[5px] w-[5px] rounded-full" style={{ background: "var(--color-success)" }} />live
        </span>
        {!open && latest && (
          <span className="mono min-w-0 flex-1 truncate text-[10.5px] text-[var(--color-text-3)]">
            <span>{(latest.ts || "").slice(11, 19)}</span>{" "}
            <span style={{ color: "var(--color-role-product)" }}>{latest.agent_id}</span>{" "}
            <span className="text-[var(--color-text-2)]">{latest.event_type}</span>
            {latest.task_id ? ` ${latest.task_id}` : ""}
          </span>
        )}
        <span className="mono ml-auto text-[10px] text-[var(--color-text-3)]">log.jsonl · {events.length}</span>
        <span className="text-[10px] text-[var(--color-text-3)]">{open ? "▾" : "▴"}</span>
      </button>

      {open && (
        <>
          <div data-testid="event-stream" className="mono h-[240px] overflow-y-auto px-4 py-3 text-[11px] leading-[1.85] text-[var(--color-text-2)]">
            {recent.length === 0 ? (
              <div className="text-[var(--color-text-3)]">no events yet…</div>
            ) : (
              recent.map((e) => {
                const g = glyph[e.event_type] ?? { ch: "·", color: "var(--color-text-3)" };
                return (
                  <div key={e.event_id} className="whitespace-nowrap">
                    <span className="text-[var(--color-text-3)]">{(e.ts || "").slice(11, 19)}</span>{" "}
                    <span style={{ color: g.color }}>{g.ch}</span>{" "}
                    <span style={{ color: "var(--color-role-product)" }}>{e.agent_id}</span>{" "}
                    <span style={{ color: "var(--color-text-2)" }}>{e.event_type}</span>
                    {e.task_id && <> <span className="text-[var(--color-text-3)]">{e.task_id}</span></>}
                  </div>
                );
              })
            )}
            <span className="mt-1 flex items-center gap-[6px]">
              <span style={{ color: "var(--color-role-dev)" }}>$</span>
              <span className="live-cursor inline-block h-[12px] w-[7px]" style={{ background: "var(--color-role-dev)" }} />
            </span>
          </div>
          <div className="flex flex-wrap items-center gap-x-3 gap-y-1 border-t border-[var(--color-border-subtle)] px-4 py-[9px]">
            <span className="mono text-[9px] text-[var(--color-text-3)]">SEATS</span>
            {seats.map((s) => (
              <span key={s.id} className="inline-flex items-center gap-[5px] text-[9.5px] font-medium text-[var(--color-text-2)]">
                <span className="h-[7px] w-[7px] rounded-full" style={{ background: s.runs > 0 ? "var(--color-success)" : "var(--color-text-3)" }} />
                {s.id} <span className="mono text-[var(--color-text-3)]">{s.runs}·{fmtTokens(s.tok)}</span>
              </span>
            ))}
          </div>
        </>
      )}
    </div>
  );
}
