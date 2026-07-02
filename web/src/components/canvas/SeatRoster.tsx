import type { Seat } from "../../lib/types";
import { casteForRoles } from "../../lib/ants";
import { Ant } from "../ui/ants/Ant";

// SeatRoster — a fixed, always-on strip of the project's participating seats and
// their roles, pinned to the canvas (it does NOT pan/zoom with the flow, unlike
// the SeatNodes / desks). So "who's playing, in what role" stays visible no
// matter where you've panned around the Office.

// role → brand color (the three-main-color language); unknown roles fall back to
// a neutral chip.
const ROLE_COLOR: Record<string, { main: string; ink: string }> = {
  orchestrator: { main: "var(--color-role-product)", ink: "var(--color-role-product-ink)" },
  reviewer: { main: "var(--color-role-design)", ink: "var(--color-role-design-ink)" },
  worker: { main: "var(--color-role-dev)", ink: "var(--color-role-dev-ink)" },
};

export function SeatRoster({ agents }: { agents: Seat[] }) {
  if (agents.length === 0) return null;
  return (
    <div
      data-testid="canvas-seat-roster"
      className="card-frost pointer-events-auto absolute bottom-3 left-1/2 z-20 flex -translate-x-1/2 items-center gap-1.5 rounded-full border border-[var(--color-border-subtle)] px-2 py-1 shadow-[var(--shadow-card)]"
    >
      <span className="px-1 text-[9px] font-semibold uppercase tracking-[.5px] text-[var(--color-text-3)]">Seats</span>
      {agents.map((a) => (
        <span
          key={a.id}
          className="inline-flex items-center gap-1.5 rounded-full bg-[var(--color-bg-surface)] py-0.5 pl-0.5 pr-2"
          title={`${a.id} — ${a.roles.join(" · ") || "seat"}`}
        >
          <span className="grid h-5 w-5 place-items-center rounded-full bg-[var(--color-bg-inset)]">
            <Ant caste={casteForRoles(a.roles)} size={13} />
          </span>
          <span className="mono text-[10.5px] font-[650] text-[var(--color-text-1)]">{a.id}</span>
          {a.roles.map((r) => {
            const c = ROLE_COLOR[r] ?? { main: "var(--color-text-3)", ink: "var(--color-text-2)" };
            return (
              <span
                key={r}
                className="rounded-full px-1.5 text-[8.5px] font-semibold uppercase tracking-[.3px]"
                style={{ color: c.ink, background: `color-mix(in srgb, ${c.main} 15%, transparent)` }}
              >
                {r}
              </span>
            );
          })}
        </span>
      ))}
    </div>
  );
}
