import { useState } from "react";
import { Icon } from "../../lib/icons";
import { StatusPill, type PactStatus } from "../ui/StatusPill";
import { Badge } from "../ui/Badge";
import { Ant } from "../ui/ants/Ant";

// ShellPreview — a static mock of the macOS-app shell (Option A: sidebar +
// unified toolbar + inspector + transport). Reached at `?shell`. Sample data
// only — NOT wired to real state — so the layout can be reviewed and approved
// before it's extracted into real components and wired into App. Composed from
// the locked Phase 0 elements (StatusPill / Badge / Icon / Ant).

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

type Task = { id: string; owner: string; reviewer: string; status: PactStatus; file: string };
const COLUMNS: { title: string; tasks: Task[] }[] = [
  { title: "派发", tasks: [{ id: "t-schema", owner: "opencode", reviewer: "claude", status: "assigned", file: ".pact/tasks/t-schema.md" }] },
  {
    title: "进行中",
    tasks: [
      { id: "t-manifest", owner: "opencode", reviewer: "claude", status: "in_progress", file: ".pact/tasks/t-manifest.md" },
      { id: "t-prompt", owner: "gemini", reviewer: "claude", status: "in_progress", file: ".pact/tasks/t-prompt.md" },
    ],
  },
  {
    title: "待评审",
    tasks: [
      { id: "t-apply", owner: "opencode", reviewer: "claude", status: "awaiting_review", file: ".pact/tasks/t-apply.md" },
      { id: "t-cli", owner: "gemini", reviewer: "claude", status: "changes_requested", file: ".pact/tasks/t-cli.md" },
    ],
  },
  { title: "已接受", tasks: [{ id: "t-init", owner: "opencode", reviewer: "claude", status: "accepted", file: ".pact/tasks/t-init.md" }] },
];

const SEATS = [
  { id: "claude", caste: "queen" as const, roles: "orchestrator · reviewer", kind: "kind-claude-code" },
  { id: "gemini", caste: "builder" as const, roles: "worker", kind: "kind-gemini-cli" },
  { id: "opencode", caste: "builder" as const, roles: "worker", kind: "kind-opencode" },
];

export function ShellPreview() {
  const [lens, setLens] = useState<Lens>("board");
  const [project, setProject] = useState("demo");
  const [selected, setSelected] = useState("t-manifest");

  return (
    <div className="min-h-screen bg-[var(--color-bg-page)] p-6 text-[var(--color-text-1)]">
      <div className="mx-auto max-w-[1180px]">
        <div className="mb-3 text-[12px] text-[var(--color-text-3)]">
          Pactify · 外壳 mock（A：边栏 + 统一工具栏）· 样例数据 · 访问 <span className="mono">?shell</span>
        </div>

        {/* macOS app window */}
        <div
          className="overflow-hidden rounded-[12px] border border-[var(--color-border-subtle)] bg-[var(--color-bg-surface)]"
          style={{ boxShadow: "0 24px 60px -20px rgba(18,22,31,.28), 0 2px 8px rgba(18,22,31,.08)" }}
        >
          {/* ── unified toolbar (window titlebar + controls in one band) ── */}
          <div className="flex items-center gap-3 border-b border-[var(--color-border-subtle)] bg-[color-mix(in_srgb,var(--color-bg-surface)_88%,var(--color-bg-page))] px-3.5 py-2.5">
            {/* traffic lights */}
            <div className="flex items-center gap-2 pr-1">
              <span className="h-[11px] w-[11px] rounded-full" style={{ background: "#ff5f57" }} />
              <span className="h-[11px] w-[11px] rounded-full" style={{ background: "#febc2e" }} />
              <span className="h-[11px] w-[11px] rounded-full" style={{ background: "#28c840" }} />
            </div>
            {/* app · project */}
            <div className="flex items-center gap-1.5">
              <Icon name="action-merge" size={15} color="var(--color-role-dev-ink)" />
              <span className="text-[12.5px] font-[680]">pactify</span>
              <span className="text-[var(--color-text-3)]">·</span>
              <span className="text-[12.5px] font-medium text-[var(--color-text-2)]">{project}</span>
            </div>

            {/* center: lens segmented control */}
            <div className="mx-auto inline-flex rounded-[9px] border border-[var(--color-border-subtle)] bg-[var(--color-bg-inset)] p-0.5">
              {LENSES.map((l) => {
                const on = lens === l.id;
                return (
                  <button
                    key={l.id}
                    type="button"
                    onClick={() => setLens(l.id)}
                    className="inline-flex items-center gap-1.5 rounded-[7px] px-3 py-1 text-[11.5px] font-medium transition-colors"
                    style={on ? { background: "var(--color-bg-surface)", color: "var(--color-text-1)", boxShadow: "var(--shadow-card)" } : { color: "var(--color-text-2)" }}
                  >
                    <Icon name={l.icon} size={13} color={on ? "var(--color-text-1)" : "var(--color-text-3)"} />
                    {l.label}
                  </button>
                );
              })}
            </div>

            {/* right: primary action (reserved, disabled) + ⌘K + live + seat */}
            <div className="flex items-center gap-2">
              <button
                type="button"
                disabled
                title="需 B5：dashboard 驱动编排（后端未做）"
                className="inline-flex cursor-not-allowed items-center gap-1.5 rounded-md border border-[var(--color-border-subtle)] px-2.5 py-1 text-[11.5px] font-medium text-[var(--color-text-3)] opacity-60"
              >
                <Icon name="action-run" size={13} color="var(--color-text-3)" /> Orchestrate
              </button>
              <span className="rounded-md border border-[var(--color-border-subtle)] bg-[var(--color-bg-inset)] px-1.5 py-1 text-[11px] text-[var(--color-text-3)]">⌘K</span>
              <span className="inline-flex items-center gap-1 rounded-full px-2 py-0.5 text-[10.5px] font-medium" style={{ color: "var(--color-success)", background: "color-mix(in srgb, var(--color-success) 12%, transparent)" }}>
                <span className="status-pill-dot-live" style={{ width: 5, height: 5, borderRadius: 999, background: "var(--color-success)", display: "inline-block" }} /> live
              </span>
              <span className="grid h-7 w-7 place-items-center rounded-full bg-[var(--color-bg-inset)]" title="claude（orchestrator）">
                <Ant caste="queen" size={20} />
              </span>
            </div>
          </div>

          {/* ── body: sidebar | content | inspector ── */}
          <div className="grid" style={{ gridTemplateColumns: "188px 1fr 244px", height: 560 }}>
            {/* sidebar (source list, vibrancy) */}
            <aside className="flex flex-col gap-4 border-r border-[var(--color-border-subtle)] bg-[color-mix(in_srgb,var(--color-bg-inset)_60%,var(--color-bg-surface))] px-2.5 py-3">
              <SidebarSection label="Projects">
                {PROJECTS.map((p) => (
                  <SidebarItem key={p.id} active={project === p.id} onClick={() => setProject(p.id)}
                    icon={<Icon name="action-merge" size={14} color={project === p.id ? "var(--color-role-dev-ink)" : "var(--color-text-3)"} />}
                    label={p.name} badge={p.awaiting > 0 ? p.awaiting : undefined} />
                ))}
                <SidebarItem icon={<span className="text-[13px] text-[var(--color-text-3)]">＋</span>} label="新建项目" muted />
              </SidebarSection>
              <SidebarSection label="Machine">
                <SidebarItem icon={<Icon name="view-ops" size={14} color="var(--color-text-3)" />} label="Agents" />
                <SidebarItem icon={<Icon name="view-recipes" size={14} color="var(--color-text-3)" />} label="Recipes" />
              </SidebarSection>
              <div className="mt-auto px-1.5">
                <div className="text-[10px] text-[var(--color-text-3)]">3 座席 · 角色齐备</div>
              </div>
            </aside>

            {/* content — Board lens sample */}
            <main className="overflow-auto canvas-stage p-3.5">
              {lens === "board" ? (
                <div className="grid gap-3" style={{ gridTemplateColumns: "repeat(4, minmax(150px,1fr))" }}>
                  {COLUMNS.map((col) => (
                    <div key={col.title} className="flex flex-col gap-2">
                      <div className="flex items-center justify-between px-0.5">
                        <span className="text-[11px] font-semibold uppercase tracking-[.4px] text-[var(--color-text-3)]">{col.title}</span>
                        <span className="text-[10px] text-[var(--color-text-3)]">{col.tasks.length}</span>
                      </div>
                      {col.tasks.map((t) => (
                        <button
                          key={t.id}
                          type="button"
                          onClick={() => setSelected(t.id)}
                          className="hover-lift rounded-lg bg-[var(--color-bg-surface)] p-2.5 text-left shadow-[var(--shadow-card)]"
                          style={{ border: selected === t.id ? "1.5px solid var(--color-role-design)" : "1px solid var(--color-border-subtle)" }}
                        >
                          <div className="flex items-center justify-between">
                            <span className="mono text-[11.5px] font-[650]">{t.id}</span>
                            <StatusPill status={t.status} />
                          </div>
                          <div className="mt-1.5 text-[10.5px] text-[var(--color-text-2)]">{t.owner} <span className="text-[var(--color-text-3)]">→</span> {t.reviewer}</div>
                        </button>
                      ))}
                    </div>
                  ))}
                </div>
              ) : (
                <div className="grid h-full place-items-center text-[12px] text-[var(--color-text-3)]">
                  <div className="flex flex-col items-center gap-2">
                    <Icon name={LENSES.find((l) => l.id === lens)!.icon} size={28} color="var(--color-text-3)" />
                    <span>{LENSES.find((l) => l.id === lens)!.label} 镜头（mock 占位）</span>
                  </div>
                </div>
              )}
            </main>

            {/* inspector */}
            <aside className="overflow-auto border-l border-[var(--color-border-subtle)] bg-[var(--color-bg-surface)] px-3.5 py-3">
              <div className="mb-2 flex items-center justify-between">
                <span className="text-[10.5px] font-semibold uppercase tracking-[.5px] text-[var(--color-text-3)]">Inspector</span>
                <span className="text-[var(--color-text-3)]" title="收起">⌘I</span>
              </div>
              <div className="flex items-center gap-2">
                <span className="grid h-7 w-7 place-items-center rounded-lg bg-[var(--color-bg-inset)]"><Icon name="node-task" size={15} /></span>
                <span className="mono text-[13px] font-[680]">{selected}</span>
              </div>
              <div className="mt-3 flex flex-col gap-3">
                <InspectorRow label="状态"><StatusPill status="in_progress" /></InspectorRow>
                <InspectorRow label="Owner → Reviewer">
                  <span className="text-[11.5px] text-[var(--color-text-2)]">opencode <span className="text-[var(--color-text-3)]">→</span> claude</span>
                </InspectorRow>
                <InspectorRow label="文件"><span className="mono text-[10.5px] text-[var(--color-text-3)] [overflow-wrap:anywhere]">.pact/tasks/{selected}.md</span></InspectorRow>
                <InspectorRow label="座席">
                  <div className="flex flex-col gap-1.5">
                    {SEATS.map((s) => (
                      <div key={s.id} className="flex items-center gap-2">
                        <span className="grid h-5 w-5 place-items-center rounded bg-[var(--color-bg-inset)]"><Ant caste={s.caste} size={14} /></span>
                        <span className="mono text-[11px] font-medium">{s.id}</span>
                        <Badge color="role-dev">drivable</Badge>
                      </div>
                    ))}
                  </div>
                </InspectorRow>
              </div>
            </aside>
          </div>

          {/* ── bottom transport (replay) ── */}
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

function SidebarSection({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div>
      <div className="mb-1 px-1.5 text-[10px] font-semibold uppercase tracking-[.5px] text-[var(--color-text-3)]">{label}</div>
      <div className="flex flex-col gap-0.5">{children}</div>
    </div>
  );
}

function SidebarItem({ icon, label, active, muted, badge, onClick }: { icon: React.ReactNode; label: string; active?: boolean; muted?: boolean; badge?: number; onClick?: () => void }) {
  return (
    <button
      type="button"
      onClick={onClick}
      className="flex items-center gap-2 rounded-md px-1.5 py-1 text-left transition-colors"
      style={active ? { background: "color-mix(in srgb, var(--color-role-design) 14%, transparent)" } : undefined}
    >
      <span className="grid h-4 w-4 place-items-center">{icon}</span>
      <span className={`flex-1 text-[12px] ${active ? "font-[650] text-[var(--color-text-1)]" : muted ? "text-[var(--color-text-3)]" : "text-[var(--color-text-2)]"}`}>{label}</span>
      {badge != null && (
        <span className="rounded-full bg-[color-mix(in_srgb,var(--color-warn)_18%,transparent)] px-1.5 text-[9.5px] font-semibold text-[var(--color-warn)]">{badge}</span>
      )}
    </button>
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
