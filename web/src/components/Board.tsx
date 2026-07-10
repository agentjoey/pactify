import { useEffect, useMemo, useState, type CSSProperties } from "react";
import type { State, BoardTask } from "../lib/types";
import { designBoard, taskMetrics, fmtTokens, eventsByTask, statsByTask, type DesignColumn } from "../lib/derive";
import type { PactEvent } from "../lib/types";
import { statusColorVar } from "../lib/lifecycle";
import { TaskCard } from "./TaskCard";
import { statusColor } from "./ui/StatusPill";
import { BoardSkeleton } from "./Skeleton";
import { casteForRoles, padGradient } from "../lib/ants";
import { type ProjectStats } from "../lib/api";
import { useDataSource } from "../lib/datasource";
import { humanizeError } from "../lib/protocolErrors";
import { Alert } from "./ui/Alert";

// The five dark-handoff columns, left→right. `review` merges awaiting_review +
// changes_requested; `shipped` collects delivered features (see derive.designBoard).
const COLS: { key: DesignColumn; label: string }[] = [
  { key: "assigned", label: "assigned" },
  { key: "working", label: "working" },
  { key: "review", label: "review" },
  { key: "accepted", label: "accepted" },
  { key: "shipped", label: "shipped" },
];

const GHOST: Record<DesignColumn, string> = {
  assigned: "No tasks to assign",
  working: "No tasks in progress",
  review: "No tasks under review",
  accepted: "No accepted tasks yet",
  shipped: "Nothing shipped yet",
};

export function Board({
  state,
  events = [],
  selected,
  onSelect,
  pulses,
  staleTasks,
  loading,
  project,
  author,
  onChanged,
  onOpenCockpit,
}: {
  state: State;
  events?: PactEvent[];
  selected: string;
  onSelect: (id: string) => void;
  pulses?: Set<string>;
  staleTasks?: Set<string>;
  loading?: boolean;
  project?: string;
  author?: boolean;
  // Bump the parent refresh tick after a pact verb so the board re-reads state.
  onChanged?: () => void;
  /** Open the cockpit panel preselecting a seat (e.g. from a task card). */
  onOpenCockpit?: (seat: string) => void;
}) {
  const src = useDataSource();
  const canWrite = src.capabilities.canWrite;
  // accepted + shipped are terminal columns that grow unbounded; show only the
  // most recent RECENT and fold the rest behind a per-column expander.
  const RECENT = 6;
  const FOLD_COLS: DesignColumn[] = ["accepted", "shipped"];
  const [expandedCols, setExpandedCols] = useState<Set<DesignColumn>>(new Set());
  const [featureFilter, setFeatureFilter] = useState<string | null>(null);
  const [pending, setPending] = useState<string | null>(null);
  const [err, setErr] = useState("");
  const [inlineChangesId, setInlineChangesId] = useState<string | null>(null);
  const [inlineReason, setInlineReason] = useState("");
  const [stats, setStats] = useState<ProjectStats | null>(null);

  // Per-task tokens live in GET /stats, not the event log (no pact event carries
  // token counts). Re-fetch when the project or its state changes; the short
  // debounce coalesces SSE refresh bursts into one request. Failures keep the
  // last snapshot — TOK degrades to "—", never blocks the board.
  useEffect(() => {
    if (!project) return;
    let cancelled = false;
    const t = setTimeout(() => {
      src.getStats(project)
        .then((s) => {
          if (!cancelled) setStats(s);
        })
        .catch(() => {});
    }, 200);
    return () => {
      cancelled = true;
      clearTimeout(t);
    };
  }, [project, state]);

  // Once-per-render indexes: per-task event slices + per-task stats entries, so
  // each card does O(own events) work instead of scanning the whole log.
  const byTask = useMemo(() => eventsByTask(events), [events]);
  const statMap = useMemo(() => statsByTask(stats), [stats]);
  // Single clock per render — every card's RUN ticks from the same instant.
  const nowMs = Date.now();
  // Full-task status map for dependency-blocked checks (assigned/working cards).
  const taskStatus = useMemo(() => {
    const m = new Map<string, string>();
    for (const f of state.features) for (const t of f.tasks) m.set(t.id, t.status);
    return m;
  }, [state]);

  // A successful verb keeps `pending` set until the refreshed state moves the
  // task out of awaiting_review — clearing eagerly would re-enable Accept in
  // the SSE round-trip window (double submit). This effect releases it once
  // the card has actually left review, so a task that re-enters review later
  // (changes → re-review) gets a live button again.
  useEffect(() => {
    if (!pending) return;
    const stillAwaiting = state.features.some((f) =>
      f.tasks.some((t) => t.id === pending && t.status === "awaiting_review"),
    );
    if (!stillAwaiting) setPending(null);
  }, [state, pending]);

  // Per-feature rollup for the context-header filter chips (accepted/total +
  // token volume). Memoized so it doesn't recompute on every hover/selection.
  const featureChips = useMemo(
    () =>
      state.features.map((f) => ({
        id: f.id,
        accepted: f.tasks.filter((t) => t.status === "accepted" || t.status === "shipped").length,
        total: f.tasks.length,
        tokens: f.tasks.reduce((n, t) => n + (statMap.get(t.id)?.tokens ?? 0), 0),
      })),
    [state.features, statMap],
  );

  const totalTasks = state.features.reduce((n, f) => n + f.tasks.length, 0);
  const totalTokens = featureChips.reduce((n, c) => n + c.tokens, 0);
  const seated = state.agents.filter((a) => (a.roles?.length ?? 0) > 0);

  if (loading) return <BoardSkeleton />;

  const rolesMap = new Map(state.agents.map((a) => [a.id, a.roles]));
  const rolesOf = (seatId: string): string[] => rolesMap.get(seatId) ?? [];

  // Apply the feature filter (null = all) before bucketing.
  const filtered: State =
    featureFilter == null
      ? state
      : { ...state, features: state.features.filter((f) => f.id === featureFilter) };
  const cols = designBoard(filtered);

  async function verb(taskId: string, v: "accept" | "changes", reason?: string): Promise<boolean> {
    if (!project) return false;
    setPending(taskId);
    setErr("");
    try {
      await src.verb!(project, v, reason ? { task: taskId, reason } : { task: taskId });
      onChanged?.();
      return true;
    } catch (e) {
      setErr(humanizeError(e instanceof Error ? e.message : String(e)));
      setPending(null);
      return false;
    }
  }

  function renderCard(bt: BoardTask, col: DesignColumn) {
    const pulsing = pulses?.has(bt.task.id);
    const isReview = col === "review";
    const blockedOn =
      (col === "assigned" || col === "working") && bt.task.deps
        ? bt.task.deps.filter((depId) => {
            const depStatus = taskStatus.get(depId);
            return depStatus !== "accepted" && depStatus !== "shipped";
          })
        : undefined;
    const reviewActions =
      isReview && author && project ? (
        inlineChangesId === bt.task.id ? (
          <form
            data-testid="inline-changes-form"
            className="flex w-full flex-col gap-1"
            onClick={(e) => e.stopPropagation()}
            onSubmit={async (e) => {
              e.preventDefault();
              const reason = inlineReason.trim();
              if (!reason) return;
              if (await verb(bt.task.id, "changes", reason)) {
                setInlineChangesId(null);
                setInlineReason("");
              }
            }}
          >
            <textarea
              value={inlineReason}
              onChange={(e) => {
                const el = e.target;
                setInlineReason(el.value);
                el.style.height = "auto";
                el.style.height = `${el.scrollHeight}px`;
              }}
              placeholder="Reason for changes…"
              autoFocus
              rows={1}
              className="w-full resize-none rounded-md border border-[var(--color-border-strong)] bg-[var(--color-bg-overlay)] px-2 py-1 text-[11px] text-[var(--color-text-1)] outline-none placeholder:text-[var(--color-text-3)]"
            />
            <div className="flex justify-end gap-1.5">
              <button
                type="button"
                data-testid="inline-changes-cancel"
                onClick={() => {
                  setInlineChangesId(null);
                  setInlineReason("");
                }}
                className="rounded-[6px] border px-2.5 py-1 text-[11px] font-medium"
                style={{
                  color: "var(--color-text-2)",
                  borderColor: "var(--color-border-strong)",
                }}
              >
                Cancel
              </button>
              <button
                type="submit"
                data-testid="inline-changes-send"
                disabled={!inlineReason.trim() || pending === bt.task.id || !canWrite}
                className="rounded-[6px] px-2.5 py-1 text-[11px] font-medium disabled:opacity-50"
                style={{ background: "var(--color-role-ops)", color: "var(--color-bg-page)" }}
              >
                Send
              </button>
            </div>
          </form>
        ) : (
          <div className="flex flex-wrap items-center gap-1.5">
            {src.cockpitSubscribe && project && onOpenCockpit && (
              <button
                type="button"
                data-testid="card-discuss-cockpit"
                onClick={(e) => {
                  e.stopPropagation();
                  onOpenCockpit(bt.task.owner);
                }}
                className="rounded-[6px] px-2 py-1 text-[11px] font-medium text-[var(--color-role-design)] hover:underline"
              >
                Discuss in Cockpit →
              </button>
            )}
            <button
              type="button"
              data-testid="card-accept"
              disabled={pending === bt.task.id || !canWrite}
              title={canWrite ? undefined : "Remote control needs U3"}
              onClick={(e) => {
                e.stopPropagation();
                verb(bt.task.id, "accept");
              }}
              className="rounded-[6px] px-2.5 py-1 text-[11px] font-medium disabled:opacity-50"
              style={{ background: "var(--color-success)", color: "var(--color-bg-page)" }}
            >
              ✓ Accept
            </button>
            <button
              type="button"
              data-testid="card-changes"
              disabled={!canWrite}
              title={canWrite ? undefined : "Remote control needs U3"}
              onClick={(e) => {
                e.stopPropagation();
                setInlineChangesId(bt.task.id);
              }}
              className="rounded-[6px] border px-2.5 py-1 text-[11px] font-medium disabled:opacity-50"
              style={{
                color: "var(--color-role-ops)",
                borderColor: "color-mix(in srgb, var(--color-role-ops) 40%, transparent)",
                background: "color-mix(in srgb, var(--color-role-ops) 14%, transparent)",
              }}
            >
              ↺ Changes
            </button>
          </div>
        )
      ) : undefined;

    return (
      <div
        key={bt.task.id}
        data-testid={pulsing ? "board-pulse" : undefined}
        className={pulsing ? "pulse rounded-[11px]" : undefined}
        style={pulsing ? ({ "--pulse-color": statusColor(bt.task.status) } as CSSProperties) : undefined}
      >
        <TaskCard
          task={bt.task}
          featureId={bt.feature}
          ownerRoles={rolesOf(bt.task.owner)}
          reviewerRoles={rolesOf(bt.task.reviewer)}
          stale={staleTasks?.has(bt.task.id)}
          blockedOn={blockedOn?.length ? blockedOn : undefined}
          selected={selected === bt.task.id}
          metrics={taskMetrics(bt.task, byTask.get(bt.task.id) ?? [], nowMs, statMap.get(bt.task.id))}
          reviewActions={reviewActions}
          onClick={() => onSelect(bt.task.id)}
        />
      </div>
    );
  }

  return (
    <div className="flex flex-1 flex-col overflow-hidden">
      {/* Context header — feature filter chips + seated cluster + new task */}
      <div
        data-testid="board-context-header"
        className="flex items-center gap-2 border-b border-[var(--color-border-subtle)] px-[18px] py-[9px]"
        style={{ background: "var(--color-bg-inset)" }}
      >
        <div className="flex min-w-0 flex-1 items-center gap-1.5 overflow-x-auto">
          <FilterChip
            label={`All tasks ${totalTasks}`}
            active={featureFilter == null}
            onClick={() => setFeatureFilter(null)}
          />
          {featureChips.map((c) => (
            <FilterChip
              key={c.id}
              label={c.id}
              active={featureFilter === c.id}
              progress={c.total ? c.accepted / c.total : 0}
              tokens={c.tokens ? fmtTokens(c.tokens) : undefined}
              onClick={() => setFeatureFilter((f) => (f === c.id ? null : c.id))}
            />
          ))}
        </div>

        <div className="ml-auto flex shrink-0 items-center gap-3">
          <div className="flex items-center">
            {seated.slice(0, 6).map((a, i) => {
              const caste = casteForRoles(a.roles ?? []);
              const pad = padGradient(a.id, caste);
              return (
                <span
                  key={a.id}
                  title={a.id}
                  className="grid h-[26px] w-[26px] place-items-center rounded-[7px] text-[10px] font-semibold"
                  style={{
                    background: `linear-gradient(135deg, ${pad.from}, ${pad.to})`,
                    color: "var(--color-bg-page)",
                    marginLeft: i === 0 ? 0 : -7,
                    boxShadow: "0 0 0 2px var(--color-bg-page)",
                    fontFamily: "var(--font-mono)",
                  }}
                >
                  {a.id.slice(0, 2)}
                </span>
              );
            })}
          </div>
          <span className="mono whitespace-nowrap text-[10.5px] text-[var(--color-text-2)]">
            {seated.length} seated · {totalTasks} tasks · {fmtTokens(totalTokens)} tok
          </span>
          <span className="h-4 w-px bg-[var(--color-border-strong)]" />
          <button
            type="button"
            data-testid="board-new-task"
            onClick={() => window.dispatchEvent(new CustomEvent("pactify:cmdk"))}
            className="rounded-[6px] px-3 py-1 text-[11.5px] font-medium"
            style={{ background: "var(--color-role-design)", color: "var(--color-bg-page)" }}
          >
            ＋ New task
          </button>
        </div>
      </div>

      {/* Inline-action failures persist here (same Alert pattern as RightRail);
          a toast would vanish before the reviewer reads why the accept bounced. */}
      {err && (
        <div data-testid="board-verb-error" className="px-[18px] pt-3">
          <Alert tone="danger" title="Action failed" onRetry={() => setErr("")} retryLabel="Dismiss">
            {err}
          </Alert>
        </div>
      )}

      {/* Columns — full-width 5-col grid (no fixed left dock) */}
      <div
        className="grid flex-1 gap-4 overflow-x-auto p-[18px]"
        style={{ gridTemplateColumns: `repeat(${COLS.length}, minmax(248px, 1fr))` }}
      >
        {COLS.map(({ key, label }) => {
          const tasks = cols[key] ?? [];
          const dot = statusColorVar(key === "working" ? "in_progress" : key === "review" ? "awaiting_review" : key);
          const dueCount = key === "review" ? tasks.filter((t) => t.task.status === "awaiting_review").length : 0;
          // accepted + shipped fold to the most-recent RECENT (newest first).
          const foldable = FOLD_COLS.includes(key);
          const ordered = foldable ? [...tasks].reverse() : tasks;
          const shown = foldable && !expandedCols.has(key) ? ordered.slice(0, RECENT) : ordered;
          return (
            <div key={key} className="min-h-[360px]">
              <div className="flex items-center gap-[7px] px-1 pb-2.5 pt-0.5">
                <span
                  className="h-[7px] w-[7px] rounded-full"
                  style={{ background: dot, boxShadow: key === "review" ? `0 0 6px ${dot}` : undefined }}
                />
                <span className="text-[11px] font-semibold uppercase tracking-[.5px] text-[var(--color-text-2)]">
                  {label}
                </span>
                <span className="mono ml-auto rounded-full bg-white/[.06] px-[7px] py-px text-[10px] tabular-nums text-[var(--color-text-3)]">
                  {tasks.length}
                </span>
                {dueCount > 0 && (
                  <span
                    className="rounded-full px-1.5 py-px text-[9.5px] font-medium"
                    style={{ color: "var(--color-warn)", background: "color-mix(in srgb, var(--color-warn) 14%, transparent)" }}
                  >
                    {dueCount} due
                  </span>
                )}
              </div>
              <div className="flex flex-col gap-[11px]">
                {tasks.length === 0 ? (
                  <div className="kb-ghost">{GHOST[key]}</div>
                ) : (
                  <>
                    {shown.map((bt) => renderCard(bt, key))}
                    {foldable && ordered.length > RECENT && (
                      <button
                        type="button"
                        data-testid={`${key}-${expandedCols.has(key) ? "less" : "more"}`}
                        className="mt-1 w-full rounded-md border border-[var(--color-success)]/30 bg-[var(--color-success)]/8 px-2 py-1.5 text-left text-[11px] text-[var(--color-text-2)]"
                        onClick={() =>
                          setExpandedCols((prev) => {
                            const next = new Set(prev);
                            next.has(key) ? next.delete(key) : next.add(key);
                            return next;
                          })
                        }
                      >
                        {expandedCols.has(key) ? `▾ show recent ${RECENT}` : `▸ ${ordered.length - RECENT} more ${key}`}
                      </button>
                    )}
                  </>
                )}
              </div>
            </div>
          );
        })}
      </div>
    </div>
  );
}

function FilterChip({
  label,
  active,
  progress,
  tokens,
  onClick,
}: {
  label: string;
  active: boolean;
  progress?: number;
  tokens?: string;
  onClick: () => void;
}) {
  return (
    <button
      type="button"
      data-testid="feature-chip"
      onClick={onClick}
      className="flex shrink-0 items-center gap-2 rounded-[8px] border px-3 py-1.5 text-[11.5px] font-medium"
      style={{
        color: active ? "var(--color-text-1)" : "var(--color-text-2)",
        background: active ? "rgba(255,255,255,.07)" : "transparent",
        borderColor: active ? "var(--color-border-strong)" : "transparent",
      }}
    >
      <span className="mono">{label}</span>
      {progress != null && (
        <span className="h-1 w-[34px] overflow-hidden rounded-full bg-white/10">
          <span className="block h-full rounded-full" style={{ width: `${Math.round(progress * 100)}%`, background: "var(--color-success)" }} />
        </span>
      )}
      {tokens && <span className="mono text-[10px] text-[var(--color-text-3)]">{tokens}</span>}
    </button>
  );
}
