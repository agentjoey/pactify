import { useState } from "react";
import { Icon } from "../../lib/icons";
import { StatusPill, type PactStatus } from "../ui/StatusPill";
import { Ant } from "../ui/ants/Ant";
import type { Caste } from "../../lib/ants";

// ShellPreview — static mock of the macOS-app shell (Option A: collapsible
// sidebar + unified toolbar + selection-driven inspector + transport). Reached
// at `?shell`. Sample data only — NOT wired to real state — so the layout can be
// reviewed before it's extracted into real components and wired into App.
//
// Rev 2 (review feedback):
//  - sidebar is collapsible → icon rail when collapsed
//  - machine-level (Agents / Recipes) moved OUT of the sidebar into Settings (⌘,)
//  - inspector slides out only on selection (× to dismiss)
//  - a persistent on-canvas SEAT ROSTER (participating agents + roles, always on)
//  - every task card shows its seat assignment (owner → reviewer, with ant avatars)

type Lens = "board" | "canvas" | "live" | "plan";
const LENSES: { id: Lens; label: string; icon: string }[] = [
  { id: "board", label: "Board", icon: "view-kanban" },
  { id: "canvas", label: "Canvas", icon: "view-canvas" },
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

type Task = { id: string; owner: string; reviewer: string; status: PactStatus };
const COLUMNS: { title: string; tasks: Task[] }[] = [
  { title: "派发", tasks: [{ id: "t-schema", owner: "opencode", reviewer: "claude", status: "assigned" }] },
  {
    title: "进行中",
    tasks: [
      { id: "t-manifest", owner: "opencode", reviewer: "claude", status: "in_progress" },
      { id: "t-prompt", owner: "gemini", reviewer: "claude", status: "in_progress" },
    ],
  },
  {
    title: "待评审",
    tasks: [
      { id: "t-apply", owner: "opencode", reviewer: "claude", status: "awaiting_review" },
      { id: "t-cli", owner: "gemini", reviewer: "claude", status: "changes_requested" },
    ],
  },
  { title: "已接受", tasks: [{ id: "t-init", owner: "opencode", reviewer: "claude", status: "accepted" }] },
];

export function ShellPreview() {
  const [lens, setLens] = useState<Lens>("board");
  const [project, setProject] = useState("demo");
  const [selected, setSelected] = useState<string | null>("t-manifest");
  const [collapsed, setCollapsed] = useState(false);

  return (
    <div className="min-h-screen bg-[var(--color-bg-page)] p-6 text-[var(--color-text-1)]">
      <div className="mx-auto max-w-[1180px]">
        <div className="mb-3 text-[12px] text-[var(--color-text-3)]">
          Pactify · 外壳 mock rev2（A：可收边栏 + 统一工具栏 + 选中滑出 inspector + 画布常亮座席）· 样例数据 · <span className="mono">?shell</span>
        </div>

        <div
          className="overflow-hidden rounded-[12px] border border-[var(--color-border-subtle)] bg-[var(--color-bg-surface)]"
          style={{ boxShadow: "0 24px 60px -20px rgba(18,22,31,.28), 0 2px 8px rgba(18,22,31,.08)" }}
        >
          {/* ── unified toolbar ── */}
          <div className="flex items-center gap-3 border-b border-[var(--color-border-subtle)] bg-[color-mix(in_srgb,var(--color-bg-surface)_88%,var(--color-bg-page))] px-3.5 py-2.5">
            <div className="flex items-center gap-2 pr-0.5">
              <span className="h-[11px] w-[11px] rounded-full" style={{ background: "#ff5f57" }} />
              <span className="h-[11px] w-[11px] rounded-full" style={{ background: "#febc2e" }} />
              <span className="h-[11px] w-[11px] rounded-full" style={{ background: "#28c840" }} />
            </div>
            {/* sidebar toggle */}
            <button type="button" onClick={() => setCollapsed((c) => !c)} title="折叠边栏 (⌘\\)"
              className="grid h-6 w-6 place-items-center rounded-md text-[var(--color-text-3)] transition-colors hover:bg-[var(--color-bg-inset)] hover:text-[var(--color-text-1)]">
              <SidebarGlyph />
            </button>
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
              <button type="button" disabled title="需 B5：dashboard 驱动编排（后端未做）"
                className="inline-flex cursor-not-allowed items-center gap-1.5 rounded-md border border-[var(--color-border-subtle)] px-2.5 py-1 text-[11.5px] font-medium text-[var(--color-text-3)] opacity-60">
                <Icon name="action-run" size={13} color="var(--color-text-3)" /> Orchestrate
              </button>
              <span className="rounded-md border border-[var(--color-border-subtle)] bg-[var(--color-bg-inset)] px-1.5 py-1 text-[11px] text-[var(--color-text-3)]">⌘K</span>
              <span className="inline-flex items-center gap-1 rounded-full px-2 py-0.5 text-[10.5px] font-medium" style={{ color: "var(--color-success)", background: "color-mix(in srgb, var(--color-success) 12%, transparent)" }}>
                <span className="status-pill-dot-live" style={{ width: 5, height: 5, borderRadius: 999, background: "var(--color-success)", display: "inline-block" }} /> live
              </span>
              <span className="grid h-7 w-7 place-items-center rounded-full bg-[var(--color-bg-inset)]" title="claude（orchestrator）"><Ant caste="queen" size={20} /></span>
            </div>
          </div>

          {/* ── body ── */}
          <div className="relative flex" style={{ height: 568 }}>
            {/* sidebar — collapsible to an icon rail */}
            <aside className="flex flex-col border-r border-[var(--color-border-subtle)] bg-[color-mix(in_srgb,var(--color-bg-inset)_60%,var(--color-bg-surface))] py-3 transition-[width] duration-200"
              style={{ width: collapsed ? 52 : 188 }}>
              <div className="flex-1 px-2">
                {!collapsed && <div className="mb-1 px-1.5 text-[10px] font-semibold uppercase tracking-[.5px] text-[var(--color-text-3)]">Projects</div>}
                <div className="flex flex-col gap-0.5">
                  {PROJECTS.map((p) => {
                    const active = project === p.id;
                    return (
                      <button key={p.id} type="button" onClick={() => setProject(p.id)} title={p.name}
                        className={`flex items-center gap-2 rounded-md py-1 text-left transition-colors ${collapsed ? "justify-center px-0" : "px-1.5"}`}
                        style={active ? { background: "color-mix(in srgb, var(--color-role-design) 14%, transparent)" } : undefined}>
                        <span className="grid h-5 w-5 shrink-0 place-items-center rounded text-[10px] font-bold"
                          style={{ background: active ? "color-mix(in srgb, var(--color-role-design) 22%, transparent)" : "var(--color-bg-inset)", color: active ? "var(--color-role-design-ink)" : "var(--color-text-3)" }}>
                          {p.name[0].toUpperCase()}
                        </span>
                        {!collapsed && <span className={`flex-1 text-[12px] ${active ? "font-[650] text-[var(--color-text-1)]" : "text-[var(--color-text-2)]"}`}>{p.name}</span>}
                        {!collapsed && p.awaiting > 0 && (
                          <span className="rounded-full bg-[color-mix(in_srgb,var(--color-warn)_18%,transparent)] px-1.5 text-[9.5px] font-semibold text-[var(--color-warn)]">{p.awaiting}</span>
                        )}
                      </button>
                    );
                  })}
                  <button type="button" title="新建项目"
                    className={`flex items-center gap-2 rounded-md py-1 text-[var(--color-text-3)] transition-colors hover:bg-[var(--color-bg-inset)] ${collapsed ? "justify-center px-0" : "px-1.5"}`}>
                    <span className="grid h-5 w-5 shrink-0 place-items-center text-[13px]">＋</span>
                    {!collapsed && <span className="text-[12px]">新建项目</span>}
                  </button>
                </div>
              </div>
              {/* footer: Settings (machine-level lives here) */}
              <div className="px-2">
                <button type="button" title="Settings（Agents · Recipes · 偏好）⌘,"
                  className={`flex w-full items-center gap-2 rounded-md py-1 text-[var(--color-text-2)] transition-colors hover:bg-[var(--color-bg-inset)] ${collapsed ? "justify-center px-0" : "px-1.5"}`}>
                  <span className="grid h-5 w-5 shrink-0 place-items-center"><Icon name="view-ops" size={14} color="var(--color-text-3)" /></span>
                  {!collapsed && <span className="text-[12px]">Settings</span>}
                </button>
              </div>
            </aside>

            {/* content */}
            <main className="canvas-stage flex min-w-0 flex-1 flex-col">
              {/* persistent seat roster — participating agents + roles, always on */}
              <div className="flex items-center gap-2 border-b border-[var(--color-border-subtle)] bg-[color-mix(in_srgb,var(--color-bg-surface)_70%,transparent)] px-3.5 py-2">
                <span className="text-[10px] font-semibold uppercase tracking-[.5px] text-[var(--color-text-3)]">参与座席</span>
                <div className="flex flex-wrap items-center gap-1.5">
                  {SEATS.map((s) => (
                    <span key={s.id} className="inline-flex items-center gap-1.5 rounded-full border border-[var(--color-border-subtle)] bg-[var(--color-bg-surface)] py-0.5 pl-1 pr-2" title="拖到任务可改派（Setup 内拖座席）">
                      <span className="grid h-5 w-5 place-items-center rounded-full bg-[var(--color-bg-inset)]"><Ant caste={s.caste} size={14} /></span>
                      <span className="mono text-[11px] font-[650]">{s.id}</span>
                      {s.roles.map((r) => (
                        <span key={r} className="rounded-full px-1.5 text-[9px] font-semibold uppercase tracking-[.3px]" style={{ color: ROLE_C[r].ink, background: `color-mix(in srgb, ${ROLE_C[r].main} 15%, transparent)` }}>{r}</span>
                      ))}
                    </span>
                  ))}
                </div>
              </div>

              <div className="flex-1 overflow-auto p-3.5">
                {lens === "board" ? (
                  <div className="grid gap-3" style={{ gridTemplateColumns: "repeat(4, minmax(150px,1fr))" }}>
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
                            {/* seat assignment on the card: owner → reviewer with ant avatars */}
                            <div className="mt-2 flex items-center gap-1.5 text-[10.5px] text-[var(--color-text-2)]">
                              <SeatChip id={t.owner} /> <span className="text-[var(--color-text-3)]">→</span> <SeatChip id={t.reviewer} />
                            </div>
                          </button>
                        ))}
                      </div>
                    ))}
                  </div>
                ) : (
                  <div className="grid h-full place-items-center text-[12px] text-[var(--color-text-3)]">
                    <div className="flex flex-col items-center gap-2">
                      <Icon name={LENSES.find((l) => l.id === lens)!.icon} size={28} color="var(--color-text-3)" />
                      <span>{LENSES.find((l) => l.id === lens)!.label} 镜头（mock 占位）· 参与座席常亮在上方</span>
                    </div>
                  </div>
                )}
              </div>
            </main>

            {/* inspector — slides out only on selection */}
            {selected && (
              <aside className="absolute right-0 top-0 bottom-0 z-10 w-[252px] overflow-auto border-l border-[var(--color-border-subtle)] bg-[var(--color-bg-surface)] px-3.5 py-3 fade-rise"
                style={{ boxShadow: "-14px 0 30px -18px rgba(18,22,31,.25)" }}>
                <div className="mb-2 flex items-center justify-between">
                  <span className="text-[10.5px] font-semibold uppercase tracking-[.5px] text-[var(--color-text-3)]">Inspector</span>
                  <button type="button" onClick={() => setSelected(null)} title="收起" className="grid h-5 w-5 place-items-center rounded text-[var(--color-text-3)] hover:bg-[var(--color-bg-inset)] hover:text-[var(--color-text-1)]">×</button>
                </div>
                <div className="flex items-center gap-2">
                  <span className="grid h-7 w-7 place-items-center rounded-lg bg-[var(--color-bg-inset)]"><Icon name="node-task" size={15} /></span>
                  <span className="mono text-[13px] font-[680]">{selected}</span>
                </div>
                <div className="mt-3 flex flex-col gap-3">
                  <InspectorRow label="状态"><StatusPill status="in_progress" /></InspectorRow>
                  <InspectorRow label="Owner → Reviewer">
                    <div className="flex items-center gap-1.5"><SeatChip id="opencode" /> <span className="text-[var(--color-text-3)]">→</span> <SeatChip id="claude" /></div>
                  </InspectorRow>
                  <InspectorRow label="文件"><span className="mono text-[10.5px] text-[var(--color-text-3)] [overflow-wrap:anywhere]">.pact/tasks/{selected}.md</span></InspectorRow>
                  <InspectorRow label="依赖"><span className="text-[11px] text-[var(--color-text-2)]">t-schema <span className="text-[var(--color-text-3)]">→</span> {selected}</span></InspectorRow>
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

// SeatChip — an ant avatar + seat id, the seat-assignment token on cards / rows.
function SeatChip({ id }: { id: string }) {
  return (
    <span className="inline-flex items-center gap-1">
      <span className="grid h-[18px] w-[18px] place-items-center rounded-full bg-[var(--color-bg-inset)]"><Ant caste={casteOf(id)} size={13} /></span>
      <span className="mono text-[10.5px] font-medium text-[var(--color-text-1)]">{id}</span>
    </span>
  );
}

// SidebarGlyph — a small "panel" icon for the collapse toggle (left pane lines).
function SidebarGlyph() {
  return (
    <svg width="15" height="15" viewBox="0 0 16 16" fill="none" aria-hidden>
      <rect x="2" y="3" width="12" height="10" rx="2" stroke="currentColor" strokeWidth="1.4" />
      <line x1="6" y1="3.5" x2="6" y2="12.5" stroke="currentColor" strokeWidth="1.4" />
    </svg>
  );
}

function InspectorRow({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div>
      <div className="mb-1 text-[10px] font-semibold uppercase tracking-[.5px] text-[var(--color-text-3)]">{label}</div>
      {children}
    </div>
  );
}
