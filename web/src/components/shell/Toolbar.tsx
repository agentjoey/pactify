import { useMemo } from "react";
import type { Seat, ProjectMeta } from "../../lib/types";
import { CableMark } from "./CableMark";
import { Ant } from "../ui/ants/Ant";
import { casteForRoles, padGradient } from "../../lib/ants";
import { ProjectMenu } from "./ProjectMenu";
import type { Worktree } from "../../lib/api";

// Toolbar — the macOS-style unified toolbar (Option A shell). Holds the brand +
// the ProjectMenu dropdown (project switching / rename / delete / add lives here
// after IA v2), a ⌘K affordance, the live badge, the ⚙ Settings and 👤 Profile
// buttons, and the acting-seat avatar. Single-view IA (PR2 of the views
// consolidation): the Board is the only lens, so the segmented control is gone.


export function Toolbar({
  projectName,
  live,
  author,
  seat,
  agents,
  projects,
  running,
  runningByProject,
  onSelectProject,
  onRenameProject,
  onDeleteProject,
  onAddProject,
  onOpenSettings,
  onOpenDispatch,
  onToggleCockpit,
  showCockpit,
  worktreesByProject,
  currentWorktree,
  onSelectWorktree,
  profileEmail,
}: {
  projectName: string;
  live: boolean;
  author: boolean;
  seat?: string;
  agents?: Seat[];
  projects: ProjectMeta[];
  running: boolean;
  runningByProject?: Record<string, boolean>;
  onSelectProject: (name: string) => void;
  onRenameProject: (name: string) => void;
  onDeleteProject: (name: string) => void;
  onAddProject: () => void;
  onOpenSettings: () => void;
  onOpenDispatch: () => void;
  onToggleCockpit?: () => void;
  showCockpit?: boolean;
  worktreesByProject?: Record<string, Worktree[]>;
  currentWorktree?: string;
  onSelectWorktree?: (project: string, branch: string) => void;
  profileEmail?: string;
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
      className="relative z-50 flex min-h-[46px] items-center gap-3 border-b border-[var(--color-border-subtle)] bg-[color-mix(in_srgb,var(--color-bg-surface)_82%,var(--color-bg-page))] px-3.5 py-2.5 backdrop-blur-[12px]"
    >
      {/* brand + project menu */}
      <div className="flex items-center gap-2 shrink-0">
        <CableMark />
        <span className="text-[13px] font-[680] text-[var(--color-text-1)]">pactify</span>
        <span className="text-[var(--color-text-3)]">·</span>
        <ProjectMenu
          projects={projects}
          current={projectName}
          running={running}
          runningByProject={runningByProject}
          worktreesByProject={worktreesByProject}
          currentWorktree={currentWorktree}
          onSelect={onSelectProject}
          onRename={onRenameProject}
          onDelete={onDeleteProject}
          onAdd={onAddProject}
          onSelectWorktree={onSelectWorktree}
        />
      </div>

      <div className="mx-auto" />

      {/* right cluster: ⌘K · live · seat · ⚙ settings · 👤 profile */}
      <button
        type="button"
        data-testid="cmdk-hint"
        aria-label="command palette"
        onClick={() => window.dispatchEvent(new CustomEvent("pactify:cmdk"))}
        className="rounded-md border border-[var(--color-border-subtle)] bg-[var(--color-bg-inset)] px-1.5 py-1 text-[11px] text-[var(--color-text-3)] transition-colors hover:text-[var(--color-text-1)]"
      >
        ⌘K
      </button>
      {showCockpit && onToggleCockpit && (
        <button
          type="button"
          data-testid="cockpit-toggle"
          aria-label="toggle cockpit"
          title="Cockpit"
          onClick={onToggleCockpit}
          className="rounded-md border border-[var(--color-border-subtle)] bg-[var(--color-bg-inset)] px-2 py-1 text-[11px] text-[var(--color-text-3)] transition-colors hover:text-[var(--color-text-1)]"
        >
          Cockpit
        </button>
      )}
      <LiveBadge live={live} />
      {author && seat && pad ? (
        <button
          type="button"
          data-testid="seat-avatar"
          aria-label={`acting as ${seat}`}
          className="grid h-[24px] w-[24px] shrink-0 place-items-center rounded-[7px] border-0 p-0"
          style={{ background: `linear-gradient(135deg, ${pad.from}, ${pad.to})`, boxShadow: "inset 0 0 0 1px rgba(255,255,255,.16)" }}
          title={`acting as ${seat} — ${caste}`}
        >
          <Ant caste={caste} size={22} />
        </button>
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
      <button
        type="button"
        data-testid="toolbar-dispatch"
        aria-label="dispatch"
        title="Dispatch a task from a goal"
        onClick={onOpenDispatch}
        className="grid h-[26px] w-[26px] place-items-center rounded-md border border-[var(--color-border-subtle)] text-[13px] text-[var(--color-text-3)] transition-colors hover:text-[var(--color-text-1)]"
      >
        ◉
      </button>
      <button
        type="button"
        data-testid="toolbar-settings"
        aria-label="settings"
        title="Settings"
        onClick={onOpenSettings}
        className="grid h-[26px] w-[26px] place-items-center rounded-md border border-[var(--color-border-subtle)] text-[13px] text-[var(--color-text-3)] transition-colors hover:text-[var(--color-text-1)]"
      >
        ⚙
      </button>
      <button
        type="button"
        data-testid="toolbar-profile"
        aria-label={profileEmail ? `signed in as ${profileEmail}` : "profile"}
        title={profileEmail ? profileEmail : "Profile"}
        onClick={onOpenSettings}
        disabled={!profileEmail}
        className={[
          "grid h-[26px] place-items-center rounded-md border border-[var(--color-border-subtle)] text-[13px] transition-colors",
          profileEmail
            ? "px-2 text-[var(--color-text-2)] hover:text-[var(--color-text-1)]"
            : "w-[26px] cursor-not-allowed text-[var(--color-text-3)] opacity-60",
        ].join(" ")}
      >
        {profileEmail ? profileEmail : "👤"}
      </button>
    </div>
  );
}

// LiveBadge — two states (light theme): live = green breathing dot,
// offline = gray.
function LiveBadge({ live }: { live: boolean }) {
  const map = live
    ? { c: "var(--color-success)", label: "live", dot: true }
    : { c: "var(--color-text-3)", label: "offline", dot: false };
  return (
    <span
      data-testid="live-badge"
      data-state={live ? "live" : "offline"}
      className="inline-flex items-center gap-1.5 rounded-full px-2 py-0.5 text-[10.5px] font-medium"
      style={{ color: map.c, background: `color-mix(in srgb, ${map.c} 12%, transparent)` }}
    >
      <span className={map.dot ? "topbar-live-dot" : ""} style={{ width: 5, height: 5, borderRadius: 999, background: map.c, display: "inline-block" }} />
      {map.label}
    </span>
  );
}
