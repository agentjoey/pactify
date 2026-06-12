import { MiniMap, useReactFlow, useViewport, type Node } from "@xyflow/react";
import { statusColorVar } from "../../lib/lifecycle";

// Hud (T7) — the bottom-right zoom HUD + the bottom-left frosted MiniMap, per
// board2-canvas-v2 `.hud` / `.mmap`. The HUD drives React Flow's own zoom via
// useReactFlow (zoomIn/zoomOut/fitView/zoomTo) and reads the live zoom percent
// from useViewport, so the markup is custom but the behavior is the real
// React Flow controls. Must render inside <ReactFlow> (uses the flow store).

// minimapNodeColor tints each minimap dot: seats → their primary role color,
// tasks → the task's status color, drafts/other → a recessed text-3. CSS vars
// resolve as SVG fill since the minimap lives under :root.
function minimapNodeColor(n: Node): string {
  if (n.type === "seat") {
    const roleVar = (n.data as { roleColor?: string }).roleColor ?? "--role-dev";
    return `var(${roleVar})`;
  }
  if (n.type === "task") {
    const status = (n.data as { task?: { status?: string } }).task?.status ?? "assigned";
    return statusColorVar(status);
  }
  // Office desks tint by their derived status: busy=success, review/waiting=warn,
  // idle (and anything else) recedes to text-3.
  if (n.type === "desk") {
    const status = (n.data as { desk?: { status?: string } }).desk?.status;
    if (status === "busy") return "var(--color-success)";
    if (status === "review_due" || status === "waiting") return "var(--color-warn)";
    return "var(--color-text-3)";
  }
  // drafts + features + anything else recede.
  return "var(--color-text-3)";
}

export function Hud() {
  const { zoomIn, zoomOut, fitView, zoomTo } = useReactFlow();
  const { zoom } = useViewport();
  const pct = Math.round(zoom * 100);

  return (
    <>
      {/* NOTE: React Flow's MiniMap does NOT forward unknown props (a
          data-testid here would be silently dropped) — tests select via the
          .react-flow__minimap class instead. */}
      <MiniMap
        className="canvas-minimap"
        pannable
        zoomable
        nodeColor={minimapNodeColor}
        nodeStrokeWidth={0}
        maskColor="rgba(25,27,38,.6)"
        bgColor="transparent"
      />
      <div className="canvas-hud" data-testid="canvas-hud">
        <button aria-label="zoom out" onClick={() => zoomOut({ duration: 120 })}>
          −
        </button>
        <button
          className="pct mono"
          aria-label="reset zoom to 100%"
          onClick={() => zoomTo(1, { duration: 120 })}
        >
          {pct}%
        </button>
        <button aria-label="zoom in" onClick={() => zoomIn({ duration: 120 })}>
          ＋
        </button>
        <button aria-label="fit view" onClick={() => fitView({ duration: 200 })}>
          ⊡
        </button>
      </div>
    </>
  );
}
