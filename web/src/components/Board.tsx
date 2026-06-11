import type { CSSProperties } from "react";
import type { State } from "../lib/types";
import { boardColumns, COLUMNS } from "../lib/derive";
import { roleColorVar } from "../lib/canvas";
export function Board({ state, selected, onSelect, pulses }: { state: State; selected: string; onSelect: (id: string) => void; pulses?: Set<string> }) {
  const cols = boardColumns(state);
  // rolesOf indexes seat → roles so a pulsing card glows in its owner's color.
  const rolesOf = new Map(state.agents.map((a) => [a.id, a.roles]));
  return (
    <div className="flex gap-2 p-3 flex-1 overflow-x-auto">
      {COLUMNS.map((c) => (
        <div key={c} className="flex-1 min-w-[140px]">
          <div className="text-[10px] font-semibold tracking-wide text-gray-500 mb-1.5 uppercase">{c.replace("_", " ")}</div>
          {cols[c].map((bt) => {
            const pulsing = pulses?.has(bt.task.id);
            const roleVar = roleColorVar(rolesOf.get(bt.task.owner) ?? []);
            return (
              <button key={bt.task.id} onClick={() => onSelect(bt.task.id)}
                data-testid={pulsing ? "board-pulse" : undefined}
                style={pulsing ? ({ "--pulse-color": `var(${roleVar})` } as CSSProperties) : undefined}
                className={`w-full text-left bg-[#161b22] border rounded p-2 mb-1.5 text-xs ${pulsing ? "pulse " : ""}${selected === bt.task.id ? "border-yellow-500" : "border-gray-700"}`}>
                <div>{bt.task.id}</div>
                <div className="text-[10px] text-gray-500">{bt.task.owner}</div>
              </button>
            );
          })}
        </div>
      ))}
    </div>
  );
}
