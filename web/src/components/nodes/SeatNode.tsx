import { Handle, Position, type NodeProps } from "@xyflow/react";

// SeatNode is a left-rail card: seat id, its roles, and a role-color dot.
export function SeatNode({ id, data }: NodeProps) {
  const d = data as { roles?: string[]; roleColor?: string };
  const seatId = id.replace(/^seat:/, "");
  const roleVar = d.roleColor ?? "--role-dev";
  const roles = d.roles ?? [];

  return (
    <div className="rounded-lg bg-[#161b22] border border-[#30363d] text-xs px-2.5 py-2 min-w-[150px]">
      <Handle type="source" position={Position.Right} style={{ background: "#484f58" }} />
      {/* Target handle for comms wait edges (task → waited-on seat). Hidden until
          an edge anchors to it; the draft-drop seat→task flow uses the source. */}
      <Handle type="target" position={Position.Left} style={{ background: "#484f58", opacity: 0 }} />
      <div className="flex items-center gap-1.5">
        <span
          className="inline-block w-2 h-2 rounded-full"
          style={{ background: `var(${roleVar})` }}
        />
        <span className="font-semibold text-[#e6edf3]">{seatId}</span>
      </div>
      <div className="mt-1 text-[10px] text-[#8b949e]">{roles.join(" · ") || "—"}</div>
    </div>
  );
}
