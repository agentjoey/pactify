import type { ReactNode } from "react";
import { useRef, useState } from "react";
import { Button } from "./ui/Button";
import { Badge } from "./ui/Badge";
import { Alert } from "./ui/Alert";
import { EmptyState } from "./ui/EmptyState";
import { Spinner } from "./ui/Spinner";
import { Input } from "./ui/Input";
import { Select } from "./ui/Select";
import { ConfigSection, Inset } from "./ui/ConfigSection";
import { Textarea } from "./ui/Textarea";
import { Skeleton } from "./Skeleton";
import { Ant } from "./ui/ants/Ant";
import { StatusPill, type PactStatus } from "./ui/StatusPill";
import { Icon, ICON_NAMES } from "../lib/icons";

// ElementsGallery — the living design system (Phase 0). Reached at `?gallery`.
// Every basic element is shown in light theme across its states, so they can be
// designed + reviewed in isolation before pages compose from them. Code IS the
// spec — no Figma/code drift.

function Section({ title, note, children }: { title: string; note?: string; children: ReactNode }) {
  return (
    <section className="mb-10">
      <div className="mb-3 border-b border-[var(--color-border-subtle)] pb-1.5">
        <h2 className="text-[13px] font-[650] text-[var(--color-text-1)]">{title}</h2>
        {note && <p className="mt-0.5 text-[11px] text-[var(--color-text-3)]">{note}</p>}
      </div>
      {children}
    </section>
  );
}

function Row({ children }: { children: ReactNode }) {
  return <div className="flex flex-wrap items-center gap-3">{children}</div>;
}

const ALL_STATUS: PactStatus[] = [
  "assigned",
  "in_progress",
  "awaiting_review",
  "changes_requested",
  "accepted",
  "shipped",
  "escalated",
  "idle",
];

// caste → the delivery-chain role it stands for (for the silhouette labels).
const ANT_CASTES = [
  ["queen", "orchestrator"],
  ["guard", "reviewer"],
  ["builder", "worker"],
  ["catcher", "qa"],
  ["brewer", "product"],
  ["painter", "design"],
  ["scout", "research"],
  ["keeper", "ops"],
] as const;

const ROLE_VARS = [
  ["product", "orchestrator"],
  ["design", "reviewer"],
  ["dev", "worker"],
  ["qa", "qa"],
  ["designer", "designer"],
  ["research", "research"],
  ["ops", "ops"],
] as const;

export function ElementsGallery() {
  const [loading, setLoading] = useState(false);
  const [toggle, setToggle] = useState(true);
  const [seg, setSeg] = useState("high");
  const [skelLoading, setSkelLoading] = useState(true);
  const [tools, setTools] = useState<Record<string, boolean>>({
    Read: true,
    Edit: true,
    Write: false,
    Bash: false,
    Grep: true,
  });

  return (
    <div className="min-h-screen bg-[var(--color-bg-page)] text-[var(--color-text-1)]">
      <div className="mx-auto max-w-[920px] px-8 py-8">
        <header className="mb-8">
          <div className="text-[18px] font-[680]">Pactify · 元素库</div>
          <div className="mt-1 text-[12px] text-[var(--color-text-3)]">
            Phase 0 基础元素（浅色）。活的设计规范 — 代码即规范。访问 <span className="mono">?gallery</span>。
          </div>
        </header>

        {/* Foundations — colors */}
        <Section title="Foundations · 色板" note="背景层 / 文本层 / role 色 / 语义色（token，浅色主题）">
          <div className="mb-3 text-[11px] uppercase tracking-[.5px] text-[var(--color-text-3)]">背景层</div>
          <Row>
            {["page", "surface", "inset", "raised", "overlay"].map((b) => (
              <Swatch key={b} label={b} bg={`var(--color-bg-${b})`} border />
            ))}
          </Row>
          <div className="my-3 text-[11px] uppercase tracking-[.5px] text-[var(--color-text-3)]">role 色</div>
          <Row>
            {ROLE_VARS.map(([v, name]) => (
              <Swatch key={v} label={name} bg={`var(--color-role-${v})`} solid />
            ))}
          </Row>
          <div className="my-3 text-[11px] uppercase tracking-[.5px] text-[var(--color-text-3)]">语义色</div>
          <Row>
            {["danger", "warn", "success"].map((c) => (
              <Swatch key={c} label={c} bg={`var(--color-${c})`} solid />
            ))}
          </Row>
        </Section>

        {/* Icon library — Phosphor duotone, role-colored per concept */}
        <Section title="Icon library" note="Phosphor duotone · role / agent kind / 状态 / 动作 / 视图 — 按概念上色（不单一蓝）">
          <div className="grid grid-cols-[repeat(auto-fill,minmax(108px,1fr))] gap-2.5">
            {ICON_NAMES.map((name) => (
              <div
                key={name}
                className="flex flex-col items-center gap-1.5 rounded-md border border-[var(--color-border-subtle)] bg-[var(--color-bg-surface)] px-2 py-2.5"
              >
                <Icon name={name} size={22} />
                <span className="text-[9.5px] text-[var(--color-text-3)] [overflow-wrap:anywhere] text-center">{name}</span>
              </div>
            ))}
          </div>
        </Section>

        {/* StatusPill — the state language */}
        <Section title="StatusPill · pact 状态语言" note="贯穿卡片/节点/Live 的状态色码；working/escalated 带呼吸点">
          <Row>
            {ALL_STATUS.map((s) => (
              <StatusPill key={s} status={s} />
            ))}
          </Row>
        </Section>

        {/* Buttons */}
        <Section title="Button" note="primary / ghost / danger · sm·md · loading · icon · disabled">
          <Row>
            <Button>Primary</Button>
            <Button variant="ghost">Ghost</Button>
            <Button variant="danger">Danger</Button>
            <Button size="sm">Small</Button>
            <Button icon={<span>✦</span>}>With icon</Button>
            <Button loading={loading} onClick={() => { setLoading(true); setTimeout(() => setLoading(false), 1200); }}>
              {loading ? "Saving" : "Click to load"}
            </Button>
            <Button disabled>Disabled</Button>
          </Row>
        </Section>

        {/* Badge */}
        <Section title="Badge" note="role 徽章 / drivable / 计数">
          <Row>
            {ROLE_VARS.map(([v, name]) => (
              <Badge key={v} colorVar={`var(--color-role-${v})`}>{name}</Badge>
            ))}
            <Badge color="role-dev">drivable</Badge>
            <Badge color="role-design">manual</Badge>
            <Badge color="warn">machine-global</Badge>
          </Row>
        </Section>

        {/* Cards */}
        <Section title="Card" note="基础卡 / 配置分组卡 / 节点卡（任务·座席）">
          <div className="grid grid-cols-2 gap-4">
            {/* base card */}
            <div className="rounded-lg border border-[var(--color-border-subtle)] bg-[var(--color-bg-surface)] p-4 shadow-[var(--shadow-card)]">
              <div className="text-[12px] font-medium">基础卡</div>
              <div className="mt-1 text-[11px] text-[var(--color-text-2)]">white surface · hairline border · soft shadow</div>
            </div>
            {/* section card (dify style) */}
            <div className="rounded-lg border border-[var(--color-border-subtle)] bg-[var(--color-bg-surface)] p-4 shadow-[var(--shadow-card)]">
              <div className="mb-1.5 text-[10.5px] font-semibold uppercase tracking-[.5px] text-[var(--color-text-3)]">
                MODEL <span className="text-[var(--color-danger)]">*</span>
              </div>
              <div className="rounded-md bg-[var(--color-bg-inset)] px-3 py-2 text-[12px]">claude-opus-4-8</div>
            </div>
            {/* node card — task */}
            <div className="rounded-lg border border-[var(--color-border-subtle)] bg-[var(--color-bg-surface)] p-3 shadow-[var(--shadow-card)] hover-lift">
              <div className="flex items-center justify-between">
                <span className="mono text-[12px] font-medium">t-manifest</span>
                <StatusPill status="awaiting_review" />
              </div>
              <div className="mt-1.5 text-[11px] text-[var(--color-text-2)]">
                opencode <span className="text-[var(--color-text-3)]">→</span> claude
              </div>
              <div className="mt-1 mono text-[10.5px] text-[var(--color-text-3)]">.pact/tasks/t-manifest.md</div>
            </div>
            {/* node card — seat */}
            <div className="rounded-lg border border-[var(--color-border-subtle)] bg-[var(--color-bg-surface)] p-3 shadow-[var(--shadow-card)] hover-lift">
              <div className="flex items-center justify-between">
                <span className="text-[12px] font-[650]">claude</span>
                <StatusPill status="in_progress" />
              </div>
              <div className="mt-1 text-[11px] text-[var(--color-text-2)]">orchestrator · reviewer</div>
              <div className="mt-1.5 flex items-center gap-1.5">
                <Badge color="role-dev">drivable</Badge>
                <span className="text-[10.5px] text-[var(--color-text-3)]">model claude-opus-4-8</span>
              </div>
            </div>
          </div>
        </Section>

        {/* Node card — Dify-style with connect "+" handles + Make-Grid Links */}
        <Section title="节点卡 · 连接 + 关系" note="dify 式节点卡 + 边上“+”连接手柄（建依赖）· Make Grid 式 Links 方向 pill">
          <div className="grid grid-cols-2 gap-8 pt-5">
            <div className="flex flex-col gap-9">
              <NodeCard role="dev" iconName="node-task" title="t-manifest" status="in_progress" sub="opencode → claude" meta="model deepseek-v4-pro" />
              <NodeCard role="design" iconName="kind-claude-code" title="claude" status="awaiting_review" sub="orchestrator · reviewer" meta="drivable · claude-opus-4-8" selected />
            </div>
            <div>
              <div className="mb-2 text-[11px] uppercase tracking-[.5px] text-[var(--color-text-3)]">Links · 关系（方向 pill）</div>
              <LinksDemo />
            </div>
          </div>
        </Section>

        {/* Inputs */}
        <Section title="Input" note="文本 / toggle / 分段控件 / chip">
          <div className="flex flex-col gap-3">
            <div className="max-w-[280px]"><Input placeholder="文本输入…" /></div>
            <Row>
              <span className="text-[11px] text-[var(--color-text-3)]">toggle</span>
              <button
                type="button"
                aria-pressed={toggle}
                onClick={() => setToggle((t) => !t)}
                className="press rounded px-2 py-0.5 text-[10.5px] transition-colors"
                style={{
                  background: toggle ? "color-mix(in srgb, var(--color-warn) 18%, transparent)" : "var(--color-bg-inset)",
                  color: toggle ? "var(--color-text-1)" : "var(--color-text-3)",
                }}
              >
                {toggle ? "scoped" : "blanket"}
              </button>
            </Row>
            <Row>
              <span className="text-[11px] text-[var(--color-text-3)]">分段</span>
              <div className="inline-flex rounded-md border border-[var(--color-border-subtle)] bg-[var(--color-bg-inset)] p-0.5">
                {["high", "low"].map((o) => (
                  <button
                    key={o}
                    type="button"
                    onClick={() => setSeg(o)}
                    className="rounded px-3 py-0.5 text-[11px] transition-colors"
                    style={
                      seg === o
                        ? { background: "var(--color-bg-surface)", color: "var(--color-text-1)", boxShadow: "var(--shadow-card)" }
                        : { color: "var(--color-text-2)" }
                    }
                  >
                    {o}
                  </button>
                ))}
              </div>
            </Row>
            <Row>
              {["Read", "Edit", "Bash"].map((c) => (
                <span key={c} className="rounded bg-[var(--color-bg-inset)] px-2 py-0.5 text-[10.5px] text-[var(--color-text-2)]">{c}</span>
              ))}
            </Row>
          </div>
        </Section>

        {/* Config surface — dify-style grouped sections (RightRail / AgentConfig) */}
        <Section
          title="配置面板元素 · 借鉴 dify"
          note="ConfigSection 分组（大写标签 + 必填 *）· Inset 控件底 · Select · Textarea（字数 / {x} 变量 / 展开）· TOOLS LIST（allowed-tools 开关行）"
        >
          <div className="grid grid-cols-2 gap-5">
            {/* left column — a model + temperature + prompt config card */}
            <div className="rounded-lg border border-[var(--color-border-subtle)] bg-[var(--color-bg-surface)] p-4 shadow-[var(--shadow-card)]">
              <ConfigSection label="Model" required action={<span className="mono">opus-4-8</span>}>
                <Select defaultValue="claude-opus-4-8">
                  <option value="claude-opus-4-8">claude-opus-4-8</option>
                  <option value="deepseek-v4-pro">deepseek-v4-pro</option>
                  <option value="gemini-3-pro">gemini-3-pro</option>
                </Select>
              </ConfigSection>
              <ConfigSection label="Temperature">
                <Inset>0.2 · 偏确定</Inset>
              </ConfigSection>
              <ConfigSection label="System prompt" action="变量可用">
                <Textarea
                  variables
                  defaultValue="你是 reviewer，逐任务翻转 accepted/changes。"
                  placeholder="输入系统提示…"
                />
              </ConfigSection>
            </div>

            {/* right column — dify TOOLS LIST: allowed-tools toggle rows */}
            <div className="rounded-lg border border-[var(--color-border-subtle)] bg-[var(--color-bg-surface)] p-4 shadow-[var(--shadow-card)]">
              <ConfigSection
                label="Tools list"
                action={`${Object.values(tools).filter(Boolean).length}/${Object.keys(tools).length}`}
              >
                <div className="flex flex-col gap-1">
                  {Object.entries(tools).map(([name, on]) => (
                    <ToolRow key={name} name={name} on={on} onToggle={() => setTools((t) => ({ ...t, [name]: !t[name] }))} />
                  ))}
                </div>
              </ConfigSection>
              <div className="mt-1 text-[10.5px] text-[var(--color-text-3)]">
                scoped 姿态下，只有开启的工具会进入 worker 的 allowed-tools。
              </div>
            </div>
          </div>
        </Section>

        {/* Skeleton — first-paint placeholders */}
        <Section title="Skeleton · 骨架屏" note="SSE 首帧前的占位（shimmer，尊重 reduced-motion）">
          <Row>
            <Button
              size="sm"
              variant="ghost"
              onClick={() => { setSkelLoading(true); setTimeout(() => setSkelLoading(false), 1400); }}
            >
              重放
            </Button>
            <span className="text-[11px] text-[var(--color-text-3)]">点重放看 shimmer→内容</span>
          </Row>
          <div className="mt-3 grid grid-cols-2 gap-4">
            <div className="rounded-lg border border-[var(--color-border-subtle)] bg-[var(--color-bg-surface)] p-3 shadow-[var(--shadow-card)]">
              {skelLoading ? (
                <div className="flex flex-col gap-2">
                  <Skeleton width={120} height={11} />
                  <Skeleton height={44} />
                  <Skeleton width="70%" height={11} />
                </div>
              ) : (
                <div className="flex flex-col gap-1.5">
                  <span className="mono text-[12px] font-medium">t-manifest</span>
                  <div className="text-[11px] text-[var(--color-text-2)]">opencode → claude</div>
                  <StatusPill status="awaiting_review" />
                </div>
              )}
            </div>
            <div className="rounded-lg border border-[var(--color-border-subtle)] bg-[var(--color-bg-surface)] p-3 shadow-[var(--shadow-card)]">
              <div className="flex flex-col gap-2">
                {Array.from({ length: 3 }).map((_, i) => (
                  <Skeleton key={i} height={28} />
                ))}
              </div>
            </div>
          </div>
        </Section>

        {/* Ant silhouettes — colony glyphs,备料 for the ant motion engine */}
        <Section
          title="蚂蚁剪影 · 蚁群引擎备料"
          note="八工种印章蚁（charcoal 身 + 工种色腹带）· 14 chip / 22 seat / 34 pad — 蚂蚁动效语言的素材"
        >
          <div className="grid grid-cols-[repeat(auto-fill,minmax(96px,1fr))] gap-2.5">
            {ANT_CASTES.map(([caste, role]) => (
              <div
                key={caste}
                className="flex flex-col items-center gap-1.5 rounded-md border border-[var(--color-border-subtle)] bg-[var(--color-bg-surface)] px-2 py-3"
              >
                <Ant caste={caste} size={34} title={caste} />
                <span className="text-[10.5px] font-medium text-[var(--color-text-2)]">{caste}</span>
                <span className="text-[9.5px] text-[var(--color-text-3)]">{role}</span>
              </div>
            ))}
          </div>
          <div className="mt-3 flex items-center gap-4 rounded-md border border-[var(--color-border-subtle)] bg-[var(--color-bg-inset)] px-3 py-2.5">
            <span className="text-[10.5px] uppercase tracking-[.5px] text-[var(--color-text-3)]">尺寸阶</span>
            {[14, 22, 34].map((s) => (
              <div key={s} className="flex items-center gap-1.5">
                <Ant caste="builder" size={s} />
                <span className="text-[10px] text-[var(--color-text-3)]">{s}px</span>
              </div>
            ))}
          </div>
        </Section>

        {/* Stitch-inspired canvas effects */}
        <Section title="画布效果 · 借鉴 Stitch" note="细点网格 + 光标放大镜（移动鼠标）· 半透明磨砂卡片">
          <GridLensDemo />
        </Section>

        {/* Feedback */}
        <Section title="Feedback" note="Alert（4 色）/ EmptyState / Spinner">
          <div className="flex flex-col gap-2">
            <Alert tone="info" title="Info">一条提示信息。</Alert>
            <Alert tone="warn" title="Warn">需要注意。</Alert>
            <Alert tone="danger" title="Danger" onRetry={() => {}}>出错了，可重试。</Alert>
            <Alert tone="success" title="Success">完成了。</Alert>
            <div className="grid grid-cols-2 gap-4">
              <EmptyState title="空态" hint="带可操作提示的空态。" />
              <div className="flex items-center gap-2 rounded-lg border border-[var(--color-border-subtle)] bg-[var(--color-bg-surface)] p-4">
                <Spinner /> <span className="text-[12px] text-[var(--color-text-2)]">加载中…</span>
              </div>
            </div>
          </div>
        </Section>
      </div>
    </div>
  );
}

// NodeCard — the refined canvas node (Dify-style): role-tinted icon tile, title,
// a floating status pill above the card, a summary meta row, a selected blue
// ring, and "+" connect handles on the right edge (the affordance to draw a
// dependency to a downstream card — Dify's edge "+").
function NodeCard({
  role,
  iconName,
  title,
  sub,
  meta,
  status,
  selected,
}: {
  role: "product" | "design" | "dev";
  iconName: string;
  title: string;
  sub: string;
  meta?: string;
  status: PactStatus;
  selected?: boolean;
}) {
  const c = `var(--color-role-${role})`;
  return (
    <div
      data-testid="node-card"
      className="relative rounded-xl bg-[var(--color-bg-surface)] p-3 shadow-[var(--shadow-card)] hover-lift"
      style={{
        // Selected = a single clean blue frame (Dify), no double-ring halo.
        border: selected ? `1.5px solid ${c}` : "1px solid var(--color-border-subtle)",
        width: 230,
      }}
    >
      {/* status pill floating just above the card's top-left, aligned over the
          header (Dify "Wait for running") — left-anchored so it reads as this
          node's status label and stays clear of the right-edge "+" handle. */}
      <div className="absolute left-1" style={{ bottom: "calc(100% + 5px)" }}>
        <StatusPill status={status} />
      </div>
      {/* header: role-tinted icon tile + title */}
      <div className="flex items-center gap-2">
        <span
          className="grid h-7 w-7 place-items-center rounded-lg"
          style={{ background: `color-mix(in srgb, ${c} 16%, transparent)` }}
        >
          <Icon name={iconName} size={16} color={`var(--color-role-${role}-ink)`} />
        </span>
        <span className="mono text-[12.5px] font-[650] text-[var(--color-text-1)]">{title}</span>
      </div>
      <div className="mt-1.5 text-[11px] text-[var(--color-text-2)]">{sub}</div>
      {meta && <div className="mt-1 text-[10.5px] text-[var(--color-text-3)]">{meta}</div>}
      {/* connect "+" handle on the right edge */}
      <ConnectPlus className="-right-2.5 top-1/2 -translate-y-1/2" />
    </div>
  );
}

function ConnectPlus({ className = "" }: { className?: string }) {
  return (
    <button type="button" aria-label="connect" className={`connect-plus absolute ${className}`}>
      +
    </button>
  );
}

// ToolRow — one allowed-tools entry in the dify TOOLS LIST: the tool icon-chip +
// name on the left, an iOS-style toggle on the right. On = role-dev green.
function ToolRow({ name, on, onToggle }: { name: string; on: boolean; onToggle: () => void }) {
  return (
    <div className="flex items-center justify-between rounded-md px-1.5 py-1 transition-colors hover:bg-[var(--color-bg-inset)]">
      <div className="flex items-center gap-2">
        <span className="grid h-5 w-5 place-items-center rounded bg-[var(--color-bg-inset)] mono text-[10px] text-[var(--color-text-2)]">
          {name[0]}
        </span>
        <span className="mono text-[11.5px] text-[var(--color-text-1)]">{name}</span>
      </div>
      <button
        type="button"
        role="switch"
        aria-checked={on}
        aria-label={name}
        onClick={onToggle}
        className="press relative h-[18px] w-[30px] rounded-full transition-colors"
        style={{ background: on ? "var(--color-role-dev)" : "var(--color-bg-inset)" }}
      >
        <span
          className="absolute top-[2px] h-[14px] w-[14px] rounded-full bg-white shadow-sm transition-[left]"
          style={{ left: on ? 14 : 2 }}
        />
      </button>
    </div>
  );
}

// LinksDemo — Make-Grid relationship rows: [source] —[direction pill]— [target].
function LinksDemo() {
  const rows: { dir: string; tone: string; target: string; ti: string; tc: string }[] = [
    { dir: "owns →", tone: "var(--color-role-dev)", target: "t-manifest", ti: "T", tc: "var(--color-role-dev)" },
    { dir: "← reviewed by", tone: "var(--color-role-design)", target: "claude", ti: "C", tc: "var(--color-role-design)" },
    { dir: "depends on →", tone: "var(--color-role-product)", target: "t-prompt", ti: "T", tc: "var(--color-role-product)" },
    { dir: "hands off →", tone: "var(--color-role-design)", target: "claude (review)", ti: "C", tc: "var(--color-role-design)" },
  ];
  return (
    <div className="rounded-xl border border-[var(--color-border-subtle)] bg-[var(--color-bg-surface)] p-3 shadow-[var(--shadow-card)]">
      <div className="flex flex-col gap-2.5">
        {rows.map((r) => (
          <div key={r.dir + r.target} className="flex items-center gap-2">
            <span className="grid h-6 w-6 place-items-center rounded-md text-[10px] font-bold" style={{ background: "color-mix(in srgb, var(--color-role-dev) 16%, transparent)", color: "var(--color-role-dev-ink)" }}>O</span>
            <span className="rounded px-1.5 py-0.5 text-[10px] font-medium" style={{ color: r.tone, background: `color-mix(in srgb, ${r.tone} 12%, transparent)`, border: `1px solid color-mix(in srgb, ${r.tone} 28%, transparent)` }}>{r.dir}</span>
            <span className="grid h-6 w-6 place-items-center rounded-md text-[10px] font-bold" style={{ background: `color-mix(in srgb, ${r.tc} 16%, transparent)`, color: r.tc }}>{r.ti}</span>
            <span className="text-[11px] text-[var(--color-text-2)]">{r.target}</span>
          </div>
        ))}
      </div>
      <div className="mt-2 text-[10.5px] text-[var(--color-role-design-ink)]">View all (4) →</div>
    </div>
  );
}

function GridLensDemo() {
  const ref = useRef<HTMLDivElement>(null);
  return (
    <div
      ref={ref}
      data-testid="grid-lens-demo"
      onMouseMove={(e) => {
        const el = ref.current;
        if (!el) return;
        const r = el.getBoundingClientRect();
        el.style.setProperty("--mx", `${e.clientX - r.left}px`);
        el.style.setProperty("--my", `${e.clientY - r.top}px`);
      }}
      className="grid-lens relative h-[300px] overflow-hidden rounded-xl border border-[var(--color-border-subtle)]"
      style={{ background: "linear-gradient(180deg,#ffffff,var(--color-bg-page))" }}
    >
      {/* a frosted, translucent card floating over the lensed grid (z-[1] so the
          dot grid sits beneath it, not painted on top) */}
      <div className="card-frost absolute left-8 top-10 z-[1] w-[260px] rounded-xl border border-[var(--color-border-subtle)] p-4 shadow-[var(--shadow-raised)]">
        <div className="flex items-center justify-between">
          <span className="mono text-[12px] font-medium">p-manifest</span>
          <StatusPill status="in_progress" />
        </div>
        <div className="mt-1.5 text-[11px] text-[var(--color-text-2)]">
          opencode <span className="text-[var(--color-text-3)]">→</span> claude
        </div>
        <div className="mt-2 text-[10.5px] text-[var(--color-text-3)]">半透明磨砂卡片 · 网格透过</div>
      </div>
      <div className="card-frost absolute right-10 bottom-12 z-[1] w-[220px] rounded-xl border border-[var(--color-border-subtle)] p-3 shadow-[var(--shadow-raised)]">
        <div className="flex items-center justify-between">
          <span className="text-[12px] font-[650]">claude</span>
          <StatusPill status="awaiting_review" />
        </div>
        <div className="mt-1 text-[11px] text-[var(--color-text-2)]">orchestrator · reviewer</div>
      </div>
      <div className="pointer-events-none absolute bottom-3 left-1/2 -translate-x-1/2 text-[10.5px] text-[var(--color-text-3)]">
        ← 在此区域移动鼠标，看网格放大镜
      </div>
    </div>
  );
}

function Swatch({ label, bg, solid, border }: { label: string; bg: string; solid?: boolean; border?: boolean }) {
  return (
    <div className="flex flex-col items-center gap-1">
      <div
        className="h-12 w-16 rounded-md"
        style={{
          background: bg,
          border: border ? "1px solid var(--color-border-subtle)" : solid ? "none" : "1px solid var(--color-border-subtle)",
        }}
      />
      <span className="text-[10px] text-[var(--color-text-3)]">{label}</span>
    </div>
  );
}
