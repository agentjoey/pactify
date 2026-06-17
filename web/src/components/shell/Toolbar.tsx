import { useMemo } from "react";
import type { Seat } from "../../lib/types";
import type { View } from "../TopBar";
import { CableMark } from "../TopBar";
import { Icon } from "../../lib/icons";
import { Ant } from "../ui/ants/Ant";
import { casteForRoles, padGradient } from "../../lib/ants";

// Toolbar — the macOS-style unified toolbar (Option A shell). Holds the brand +
// current project label, the centered lens segmented control (the four ways to
// view a project: Canvas / Board / Live / Plan), a ⌘K affordance, the live
// badge, and the acting-seat avatar. Project SWITCHING and machine-level
// destinations live in the Sidebar, not here. Light theme, English copy.
//
// The toolbar surfaces the three lenses (Board / Canvas / Live), the reduced
// View union after the IA v2 view reduction.

const LENSES: ReadonlyArray<{ v: View; label: string; icon: string }> = [
  { v: "board", label: "Board", icon: "view-kanban" },
  { v: "canvas", label: "Canvas", icon: "view-canvas" },
  { v: "live", label: "Live", icon: "view-live" },
];

export function Toolbar({
  projectName,
  view,
  onView,
  live,
  replaying,
  author,
  seat,
  agents,
}: {
  projectName: string;
  view: View;
  onView: (v: View) => void;
  live: boolean;
  replaying?: boolean;
  author: boolean;
  seat?: string;
  agents?: Seat[];
}) {
  const seatRoles = useMemo(
    () => (seat ? agents?.find((a) => a.id === seat)?.roles ?? [] : []),
    [agents, seat],
  );
  const caste = casteForRoles(seatRoles);
  const pad = seat ? padGradient(seat, caste) : null;

  return (
    <div
      data-testid="toolbar"
      className="relative z-50 flex items-center gap-3 border-b border-[var(--color-border-subtle)] bg-[color-mix(in_srgb,var(--color-bg-surface)_88%,var(--color-bg-page))] px-3.5 py-2.5 backdrop-blur-[10px]"
    >
      {/* brand + current project */}
      <div className="flex items-center gap-2 shrink-0">
        <CableMark />
        <span className="text-[13px] font-[680] text-[var(--color-text-1)]">pactify</span>
        {projectName && (
          <>
            <span className="text-[var(--color-text-3)]">·</span>
            <span className="mono text-[12px] font-medium text-[var(--color-text-2)]">{projectName}</span>
          </>
        )}
      </div>

      {/* centered lens segmented control */}
      <div role="group" aria-label="lens" className="mx-auto inline-flex rounded-[9px] border border-[var(--color-border-subtle)] bg-[var(--color-bg-inset)] p-0.5">
        {LENSES.map((l) => {
          const on = view === l.v;
          return (
            <button
              key={l.v}
              type="button"
              onClick={() => onView(l.v)}
              aria-pressed={on}
              className="inline-flex items-center gap-1.5 rounded-[7px] px-3 py-1 text-[11.5px] font-medium outline-none transition-colors focus-visible:ring-2 focus-visible:ring-[color-mix(in_srgb,var(--color-role-design)_40%,transparent)]"
              style={on ? { background: "var(--color-bg-surface)", color: "var(--color-text-1)", boxShadow: "var(--shadow-card)" } : { color: "var(--color-text-2)" }}
            >
              <Icon name={l.icon} size={13} color={on ? "var(--color-text-1)" : "var(--color-text-3)"} />
              {l.label}
            </button>
          );
        })}
      </div>

      {/* right cluster: reserved Orchestrate (disabled until B5) · ⌘K · live · seat */}
      <button
        type="button"
        disabled
        title="Needs B5: dashboard-driven orchestration (backend pending)"
        className="inline-flex cursor-not-allowed items-center gap-1.5 rounded-md border border-[var(--color-border-subtle)] px-2.5 py-1 text-[11.5px] font-medium text-[var(--color-text-3)] opacity-60"
      >
        <Icon name="action-run" size={13} color="var(--color-text-3)" /> Orchestrate
      </button>
      <button
        type="button"
        data-testid="cmdk-hint"
        aria-label="command palette"
        onClick={() => window.dispatchEvent(new CustomEvent("pactify:cmdk"))}
        className="rounded-md border border-[var(--color-border-subtle)] bg-[var(--color-bg-inset)] px-1.5 py-1 text-[11px] text-[var(--color-text-3)] transition-colors hover:text-[var(--color-text-1)]"
      >
        ⌘K
      </button>
      <LiveBadge live={live} replaying={!!replaying} />
      {author && seat && pad ? (
        <span
          data-testid="seat-avatar"
          className="grid h-[24px] w-[24px] shrink-0 place-items-center rounded-[7px]"
          style={{ background: `linear-gradient(135deg, ${pad.from}, ${pad.to})`, boxShadow: "inset 0 0 0 1px rgba(18,22,31,.16)" }}
          title={`acting as ${seat} — ${caste}`}
        >
          <Ant caste={caste} size={22} />
        </span>
      ) : !author ? (
        <span
          data-testid="observe-badge"
          title="read-only — start with: pactify serve --seat <id>"
          className="inline-flex items-center gap-1 rounded-full px-2 py-0.5 text-[10.5px] font-medium"
          style={{ color: "var(--color-warn)", background: "color-mix(in srgb, var(--color-warn) 12%, transparent)" }}
        >
          observing
        </span>
      ) : null}
    </div>
  );
}

// LiveBadge — three states (light theme): live = green breathing dot,
// replay = amber, offline = gray.
function LiveBadge({ live, replaying }: { live: boolean; replaying: boolean }) {
  const map = replaying
    ? { c: "var(--color-warn)", label: "replay", dot: false }
    : live
      ? { c: "var(--color-success)", label: "live", dot: true }
      : { c: "var(--color-text-3)", label: "offline", dot: false };
  return (
    <span
      data-testid="live-badge"
      data-state={replaying ? "replay" : live ? "live" : "offline"}
      className="inline-flex items-center gap-1.5 rounded-full px-2 py-0.5 text-[10.5px] font-medium"
      style={{ color: map.c, background: `color-mix(in srgb, ${map.c} 12%, transparent)` }}
    >
      <span className={map.dot ? "topbar-live-dot" : ""} style={{ width: 5, height: 5, borderRadius: 999, background: map.c, display: "inline-block" }} />
      {map.label}
    </span>
  );
}
