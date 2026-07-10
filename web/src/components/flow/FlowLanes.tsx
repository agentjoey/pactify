import { useEffect, useMemo, useRef, useState } from "react";
import {
  blockedTasks,
  liveStates,
  tAt,
  type FlowModel,
  type FlowStint,
  type FlowArrow,
} from "../../lib/flowderive";
import type { State } from "../../lib/types";
import { useDataSource } from "../../lib/datasource";
import type { AgentStat, ProjectStats } from "../../lib/api";
import { fmtDuration as fmtDurationSec } from "../../lib/api";

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

function fmtTime(t: number): string {
  const d = new Date(t);
  return d.toLocaleTimeString(undefined, { hour: "2-digit", minute: "2-digit", hour12: false });
}

function fmtDuration(ms: number): string {
  const m = Math.max(0, Math.round(ms / 60000));
  if (m < 1) return "<1m";
  if (m < 60) return `${m}m`;
  const h = Math.floor(m / 60);
  const rem = m % 60;
  return rem === 0 ? `${h}h` : `${h}h${String(rem).padStart(2, "0")}m`;
}

function stintTitle(s: FlowStint): string {
  const start = fmtTime(s.t0);
  const end = s.t1 === null ? "now" : fmtTime(s.t1);
  const duration = fmtDuration((s.t1 ?? Date.now()) - s.t0);
  return `${s.task} · ${s.kind} · ${start}–${end} · ${duration}`;
}

function isInGap(model: FlowModel, x: number): boolean {
  return model.gaps.some((g) => {
    const x0 = model.x(g.t0);
    const x1 = model.x(g.t1);
    return x > x0 && x < x1;
  });
}

interface FlowLanesProps {
  model: FlowModel;
  agents: State["agents"];
  selected: string;
  onSelect: (taskId: string) => void;
  state: State;
  project: string;
}

export function FlowLanes({ model, agents, selected, onSelect, state, project }: FlowLanesProps) {
  const wrapRef = useRef<HTMLDivElement>(null);
  const scrollRef = useRef<HTMLDivElement>(null);
  const [baseWidth, setBaseWidth] = useState(MIN_W);
  const [zoom, setZoom] = useState<1 | 2 | 4>(1);
  const [idleExpanded, setIdleExpanded] = useState(false);
  const [selectedAgent, setSelectedAgent] = useState<string | null>(null);
  const [stats, setStats] = useState<ProjectStats | null>(null);
  const [statsErr, setStatsErr] = useState(false);
  const cardRef = useRef<HTMLDivElement>(null);
  const src = useDataSource();
  const blockedMap = useMemo(() => blockedTasks(state), [state]);

  useEffect(() => {
    const el = wrapRef.current;
    if (!el) return;
    const ro = new ResizeObserver((entries) => {
      const cw = entries[0]?.contentRect.width ?? 0;
      setBaseWidth(Math.max(cw, MIN_W));
    });
    ro.observe(el);
    setBaseWidth(Math.max(el.clientWidth, MIN_W));
    return () => ro.disconnect();
  }, []);

  const canvasWidth = baseWidth * zoom;

  const prevTMaxRef = useRef(model.tMax);
  useEffect(() => {
    const el = scrollRef.current;
    if (!el) return;
    if (model.tMax > prevTMaxRef.current) {
      const nearRight = el.scrollWidth - el.clientWidth - el.scrollLeft < 80;
      if (nearRight) {
        el.scrollLeft = el.scrollWidth - el.clientWidth;
      }
    }
    prevTMaxRef.current = model.tMax;
  }, [model.tMax]);

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

  const hasActivity = (id: string) =>
    model.stints.some((s) => s.agent === id) ||
    model.arrows.some((a) => a.from === id || a.to === id);

  const { activeLanes, idleLanes, showFoldRow, visibleLanes } = useMemo(() => {
    const active = laneOrder.filter((a) => hasActivity(a.id));
    const idle = laneOrder.filter((a) => !hasActivity(a.id));
    const allIdle = active.length === 0 && idle.length > 0;
    const showFold = !allIdle && idle.length > 0;
    const visible = allIdle ? laneOrder : [...active, ...(idleExpanded ? idle : [])];
    return { activeLanes: active, idleLanes: idle, showFoldRow: showFold, visibleLanes: visible };
  }, [laneOrder, model.stints, model.arrows, idleExpanded]);

  const laneIndex = useMemo(() => {
    const m = new Map<string, number>();
    visibleLanes.forEach((a, i) => m.set(a.id, i));
    return m;
  }, [visibleLanes]);

  const liveMap = useMemo(() => liveStates(model), [model]);

  useEffect(() => {
    if (!selectedAgent) return;
    let cancelled = false;
    setStatsErr(false);
    src.getStats(project)
      .then((s) => {
        if (!cancelled) setStats(s);
      })
      .catch(() => {
        if (!cancelled) setStatsErr(true);
      });
    return () => {
      cancelled = true;
    };
  }, [selectedAgent, project, src]);

  useEffect(() => {
    if (!selectedAgent) return;
    function handle(e: MouseEvent) {
      const target = e.target as Node;
      // Lane-row clicks toggle the card themselves; closing here first would
      // make a same-row click reopen instead of close.
      if ((target as Element).closest?.('[data-testid="flow-lane-row"]')) return;
      if (cardRef.current && !cardRef.current.contains(target)) {
        setSelectedAgent(null);
      }
    }
    document.addEventListener("mousedown", handle);
    return () => document.removeEventListener("mousedown", handle);
  }, [selectedAgent]);

  const ticks = useMemo(() => {
    if (model.tMax <= model.tMin) return [];
    const n = 6;
    const arr: number[] = [];
    for (let i = 0; i < n; i++) {
      const xi = i / (n - 1);
      if (isInGap(model, xi)) continue;
      arr.push(tAt(model, xi));
    }
    return arr;
  }, [model]);

  function rowOfLane(i: number): number {
    if (!showFoldRow) return i;
    return i < activeLanes.length ? i : i + 1;
  }

  function laneY(i: number): number {
    return HEADER_H + rowOfLane(i) * LANE_H + LANE_H / 2;
  }

  function px(t: number): number {
    return model.x(t) * canvasWidth;
  }

  if (model.lanes.length === 0 && model.stints.length === 0 && model.marks.length === 0) {
    return (
      <div className="grid flex-1 place-items-center text-sm text-[var(--color-text-2)]">
        No activity yet
      </div>
    );
  }

  const totalRows = visibleLanes.length + (showFoldRow ? 1 : 0);
  const svgHeight = HEADER_H + totalRows * LANE_H;

  function renderLaneRow(a: (typeof laneOrder)[number]) {
    const live = liveMap[a.id] ?? { kind: "idle" };
    const blockedDeps = live.task ? blockedMap.get(live.task) : undefined;
    const isBlocked = live.kind === "work" && blockedDeps && blockedDeps.length > 0;
    const isSelected = selectedAgent === a.id;

    let chipLabel: string;
    let chipColor: string;
    if (isBlocked) {
      chipLabel = `blocked · 等 ${blockedDeps[0]}`;
      chipColor = "var(--color-warn)";
    } else if (live.kind !== "idle") {
      chipLabel = live.kind === "work" ? "working" : live.kind === "review" ? "reviewing" : live.kind;
      chipColor = stintFill(live.kind);
    } else {
      chipLabel = "idle";
      chipColor = "var(--color-text-3)";
    }

    return (
      <div
        key={a.id}
        data-testid="flow-lane-row"
        data-agent={a.id}
        onClick={() => setSelectedAgent((cur) => (cur === a.id ? null : a.id))}
        className={`flex items-center gap-2.5 border-b border-[var(--color-border-subtle)] px-3 transition-colors ${
          isSelected ? "bg-[var(--color-bg-raised)]" : "cursor-pointer hover:bg-[var(--color-bg-page)]"
        }`}
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
        <span
          className="ml-auto rounded-full px-1.5 py-px text-[9.5px] font-medium"
          style={{
            color: chipColor,
            background: `color-mix(in srgb, ${chipColor} 14%, transparent)`,
            border: `1px solid color-mix(in srgb, ${chipColor} 30%, transparent)`,
          }}
        >
          {chipLabel}
        </span>
      </div>
    );
  }

  const selectedStat: AgentStat | undefined = selectedAgent
    ? stats?.agents.find((a) => a.seat === selectedAgent)
    : undefined;

  return (
    <div ref={wrapRef} className="relative flex flex-1 overflow-hidden">
      {/* Left seat rail */}
      <div className="shrink-0 border-r border-[var(--color-border-subtle)] bg-[var(--color-bg-inset)]" style={{ width: 176 }}>
        <div className="flex h-[32px] items-center justify-between px-3 text-[10px] font-semibold uppercase tracking-[.5px] text-[var(--color-text-3)]">
          <span>Seat</span>
          <div className="flex items-center gap-1">
            {[1, 2, 4].map((z) => (
              <button
                key={z}
                type="button"
                aria-pressed={zoom === z}
                onClick={(e) => {
                  e.stopPropagation();
                  setZoom(z as 1 | 2 | 4);
                }}
                className="rounded px-1.5 py-px transition-opacity hover:opacity-80"
                style={{
                  fontSize: 9.5,
                  fontWeight: 500,
                  color: zoom === z ? "var(--color-bg-page)" : "var(--color-text-3)",
                  background: zoom === z ? "var(--color-text-3)" : "transparent",
                }}
              >
                ×{z}
              </button>
            ))}
          </div>
        </div>
        {(showFoldRow ? activeLanes : visibleLanes).map(renderLaneRow)}
        {showFoldRow && (
          <button
            type="button"
            onClick={() => setIdleExpanded((v) => !v)}
            className="flex w-full items-center justify-between border-b border-[var(--color-border-subtle)] px-3 text-[10.5px] font-medium text-[var(--color-text-2)] transition-colors hover:bg-[var(--color-bg-page)]"
            style={{ height: LANE_H }}
          >
            <span>{idleLanes.length} idle seats</span>
            <span aria-hidden="true">{idleExpanded ? "▼" : "▶"}</span>
          </button>
        )}
        {idleExpanded && idleLanes.map(renderLaneRow)}
      </div>

      {/* Right canvas */}
      <div ref={scrollRef} className="flex-1 overflow-x-auto bg-[var(--color-bg-page)]">
        <svg width={canvasWidth} height={svgHeight} role="img" aria-label="Flow lanes">
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
                  <title>{fmtDuration(g.t1 - g.t0)} idle (compressed)</title>
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
                {/* Edge ticks anchor inward so labels never clip at the canvas bounds. */}
                <text
                  x={x}
                  y={18}
                  textAnchor={x < 24 ? "start" : x > canvasWidth - 24 ? "end" : "middle"}
                  fill="var(--color-text-3)"
                  fontSize={10}
                >
                  {fmtTime(t)}
                </text>
              </g>
            );
          })}

          {/* Lane separators */}
          {Array.from({ length: totalRows }).map((_, i) => (
            <line
              key={`sep-${i}`}
              x1={0}
              y1={HEADER_H + (i + 1) * LANE_H}
              x2={canvasWidth}
              y2={HEADER_H + (i + 1) * LANE_H}
              stroke="var(--color-border-subtle)"
              opacity={0.6}
            />
          ))}

          {/* Stints */}
          {model.stints.map((s, i) => {
            const laneIdx = laneIndex.get(s.agent);
            if (laneIdx === undefined) return null;
            const y = laneY(laneIdx);
            const x0 = px(s.t0);
            const x1 = s.t1 === null ? canvasWidth : px(s.t1);
            const barW = Math.max(4, x1 - x0);
            const fill = s.kind === "review" ? "url(#review-stripes)" : stintFill(s.kind);
            const isSelected = selected === s.task;
            const labelW = Math.max(0, barW - 12);
            const label = `${s.task} · ${s.kind}`;
            return (
              <g
                key={`stint-${i}`}
                data-testid="flow-stint"
                data-task={s.task}
                data-kind={s.kind}
                style={{ cursor: "pointer" }}
                onClick={() => onSelect(s.task)}
              >
                <title>{stintTitle(s)}</title>
                <rect
                  x={x0}
                  y={y - STINT_H / 2}
                  width={barW}
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
                    width={barW}
                    height={STINT_H}
                    rx={4}
                    fill="url(#live-fade)"
                    style={{ color: stintFill(s.kind), pointerEvents: "none" }}
                  />
                )}
                {barW > 60 && labelW > 0 && (
                  <text
                    x={x0 + 6}
                    y={y}
                    dy="0.32em"
                    fill="white"
                    fontSize={8.5}
                    fontFamily="monospace"
                    fontWeight={500}
                    pointerEvents="none"
                  >
                    {/* Truncate to what fits (mono ≈5.2px/char) — never stretch. */}
                    {label.length > Math.floor(labelW / 5.2)
                      ? `${label.slice(0, Math.max(0, Math.floor(labelW / 5.2) - 1))}…`
                      : label}
                  </text>
                )}
              </g>
            );
          })}

          {/* Arrows */}
          {model.arrows.map((a, i) => {
            const fromIdx = laneIndex.get(a.from);
            const toIdx = laneIndex.get(a.to);
            if (fromIdx === undefined || toIdx === undefined) return null;
            const y0 = laneY(fromIdx) + (a.from === a.to ? 0 : ARROW_OFFSET);
            const y1 = laneY(toIdx) + (a.from === a.to ? 0 : -ARROW_OFFSET);
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
            const laneIdx = laneIndex.get(m.agent);
            if (laneIdx === undefined) return null;
            const y = laneY(laneIdx);
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

      {/* Agent side card */}
      {selectedAgent && (
        <div
          ref={cardRef}
          data-testid="flow-agent-card"
          data-agent={selectedAgent}
          className="absolute right-3 top-3 z-10 w-56 rounded-xl border border-[var(--color-border-subtle)] bg-[var(--color-bg-surface)] p-3 shadow-lg"
        >
          <div className="flex items-center gap-2.5">
            <span
              className="grid h-8 w-8 place-items-center rounded-[8px] text-[10px] font-semibold"
              style={{
                background: roleColor(visibleLanes.find((a) => a.id === selectedAgent)?.roles),
                color: "var(--color-bg-page)",
              }}
            >
              {selectedAgent.slice(0, 2)}
            </span>
            <div className="flex min-w-0 flex-1 flex-col">
              <span className="truncate text-[12px] font-semibold text-[var(--color-text-1)]">{selectedAgent}</span>
              <span className="text-[10px] text-[var(--color-text-3)]">
                {roleLabel(visibleLanes.find((a) => a.id === selectedAgent)?.roles)}
              </span>
            </div>
            <button
              type="button"
              data-testid="flow-agent-card-close"
              onClick={() => setSelectedAgent(null)}
              className="grid h-6 w-6 place-items-center rounded-md text-[12px] text-[var(--color-text-3)] hover:bg-[var(--color-bg-raised)]"
            >
              ✕
            </button>
          </div>

          <div className="mt-3 grid grid-cols-2 gap-2 text-[11px]">
            {statsErr || !selectedStat ? (
              <div className="col-span-2 rounded-md bg-[var(--color-bg-inset)] px-2 py-1.5 text-[var(--color-text-3)]">
                {/* Fetch failure vs seat simply having no owned-task stats. */}
                {statsErr ? "stats unavailable" : "no task stats yet"}
              </div>
            ) : (
              <>
                <Stat label="Tasks" value={String(selectedStat.tasks)} />
                <Stat label="Accepted" value={String(selectedStat.accepted)} />
                <Stat label="Reworked" value={String(selectedStat.reworked)} />
                <Stat label="Tokens" value={String(selectedStat.tokens)} />
                <Stat
                  label="±LOC"
                  value={`${selectedStat.added > 0 ? "+" : ""}${selectedStat.added - selectedStat.deleted}`}
                />
                <Stat label="时长" value={fmtDurationSec(selectedStat.duration_sec)} />
              </>
            )}
          </div>
        </div>
      )}
    </div>
  );
}

function Stat({ label, value }: { label: string; value: string }) {
  return (
    <div className="rounded-md bg-[var(--color-bg-inset)] px-2 py-1.5">
      <div className="text-[9px] uppercase tracking-[.5px] text-[var(--color-text-3)]">{label}</div>
      <div className="mt-0.5 font-mono text-[var(--color-text-1)]">{value}</div>
    </div>
  );
}
