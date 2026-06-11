import { Handle, Position, type NodeProps } from "@xyflow/react";

// Status chip colors mirror the board's column semantics. Kept local so the
// canvas reads at a glance without importing board styling.
const STATUS_COLOR: Record<string, string> = {
  assigned: "#8b949e",
  in_progress: "#2f81f7",
  awaiting_review: "#d29922",
  accepted: "#3fb950",
  changes_requested: "#f85149",
};

// TaskNode renders both committed tasks and in-flight drafts (data.draft).
// The 3px left border is tinted by the owner's role color var.
export function TaskNode({ id, data }: NodeProps) {
  const d = data as {
    status?: string;
    owner?: string;
    reviewer?: string;
    deps?: string[];
    roleColor?: string;
    draft?: boolean;
    stale?: boolean;
    onDispatch?: () => void;
  };
  const taskId = id.replace(/^(task|draft):/, "");
  const roleVar = d.roleColor ?? "--role-dev";
  const statusColor = d.status ? STATUS_COLOR[d.status] ?? "#8b949e" : "#8b949e";
  const awaiting = d.status === "awaiting_review";

  return (
    <div
      className={`rounded bg-[#161b22] text-xs px-2.5 py-2 min-w-[160px] ${awaiting ? "pact-awaiting" : ""}`}
      style={{
        borderTop: d.draft ? "1px dashed #6e7681" : "1px solid #30363d",
        borderRight: d.draft ? "1px dashed #6e7681" : "1px solid #30363d",
        borderBottom: d.draft ? "1px dashed #6e7681" : "1px solid #30363d",
        borderLeft: `3px solid var(${roleVar})`,
      }}
    >
      <Handle type="target" position={Position.Top} style={{ background: "#484f58" }} />
      <div className="flex items-center gap-1.5">
        <span className="font-semibold text-[#e6edf3]">{taskId}</span>
        {d.stale && (
          <span
            data-testid="stale-dot"
            title="in_progress > 30min"
            className="inline-block h-2 w-2 rounded-full bg-[#d29922]"
          />
        )}
        {d.draft && (
          <span className="text-[9px] uppercase tracking-wide rounded px-1 bg-[#21262d] text-[#8b949e] border border-dashed border-[#6e7681]">
            draft
          </span>
        )}
      </div>
      {d.status && (
        <div className="mt-1">
          <span
            className="text-[9px] uppercase tracking-wide rounded px-1 py-0.5"
            style={{ background: "#21262d", color: statusColor }}
          >
            {d.status.replace(/_/g, " ")}
          </span>
        </div>
      )}
      {d.draft && d.onDispatch && (
        <button
          data-testid="dispatch-btn"
          className="mt-1 rounded border border-[#30363d] bg-[#21262d] px-1.5 py-0.5 text-[10px] text-[#8ab4ff] hover:border-[#8ab4ff]"
          onClick={(e) => {
            e.stopPropagation(); // don't trigger the node-click editor
            d.onDispatch?.();
          }}
        >
          dispatch →
        </button>
      )}
      <div className="mt-1 flex flex-wrap gap-1 text-[10px] text-[#8b949e]">
        {d.owner && (
          <span className="rounded bg-[#21262d] px-1">owner: {d.owner}</span>
        )}
        {d.reviewer && (
          <span className="rounded bg-[#21262d] px-1">rev: {d.reviewer}</span>
        )}
      </div>
      <Handle type="source" position={Position.Bottom} style={{ background: "#484f58" }} />
    </div>
  );
}
