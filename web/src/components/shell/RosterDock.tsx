import { useMemo } from "react";
import type { Seat } from "../../lib/types";
import { AgentLogo } from "../../lib/agentLogos";

// Role display order, top → bottom. Each seat is placed once, under its
// highest-priority role (claude = orchestrator+reviewer → the orchestrator row).
const ROLE_ORDER = ["orchestrator", "reviewer", "worker"];

function topRole(s: Seat): string {
  for (const r of ROLE_ORDER) if ((s.roles ?? []).includes(r)) return r;
  return (s.roles ?? [])[0] ?? "seat";
}

// RosterDock — a refined "seated agents" card: a vertical identity list (logo +
// name + role tag), grouped by role with a thin divider between groups. Clicking
// a row opens that seat's settings. Card 1 of the floating left dock (PlanDock is
// card 2); positioning lives in the App-side container.
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
      className="pointer-events-auto w-full rounded-2xl border border-[var(--color-border-subtle)] bg-[var(--color-bg-overlay)]/70 p-2.5 shadow-[var(--shadow-overlay)] backdrop-blur-md"
    >
      <div className="mb-1.5 px-1 text-[9.5px] font-semibold uppercase tracking-[0.6px] text-[var(--color-text-3)]">
        Seated
      </div>
      <div className="flex flex-col">
        {groups.map((g, gi) => (
          <div key={g.role} data-testid={`roster-role-${g.role}`}>
            {gi > 0 && <div className="mx-1 my-1 h-px bg-[var(--color-border-subtle)]" />}
            {g.seats.map((s) => (
              <button
                key={s.id}
                type="button"
                data-testid={`roster-logo-${s.id}`}
                title={`${s.id} · ${(s.roles ?? []).join(", ")} — open seat settings`}
                aria-label={`settings for ${s.id}`}
                onClick={() => onSeatSettings(s.id)}
                className="flex w-full items-center gap-2 rounded-lg px-1 py-1 text-left transition-colors hover:bg-white/[.06]"
              >
                <AgentLogo kind={s.kind ?? ""} size={20} />
                <span className="min-w-0 flex-1 truncate text-[12px] font-medium text-[var(--color-text-1)]">
                  {s.id}
                </span>
                <span className="shrink-0 text-[9px] font-medium uppercase tracking-wide text-[var(--color-text-3)]">
                  {g.role}
                </span>
              </button>
            ))}
          </div>
        ))}
      </div>
    </div>
  );
}
