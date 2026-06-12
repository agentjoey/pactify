import { getBezierPath, type ConnectionLineComponentProps } from "@xyflow/react";

// ConnectionLine is the custom drag-time connection preview (Canvas P0 §2,
// Dify-style). React Flow's default connection line is a plain bezier with no
// endpoint affordance; ours draws a role-colored bezier (gentle curvature) plus
// a small rounded-rect marker at the drag target — the same 14×4 mark shape the
// task ports show, so the line visually "docks" into a port. Geometry is whatever
// React Flow reports each frame (fromX/Y → toX/Y); nothing is persisted.
const MARK_W = 14;
const MARK_H = 4;

export function ConnectionLine({
  fromX,
  fromY,
  toX,
  toY,
  fromPosition,
  toPosition,
}: ConnectionLineComponentProps) {
  const [path] = getBezierPath({
    sourceX: fromX,
    sourceY: fromY,
    sourcePosition: fromPosition,
    targetX: toX,
    targetY: toY,
    targetPosition: toPosition,
    curvature: 0.16,
  });

  return (
    <g className="canvas-connectionline">
      <path
        d={path}
        fill="none"
        stroke="var(--color-role-design)"
        strokeWidth={1.6}
      />
      <rect
        data-testid="connectionline-endpoint"
        x={toX - MARK_W / 2}
        y={toY - MARK_H / 2}
        width={MARK_W}
        height={MARK_H}
        rx={2}
        fill="var(--color-role-design)"
      />
    </g>
  );
}
