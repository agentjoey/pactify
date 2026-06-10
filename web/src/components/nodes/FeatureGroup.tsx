import { type NodeProps } from "@xyflow/react";

// FeatureGroup is a translucent subflow container: child task/draft nodes carry
// parentId pointing here. The width/height come from Canvas (style on the node).
// Header shows the feature id plus its branch label, top-left.
export function FeatureGroup({ data }: NodeProps) {
  const d = data as { id?: string; branch?: string; status?: string };
  return (
    <div className="w-full h-full rounded-lg border border-[#30363d] bg-[#161b22]/30 pointer-events-none">
      <div className="px-2 py-1 text-[11px] pointer-events-auto">
        <span className="font-semibold text-[#e6edf3]">{d.id}</span>
        {d.branch && (
          <span className="ml-1.5 text-[10px] text-[#8b949e] font-mono">{d.branch}</span>
        )}
      </div>
    </div>
  );
}
