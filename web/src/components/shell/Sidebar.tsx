import { useMemo, useState } from "react";
import type { ProjectMeta } from "../../lib/types";
import type { View } from "../TopBar";
import { Icon } from "../../lib/icons";
import { AddProjectWizard } from "./AddProjectWizard";
import { deleteRegistry } from "../../lib/api";
import { Modal } from "../ui/Modal";
import { Button } from "../ui/Button";

// Sidebar — the macOS source list (Option A shell). Holds the primary
// navigation axis (Projects). Collapsible to a function-icon rail (NOT project
// initials). Light theme, English copy.
//
// NOTE (IA v2): machine-level destinations (Settings = Agents/registry · Recipes
// · Setup) were removed from the footer when the dashboard reduced to three
// lenses (Board/Canvas/Live). The proper Settings sheet returns via a later task.

export function Sidebar({
  projects,
  current,
  onSelect,
  view,
  onView,
  collapsed,
  onToggleCollapse,
  onChanged,
}: {
  projects: ProjectMeta[];
  current: string;
  onSelect: (id: string) => void;
  view: View;
  onView: (v: View) => void;
  collapsed: boolean;
  onToggleCollapse: () => void;
  onChanged?: () => void;
}) {
  const [wizardOpen, setWizardOpen] = useState(false);
  const [toDelete, setToDelete] = useState<ProjectMeta | null>(null);
  const [deleting, setDeleting] = useState(false);

  // Every lens (board/canvas/live) means "Projects" is the active context.
  const onProjects = view === "board" || view === "canvas" || view === "live";

  async function confirmDelete() {
    if (!toDelete) return;
    setDeleting(true);
    try {
      await deleteRegistry(toDelete.id);
      onChanged?.();
    } catch { /* error surfaced by toast in a full app; here we just close */ }
    setDeleting(false);
    setToDelete(null);
  }

  if (collapsed) {
    return (
      <aside className="flex w-[50px] shrink-0 flex-col items-center gap-1 border-r border-[var(--color-border-subtle)] bg-[color-mix(in_srgb,var(--color-bg-inset)_60%,var(--color-bg-surface))] py-2.5">
        <RailIcon title="Show sidebar" onClick={onToggleCollapse}><SidebarGlyph /></RailIcon>
        <RailIcon title="Projects" active={onProjects} onClick={onToggleCollapse}>
          <Icon name="view-kanban" size={16} color={onProjects ? "var(--color-role-design-ink)" : "var(--color-text-3)"} />
        </RailIcon>
      </aside>
    );
  }

  return (
    <>
      <aside className="flex w-[188px] shrink-0 flex-col border-r border-[var(--color-border-subtle)] bg-[color-mix(in_srgb,var(--color-bg-inset)_60%,var(--color-bg-surface))] py-3">
        <div className="flex-1 overflow-y-auto px-2">
          <div className="mb-1 flex items-center justify-between px-1.5">
            <span className="text-[10px] font-semibold uppercase tracking-[.5px] text-[var(--color-text-3)]">Projects</span>
            <div className="flex items-center gap-0.5">
              <button
                type="button"
                data-testid="sidebar-add-project"
                onClick={() => setWizardOpen(true)}
                title="Create project"
                aria-label="create project"
                className="grid h-5 w-5 place-items-center rounded text-[var(--color-text-3)] transition-colors hover:bg-[var(--color-bg-inset)] hover:text-[var(--color-role-design)]"
              >
                <PlusGlyph />
              </button>
              <button type="button" onClick={onToggleCollapse} title="Hide sidebar" aria-label="hide sidebar"
                className="grid h-5 w-5 place-items-center rounded text-[var(--color-text-3)] transition-colors hover:bg-[var(--color-bg-inset)] hover:text-[var(--color-text-1)]">
                <ChevronLeft />
              </button>
            </div>
          </div>
          <ProjectList projects={projects} current={current} onSelect={onSelect} onView={onView} onProjects={onProjects} onRequestDelete={setToDelete} />
        </div>
      </aside>

      <AddProjectWizard open={wizardOpen} onClose={() => setWizardOpen(false)} onAdded={() => { setWizardOpen(false); onChanged?.(); }} />

      {toDelete && (
        <Modal
          testId="delete-project-modal"
          variant="danger"
          title="Remove project?"
          onClose={() => setToDelete(null)}
          footer={
            <>
              <Button variant="ghost" size="sm" onClick={() => setToDelete(null)} disabled={deleting}>Cancel</Button>
              <Button data-testid="delete-project-confirm" variant="danger" size="sm" loading={deleting} onClick={confirmDelete}>Remove</Button>
            </>
          }
        >
          <p className="text-[12px] text-[var(--color-text-2)]">
            Remove <span className="font-medium text-[var(--color-text-1)]">{toDelete.name || toDelete.id}</span> from the dashboard? Its files and <code className="mono">.pact/</code> directory will not be touched.
          </p>
        </Modal>
      )}
    </>
  );
}

function ProjectList({
  projects,
  current,
  onSelect,
  onView,
  onProjects,
  onRequestDelete,
}: {
  projects: ProjectMeta[];
  current: string;
  onSelect: (id: string) => void;
  onView: (v: View) => void;
  onProjects: boolean;
  onRequestDelete: (p: ProjectMeta) => void;
}) {
  const { grouped, ungrouped } = useMemo(() => {
    const grouped = new Map<string, ProjectMeta[]>();
    const ungrouped: ProjectMeta[] = [];
    for (const p of projects) {
      if (p.group) {
        const list = grouped.get(p.group) ?? [];
        list.push(p);
        grouped.set(p.group, list);
      } else {
        ungrouped.push(p);
      }
    }
    return { grouped, ungrouped };
  }, [projects]);

  const [expanded, setExpanded] = useState<Set<string>>(new Set());

  function toggleGroup(name: string) {
    setExpanded((prev) => {
      const next = new Set(prev);
      if (next.has(name)) next.delete(name);
      else next.add(name);
      return next;
    });
  }

  return (
    <div className="flex flex-col gap-0.5">
      {ungrouped.map((p) => (
        <ProjectRow
          key={p.id}
          project={p}
          active={current === p.id && onProjects}
          onSelect={onSelect}
          onView={onView}
          onProjects={onProjects}
          onRequestDelete={onRequestDelete}
        />
      ))}
      {Array.from(grouped.entries()).map(([group, items]) => (
        <div key={group} data-testid={`sidebar-group-${group}`}>
          <button
            type="button"
            onClick={() => toggleGroup(group)}
            className="group flex w-full items-center gap-1.5 rounded-md px-1.5 py-1 text-left transition-colors hover:bg-[var(--color-bg-inset)]"
          >
            <span className="text-[10px] text-[var(--color-text-3)] transition-transform" style={{ transform: expanded.has(group) ? "rotate(90deg)" : "rotate(0deg)" }}>
              <ChevronRight />
            </span>
            <span className="flex-1 truncate text-[11px] font-semibold text-[var(--color-text-2)]">{group}</span>
          </button>
          {expanded.has(group) && (
            <div className="ml-2 flex flex-col gap-0.5 border-l border-[var(--color-border-subtle)] pl-1.5">
              {items.map((p) => (
                <ProjectRow
                  key={p.id}
                  project={p}
                  active={current === p.id && onProjects}
                  onSelect={onSelect}
                  onView={onView}
                  onProjects={onProjects}
                  onRequestDelete={onRequestDelete}
                />
              ))}
            </div>
          )}
        </div>
      ))}
    </div>
  );
}

function ProjectRow({
  project,
  active,
  onSelect,
  onView,
  onProjects,
  onRequestDelete,
}: {
  project: ProjectMeta;
  active: boolean;
  onSelect: (id: string) => void;
  onView: (v: View) => void;
  onProjects: boolean;
  onRequestDelete: (p: ProjectMeta) => void;
}) {
  return (
    <div
      key={project.id}
      data-testid={`sidebar-project-${project.id}`}
      onClick={() => { onSelect(project.id); if (!onProjects) onView("canvas"); }}
      role="button"
      tabIndex={0}
      onKeyDown={(e) => { if (e.key === "Enter" || e.key === " ") { e.preventDefault(); onSelect(project.id); if (!onProjects) onView("canvas"); }}}
      className="group flex cursor-pointer items-center gap-2 rounded-md px-1.5 py-1 text-left transition-colors hover:bg-[var(--color-bg-inset)]"
      style={active ? { background: "color-mix(in srgb, var(--color-role-design) 14%, transparent)" } : undefined}
    >
      <span className="grid h-5 w-5 shrink-0 place-items-center rounded text-[10px] font-bold"
        style={{ background: active ? "color-mix(in srgb, var(--color-role-design) 22%, transparent)" : "var(--color-bg-inset)", color: active ? "var(--color-role-design-ink)" : "var(--color-text-3)" }}>
        {(project.name || project.id)[0]?.toUpperCase()}
      </span>
      <span className={`flex-1 truncate text-[12px] ${active ? "font-[650] text-[var(--color-text-1)]" : "text-[var(--color-text-2)]"}`}>{project.name || project.id}</span>
      <button
        type="button"
        data-testid={`sidebar-delete-${project.id}`}
        onClick={(e) => { e.stopPropagation(); onRequestDelete(project); }}
        aria-label={`remove ${project.name || project.id}`}
        className="grid h-5 w-5 place-items-center rounded text-[var(--color-text-3)] opacity-0 transition-colors hover:bg-[var(--color-bg-surface)] hover:text-[var(--color-danger)] group-hover:opacity-100"
      >
        <TrashGlyph />
      </button>
    </div>
  );
}

function RailIcon({ children, title, active, onClick }: { children: React.ReactNode; title: string; active?: boolean; onClick?: () => void }) {
  return (
    <button type="button" title={title} aria-label={title} onClick={onClick}
      className="grid h-9 w-9 place-items-center rounded-lg transition-colors hover:bg-[var(--color-bg-inset)]"
      style={active ? { background: "color-mix(in srgb, var(--color-role-design) 14%, transparent)" } : undefined}>
      {children}
    </button>
  );
}

function SidebarGlyph() {
  return (
    <svg width="16" height="16" viewBox="0 0 16 16" fill="none" aria-hidden className="text-[var(--color-text-2)]">
      <rect x="2" y="3" width="12" height="10" rx="2" stroke="currentColor" strokeWidth="1.4" />
      <line x1="6" y1="3.5" x2="6" y2="12.5" stroke="currentColor" strokeWidth="1.4" />
    </svg>
  );
}
function ChevronLeft() {
  return <svg width="13" height="13" viewBox="0 0 16 16" fill="none"><path d="M10 3 L5.5 8 L10 13" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round" /></svg>;
}
function ChevronRight() {
  return <svg width="10" height="10" viewBox="0 0 16 16" fill="none"><path d="M6 3 L10.5 8 L6 13" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round" /></svg>;
}
function PlusGlyph() {
  return <svg width="12" height="12" viewBox="0 0 16 16" fill="none"><path d="M8 3 V13 M3 8 H13" stroke="currentColor" strokeWidth="1.6" strokeLinecap="round" /></svg>;
}
function TrashGlyph() {
  return <svg width="12" height="12" viewBox="0 0 16 16" fill="none"><path d="M3 4 H13 M6 4 V2 H10 V4 M5 4 V13 H11 V4" stroke="currentColor" strokeWidth="1.4" strokeLinecap="round" strokeLinejoin="round" /></svg>;
}
