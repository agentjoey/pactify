import { BaseEdge, getBezierPath, type EdgeProps } from "@xyflow/react";
import type { AntEdgeKind } from "../../../lib/canvas";

// prefersReducedMotion reads the OS setting at render time. matchMedia is absent
// in some jsdom configs — treat that as "no preference" (allow motion). Kept
// local (not imported from Canvas) so the edge component is self-contained and
// its reduced-motion branch is independently testable by stubbing matchMedia.
function prefersReducedMotion(): boolean {
  return (
    typeof window !== "undefined" &&
    typeof window.matchMedia === "function" &&
    window.matchMedia("(prefers-reduced-motion: reduce)").matches
  );
}

// AntEdgeData rides on each `type:"ant"` edge. `kind` picks the ant species,
// `ant` is the cap/priority decision baked by Canvas (assignAntFlags) — only
// `ant:true` edges animate, and only when reduced-motion is off. `color` carries
// the stroke (comms edges color by role; dep edges are neutral grey today).
export interface AntEdgeData {
  kind?: AntEdgeKind;
  ant?: boolean;
  color?: string;
  [key: string]: unknown;
}

// MessengerAnt — board4 §① "wait" species (review/rework). Stroke-colored body,
// no cargo. Scaled ~0.9 vs the board demo (board used 1.15).
function MessengerAnt({ color }: { color: string }) {
  return (
    <g transform="scale(0.9)">
      <ellipse cx="0" cy="0" rx="4.5" ry="3" fill={color} />
      <circle cx="6.5" cy="0" r="2.6" fill={color} />
      <ellipse cx="-6.5" cy="0" rx="4" ry="2.6" fill={color} />
      <path
        d="M-2 -2.5 L-4 -5.5 M1.5 -2.5 L3 -5.5 M-2 2.5 L-4 5.5 M1.5 2.5 L3 5.5 M8 -1.5 L10.5 -3.5 M8 1.5 L10.5 3.5"
        stroke={color}
        strokeWidth="1"
      />
    </g>
  );
}

// CarrierAnt — board4 §① "dep"/"blocked" species. Carries a tiny cargo cube on
// its back. Scaled ~0.9 vs the board demo (board used 1.05).
function CarrierAnt({ color }: { color: string }) {
  return (
    <g transform="scale(0.9)">
      <rect
        x="3"
        y="-7"
        width="7"
        height="6"
        rx="1.5"
        fill={`color-mix(in srgb, ${color} 30%, transparent)`}
        stroke={color}
        strokeWidth="0.9"
      />
      <ellipse cx="0" cy="0" rx="4.5" ry="3" fill={color} />
      <circle cx="6.5" cy="0" r="2.6" fill={color} />
      <ellipse cx="-6.5" cy="0" rx="4" ry="2.6" fill={color} />
      <path
        d="M-2 -2.5 L-4 -5.5 M1.5 -2.5 L3 -5.5 M-2 2.5 L-4 5.5 M1.5 2.5 L3 5.5"
        stroke={color}
        strokeWidth="1"
      />
    </g>
  );
}

// AntEdge is the custom React Flow edge (`edgeTypes.ant`). It draws the SAME base
// path React Flow's default bezier edge would (dashed + colored via `style`
// passthrough, exactly as the dep/comms edges look today) and — when this edge
// won an ant slot (`data.ant`) and motion is allowed — embeds an
// `<animateMotion rotate="auto"><mpath>` ant group crawling along that very path.
//
// The path needs a unique id (`${id}-path`) so the mpath can reference it; the
// ant <g> wraps a <use> of that path so React Flow's own path element (rendered
// by BaseEdge) and the motion path stay the same geometry without duplicating
// the `d` string. Dur scales mildly by kind (messenger faster than carrier) to
// echo the board demo's 7s/9s feel.
export function AntEdge(props: EdgeProps) {
  const {
    id,
    sourceX,
    sourceY,
    sourcePosition,
    targetX,
    targetY,
    targetPosition,
    style,
    markerEnd,
    data,
  } = props;

  const [edgePath] = getBezierPath({
    sourceX,
    sourceY,
    sourcePosition,
    targetX,
    targetY,
    targetPosition,
  });

  const d = (data ?? {}) as AntEdgeData;
  const kind: AntEdgeKind = d.kind ?? "dep";
  const color =
    d.color ??
    (typeof style?.stroke === "string" ? style.stroke : "var(--color-text-3)");
  const showAnt = !!d.ant && !prefersReducedMotion();
  const pathId = `${id}-path`;
  const dur = kind === "wait" ? "7s" : "9s";

  return (
    <>
      <BaseEdge id={pathId} path={edgePath} style={style} markerEnd={markerEnd} />
      {showAnt && (
        <g data-testid={`ant-${id}`} style={{ overflow: "visible" }}>
          {kind === "wait" ? (
            <MessengerAnt color={color} />
          ) : (
            <CarrierAnt color={color} />
          )}
          <animateMotion dur={dur} repeatCount="indefinite" rotate="auto">
            <mpath href={`#${pathId}`} />
          </animateMotion>
        </g>
      )}
    </>
  );
}
