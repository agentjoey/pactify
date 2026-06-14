import type { CSSProperties } from "react";
import type { State } from "../lib/types";
import { boardColumns, type Column } from "../lib/derive";
import { roleColorVar } from "../lib/canvas";
import { statusColorVar } from "../lib/lifecycle";
import { TaskCard } from "./TaskCard";
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
              {tasks.length === 0 ? (
                <div className="kb-ghost">{GHOST[c] ?? "No tasks"}</div>
              ) : (
                tasks.map((bt) => {
                  const pulsing = pulses?.has(bt.task.id);
                  const roleVar = roleColorVar(rolesOf(bt.task.owner));
                  return (
                    <div
                      key={bt.task.id}
                      data-testid={pulsing ? "board-pulse" : undefined}
                      className={pulsing ? "pulse rounded-[11px]" : undefined}
                      style={
                        pulsing
                          ? ({ "--pulse-color": `var(${roleVar})` } as CSSProperties)
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
