import { Handle, Position, type NodeProps } from "@xyflow/react";
import { Ant } from "../ui/ants/Ant";
import { casteForRoles, padGradient } from "../../lib/ants";

// SeatNode is a left-rail card: seat id, its roles, and the seat's caste ant on
// a per-seat pad gradient (replacing the old role-color dot).
export function SeatNode({ id, data }: NodeProps) {
  const d = data as { roles?: string[] };
  const seatId = id.replace(/^seat:/, "");
  const roles = d.roles ?? [];
  const caste = casteForRoles(roles);
  const pad = padGradient(seatId, caste);

  return (
    <div className="rounded-lg bg-[#161b22] border border-[#30363d] text-xs px-2.5 py-2 min-w-[150px]">
      <Handle type="source" position={Position.Right} style={{ background: "rgba(18,22,31,.28)" }} />
      {/* Target handle for comms wait edges (task → waited-on seat). Hidden until
          an edge anchors to it; the draft-drop seat→task flow uses the source. */}
      <Handle type="target" position={Position.Left} style={{ background: "rgba(18,22,31,.28)", opacity: 0 }} />
      <div className="flex items-center gap-2">
        <span
          className="flex items-center justify-center w-[26px] h-[26px] rounded-[8px] shrink-0"
          style={{
            background: `linear-gradient(135deg, ${pad.from}, ${pad.to})`,
            boxShadow: "inset 0 0 0 1px rgba(18,22,31,.18)",
          }}
        >
          <Ant caste={caste} size={22} title={`${caste} — ${roles.join(" · ") || "seat"}`} />
        </span>
        <span className="font-semibold text-[#1a1d27]">{seatId}</span>
      </div>
      <div className="mt-1 text-[10px] text-[#8b949e]">{roles.join(" · ") || "—"}</div>
    </div>
  );
}
