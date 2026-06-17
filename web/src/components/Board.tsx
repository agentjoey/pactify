import { useMemo, useState, type CSSProperties } from "react";
import type { State, Task } from "../lib/types";
import { boardColumns, type Column } from "../lib/derive";
import { statusColorVar } from "../lib/lifecycle";
import { TaskCard } from "./TaskCard";
import { statusColor } from "./ui/StatusPill";
import { Tooltip } from "./ui/Tooltip";
import { BoardSkeleton } from "./Skeleton";

// Kanban column order + presentation per board3 / spec §4 (note: distinct from
// derive's COLUMNS order — that one is derivation-internal; this is the visual
// left→right flow assigned → in_progress → awaiting → changes → accepted).
const ORDER: Column[] = [
  "assigned",
  "in_progress",
  "awaiting_review",
  "changes_requested",
  "accepted",
];

// Per-column empty-state copy (board3 ghost text).
const GHOST: Record<string, string> = {
  assigned: "No tasks to assign",
  in_progress: "No tasks in progress",
  awaiting_review: "No tasks awaiting review",
  changes_requested: "No tasks needing rework",
  accepted: "No accepted tasks yet",
};

export function Board({
  state,
  selected,
  onSelect,
  pulses,
  staleTasks,
  loading,
}: {
  state: State;
  selected: string;
  onSelect: (id: string) => void;
  pulses?: Set<string>;
  staleTasks?: Set<string>;
  // First-load only: a project is current but its first snapshot hasn't landed.
  loading?: boolean;
}) {
  if (loading) return <BoardSkeleton />;
  const cols = boardColumns(state);
  // rolesOf indexes seat → roles so a pulsing card glows in its owner's color
  // and the ant chips pick the right caste.
  const rolesMap = new Map(state.agents.map((a) => [a.id, a.roles]));
  const rolesOf = (seat: string): string[] => rolesMap.get(seat) ?? [];

  // --- accepted column: grouped + collapsible ---
  // eslint-disable-next-line react-hooks/rules-of-hooks
  const acceptedByFeature = useMemo(() => {
    const shippedIds = new Set(state.features.filter((f) => f.status === "merged").map((f) => f.id));
    const groups: { feature: string; shipped: boolean; tasks: Task[] }[] = [];
    for (const f of state.features) {
      const accepted = f.tasks.filter((t) => t.status === "accepted");
      if (accepted.length) groups.push({ feature: f.id, shipped: shippedIds.has(f.id), tasks: accepted });
    }
    return groups;
  }, [state.features]);
  // eslint-disable-next-line react-hooks/rules-of-hooks
  const acceptedTotal = useMemo(
    () => acceptedByFeature.reduce((n, g) => n + g.tasks.length, 0),
    [acceptedByFeature],
  );
  // eslint-disable-next-line react-hooks/rules-of-hooks
  const [acceptedOpen, setAcceptedOpen] = useState(false);
  // openGroups toggles: shipped groups are closed by default (open when id IN set);
  // active groups are open by default (closed when id IN set).
  // eslint-disable-next-line react-hooks/rules-of-hooks
  const [openGroups, setOpenGroups] = useState<Set<string>>(new Set());
  const LIMIT = 20;

  return (
    <div
      className="grid flex-1 gap-3 overflow-x-auto p-4"
      style={{ gridTemplateColumns: `repeat(${ORDER.length}, minmax(140px, 1fr))` }}
    >
      {ORDER.map((c) => {
        const tasks = cols[c] ?? [];
        const dot = statusColorVar(c);
        return (
          <div key={c} className={`min-h-[360px] ${c === "accepted" ? "opacity-[.82]" : ""}`}>
            <Tooltip label="status flows through pact verbs">
              <div className="flex items-center gap-[7px] px-1 pb-2.5 pt-0.5">
                <span
                  className="h-[7px] w-[7px] rounded-full"
                  style={{
                    background: dot,
                    boxShadow: c === "awaiting_review" ? `0 0 6px ${dot}` : undefined,
                  }}
                />
                <span className="text-[11px] font-semibold uppercase tracking-[.5px] text-[var(--color-text-2)]">
                  {c.replace(/_/g, " ")}
                </span>
                <span className="mono ml-auto rounded-full bg-white/[.06] px-[7px] py-px text-[10px] tabular-nums text-[var(--color-text-3)]">
                  {tasks.length}
                </span>
              </div>
            </Tooltip>
            <div className="flex flex-col gap-[9px]">
              {c === "accepted" ? (
                <>
                  <button
                    type="button"
                    data-testid="accepted-summary"
                    className="w-full rounded-md border border-[var(--color-success)]/30 bg-[var(--color-success)]/8 px-2 py-1.5 text-left text-[11px]"
                    onClick={() => setAcceptedOpen((o) => !o)}
                  >
                    {acceptedOpen ? "▾" : "▸"} {acceptedTotal} accepted
                  </button>
                  {acceptedOpen && acceptedByFeature.map((g) => {
                    const open = g.shipped ? openGroups.has(g.feature) : !openGroups.has(g.feature);
                    const shown = g.tasks.slice(0, LIMIT);
                    return (
                      <div key={g.feature} className="mt-1">
                        <button
                          type="button"
                          data-testid={`accepted-group-${g.feature}`}
                          className="w-full text-left text-[10px] text-[var(--color-text-3)]"
                          onClick={() => setOpenGroups((s) => {
                            const n = new Set(s);
                            if (n.has(g.feature)) n.delete(g.feature); else n.add(g.feature);
                            return n;
                          })}
                        >
                          {open ? "▾" : "▸"} {g.feature}{g.shipped ? " · shipped" : ""} ({g.tasks.length})
                        </button>
                        {open && shown.map((t) => {
                          const pulsing = pulses?.has(t.id);
                          return (
                            <div
                              key={t.id}
                              className="mt-1"
                              data-testid={pulsing ? "board-pulse" : undefined}
                              style={
                                pulsing
                                  ? ({ "--pulse-color": statusColor(t.status) } as CSSProperties)
                                  : undefined
                              }
                            >
                              <TaskCard
                                task={t}
                                featureId={g.feature}
                                ownerRoles={rolesOf(t.owner)}
                                reviewerRoles={rolesOf(t.reviewer)}
                                stale={staleTasks?.has(t.id)}
                                selected={selected === t.id}
                                onClick={() => onSelect(t.id)}
                              />
                            </div>
                          );
                        })}
                        {open && g.tasks.length > LIMIT && (
                          <div className="mt-1 text-[10px] text-[var(--color-text-3)]">+{g.tasks.length - LIMIT} more — refine via search</div>
                        )}
                      </div>
                    );
                  })}
                  {acceptedTotal === 0 && (
                    <div className="kb-ghost">{GHOST.accepted}</div>
                  )}
                </>
              ) : tasks.length === 0 ? (
                <div className="kb-ghost">{GHOST[c] ?? "No tasks"}</div>
              ) : (
                tasks.map((bt) => {
                  const pulsing = pulses?.has(bt.task.id);
                  return (
                    <div
                      key={bt.task.id}
                      data-testid={pulsing ? "board-pulse" : undefined}
                      className={pulsing ? "pulse rounded-[11px]" : undefined}
                      style={
                        pulsing
                          ? ({ "--pulse-color": statusColor(bt.task.status) } as CSSProperties)
                          : undefined
                      }
                    >
                      <TaskCard
                        task={bt.task}
                        featureId={bt.feature}
                        ownerRoles={rolesOf(bt.task.owner)}
                        reviewerRoles={rolesOf(bt.task.reviewer)}
                        stale={staleTasks?.has(bt.task.id)}
                        selected={selected === bt.task.id}
                        onClick={() => onSelect(bt.task.id)}
                      />
                    </div>
                  );
                })
              )}
            </div>
          </div>
        );
      })}
    </div>
  );
}
