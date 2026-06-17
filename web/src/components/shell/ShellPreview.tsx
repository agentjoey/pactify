import { useState } from "react";
import { Icon } from "../../lib/icons";
import { StatusPill, type PactStatus } from "../ui/StatusPill";
import { Ant } from "../ui/ants/Ant";
import type { Caste } from "../../lib/ants";

// ShellPreview — static mock of the macOS-app shell. Reached at `?shell`. Sample
// data only — NOT wired to real state. English-only UI.
//
// Rev 3 (review feedback):
//  - UI copy is English-only (no mixed CN/EN)
//  - sidebar collapse control lives ON the sidebar (header chevron / rail top),
//    NOT in the toolbar; collapsed rail shows sidebar FUNCTION icons, not the
//    project-name initials
//  - Canvas is the PRIMARY surface (default lens): a dot-grid node graph with
//    task nodes, dependency edges, a working ant, per-node seat assignment, and
//    the always-on seat roster

type Lens = "canvas" | "board" | "live" | "plan";
const LENSES: { id: Lens; label: string; icon: string }[] = [
  { id: "canvas", label: "Canvas", icon: "view-canvas" },
  { id: "board", label: "Board", icon: "view-kanban" },
  { id: "live", label: "Live", icon: "view-live" },
  { id: "plan", label: "Plan", icon: "view-plan" },
];

const PROJECTS = [
  { id: "demo", name: "demo", awaiting: 2 },
  { id: "relay", name: "relay", awaiting: 0 },
  { id: "futu", name: "futu-agent", awaiting: 1 },
];

type RoleKey = "orchestrator" | "reviewer" | "worker";
const ROLE_C: Record<RoleKey, { main: string; ink: string }> = {
  orchestrator: { main: "var(--color-role-product)", ink: "var(--color-role-product-ink)" },
  reviewer: { main: "var(--color-role-design)", ink: "var(--color-role-design-ink)" },
  worker: { main: "var(--color-role-dev)", ink: "var(--color-role-dev-ink)" },
};

type Seat = { id: string; caste: Caste; roles: RoleKey[] };
const SEATS: Seat[] = [
  { id: "claude", caste: "queen", roles: ["orchestrator", "reviewer"] },
  { id: "gemini", caste: "builder", roles: ["worker"] },
  { id: "opencode", caste: "builder", roles: ["worker"] },
];
const casteOf = (id: string): Caste => SEATS.find((s) => s.id === id)?.caste ?? "builder";

type Task = { id: string; owner: string; reviewer: string; status: PactStatus; x: number; y: number };
const NODES: Task[] = [
  { id: "t-schema", owner: "opencode", reviewer: "claude", status: "assigned", x: 24, y: 150 },
  { id: "t-manifest", owner: "opencode", reviewer: "claude", status: "in_progress", x: 256, y: 42 },
  { id: "t-prompt", owner: "gemini", reviewer: "claude", status: "in_progress", x: 256, y: 258 },
  { id: "t-apply", owner: "opencode", reviewer: "claude", status: "awaiting_review", x: 496, y: 150 },
];
const NODE_W = 188;
const NODE_H = 78;
// edges: [sourceId, targetId]
const EDGES: [string, string][] = [
  ["t-schema", "t-manifest"],
  ["t-schema", "t-prompt"],
  ["t-manifest", "t-apply"],
  ["t-prompt", "t-apply"],
];
const node = (id: string) => NODES.find((n) => n.id === id)!;
const rightAnchor = (n: Task) => ({ x: n.x + NODE_W, y: n.y + NODE_H / 2 });
const leftAnchor = (n: Task) => ({ x: n.x, y: n.y + NODE_H / 2 });
const edgePath = (s: Task, t: Task) => {
  const a = rightAnchor(s), b = leftAnchor(t);
  const dx = Math.max(40, (b.x - a.x) / 2);
  return `M ${a.x} ${a.y} C ${a.x + dx} ${a.y}, ${b.x - dx} ${b.y}, ${b.x} ${b.y}`;
};

// Board columns (secondary lens).
const COLUMNS: { title: string; tasks: Task[] }[] = [
  { title: "Assigned", tasks: NODES.filter((n) => n.status === "assigned") },
  { title: "In progress", tasks: NODES.filter((n) => n.status === "in_progress") },
  { title: "Awaiting review", tasks: NODES.filter((n) => n.status === "awaiting_review") },
  { title: "Accepted", tasks: [{ id: "t-init", owner: "opencode", reviewer: "claude", status: "accepted", x: 0, y: 0 }] },
];

export function ShellPreview() {
  const [lens, setLens] = useState<Lens>("canvas");
  const [project, setProject] = useState("demo");
  const [selected, setSelected] = useState<string | null>("t-manifest");
  const [collapsed, setCollapsed] = useState(false);

  return (
    <div className="min-h-screen bg-[var(--color-bg-page)] p-6 text-[var(--color-text-1)]">
      <div className="mx-auto max-w-[1180px]">
        <div className="mb-3 text-[12px] text-[var(--color-text-3)]">
          Pactify · shell mock rev3 — Canvas-primary · sample data · <span className="mono">?shell</span>
        </div>

        <div className="overflow-hidden rounded-[12px] border border-[var(--color-border-subtle)] bg-[var(--color-bg-surface)]"
          style={{ boxShadow: "0 24px 60px -20px rgba(18,22,31,.28), 0 2px 8px rgba(18,22,31,.08)" }}>
          {/* ── unified toolbar (no collapse control here) ── */}
          <div className="flex items-center gap-3 border-b border-[var(--color-border-subtle)] bg-[color-mix(in_srgb,var(--color-bg-surface)_88%,var(--color-bg-page))] px-3.5 py-2.5">
            <div className="flex items-center gap-2 pr-1">
              <span className="h-[11px] w-[11px] rounded-full" style={{ background: "#ff5f57" }} />
              <span className="h-[11px] w-[11px] rounded-full" style={{ background: "#febc2e" }} />
              <span className="h-[11px] w-[11px] rounded-full" style={{ background: "#28c840" }} />
            </div>
            <div className="flex items-center gap-1.5">
              <Icon name="action-merge" size={15} color="var(--color-role-dev-ink)" />
              <span className="text-[12.5px] font-[680]">pactify</span>
              <span className="text-[var(--color-text-3)]">·</span>
              <span className="text-[12.5px] font-medium text-[var(--color-text-2)]">{project}</span>
            </div>

            <div className="mx-auto inline-flex rounded-[9px] border border-[var(--color-border-subtle)] bg-[var(--color-bg-inset)] p-0.5">
              {LENSES.map((l) => {
                const on = lens === l.id;
                return (
                  <button key={l.id} type="button" onClick={() => setLens(l.id)}
                    className="inline-flex items-center gap-1.5 rounded-[7px] px-3 py-1 text-[11.5px] font-medium transition-colors"
                    style={on ? { background: "var(--color-bg-surface)", color: "var(--color-text-1)", boxShadow: "var(--shadow-card)" } : { color: "var(--color-text-2)" }}>
                    <Icon name={l.icon} size={13} color={on ? "var(--color-text-1)" : "var(--color-text-3)"} />
                    {l.label}
                  </button>
                );
              })}
            </div>

            <div className="flex items-center gap-2">
              <span className="rounded-md border border-[var(--color-border-subtle)] bg-[var(--color-bg-inset)] px-1.5 py-1 text-[11px] text-[var(--color-text-3)]">⌘K</span>
              <span className="inline-flex items-center gap-1 rounded-full px-2 py-0.5 text-[10.5px] font-medium" style={{ color: "var(--color-success)", background: "color-mix(in srgb, var(--color-success) 12%, transparent)" }}>
                <span className="status-pill-dot-live" style={{ width: 5, height: 5, borderRadius: 999, background: "var(--color-success)", display: "inline-block" }} /> live
              </span>
              <span className="grid h-7 w-7 place-items-center rounded-full bg-[var(--color-bg-inset)]" title="claude (orchestrator)"><Ant caste="queen" size={20} /></span>
            </div>
          </div>

          {/* ── body ── */}
          <div className="relative flex" style={{ height: 568 }}>
            {/* sidebar */}
            {collapsed ? (
              // collapsed = a function-icon rail (NOT project initials)
              <aside className="flex w-[50px] flex-col items-center gap-1 border-r border-[var(--color-border-subtle)] bg-[color-mix(in_srgb,var(--color-bg-inset)_60%,var(--color-bg-surface))] py-2.5">
                <RailIcon title="Show sidebar" onClick={() => setCollapsed(false)}><SidebarGlyph /></RailIcon>
                <RailIcon title="Projects" active><Icon name="view-kanban" size={16} color="var(--color-role-design-ink)" /></RailIcon>
                <RailIcon title="Search"><SearchGlyph /></RailIcon>
                <div className="mt-auto">
                  <RailIcon title="Settings"><Icon name="view-ops" size={16} color="var(--color-text-3)" /></RailIcon>
                </div>
              </aside>
            ) : (
              <aside className="flex w-[188px] flex-col border-r border-[var(--color-border-subtle)] bg-[color-mix(in_srgb,var(--color-bg-inset)_60%,var(--color-bg-surface))] py-3">
                <div className="flex-1 px-2">
                  <div className="mb-1 flex items-center justify-between px-1.5">
                    <span className="text-[10px] font-semibold uppercase tracking-[.5px] text-[var(--color-text-3)]">Projects</span>
                    <button type="button" onClick={() => setCollapsed(true)} title="Hide sidebar"
                      className="grid h-5 w-5 place-items-center rounded text-[var(--color-text-3)] transition-colors hover:bg-[var(--color-bg-inset)] hover:text-[var(--color-text-1)]">
                      <ChevronLeft />
                    </button>
                  </div>
                  <div className="flex flex-col gap-0.5">
                    {PROJECTS.map((p) => {
                      const active = project === p.id;
                      return (
                        <button key={p.id} type="button" onClick={() => setProject(p.id)}
                          className="flex items-center gap-2 rounded-md px-1.5 py-1 text-left transition-colors"
                          style={active ? { background: "color-mix(in srgb, var(--color-role-design) 14%, transparent)" } : undefined}>
                          <span className="grid h-5 w-5 shrink-0 place-items-center rounded text-[10px] font-bold"
                            style={{ background: active ? "color-mix(in srgb, var(--color-role-design) 22%, transparent)" : "var(--color-bg-inset)", color: active ? "var(--color-role-design-ink)" : "var(--color-text-3)" }}>
                            {p.name[0].toUpperCase()}
                          </span>
                          <span className={`flex-1 text-[12px] ${active ? "font-[650] text-[var(--color-text-1)]" : "text-[var(--color-text-2)]"}`}>{p.name}</span>
                          {p.awaiting > 0 && <span className="rounded-full bg-[color-mix(in_srgb,var(--color-warn)_18%,transparent)] px-1.5 text-[9.5px] font-semibold text-[var(--color-warn)]">{p.awaiting}</span>}
                        </button>
                      );
                    })}
                    <button type="button" className="flex items-center gap-2 rounded-md px-1.5 py-1 text-[var(--color-text-3)] transition-colors hover:bg-[var(--color-bg-inset)]">
                      <span className="grid h-5 w-5 shrink-0 place-items-center text-[13px]">＋</span>
                      <span className="text-[12px]">New project</span>
                    </button>
                  </div>
                </div>
                <div className="px-2">
                  <button type="button" title="Settings (Agents · Recipes · Preferences) ⌘,"
                    className="flex w-full items-center gap-2 rounded-md px-1.5 py-1 text-[var(--color-text-2)] transition-colors hover:bg-[var(--color-bg-inset)]">
                    <span className="grid h-5 w-5 shrink-0 place-items-center"><Icon name="view-ops" size={14} color="var(--color-text-3)" /></span>
                    <span className="text-[12px]">Settings</span>
                  </button>
                </div>
              </aside>
            )}

            {/* content */}
            <main className="canvas-stage flex min-w-0 flex-1 flex-col">
              {/* persistent seat roster */}
              <div className="flex items-center gap-2 border-b border-[var(--color-border-subtle)] bg-[color-mix(in_srgb,var(--color-bg-surface)_70%,transparent)] px-3.5 py-2">
                <span className="text-[10px] font-semibold uppercase tracking-[.5px] text-[var(--color-text-3)]">Seats</span>
                <div className="flex flex-wrap items-center gap-1.5">
                  {SEATS.map((s) => (
                    <span key={s.id} className="inline-flex items-center gap-1.5 rounded-full border border-[var(--color-border-subtle)] bg-[var(--color-bg-surface)] py-0.5 pl-1 pr-2" title="Drag onto a task to reassign">
                      <span className="grid h-5 w-5 place-items-center rounded-full bg-[var(--color-bg-inset)]"><Ant caste={s.caste} size={14} /></span>
                      <span className="mono text-[11px] font-[650]">{s.id}</span>
                      {s.roles.map((r) => (
                        <span key={r} className="rounded-full px-1.5 text-[9px] font-semibold uppercase tracking-[.3px]" style={{ color: ROLE_C[r].ink, background: `color-mix(in srgb, ${ROLE_C[r].main} 15%, transparent)` }}>{r}</span>
                      ))}
                    </span>
                  ))}
                </div>
              </div>

              {/* lens body */}
              <div className="relative flex-1 overflow-hidden">
                {lens === "canvas" ? (
                  <CanvasMock selected={selected} onSelect={setSelected} />
                ) : lens === "board" ? (
                  <div className="grid gap-3 overflow-auto p-3.5" style={{ gridTemplateColumns: "repeat(4, minmax(150px,1fr))" }}>
                    {COLUMNS.map((col) => (
                      <div key={col.title} className="flex flex-col gap-2">
                        <div className="flex items-center justify-between px-0.5">
                          <span className="text-[11px] font-semibold uppercase tracking-[.4px] text-[var(--color-text-3)]">{col.title}</span>
                          <span className="text-[10px] text-[var(--color-text-3)]">{col.tasks.length}</span>
                        </div>
                        {col.tasks.map((t) => (
                          <button key={t.id} type="button" onClick={() => setSelected(t.id)}
                            className="hover-lift rounded-lg bg-[var(--color-bg-surface)] p-2.5 text-left shadow-[var(--shadow-card)]"
                            style={{ border: selected === t.id ? "1.5px solid var(--color-role-design)" : "1px solid var(--color-border-subtle)" }}>
                            <div className="flex items-center justify-between">
                              <span className="mono text-[11.5px] font-[650]">{t.id}</span>
                              <StatusPill status={t.status} />
                            </div>
                            <div className="mt-2 flex items-center gap-1.5 text-[10.5px]"><SeatChip id={t.owner} /> <span className="text-[var(--color-text-3)]">→</span> <SeatChip id={t.reviewer} /></div>
                          </button>
                        ))}
                      </div>
                    ))}
                  </div>
                ) : (
                  <div className="grid h-full place-items-center text-[12px] text-[var(--color-text-3)]">
                    <div className="flex flex-col items-center gap-2">
                      <Icon name={LENSES.find((l) => l.id === lens)!.icon} size={28} color="var(--color-text-3)" />
                      <span>{LENSES.find((l) => l.id === lens)!.label} lens (mock)</span>
                    </div>
                  </div>
                )}
              </div>
            </main>

            {/* inspector — slides out on selection */}
            {selected && (
              <aside className="absolute right-0 top-0 bottom-0 z-10 w-[252px] overflow-auto border-l border-[var(--color-border-subtle)] bg-[var(--color-bg-surface)] px-3.5 py-3 fade-rise"
                style={{ boxShadow: "-14px 0 30px -18px rgba(18,22,31,.25)" }}>
                <div className="mb-2 flex items-center justify-between">
                  <span className="text-[10.5px] font-semibold uppercase tracking-[.5px] text-[var(--color-text-3)]">Inspector</span>
                  <button type="button" onClick={() => setSelected(null)} title="Close" className="grid h-5 w-5 place-items-center rounded text-[var(--color-text-3)] hover:bg-[var(--color-bg-inset)] hover:text-[var(--color-text-1)]">×</button>
                </div>
                <div className="flex items-center gap-2">
                  <span className="grid h-7 w-7 place-items-center rounded-lg bg-[var(--color-bg-inset)]"><Icon name="node-task" size={15} /></span>
                  <span className="mono text-[13px] font-[680]">{selected}</span>
                </div>
                <div className="mt-3 flex flex-col gap-3">
                  <InspectorRow label="Status"><StatusPill status="in_progress" /></InspectorRow>
                  <InspectorRow label="Owner → Reviewer"><div className="flex items-center gap-1.5"><SeatChip id="opencode" /> <span className="text-[var(--color-text-3)]">→</span> <SeatChip id="claude" /></div></InspectorRow>
                  <InspectorRow label="File"><span className="mono text-[10.5px] text-[var(--color-text-3)] [overflow-wrap:anywhere]">.pact/tasks/{selected}.md</span></InspectorRow>
                  <InspectorRow label="Depends on"><span className="text-[11px] text-[var(--color-text-2)]">t-schema <span className="text-[var(--color-text-3)]">→</span> {selected}</span></InspectorRow>
                </div>
              </aside>
            )}
          </div>

          {/* ── transport ── */}
          <div className="flex items-center gap-3 border-t border-[var(--color-border-subtle)] bg-[color-mix(in_srgb,var(--color-bg-surface)_88%,var(--color-bg-page))] px-3.5 py-2">
            <Icon name="state-idle" size={13} color="var(--color-text-3)" />
            <span className="text-[10.5px] text-[var(--color-text-3)]">replay</span>
            <div className="relative h-1 flex-1 rounded-full bg-[var(--color-bg-inset)]">
              <div className="absolute left-0 top-0 h-1 rounded-full bg-[color-mix(in_srgb,var(--color-role-design)_55%,transparent)]" style={{ width: "62%" }} />
              <div className="absolute top-1/2 h-2.5 w-2.5 -translate-y-1/2 rounded-full border-2 border-white bg-[var(--color-role-design)]" style={{ left: "62%", boxShadow: "var(--shadow-card)" }} />
            </div>
            <span className="inline-flex items-center gap-1 text-[10.5px] font-medium text-[var(--color-success)]">
              <span className="status-pill-dot-live" style={{ width: 5, height: 5, borderRadius: 999, background: "var(--color-success)", display: "inline-block" }} /> live
            </span>
          </div>
        </div>
      </div>
    </div>
  );
}

// CanvasMock — the primary surface: a dot-grid node graph (task nodes +
// dependency edges + a working ant on an active edge).
function CanvasMock({ selected, onSelect }: { selected: string | null; onSelect: (id: string) => void }) {
  const antEdge = ["t-schema", "t-manifest"] as const;
  const s = node(antEdge[0]), t = node(antEdge[1]);
  const mid = { x: (rightAnchor(s).x + leftAnchor(t).x) / 2, y: (rightAnchor(s).y + leftAnchor(t).y) / 2 };
  return (
    <div className="absolute inset-0 overflow-auto"
      style={{ backgroundImage: "radial-gradient(rgba(18,22,31,0.06) 1px, transparent 1px)", backgroundSize: "20px 20px" }}>
      <div className="relative" style={{ width: 760, height: 430, margin: "8px" }}>
        {/* edges */}
        <svg className="absolute inset-0" width="760" height="430" style={{ pointerEvents: "none" }}>
          {EDGES.map(([sid, tid]) => (
            <path key={`${sid}-${tid}`} d={edgePath(node(sid), node(tid))} fill="none"
              stroke="color-mix(in srgb, var(--color-text-3) 55%, transparent)" strokeWidth="1.5" />
          ))}
        </svg>
        {/* working ant on the active edge */}
        <div className="absolute" style={{ left: mid.x - 8, top: mid.y - 8 }} title="opencode working">
          <Ant caste="builder" size={16} />
        </div>
        {/* nodes */}
        {NODES.map((n) => (
          <CanvasNode key={n.id} task={n} selected={selected === n.id} onClick={() => onSelect(n.id)} />
        ))}
      </div>
    </div>
  );
}

function CanvasNode({ task, selected, onClick }: { task: Task; selected: boolean; onClick: () => void }) {
  return (
    <button type="button" onClick={onClick}
      className="card-frost absolute rounded-xl p-2.5 text-left hover-lift"
      style={{ left: task.x, top: task.y, width: NODE_W, border: selected ? "1.5px solid var(--color-role-design)" : "1px solid var(--color-border-subtle)", boxShadow: "var(--shadow-raised)" }}>
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-1.5">
          <span className="grid h-6 w-6 place-items-center rounded-lg" style={{ background: "color-mix(in srgb, var(--color-role-dev) 16%, transparent)" }}>
            <Icon name="node-task" size={13} color="var(--color-role-dev-ink)" />
          </span>
          <span className="mono text-[12px] font-[650]">{task.id}</span>
        </div>
        <StatusPill status={task.status} />
      </div>
      <div className="mt-1.5 flex items-center gap-1.5 text-[10px]"><SeatChip id={task.owner} /> <span className="text-[var(--color-text-3)]">→</span> <SeatChip id={task.reviewer} /></div>
    </button>
  );
}

function SeatChip({ id }: { id: string }) {
  return (
    <span className="inline-flex items-center gap-1">
      <span className="grid h-[17px] w-[17px] place-items-center rounded-full bg-[var(--color-bg-inset)]"><Ant caste={casteOf(id)} size={12} /></span>
      <span className="mono text-[10px] font-medium text-[var(--color-text-1)]">{id}</span>
    </span>
  );
}

function RailIcon({ children, title, active, onClick }: { children: React.ReactNode; title: string; active?: boolean; onClick?: () => void }) {
  return (
    <button type="button" title={title} onClick={onClick}
      className="grid h-9 w-9 place-items-center rounded-lg transition-colors hover:bg-[var(--color-bg-inset)]"
      style={active ? { background: "color-mix(in srgb, var(--color-role-design) 14%, transparent)" } : undefined}>
      {children}
    </button>
  );
}

function SidebarGlyph() {
  return (
    <svg width="16" height="16" viewBox="0 0 16 16" fill="none" aria-hidden>
      <rect x="2" y="3" width="12" height="10" rx="2" stroke="currentColor" strokeWidth="1.4" className="text-[var(--color-text-2)]" />
      <line x1="6" y1="3.5" x2="6" y2="12.5" stroke="currentColor" strokeWidth="1.4" className="text-[var(--color-text-2)]" />
    </svg>
  );
}
function ChevronLeft() {
  return <svg width="13" height="13" viewBox="0 0 16 16" fill="none" aria-hidden><path d="M10 3 L5.5 8 L10 13" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round" /></svg>;
}
function SearchGlyph() {
  return <svg width="16" height="16" viewBox="0 0 16 16" fill="none" aria-hidden className="text-[var(--color-text-3)]"><circle cx="7" cy="7" r="4.2" stroke="currentColor" strokeWidth="1.4" /><line x1="10.2" y1="10.2" x2="13.5" y2="13.5" stroke="currentColor" strokeWidth="1.4" strokeLinecap="round" /></svg>;
}

function InspectorRow({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div>
      <div className="mb-1 text-[10px] font-semibold uppercase tracking-[.5px] text-[var(--color-text-3)]">{label}</div>
      {children}
    </div>
  );
}
