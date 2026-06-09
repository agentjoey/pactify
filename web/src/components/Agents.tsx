import type { State, PactEvent } from "../lib/types";
import { lastAction } from "../lib/derive";
export function Agents({ state, events, onPick }: { state: State; events: PactEvent[]; onPick: (id: string) => void }) {
  return (
    <div className="flex gap-2 px-3 py-2 border-b border-gray-800 flex-wrap">
      {state.agents.map((a) => {
        const la = lastAction(a.id, events);
        return (
          <button key={a.id} onClick={() => onPick(a.id)} className="text-left bg-[#161b22] border border-gray-700 rounded-lg px-3 py-1.5">
            <div className="text-sm font-semibold">🟢 {a.id} <span className="text-gray-500 font-normal">{a.roles.join("·")}</span></div>
            <div className="text-[10px] text-gray-400 font-mono">{la ? `last: ${la.event_type} ${la.task_id || ""}` : "no activity"}</div>
          </button>
        );
      })}
    </div>
  );
}
