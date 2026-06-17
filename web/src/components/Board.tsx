import { useState, type CSSProperties } from "react";
import type { State } from "../lib/types";
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
  // accepted column: show the most recent N cards, fold the rest behind a
  // "+K more" expander (state hooks live above the loading early-return so the
  // hook order is stable across the loading→loaded transition).
  const RECENT = 10;
  const [acceptedExpanded, setAcceptedExpanded] = useState(false);

  if (loading) return <BoardSkeleton />;
  const cols = boardColumns(state);
  // rolesOf indexes seat → roles so a pulsing card glows in its owner's color
  // and the ant chips pick the right caste.
  const rolesMap = new Map(state.agents.map((a) => [a.id, a.roles]));
  const rolesOf = (seat: string): string[] => rolesMap.get(seat) ?? [];

  // Most-recent-first: the board derivation appends in log order, so reversing
  // surfaces the latest-accepted tasks at the top of the column.
  const acceptedAll = [...(cols.accepted ?? [])].reverse();
  const acceptedShown = acceptedExpanded ? acceptedAll : acceptedAll.slice(0, RECENT);

  return (
    <div
      // pl gutter clears the floating RosterDock/PlanDock (left strip) so the
      // first kanban column is never hidden behind it.
      className="grid flex-1 gap-3 overflow-x-auto py-4 pr-4 pl-[216px]"
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
                acceptedAll.length === 0 ? (
                  <div className="kb-ghost">{GHOST.accepted}</div>
                ) : (
                  <>
                    {acceptedShown.map((bt) => {
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
                    })}
                    {acceptedAll.length > RECENT && (
                      <button
                        type="button"
                        data-testid={acceptedExpanded ? "accepted-less" : "accepted-more"}
                        className="mt-1 w-full rounded-md border border-[var(--color-success)]/30 bg-[var(--color-success)]/8 px-2 py-1.5 text-left text-[11px] text-[var(--color-text-2)]"
                        onClick={() => setAcceptedExpanded((o) => !o)}
                      >
                        {acceptedExpanded ? `▾ show recent ${RECENT}` : `▸ ${acceptedAll.length - RECENT} more accepted`}
                      </button>
                    )}
                  </>
                )
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
