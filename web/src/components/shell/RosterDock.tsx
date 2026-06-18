import { useMemo } from "react";
import type { Seat } from "../../lib/types";
import { AgentLogo } from "../../lib/agentLogos";

// Role display order, top → bottom. Each seat is placed once, under its
// highest-priority role (claude = orchestrator+reviewer → the orchestrator row).
const ROLE_ORDER = ["orchestrator", "reviewer", "worker"];

function topRole(s: Seat): string {
  for (const r of ROLE_ORDER) if (s.roles.includes(r)) return r;
  return s.roles[0] ?? "seat";
}

// RosterDock — a single floating card listing seated agents grouped by role,
// each row a short role label + that role's agent logos. Clicking a logo opens
// the seat's settings. Card 1 of the floating left dock (PlanDock is card 2);
// positioning + the gap from the header live in the App-side container.
export function RosterDock({
  seats,
  onSeatSettings,
}: {
  seats: Seat[];
  onSeatSettings: (seatId: string) => void;
}) {
  const groups = useMemo(() => {
    const byRole = new Map<string, Seat[]>();
    for (const s of seats) {
      const r = topRole(s);
      const arr = byRole.get(r) ?? [];
      arr.push(s);
      byRole.set(r, arr);
    }
    const known = ROLE_ORDER.filter((r) => byRole.has(r));
    const extra = [...byRole.keys()].filter((r) => !ROLE_ORDER.includes(r));
    return [...known, ...extra].map((role) => ({ role, seats: byRole.get(role)! }));
  }, [seats]);

  if (seats.length === 0) return null;

  return (
    <div
      data-testid="roster-dock"
      className="pointer-events-auto rounded-2xl border border-white/10 bg-[var(--color-bg-overlay)]/60 p-3 shadow-[var(--shadow-overlay)] backdrop-blur-md"
    >
      <div className="flex flex-col gap-2.5">
        {groups.map((g) => (
          <div key={g.role} data-testid={`roster-role-${g.role}`} className="flex items-center gap-2">
            <span className="w-[74px] shrink-0 whitespace-nowrap text-[9px] font-medium uppercase tracking-wide text-[var(--color-text-3)]">
              {g.role}
            </span>
            <div className="flex flex-wrap items-center gap-1.5">
              {g.seats.map((s) => (
                <button
                  key={s.id}
                  type="button"
                  data-testid={`roster-logo-${s.id}`}
                  title={`${s.id} · ${s.roles.join(", ")}`}
                  aria-label={`settings for ${s.id}`}
                  onClick={() => onSeatSettings(s.id)}
                  className="rounded-lg transition-transform hover:scale-110"
                >
                  <AgentLogo kind={s.kind ?? ""} size={24} />
                </button>
              ))}
            </div>
          </div>
        ))}
      </div>
    </div>
  );
}
