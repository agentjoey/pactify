import { useEffect, useMemo, useState } from "react";
import type { State, PactEvent, ProjectMeta, OrchestrateStatusResponse } from "../lib/types";
import type { Worktree, ProjectStats } from "../lib/api";
import { useDataSource } from "../lib/datasource";
import { ProjectMenu } from "./shell/ProjectMenu";
import { MiniPipeline } from "./ui/MiniPipeline";
import { ReviewGate } from "./ui/ReviewGate";
import { FallbackCards } from "./ui/FallbackCard";
import {
  deriveDashboardKPIs,
  deriveFeatureLanes,
  deriveSeatRoster,
  deriveActivityFeed,
  deriveRunProgress,
  deriveRunStats,
  fmtRelTime,
} from "../lib/dashboard";
import { fmtDuration, fmtTokens } from "../lib/derive";

const LAST_SEEN_KEY = "pactify:dashboard:lastSeen";

export interface DashboardProps {
  project: string;
  projects: ProjectMeta[];
  state: State;
  events?: PactEvent[];
  author?: boolean;
  running?: boolean;
  orchestrateStatus?: OrchestrateStatusResponse;
  runningByProject?: Record<string, boolean>;
  worktreesByProject?: Record<string, Worktree[]>;
  currentWorktree?: string;
  onSelectProject: (name: string) => void;
  onRenameProject: (name: string) => void;
  onDeleteProject: (name: string) => void;
  onAddProject: () => void;
  onSelectWorktree?: (project: string, branch: string) => void;
  onOpenBoard: (taskId?: string) => void;
  onOpenCockpit: (seat: string) => void;
  onChanged?: () => void;
}

function useNow(intervalMs = 60_000) {
  const [now, setNow] = useState(Date.now);
  useEffect(() => {
    const t = setInterval(() => setNow(Date.now()), intervalMs);
    return () => clearInterval(t);
  }, [intervalMs]);
  return now;
}

function useLastSeen() {
  const [lastSeen, setLastSeen] = useState(() => {
    if (typeof localStorage === "undefined") return 0;
    const raw = localStorage.getItem(LAST_SEEN_KEY);
    const n = raw ? Number(raw) : 0;
    return Number.isNaN(n) ? 0 : n;
  });
  const markSeen = () => {
    const now = Date.now();
    if (typeof localStorage !== "undefined") localStorage.setItem(LAST_SEEN_KEY, String(now));
    setLastSeen(now);
  };
  return { lastSeen, markSeen };
}

export function Dashboard({
  project,
  projects,
  state,
  events = [],
  author,
  running,
  orchestrateStatus,
  runningByProject,
  worktreesByProject,
  currentWorktree,
  onSelectProject,
  onRenameProject,
  onDeleteProject,
  onAddProject,
  onSelectWorktree,
  onOpenBoard,
  onOpenCockpit,
  onChanged,
}: DashboardProps) {
  const src = useDataSource();
  const nowMs = useNow();
  const { lastSeen, markSeen } = useLastSeen();
  const [stats, setStats] = useState<ProjectStats | null>(null);

  useEffect(() => {
    if (!project) return;
    let cancelled = false;
    const t = setTimeout(() => {
      src
        .getStats(project)
        .then((s) => {
          if (!cancelled) setStats(s);
        })
        .catch(() => {});
    }, 200);
    return () => {
      cancelled = true;
      clearTimeout(t);
    };
  }, [project, state, src]);

  useEffect(() => {
    // Mark the feed as seen after the user has had the dashboard in view for
    // a short beat; immediate update would hide the "new" count on entry.
    const t = setTimeout(markSeen, 3000);
    return () => clearTimeout(t);
  }, [project, markSeen]);

  const kpis = useMemo(
    () => deriveDashboardKPIs(state, stats, orchestrateStatus, events, nowMs),
    [state, stats, orchestrateStatus, events, nowMs],
  );
  const lanes = useMemo(
    () => deriveFeatureLanes(state, stats, events, nowMs),
    [state, stats, events, nowMs],
  );
  const roster = useMemo(() => deriveSeatRoster(state, events, stats), [state, events, stats]);
  const feed = useMemo(
    () => deriveActivityFeed(events, state, nowMs, lastSeen),
    [events, state, nowMs, lastSeen],
  );
  const runProgress = useMemo(
    () => deriveRunProgress(state, orchestrateStatus),
    [state, orchestrateStatus],
  );
  const runStats = useMemo(
    () => deriveRunStats(state, stats, orchestrateStatus, events, nowMs),
    [state, stats, orchestrateStatus, events, nowMs],
  );

  const branch = currentWorktree || "main";
  const featureCount = state.features.length;
  const seatCount = state.agents.length;

  return (
    <div
      data-testid="view-dashboard"
      className="view-enter flex flex-1 flex-col overflow-hidden bg-[var(--color-bg-page)]"
    >
      <ContextBar
        projects={projects}
        currentProjectId={project}
        running={running ?? false}
        runningByProject={runningByProject}
        worktreesByProject={worktreesByProject}
        currentWorktree={currentWorktree}
        onSelectProject={onSelectProject}
        onRenameProject={onRenameProject}
        onDeleteProject={onDeleteProject}
        onAddProject={onAddProject}
        onSelectWorktree={onSelectWorktree}
        subtitle={`${branch} · ${featureCount} features · ${seatCount} seats`}
      />

      <div className="flex flex-1 flex-col gap-4 overflow-y-auto p-[22px]">
        <KPIStrip kpis={kpis} />

        <div className="grid gap-4" style={{ gridTemplateColumns: "1.55fr 1fr", alignItems: "start" }}>
          <div className="flex flex-col gap-[14px]">
            <RunControl
              project={project}
              running={kpis.activeRun.live}
              stats={runStats}
              progress={runProgress}
              author={author}
              onChanged={onChanged}
            />
            <FallbackCards
              project={project}
              canWrite={!!author && src.capabilities.canWrite}
              onApproved={onChanged}
            />
            {lanes.map((lane) => (
              <FeatureLaneCard
                key={lane.feature.id}
                lane={lane}
                project={project}
                author={author}
                onOpenBoard={onOpenBoard}
                onOpenCockpit={onOpenCockpit}
                onChanged={onChanged}
              />
            ))}
            {lanes.length === 0 && (
              <div className="rounded-[13px] border border-[var(--border-2)] bg-[var(--bg-panel)] p-4 text-sm text-[var(--color-text-3)]">
                No active feature lanes.
              </div>
            )}
          </div>

          <div className="flex flex-col gap-[14px]">
            <SeatsRoster roster={roster} onOpenCockpit={onOpenCockpit} />
            <ActivityFeed feed={feed} nowMs={nowMs} />
          </div>
        </div>
      </div>
    </div>
  );
}

function ContextBar({
  projects,
  currentProjectId,
  running,
  runningByProject,
  worktreesByProject,
  currentWorktree,
  onSelectProject,
  onRenameProject,
  onDeleteProject,
  onAddProject,
  onSelectWorktree,
  subtitle,
}: {
  projects: ProjectMeta[];
  currentProjectId: string;
  running: boolean;
  runningByProject?: Record<string, boolean>;
  worktreesByProject?: Record<string, Worktree[]>;
  currentWorktree?: string;
  onSelectProject: (name: string) => void;
  onRenameProject: (name: string) => void;
  onDeleteProject: (name: string) => void;
  onAddProject: () => void;
  onSelectWorktree?: (project: string, branch: string) => void;
  subtitle: string;
}) {
  return (
    <div
      data-testid="dashboard-context-bar"
      className="flex shrink-0 items-center gap-3 border-b border-[var(--border-2)] bg-[var(--bg-panel)] px-[22px] py-[10px]"
    >
      <ProjectMenu
        projects={projects}
        current={currentProjectId}
        running={running}
        dot={<span className="h-[6px] w-[6px] rounded-[2px] bg-[var(--proj)]" />}
        runningByProject={runningByProject}
        worktreesByProject={worktreesByProject}
        currentWorktree={currentWorktree}
        onSelect={onSelectProject}
        onRename={onRenameProject}
        onDelete={onDeleteProject}
        onAdd={onAddProject}
        onSelectWorktree={onSelectWorktree}
      />
      <span className="font-mono text-[11px] text-[var(--text-3)]">{subtitle}</span>
      <span className="flex-1" />
      <button
        type="button"
        data-testid="dashboard-new-task"
        onClick={() => window.dispatchEvent(new CustomEvent("pactify:cmdk"))}
        className="inline-flex items-center gap-[7px] rounded-[9px] px-[14px] py-[7px] text-[12.5px] font-semibold text-[var(--accent-ink)]"
        style={{ background: "var(--accent)" }}
      >
        <PlusIcon />
        New task
      </button>
    </div>
  );
}

function KPIStrip({ kpis }: { kpis: ReturnType<typeof deriveDashboardKPIs> }) {
  return (
    <div data-testid="dashboard-kpi-strip" className="grid grid-cols-4 gap-3">
      <KPICard
        testId="kpi-active-run"
        label="Active run"
        value={String(kpis.activeRun.count)}
        qualifier={kpis.activeRun.label}
        qualifierLive={kpis.activeRun.live}
      />
      <KPICard
        testId="kpi-awaiting-review"
        label="Awaiting review"
        value={String(kpis.awaitingReview.count)}
        qualifier={kpis.awaitingReview.label}
        valueColor="var(--warn)"
      />
      <KPICard
        testId="kpi-tokens-today"
        label="Tokens today"
        value={kpis.tokensToday.tokens}
        qualifier={kpis.tokensToday.cost}
      />
      <KPICard
        testId="kpi-shipped-7d"
        label="Shipped · 7d"
        value={String(kpis.shipped7d.count)}
        qualifier={kpis.shipped7d.label}
        valueColor="var(--dev)"
      />
    </div>
  );
}

function KPICard({
  testId,
  label,
  value,
  qualifier,
  qualifierLive,
  valueColor,
}: {
  testId: string;
  label: string;
  value: string;
  qualifier: string;
  qualifierLive?: boolean;
  valueColor?: string;
}) {
  return (
    <div data-testid={testId} className="rounded-[12px] border border-[var(--border-2)] bg-[var(--bg-panel)] p-[14px_15px]">
      <div className="flex items-center gap-[7px] font-mono text-[9px] uppercase tracking-[0.16em] text-[var(--text-4)]">
        {label}
      </div>
      <div className="mt-2 flex items-baseline gap-2">
        <span
          data-testid={`${testId}-value`}
          className="font-mono text-[26px] font-bold"
          style={{ color: valueColor ?? "var(--text)" }}
        >
          {value}
        </span>
        <span
          className="inline-flex items-center gap-[5px] text-[11px]"
          style={{ color: qualifierLive ? "var(--info)" : "var(--text-3)" }}
        >
          {qualifierLive && (
            <span
              className="shell-breath h-[6px] w-[6px] rounded-full"
              style={{ background: "var(--info)", boxShadow: "0 0 6px var(--info)" }}
            />
          )}
          {qualifier}
        </span>
      </div>
    </div>
  );
}

function RunControl({
  project,
  running,
  stats,
  progress,
  author,
  onChanged,
}: {
  project: string;
  running: boolean;
  stats: ReturnType<typeof deriveRunStats>;
  progress: number;
  author?: boolean;
  onChanged?: () => void;
}) {
  const src = useDataSource();
  const [busy, setBusy] = useState(false);

  async function run() {
    if (!src.runOrchestrate) return;
    setBusy(true);
    try {
      await src.runOrchestrate(project, {});
      onChanged?.();
    } catch {
      // parent toast / refresh handles errors
    } finally {
      setBusy(false);
    }
  }

  async function stop() {
    if (!src.stopOrchestrate) return;
    setBusy(true);
    try {
      await src.stopOrchestrate(project);
      onChanged?.();
    } catch {
      // parent toast / refresh handles errors
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="rounded-[13px] border border-[var(--border-2)] bg-[var(--bg-panel)] p-[15px_16px]">
      <div className="flex items-center gap-[10px]">
        {running ? (
          <span className="inline-flex items-center gap-[7px] text-[14px] font-semibold text-[var(--color-text-1)]">
            <span
              className="shell-breath h-2 w-2 rounded-full"
              style={{ background: "var(--info)", boxShadow: "0 0 8px var(--info)" }}
            />
            Orchestrating
          </span>
        ) : (
          <span className="text-[14px] font-semibold text-[var(--color-text-1)]">Idle</span>
        )}
        <span className="flex-1" />
        {running ? (
          <>
            {/* Pause is not supported by the current backend slice; omit per spec. */}
            <button
              type="button"
              data-testid="run-control-stop"
              disabled={busy || !src.stopOrchestrate}
              title={src.stopOrchestrate ? undefined : "Stop endpoint not available"}
              onClick={stop}
              className="rounded-lg px-[13px] py-[6px] text-[11.5px] font-semibold text-[var(--err)] disabled:opacity-50"
              style={{
                background: "rgba(249,112,102,0.1)",
                border: "1px solid rgba(249,112,102,0.32)",
              }}
            >
              Stop
            </button>
          </>
        ) : (
          <button
            type="button"
            data-testid="run-control-run"
            disabled={busy || !src.runOrchestrate || !author}
            onClick={run}
            className="rounded-lg px-[13px] py-[6px] text-[11.5px] font-semibold text-[var(--color-on-accent)] disabled:opacity-50"
            style={{ background: "var(--color-role-design)" }}
          >
            Run
          </button>
        )}
      </div>
      <div className="mt-[9px] font-mono text-[11px] text-[var(--text-3)]">
        {stats.features} features · concurrency {stats.concurrency} · iter {stats.iter} · {fmtTokens(stats.tokens)} tok · {fmtDuration(stats.elapsedMs)} · {stats.cost}
      </div>
      <div className="mt-[11px] h-[6px] overflow-hidden rounded-full bg-[var(--bg-input)]">
        <div
          data-testid="run-control-progress"
          className="h-full rounded-full"
          style={{
            width: `${Math.round(progress * 100)}%`,
            background: "linear-gradient(90deg, var(--accent), var(--dev))",
          }}
        />
      </div>
    </div>
  );
}

function FeatureLaneCard({
  lane,
  project,
  author,
  onOpenBoard,
  onOpenCockpit,
  onChanged,
}: {
  lane: ReturnType<typeof deriveFeatureLanes>[number];
  project: string;
  author?: boolean;
  onOpenBoard: (taskId?: string) => void;
  onOpenCockpit: (seat: string) => void;
  onChanged?: () => void;
}) {
  return (
    <div
      data-testid={`feature-lane-${lane.feature.id}`}
      className="rounded-[13px] border border-[var(--border-2)] bg-[var(--bg-panel)] p-[15px_16px]"
    >
      <div className="flex items-center gap-[9px]">
        <span className="font-mono text-[13px] font-semibold text-[var(--text)]">{lane.feature.id}</span>
        <span className="font-mono text-[10.5px] text-[var(--text-4)]">{lane.feature.branch}</span>
        <span className="flex-1" />
        <span className="font-mono text-[10.5px] text-[var(--text-3)]">
          {fmtTokens(lane.tokens)} tok · {fmtDuration(lane.elapsedMs)}
        </span>
        <span className="font-mono text-[10.5px] text-[var(--text-2)]">
          {lane.progress.done}/{lane.progress.total}
        </span>
      </div>
      <div className="mt-[13px]">
        <MiniPipeline
          tasks={lane.feature.tasks}
          merge={lane.feature.status !== "shipped"}
          testId={`feature-lane-pipeline-${lane.feature.id}`}
        />
      </div>
      {lane.reviewTask && (
        <div className="mt-[13px]">
          <ReviewGate
            project={project}
            task={lane.reviewTask}
            featureId={lane.feature.id}
            author={author}
            onChanged={onChanged}
            onSeeDiff={() => onOpenBoard(lane.reviewTask?.id)}
            onTakeOver={() => onOpenCockpit(lane.reviewTask?.owner ?? "")}
          />
        </div>
      )}
    </div>
  );
}

function SeatsRoster({
  roster,
  onOpenCockpit,
}: {
  roster: ReturnType<typeof deriveSeatRoster>;
  onOpenCockpit: (seat: string) => void;
}) {
  return (
    <div className="rounded-[13px] border border-[var(--border-2)] bg-[var(--bg-panel)] px-[15px] pb-[6px] pt-[4px]">
      <div className="flex items-center gap-2 py-[13px_4px]">
        <span className="text-[13px] font-semibold text-[var(--text)]">Seats</span>
        <span className="font-mono text-[10.5px] text-[var(--text-4)]">{roster.length} seated</span>
      </div>
      {roster.map((entry) => (
        <button
          key={entry.seat.id}
          type="button"
          data-testid={`seat-row-${entry.seat.id}`}
          onClick={() => onOpenCockpit(entry.seat.id)}
          className="flex w-full items-center gap-[11px] border-t border-[var(--border-2)] py-[11px] text-left transition-colors hover:bg-white/[0.02]"
          style={{ opacity: entry.status === "idle" ? 0.6 : 1 }}
        >
          <AgentAvatar seatId={entry.seat.id} />
          <div className="min-w-0 flex-1">
            <div className="flex items-center gap-[6px]">
              <span className="text-[13px] font-semibold text-[var(--text)]">{entry.seat.id}</span>
              {entry.seat.roles.map((r) => (
                <RoleChip key={r} role={r} />
              ))}
            </div>
            <div className="mt-[2px] font-mono text-[10px] text-[var(--text-4)]">
              {entry.status === "working" && entry.currentTask
                ? `${entry.currentTask} · ${fmtTokens(entry.tokens)} tok · running`
                : `${entry.shipped} shipped · ${fmtTokens(entry.tokens)} tok`}
            </div>
          </div>
          <SeatStatus status={entry.status} />
        </button>
      ))}
    </div>
  );
}

function RoleChip({ role }: { role: string }) {
  const color = role === "orchestrator" ? "var(--proj)" : role === "reviewer" ? "var(--info)" : "var(--dev)";
  return (
    <span
      className="rounded px-[5px] py-px font-mono text-[9px]"
      style={{
        color,
        background: `color-mix(in srgb, ${color} 12%, transparent)`,
      }}
    >
      {role === "orchestrator" ? "orch" : role === "reviewer" ? "rev" : role}
    </span>
  );
}

function SeatStatus({ status }: { status: "active" | "working" | "idle" }) {
  if (status === "working") {
    return (
      <span className="inline-flex shrink-0 items-center gap-[5px] font-mono text-[10px] text-[var(--info)]">
        <span
          className="shell-breath h-[7px] w-[7px] rounded-full"
          style={{ background: "var(--info)", boxShadow: "0 0 6px var(--info)" }}
        />
        working
      </span>
    );
  }
  if (status === "active") {
    return (
      <span className="inline-flex shrink-0 items-center gap-[5px] font-mono text-[10px] text-[var(--success)]">
        <span className="h-[7px] w-[7px] rounded-full bg-[var(--success)]" />
        active
      </span>
    );
  }
  return (
    <span className="inline-flex shrink-0 items-center gap-[5px] font-mono text-[10px] text-[var(--text-4)]">
      <span className="h-[7px] w-[7px] rounded-full border-[1.5px] border-[var(--text-4)]" />
      idle
    </span>
  );
}

function ActivityFeed({
  feed,
  nowMs,
}: {
  feed: ReturnType<typeof deriveActivityFeed>;
  nowMs: number;
}) {
  const ICON: Record<string, { glyph: string; color: string; bg: string }> = {
    awaiting: { glyph: "◉", color: "var(--warn)", bg: "rgba(245,196,81,0.14)" },
    started: { glyph: "⚡", color: "var(--info)", bg: "rgba(138,180,255,0.14)" },
    accepted: { glyph: "✓", color: "var(--dev)", bg: "rgba(110,231,160,0.14)" },
    changes: { glyph: "↺", color: "var(--ops)", bg: "rgba(224,136,74,0.14)" },
    shipped: { glyph: "⇧", color: "var(--dev)", bg: "rgba(110,231,160,0.14)" },
  };

  return (
    <div className="rounded-[13px] border border-[var(--border-2)] bg-[var(--bg-panel)] px-[15px] pb-[8px] pt-[4px]">
      <div className="flex items-center gap-2 py-[13px_4px]">
        <span className="text-[13px] font-semibold text-[var(--text)]">Activity</span>
        <span className="flex-1" />
        {feed.newCount > 0 && (
          <span
            data-testid="activity-new-pill"
            className="rounded-full border border-[var(--accent-line)] px-[8px] py-[1.5px] font-mono text-[9.5px] text-[var(--accent)]"
            style={{ background: "var(--accent-2)" }}
          >
            {feed.newCount} new
          </span>
        )}
      </div>
      {feed.items.map((item) => {
        const icon = ICON[item.kind];
        return (
          <div
            key={`${item.kind}-${item.taskId ?? item.feature}-${item.ts}`}
            data-testid="activity-row"
            className="flex items-start gap-[10px] border-t border-[var(--border-2)] py-[10px]"
          >
            <span
              className="grid h-5 w-5 shrink-0 place-items-center rounded-md text-[11px]"
              style={{ background: icon.bg, color: icon.color }}
            >
              {icon.glyph}
            </span>
            <div className="min-w-0 flex-1">
              <div className="text-[12px] text-[var(--text-2)]">
                <Highlight text={item.text} />
              </div>
              <div className="mt-[2px] font-mono text-[9.5px] text-[var(--text-4)]">
                {item.feature ? `${item.feature} · ` : ""}
                {fmtRelTime(nowMs - item.ts)} ago
              </div>
            </div>
          </div>
        );
      })}
      {feed.items.length === 0 && (
        <div className="border-t border-[var(--border-2)] py-4 text-[11px] text-[var(--color-text-3)]">
          No recent activity.
        </div>
      )}
    </div>
  );
}

function Highlight({ text }: { text: string }) {
  // Bold the first token (task id or agent name) up to the first space.
  const firstSpace = text.indexOf(" ");
  if (firstSpace <= 0) return <>{text}</>;
  const head = text.slice(0, firstSpace);
  const tail = text.slice(firstSpace);
  return (
    <>
      <span className="font-medium text-[var(--text)]">{head}</span>
      {tail}
    </>
  );
}

function avatarGradient(seatId: string): string {
  const id = seatId.toLowerCase();
  if (id.startsWith("cl")) return "linear-gradient(135deg,#ffd479,#e0a93a)";
  return "linear-gradient(135deg,#6ee7a0,#39b97a)";
}

function AgentAvatar({ seatId }: { seatId: string }) {
  return (
    <span
      className="grid h-[34px] w-[34px] flex-none place-items-center rounded-[10px] font-mono text-[12px] font-semibold"
      style={{
        background: avatarGradient(seatId),
        color: "#0a0e14",
      }}
    >
      {seatId.slice(0, 2).toLowerCase()}
    </span>
  );
}

function PlusIcon() {
  return (
    <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.6" strokeLinecap="round">
      <path d="M12 5v14M5 12h14" />
    </svg>
  );
}
