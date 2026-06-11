import { type NodeProps } from "@xyflow/react";

// FeatureGroup v2 (T7) — translucent subflow container per board2-canvas-v2
// `.frame`/`.fhead`. Child task/draft nodes carry parentId pointing here; the
// width/height come from Canvas (style on the node). The header is a gradient
// strip (success-tinted left edge) with the feature id, branch (mono, text-3)
// and a right-aligned `n/total accepted` progress readout + bar. When every
// task is accepted (total>0 && accepted===total) the header tints green.
export function FeatureGroup({ data }: NodeProps) {
  const d = data as {
    id?: string;
    branch?: string;
    status?: string;
    accepted?: number;
    total?: number;
    // Clicking the header toggles feature focus mode (injected by Canvas via
    // node data, mirroring the draft onDispatch injection).
    onFocus?: () => void;
  };
  const accepted = d.accepted ?? 0;
  const total = d.total ?? 0;
  const allDone = total > 0 && accepted === total;
  const pct = total > 0 ? Math.round((accepted / total) * 100) : 0;

  return (
    <div className="feature-frame pointer-events-none h-full w-full">
      <div
        className={`fhead pointer-events-auto${allDone ? " done" : ""}${d.onFocus ? " cursor-pointer" : ""}`}
        data-testid="feature-frame-head"
        onClick={d.onFocus ? (e) => { e.stopPropagation(); d.onFocus!(); } : undefined}
        title={d.onFocus ? "聚焦此 feature(Esc 退出)" : undefined}
      >
        <span className="fname">{d.id}</span>
        {d.branch && <span className="fbranch mono">{d.branch}</span>}
        {total > 0 && (
          <span className="fprog" data-testid="feature-progress">
            <span className="mono">
              {accepted}/{total} accepted
            </span>
            <span className="fbar" aria-hidden>
              <i style={{ width: `${pct}%` }} />
            </span>
          </span>
        )}
      </div>
    </div>
  );
}
