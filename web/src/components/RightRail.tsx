import type { State, PactEvent } from "../lib/types";
import { findTask } from "../lib/derive";
export function RightRail({ state, events, selected }: { state: State; events: PactEvent[]; selected: string }) {
  const bt = selected ? findTask(state, selected) : undefined;
  return (
    <div className="w-[320px] p-3 border-l border-gray-800 overflow-y-auto">
      {bt && (
        <div className="bg-[#161b22] border border-gray-700 rounded p-2 mb-3">
          <div className="text-[10px] font-semibold text-gray-500 uppercase">Evidence · {bt.task.id} ({bt.task.status})</div>
          <pre className="text-[10px] text-green-400 bg-[#0d1117] rounded p-2 mt-1 whitespace-pre-wrap">{bt.task.evidence || "(none yet)"}</pre>
        </div>
      )}
      <div className="text-[10px] font-semibold text-gray-500 uppercase mb-1">Live event stream</div>
      {events.slice().reverse().slice(0, 50).map((e) => (
        <div key={e.event_id} className="text-[10px] font-mono text-gray-300">{e.ts.slice(11, 19)} {e.agent_id} → {e.event_type} {e.task_id}</div>
      ))}
    </div>
  );
}
