import { Handle, Position, useConnection, type NodeProps } from "@xyflow/react";
import type { Task } from "../../lib/types";
import { TaskCard } from "../TaskCard";
import { AntActivity } from "../canvas/AntActivity";

// TaskNode renders both committed tasks and in-flight drafts (data.draft) by
// wrapping the shared TaskCard genome in React Flow Handles. The pulse / comms
// (idle/blocked/notjoined) classes are applied at the React Flow node-wrapper
// level by Canvas's className merge — TaskNode itself just draws the card and
// keeps the draft "dispatch →" affordance + the not-joined warning badge.
export function TaskNode({ id, data }: NodeProps) {
  const d = data as {
    // Committed tasks carry the protocol Task object + resolved roles (baked by
    // deriveFlow). Drafts carry only draft/specMd/deps — no task — so the card
    // is materialized from the draft id below.
    task?: Task;
    feature?: string;
    ownerRoles?: string[];
    reviewerRoles?: string[];
    draft?: boolean;
    stale?: boolean;
    commsNotJoined?: boolean;
    onDispatch?: () => void;
  };

  // Drafts carry no protocol Task; present them as an assigned-shaped, dashed
  // card (status drives the medallion/lifecycle genome) keyed by the draft id.
  const task: Task =
    d.task ?? {
      id: id.replace(/^(task|draft):/, ""),
      owner: "",
      reviewer: "",
      status: "assigned",
      spec: "",
      evidence: "",
    };

  // useConnection lets a node light up as a valid drop target WHILE a connection
  // is being dragged. We highlight any node that is not the connection's own
  // source (self-loops are invalid; cross-feature/cycle invalidity is enforced
  // by isValidConnection on the flow, which paints the not-allowed cursor — the
  // highlight here is the affordance that says "you can aim here").
  const connection = useConnection();
  const connecting = connection.inProgress;
  const fromId = connection.fromNode?.id;
  const isValidTarget = connecting && fromId !== undefined && fromId !== id;

  return (
    <div
      className={`task-node relative min-w-[176px]${isValidTarget ? " connect-target" : ""}`}
      data-testid="task-node"
    >
      {/* always-on, state-driven ant motion (committed tasks only — drafts have
          no live status to animate) */}
      {!d.draft && <AntActivity status={task.status} />}
      <Handle
        type="target"
        position={Position.Top}
        className="task-port task-port-in"
        isConnectableStart={false}
      >
        <span className="port-mark" />
      </Handle>
      <TaskCard
        task={task}
        ownerRoles={d.ownerRoles}
        reviewerRoles={d.reviewerRoles}
        stale={d.stale}
        draft={d.draft}
      >
        {(d.commsNotJoined || (d.draft && d.onDispatch)) && (
          <div className="mb-1.5 flex items-center gap-1.5">
            {d.commsNotJoined && (
              <span
                data-testid="notjoined-badge"
                title="owner or reviewer never joined"
                className="rounded border border-[var(--color-danger)] bg-[var(--color-bg-raised)] px-1 text-[9px] uppercase tracking-wide text-[var(--color-danger)]"
              >
                ⚠ not joined
              </span>
            )}
            {d.draft && d.onDispatch && (
              <button
                data-testid="dispatch-btn"
                className="rounded border border-[var(--color-border-strong)] bg-[var(--color-bg-raised)] px-1.5 py-0.5 text-[10px] text-[var(--color-role-design)] hover:border-[var(--color-role-design)]"
                onClick={(e) => {
                  e.stopPropagation(); // don't trigger the node-click editor
                  d.onDispatch?.();
                }}
              >
                dispatch →
              </button>
            )}
          </div>
        )}
      </TaskCard>
      <Handle
        type="source"
        position={Position.Bottom}
        className="task-port task-port-out"
      >
        <span className="port-mark" />
      </Handle>
    </div>
  );
}
