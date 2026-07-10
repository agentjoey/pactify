import { useEffect, useMemo, useRef } from "react";
import { blockedTasks, deriveFlow, liveStates } from "../../lib/flowderive";
import type { PactEvent, State } from "../../lib/types";

type Verb = "join" | "assign" | "checkpoint" | "changes" | "accept" | "merge";

function asString(v: unknown): string | undefined {
  return typeof v === "string" ? v : undefined;
}

function asStringArray(v: unknown): string[] | undefined {
  return Array.isArray(v) && v.every((x) => typeof x === "string")
    ? (v as string[])
    : undefined;
}

function roleColor(roles: string[] | undefined): string {
  if (roles?.includes("orchestrator")) return "var(--color-role-product)";
  if (roles?.includes("reviewer")) return "var(--color-role-design)";
  return "var(--color-role-dev)";
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

function frameColor(verb: Verb): string {
  switch (verb) {
    case "checkpoint":
      return "var(--color-role-design)";
    case "changes":
      return "var(--color-danger)";
    case "accept":
      return "var(--color-success)";
    case "merge":
      return "var(--color-role-product)";
    default:
      return "var(--color-text-3)";
  }
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

interface FlowFeedProps {
  events: PactEvent[];
  agents: State["agents"];
  selected: string;
  onSelect: (taskId: string) => void;
  state: State;
}

export function FlowFeed({ events, agents, selected, onSelect, state }: FlowFeedProps) {
  const listRef = useRef<HTMLDivElement>(null);

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

  const assignCtx = useMemo(() => {
    const m = new Map<
      string,
      { owner?: string; reviewer?: string; reviewers?: string[] }
    >();
    for (const e of sorted) {
      if (normalizeVerb(e.event_type) === "assign") {
        const owner = asString(e.payload.owner);
        const reviewer = asString(e.payload.reviewer);
        const reviewers = asStringArray(e.payload.reviewers);
        if (e.task_id) {
          m.set(e.task_id, { owner, reviewer, reviewers });
        }
      }
    }
    return m;
  }, [sorted]);

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

  useEffect(() => {
    const el = listRef.current;
    if (!el) return;
    if (typeof el.scrollTo === "function") {
      el.scrollTo({ top: el.scrollHeight, behavior: "auto" });
    } else {
      el.scrollTop = el.scrollHeight;
    }
  }, [sorted]);

  function messageText(e: PactEvent): string {
    const verb = normalizeVerb(e.event_type);
    switch (verb) {
      case "join":
        return `${e.agent_id} joined`;
      case "assign": {
        const owner = asString(e.payload.owner) ?? "?";
        const reviewer = asString(e.payload.reviewer);
        return `assign ${e.task_id} → ${owner}${reviewer ? ` · reviewer ${reviewer}` : ""}`;
      }
      case "checkpoint": {
        const ctx = assignCtx.get(e.task_id);
        const reviewers =
          ctx?.reviewers && ctx.reviewers.length > 0
            ? ctx.reviewers
            : ctx?.reviewer
              ? [ctx.reviewer]
              : [];
        return `checkpoint ${e.task_id} → ${reviewers.join(", ") || "reviewer"}`;
      }
      case "changes": {
        const reason = asString(e.payload.reason);
        return `↺ changes ${e.task_id}${reason ? ` · "${reason}"` : ""}`;
      }
      case "accept":
        return `accept ✓ ${e.task_id}`;
      case "merge":
        return `merge${e.task_id ? ` · ${e.task_id}` : ""}`;
    }
  }

  if (sorted.length === 0) {
    return (
      <div className="grid flex-1 place-items-center text-sm text-[var(--color-text-2)]">
        No activity yet
      </div>
    );
  }

  return (
    <div className="flex flex-1 flex-col overflow-hidden">
      {/* Seat chips */}
      <div className="flex flex-wrap gap-2 border-b border-[var(--color-border-subtle)] bg-[var(--color-bg-inset)] px-4 py-3">
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
          return (
            <div
              key={a.id}
              data-testid="flow-feed-seat-chip"
              data-agent={a.id}
              className="flex items-center gap-2 rounded-full border border-[var(--color-border-subtle)] bg-[var(--color-bg-page)] px-2.5 py-1"
            >
              <span
                className="grid h-5 w-5 place-items-center rounded-md text-[9px] font-semibold"
                style={{ background: roleColor(a.roles), color: "var(--color-bg-page)" }}
              >
                {a.id.slice(0, 2)}
              </span>
              <span className="text-[11px] font-medium text-[var(--color-text-1)]">{a.id}</span>
              <span
                className="rounded-full px-1.5 py-px text-[9.5px] font-medium"
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
          );
        })}
      </div>

      {/* Message feed */}
      <div ref={listRef} className="flex-1 overflow-y-auto px-4 py-3">
        <div className="flex flex-col gap-2">
          {sorted.map((e) => {
            const verb = normalizeVerb(e.event_type);
            const taskId = e.task_id || undefined;
            const color = frameColor(verb);
            const isSelected = selected && selected === taskId;
            return (
              <button
                key={e.event_id}
                type="button"
                data-testid={`flow-msg-${verb}`}
                data-task={taskId}
                data-frame={verb === "changes" ? "danger" : verb === "checkpoint" ? "design" : verb === "accept" ? "success" : verb === "merge" ? "product" : "neutral"}
                disabled={!taskId}
                onClick={() => taskId && onSelect(taskId)}
                className={`flex items-center gap-3 rounded-lg border border-[var(--color-border-subtle)] bg-[var(--color-bg-surface)] px-3 py-2 text-left transition-colors ${
                  taskId ? "cursor-pointer hover:bg-[var(--color-bg-raised)]" : "cursor-default"
                }`}
                style={{
                  borderLeft: `3px solid ${color}`,
                  outline: isSelected ? "1px solid var(--color-role-product)" : undefined,
                }}
              >
                <span
                  className="grid h-6 w-6 shrink-0 place-items-center rounded-md text-[9px] font-semibold"
                  style={{ background: roleColor(agents.find((x) => x.id === e.agent_id)?.roles), color: "var(--color-bg-page)" }}
                >
                  {e.agent_id.slice(0, 2)}
                </span>
                <span className="flex-1 truncate font-mono text-[12px] text-[var(--color-text-1)]">
                  {messageText(e)}
                </span>
                <span className="shrink-0 font-mono text-[10px] text-[var(--color-text-3)]">
                  {fmtTime(e.ts)}
                </span>
              </button>
            );
          })}
        </div>
      </div>
    </div>
  );
}
