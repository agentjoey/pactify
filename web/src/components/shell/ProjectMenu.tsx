import { useMemo, useState } from "react";
import type { ProjectMeta } from "../../lib/types";

export function ProjectMenu({
  projects,
  current,
  running,
  onSelect,
  onRename,
  onDelete,
  onAdd,
}: {
  projects: ProjectMeta[];
  current: string;
  running: boolean;
  onSelect: (name: string) => void;
  onRename: (name: string) => void;
  onDelete: (name: string) => void;
  onAdd: () => void;
}) {
  const [open, setOpen] = useState(false);
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
    <div className="relative">
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
        {current} ▾
      </button>
      {open && (
        <div
          data-testid="project-menu"
          className="absolute left-0 top-full z-30 mt-1 w-56 rounded-lg border border-[var(--color-border-subtle)] bg-[var(--color-bg-overlay)] p-1 shadow-[var(--shadow-overlay)]"
        >
          {flat.map((p) => (
            <ProjectRow key={p.name} p={p} onSelect={(n) => { onSelect(n); setOpen(false); }} onRename={onRename} onDelete={onDelete} />
          ))}
          {grouped.map(([group, items]) => (
            <div key={group} className="mt-1">
              <div className="px-2 py-1 text-[10px] uppercase tracking-wide text-[var(--color-text-3)]">{group}</div>
              {items.map((p) => (
                <ProjectRow key={p.name} p={p} onSelect={(n) => { onSelect(n); setOpen(false); }} onRename={onRename} onDelete={onDelete} />
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

function ProjectRow({
  p, onSelect, onRename, onDelete,
}: {
  p: ProjectMeta;
  onSelect: (name: string) => void;
  onRename: (name: string) => void;
  onDelete: (name: string) => void;
}) {
  return (
    <div className="group flex items-center rounded-md px-2 py-1.5 text-[12px] hover:bg-white/5">
      <button type="button" className="flex-1 text-left" onClick={() => onSelect(p.name)}>{p.name}</button>
      <button type="button" aria-label={`rename ${p.name}`} className="px-1 opacity-0 group-hover:opacity-100" onClick={() => onRename(p.name)}>✎</button>
      <button type="button" aria-label={`delete ${p.name}`} className="px-1 opacity-0 group-hover:opacity-100" onClick={() => onDelete(p.name)}>🗑</button>
    </div>
  );
}
