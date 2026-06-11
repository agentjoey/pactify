import { useMemo, useState } from "react";
import type { ProjectMeta, Seat } from "../lib/types";
import { Popover } from "./ui/Popover";
import { Input } from "./ui/Input";
import { Kbd } from "./ui/Kbd";
import { Badge } from "./ui/Badge";
import { Tooltip } from "./ui/Tooltip";
import { Ant } from "./ui/ants/Ant";
import { casteForRoles, padGradient } from "../lib/ants";

export type View = "kanban" | "canvas" | "ops";

// The three-color cable mini-mark (board3 `.logo` svg). Also used as the
// favicon (index.html, static data-URI).
function CableMark() {
  return (
    <svg width="22" height="14" viewBox="0 0 30 18" aria-hidden="true">
      <path d="M1 4 C10 4, 14 9, 29 9" stroke="#ECC678" strokeWidth="2" fill="none" strokeLinecap="round" />
      <path d="M1 9 L29 9" stroke="#93B4F2" strokeWidth="2" fill="none" strokeLinecap="round" />
      <path d="M1 14 C10 14, 14 9, 29 9" stroke="#7BD8A0" strokeWidth="2" fill="none" strokeLinecap="round" />
    </svg>
  );
}

const VIEWS: ReadonlyArray<{ v: View; label: string; key: string }> = [
  { v: "kanban", label: "Kanban", key: "1" },
  { v: "canvas", label: "Canvas", key: "2" },
  { v: "ops", label: "Ops", key: "3" },
];

export function TopBar({
  projects,
  current,
  onSelect,
  live,
  replaying,
  view,
  onView,
  author,
  seat,
  agents,
}: {
  projects: ProjectMeta[];
  current: string;
  onSelect: (id: string) => void;
  live: boolean;
  // When true, the dashboard is scrubbing historical state — the live indicator
  // shows "replay" instead of live/offline.
  replaying?: boolean;
  view: View;
  onView: (v: View) => void;
  // Author => this dashboard can act (has an acting seat). When false the
  // observe-only badge replaces the seat avatar.
  author: boolean;
  // The acting seat id (from getActingSeat, fetched in App). Empty when observing.
  seat?: string;
  // Roster — used to resolve the acting seat's roles → caste for its avatar.
  agents?: Seat[];
}) {
  const [projOpen, setProjOpen] = useState(false);
  const [query, setQuery] = useState("");

  const currentName = useMemo(
    () => projects.find((p) => p.id === current)?.name ?? current,
    [projects, current]
  );

  const filtered = useMemo(() => {
    const q = query.trim().toLowerCase();
    if (!q) return projects;
    return projects.filter((p) => p.name.toLowerCase().includes(q) || p.id.toLowerCase().includes(q));
  }, [projects, query]);

  // Resolve the acting seat's caste from the roster (fallback: builder).
  const seatRoles = useMemo(
    () => (seat ? agents?.find((a) => a.id === seat)?.roles ?? [] : []),
    [agents, seat]
  );
  const caste = casteForRoles(seatRoles);
  const pad = seat ? padGradient(seat, caste) : null;

  return (
    <div
      data-testid="topbar"
      className="flex items-center gap-3.5 px-4 py-2.5 border-b border-[var(--color-border-subtle)] bg-[rgba(32,34,47,.7)] backdrop-blur-[10px]"
    >
      {/* logo */}
      <div className="flex items-center gap-2 shrink-0">
        <CableMark />
        <span className="text-[13.5px] font-bold tracking-[.2px] text-[var(--color-text-1)]">pactify</span>
      </div>

      {/* project switcher (chip → Popover with search) */}
      <Popover
        open={projOpen}
        onClose={() => setProjOpen(false)}
        align="left"
        anchor={
          <button
            type="button"
            data-testid="project-chip"
            aria-haspopup="listbox"
            aria-expanded={projOpen}
            onClick={() => setProjOpen((o) => !o)}
            className="flex items-center gap-[7px] rounded-lg border border-[var(--color-border-strong)] bg-[rgba(255,255,255,.05)] px-2.5 py-[5px] text-[12.5px] text-[var(--color-text-1)]"
          >
            <span className="mono text-[11.5px]">{currentName || "—"}</span>
            <span className="text-[9px] text-[var(--color-text-3)]">▾</span>
          </button>
        }
      >
        <div className="w-[220px] p-1" role="listbox" aria-label="projects">
          <Input
            autoFocus
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            placeholder="Search projects…"
            aria-label="search projects"
            className="mb-1"
          />
          <div className="max-h-[240px] overflow-y-auto">
            {filtered.length === 0 ? (
              <div className="px-2 py-2 text-[11px] text-[var(--color-text-3)]">no match</div>
            ) : (
              filtered.map((p) => (
                <button
                  key={p.id}
                  type="button"
                  role="option"
                  aria-selected={p.id === current}
                  onClick={() => {
                    onSelect(p.id);
                    setProjOpen(false);
                    setQuery("");
                  }}
                  className={[
                    "flex w-full items-center justify-between rounded-md px-2 py-1.5 text-left text-xs",
                    "hover:bg-[var(--color-bg-surface)]",
                    p.id === current ? "text-[var(--color-text-1)]" : "text-[var(--color-text-2)]",
                  ].join(" ")}
                >
                  <span className="mono">{p.name}</span>
                  {p.id === current && <span className="text-[var(--color-success)] text-[10px]">●</span>}
                </button>
              ))
            )}
          </div>
        </div>
      </Popover>

      {/* centered segmented control */}
      <div
        role="group"
        aria-label="view toggle"
        className="mx-auto flex rounded-[9px] border border-[var(--color-border-subtle)] bg-[rgba(13,14,20,.5)] p-[3px]"
      >
        {VIEWS.map(({ v, label, key }) => {
          const on = view === v;
          return (
            <button
              key={v}
              type="button"
              onClick={() => onView(v)}
              aria-pressed={on}
              className={[
                "flex items-center gap-[7px] rounded-md px-3.5 py-[4.5px] text-xs font-medium transition-colors",
                on
                  ? "bg-gradient-to-b from-[rgba(255,255,255,.1)] to-[rgba(255,255,255,.06)] text-white shadow-[0_1px_3px_rgba(0,0,0,.3)]"
                  : "text-[var(--color-text-2)]",
              ].join(" ")}
            >
              {label}
              <Kbd>{key}</Kbd>
            </button>
          );
        })}
      </div>

      {/* ⌘K hint — no-op affordance until W3 (dispatches an event W3 will listen for) */}
      <button
        type="button"
        data-testid="cmdk-hint"
        aria-label="command palette"
        onClick={() => window.dispatchEvent(new CustomEvent("pactify:cmdk"))}
        className="flex items-center gap-[5px] rounded-[7px] border border-[var(--color-border-strong)] bg-[rgba(255,255,255,.04)] px-2.5 py-[4.5px] text-[11px] text-[var(--color-text-2)]"
      >
        <span className="text-[10px]">⌘K</span>
      </button>

      {/* live badge — three states */}
      <LiveBadge live={live} replaying={!!replaying} />

      {/* acting seat ant, or observe-only badge */}
      {author && seat && pad ? (
        <div className="flex items-center shrink-0">
          <span
            data-testid="seat-avatar"
            className="flex h-[22px] w-[22px] items-center justify-center rounded-[7px] shrink-0"
            style={{
              background: `linear-gradient(135deg, ${pad.from}, ${pad.to})`,
              boxShadow: "inset 0 0 0 1px rgba(255,255,255,.16)",
            }}
          >
            <Ant caste={caste} size={22} title={`acting as ${seat} — ${caste}`} />
          </span>
        </div>
      ) : !author ? (
        <Tooltip label="read-only — start with: pactify serve --seat <id>" side="bottom">
          <span data-testid="observe-badge">
            <Badge color="warn">👁 observing</Badge>
          </span>
        </Tooltip>
      ) : null}
    </div>
  );
}

// LiveBadge renders the three live states (board3 `.live`): live = green
// breathing dot, replaying = amber "● replay", offline = gray dot.
function LiveBadge({ live, replaying }: { live: boolean; replaying: boolean }) {
  if (replaying) {
    return (
      <span
        data-testid="live-badge"
        data-state="replay"
        className="flex items-center gap-1.5 rounded-full border border-[color-mix(in_srgb,var(--color-warn)_30%,transparent)] bg-[color-mix(in_srgb,var(--color-warn)_10%,transparent)] px-2.5 py-1 text-[11px] text-[var(--color-warn)]"
      >
        <span className="h-1.5 w-1.5 rounded-full bg-[var(--color-warn)]" />
        replay
      </span>
    );
  }
  if (live) {
    return (
      <span
        data-testid="live-badge"
        data-state="live"
        className="flex items-center gap-1.5 rounded-full border border-[color-mix(in_srgb,var(--color-success)_30%,transparent)] bg-[color-mix(in_srgb,var(--color-success)_10%,transparent)] px-2.5 py-1 text-[11px] text-[var(--color-success)]"
      >
        <span className="topbar-live-dot h-1.5 w-1.5 rounded-full bg-[var(--color-success)]" />
        live
      </span>
    );
  }
  return (
    <span
      data-testid="live-badge"
      data-state="offline"
      className="flex items-center gap-1.5 rounded-full border border-[var(--color-border-strong)] bg-[rgba(255,255,255,.03)] px-2.5 py-1 text-[11px] text-[var(--color-text-3)]"
    >
      <span className="h-1.5 w-1.5 rounded-full bg-[var(--color-text-3)]" />
      offline
    </span>
  );
}
