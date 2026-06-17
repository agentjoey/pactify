import { useMemo } from "react";
import type { Seat } from "../../lib/types";
import { AgentLogo } from "../../lib/agentLogos";

// RosterDock — floating column of frosted agent cards (top-left of the body).
// The orchestrator seat is pinned first; remaining seats keep roster order.
export function RosterDock({
  seats,
  onSeatSettings,
}: {
  seats: Seat[];
  onSeatSettings: (seatId: string) => void;
}) {
  const ordered = useMemo(() => {
    const isOrch = (s: Seat) => s.roles.includes("orchestrator");
    return [...seats].sort((a, b) => Number(isOrch(b)) - Number(isOrch(a)));
  }, [seats]);

  return (
    <div
      data-testid="roster-dock"
      className="pointer-events-none absolute left-3 top-3 z-20 flex w-[188px] flex-col gap-2"
    >
      <div className="pointer-events-auto text-[10px] uppercase tracking-wide text-[var(--color-text-3)]">
        Seated · {ordered.length}
      </div>
      {ordered.map((s) => (
        <div
          key={s.id}
          data-testid="roster-card"
          className="pointer-events-auto flex items-center gap-2 rounded-xl border border-white/10 bg-[var(--color-bg-overlay)]/55 p-2.5 shadow-[var(--shadow-overlay)] backdrop-blur-md"
        >
          <AgentLogo kind={s.kind ?? ""} size={18} />
          <div className="min-w-0 flex-1">
            <div className="truncate text-[12px] font-semibold text-[var(--color-text-1)]">{s.id}</div>
            <div className="truncate text-[10px] text-[var(--color-text-3)]">{s.roles.join(" · ")}</div>
          </div>
          <button
            type="button"
            data-testid="roster-card-settings"
            aria-label={`settings for ${s.id}`}
            className="text-[var(--color-text-3)] transition-colors hover:text-[var(--color-text-1)]"
            onClick={() => onSeatSettings(s.id)}
          >
            ⚙
          </button>
        </div>
      ))}
    </div>
  );
}
