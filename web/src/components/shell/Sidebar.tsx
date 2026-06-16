import type { ProjectMeta } from "../../lib/types";
import type { View } from "../TopBar";
import { Icon } from "../../lib/icons";

// Sidebar — the macOS source list (Option A shell). Holds the primary
// navigation axis (Projects) plus machine-level destinations in the footer
// (Settings = Agents/registry · Recipes · Setup). Collapsible to a function-icon
// rail (NOT project initials). Light theme, English copy.
//
// NOTE (interim): Settings/Recipes/Setup currently route to the existing
// `ops`/`recipes`/`setup` views. The proper Settings sheet (hosting Agents +
// Recipes) and the project-level Setup sheet are the next increment.

export function Sidebar({
  projects,
  current,
  onSelect,
  view,
  onView,
  collapsed,
  onToggleCollapse,
}: {
  projects: ProjectMeta[];
  current: string;
  onSelect: (id: string) => void;
  view: View;
  onView: (v: View) => void;
  collapsed: boolean;
  onToggleCollapse: () => void;
}) {
  // a lens view (canvas/kanban/live/plan) means "Projects" is the active context.
  const onProjects = view === "canvas" || view === "kanban" || view === "live" || view === "plan";

  if (collapsed) {
    return (
      <aside className="flex w-[50px] shrink-0 flex-col items-center gap-1 border-r border-[var(--color-border-subtle)] bg-[color-mix(in_srgb,var(--color-bg-inset)_60%,var(--color-bg-surface))] py-2.5">
        <RailIcon title="Show sidebar" onClick={onToggleCollapse}><SidebarGlyph /></RailIcon>
        <RailIcon title="Projects" active={onProjects} onClick={onToggleCollapse}>
          <Icon name="view-kanban" size={16} color={onProjects ? "var(--color-role-design-ink)" : "var(--color-text-3)"} />
        </RailIcon>
        <RailIcon title="Setup" active={view === "setup"} onClick={() => onView("setup")}>
          <Icon name="view-setup" size={16} color={view === "setup" ? "var(--color-role-design-ink)" : "var(--color-text-3)"} />
        </RailIcon>
        <RailIcon title="Recipes" active={view === "recipes"} onClick={() => onView("recipes")}>
          <Icon name="view-recipes" size={16} color={view === "recipes" ? "var(--color-role-design-ink)" : "var(--color-text-3)"} />
        </RailIcon>
        <div className="mt-auto">
          <RailIcon title="Settings" active={view === "ops"} onClick={() => onView("ops")}>
            <Icon name="view-ops" size={16} color={view === "ops" ? "var(--color-role-design-ink)" : "var(--color-text-3)"} />
          </RailIcon>
        </div>
      </aside>
    );
  }

  return (
    <aside className="flex w-[188px] shrink-0 flex-col border-r border-[var(--color-border-subtle)] bg-[color-mix(in_srgb,var(--color-bg-inset)_60%,var(--color-bg-surface))] py-3">
      <div className="flex-1 overflow-y-auto px-2">
        <div className="mb-1 flex items-center justify-between px-1.5">
          <span className="text-[10px] font-semibold uppercase tracking-[.5px] text-[var(--color-text-3)]">Projects</span>
          <button type="button" onClick={onToggleCollapse} title="Hide sidebar" aria-label="hide sidebar"
            className="grid h-5 w-5 place-items-center rounded text-[var(--color-text-3)] transition-colors hover:bg-[var(--color-bg-inset)] hover:text-[var(--color-text-1)]">
            <ChevronLeft />
          </button>
        </div>
        <div className="flex flex-col gap-0.5">
          {projects.map((p) => {
            const active = current === p.id && onProjects;
            return (
              <button
                key={p.id}
                type="button"
                data-testid={`sidebar-project-${p.id}`}
                onClick={() => { onSelect(p.id); if (!onProjects) onView("canvas"); }}
                className="flex items-center gap-2 rounded-md px-1.5 py-1 text-left transition-colors hover:bg-[var(--color-bg-inset)]"
                style={active ? { background: "color-mix(in srgb, var(--color-role-design) 14%, transparent)" } : undefined}
              >
                <span className="grid h-5 w-5 shrink-0 place-items-center rounded text-[10px] font-bold"
                  style={{ background: active ? "color-mix(in srgb, var(--color-role-design) 22%, transparent)" : "var(--color-bg-inset)", color: active ? "var(--color-role-design-ink)" : "var(--color-text-3)" }}>
                  {(p.name || p.id)[0]?.toUpperCase()}
                </span>
                <span className={`flex-1 truncate text-[12px] ${active ? "font-[650] text-[var(--color-text-1)]" : "text-[var(--color-text-2)]"}`}>{p.name || p.id}</span>
              </button>
            );
          })}
        </div>
      </div>

      {/* footer — machine-level destinations */}
      <div className="flex flex-col gap-0.5 px-2 pt-1">
        <FooterItem label="Setup" icon="view-setup" active={view === "setup"} onClick={() => onView("setup")} />
        <FooterItem label="Recipes" icon="view-recipes" active={view === "recipes"} onClick={() => onView("recipes")} />
        <FooterItem label="Settings" icon="view-ops" active={view === "ops"} onClick={() => onView("ops")} />
      </div>
    </aside>
  );
}

function FooterItem({ label, icon, active, onClick }: { label: string; icon: string; active: boolean; onClick: () => void }) {
  return (
    <button type="button" onClick={onClick}
      className="flex items-center gap-2 rounded-md px-1.5 py-1 text-left transition-colors hover:bg-[var(--color-bg-inset)]"
      style={active ? { background: "color-mix(in srgb, var(--color-role-design) 14%, transparent)" } : undefined}>
      <span className="grid h-5 w-5 shrink-0 place-items-center">
        <Icon name={icon} size={14} color={active ? "var(--color-role-design-ink)" : "var(--color-text-3)"} />
      </span>
      <span className={`text-[12px] ${active ? "font-[650] text-[var(--color-text-1)]" : "text-[var(--color-text-2)]"}`}>{label}</span>
    </button>
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
  return <svg width="13" height="13" viewBox="0 0 16 16" fill="none" aria-hidden><path d="M10 3 L5.5 8 L10 13" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round" /></svg>;
}
