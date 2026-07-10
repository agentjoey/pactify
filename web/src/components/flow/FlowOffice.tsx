import { useEffect, useMemo, useState } from "react";
import { blockedTasks, deriveFlow, liveStates } from "../../lib/flowderive";
import type { PactEvent, State } from "../../lib/types";

type Verb = "join" | "assign" | "checkpoint" | "changes" | "accept" | "merge";

const ELAPSED_TICK_MS = 30_000;

function asString(v: unknown): string | undefined {
  return typeof v === "string" ? v : undefined;
}

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

function normalizeVerb(eventType: string): Verb {
  if (eventType === "changes_requested") return "changes";
  if (eventType === "join") return "join";
  if (eventType === "assign") return "assign";
  if (eventType === "checkpoint") return "checkpoint";
  if (eventType === "accept") return "accept";
  if (eventType === "merge") return "merge";
  return "join";
}

function stintColor(kind: "work" | "rework" | "review"): string {
  switch (kind) {
    case "work":
      return "var(--color-role-dev)";
    case "rework":
      return "var(--color-warn)";
    case "review":
      return "var(--color-role-design)";
  }
}

function fmtTime(ts: string): string {
  const d = new Date(ts);
  if (Number.isNaN(d.getTime())) return "--:--";
  return d.toLocaleTimeString(undefined, {
    hour: "2-digit",
    minute: "2-digit",
    hour12: false,
    timeZone: "UTC",
  });
}

function tickerText(e: PactEvent): string {
  const verb = normalizeVerb(e.event_type);
  const task = e.task_id ? ` · ${e.task_id}` : "";
  const note =
    verb === "changes" && asString(e.payload.reason)
      ? `: ${asString(e.payload.reason)}`
      : "";
  return `${e.agent_id} ${verb}${task}${note}`;
}

interface FlowOfficeProps {
  events: PactEvent[];
  agents: State["agents"];
  onSelect: (taskId: string) => void;
  state: State;
}

export function FlowOffice({ events, agents, onSelect, state }: FlowOfficeProps) {
  const sorted = useMemo(
    () =>
      [...events]
        .map((e) => ({ e, ms: Date.parse(e.ts) }))
        .filter((item) => !Number.isNaN(item.ms))
        .sort((a, b) => a.ms - b.ms)
        .map((item) => item.e),
    [events],
  );

  const model = useMemo(() => deriveFlow(events), [events]);
  const liveMap = useMemo(() => liveStates(model), [model]);
  const blockedMap = useMemo(() => blockedTasks(state), [state]);

  const [now, setNow] = useState(Date.now());
  useEffect(() => {
    const id = setInterval(() => setNow(Date.now()), ELAPSED_TICK_MS);
    return () => clearInterval(id);
  }, []);

  const openStintStart = useMemo(() => {
    const m = new Map<string, number>();
    for (const s of model.stints) {
      if (s.t1 === null && !m.has(s.agent)) {
        m.set(s.agent, s.t0);
      }
    }
    return m;
  }, [model]);

  const agentList = useMemo(() => {
    const firstT = new Map<string, number>();
    for (const e of sorted) {
      const ms = Date.parse(e.ts);
      if (Number.isNaN(ms)) continue;
      if (!firstT.has(e.agent_id) || ms < firstT.get(e.agent_id)!) {
        firstT.set(e.agent_id, ms);
      }
    }
    const all =
      agents.length
        ? agents
        : Array.from(firstT.keys()).map((id) => ({
            id,
            roles: [] as string[],
          }));
    return [...all].sort((a, b) => {
      const aOrc = a.roles.includes("orchestrator") ? 0 : 1;
      const bOrc = b.roles.includes("orchestrator") ? 0 : 1;
      if (aOrc !== bOrc) return aOrc - bOrc;
      const at = firstT.get(a.id) ?? Infinity;
      const bt = firstT.get(b.id) ?? Infinity;
      if (at !== bt) return at - bt;
      return a.id.localeCompare(b.id);
    });
  }, [agents, sorted]);

  const eventCounts = useMemo(() => {
    const m = new Map<string, number>();
    for (const e of sorted) {
      m.set(e.agent_id, (m.get(e.agent_id) ?? 0) + 1);
    }
    return m;
  }, [sorted]);

  const merges = useMemo(
    () => sorted.filter((e) => e.event_type === "merge"),
    [sorted],
  );
  const latestMerge = merges[merges.length - 1];

  const ticker = useMemo(() => {
    return [...sorted].reverse().slice(0, 3);
  }, [sorted]);

  return (
    <div className="flex flex-1 flex-col overflow-hidden">
      <div className="flex-1 overflow-y-auto p-4">
        {/* Main base */}
        <div
          className="mb-4 flex items-center gap-3 rounded-lg border border-[var(--color-border-subtle)] bg-[var(--color-bg-inset)] px-4 py-3"
          data-testid="flow-office-main"
        >
          <span className="font-mono text-sm text-[var(--color-role-product)]">⎇ main</span>
          <span className="ml-auto text-[11px] text-[var(--color-text-2)]">
            merges <span className="font-mono text-[var(--color-text-1)]">{merges.length}</span>
          </span>
          {latestMerge && (
            <span className="font-mono text-[10px] text-[var(--color-text-3)]">
              {latestMerge.task_id || latestMerge.feature || "—"} · {fmtTime(latestMerge.ts)}
            </span>
          )}
        </div>

        {/* Desk grid */}
        <div className="grid grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-3">
          {agentList.map((a) => {
            const live = liveMap[a.id] ?? { kind: "idle" };
            const blockedDeps = live.task ? blockedMap.get(live.task) : undefined;
            const isBlocked = live.kind === "work" && blockedDeps && blockedDeps.length > 0;
            let label: string;
            let color: string;
            if (isBlocked) {
              label = `blocked · 等 ${blockedDeps[0]}`;
              color = "var(--color-warn)";
            } else if (live.kind === "idle") {
              label = "idle";
              color = "var(--color-text-3)";
            } else {
              label = live.kind === "work" ? "working" : live.kind === "review" ? "reviewing" : live.kind;
              color = stintColor(live.kind);
            }
            const count = eventCounts.get(a.id) ?? 0;
            const t0 = openStintStart.get(a.id);
            const elapsedText = t0 !== undefined ? `elapsed ${Math.max(0, Math.round((now - t0) / 60000))}m` : undefined;
            return (
              <button
                key={a.id}
                type="button"
                data-testid="flow-desk"
                data-agent={a.id}
                disabled={!live.task}
                onClick={() => live.task && onSelect(live.task)}
                className={`flex flex-col gap-2 rounded-xl border border-[var(--color-border-subtle)] bg-[var(--color-bg-surface)] p-3 text-left transition-colors ${
                  live.task
                    ? "cursor-pointer hover:bg-[var(--color-bg-raised)]"
                    : "cursor-default"
                }`}
              >
                <div className="flex items-center gap-2.5">
                  <span
                    className="grid h-9 w-9 place-items-center rounded-[10px] text-[11px] font-semibold"
                    style={{ background: roleColor(a.roles), color: "var(--color-bg-page)" }}
                  >
                    {a.id.slice(0, 2)}
                  </span>
                  <div className="flex min-w-0 flex-col">
                    <span className="truncate text-[13px] font-semibold text-[var(--color-text-1)]">
                      {a.id}
                    </span>
                    <span className="text-[10px] text-[var(--color-text-3)]">{roleLabel(a.roles)}</span>
                  </div>
                  <span
                    className="ml-auto rounded-full px-2 py-px text-[9.5px] font-medium"
                    style={{
                      color,
                      background: `color-mix(in srgb, ${color} 12%, transparent)`,
                      border: `1px solid color-mix(in srgb, ${color} 28%, transparent)`,
                    }}
                  >
                    {label}
                    {live.task && !isBlocked ? ` · ${live.task}` : ""}
                  </span>
                </div>
                <div className="flex items-center justify-between text-[10px] text-[var(--color-text-3)]">
                  <span>
                    events <span className="font-mono text-[var(--color-text-1)]">{count}</span>
                  </span>
                  {elapsedText ? (
                    <span className="font-mono text-[10px] text-[var(--color-text-2)]" data-testid="flow-desk-elapsed">
                      {elapsedText}
                    </span>
                  ) : live.task ? (
                    <span className="font-mono text-[10px] text-[var(--color-text-2)]">
                      {live.task}
                    </span>
                  ) : null}
                </div>
              </button>
            );
          })}
        </div>
      </div>

      {/* Ticker */}
      <div className="border-t border-[var(--color-border-subtle)] bg-[var(--color-bg-inset)] px-4 py-3">
        <div className="flex flex-col gap-1">
          {ticker.map((e, i) => (
            <div
              key={e.event_id}
              className="font-mono text-[11px]"
              style={{
                color: "var(--color-text-2)",
                opacity: 1 - i * 0.25,
              }}
            >
              {tickerText(e)}
              <span className="ml-2 text-[10px] text-[var(--color-text-3)]">{fmtTime(e.ts)}</span>
            </div>
          ))}
          {ticker.length === 0 && (
            <div className="text-[11px] text-[var(--color-text-3)]">No activity yet</div>
          )}
        </div>
      </div>
    </div>
  );
}
