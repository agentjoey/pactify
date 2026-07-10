import { useEffect, useState } from "react";
import type { State, PactEvent } from "../../lib/types";
import { deriveFlow } from "../../lib/flowderive";
import { FlowLanes } from "./FlowLanes";
import { FlowFeed } from "./FlowFeed";
import { FlowOffice } from "./FlowOffice";

const STORAGE_KEY = "pactify:flowMode";
type FlowMode = "lanes" | "feed" | "office";

interface FlowViewProps {
  state: State;
  events: PactEvent[];
  project: string;
  selected: string;
  onSelect: (taskId: string) => void;
}

export function FlowView({ state, events, selected, onSelect }: FlowViewProps) {
  const [mode, setMode] = useState<FlowMode>(() => {
    const saved = typeof localStorage !== "undefined" ? localStorage.getItem(STORAGE_KEY) : null;
    return (saved as FlowMode) ?? "lanes";
  });

  useEffect(() => {
    localStorage.setItem(STORAGE_KEY, mode);
  }, [mode]);

  const model = deriveFlow(events);

  return (
    <div className="flex flex-1 flex-col overflow-hidden">
      <div className="flex items-center gap-2 border-b border-[var(--color-border-subtle)] px-[18px] py-[9px]" style={{ background: "var(--color-bg-inset)" }}>
        <span className="text-[11px] font-semibold uppercase tracking-[.5px] text-[var(--color-text-2)]">Flow</span>
        <div className="ml-auto flex items-center gap-1 rounded-lg border border-[var(--color-border-subtle)] bg-[var(--color-bg-page)] p-0.5">
          {(["lanes", "feed", "office"] as FlowMode[]).map((m) => (
            <button
              key={m}
              type="button"
              data-testid={`flow-tab-${m}`}
              onClick={() => setMode(m)}
              className="rounded-md px-2.5 py-1 text-[11px] font-medium transition-colors"
              style={{
                color: mode === m ? "var(--color-text-1)" : "var(--color-text-3)",
                background: mode === m ? "rgba(255,255,255,0.09)" : "transparent",
              }}
            >
              {m === "lanes" ? "泳道" : m === "feed" ? "会话流" : "办公室"}
            </button>
          ))}
        </div>
      </div>

      {mode === "lanes" && (
        <FlowLanes model={model} agents={state.agents} selected={selected} onSelect={onSelect} />
      )}
      {mode === "feed" && (
        <FlowFeed events={events} agents={state.agents} selected={selected} onSelect={onSelect} />
      )}
      {mode === "office" && (
        <FlowOffice events={events} agents={state.agents} onSelect={onSelect} />
      )}
    </div>
  );
}
