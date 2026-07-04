import type { CSSProperties, ReactNode } from "react";
import type { Task } from "../lib/types";
import { lifecycleStage, statusColorVar } from "../lib/lifecycle";
import { casteForRoles } from "../lib/ants";
import { Ant } from "./ui/ants/Ant";
import { MetricStrip, type MetricItem } from "./ui/MetricStrip";

// Empty default so callers that omit owner/reviewer roles fall back to the
// worker caste (casteForRoles([]) === "builder").
const NO_ROLES: string[] = [];

// TaskCard — the shared "genome" rendered by both the kanban board and the
// canvas task node (the canvas node wraps this in React Flow Handles + comms /
// pulse plumbing). Lifted from board3-kanban.html `.task` markup, translated to
// T1 tokens. See plan T4 + spec §3/§4.

// Status → medallion glyph (board3): ◇ assigned · ⚡ in_progress · ◉ awaiting ·
// ↺ changes · ✓ accepted. Unknown statuses (drafts) fall back to the diamond.
const MEDALLION: Record<string, string> = {
  assigned: "◇",
  in_progress: "⚡",
  awaiting_review: "◉",
  changes_requested: "↺",
  accepted: "✓",
};

// Statuses whose card shows the owner→reviewer hand-off (review flow).
const REVIEW_FLOW = new Set(["awaiting_review", "changes_requested"]);

export interface TaskCardProps {
  task: Task;
  // Feature id shown in the top-right chip (omitted on the canvas, where the
  // surrounding feature frame already names it).
  featureId?: string;
  // Display title; falls back to the task id when absent (the protocol task has
  // no separate title field today).
  title?: string;
  // Pre-resolved owner/reviewer roles, used to pick the ant caste. Resolved by
  // the caller (Board / TaskNode) from the seat roster — the card no longer takes
  // a rolesOf callback, so there is one less per-consumer adapter. Default empty
  // → worker caste.
  ownerRoles?: string[];
  reviewerRoles?: string[];
  stale?: boolean;
  draft?: boolean;
  selected?: boolean;
  // Compact RUN/TOK/×iter stat strip (dark handoff), computed by the caller via
  // taskMetrics(task, events). Omitted on cards with no stat context.
  metrics?: MetricItem[];
  // Inline reviewer actions (Accept / Changes) that REPLACE the owner→reviewer
  // bottom row on review-column cards. The caller owns the pact verb calls.
  reviewActions?: ReactNode;
  onClick?: () => void;
  onMenu?: (e: React.MouseEvent) => void;
  // Extra content injected at the card top (e.g. the canvas draft "dispatch →"
  // button). Kept generic so TaskNode can augment without forking the genome.
  children?: ReactNode;
}

export function TaskCard({
  task,
  featureId,
  title,
  ownerRoles = NO_ROLES,
  reviewerRoles = NO_ROLES,
  stale,
  draft,
  selected,
  metrics,
  reviewActions,
  onClick,
  onMenu,
  children,
}: TaskCardProps) {
  const status = task.status;
  const st = statusColorVar(status);
  const stage = lifecycleStage(status);
  const med = MEDALLION[status] ?? "◇";
  const showReviewer =
    REVIEW_FLOW.has(status) && task.reviewer && task.reviewer !== task.owner;

  const ownerCaste = casteForRoles(ownerRoles);
  const reviewerCaste = casteForRoles(reviewerRoles);

  // Quorum badge: multi-reviewer tasks carry a quorum (>0) + reviewers list.
  // Single-reviewer tasks leave both unset → no badge (card unchanged).
  const quorum = task.quorum ?? 0;
  const showQuorum = quorum > 0 && !!task.reviewers && task.reviewers.length > 0;
  const acceptsLen = task.accepts?.length ?? 0;

  return (
    <div
      data-testid="task-card"
      onClick={onClick}
      onContextMenu={onMenu}
      style={{ ["--st" as string]: st } as CSSProperties}
      className={[
        "task-card",
        draft ? "dashed" : "",
        status === "in_progress" ? "glow" : "",
        selected ? "selected" : "",
        onClick ? "cursor-pointer hover-lift" : "",
      ]
        .filter(Boolean)
        .join(" ")}
    >
      {children}
      <div className="flex items-center gap-[7px]">
        <span data-testid="task-card-med" className="task-card-med">
          {med}
        </span>
        <div className="min-w-0">
          <div className="mono text-[10.5px] leading-tight text-[var(--color-text-3)] truncate">
            {task.id}
          </div>
          <div className="text-[12.5px] font-semibold leading-tight text-[var(--color-text-1)] truncate">
            {title ?? task.id}
          </div>
        </div>
        {stale && (
          <span
            data-testid="task-card-stale"
            title="in_progress > 30min"
            className="ml-1 inline-block h-1.5 w-1.5 shrink-0 rounded-full bg-[var(--color-warn)] shadow-[0_0_5px_var(--color-warn)]"
          />
        )}
        {featureId && (
          <span className="mono ml-auto shrink-0 rounded-full bg-white/5 px-[7px] py-px text-[9px] text-[var(--color-text-3)]">
            {featureId}
          </span>
        )}
        {showQuorum && (
          <span
            data-testid="task-card-quorum"
            title={`quorum ${acceptsLen}/${quorum}`}
            className={`mono shrink-0 rounded-full bg-white/5 px-[7px] py-px text-[9px] text-[var(--color-text-2)] ${featureId ? "" : "ml-auto"}`}
          >
            {acceptsLen}/{quorum} ✓
          </span>
        )}
      </div>

      {metrics && metrics.length > 0 && (
        <div className="mt-[8px] ml-[28px]">
          <MetricStrip items={metrics} />
        </div>
      )}

      {reviewActions ? (
        <div className="task-card-bot mt-[9px] ml-[28px]">{reviewActions}</div>
      ) : (
      <div className="task-card-bot mt-[9px] ml-[28px] flex items-center gap-[4px]">
        {task.owner && (
          <span className="task-card-ant">
            <Ant caste={ownerCaste} size={14} title={task.owner} />
          </span>
        )}
        {task.owner && (
          <span className="mono max-w-[68px] truncate text-[9px] text-[var(--color-text-2)]">{task.owner}</span>
        )}
        {showReviewer && (
          <>
            <span className="text-[9px] text-[var(--color-text-3)]">→</span>
            <span className="task-card-ant">
              <Ant caste={reviewerCaste} size={14} title={task.reviewer} />
            </span>
            <span className="mono max-w-[60px] truncate text-[9px] text-[var(--color-text-2)]">{task.reviewer}</span>
          </>
        )}
        <span className="life ml-auto flex gap-[2.5px]">
          {[0, 1, 2, 3].map((i) => {
            // accepted (stage 3) fills every bar; otherwise bars before the
            // current stage are done and the stage bar glows (board3).
            const cls = stage >= 3 || i < stage ? "done" : i === stage ? "cur" : "";
            return <i key={i} className={cls} />;
          })}
        </span>
      </div>
      )}
    </div>
  );
}
