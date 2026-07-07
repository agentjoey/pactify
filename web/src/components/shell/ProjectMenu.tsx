import { useEffect, useMemo, useRef, useState } from "react";
import type { ProjectMeta } from "../../lib/types";
import type { Worktree } from "../../lib/api";

export function ProjectMenu({
  projects,
  current,
  running,
  runningByProject,
  worktreesByProject,
  currentWorktree,
  onSelect,
  onRename,
  onDelete,
  onAdd,
  onSelectWorktree,
}: {
  projects: ProjectMeta[];
  current: string;
  running: boolean;
  runningByProject?: Record<string, boolean>;
  worktreesByProject?: Record<string, Worktree[]>;
  currentWorktree?: string;
  onSelect: (name: string) => void;
  onRename: (name: string) => void;
  onDelete: (name: string) => void;
  onAdd: () => void;
  onSelectWorktree?: (project: string, branch: string) => void;
}) {
  const [open, setOpen] = useState(false);
  const rootRef = useRef<HTMLDivElement>(null);

  // Dismiss the dropdown on any click outside its root (standard header-menu UX).
  useEffect(() => {
    if (!open) return;
    const onDown = (e: MouseEvent) => {
      if (rootRef.current && !rootRef.current.contains(e.target as Node)) setOpen(false);
    };
    document.addEventListener("mousedown", onDown);
    return () => document.removeEventListener("mousedown", onDown);
  }, [open]);

  const { grouped, flat } = useMemo(() => {
    const g = new Map<string, ProjectMeta[]>();
    const f: ProjectMeta[] = [];
    for (const p of projects) {
      if (p.group) {
        const arr = g.get(p.group) ?? [];
        arr.push(p);
        g.set(p.group, arr);
      } else {
        f.push(p);
      }
    }
    return { grouped: [...g.entries()], flat: f };
  }, [projects]);

  return (
    <div className="relative" ref={rootRef}>
      <button
        type="button"
        data-testid="project-menu-trigger"
        onClick={() => setOpen((o) => !o)}
        className="inline-flex items-center gap-1.5 rounded-md border border-[var(--color-border-subtle)] bg-white/5 px-2.5 py-1 text-[13px]"
      >
        <span
          data-testid="project-status-light"
          data-running={running ? "true" : "false"}
          className={[
            "h-[7px] w-[7px] rounded-full",
            running ? "animate-pulse bg-[var(--color-success)] shadow-[0_0_6px_var(--color-success)]" : "bg-[var(--color-text-3)]",
          ].join(" ")}
        />
        {projects.find((p) => p.id === current)?.name ?? current} ▾
      </button>
      {open && (
        <div
          data-testid="project-menu"
          className="absolute left-0 top-full z-30 mt-1 w-56 rounded-lg border border-[var(--color-border-subtle)] bg-[var(--color-bg-overlay)] p-1 shadow-[var(--shadow-overlay)]"
        >
          {flat.map((p) => (
            <ProjectEntry key={p.name} p={p} running={!!runningByProject?.[p.name]} worktrees={worktreesByProject?.[p.name]} currentWorktree={currentWorktree} onSelect={(n) => { onSelect(n); setOpen(false); }} onRename={onRename} onDelete={onDelete} onSelectWorktree={(proj, branch) => { onSelectWorktree?.(proj, branch); setOpen(false); }} />
          ))}
          {grouped.map(([group, items]) => (
            <div key={group} className="mt-1">
              <div className="px-2 py-1 text-[10px] uppercase tracking-wide text-[var(--color-text-3)]">{group}</div>
              {items.map((p) => (
                <ProjectEntry key={p.name} p={p} running={!!runningByProject?.[p.name]} worktrees={worktreesByProject?.[p.name]} currentWorktree={currentWorktree} onSelect={(n) => { onSelect(n); setOpen(false); }} onRename={onRename} onDelete={onDelete} onSelectWorktree={(proj, branch) => { onSelectWorktree?.(proj, branch); setOpen(false); }} />
              ))}
            </div>
          ))}
          <button
            type="button"
            data-testid="project-menu-add"
            onClick={() => { onAdd(); setOpen(false); }}
            className="mt-1 w-full rounded-md px-2 py-1.5 text-left text-[12px] text-[var(--color-text-1)] hover:bg-white/5"
          >
            ＋ Add project
          </button>
        </div>
      )}
    </div>
  );
}

function ProjectEntry({
  p, running, worktrees, currentWorktree, onSelect, onRename, onDelete, onSelectWorktree,
}: {
  p: ProjectMeta;
  running: boolean;
  worktrees?: Worktree[];
  currentWorktree?: string;
  onSelect: (name: string) => void;
  onRename: (name: string) => void;
  onDelete: (name: string) => void;
  onSelectWorktree: (project: string, branch: string) => void;
}) {
  return (
    <div>
      <ProjectRow p={p} running={running} onSelect={onSelect} onRename={onRename} onDelete={onDelete} />
      {worktrees && worktrees.length > 1 && (
        <div className="ml-4 border-l border-[var(--color-border-subtle)] pl-1">
          {worktrees.map((w) => (
            <button
              key={w.branch || w.path}
              type="button"
              data-testid={`worktree-${p.name}-${w.branch}`}
              onClick={() => onSelectWorktree(p.id, w.primary ? "" : w.branch)}
              className={["flex w-full items-center gap-1.5 rounded-md px-2 py-1 text-[11px] hover:bg-white/5",
                ((w.primary && currentWorktree === "") || currentWorktree === w.branch) ? "text-[var(--color-text-1)]" : "text-[var(--color-text-3)]"].join(" ")}
            >
              <span className="h-[5px] w-[5px] shrink-0 rounded-full bg-[var(--color-text-3)]/50" />
              {w.branch || "(detached)"}{w.primary ? " · main" : ""}
            </button>
          ))}
        </div>
      )}
    </div>
  );
}

function ProjectRow({
  p, running, onSelect, onRename, onDelete,
}: {
  p: ProjectMeta;
  running: boolean;
  onSelect: (name: string) => void;
  onRename: (name: string) => void;
  onDelete: (name: string) => void;
}) {
  return (
    <div className="group flex items-center gap-1.5 rounded-md px-2 py-1.5 text-[12px] hover:bg-white/5">
      <span
        data-testid={`project-row-light-${p.name}`}
        data-running={running ? "true" : "false"}
        className={[
          "h-[6px] w-[6px] shrink-0 rounded-full",
          running ? "animate-pulse bg-[var(--color-success)] shadow-[0_0_5px_var(--color-success)]" : "bg-[var(--color-text-3)]/50",
        ].join(" ")}
      />
      <button type="button" className="flex-1 text-left" onClick={() => onSelect(p.id)}>{p.name}</button>
      <button type="button" aria-label={`rename ${p.name}`} className="px-1 opacity-0 group-hover:opacity-100" onClick={() => onRename(p.name)}>✎</button>
      <button type="button" aria-label={`delete ${p.name}`} className="px-1 opacity-0 group-hover:opacity-100" onClick={() => onDelete(p.name)}>🗑</button>
    </div>
  );
}
