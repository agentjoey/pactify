import { useEffect, useMemo, useRef, useState } from "react";
import type { FlowModel, FlowStint, FlowArrow } from "../../lib/flowderive";
import type { State } from "../../lib/types";

const LANE_H = 64;
const HEADER_H = 32;
const MIN_W = 900;
const STINT_H = 24;
const ARROW_OFFSET = 12;

function roleColor(roles: string[] | undefined): string {
  if (roles?.includes("orchestrator")) return "var(--color-role-product)";
  if (roles?.includes("reviewer")) return "var(--color-role-design)";
  return "var(--color-role-dev)";
}

function roleLabel(roles: string[] | undefined): string {
  if (roles?.includes("orchestrator")) return "orchestrator";
  if (roles?.includes("reviewer")) return "reviewer";
  return "worker";
}

function stintFill(kind: FlowStint["kind"]): string {
  switch (kind) {
    case "work":
      return "var(--color-role-dev)";
    case "rework":
      return "var(--color-warn)";
    case "review":
      return "var(--color-role-design)";
  }
}

function arrowColor(verb: FlowArrow["verb"]): string {
  switch (verb) {
    case "assign":
      return "var(--color-text-3)";
    case "checkpoint":
      return "var(--color-role-design)";
    case "changes":
      return "var(--color-danger)";
    case "accept":
      return "var(--color-success)";
  }
}

function arrowTestId(verb: FlowArrow["verb"]): string {
  return `flow-arrow-${verb}`;
}

function fmtTick(t: number): string {
  const d = new Date(t);
  return d.toLocaleTimeString(undefined, { hour: "2-digit", minute: "2-digit", hour12: false });
}

interface FlowLanesProps {
  model: FlowModel;
  agents: State["agents"];
  selected: string;
  onSelect: (taskId: string) => void;
}

export function FlowLanes({ model, agents, selected, onSelect }: FlowLanesProps) {
  const wrapRef = useRef<HTMLDivElement>(null);
  const [width, setWidth] = useState(MIN_W);

  useEffect(() => {
    const el = wrapRef.current;
    if (!el) return;
    const ro = new ResizeObserver((entries) => {
      const cw = entries[0]?.contentRect.width ?? 0;
      setWidth(Math.max(cw, MIN_W));
    });
    ro.observe(el);
    setWidth(Math.max(el.clientWidth, MIN_W));
    return () => ro.disconnect();
  }, []);

  const laneOrder = useMemo(() => {
    const firstT = new Map(model.lanes.map((l) => [l.id, l.firstT]));
    const all = agents.length ? agents : model.lanes.map((l) => ({ id: l.id, roles: [] as string[] }));
    return [...all].sort((a, b) => {
      const aOrc = a.roles.includes("orchestrator") ? 0 : 1;
      const bOrc = b.roles.includes("orchestrator") ? 0 : 1;
      if (aOrc !== bOrc) return aOrc - bOrc;
      const at = firstT.get(a.id) ?? Infinity;
      const bt = firstT.get(b.id) ?? Infinity;
      if (at !== bt) return at - bt;
      return a.id.localeCompare(b.id);
    });
  }, [model.lanes, agents]);

  const laneIndex = useMemo(() => {
    const m = new Map<string, number>();
    laneOrder.forEach((a, i) => m.set(a.id, i));
    return m;
  }, [laneOrder]);

  const ticks = useMemo(() => {
    if (model.tMax <= model.tMin) return [];
    const n = 6;
    const arr: number[] = [];
    for (let i = 0; i < n; i++) {
      arr.push(model.tMin + (i * (model.tMax - model.tMin)) / (n - 1));
    }
    return arr;
  }, [model.tMin, model.tMax]);

  const liveByAgent = useMemo(() => {
    const m = new Map<string, { kind: FlowStint["kind"]; task: string }>();
    for (const s of model.stints) {
      if (s.t1 === null && !m.has(s.agent)) {
        m.set(s.agent, { kind: s.kind, task: s.task });
      }
    }
    return m;
  }, [model.stints]);

  function laneY(i: number): number {
    return HEADER_H + i * LANE_H + LANE_H / 2;
  }

  function px(t: number): number {
    return model.x(t) * width;
  }

  if (model.lanes.length === 0 && model.stints.length === 0 && model.marks.length === 0) {
    return (
      <div className="grid flex-1 place-items-center text-sm text-[var(--color-text-2)]">
        No activity yet
      </div>
    );
  }

  const svgHeight = HEADER_H + laneOrder.length * LANE_H;

  return (
    <div ref={wrapRef} className="flex flex-1 overflow-hidden">
      {/* Left seat rail */}
      <div className="shrink-0 border-r border-[var(--color-border-subtle)] bg-[var(--color-bg-inset)]" style={{ width: 176 }}>
        <div className="flex h-[32px] items-center px-3 text-[10px] font-semibold uppercase tracking-[.5px] text-[var(--color-text-3)]">
          Seat
        </div>
        {laneOrder.map((a) => {
          const live = liveByAgent.get(a.id);
          return (
            <div
              key={a.id}
              className="flex items-center gap-2.5 border-b border-[var(--color-border-subtle)] px-3"
              style={{ height: LANE_H }}
            >
              <span
                className="grid h-[28px] w-[28px] shrink-0 place-items-center rounded-[7px] text-[10px] font-semibold"
                style={{ background: roleColor(a.roles), color: "var(--color-bg-page)" }}
              >
                {a.id.slice(0, 2)}
              </span>
              <div className="flex min-w-0 flex-col">
                <span className="truncate text-[11.5px] font-medium text-[var(--color-text-1)]">{a.id}</span>
                <span className="text-[10px] text-[var(--color-text-3)]">{roleLabel(a.roles)}</span>
              </div>
              {live ? (
                <span
                  className="ml-auto rounded-full px-1.5 py-px text-[9.5px] font-medium"
                  style={{
                    color: stintFill(live.kind),
                    background: `color-mix(in srgb, ${stintFill(live.kind)} 14%, transparent)`,
                    border: `1px solid color-mix(in srgb, ${stintFill(live.kind)} 30%, transparent)`,
                  }}
                >
                  {live.kind === "work" ? "working" : live.kind}
                </span>
              ) : (
                <span
                  className="ml-auto rounded-full px-1.5 py-px text-[9.5px] font-medium text-[var(--color-text-3)]"
                  style={{ background: "rgba(255,255,255,0.06)" }}
                >
                  idle
                </span>
              )}
            </div>
          );
        })}
      </div>

      {/* Right canvas */}
      <div className="flex-1 overflow-x-auto bg-[var(--color-bg-page)]">
        <svg width={width} height={svgHeight} role="img" aria-label="Flow lanes">
          <defs>
            <marker id="arrowhead" markerWidth="8" markerHeight="6" refX="7" refY="3" orient="auto">
              <path d="M0,0 L8,3 L0,6 Z" fill="var(--color-text-2)" />
            </marker>
            <marker id="arrowhead-checkpoint" markerWidth="8" markerHeight="6" refX="7" refY="3" orient="auto">
              <path d="M0,0 L8,3 L0,6 Z" fill="var(--color-role-design)" />
            </marker>
            <marker id="arrowhead-changes" markerWidth="8" markerHeight="6" refX="7" refY="3" orient="auto">
              <path d="M0,0 L8,3 L0,6 Z" fill="var(--color-danger)" />
            </marker>
            <marker id="arrowhead-accept" markerWidth="8" markerHeight="6" refX="7" refY="3" orient="auto">
              <path d="M0,0 L8,3 L0,6 Z" fill="var(--color-success)" />
            </marker>
            <pattern id="review-stripes" patternUnits="userSpaceOnUse" width="8" height="8" patternTransform="rotate(45)">
              <rect width="4" height="8" fill="var(--color-role-design)" opacity="0.35" />
            </pattern>
            <linearGradient id="live-fade" x1="0" y1="0" x2="1" y2="0">
              <stop offset="0%" stopColor="currentColor" stopOpacity="1" />
              <stop offset="100%" stopColor="currentColor" stopOpacity="0.15" />
            </linearGradient>
          </defs>

          {/* Gap bands */}
          {model.gaps.map((g, i) => {
            const x0 = px(g.t0);
            const x1 = px(g.t1);
            return (
              <g key={`gap-${i}`}>
                <rect x={x0} y={HEADER_H} width={x1 - x0} height={svgHeight - HEADER_H} fill="rgba(255,255,255,0.03)" />
                <text x={(x0 + x1) / 2} y={HEADER_H - 6} textAnchor="middle" fill="var(--color-text-3)" fontSize={10}>
                  ⌇
                </text>
              </g>
            );
          })}

          {/* Ticks */}
          {ticks.map((t, i) => {
            const x = px(t);
            return (
              <g key={`tick-${i}`}>
                <line x1={x} y1={HEADER_H} x2={x} y2={svgHeight} stroke="var(--color-border-subtle)" strokeDasharray="2 4" opacity={0.5} />
                <text x={x} y={18} textAnchor="middle" fill="var(--color-text-3)" fontSize={10}>
                  {fmtTick(t)}
                </text>
              </g>
            );
          })}

          {/* Lane separators */}
          {laneOrder.map((_, i) => (
            <line
              key={`sep-${i}`}
              x1={0}
              y1={HEADER_H + (i + 1) * LANE_H}
              x2={width}
              y2={HEADER_H + (i + 1) * LANE_H}
              stroke="var(--color-border-subtle)"
              opacity={0.6}
            />
          ))}

          {/* Stints */}
          {model.stints.map((s, i) => {
            const y = laneY(laneIndex.get(s.agent) ?? 0);
            const x0 = px(s.t0);
            const x1 = s.t1 === null ? width : px(s.t1);
            const fill = s.kind === "review" ? "url(#review-stripes)" : stintFill(s.kind);
            const isSelected = selected === s.task;
            return (
              <g
                key={`stint-${i}`}
                data-testid="flow-stint"
                data-task={s.task}
                data-kind={s.kind}
                style={{ cursor: "pointer" }}
                onClick={() => onSelect(s.task)}
              >
                <rect
                  x={x0}
                  y={y - STINT_H / 2}
                  width={Math.max(4, x1 - x0)}
                  height={STINT_H}
                  rx={4}
                  fill={fill}
                  stroke={isSelected ? "var(--color-text-1)" : "none"}
                  strokeWidth={isSelected ? 2 : 0}
                />
                {s.t1 === null && (
                  <rect
                    x={x0}
                    y={y - STINT_H / 2}
                    width={Math.max(4, x1 - x0)}
                    height={STINT_H}
                    rx={4}
                    fill="url(#live-fade)"
                    style={{ color: stintFill(s.kind), pointerEvents: "none" }}
                  />
                )}
              </g>
            );
          })}

          {/* Arrows */}
          {model.arrows.map((a, i) => {
            const fromIdx = laneIndex.get(a.from) ?? 0;
            const toIdx = laneIndex.get(a.to) ?? 0;
            const y0 = laneY(fromIdx) + (a.from === a.to ? 0 : a.from === laneOrder[fromIdx]?.id ? ARROW_OFFSET : -ARROW_OFFSET);
            const y1 = laneY(toIdx) + (a.from === a.to ? 0 : a.to === laneOrder[toIdx]?.id ? -ARROW_OFFSET : ARROW_OFFSET);
            const x = px(a.t);
            const marker =
              a.verb === "checkpoint"
                ? "url(#arrowhead-checkpoint)"
                : a.verb === "changes"
                  ? "url(#arrowhead-changes)"
                  : a.verb === "accept"
                    ? "url(#arrowhead-accept)"
                    : "url(#arrowhead)";
            const d =
              a.verb === "changes"
                ? `M${x},${y0} Q${x + 24},${(y0 + y1) / 2} ${x},${y1}`
                : `M${x},${y0} L${x},${y1}`;
            return (
              <path
                key={`arrow-${i}`}
                data-testid={arrowTestId(a.verb)}
                data-task={a.task}
                d={d}
                fill="none"
                stroke={arrowColor(a.verb)}
                strokeWidth={1.5}
                markerEnd={marker}
                style={{ cursor: "pointer" }}
                onClick={() => onSelect(a.task)}
              />
            );
          })}

          {/* Merge / join marks */}
          {model.marks.map((m, i) => {
            const y = laneY(laneIndex.get(m.agent) ?? 0);
            const x = px(m.t);
            if (m.verb === "merge") {
              return (
                <g key={`mark-${i}`} transform={`translate(${x},${y})`}>
                  <polygon points="0,-6 6,0 0,6 -6,0" fill="var(--color-role-product)" />
                </g>
              );
            }
            return (
              <g key={`mark-${i}`} transform={`translate(${x},${y})`}>
                <circle r={4} fill="var(--color-text-2)" />
              </g>
            );
          })}
        </svg>
      </div>
    </div>
  );
}
