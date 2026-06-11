import type { State, PactEvent } from "../lib/types";
import { lastAction } from "../lib/derive";
import { Ant } from "./ui/ants/Ant";
import { casteForRoles, padGradient } from "../lib/ants";

export function Agents({ state, events, onPick }: { state: State; events: PactEvent[]; onPick: (id: string) => void }) {
  return (
    <div className="flex gap-2 px-3 py-2 border-b border-gray-800 flex-wrap">
      {state.agents.map((a) => {
        const la = lastAction(a.id, events);
        const caste = casteForRoles(a.roles);
        const pad = padGradient(a.id, caste);
        return (
          <button key={a.id} onClick={() => onPick(a.id)} className="flex items-center gap-2 text-left bg-[#161b22] border border-gray-700 rounded-lg px-3 py-1.5">
            <span
              className="flex items-center justify-center w-8 h-8 rounded-[10px] shrink-0"
              style={{
                background: `linear-gradient(135deg, ${pad.from}, ${pad.to})`,
                boxShadow: "inset 0 0 0 1px rgba(255,255,255,.16)",
              }}
            >
              <Ant caste={caste} size={22} title={`${caste} — ${a.roles.join("·") || "seat"}`} />
            </span>
            <span>
              <div className="text-sm font-semibold">{a.id} <span className="text-gray-500 font-normal">{a.roles.join("·")}</span></div>
              <div className="text-[10px] text-gray-400 font-mono">{la ? `last: ${la.event_type} ${la.task_id || ""}` : "no activity"}</div>
            </span>
          </button>
        );
      })}
    </div>
  );
}
